package deploy

import (
	"archive/tar"
	"bufio"
	"bytes"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"Draft/internal/executil"
	"Draft/internal/gitsrc"
	"Draft/internal/ignore"
	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

type Engine struct {
	store          *store.Store
	router         *networking.Router
	emit           func(event string, data any)
	logDir         string
	execCommand    func(context.Context, string, ...string) *exec.Cmd
	mu             sync.Mutex
	active         map[string]context.CancelFunc // nodeID → cancel build
	watchMu        sync.Mutex
	watchers       map[string]context.CancelFunc // nodeID → cancel container wait
	watchGen       map[string]uint64             // nodeID → generation (avoid comparing funcs)
	logsMu         sync.Mutex
	logSubs        map[string]context.CancelFunc // nodeID → cancel log stream
	statsMu        sync.Mutex
	stats          map[string][]MetricPoint
	activityMu     sync.Mutex
	lastProxyTouch map[string]time.Time // nodeID → last sandbox activity touch
}

func New(s *store.Store, router *networking.Router, logDir string, emit func(string, any)) *Engine {
	os.MkdirAll(logDir, 0o755)
	return &Engine{
		store:          s,
		router:         router,
		emit:           emit,
		logDir:         logDir,
		execCommand:    executil.CommandContext,
		active:         make(map[string]context.CancelFunc),
		watchers:       make(map[string]context.CancelFunc),
		watchGen:       make(map[string]uint64),
		logSubs:        make(map[string]context.CancelFunc),
		stats:          make(map[string][]MetricPoint),
		lastProxyTouch: make(map[string]time.Time),
	}
}

type StatusEvent struct {
	DeploymentID uint   `json:"deploymentId"`
	Status       string `json:"status"`
	Hostname     string `json:"hostname,omitempty"`
	HostPort     int    `json:"hostPort,omitempty"`
	Error        string `json:"error,omitempty"`
}

type LogLine struct {
	Line   string `json:"line"`
	Stream string `json:"stream"` // "build", "stdout", "stderr"
}

type buildContextPlan struct {
	ContextRoot        string
	ServiceRoot        string
	DockerfilePath     string
	RelativeDockerfile string
}

func (e *Engine) Deploy(ctx context.Context, nodeID string) error {
	e.mu.Lock()
	if cancel, ok := e.active[nodeID]; ok {
		cancel()
	}
	buildCtx, cancel := context.WithCancel(ctx)
	e.active[nodeID] = cancel
	e.mu.Unlock()

	go func() {
		defer func() {
			e.mu.Lock()
			delete(e.active, nodeID)
			e.mu.Unlock()
			cancel()
		}()
		e.runDeployWith(buildCtx, nodeID, nil)
	}()
	return nil
}

func (e *Engine) runDeploy(ctx context.Context, nodeID string) {
	e.runDeployWith(ctx, nodeID, nil)
}

func (e *Engine) runDeployWith(ctx context.Context, nodeID string, settingsOverride map[string]string) {
	e.emitBuildLog(nodeID, "==> Initializing deployment...")
	e.emitBuildLog(nodeID, "    Loading node settings")

	settings, err := e.loadEffectiveSettings(nodeID)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "failed to read settings: " + err.Error()})
		return
	}
	for k, v := range settingsOverride {
		settings[k] = v
	}

	// Linked (virtualized) services do not run a local container.
	if link := ParseServiceLink(settings[SettingServiceLink]); link != nil {
		e.ensureLinkedDeploy(ctx, nodeID, link)
		return
	}

	dockerfilePath := settings["dockerfile"]
	portStr := settings["service_port"]
	imageRef := strings.TrimSpace(settings["image"])
	// Image-mode services (datastores + custom image templates) skip the build
	// entirely: they only need a port and an image to pull. Build-mode services
	// still require a Dockerfile path.
	if imageRef != "" && dockerfilePath == "" {
		if portStr == "" {
			e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "service_port is required"})
			return
		}
		var node store.CanvasNode
		if err := e.store.DB.First(&node, "id = ?", nodeID).Error; err != nil {
			e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "node not found"})
			return
		}
		project, err := e.store.GetProject(node.ProjectID)
		if err != nil {
			e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "project not found"})
			return
		}
		e.runImageDeploy(ctx, nodeID, settings, &node, project)
		return
	}
	if dockerfilePath == "" || portStr == "" {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "dockerfile and port are required settings"})
		return
	}

	var node store.CanvasNode
	if err := e.store.DB.First(&node, "id = ?", nodeID).Error; err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "node not found"})
		return
	}

	project, err := e.store.GetProject(node.ProjectID)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "project not found"})
		return
	}

	// By default the build reads directly from the project directory on disk.
	// If a git branch is pinned for this service, the build uses that branch's
	// committed files instead, leaving the working tree (and any uncommitted
	// changes) untouched. Two strategies:
	//   - stream (default): pipe `git archive` straight to Docker. Fastest, but
	//     .dockerignore/.gitignore and BuildKit local context do not apply.
	//   - checkout: materialize the branch into an ephemeral directory, then
	//     build it like a normal on-disk service (honors ignore files/BuildKit).
	// When the pinned tree has submodules, stream splices local module objects
	// when available; otherwise Draft falls back to an ephemeral worktree +
	// `git submodule update --init --recursive`.
	sourcePath := project.Path
	repoRoot := project.Path
	if strings.TrimSpace(settings["git_branch"]) != "" {
		if resolvedRoot, err := e.store.ResolveGitRepoRoot(ctx, nodeID, node.ProjectID); err == nil && resolvedRoot != "" {
			repoRoot = resolvedRoot
		}
	}
	gitBranch := gitsrc.PreferLocalRef(ctx, repoRoot, strings.TrimSpace(settings["git_branch"]))
	gitStream := gitBranch != "" && gitStreamEnabled(settings)
	var gitSourceCleanup func()
	defer func() {
		if gitSourceCleanup != nil {
			gitSourceCleanup()
		}
	}()

	if gitBranch != "" {
		pinned, err := e.resolvePinnedGitSource(ctx, nodeID, project.Path, repoRoot, gitBranch, dockerfilePath, settings["service_root"], gitStream, func(line string) {
			e.emitBuildLog(nodeID, line)
		})
		if err != nil {
			e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: err.Error()})
			return
		}
		gitSourceCleanup = pinned.Cleanup
		gitStream = pinned.Stream
		dockerfilePath = pinned.DockerfilePath
		if !pinned.Stream {
			sourcePath = pinned.SourcePath
		}
	}

	// For streaming, resolve the plan against the real project path — the path
	// math is filesystem-independent, and it yields the build-context subtree
	// and Dockerfile path we hand to `git archive` and Docker respectively.
	plan, err := resolveBuildContextPlan(sourcePath, settings["service_root"], dockerfilePath)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: err.Error()})
		return
	}

	e.emitBuildLog(nodeID, fmt.Sprintf("    Dockerfile: %s", plan.RelativeDockerfile))
	e.emitBuildLog(nodeID, fmt.Sprintf("    Service port: %s", portStr))
	e.emitBuildLog(nodeID, fmt.Sprintf("    Service root: %s", plan.ServiceRoot))
	e.emitBuildLog(nodeID, fmt.Sprintf("    Build context: %s", plan.ContextRoot))
	if plan.ContextRoot != plan.ServiceRoot {
		e.emitBuildLog(nodeID, "    Dockerfile is outside the service root; using the repository root as build context")
	}

	// Record the commit being built when a branch is pinned, so the git-trigger
	// reconciler can tell whether a later commit/push actually moved the tracked
	// branch past what we last deployed. Best-effort: a resolve failure must not
	// block the deploy.
	var sourceSHA string
	if gitBranch != "" {
		ref := gitBranch
		if i := strings.IndexByte(ref, ':'); i >= 0 {
			ref = ref[:i]
		}
		if sha, err := gitsrc.ResolveSHA(ctx, repoRoot, ref); err == nil {
			sourceSHA = sha
		}
	}

	dep := &store.Deployment{
		NodeID:         nodeID,
		ProjectID:      node.ProjectID,
		SourceSHA:      sourceSHA,
		Status:         "building",
		JobID:          fmt.Sprintf("%d-%s", time.Now().UnixNano(), nodeID),
		WorkerPID:      os.Getpid(),
		StartedAt:      ptrTime(time.Now()),
		BuildStartedAt: ptrTime(time.Now()),
		LastSeenAt:     ptrTime(time.Now()),
	}
	dep, err = e.store.CreateDeployment(dep)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "failed to create deployment record"})
		return
	}

	addr, err := e.computeNodeAddress(&node)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "failed to resolve node identity: " + err.Error()})
		return
	}
	serviceName := addr.ServiceName
	projectName := addr.ProjectName
	uid, err := e.store.EnsureNodeUID(nodeID)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "failed to resolve node identity: " + err.Error()})
		return
	}
	deployEnv, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:           nodeID,
		ProjectID:        node.ProjectID,
		EnvironmentID:    node.EnvironmentID,
		ServiceName:      addr.ServiceName,
		ProjectName:      addr.ProjectName,
		Environment:      addr.Environment,
		ServicePort:      addr.ServicePort,
		InternalHostname: addr.InternalHostname,
		InternalURL:      addr.InternalURL,
		PublicHostname:   addr.PublicHostname,
		PublicURL:        addr.PublicURL,
	})
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "failed to resolve environment: " + err.Error()})
		return
	}

	imageTag := draftImageTag(projectName, addr.DockerEnvironment, serviceName, dep.Sequence)
	dep.ImageTag = imageTag
	dep.LastSeenAt = ptrTime(time.Now())
	e.store.UpdateDeployment(dep)

	e.emitBuildLog(nodeID, fmt.Sprintf("    Image tag: %s", imageTag))
	e.emitBuildLog(nodeID, fmt.Sprintf("    Runtime variables: %d", len(deployEnv.RuntimeEnv)))
	e.emitBuildLog(nodeID, fmt.Sprintf("    Build args: %d", len(deployEnv.BuildArgs)))

	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "building"})

	e.emitBuildLog(nodeID, "==> Connecting to Docker daemon...")
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		e.failDeployment(dep, nodeID, "cannot connect to Docker: "+err.Error())
		return
	}
	defer cli.Close()

	ping, pingErr := cli.Ping(ctx)
	if pingErr != nil {
		e.failDeployment(dep, nodeID, "Docker daemon not reachable: "+pingErr.Error())
		return
	}
	e.emitBuildLog(nodeID, fmt.Sprintf("    Connected (API v%s)", ping.APIVersion))

	logFile, err := os.Create(e.logPath(dep.ID))
	if err != nil {
		e.failDeployment(dep, nodeID, "cannot create build log: "+err.Error())
		return
	}
	defer logFile.Close()

	hooks := parseLifecycleHooks(settings)
	overrides := parseContainerOverrides(settings)
	buildOvr := parseBuildOverrides(settings)

	if hooks.PreBuild != "" {
		if err := runLifecycleHook(ctx, "pre-build", hooks.PreBuild, plan.ServiceRoot, func(line string) {
			e.emitBuildLog(nodeID, line)
		}); err != nil {
			e.failDeployment(dep, nodeID, err.Error())
			return
		}
	}

	var buildErr error
	if gitStream {
		buildErr = e.buildImageGitStream(ctx, cli, logFile, nodeID, imageTag, repoRoot, gitBranch, plan, deployEnv, buildOvr)
	} else {
		buildErr = e.buildImage(ctx, cli, logFile, nodeID, imageTag, sourcePath, settings, plan, deployEnv, buildOvr)
	}
	if buildErr != nil {
		e.failDeployment(dep, nodeID, "build error: "+buildErr.Error())
		return
	}

	if hooks.PostBuild != "" {
		if err := runLifecycleHook(ctx, "post-build", hooks.PostBuild, plan.ServiceRoot, func(line string) {
			e.emitBuildLog(nodeID, line)
		}); err != nil {
			e.failDeployment(dep, nodeID, err.Error())
			return
		}
	}

	if ctx.Err() != nil {
		e.failDeployment(dep, nodeID, "build cancelled")
		return
	}

	now := time.Now()
	dep.Status = "built"
	dep.BuildFinishedAt = &now
	dep.LastSeenAt = &now
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "built"})

	// The previous deployment is intentionally left running here. It keeps
	// serving traffic while the new container is created, started, and verified
	// ready below; only once the route is repointed to the new container do we
	// retire the old one. This makes the cutover zero-downtime (blue-green) and
	// leaves the previous deployment in place as an automatic rollback if the
	// new container never becomes ready.

	if hooks.PreDeploy != "" {
		if err := runLifecycleHook(ctx, "pre-deploy", hooks.PreDeploy, plan.ServiceRoot, func(line string) {
			e.emitBuildLog(nodeID, line)
		}); err != nil {
			e.failDeployment(dep, nodeID, err.Error())
			return
		}
	}

	e.startContainerAndRegister(ctx, cli, dep, &node, settings, deployEnv, overrides, hooks, plan.ServiceRoot, imageTag, portStr, addr, uid)
}

