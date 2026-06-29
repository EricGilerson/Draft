package deploy

import (
	"archive/tar"
	"bufio"
	"compress/gzip"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"Draft/internal/ignore"
	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
	"github.com/docker/go-connections/nat"
)

type Engine struct {
	store   *store.Store
	router  *networking.Router
	emit    func(event string, data any)
	logDir  string
	mu      sync.Mutex
	active  map[string]context.CancelFunc // nodeID → cancel build
	logsMu  sync.Mutex
	logSubs map[string]context.CancelFunc // nodeID → cancel log stream
}

func New(s *store.Store, router *networking.Router, logDir string, emit func(string, any)) *Engine {
	os.MkdirAll(logDir, 0o755)
	return &Engine{
		store:   s,
		router:  router,
		emit:    emit,
		logDir:  logDir,
		active:  make(map[string]context.CancelFunc),
		logSubs: make(map[string]context.CancelFunc),
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

	serviceRoot := project.Path
	if rel := settings["service_root"]; rel != "" {
		serviceRoot = filepath.Join(project.Path, rel)
	}

	if !filepath.IsAbs(dockerfilePath) {
		dockerfilePath = filepath.Join(serviceRoot, dockerfilePath)
	}

	relDockerfile, err := filepath.Rel(serviceRoot, dockerfilePath)
	if err != nil || strings.HasPrefix(relDockerfile, "..") {
		serviceRoot = project.Path
		relDockerfile, err = filepath.Rel(serviceRoot, dockerfilePath)
		if err != nil {
			e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "cannot resolve dockerfile path"})
			return
		}
	}
	relDockerfile = filepath.ToSlash(relDockerfile)

	e.emitBuildLog(nodeID, fmt.Sprintf("    Dockerfile: %s", relDockerfile))
	e.emitBuildLog(nodeID, fmt.Sprintf("    Service port: %s", portStr))
	e.emitBuildLog(nodeID, fmt.Sprintf("    Build context: %s", serviceRoot))

	dep := &store.Deployment{
		NodeID:     nodeID,
		ProjectID:  node.ProjectID,
		Status:     "building",
		JobID:      fmt.Sprintf("%d-%s", time.Now().UnixNano(), nodeID),
		WorkerPID:  os.Getpid(),
		StartedAt:  ptrTime(time.Now()),
		LastSeenAt: ptrTime(time.Now()),
	}
	dep, err = e.store.CreateDeployment(dep)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "failed to create deployment record"})
		return
	}

	serviceName := sanitize(node.Label)
	projectName := sanitize(project.Name)
	environment := "default"
	uid := networking.GenerateUID()
	hostname := networking.Hostname(serviceName, projectName, environment, uid)
	deployEnv, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:      nodeID,
		ServiceName: serviceName,
		ProjectName: projectName,
		Environment: environment,
		Port:        portStr,
		Hostname:    hostname,
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

	matcher := ignore.New()
	if settings["use_dockerignore"] == "true" {
		e.emitBuildLog(nodeID, "==> Scanning for .dockerignore files...")
		n, _ := matcher.ScanDir(project.Path, serviceRoot, ".dockerignore")
		e.emitBuildLog(nodeID, fmt.Sprintf("    Found %d .dockerignore file(s)", n))
	}
	if settings["use_gitignore"] == "true" {
		e.emitBuildLog(nodeID, "==> Scanning for .gitignore files...")
		n, _ := matcher.ScanDir(project.Path, serviceRoot, ".gitignore")
		e.emitBuildLog(nodeID, fmt.Sprintf("    Found %d .gitignore file(s)", n))
	}

	e.emitBuildLog(nodeID, "==> Packaging build context...")
	packStart := time.Now()
	buildContext, fileCount, totalBytes, err := tarDirectoryWithProgress(serviceRoot, matcher, func(files int, bytes int64) {
		e.emitBuildLog(nodeID, fmt.Sprintf("    Packaged %d files (%.1f MB)", files, float64(bytes)/(1024*1024)))
	})
	if err != nil {
		e.failDeployment(dep, nodeID, "cannot create build context: "+err.Error())
		return
	}
	defer buildContext.Close()
	e.emitBuildLog(nodeID, fmt.Sprintf("    Done: %d files, %.1f MB in %s", fileCount, float64(totalBytes)/(1024*1024), time.Since(packStart).Round(time.Millisecond)))

	e.emitBuildLog(nodeID, "==> Sending build context to Docker...")
	e.emitBuildLog(nodeID, fmt.Sprintf("    docker build -t %s -f %s", imageTag, relDockerfile))

	uploadStart := time.Now()
	tracker := &uploadTracker{
		reader:     buildContext,
		totalBytes: totalBytes,
		onProgress: func(sent int64, total int64) {
			pct := float64(sent) / float64(total) * 100
			e.emitBuildLog(nodeID, fmt.Sprintf("    Uploading: %.0f%% (%.1f / %.1f MB)", pct, float64(sent)/(1024*1024), float64(total)/(1024*1024)))
		},
	}

	resp, err := cli.ImageBuild(ctx, tracker, build.ImageBuildOptions{
		Tags:       []string{imageTag},
		Dockerfile: relDockerfile,
		Remove:     true,
		BuildArgs:  deployEnv.BuildArgs,
	})
	if err != nil {
		e.failDeployment(dep, nodeID, "docker build failed: "+err.Error())
		return
	}
	defer resp.Body.Close()
	e.emitBuildLog(nodeID, fmt.Sprintf("    Upload complete in %s", time.Since(uploadStart).Round(time.Millisecond)))

	buildStart := time.Now()
	buildErr := e.streamBuildOutput(ctx, resp.Body, logFile, nodeID, dep.ID)
	if buildErr != nil {
		e.failDeployment(dep, nodeID, "build error: "+buildErr.Error())
		return
	}

	if ctx.Err() != nil {
		e.failDeployment(dep, nodeID, "build cancelled")
		return
	}

	e.emitBuildLog(nodeID, fmt.Sprintf("==> Build completed in %s", time.Since(buildStart).Round(time.Millisecond)))

	now := time.Now()
	dep.Status = "built"
	dep.FinishedAt = &now
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

	e.emitBuildLog(nodeID, "==> Creating container...")
	dep.Status = "starting"
	dep.LastSeenAt = ptrTime(time.Now())
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "starting"})

	containerPort := nat.Port(portStr + "/tcp")
	containerName := fmt.Sprintf("draft-%s-%s-%d", projectName, serviceName, dep.ID)
	createResp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: imageTag,
		Env:   deployEnv.RuntimeEnv,
		Labels: map[string]string{
			"draft.project":    fmt.Sprintf("%d", node.ProjectID),
			"draft.node":       nodeID,
			"draft.deployment": fmt.Sprintf("%d", dep.ID),
		},
		ExposedPorts: nat.PortSet{containerPort: struct{}{}},
	}, &container.HostConfig{
		PortBindings: nat.PortMap{
			containerPort: []nat.PortBinding{{HostIP: "127.0.0.1", HostPort: "0"}},
		},
	}, nil, nil, containerName)
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
	dep.LastSeenAt = ptrTime(time.Now())
	if regResult != nil {
		dep.Hostname = regResult.Hostname
	}
	e.store.UpdateDeployment(dep)
	e.emitBuildLog(nodeID, "==> Deployed successfully!")

	e.emitStatus(nodeID, StatusEvent{
		DeploymentID: dep.ID,
		Status:       "running",
		Hostname:     dep.Hostname,
		HostPort:     hostPort,
	})

	go e.watchContainer(context.Background(), dep, nodeID)
}

