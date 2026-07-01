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
	store       *store.Store
	router      *networking.Router
	emit        func(event string, data any)
	logDir      string
	execCommand func(context.Context, string, ...string) *exec.Cmd
	mu          sync.Mutex
	active      map[string]context.CancelFunc // nodeID → cancel build
	logsMu      sync.Mutex
	logSubs     map[string]context.CancelFunc // nodeID → cancel log stream
	statsMu     sync.Mutex
	stats       map[string][]MetricPoint
}

func New(s *store.Store, router *networking.Router, logDir string, emit func(string, any)) *Engine {
	os.MkdirAll(logDir, 0o755)
	return &Engine{
		store:       s,
		router:      router,
		emit:        emit,
		logDir:      logDir,
		execCommand: exec.CommandContext,
		active:      make(map[string]context.CancelFunc),
		logSubs:     make(map[string]context.CancelFunc),
		stats:       make(map[string][]MetricPoint),
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
		e.runDeploy(buildCtx, nodeID)
	}()
	return nil
}

func (e *Engine) runDeploy(ctx context.Context, nodeID string) {
	e.emitBuildLog(nodeID, "==> Initializing deployment...")
	e.emitBuildLog(nodeID, "    Loading node settings")

	settings, err := e.store.GetNodeSettings(nodeID)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "failed to read settings: " + err.Error()})
		return
	}

	dockerfilePath := settings["dockerfile"]
	portStr := settings["service_port"]
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
	sourcePath := project.Path
	gitBranch := strings.TrimSpace(settings["git_branch"])
	gitStream := gitBranch != "" && gitStreamEnabled(settings)

	if gitBranch != "" && !gitStream {
		archiveDir, err := e.prepareGitSource(ctx, nodeID, project.Path, gitBranch)
		if err != nil {
			e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: err.Error()})
			return
		}
		defer os.RemoveAll(archiveDir)
		sourcePath = archiveDir

		// The Dockerfile setting may be stored as an absolute path pointing into
		// the on-disk project (e.g. picked via the file dialog). Re-anchor it to
		// the archived workspace so it resolves against the branch's files rather
		// than the original working tree.
		rebased, err := rebaseUnderSource(project.Path, sourcePath, dockerfilePath)
		if err != nil {
			e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: err.Error()})
			return
		}
		dockerfilePath = rebased
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
		e.emitBuildLog(nodeID, "    Dockerfile is outside the service root; using the project root as build context")
	}

	dep := &store.Deployment{
		NodeID:         nodeID,
		ProjectID:      node.ProjectID,
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
	environment := addr.Environment
	hostname := addr.InternalHostname
	uid, err := e.store.EnsureNodeUID(nodeID)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "failed to resolve node identity: " + err.Error()})
		return
	}
	deployEnv, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:           nodeID,
		ProjectID:        node.ProjectID,
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

	imageTag := fmt.Sprintf("draft-%s-%s:%d", projectName, serviceName, dep.ID)
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
		buildErr = e.buildImageGitStream(ctx, cli, logFile, nodeID, imageTag, project.Path, gitBranch, plan, deployEnv, buildOvr)
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

	e.emitBuildLog(nodeID, "==> Stopping previous deployment...")
	if err := e.stopPrevious(ctx, cli, nodeID, dep.ID); err != nil {
		log.Printf("[deploy] warning: stop previous: %v", err)
		e.emitBuildLog(nodeID, fmt.Sprintf("    Warning: %v", err))
	} else {
		e.emitBuildLog(nodeID, "    Done")
	}

	if hooks.PreDeploy != "" {
		if err := runLifecycleHook(ctx, "pre-deploy", hooks.PreDeploy, plan.ServiceRoot, func(line string) {
			e.emitBuildLog(nodeID, line)
		}); err != nil {
			e.failDeployment(dep, nodeID, err.Error())
			return
		}
	}

	e.emitBuildLog(nodeID, "==> Creating container...")
	dep.Status = "starting"
	dep.LastSeenAt = ptrTime(time.Now())
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "starting"})

	containerPort := nat.Port(portStr + "/tcp")
	containerName := fmt.Sprintf("draft-%s-%s-%d", projectName, serviceName, dep.ID)
	networkName := draftNetworkName(node.ProjectID, projectName, environment)
	if err := ensureDraftNetwork(ctx, cli, networkName, node.ProjectID, projectName, environment); err != nil {
		e.failDeployment(dep, nodeID, "docker network setup failed: "+err.Error())
		return
	}
	e.emitBuildLog(nodeID, fmt.Sprintf("    Network: %s", networkName))

	labels := map[string]string{
		"draft.project":    fmt.Sprintf("%d", node.ProjectID),
		"draft.node":       nodeID,
		"draft.deployment": fmt.Sprintf("%d", dep.ID),
	}
	for k, v := range overrides.Labels {
		labels[k] = v
	}

	containerCfg := &container.Config{
		Image:        imageTag,
		Env:          deployEnv.RuntimeEnv,
		Labels:       labels,
		ExposedPorts: nat.PortSet{containerPort: struct{}{}},
	}
	if len(overrides.Cmd) > 0 {
		containerCfg.Cmd = overrides.Cmd
	}
	if len(overrides.Entrypoint) > 0 {
		containerCfg.Entrypoint = overrides.Entrypoint
	}
	if overrides.WorkingDir != "" {
		containerCfg.WorkingDir = overrides.WorkingDir
	}
	if overrides.User != "" {
		containerCfg.User = overrides.User
	}
	if overrides.StopSignal != "" {
		containerCfg.StopSignal = overrides.StopSignal
	}
	if overrides.Healthcheck != nil {
		containerCfg.Healthcheck = overrides.Healthcheck
	}
	if overrides.StopTimeout != nil {
		containerCfg.StopTimeout = overrides.StopTimeout
	}

	hostCfg := &container.HostConfig{
		PortBindings: nat.PortMap{
			containerPort: []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "0"}},
		},
		RestartPolicy:  overrides.RestartPolicy,
		Resources:      overrides.Resources,
		Mounts:         overrides.Mounts,
		Privileged:     overrides.Privileged,
		ReadonlyRootfs: overrides.ReadonlyRootfs,
		CapAdd:         overrides.CapAdd,
		CapDrop:        overrides.CapDrop,
		Init:           overrides.Init,
	}

	createResp, err := cli.ContainerCreate(ctx, containerCfg, hostCfg, &dockernetwork.NetworkingConfig{
		EndpointsConfig: map[string]*dockernetwork.EndpointSettings{
			networkName: {
				Aliases: internalNetworkAliases(serviceName, hostname),
			},
		},
	}, nil, containerName)
	if err != nil {
		e.failDeployment(dep, nodeID, "container create failed: "+err.Error())
		return
	}

	dep.ContainerID = createResp.ID
	dep.LastSeenAt = ptrTime(time.Now())
	e.store.UpdateDeployment(dep)
	e.emitBuildLog(nodeID, fmt.Sprintf("    Container: %s (%s)", containerName, createResp.ID[:12]))

	e.emitBuildLog(nodeID, "==> Starting container...")
	if err := cli.ContainerStart(ctx, createResp.ID, container.StartOptions{}); err != nil {
		e.failDeployment(dep, nodeID, "container start failed: "+err.Error())
		return
	}

	inspect, err := cli.ContainerInspect(ctx, createResp.ID)
	if err != nil {
		e.failDeployment(dep, nodeID, "container inspect failed: "+err.Error())
		return
	}

	var hostPort int
	bindings := inspect.NetworkSettings.Ports[containerPort]
	if len(bindings) > 0 {
		fmt.Sscanf(bindings[0].HostPort, "%d", &hostPort)
	}
	e.emitBuildLog(nodeID, fmt.Sprintf("    Listening on 127.0.0.1:%d (container port %s)", hostPort, portStr))

	e.emitBuildLog(nodeID, "==> Registering route...")
	regResult, err := e.router.Register(networking.RegisterRequest{
		Service:     serviceName,
		Project:     projectName,
		ProjectID:   node.ProjectID,
		NodeID:      nodeID,
		UID:         uid,
		Environment: environment,
		Protocol:    "http",
		TargetHost:  "127.0.0.1",
		TargetPort:  hostPort,
	})
	if err != nil {
		log.Printf("[deploy] route registration failed (non-fatal): %v", err)
		e.emitBuildLog(nodeID, fmt.Sprintf("    Warning: %v", err))
	} else if regResult != nil {
		e.emitBuildLog(nodeID, fmt.Sprintf("    Route: %s → 127.0.0.1:%d", regResult.Hostname, hostPort))
	}

	dep.Status = "running"
	dep.HostPort = hostPort
	dep.ContainerStartedAt = ptrTime(time.Now())
	dep.ContainerStoppedAt = nil
	dep.FinishedAt = nil
	dep.LastSeenAt = ptrTime(time.Now())
	if regResult != nil {
		dep.Hostname = regResult.Hostname
	}
	e.store.UpdateDeployment(dep)

	if hooks.PostDeploy != "" {
		if err := runLifecycleHook(ctx, "post-deploy", hooks.PostDeploy, plan.ServiceRoot, func(line string) {
			e.emitBuildLog(nodeID, line)
		}); err != nil {
			log.Printf("[deploy] post-deploy hook failed (non-fatal): %v", err)
		}
	}

	e.emitBuildLog(nodeID, "==> Deployed successfully!")

	e.emitStatus(nodeID, StatusEvent{
		DeploymentID: dep.ID,
		Status:       "running",
		Hostname:     dep.Hostname,
		HostPort:     hostPort,
	})

	go e.watchContainer(context.Background(), dep, nodeID)
}