// prepareGitSource materializes the given branch/ref of the project's git
// repository into a fresh temporary directory and returns its path. The caller
// owns the returned directory and must remove it when done. The project's
// working tree and index are never read or modified.
//
// contextRel is the build-context path relative to the repo root (e.g.
// "backend"); only that subtree is exported, mirrored at the same relative
// location inside the workspace so the caller's service-root/context
// resolution against the workspace is identical to the on-disk project. This
// avoids materializing an entire monorepo when a service lives in one
// subdirectory. Pass "." (or "") to export the whole repository.
func (e *Engine) prepareGitSource(ctx context.Context, nodeID, repoPath, ref, contextRel string) (string, error) {
	if !gitsrc.IsRepo(repoPath) {
		return "", fmt.Errorf("git branch is set but %s is not a git repository", repoPath)
	}

	contextRel = filepath.ToSlash(strings.TrimSpace(contextRel))

	archiveDir, err := os.MkdirTemp("", "draft-src-")
	if err != nil {
		return "", fmt.Errorf("create source workspace: %w", err)
	}

	treeish := ref
	destDir := archiveDir
	exported := fmt.Sprintf("%q", ref)
	if contextRel != "" && contextRel != "." {
		treeish = ref + ":" + contextRel
		destDir = filepath.Join(archiveDir, filepath.FromSlash(contextRel))
		exported = fmt.Sprintf("%q:%s", ref, contextRel)
		if err := os.MkdirAll(destDir, 0o755); err != nil {
			os.RemoveAll(archiveDir)
			return "", fmt.Errorf("create source workspace: %w", err)
		}
	}

	e.emitBuildLog(nodeID, fmt.Sprintf("==> Preparing source from git branch %q...", ref))

	if err := gitsrc.ArchiveToDir(ctx, repoPath, treeish, destDir); err != nil {
		os.RemoveAll(archiveDir)
		return "", err
	}

	modulePrefix := ""
	if contextRel != "" && contextRel != "." {
		modulePrefix = contextRel
	}
	links, err := gitsrc.ListGitlinks(ctx, repoPath, treeish)
	if err != nil {
		os.RemoveAll(archiveDir)
		return "", err
	}
	if len(links) > 0 {
		e.emitBuildLog(nodeID, fmt.Sprintf("    Expanding %d submodule(s) from local modules cache...", len(links)))
		if err := gitsrc.MaterializeSubmodules(ctx, repoPath, treeish, destDir, modulePrefix); err != nil {
			os.RemoveAll(archiveDir)
			return "", err
		}
	}

	e.emitBuildLog(nodeID, fmt.Sprintf("    Exported %s into an ephemeral workspace (working tree untouched)", exported))
	return archiveDir, nil
}

// rebaseUnderSource re-anchors a path from the on-disk project directory onto
// the archived source directory. Relative paths are returned unchanged (they
// already resolve against the source root). An absolute path that lives inside
// projectPath is rewritten to the equivalent location under sourcePath. An
// absolute path outside the project cannot be reproduced from a branch archive
// and is rejected with a clear error.
func rebaseUnderSource(projectPath, sourcePath, p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) {
		return p, nil
	}
	rel, err := filepath.Rel(projectPath, p)
	if err != nil || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", fmt.Errorf("dockerfile %q is outside the project and cannot be resolved from a git branch; set it to a path inside the project", p)
	}
	return filepath.Join(sourcePath, rel), nil
}

func resolveBuildContextPlan(projectPath, serviceRootSetting, dockerfilePath string) (buildContextPlan, error) {
	serviceRoot := projectPath
	if rel := strings.TrimSpace(serviceRootSetting); rel != "" {
		// Absolute service_root is allowed for imports whose compose context
		// resolved outside the Draft project folder.
		if filepath.IsAbs(rel) {
			serviceRoot = rel
		} else {
			serviceRoot = filepath.Join(projectPath, rel)
		}
	}

	absDockerfile := dockerfilePath
	if !filepath.IsAbs(absDockerfile) {
		absDockerfile = filepath.Join(serviceRoot, absDockerfile)
	}
	absDockerfile = filepath.Clean(absDockerfile)

	contextRoot := serviceRoot
	relDockerfile, err := filepath.Rel(contextRoot, absDockerfile)
	if err != nil || relDockerfile == "." || strings.HasPrefix(relDockerfile, "..") {
		contextRoot = projectPath
		relDockerfile, err = filepath.Rel(contextRoot, absDockerfile)
		if err != nil || relDockerfile == "." || strings.HasPrefix(relDockerfile, "..") {
			return buildContextPlan{}, fmt.Errorf("cannot resolve dockerfile path")
		}
	}

	return buildContextPlan{
		ContextRoot:        filepath.Clean(contextRoot),
		ServiceRoot:        filepath.Clean(serviceRoot),
		DockerfilePath:     absDockerfile,
		RelativeDockerfile: filepath.ToSlash(relDockerfile),
	}, nil
}

