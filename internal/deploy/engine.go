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
	"strings"
	"sync"
	"time"

	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types/build"
	"github.com/docker/docker/api/types/container"
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

	dep := &store.Deployment{
		NodeID:    nodeID,
		ProjectID: node.ProjectID,
		Status:    "building",
	}
	dep, err = e.store.CreateDeployment(dep)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "failed to create deployment record"})
		return
	}

	serviceName := sanitize(node.Label)
	projectName := sanitize(project.Name)
	imageTag := fmt.Sprintf("draft-%s-%s:%d", projectName, serviceName, dep.ID)
	dep.ImageTag = imageTag
	e.store.UpdateDeployment(dep)

	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "building"})

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		e.failDeployment(dep, nodeID, "cannot connect to Docker: "+err.Error())
		return
	}
	defer cli.Close()

	logFile, err := os.Create(e.logPath(dep.ID))
	if err != nil {
		e.failDeployment(dep, nodeID, "cannot create build log: "+err.Error())
		return
	}
	defer logFile.Close()

	buildContext, err := tarDirectory(serviceRoot)
	if err != nil {
		e.failDeployment(dep, nodeID, "cannot create build context: "+err.Error())
		return
	}
	defer buildContext.Close()

	resp, err := cli.ImageBuild(ctx, buildContext, build.ImageBuildOptions{
		Tags:       []string{imageTag},
		Dockerfile: relDockerfile,
		Remove:     true,
	})
	if err != nil {
		e.failDeployment(dep, nodeID, "docker build failed: "+err.Error())
		return
	}
	defer resp.Body.Close()

	buildErr := e.streamBuildOutput(ctx, resp.Body, logFile, nodeID, dep.ID)
	if buildErr != nil {
		e.failDeployment(dep, nodeID, "build error: "+buildErr.Error())
		return
	}

	if ctx.Err() != nil {
		e.failDeployment(dep, nodeID, "build cancelled")
		return
	}

	now := time.Now()
	dep.Status = "built"
	dep.FinishedAt = &now
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "built"})

	if err := e.stopPrevious(ctx, cli, nodeID, dep.ID); err != nil {
		log.Printf("[deploy] warning: stop previous: %v", err)
	}

	dep.Status = "starting"
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "starting"})

	containerPort := nat.Port(portStr + "/tcp")
	createResp, err := cli.ContainerCreate(ctx, &container.Config{
		Image: imageTag,
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
	}, nil, nil, fmt.Sprintf("draft-%s-%s-%d", projectName, serviceName, dep.ID))
	if err != nil {
		e.failDeployment(dep, nodeID, "container create failed: "+err.Error())
		return
	}

	dep.ContainerID = createResp.ID
	e.store.UpdateDeployment(dep)

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

	uid := networking.GenerateUID()
	regResult, err := e.router.Register(networking.RegisterRequest{
		Service:    serviceName,
		Project:    projectName,
		ProjectID:  node.ProjectID,
		NodeID:     nodeID,
		UID:        uid,
		Protocol:   "http",
		TargetHost: "127.0.0.1",
		TargetPort: hostPort,
	})
	if err != nil {
		log.Printf("[deploy] route registration failed (non-fatal): %v", err)
	}

	dep.Status = "running"
	dep.HostPort = hostPort
	if regResult != nil {
		dep.Hostname = regResult.Hostname
	}
	e.store.UpdateDeployment(dep)

	e.emitStatus(nodeID, StatusEvent{
		DeploymentID: dep.ID,
		Status:       "running",
		Hostname:     dep.Hostname,
		HostPort:     hostPort,
	})

	go e.watchContainer(context.Background(), cli, dep, nodeID)
}

func (e *Engine) streamBuildOutput(ctx context.Context, reader io.Reader, logFile *os.File, nodeID string, deploymentID uint) error {
	scanner := bufio.NewScanner(reader)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)

	for scanner.Scan() {
		if ctx.Err() != nil {
			return ctx.Err()
		}

		line := scanner.Text()
		logFile.WriteString(line + "\n")

		var msg struct {
			Stream string `json:"stream"`
			Error  string `json:"error"`
		}
		if json.Unmarshal([]byte(line), &msg) == nil {
			if msg.Error != "" {
				e.emit("build:log:"+nodeID, LogLine{Line: msg.Error, Stream: "build"})
				return fmt.Errorf("%s", msg.Error)
			}
			if msg.Stream != "" {
				e.emit("build:log:"+nodeID, LogLine{Line: strings.TrimRight(msg.Stream, "\n"), Stream: "build"})
			}
		}
	}
	return scanner.Err()
}

func (e *Engine) watchContainer(ctx context.Context, cli *client.Client, dep *store.Deployment, nodeID string) {
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
}

func (e *Engine) failDeployment(dep *store.Deployment, nodeID, errMsg string) {
	dep.Status = "failed"
	dep.Error = errMsg
	now := time.Now()
	dep.FinishedAt = &now
	e.store.UpdateDeployment(dep)
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "failed", Error: errMsg})
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

func tarDirectory(dir string) (io.ReadCloser, error) {
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

			if shouldSkip(rel) {
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
	return pr, nil
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