// prepareGitSource materializes the given branch/ref of the project's git
// repository into a fresh temporary directory and returns its path. The caller
// owns the returned directory and must remove it when done. The project's
// working tree and index are never read or modified.
func (e *Engine) prepareGitSource(ctx context.Context, nodeID, projectPath, ref string) (string, error) {
	if !gitsrc.IsRepo(projectPath) {
		return "", fmt.Errorf("git branch is set but %s is not a git repository", projectPath)
	}

	e.emitBuildLog(nodeID, fmt.Sprintf("==> Preparing source from git branch %q...", ref))

	archiveDir, err := os.MkdirTemp("", "draft-src-")
	if err != nil {
		return "", fmt.Errorf("create source workspace: %w", err)
	}

	if err := gitsrc.ArchiveToDir(ctx, projectPath, ref, archiveDir); err != nil {
		os.RemoveAll(archiveDir)
		return "", err
	}

	e.emitBuildLog(nodeID, fmt.Sprintf("    Exported %q into an ephemeral workspace (working tree untouched)", ref))
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
		serviceRoot = filepath.Join(projectPath, rel)
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
func gitArchiveTreeish(projectPath, contextRoot, ref string) (string, error) {
	contextRel, err := filepath.Rel(projectPath, contextRoot)
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
func (e *Engine) buildImageGitStream(ctx context.Context, cli *client.Client, logFile *os.File, nodeID, imageTag, projectPath, ref string, plan buildContextPlan, deployEnv deploymentEnv, bo buildOverrides) error {
	if !gitsrc.IsRepo(projectPath) {
		return fmt.Errorf("git branch is set but %s is not a git repository", projectPath)
	}
	if err := gitsrc.VerifyRef(ctx, projectPath, ref); err != nil {
		return err
	}

	// Archive only the build-context subtree. `<ref>:<subdir>` roots the archive
	// at that subdir, so entries line up with plan.RelativeDockerfile.
	treeish, err := gitArchiveTreeish(projectPath, plan.ContextRoot, ref)
	if err != nil {
		return err
	}

	e.emitBuildLog(nodeID, "==> Streaming git branch to Docker...")
	e.emitBuildLog(nodeID, fmt.Sprintf("    git archive --format=tar %s", treeish))
	e.emitBuildLog(nodeID, fmt.Sprintf("    docker build -t %s -f %s", imageTag, plan.RelativeDockerfile))
	e.emitBuildLog(nodeID, "    Note: .dockerignore, .gitignore and BuildKit local context do not apply while streaming")

	// A dedicated cancellable context lets us tear down git archive if Docker
	// rejects the build before draining the tar, avoiding a blocked-writer hang.
	gitCtx, cancelGit := context.WithCancel(ctx)
	defer cancelGit()

	cmd := e.execCommand(gitCtx, "git", "-C", projectPath, "archive", "--format=tar", treeish)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return fmt.Errorf("git archive: %w", err)
	}
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Start(); err != nil {
		return fmt.Errorf("git archive: %w", err)
	}

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
		r: stdout,
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
		_ = cmd.Wait()
		clearProgress(atomic.LoadInt64(&reader.n))
		return fmt.Errorf("docker build failed: %w", err)
	}
	defer resp.Body.Close()

	buildStart := time.Now()
	buildErr := e.streamBuildOutput(ctx, resp.Body, logFile, nodeID, 0)

	// The build response is fully read above, so the entire tar has been sent
	// and git has finished writing; Wait reaps it and surfaces archive errors.
	waitErr := cmd.Wait()

	// Belt-and-suspenders: ensure the bar is cleared even if EOF wasn't observed.
	clearProgress(atomic.LoadInt64(&reader.n))

	if buildErr != nil {
		return buildErr
	}
	if waitErr != nil && ctx.Err() == nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = waitErr.Error()
		}
		return fmt.Errorf("git archive %s: %s", ref, msg)
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
	select {
	case result := <-statusCh:
		exitCode := int(result.StatusCode)
		dep.ExitCode = &exitCode
		if result.StatusCode != 0 {
			errMsg := fmt.Sprintf("container exited with code %d", result.StatusCode)
			if result.Error != nil && result.Error.Message != "" {
				errMsg = result.Error.Message
			}
			dep.Status = "failed"
			dep.Error = errMsg
		} else {
			dep.Status = "stopped"
		}
	case err := <-errCh:
		dep.Status = "failed"
		dep.Error = "container watch error: " + err.Error()
	case <-ctx.Done():
		return
	}

	now := time.Now()
	dep.ContainerStoppedAt = &now
	dep.FinishedAt = &now
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{
		DeploymentID: dep.ID,
		Status:       dep.Status,
		Error:        dep.Error,
	})

	// Clean up the stopped container and its image to reclaim disk space.
	if dep.ContainerID != "" {
		cli.ContainerRemove(ctx, dep.ContainerID, container.RemoveOptions{})
	}
	if dep.ImageTag != "" {
		cli.ImageRemove(ctx, dep.ImageTag, image.RemoveOptions{})
	}
}