func (e *Engine) buildImage(ctx context.Context, cli *client.Client, logFile *os.File, nodeID, imageTag, projectPath string, settings map[string]string, plan buildContextPlan, deployEnv deploymentEnv, bo buildOverrides) error {
	if buildkitEnabled(settings) {
		if compatible, reason := buildxCompatibleWithSettings(projectPath, plan, settings); compatible {
			if status, err := e.inspectBuildx(ctx); err == nil {
				e.emitBuildLog(nodeID, "==> Using BuildKit local-context deploy...")
				e.emitBuildLog(nodeID, fmt.Sprintf("    Builder: %s", status.Name))
				e.emitBuildLog(nodeID, fmt.Sprintf("    BuildKit: %s", status.Version))
				if err := e.buildImageWithBuildx(ctx, logFile, nodeID, imageTag, plan, deployEnv, bo); err != nil {
					if ctx.Err() != nil {
						return ctx.Err()
					}
					e.emitBuildLog(nodeID, fmt.Sprintf("    BuildKit local-context build failed; falling back to legacy tar upload: %v", err))
				} else {
					return nil
				}
			} else {
				e.emitBuildLog(nodeID, fmt.Sprintf("==> BuildKit local-context unavailable; using legacy tar upload (%v)", err))
			}
		} else {
			e.emitBuildLog(nodeID, fmt.Sprintf("==> BuildKit local-context skipped; using legacy tar upload (%s)", reason))
		}
	} else {
		e.emitBuildLog(nodeID, "==> BuildKit local-context disabled for this service; using legacy tar upload")
	}

	return e.buildImageLegacy(ctx, cli, logFile, nodeID, imageTag, projectPath, settings, plan, deployEnv, bo)
}

func buildkitEnabled(settings map[string]string) bool {
	value := strings.TrimSpace(strings.ToLower(settings["use_buildkit_local_context"]))
	return value != "false" && value != "0" && value != "off"
}

// gitStreamEnabled reports whether a pinned git branch should be streamed
// directly to Docker (the default) rather than checked out into a temp dir.
func gitStreamEnabled(settings map[string]string) bool {
	value := strings.TrimSpace(strings.ToLower(settings["git_stream"]))
	return value != "false" && value != "0" && value != "off"
}

type buildxStatus struct {
	Name    string
	Version string
}

func (e *Engine) inspectBuildx(ctx context.Context) (buildxStatus, error) {
	if _, err := exec.LookPath("docker"); err != nil {
		return buildxStatus{}, fmt.Errorf("docker CLI not found")
	}

	inspectCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	cmd := e.execCommand(inspectCtx, "docker", "buildx", "inspect", "--bootstrap")
	output, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(output))
		if msg == "" {
			msg = err.Error()
		}
		return buildxStatus{}, fmt.Errorf("%s", msg)
	}

	status := buildxStatus{Name: "buildx"}
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		switch {
		case strings.HasPrefix(line, "Name:"):
			status.Name = strings.TrimSpace(strings.TrimPrefix(line, "Name:"))
		case strings.HasPrefix(line, "BuildKit version:"):
			status.Version = strings.TrimSpace(strings.TrimPrefix(line, "BuildKit version:"))
		}
	}
	if status.Version == "" {
		status.Version = "available"
	}
	return status, nil
}

func (e *Engine) buildImageWithBuildx(ctx context.Context, logFile *os.File, nodeID, imageTag string, plan buildContextPlan, deployEnv deploymentEnv, bo buildOverrides) error {
	args := []string{
		"buildx", "build",
		"--load",
		"--progress=plain",
		"-t", imageTag,
		"-f", plan.RelativeDockerfile,
	}
	if bo.Target != "" {
		args = append(args, "--target", bo.Target)
	}
	if bo.Platform != "" {
		args = append(args, "--platform", bo.Platform)
	}
	if bo.NoCache {
		args = append(args, "--no-cache")
	}
	for _, arg := range buildArgsForCLI(deployEnv.BuildArgs) {
		args = append(args, "--build-arg", arg)
	}
	args = append(args, plan.ContextRoot)

	e.emitBuildLog(nodeID, "==> Handing local directory context to BuildKit...")
	e.emitBuildLog(nodeID, fmt.Sprintf("    docker %s", strings.Join(args, " ")))

	cmd := e.execCommand(ctx, "docker", args...)
	writer := newLineEmitterWriter(func(line string) {
		logFile.WriteString(line + "\n")
		e.emitBuildLog(nodeID, line)
	})
	cmd.Stdout = writer
	cmd.Stderr = writer

	buildStart := time.Now()
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("start buildx: %w", err)
	}
	err := cmd.Wait()
	writer.Flush()
	if err != nil {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		return fmt.Errorf("buildx: %w", err)
	}
	e.emitBuildLog(nodeID, fmt.Sprintf("==> Build completed in %s", time.Since(buildStart).Round(time.Millisecond)))
	return nil
}

func (e *Engine) buildImageLegacy(ctx context.Context, cli *client.Client, logFile *os.File, nodeID, imageTag, projectPath string, settings map[string]string, plan buildContextPlan, deployEnv deploymentEnv, bo buildOverrides) error {
	matcher := ignore.New()
	if settings["use_dockerignore"] == "true" {
		e.emitBuildLog(nodeID, "==> Scanning for .dockerignore files...")
		n, _ := matcher.ScanDir(projectPath, plan.ContextRoot, ".dockerignore")
		e.emitBuildLog(nodeID, fmt.Sprintf("    Found %d .dockerignore file(s)", n))
	}
	if settings["use_gitignore"] == "true" {
		e.emitBuildLog(nodeID, "==> Scanning for .gitignore files...")
		n, _ := matcher.ScanDir(projectPath, plan.ContextRoot, ".gitignore")
		e.emitBuildLog(nodeID, fmt.Sprintf("    Found %d .gitignore file(s)", n))
	}

	e.emitBuildLog(nodeID, "==> Packaging build context...")
	packStart := time.Now()
	buildContext, fileCount, totalBytes, err := tarDirectoryWithProgress(plan.ContextRoot, matcher, func(files int, bytes int64) {
		e.emitBuildLog(nodeID, fmt.Sprintf("    Packaged %d files (%.1f MB)", files, float64(bytes)/(1024*1024)))
	})
	if err != nil {
		return fmt.Errorf("cannot create build context: %w", err)
	}
	defer buildContext.Close()
	e.emitBuildLog(nodeID, fmt.Sprintf("    Done: %d files, %.1f MB in %s", fileCount, float64(totalBytes)/(1024*1024), time.Since(packStart).Round(time.Millisecond)))

	e.emitBuildLog(nodeID, "==> Sending build context to Docker...")
	e.emitBuildLog(nodeID, fmt.Sprintf("    docker build -t %s -f %s", imageTag, plan.RelativeDockerfile))

	uploadStart := time.Now()
	tracker := &uploadTracker{
		reader:     buildContext,
		totalBytes: totalBytes,
		onProgress: func(sent int64, total int64) {
			pct := int(float64(sent) / float64(total) * 100)
			e.emit("deploy:upload-progress", map[string]any{
				"nodeId":     nodeID,
				"percent":    pct,
				"sentBytes":  sent,
				"totalBytes": total,
			})
		},
	}

	resp, err := cli.ImageBuild(ctx, tracker, legacyImageBuildOptions(imageTag, plan.RelativeDockerfile, deployEnv.BuildArgs, bo))
	if err != nil {
		return fmt.Errorf("docker build failed: %w", err)
	}
	defer resp.Body.Close()
	e.emit("deploy:upload-progress", map[string]any{
		"nodeId":     nodeID,
		"percent":    100,
		"sentBytes":  totalBytes,
		"totalBytes": totalBytes,
		"done":       true,
	})
	e.emitBuildLog(nodeID, fmt.Sprintf("    Upload complete in %s", time.Since(uploadStart).Round(time.Millisecond)))

	buildStart := time.Now()
	buildErr := e.streamBuildOutput(ctx, resp.Body, logFile, nodeID, 0)
	if buildErr != nil {
		return buildErr
	}
	e.emitBuildLog(nodeID, fmt.Sprintf("==> Build completed in %s", time.Since(buildStart).Round(time.Millisecond)))
	return nil
}

// gitArchiveTreeish builds the `git archive` tree-ish for a build context that
// lives at contextRoot inside the repo at projectPath. When the context is the
// repo root the bare ref is used; for a subdirectory the `<ref>:<subdir>` form
// roots the archive at that subdirectory. A context outside the repo is an
// error — it cannot be reproduced from a branch archive.
func gitArchiveTreeish(repoRoot, contextRoot, ref string) (string, error) {
	contextRel, err := filepath.Rel(repoRoot, contextRoot)
	if err != nil {
		return "", fmt.Errorf("resolve build context within repo: %w", err)
	}
	contextRel = filepath.ToSlash(contextRel)
	if contextRel == ".." || strings.HasPrefix(contextRel, "../") {
		return "", fmt.Errorf("build context %q is outside the repository and cannot be streamed from a git branch", contextRoot)
	}
	if contextRel == "." || contextRel == "" {
		return ref, nil
	}
	return ref + ":" + contextRel, nil
}