type uploadTracker struct {
	reader     io.Reader
	totalBytes int64
	sent       int64
	lastPct    int
	onProgress func(sent int64, total int64)
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
	dep.FinishedAt = &now
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{
		DeploymentID: dep.ID,
		Status:       dep.Status,
		Error:        dep.Error,
	})
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
		if d.Status == "stopped" || d.Status == "failed" {
			continue
		}
		if d.ContainerID != "" {
			timeout := 10
			cli.ContainerStop(ctx, d.ContainerID, container.StopOptions{Timeout: &timeout})
			cli.ContainerRemove(ctx, d.ContainerID, container.RemoveOptions{})
		}
		if d.ImageTag != "" {
			cli.ImageRemove(ctx, d.ImageTag, image.RemoveOptions{})
		}
		if d.Hostname != "" {
			e.router.Unregister(d.Hostname)
		}
		d.Status = "stopped"
		now := time.Now()
		d.FinishedAt = &now
		e.store.UpdateDeployment(&d)
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
	dep.FinishedAt = &now
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
			e.emitStatus(dep.NodeID, StatusEvent{DeploymentID: dep.ID, Status: "failed", Error: dep.Error})
		} else {
			dep.Status = "stopped"
			dep.FinishedAt = ptrTime(time.Now())
			e.emitStatus(dep.NodeID, StatusEvent{DeploymentID: dep.ID, Status: "stopped"})
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
			dep.Status = "failed"
			dep.Error = "deployment was interrupted before the daemon could recover it"
			dep.FinishedAt = ptrTime(time.Now())
			e.store.UpdateDeployment(dep)
			e.emitStatus(dep.NodeID, StatusEvent{DeploymentID: dep.ID, Status: "failed", Error: dep.Error})
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