func (e *Engine) stopPrevious(ctx context.Context, cli *client.Client, nodeID string, currentID uint) error {
	deployments, err := e.store.ListDeployments(nodeID)
	if err != nil {
		return err
	}

	for _, d := range deployments {
		if d.ID == currentID {
			continue
		}

		if d.Status != "stopped" && d.Status != "failed" && d.Status != "interrupted" {
			if d.ContainerID != "" {
				timeout := 10
				cli.ContainerStop(ctx, d.ContainerID, container.StopOptions{Timeout: &timeout})
				cli.ContainerRemove(ctx, d.ContainerID, container.RemoveOptions{})
			}
			if d.Hostname != "" {
				e.router.Unregister(d.Hostname)
			}
			d.Status = "stopped"
			now := time.Now()
			d.FinishedAt = &now
			e.store.UpdateDeployment(&d)
		} else {
			// Already terminal — clean up leftover container if still present.
			if d.ContainerID != "" {
				cli.ContainerRemove(ctx, d.ContainerID, container.RemoveOptions{})
			}
		}

		// Always remove old images from previous deployments.
		if d.ImageTag != "" {
			cli.ImageRemove(ctx, d.ImageTag, image.RemoveOptions{})
		}
	}
	return nil
}

func (e *Engine) Stop(ctx context.Context, nodeID string) error {
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

	if dep.ContainerID != "" {
		timeout := 10
		cli.ContainerStop(ctx, dep.ContainerID, container.StopOptions{Timeout: &timeout})
		cli.ContainerRemove(ctx, dep.ContainerID, container.RemoveOptions{})
	}

	if dep.ImageTag != "" {
		cli.ImageRemove(ctx, dep.ImageTag, image.RemoveOptions{})
	}

	if dep.Hostname != "" {
		e.router.Unregister(dep.Hostname)
	}

	dep.Status = "stopped"
	now := time.Now()
	dep.ContainerStoppedAt = &now
	dep.FinishedAt = &now
	exitCode := 0
	dep.ExitCode = &exitCode
	e.store.UpdateDeployment(dep)

	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "stopped"})
	return nil
}

func (e *Engine) Restart(ctx context.Context, nodeID string) error {
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

	timeout := 10
	if err := cli.ContainerRestart(ctx, dep.ContainerID, container.StopOptions{Timeout: &timeout}); err != nil {
		return err
	}

	dep.Status = "running"
	dep.ContainerStartedAt = ptrTime(time.Now())
	dep.ContainerStoppedAt = nil
	dep.FinishedAt = nil
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "running"})
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
				cli.ImageRemove(ctx, dep.ImageTag, image.RemoveOptions{})
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
				cli.ImageRemove(ctx, dep.ImageTag, image.RemoveOptions{})
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
				cli.ImageRemove(ctx, dep.ImageTag, image.RemoveOptions{})
			}
		case "failed":
			if strings.Contains(dep.Error, "interrupted") {
				dep.Status = "interrupted"
				e.store.UpdateDeployment(dep)
			}
			if dep.ImageTag != "" {
				cli.ImageRemove(ctx, dep.ImageTag, image.RemoveOptions{})
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