// buildImageGitStream builds directly from a git branch by piping
// `git archive <ref>[:<context-subdir>]` straight into the Docker build API.
// It never materializes the branch to disk, so it is the fastest path — but
// .dockerignore/.gitignore filtering and BuildKit local context do not apply
// (the tar comes straight from git's object store, not the filesystem).
func (e *Engine) buildImageGitStream(ctx context.Context, cli *client.Client, logFile *os.File, nodeID, imageTag, repoRoot, ref string, plan buildContextPlan, deployEnv deploymentEnv, bo buildOverrides) error {
	if !gitsrc.IsRepo(repoRoot) {
		return fmt.Errorf("git branch is set but %s is not a git repository", repoRoot)
	}
	if err := gitsrc.VerifyRef(ctx, repoRoot, ref); err != nil {
		return err
	}

	// Archive only the build-context subtree. `<ref>:<subdir>` roots the archive
	// at that subdir, so entries line up with plan.RelativeDockerfile.
	treeish, err := gitArchiveTreeish(repoRoot, plan.ContextRoot, ref)
	if err != nil {
		return err
	}

	e.emitBuildLog(nodeID, "==> Streaming git branch to Docker...")
	e.emitBuildLog(nodeID, fmt.Sprintf("    git archive --format=tar %s", treeish))
	e.emitBuildLog(nodeID, fmt.Sprintf("    docker build -t %s -f %s", imageTag, plan.RelativeDockerfile))
	e.emitBuildLog(nodeID, "    Note: .dockerignore, .gitignore and BuildKit local context do not apply while streaming")

	modulePrefix := ""
	if contextRel, err := filepath.Rel(repoRoot, plan.ContextRoot); err == nil {
		contextRel = filepath.ToSlash(contextRel)
		if contextRel != "" && contextRel != "." {
			modulePrefix = contextRel
		}
	}
	links, err := gitsrc.ListGitlinks(ctx, repoRoot, treeish)
	if err != nil {
		return err
	}
	spliceSubs := len(links) > 0
	if spliceSubs {
		ok, availErr := gitsrc.SubmoduleObjectsAvailable(ctx, repoRoot, treeish, modulePrefix)
		if availErr != nil {
			return availErr
		}
		if !ok {
			return fmt.Errorf("%w: cannot stream with missing submodule objects", gitsrc.ErrSubmoduleObjectsMissing)
		}
		e.emitBuildLog(nodeID, fmt.Sprintf("    Including %d submodule(s) via archive splice", len(links)))
	}

	// A dedicated cancellable context lets us tear down git archive if Docker
	// rejects the build before draining the tar, avoiding a blocked-writer hang.
	gitCtx, cancelGit := context.WithCancel(ctx)
	defer cancelGit()

	pr, pw := io.Pipe()
	var archiveErr error
	var archiveWG sync.WaitGroup
	archiveWG.Add(1)
	go func() {
		defer archiveWG.Done()
		if spliceSubs {
			archiveErr = gitsrc.WriteArchiveWithSubmodules(gitCtx, repoRoot, treeish, modulePrefix, pw)
			if archiveErr != nil {
				_ = pw.CloseWithError(archiveErr)
				return
			}
			_ = pw.Close()
			return
		}
		cmd := e.execCommand(gitCtx, "git", "-C", repoRoot, "archive", "--format=tar", treeish)
		var stderr bytes.Buffer
		cmd.Stdout = pw
		cmd.Stderr = &stderr
		if err := cmd.Run(); err != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			archiveErr = fmt.Errorf("git archive %s: %s", treeish, msg)
			_ = pw.CloseWithError(archiveErr)
			return
		}
		_ = pw.Close()
	}()

	uploadStart := time.Now()
	// clearProgress hides the (indeterminate) upload bar. It must fire exactly
	// once the tar has been fully sent — i.e. when the archive stream reaches
	// EOF — not after the build, so the bar doesn't linger through the build.
	var progressCleared atomic.Bool
	clearProgress := func(sent int64) {
		if progressCleared.Swap(true) {
			return
		}
		e.emit("deploy:upload-progress", map[string]any{
			"nodeId":        nodeID,
			"sentBytes":     sent,
			"indeterminate": true,
			"done":          true,
		})
		e.emitBuildLog(nodeID, fmt.Sprintf("    Streamed %.1f MB of committed files in %s", float64(sent)/(1024*1024), time.Since(uploadStart).Round(time.Millisecond)))
	}
	reader := &countingReader{
		r: pr,
		onProgress: func(sent int64) {
			e.emit("deploy:upload-progress", map[string]any{
				"nodeId":        nodeID,
				"sentBytes":     sent,
				"indeterminate": true,
			})
		},
		onEOF: clearProgress,
	}

	e.emitBuildLog(nodeID, "==> Sending build context to Docker...")
	e.emitBuildLog(nodeID, "    Only files committed to this branch are sent (uncommitted and gitignored files such as node_modules are excluded)")
	resp, err := cli.ImageBuild(ctx, reader, legacyImageBuildOptions(imageTag, plan.RelativeDockerfile, deployEnv.BuildArgs, bo))
	if err != nil {
		cancelGit()
		_ = pr.Close()
		archiveWG.Wait()
		clearProgress(atomic.LoadInt64(&reader.n))
		return fmt.Errorf("docker build failed: %w", err)
	}
	defer resp.Body.Close()

	buildStart := time.Now()
	buildErr := e.streamBuildOutput(ctx, resp.Body, logFile, nodeID, 0)

	// The build response is fully read above, so the entire tar has been sent
	// and the archive producer has finished; Wait reaps it and surfaces errors.
	_ = pr.Close()
	archiveWG.Wait()

	// Belt-and-suspenders: ensure the bar is cleared even if EOF wasn't observed.
	clearProgress(atomic.LoadInt64(&reader.n))

	if buildErr != nil {
		return buildErr
	}
	if archiveErr != nil && ctx.Err() == nil {
		return archiveErr
	}
	e.emitBuildLog(nodeID, fmt.Sprintf("==> Build completed in %s", time.Since(buildStart).Round(time.Millisecond)))
	return nil
}

// countingReader tallies bytes read, periodically reports progress, and fires
// onEOF once when the underlying stream is exhausted. n is updated atomically
// so a caller in another goroutine can read the final total safely.
type countingReader struct {
	r          io.Reader
	n          int64
	lastEmit   int64
	firedEOF   bool
	onProgress func(sent int64)
	onEOF      func(sent int64)
}

func (c *countingReader) Read(p []byte) (int, error) {
	n, err := c.r.Read(p)
	if n > 0 {
		total := atomic.AddInt64(&c.n, int64(n))
		if c.onProgress != nil && total-c.lastEmit >= 4<<20 { // every ~4 MB
			c.lastEmit = total
			c.onProgress(total)
		}
	}
	if err == io.EOF && !c.firedEOF {
		c.firedEOF = true
		if c.onEOF != nil {
			c.onEOF(atomic.LoadInt64(&c.n))
		}
	}
	return n, err
}

func buildArgsForCLI(buildArgs map[string]*string) []string {
	if len(buildArgs) == 0 {
		return nil
	}
	keys := make([]string, 0, len(buildArgs))
	for key := range buildArgs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	args := make([]string, 0, len(keys))
	for _, key := range keys {
		if buildArgs[key] == nil {
			args = append(args, key+"=")
			continue
		}
		args = append(args, key+"="+*buildArgs[key])
	}
	return args
}

func legacyImageBuildOptions(imageTag, relativeDockerfile string, buildArgs map[string]*string, bo buildOverrides) build.ImageBuildOptions {
	opts := build.ImageBuildOptions{
		Tags:       []string{imageTag},
		Dockerfile: relativeDockerfile,
		Remove:     true,
		BuildArgs:  buildArgs,
		Version:    build.BuilderV1,
		NoCache:    bo.NoCache,
	}
	if bo.Target != "" {
		opts.Target = bo.Target
	}
	if bo.Platform != "" {
		opts.Platform = bo.Platform
	}
	return opts
}

func buildxCompatibleWithSettings(projectPath string, plan buildContextPlan, settings map[string]string) (bool, string) {
	if strings.EqualFold(strings.TrimSpace(settings["use_gitignore"]), "true") {
		return false, ".gitignore-based context filtering is enabled"
	}

	rootDockerignore := filepath.Join(plan.ContextRoot, ".dockerignore")
	hasRootDockerignore := fileExists(rootDockerignore)
	useDockerignore := strings.EqualFold(strings.TrimSpace(settings["use_dockerignore"]), "true")
	legacySkips, err := topLevelLegacySkipEntries(plan.ContextRoot)
	if err != nil {
		return false, fmt.Sprintf("could not inspect the build context (%v)", err)
	}

	if !useDockerignore && hasRootDockerignore {
		return false, "the service has a root .dockerignore but the Draft .dockerignore toggle is off"
	}
	if useDockerignore {
		matcher := ignore.New()
		count, _ := matcher.ScanDir(projectPath, plan.ContextRoot, ".dockerignore")
		if count > 1 || (count == 1 && !hasRootDockerignore) {
			return false, "the current .dockerignore mode relies on nested or ancestor ignore files"
		}
	}
	if len(legacySkips) > 0 {
		if !useDockerignore || !hasRootDockerignore {
			return false, fmt.Sprintf("the build context includes Draft-skipped entries (%s) without a compatible root .dockerignore", strings.Join(legacySkips, ", "))
		}
		matcher := ignore.New()
		if err := matcher.AddFile(rootDockerignore, ""); err != nil {
			return false, fmt.Sprintf("could not read the root .dockerignore (%v)", err)
		}
		for _, entry := range legacySkips {
			if !matcher.Match(entry, true) {
				return false, fmt.Sprintf("the root .dockerignore does not exclude Draft-skipped entry %s", entry)
			}
		}
	}
	return true, ""
}

func fileExists(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}

func topLevelLegacySkipEntries(contextRoot string) ([]string, error) {
	names := []string{".git", "node_modules", ".next", "__pycache__", ".venv"}
	found := make([]string, 0, len(names))
	for _, name := range names {
		path := filepath.Join(contextRoot, name)
		info, err := os.Stat(path)
		if err == nil {
			if info.IsDir() || name == ".git" {
				found = append(found, name)
			}
			continue
		}
		if !os.IsNotExist(err) {
			return nil, err
		}
	}
	return found, nil
}

func draftNetworkName(projectID uint, projectName, environment string) string {
	env := sanitize(environment)
	if env == "" {
		env = "default"
	}
	return fmt.Sprintf("draft-%d-%s-%s", projectID, sanitize(projectName), env)
}

// draftImageTag is the deterministic Docker tag for a Draft-built image.
// Environment is required so duplicated environments with the same service
// labels get distinct tags (sequence is per-node and both start at :1).
//
// Note: distinct tags do not always mean distinct image IDs. When two envs
// build identical layers, Docker stores one image and attaches every tag to
// it — the Docker tab is one row per ID, with all RepoTags listed.
// Format: draft-{project}-{environment}-{service}:{sequence}
// After cutover the N-1 rollback candidate is retagged to the same name with
// a "-previous" suffix (see draftPreviousImageTag / retainImageForRollback).
func draftImageTag(projectName, environment, serviceName string, sequence int) string {
	env := sanitize(environment)
	if env == "" {
		env = "default"
	}
	return fmt.Sprintf("draft-%s-%s-%s:%d", sanitize(projectName), env, sanitize(serviceName), sequence)
}

// draftContainerName is the Docker container name for a deployment.
// Same identity segments as draftImageTag (colon → hyphen for Docker name rules).
// Format: draft-{project}-{environment}-{service}-{sequence}
func draftContainerName(projectName, environment, serviceName string, sequence int) string {
	env := sanitize(environment)
	if env == "" {
		env = "default"
	}
	return fmt.Sprintf("draft-%s-%s-%s-%d", sanitize(projectName), env, sanitize(serviceName), sequence)
}

func ensureDraftNetwork(ctx context.Context, cli *client.Client, name string, projectID uint, projectName, environment string) error {
	if _, err := cli.NetworkInspect(ctx, name, dockernetwork.InspectOptions{}); err == nil {
		return nil
	} else if !errdefs.IsNotFound(err) {
		return err
	}
	_, err := cli.NetworkCreate(ctx, name, dockernetwork.CreateOptions{
		Driver: "bridge",
		Labels: map[string]string{
			"draft.managed":     "true",
			"draft.project":     fmt.Sprintf("%d", projectID),
			"draft.projectName": projectName,
			"draft.environment": environment,
		},
	})
	if err != nil && !errdefs.IsConflict(err) {
		return err
	}
	return nil
}

func removeContainerAndWait(ctx context.Context, cli *client.Client, containerID string) error {
	if containerID == "" {
		return nil
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if err := cli.ContainerRemove(ctx, containerID, container.RemoveOptions{}); err != nil &&
			!errdefs.IsNotFound(err) &&
			!errdefs.IsConflict(err) &&
			!strings.Contains(err.Error(), "already in progress") {
			return err
		}
		if _, err := cli.ContainerInspect(ctx, containerID); err != nil {
			if errdefs.IsNotFound(err) {
				return nil
			}
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("container %s still exists after removal", containerID)
}

func stopTimeoutForSettings(settings map[string]string) int {
	if overrides := parseContainerOverrides(settings); overrides.StopTimeout != nil && *overrides.StopTimeout > 0 {
		return *overrides.StopTimeout
	}
	return 10
}

func deploymentWasIntentionallyStopped(dep *store.Deployment) bool {
	return dep != nil && (dep.Status == "stopped" || dep.Status == "interrupted")
}

func markDeploymentStopped(dep *store.Deployment, now time.Time) {
	dep.Status = "stopped"
	dep.Error = ""
	dep.ContainerStoppedAt = &now
	dep.FinishedAt = &now
	dep.LastSeenAt = &now
	dep.OOMKilled = false
	exitCode := 0
	dep.ExitCode = &exitCode
}

func applyContainerExitResult(dep *store.Deployment, exitCode int, waitErr error) {
	if deploymentWasIntentionallyStopped(dep) {
		dep.Status = "stopped"
		dep.Error = ""
		dep.OOMKilled = false
		if dep.ExitCode == nil {
			cleanExit := 0
			dep.ExitCode = &cleanExit
		}
		return
	}
	if waitErr != nil {
		dep.Status = "failed"
		dep.Error = "container watch error: " + waitErr.Error()
		return
	}
	dep.ExitCode = &exitCode
	if exitCode != 0 {
		dep.Status = "failed"
		dep.Error = fmt.Sprintf("container exited with code %d", exitCode)
		return
	}
	dep.Status = "stopped"
	dep.Error = ""
}

func removeImageAndWait(ctx context.Context, cli *client.Client, imageTag string) error {
	if imageTag == "" {
		return nil
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := cli.ImageRemove(ctx, imageTag, image.RemoveOptions{Force: true}); err != nil &&
			!errdefs.IsNotFound(err) &&
			!errdefs.IsConflict(err) &&
			!strings.Contains(err.Error(), "being used by") {
			return err
		}
		if _, _, err := cli.ImageInspectWithRaw(ctx, imageTag); err != nil {
			if errdefs.IsNotFound(err) {
				return nil
			}
			return err
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("image %s still exists after removal", imageTag)
}

func containerRestarted(ctx context.Context, cli *client.Client, containerID string, timeout time.Duration) bool {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		inspect, err := cli.ContainerInspect(ctx, containerID)
		if err == nil && inspect.State != nil && inspect.State.Running {
			return true
		}
		if err != nil && !errdefs.IsNotFound(err) {
			return false
		}
		time.Sleep(100 * time.Millisecond)
	}
	return false
}

func internalNetworkAliases(serviceName, hostname string) []string {
	aliases := []string{serviceName}
	if hostname != "" && hostname != serviceName {
		aliases = append(aliases, hostname)
	}
	return aliases
}

type uploadTracker struct {
	reader     io.Reader
	totalBytes int64
	sent       int64
	lastPct    int
	onProgress func(sent int64, total int64)
}

type lineEmitterWriter struct {
	mu       sync.Mutex
	buf      strings.Builder
	emitLine func(string)
}

func newLineEmitterWriter(emitLine func(string)) *lineEmitterWriter {
	return &lineEmitterWriter{emitLine: emitLine}
}

func (w *lineEmitterWriter) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()

	for _, b := range p {
		if b == '\r' {
			continue
		}
		if b == '\n' {
			w.flushLocked()
			continue
		}
		w.buf.WriteByte(b)
	}
	return len(p), nil
}

func (w *lineEmitterWriter) Flush() {
	w.mu.Lock()
	defer w.mu.Unlock()
	w.flushLocked()
}

func (w *lineEmitterWriter) flushLocked() {
	if w.buf.Len() == 0 {
		return
	}
	line := w.buf.String()
	w.buf.Reset()
	if w.emitLine != nil {
		w.emitLine(line)
	}
}

func (u *uploadTracker) Read(p []byte) (int, error) {
	n, err := u.reader.Read(p)
	u.sent += int64(n)
	if u.totalBytes > 0 {
		pct := int(float64(u.sent) / float64(u.totalBytes) * 100)
		// Report at every 10% increment
		step := pct / 10
		if step > u.lastPct/10 {
			u.lastPct = pct
			if u.onProgress != nil {
				u.onProgress(u.sent, u.totalBytes)
			}
		}
	}
	return n, err
}

func (e *Engine) streamBuildOutput(ctx context.Context, reader io.Reader, logFile *os.File, nodeID string, deploymentID uint) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	e.emitBuildLog(nodeID, "==> Building image...")

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line := scanner.Text()
		logFile.WriteString(line + "\n")

		var msg struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
			Status string `json:"status"`
			ID     string `json:"id"`
		}
		if json.Unmarshal([]byte(line), &msg) == nil {
			if msg.Error != "" {
				e.emitBuildLog(nodeID, msg.Error)
				return fmt.Errorf("%s", msg.Error)
			}
			if msg.Stream != "" {
				e.emitBuildLog(nodeID, strings.TrimRight(msg.Stream, "\n"))
			}
			if msg.Status != "" {
				text := msg.Status
				if msg.ID != "" {
					text = msg.ID + ": " + text
				}
				e.emitBuildLog(nodeID, text)
			}
		}
	}
	return scanner.Err()
}

// startContainerWatch attaches a background waiter for a running deployment.
// Safe to call after cutover, Restart, or Reconcile — replaces any prior
// watcher for the same node so daemon restarts regain exit detection.
func (e *Engine) startContainerWatch(dep *store.Deployment, nodeID string) {
	if dep == nil || dep.ContainerID == "" || nodeID == "" {
		return
	}
	e.watchMu.Lock()
	if cancel, ok := e.watchers[nodeID]; ok {
		cancel()
	}
	e.watchGen[nodeID]++
	gen := e.watchGen[nodeID]
	ctx, cancel := context.WithCancel(context.Background())
	e.watchers[nodeID] = cancel
	e.watchMu.Unlock()

	go func() {
		defer func() {
			e.watchMu.Lock()
			if e.watchGen[nodeID] == gen {
				delete(e.watchers, nodeID)
			}
			e.watchMu.Unlock()
			cancel()
		}()
		e.watchContainer(ctx, dep, nodeID)
	}()
}

func (e *Engine) stopContainerWatch(nodeID string) {
	e.watchMu.Lock()
	if cancel, ok := e.watchers[nodeID]; ok {
		cancel()
		delete(e.watchers, nodeID)
	}
	e.watchGen[nodeID]++
	e.watchMu.Unlock()
}

func (e *Engine) watchContainer(ctx context.Context, dep *store.Deployment, nodeID string) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		dep.Status = "failed"
		dep.Error = "container watch error: " + err.Error()
		dep.FinishedAt = ptrTime(time.Now())
		e.store.UpdateDeployment(dep)
		e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "failed", Error: dep.Error})
		return
	}
	defer cli.Close()

	statusCh, errCh := cli.ContainerWait(ctx, dep.ContainerID, container.WaitConditionNotRunning)
	var waitErr error
	var exitCode int
	select {
	case result := <-statusCh:
		exitCode = int(result.StatusCode)
		if result.Error != nil && result.Error.Message != "" {
			waitErr = fmt.Errorf("%s", result.Error.Message)
		}
	case err := <-errCh:
		waitErr = err
	case <-ctx.Done():
		return
	}

	if latest, err := e.store.GetDeployment(dep.ID); err == nil && latest != nil {
		dep = latest
	}
	applyContainerExitResult(dep, exitCode, waitErr)

	if dep.ContainerID != "" && containerRestarted(ctx, cli, dep.ContainerID, 10*time.Second) {
		// Docker restart policy brought the container back — keep waiting.
		e.watchContainer(ctx, dep, nodeID)
		return
	}

	e.finalizeContainerExit(ctx, cli, dep, nodeID)
}

// finalizeContainerExit updates deployment status, removes the stopped
// container/image, and clears the public route after an unexpected exit.
func (e *Engine) finalizeContainerExit(ctx context.Context, cli *client.Client, dep *store.Deployment, nodeID string) {
	now := time.Now()
	if dep.ContainerStoppedAt == nil {
		dep.ContainerStoppedAt = &now
	}
	if dep.FinishedAt == nil {
		dep.FinishedAt = &now
	}
	dep.LastSeenAt = &now

	if dep.ContainerID != "" {
		if err := removeContainerAndWait(ctx, cli, dep.ContainerID); err != nil {
			log.Printf("[deploy] remove stopped container %s: %v", dep.ContainerID, err)
		}
	}
	if dep.ImageTag != "" {
		if err := removeDraftDeploymentImage(ctx, cli, dep.ImageTag); err != nil {
			log.Printf("[deploy] remove image %s: %v", dep.ImageTag, err)
		}
	}
	if dep.Hostname != "" {
		_ = e.router.Unregister(dep.Hostname)
	}

	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{
		DeploymentID: dep.ID,
		Status:       dep.Status,
		Error:        dep.Error,
	})
}

// HandleDockerContainerEvent reacts to dockerwatch container lifecycle events
// for Draft-labeled containers. Covers the gap where ContainerWait was not
// attached (e.g. between daemon crash and Reconcile reattach).
func (e *Engine) HandleDockerContainerEvent(action string, attrs map[string]string) {
	if attrs == nil {
		return
	}
	action = strings.ToLower(strings.TrimSpace(action))
	switch action {
	case "die", "oom", "destroy", "kill":
	default:
		return
	}
	idStr := attrs["draft.deployment"]
	if idStr == "" {
		return
	}
	id, err := strconv.ParseUint(idStr, 10, 64)
	if err != nil || id == 0 {
		return
	}
	dep, err := e.store.GetDeployment(uint(id))
	if err != nil || dep == nil {
		return
	}
	if dep.Status != "running" && dep.Status != "starting" {
		return
	}
	if deploymentWasIntentionallyStopped(dep) {
		return
	}

	exitCode := 0
	if ec, ok := attrs["exitCode"]; ok {
		if n, err := strconv.Atoi(ec); err == nil {
			exitCode = n
		}
	}
	applyContainerExitResult(dep, exitCode, nil)

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		now := time.Now()
		dep.FinishedAt = &now
		dep.ContainerStoppedAt = &now
		_ = e.store.UpdateDeployment(dep)
		e.emitStatus(dep.NodeID, StatusEvent{DeploymentID: dep.ID, Status: dep.Status, Error: dep.Error})
		return
	}
	defer cli.Close()

	e.stopContainerWatch(dep.NodeID)
	e.finalizeContainerExit(context.Background(), cli, dep, dep.NodeID)
}

// stopPrevious retires every deployment for the node other than currentID:
// it stops and removes their containers and images. keepHostname is the route
// the current deployment now owns; it is never unregistered here, since the
// current and previous deployments of a node share a stable hostname and the
// route was just repointed to the new container.
func (e *Engine) stopPrevious(ctx context.Context, cli *client.Client, nodeID string, currentID uint, keepHostname string) error {
	deployments, err := e.store.ListDeployments(nodeID)
	if err != nil {
		return err
	}
	settings, err := e.store.GetNodeSettings(nodeID)
	if err != nil {
		settings = nil
	}
	stopTimeout := stopTimeoutForSettings(settings)
	keepImages := keepImagesPolicy(settings)

	// The most-recent previous deployment (highest created_at that isn't the
	// one that just cut over) is the N-1 we keep an image of when the policy is
	// "last", so the user can roll back to what was running a moment ago. All
	// older images (N-2+) are removed every deploy — both the live tag and the
	// -previous retention tag.
	prevKeptID := uint(0)
	if keepImages == keepImagesLast {
		for _, d := range deployments {
			if d.ID != currentID {
				prevKeptID = d.ID
				break
			}
		}
	}

	for _, d := range deployments {
		if d.ID == currentID {
			continue
		}

		if d.Status != "stopped" && d.Status != "failed" && d.Status != "interrupted" {
			now := time.Now()
			markDeploymentStopped(&d, now)
			e.store.UpdateDeployment(&d)
			if d.ContainerID != "" {
				cli.ContainerStop(ctx, d.ContainerID, container.StopOptions{Timeout: &stopTimeout})
				_ = removeContainerAndWait(ctx, cli, d.ContainerID)
			}
			if d.Hostname != "" && d.Hostname != keepHostname {
				e.router.Unregister(d.Hostname)
			}
		} else {
			// Already terminal — clean up leftover container if still present.
			if d.ContainerID != "" {
				_ = removeContainerAndWait(ctx, cli, d.ContainerID)
			}
		}

		// Image retention.
		//   all  — leave every prior image tagged as built (live names).
		//   last — keep only N-1, retagged to …:N-previous for rollback identity.
		//   none — remove every prior image (live + -previous forms).
		// Container cleanup above always runs regardless of policy.
		switch keepImages {
		case keepImagesAll:
			// Leave historical live tags in place.
		case keepImagesLast:
			if d.ID == prevKeptID {
				if d.ImageTag != "" {
					_ = retainImageForRollback(ctx, cli, d.ImageTag)
				}
			} else if d.ImageTag != "" {
				_ = removeDraftDeploymentImage(ctx, cli, d.ImageTag)
			}
		default: // keepImagesNone
			if d.ImageTag != "" {
				_ = removeDraftDeploymentImage(ctx, cli, d.ImageTag)
			}
		}
	}
	return nil
}

func (e *Engine) Stop(ctx context.Context, nodeID string) error {
	if link, _ := e.GetServiceLink(nodeID); link != nil {
		return fmt.Errorf("this service is linked to another environment — stop the root service instead")
	}

	e.mu.Lock()
	if cancel, ok := e.active[nodeID]; ok {
		cancel()
		delete(e.active, nodeID)
	}
	e.mu.Unlock()

	dep, err := e.store.ActiveDeployment(nodeID)
	if err != nil {
		return err
	}
	if dep == nil {
		return nil
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()
	settings, err := e.store.GetNodeSettings(nodeID)
	if err != nil {
		settings = nil
	}
	stopTimeout := stopTimeoutForSettings(settings)

	now := time.Now()
	markDeploymentStopped(dep, now)
	e.store.UpdateDeployment(dep)

	e.stopContainerWatch(nodeID)

	if dep.ContainerID != "" {
		cli.ContainerStop(ctx, dep.ContainerID, container.StopOptions{Timeout: &stopTimeout})
		if err := removeContainerAndWait(ctx, cli, dep.ContainerID); err != nil {
			return err
		}
	}

	if dep.ImageTag != "" {
		if err := removeDraftDeploymentImage(ctx, cli, dep.ImageTag); err != nil {
			return err
		}
	}

	if dep.Hostname != "" {
		e.router.Unregister(dep.Hostname)
	}

	e.store.UpdateDeployment(dep)

	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "stopped"})
	return nil
}

func (e *Engine) Restart(ctx context.Context, nodeID string) error {
	if link, _ := e.GetServiceLink(nodeID); link != nil {
		return fmt.Errorf("this service is linked to another environment — restart the root service instead")
	}

	dep, err := e.store.ActiveDeployment(nodeID)
	if err != nil {
		return err
	}
	if dep == nil || dep.ContainerID == "" {
		return fmt.Errorf("no running container to restart")
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	settings, err := e.store.GetNodeSettings(nodeID)
	if err != nil {
		settings = nil
	}
	stopTimeout := stopTimeoutForSettings(settings)
	if err := cli.ContainerRestart(ctx, dep.ContainerID, container.StopOptions{Timeout: &stopTimeout}); err != nil {
		return err
	}

	dep.Status = "running"
	dep.ContainerStartedAt = ptrTime(time.Now())
	dep.ContainerStoppedAt = nil
	dep.FinishedAt = nil
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "running"})
	e.startContainerWatch(dep, nodeID)
	return nil
}

func (e *Engine) StartLogStream(ctx context.Context, nodeID string) error {
	e.logsMu.Lock()
	if cancel, ok := e.logSubs[nodeID]; ok {
		cancel()
	}
	logCtx, cancel := context.WithCancel(ctx)
	e.logSubs[nodeID] = cancel
	e.logsMu.Unlock()

	dep, err := e.store.ActiveDeployment(nodeID)
	if err != nil || dep == nil || dep.ContainerID == "" {
		cancel()
		return fmt.Errorf("no active container for log streaming")
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		cancel()
		return err
	}

	reader, err := cli.ContainerLogs(logCtx, dep.ContainerID, container.LogsOptions{
		ShowStdout: true,
		ShowStderr: true,
		Follow:     true,
		Tail:       "200",
	})
	if err != nil {
		cli.Close()
		cancel()
		return err
	}

	go func() {
		defer func() {
			reader.Close()
			cli.Close()
			e.logsMu.Lock()
			delete(e.logSubs, nodeID)
			e.logsMu.Unlock()
			cancel()
		}()

		stdoutPr, stdoutPw := io.Pipe()
		stderrPr, stderrPw := io.Pipe()

		go func() {
			stdcopy.StdCopy(stdoutPw, stderrPw, reader)
			stdoutPw.Close()
			stderrPw.Close()
		}()

		go e.scanLogStream(logCtx, stderrPr, nodeID, "stderr")
		e.scanLogStream(logCtx, stdoutPr, nodeID, "stdout")
	}()
	return nil
}

func (e *Engine) scanLogStream(ctx context.Context, r io.Reader, nodeID, stream string) {
	scanner := bufio.NewScanner(r)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		if ctx.Err() != nil {
			return
		}
		e.emit("container:log:"+nodeID, LogLine{Line: scanner.Text(), Stream: stream})
	}
}

func (e *Engine) StopLogStream(nodeID string) {
	e.logsMu.Lock()
	if cancel, ok := e.logSubs[nodeID]; ok {
		cancel()
		delete(e.logSubs, nodeID)
	}
	e.logsMu.Unlock()
}

func (e *Engine) Reconcile(ctx context.Context) error {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()

	args := filters.NewArgs()
	args.Add("label", "draft.deployment")
	containers, err := cli.ContainerList(ctx, container.ListOptions{
		All:     true,
		Filters: args,
	})
	if err != nil {
		return err
	}

	seen := make(map[uint]struct{}, len(containers))
	for _, c := range containers {
		idStr := c.Labels["draft.deployment"]
		id, err := strconv.ParseUint(idStr, 10, 64)
		if err != nil || id == 0 {
			continue
		}
		dep, err := e.store.GetDeployment(uint(id))
		if err != nil {
			continue
		}
		seen[dep.ID] = struct{}{}

		inspect, err := cli.ContainerInspect(ctx, c.ID)
		if err != nil {
			continue
		}

		dep.ContainerID = c.ID
		dep.LastSeenAt = ptrTime(time.Now())
		if inspect.State != nil && inspect.State.Running {
			dep.Status = "running"
			dep.Error = ""
			dep.FinishedAt = nil
			dep.ContainerStoppedAt = nil
			if dep.ContainerStartedAt == nil {
				if startedAt, err := time.Parse(time.RFC3339Nano, inspect.State.StartedAt); err == nil {
					dep.ContainerStartedAt = &startedAt
				}
			}
			dep.OOMKilled = inspect.State.OOMKilled
			if dep.ExitCode == nil || inspect.State.ExitCode != 0 {
				exitCode := inspect.State.ExitCode
				dep.ExitCode = &exitCode
			}
			if dep.HostPort == 0 {
				dep.HostPort = firstHostPort(inspect.NetworkSettings.Ports)
			}
			if dep.Hostname != "" && dep.HostPort > 0 {
				e.restoreRoute(dep)
			}
			e.emitStatus(dep.NodeID, StatusEvent{
				DeploymentID: dep.ID,
				Status:       "running",
				Hostname:     dep.Hostname,
				HostPort:     dep.HostPort,
			})
			e.startContainerWatch(dep, dep.NodeID)
		} else if deploymentWasIntentionallyStopped(dep) {
			if dep.FinishedAt == nil {
				dep.FinishedAt = ptrTime(time.Now())
			}
			if dep.ContainerStoppedAt == nil {
				dep.ContainerStoppedAt = dep.FinishedAt
			}
			dep.Status = "stopped"
			dep.Error = ""
			dep.OOMKilled = false
			if dep.ExitCode == nil {
				exitCode := 0
				dep.ExitCode = &exitCode
			}
			e.emitStatus(dep.NodeID, StatusEvent{DeploymentID: dep.ID, Status: "stopped"})
			cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{})
			if dep.ImageTag != "" {
				_ = removeDraftDeploymentImage(ctx, cli, dep.ImageTag)
			}
		} else if inspect.State != nil && inspect.State.ExitCode != 0 {
			dep.Status = "failed"
			dep.Error = fmt.Sprintf("container exited with code %d", inspect.State.ExitCode)
			dep.FinishedAt = ptrTime(time.Now())
			dep.ContainerStoppedAt = dep.FinishedAt
			exitCode := inspect.State.ExitCode
			dep.ExitCode = &exitCode
			dep.OOMKilled = inspect.State.OOMKilled
			e.emitStatus(dep.NodeID, StatusEvent{DeploymentID: dep.ID, Status: "failed", Error: dep.Error})
			cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{})
			if dep.ImageTag != "" {
				_ = removeDraftDeploymentImage(ctx, cli, dep.ImageTag)
			}
		} else {
			dep.Status = "stopped"
			dep.FinishedAt = ptrTime(time.Now())
			dep.ContainerStoppedAt = dep.FinishedAt
			if inspect.State != nil {
				exitCode := inspect.State.ExitCode
				dep.ExitCode = &exitCode
				dep.OOMKilled = inspect.State.OOMKilled
			}
			e.emitStatus(dep.NodeID, StatusEvent{DeploymentID: dep.ID, Status: "stopped"})
			cli.ContainerRemove(ctx, c.ID, container.RemoveOptions{})
			if dep.ImageTag != "" {
				_ = removeDraftDeploymentImage(ctx, cli, dep.ImageTag)
			}
		}
		e.store.UpdateDeployment(dep)
	}

	deployments, err := e.store.ListAllDeployments()
	if err != nil {
		return err
	}
	for i := range deployments {
		dep := &deployments[i]
		if _, ok := seen[dep.ID]; ok {
			continue
		}
		switch dep.Status {
		case "building", "built", "starting":
			dep.Status = "interrupted"
			dep.Error = "deployment was interrupted by an app restart"
			dep.FinishedAt = ptrTime(time.Now())
			e.store.UpdateDeployment(dep)
			e.emitStatus(dep.NodeID, StatusEvent{DeploymentID: dep.ID, Status: "interrupted", Error: dep.Error})
			if dep.ImageTag != "" {
				_ = removeDraftDeploymentImage(ctx, cli, dep.ImageTag)
			}
		case "failed":
			if strings.Contains(dep.Error, "interrupted") {
				dep.Status = "interrupted"
				e.store.UpdateDeployment(dep)
			}
			if dep.ImageTag != "" {
				_ = removeDraftDeploymentImage(ctx, cli, dep.ImageTag)
			}
		}
	}
	return nil
}

func (e *Engine) GetBuildLog(deploymentID uint) (string, error) {
	data, err := os.ReadFile(e.logPath(deploymentID))
	if err != nil {
		if os.IsNotExist(err) {
			return "", nil
		}
		return "", err
	}
	return string(data), nil
}

func (e *Engine) GetDeployments(nodeID string) ([]store.Deployment, error) {
	return e.store.ListDeployments(nodeID)
}

func (e *Engine) GetActiveDeployment(nodeID string) (*store.Deployment, error) {
	return e.store.ActiveDeployment(nodeID)
}

func (e *Engine) logPath(deploymentID uint) string {
	return filepath.Join(e.logDir, fmt.Sprintf("%d.log", deploymentID))
}

func (e *Engine) emitStatus(nodeID string, ev StatusEvent) {
	e.emit("deploy:status:"+nodeID, ev)
	e.emit("deploy:status", map[string]any{"nodeId": nodeID, "event": ev})
}

var ansiRe = regexp.MustCompile(`\x1b\[[0-9;]*[a-zA-Z]`)

func (e *Engine) emitBuildLog(nodeID string, line string) {
	line = ansiRe.ReplaceAllString(line, "")
	ll := LogLine{Line: line, Stream: "build"}
	e.emit("build:log:"+nodeID, ll)
	e.emit("build:log", map[string]any{"nodeId": nodeID, "line": ll})
}

func (e *Engine) failDeployment(dep *store.Deployment, nodeID, errMsg string) {
	dep.Status = "failed"
	dep.Error = errMsg
	now := time.Now()
	if dep.BuildFinishedAt == nil {
		dep.BuildFinishedAt = &now
	}
	if dep.ContainerStartedAt != nil {
		dep.ContainerStoppedAt = &now
	}
	dep.FinishedAt = &now
	dep.LastSeenAt = &now
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "failed", Error: errMsg})
}

func ptrTime(t time.Time) *time.Time {
	return &t
}

const (
	// readinessTimeout bounds how long we hold both the old and new containers
	// while waiting for the new one to serve. A crash is detected immediately
	// (container exit), so this mainly caps slow-booting apps.
	readinessTimeout = 90 * time.Second
	// readinessInterval is how often readiness is polled.
	readinessInterval = 500 * time.Millisecond
	// readinessSettle is how long a container must remain in a "likely ready"
	// state before we trust it: either running with no published port and no
	// healthcheck, or with a port that is open but not answering HTTP (e.g. a
	// database). It rules out an immediate crash without waiting on HTTP.
	readinessSettle = 3 * time.Second
	// tcpDialTimeout caps each TCP reachability dial so a closed port is
	// detected quickly and a polling cycle stays cheap.
	tcpDialTimeout = 300 * time.Millisecond
	// httpProbeTimeout caps the best-effort HTTP confirmation probe. It is
	// deliberately short: any HTTP response means ready, and a non-HTTP service
	// should not burn a full second per cycle.
	httpProbeTimeout = 600 * time.Millisecond
)

// waitForReady blocks until the freshly-started container is ready to receive
// traffic, or returns an error if it exits or never becomes ready within
// readinessTimeout. Because the route is not repointed until this returns, the
// wait is invisible to traffic still hitting the previous container — and the
// probe deliberately targets the new container's own published loopback port
// (127.0.0.1:hostPort), never the shared internal hostname, which still resolves
// to the old container until the post-wait cutover. That also means readiness
// never depends on public DNS or the reverse proxy.
//
// Readiness is driven entirely by observable signals — no name-based service
// classification. In priority order:
//   - A Docker HEALTHCHECK, when defined, is authoritative: we wait for
//     "healthy" and fail on "unhealthy" (or on timeout).
//   - A service with a published port is gated on a TCP dial to that port.
//     Once the port accepts a connection we try a short HTTP probe as a
//     confirmation bonus: any HTTP response means ready now. If the service
//     doesn't speak HTTP (a database, cache, or worker that binds a port), the
//     probe never answers, so we fall back to "port has been open past the
//     settle window" — bounded by readinessSettle, not the full timeout.
//   - A service with no healthcheck and no published port is ready once it has
//     stayed running past the settle window without exiting, ruling out an
//     immediate crash.
func (e *Engine) waitForReady(ctx context.Context, cli *client.Client, containerID string, hostPort int) error {
	deadline := time.Now().Add(readinessTimeout)
	hasPort := hostPort > 0
	var runningSince time.Time
	var portOpenSince time.Time

	for {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		inspect, err := cli.ContainerInspect(ctx, containerID)
		if err == nil && inspect.State != nil {
			st := inspect.State
			switch {
			case st.Running:
				if runningSince.IsZero() {
					runningSince = time.Now()
				}
			case st.Status == "created":
				// Not started yet; keep waiting.
			default:
				// Exited or dead before becoming ready — a crash.
				return fmt.Errorf("container exited (%s, code %d)", st.Status, st.ExitCode)
			}

			if st.Running {
				switch {
				case st.Health != nil:
					// A Docker healthcheck is authoritative when present.
					switch st.Health.Status {
					case "healthy":
						return nil
					case "unhealthy":
						return fmt.Errorf("container reported unhealthy")
					}
					// "starting" → keep polling until healthy or timeout.
				case hasPort:
					// Port-based readiness: a single probe does a fast TCP dial
					// and, only if that succeeds, a short HTTP confirmation. This
					// works uniformly for HTTP and non-HTTP services without any
					// name-based classification.
					switch r := probeReachability(hostPort, ""); r.Status {
					case "healthy", "degraded":
						return nil
					case "reachable":
						// Port is open but not answering HTTP. Don't block on HTTP
						// forever — once it has been open past the settle window,
						// trust it (e.g. a database or cache).
						if portOpenSince.IsZero() {
							portOpenSince = time.Now()
						}
						if time.Since(portOpenSince) >= readinessSettle {
							return nil
						}
					default:
						// "unreachable": port not open yet; keep polling.
					}
				default:
					// No healthcheck and no published port: ready once it has
					// stayed up long enough not to be an immediate crash.
					if time.Since(runningSince) >= readinessSettle {
						return nil
					}
				}
			}
		}

		if time.Now().After(deadline) {
			if hasPort {
				return fmt.Errorf("service did not become ready on 127.0.0.1:%d within %s", hostPort, readinessTimeout)
			}
			return fmt.Errorf("timed out after %s", readinessTimeout)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(readinessInterval):
		}
	}
}

func firstHostPort(ports nat.PortMap) int {
	for _, bindings := range ports {
		if len(bindings) == 0 {
			continue
		}
		var hostPort int
		fmt.Sscanf(bindings[0].HostPort, "%d", &hostPort)
		return hostPort
	}
	return 0
}

func (e *Engine) restoreRoute(dep *store.Deployment) {
	if e.router == nil || dep.Hostname == "" || dep.HostPort == 0 {
		return
	}
	// TCP services are not HTTP-proxied; restoring them as HTTP would put a
	// wire protocol (Postgres, Redis, …) behind the reverse proxy.
	if settings, err := e.store.GetNodeSettings(dep.NodeID); err == nil {
		if networking.IsTCPProtocol(settings["route_protocol"]) {
			return
		}
	}
	if route, err := e.store.GetRoute(dep.Hostname); err == nil && route != nil {
		if networking.IsTCPProtocol(route.Protocol) {
			return
		}
	}
	_ = e.router.RestoreHTTPRoute(dep.Hostname, dep.ProjectID, dep.NodeID, "127.0.0.1", dep.HostPort)
}

func sanitize(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, s)
	return strings.Trim(s, "-")
}

func tarDirectoryWithProgress(dir string, matcher *ignore.Matcher, progress func(files int, bytes int64)) (io.ReadCloser, int, int64, error) {
	skipEntry := func(rel string, isDir bool) bool {
		if shouldSkip(rel) {
			return true
		}
		if matcher != nil && matcher.Match(rel, isDir) {
			return true
		}
		return false
	}

	var fileCount int
	var totalBytes int64

	// First pass: stat-only walk to count files and bytes.
	err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(dir, path)
		if err != nil {
			return err
		}
		if rel == "." {
			return nil
		}
		rel = filepath.ToSlash(rel)
		if skipEntry(rel, info.IsDir()) {
			if info.IsDir() {
				return filepath.SkipDir
			}
			return nil
		}
		if !info.IsDir() {
			fileCount++
			totalBytes += info.Size()
		}
		return nil
	})
	if err != nil {
		return nil, 0, 0, err
	}

	if progress != nil {
		progress(fileCount, totalBytes)
	}

	// Second pass: build the tar archive.
	pr, pw := io.Pipe()
	go func() {
		gw := gzip.NewWriter(pw)
		tw := tar.NewWriter(gw)
		err := filepath.Walk(dir, func(path string, info os.FileInfo, err error) error {
			if err != nil {
				return err
			}
			rel, err := filepath.Rel(dir, path)
			if err != nil {
				return err
			}
			if rel == "." {
				return nil
			}
			rel = filepath.ToSlash(rel)

			if skipEntry(rel, info.IsDir()) {
				if info.IsDir() {
					return filepath.SkipDir
				}
				return nil
			}

			header, err := tar.FileInfoHeader(info, "")
			if err != nil {
				return err
			}
			header.Name = rel

			if err := tw.WriteHeader(header); err != nil {
				return err
			}
			if info.IsDir() {
				return nil
			}
			f, err := os.Open(path)
			if err != nil {
				return err
			}
			defer f.Close()
			_, err = io.Copy(tw, f)
			return err
		})
		tw.Close()
		gw.Close()
		pw.CloseWithError(err)
	}()
	return pr, fileCount, totalBytes, nil
}

func shouldSkip(rel string) bool {
	parts := strings.SplitN(rel, "/", 2)
	first := parts[0]
	switch first {
	case ".git", "node_modules", ".next", "__pycache__", ".venv":
		return true
	}
	return false
}
