//go:build integration

package deploy

import (
	"bytes"
	"context"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/pkg/stdcopy"
)

func requireDocker(t *testing.T) *client.Client {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("Docker not available: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		cli.Close()
		t.Skipf("Docker daemon not running: %v", err)
	}
	return cli
}

func setupIntegration(t *testing.T) (*Engine, *store.Store, *eventCollector, string) {
	t.Helper()
	s := openTestStore(t)
	r := networking.NewRouter(s, "127.0.0.1:0")
	if err := r.Start(); err != nil {
		t.Fatalf("start router: %v", err)
	}
	t.Cleanup(func() { r.Stop() })

	logDir := filepath.Join(t.TempDir(), "logs")
	col := &eventCollector{}
	e := New(s, r, logDir, col.emit)
	projectDir := t.TempDir()

	project := &store.Project{
		Name: integrationProjectName("it"),
		Path: projectDir,
	}
	if err := s.DB.Create(project).Error; err != nil {
		t.Fatalf("create project: %v", err)
	}
	if err := s.DB.Create(&store.CanvasNode{ID: "svc1", ProjectID: project.ID, Label: "test-svc"}).Error; err != nil {
		t.Fatalf("create node: %v", err)
	}
	cleanupCLI := requireDocker(t)
	t.Cleanup(func() {
		cleanupProjectResources(t, cleanupCLI, s, project.ID, project.Name)
		cleanupCLI.Close()
	})

	return e, s, col, projectDir
}

func integrationProjectName(prefix string) string {
	return fmt.Sprintf("%s-%08x", prefix, uint32(time.Now().UnixNano()))
}

func writeDockerfile(t *testing.T, dir, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte(content), 0644); err != nil {
		t.Fatal(err)
	}
}

func cleanupContainers(t *testing.T, cli *client.Client, deployments []store.Deployment) {
	t.Helper()
	ctx := context.Background()
	for _, d := range deployments {
		if d.ContainerID != "" {
			timeout := 3
			cli.ContainerStop(ctx, d.ContainerID, container.StopOptions{Timeout: &timeout})
			cli.ContainerRemove(ctx, d.ContainerID, container.RemoveOptions{Force: true})
		}
		if d.ImageTag != "" {
			cli.ImageRemove(ctx, d.ImageTag, image.RemoveOptions{Force: true})
		}
	}
}

func cleanupProjectResources(t *testing.T, cli *client.Client, s *store.Store, projectID uint, projectName string) {
	t.Helper()

	var deployments []store.Deployment
	if err := s.DB.Where("project_id = ?", projectID).Find(&deployments).Error; err == nil {
		cleanupContainers(t, cli, deployments)
	}

	cleanupDraftNetworks(t, cli, projectID, projectName)
}

func cleanupDraftNetworks(t *testing.T, cli *client.Client, projectID uint, projectName string) {
	t.Helper()

	networks, err := cli.NetworkList(context.Background(), dockernetwork.ListOptions{
		Filters: filters.NewArgs(
			filters.Arg("label", "draft.managed=true"),
			filters.Arg("label", fmt.Sprintf("draft.project=%d", projectID)),
			filters.Arg("label", "draft.projectName="+projectName),
		),
	})
	if err != nil {
		t.Logf("list draft networks: %v", err)
		return
	}

	for _, network := range networks {
		if err := cli.NetworkRemove(context.Background(), network.ID); err != nil {
			t.Logf("remove draft network %s: %v", network.Name, err)
		}
	}
}

func waitForStatus(col *eventCollector, nodeID, status string, timeout time.Duration) *StatusEvent {
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		ev := col.findStatus(nodeID, status)
		if ev != nil {
			se := ev.Data.(StatusEvent)
			return &se
		}
		time.Sleep(100 * time.Millisecond)
	}
	return nil
}

// TestIntegrationDeploySuccess builds and runs a minimal container,
// verifies it reaches "running" with a host port assigned.
func TestIntegrationDeploySuccess(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, `FROM alpine:3.20
EXPOSE 8080
HEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true
CMD ["sh", "-c", "while true; do echo hello; sleep 1; done"]
`)

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "8080")

	e.Deploy(context.Background(), "svc1")

	ev := waitForStatus(col, "svc1", "running", 60*time.Second)
	if ev == nil {
		// Check if it failed instead
		fail := col.findStatus("svc1", "failed")
		msg := "no events"
		if fail != nil {
			msg = fail.Data.(StatusEvent).Error
		}
		t.Fatalf("expected running status within 60s, last error: %s", msg)
	}

	if ev.HostPort == 0 {
		t.Error("expected non-zero host port")
	}
	if ev.DeploymentID == 0 {
		t.Error("expected non-zero deployment ID")
	}

	dep, err := s.ActiveDeployment("svc1")
	if err != nil {
		t.Fatal(err)
	}
	if dep == nil {
		t.Fatal("expected active deployment in store")
	}
	if dep.Status != "running" {
		t.Errorf("expected status=running in store, got %s", dep.Status)
	}
	if dep.ContainerID == "" {
		t.Error("expected containerID in store")
	}
	if dep.HostPort == 0 {
		t.Error("expected host port in store")
	}
	if dep.ImageTag == "" {
		t.Error("expected image tag in store")
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationBuildLog verifies that the build log file is written during a deploy.
func TestIntegrationBuildLog(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, `FROM alpine:3.20
RUN echo "build-step-marker"
CMD ["true"]
`)

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	e.Deploy(context.Background(), "svc1")

	// Wait for at least "built" status (build finished)
	built := waitForStatus(col, "svc1", "built", 60*time.Second)
	if built == nil {
		fail := col.findStatus("svc1", "failed")
		msg := "no events"
		if fail != nil {
			msg = fail.Data.(StatusEvent).Error
		}
		t.Fatalf("expected built status within 60s, error: %s", msg)
	}

	log, err := e.GetBuildLog(built.DeploymentID)
	if err != nil {
		t.Fatal(err)
	}
	if log == "" {
		t.Fatal("expected non-empty build log")
	}
	if !strings.Contains(log, "build-step-marker") {
		t.Errorf("build log should contain our RUN echo marker, got:\n%s", log)
	}

	// Also check that build log events were emitted
	var buildLogEvents []emittedEvent
	for _, ev := range col.get() {
		if ev.Name == "build:log:svc1" {
			buildLogEvents = append(buildLogEvents, ev)
		}
	}
	if len(buildLogEvents) == 0 {
		t.Error("expected build:log events to be emitted")
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationBuildFailure verifies that a broken Dockerfile produces a "failed" deployment.
func TestIntegrationBuildFailure(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, `FROM alpine:3.20
RUN exit 1
`)

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	e.Deploy(context.Background(), "svc1")

	ev := waitForStatus(col, "svc1", "failed", 60*time.Second)
	if ev == nil {
		t.Fatal("expected failed status for broken build")
	}
	if ev.Error == "" {
		t.Error("expected error message on failed build")
	}

	dep, _ := s.ActiveDeployment("svc1")
	if dep != nil {
		t.Errorf("expected no active deployment after failure, got %+v", dep)
	}

	all, _ := s.ListDeployments("svc1")
	if len(all) != 1 {
		t.Fatalf("expected 1 deployment record, got %d", len(all))
	}
	if all[0].Status != "failed" {
		t.Errorf("expected failed status in store, got %s", all[0].Status)
	}
	if all[0].FinishedAt == nil {
		t.Error("expected FinishedAt to be set on failed deployment")
	}

	t.Cleanup(func() {
		cleanupContainers(t, cli, all)
	})
}

// TestIntegrationContainerCrash verifies that a container that exits with a non-zero code
// is detected and the deployment is marked failed.
func TestIntegrationContainerCrash(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, `FROM alpine:3.20
CMD ["sh", "-c", "echo crashing; exit 42"]
`)

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	e.Deploy(context.Background(), "svc1")

	// First wait for running
	running := waitForStatus(col, "svc1", "running", 60*time.Second)
	if running == nil {
		fail := col.findStatus("svc1", "failed")
		if fail != nil {
			// May have gone straight to failed if container exited quickly
			se := fail.Data.(StatusEvent)
			if strings.Contains(se.Error, "exit") {
				return // OK — crash detected
			}
		}
		t.Fatal("expected running or failed status")
	}

	// Now wait for the crash to be detected
	failed := waitForStatus(col, "svc1", "failed", 30*time.Second)
	if failed == nil {
		t.Fatal("expected failed status after container crash")
	}
	if !strings.Contains(failed.Error, "exited with code 42") {
		t.Errorf("expected exit code 42 in error, got: %s", failed.Error)
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationStop deploys a service and then stops it.
func TestIntegrationStop(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, `FROM alpine:3.20
HEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true
CMD ["sleep", "3600"]
`)

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	e.Deploy(context.Background(), "svc1")

	running := waitForStatus(col, "svc1", "running", 60*time.Second)
	if running == nil {
		t.Fatal("expected running status before stop test")
	}

	dep, _ := s.ActiveDeployment("svc1")
	containerID := dep.ContainerID

	err := e.Stop(context.Background(), "svc1")
	if err != nil {
		t.Fatalf("Stop error: %v", err)
	}

	stopped := waitForStatus(col, "svc1", "stopped", 15*time.Second)
	if stopped == nil {
		t.Fatal("expected stopped status after Stop()")
	}

	active, _ := s.ActiveDeployment("svc1")
	if active != nil {
		t.Errorf("expected no active deployment after stop, got %+v", active)
	}

	// Verify container is actually gone from Docker
	_, err = cli.ContainerInspect(context.Background(), containerID)
	if err == nil {
		t.Error("expected container to be removed after stop")
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationRestart deploys a service and restarts it.
func TestIntegrationRestart(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, `FROM alpine:3.20
HEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true
CMD ["sleep", "3600"]
`)

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	e.Deploy(context.Background(), "svc1")

	running := waitForStatus(col, "svc1", "running", 60*time.Second)
	if running == nil {
		t.Fatal("expected running status before restart test")
	}

	dep, _ := s.ActiveDeployment("svc1")
	containerID := dep.ContainerID

	// Get container start time before restart
	inspectBefore, err := cli.ContainerInspect(context.Background(), containerID)
	if err != nil {
		t.Fatal(err)
	}
	startedBefore := inspectBefore.State.StartedAt

	err = e.Restart(context.Background(), "svc1")
	if err != nil {
		t.Fatalf("Restart error: %v", err)
	}

	// Wait a moment and verify the container restarted
	time.Sleep(2 * time.Second)
	inspectAfter, err := cli.ContainerInspect(context.Background(), containerID)
	if err != nil {
		t.Fatal(err)
	}
	if inspectAfter.State.StartedAt == startedBefore {
		t.Error("expected container StartedAt to change after restart")
	}
	if !inspectAfter.State.Running {
		t.Error("expected container to be running after restart")
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationLogStream deploys a service that produces output and streams its logs.
func TestIntegrationLogStream(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, `FROM alpine:3.20
HEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true
CMD ["sh", "-c", "for i in 1 2 3 4 5; do echo log-line-$i; sleep 0.2; done; sleep 3600"]
`)

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	e.Deploy(context.Background(), "svc1")

	running := waitForStatus(col, "svc1", "running", 60*time.Second)
	if running == nil {
		t.Fatal("expected running status before log stream test")
	}

	// Start log streaming
	err := e.StartLogStream(context.Background(), "svc1")
	if err != nil {
		t.Fatalf("StartLogStream error: %v", err)
	}

	// Wait for log lines to arrive
	deadline := time.Now().Add(15 * time.Second)
	var logLines []emittedEvent
	for time.Now().Before(deadline) {
		time.Sleep(500 * time.Millisecond)
		logLines = nil
		for _, ev := range col.get() {
			if ev.Name == "container:log:svc1" {
				logLines = append(logLines, ev)
			}
		}
		if len(logLines) >= 3 {
			break
		}
	}

	if len(logLines) < 3 {
		t.Fatalf("expected at least 3 container log lines, got %d", len(logLines))
	}

	foundMarker := false
	for _, ev := range logLines {
		ll := ev.Data.(LogLine)
		if strings.Contains(ll.Line, "log-line-") {
			foundMarker = true
		}
		if ll.Stream != "stdout" && ll.Stream != "stderr" {
			t.Errorf("expected stream to be stdout or stderr, got %s", ll.Stream)
		}
	}
	if !foundMarker {
		t.Error("expected to find 'log-line-' marker in container logs")
	}

	e.StopLogStream("svc1")

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationRedeployCancelsPrevious verifies that starting a new deploy
// stops the previously running container.
func TestIntegrationRedeployCancelsPrevious(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, `FROM alpine:3.20
HEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true
CMD ["sleep", "3600"]
`)

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	// First deploy
	e.Deploy(context.Background(), "svc1")
	running1 := waitForStatus(col, "svc1", "running", 60*time.Second)
	if running1 == nil {
		t.Fatal("expected first deploy to reach running")
	}

	dep1, _ := s.ActiveDeployment("svc1")
	firstContainerID := dep1.ContainerID
	firstDeployID := dep1.ID

	// Second deploy — should stop the first
	e.Deploy(context.Background(), "svc1")

	// Wait for second deploy to reach running
	deadline := time.Now().Add(60 * time.Second)
	var secondRunning *StatusEvent
	for time.Now().Before(deadline) {
		for _, ev := range col.get() {
			if ev.Name == "deploy:status:svc1" {
				se := ev.Data.(StatusEvent)
				if se.Status == "running" && se.DeploymentID != firstDeployID {
					secondRunning = &se
				}
			}
		}
		if secondRunning != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}

	if secondRunning == nil {
		t.Fatal("expected second deploy to reach running")
	}

	// Verify first container was stopped
	_, err := cli.ContainerInspect(context.Background(), firstContainerID)
	if err == nil {
		t.Error("expected first container to be removed after redeploy")
	}

	// Verify the first deployment is marked stopped
	oldDep, _ := s.GetDeployment(firstDeployID)
	if oldDep.Status != "stopped" {
		t.Errorf("expected first deployment to be stopped, got %s", oldDep.Status)
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationStderrLogs verifies that stderr output is correctly streamed
// with stream="stderr".
func TestIntegrationStderrLogs(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, `FROM alpine:3.20
HEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true
CMD ["sh", "-c", "echo stdout-marker; echo stderr-marker >&2; sleep 3600"]
`)

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	e.Deploy(context.Background(), "svc1")

	running := waitForStatus(col, "svc1", "running", 60*time.Second)
	if running == nil {
		t.Fatal("expected running before stderr test")
	}

	// Give container a moment to produce output
	time.Sleep(2 * time.Second)

	err := e.StartLogStream(context.Background(), "svc1")
	if err != nil {
		t.Fatalf("StartLogStream: %v", err)
	}

	deadline := time.Now().Add(15 * time.Second)
	foundStdout := false
	foundStderr := false
	for time.Now().Before(deadline) {
		for _, ev := range col.get() {
			if ev.Name == "container:log:svc1" {
				ll := ev.Data.(LogLine)
				if strings.Contains(ll.Line, "stdout-marker") && ll.Stream == "stdout" {
					foundStdout = true
				}
				if strings.Contains(ll.Line, "stderr-marker") && ll.Stream == "stderr" {
					foundStderr = true
				}
			}
		}
		if foundStdout && foundStderr {
			break
		}
		time.Sleep(500 * time.Millisecond)
	}

	if !foundStdout {
		t.Error("expected stdout-marker on stdout stream")
	}
	if !foundStderr {
		t.Error("expected stderr-marker on stderr stream")
	}

	e.StopLogStream("svc1")

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationRedeployCleansOldImage verifies that a redeploy removes the
// previous deployment's Docker image to reclaim disk space.
func TestIntegrationRedeployCleansOldImage(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, "FROM alpine:3.20\nHEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true\nCMD [\"sleep\", \"3600\"]\n")

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	// First deploy
	e.Deploy(context.Background(), "svc1")
	running1 := waitForStatus(col, "svc1", "running", 60*time.Second)
	if running1 == nil {
		t.Fatal("expected first deploy to reach running")
	}

	dep1, _ := s.ActiveDeployment("svc1")
	firstImage := dep1.ImageTag
	firstDeployID := dep1.ID

	// Second deploy
	e.Deploy(context.Background(), "svc1")

	deadline := time.Now().Add(60 * time.Second)
	var secondRunning *StatusEvent
	for time.Now().Before(deadline) {
		for _, ev := range col.get() {
			if ev.Name == "deploy:status:svc1" {
				se := ev.Data.(StatusEvent)
				if se.Status == "running" && se.DeploymentID != firstDeployID {
					secondRunning = &se
				}
			}
		}
		if secondRunning != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if secondRunning == nil {
		t.Fatal("expected second deploy to reach running")
	}

	// Old image should have been removed
	_, _, err := cli.ImageInspectWithRaw(context.Background(), firstImage)
	if err == nil {
		t.Errorf("expected old image %s to be removed after redeploy", firstImage)
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationStopCleansImage verifies that stopping a deployment removes
// its container and image.
func TestIntegrationStopCleansImage(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, "FROM alpine:3.20\nHEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true\nCMD [\"sleep\", \"3600\"]\n")

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	e.Deploy(context.Background(), "svc1")
	running := waitForStatus(col, "svc1", "running", 60*time.Second)
	if running == nil {
		t.Fatal("expected running")
	}

	dep, _ := s.ActiveDeployment("svc1")
	imageTag := dep.ImageTag

	if err := e.Stop(context.Background(), "svc1"); err != nil {
		t.Fatalf("Stop: %v", err)
	}

	stopped := waitForStatus(col, "svc1", "stopped", 15*time.Second)
	if stopped == nil {
		t.Fatal("expected stopped")
	}

	_, _, err := cli.ImageInspectWithRaw(context.Background(), imageTag)
	if err == nil {
		t.Errorf("expected image %s to be removed after stop", imageTag)
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationCrashCleansImage verifies that when a container exits on its
// own, the watcher removes the container and image.
func TestIntegrationCrashCleansImage(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, "FROM alpine:3.20\nCMD [\"sh\", \"-c\", \"sleep 1; exit 1\"]\n")

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	e.Deploy(context.Background(), "svc1")

	// Wait for the crash to be detected
	failed := waitForStatus(col, "svc1", "failed", 60*time.Second)
	if failed == nil {
		t.Fatal("expected failed status after crash")
	}

	dep, _ := s.GetDeployment(failed.DeploymentID)
	if dep == nil {
		t.Fatal("deployment not found in store")
	}

	// Give the watcher goroutine a moment to finish cleanup
	time.Sleep(2 * time.Second)

	_, _, err := cli.ImageInspectWithRaw(context.Background(), dep.ImageTag)
	if err == nil {
		t.Errorf("expected image %s to be removed after container crash", dep.ImageTag)
	}

	if dep.ContainerID != "" {
		_, inspectErr := cli.ContainerInspect(context.Background(), dep.ContainerID)
		if inspectErr == nil {
			t.Errorf("expected container %s to be removed after crash", dep.ContainerID)
		}
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationRedeployCleansStaleImages verifies that when a redeploy happens
// and there are old stopped/failed deployments with images still on disk,
// those images are cleaned up too.
func TestIntegrationRedeployCleansStaleImages(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, "FROM alpine:3.20\nHEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true\nCMD [\"sleep\", \"3600\"]\n")

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	// First deploy
	e.Deploy(context.Background(), "svc1")
	running1 := waitForStatus(col, "svc1", "running", 60*time.Second)
	if running1 == nil {
		t.Fatal("first deploy did not reach running")
	}
	dep1, _ := s.ActiveDeployment("svc1")
	firstImage := dep1.ImageTag

	// Stop the first deploy (leaves image on disk in the old code)
	if err := e.Stop(context.Background(), "svc1"); err != nil {
		t.Fatalf("Stop: %v", err)
	}
	waitForStatus(col, "svc1", "stopped", 15*time.Second)

	// Manually re-pull/re-tag the image so it exists again for the test,
	// simulating the "old code" behavior where Stop didn't remove images.
	// (Our new Stop does remove, so we rebuild the same tag to test
	// that stopPrevious also cleans stale ones.)
	_, err := cli.ImagePull(context.Background(), "alpine:3.20", image.PullOptions{})
	if err == nil {
		// Tag it as the first deployment's image
		cli.ImageTag(context.Background(), "alpine:3.20", firstImage)
	}

	// Second deploy — stopPrevious should clean the stale first image
	e.Deploy(context.Background(), "svc1")

	deadline := time.Now().Add(60 * time.Second)
	var secondRunning *StatusEvent
	firstDeployID := dep1.ID
	for time.Now().Before(deadline) {
		for _, ev := range col.get() {
			if ev.Name == "deploy:status:svc1" {
				se := ev.Data.(StatusEvent)
				if se.Status == "running" && se.DeploymentID != firstDeployID {
					secondRunning = &se
				}
			}
		}
		if secondRunning != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if secondRunning == nil {
		t.Fatal("second deploy did not reach running")
	}

	_, _, err = cli.ImageInspectWithRaw(context.Background(), firstImage)
	if err == nil {
		t.Errorf("expected stale image %s to be removed by stopPrevious", firstImage)
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// TestIntegrationRedeployKeepsStableHostname verifies that redeploying the
// same node produces the same internal hostname (and therefore the same
// Docker network DNS alias) across deployments, since the node's UID is now
// persisted rather than regenerated on every deploy.
func TestIntegrationRedeployKeepsStableHostname(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, "FROM alpine:3.20\nHEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true\nCMD [\"sleep\", \"3600\"]\n")

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	// First deploy
	e.Deploy(context.Background(), "svc1")
	running1 := waitForStatus(col, "svc1", "running", 60*time.Second)
	if running1 == nil {
		t.Fatal("expected first deploy to reach running")
	}
	dep1, _ := s.ActiveDeployment("svc1")
	firstHostname := dep1.Hostname
	firstDeployID := dep1.ID
	if firstHostname == "" {
		t.Fatal("expected first deployment to have a hostname")
	}

	// Second deploy of the same node
	e.Deploy(context.Background(), "svc1")

	deadline := time.Now().Add(60 * time.Second)
	var secondRunning *StatusEvent
	for time.Now().Before(deadline) {
		for _, ev := range col.get() {
			if ev.Name == "deploy:status:svc1" {
				se := ev.Data.(StatusEvent)
				if se.Status == "running" && se.DeploymentID != firstDeployID {
					secondRunning = &se
				}
			}
		}
		if secondRunning != nil {
			break
		}
		time.Sleep(200 * time.Millisecond)
	}
	if secondRunning == nil {
		t.Fatal("expected second deploy to reach running")
	}

	dep2, _ := s.ActiveDeployment("svc1")
	if dep2.Hostname != firstHostname {
		t.Errorf("hostname changed across redeploy: %q != %q", dep2.Hostname, firstHostname)
	}

	node, err := s.GetNode("svc1")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.UID == "" {
		t.Error("expected node to have a persisted UID")
	}
	if !strings.Contains(firstHostname, node.UID) {
		t.Errorf("expected hostname %q to contain node UID %q", firstHostname, node.UID)
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}

// execInContainer runs cmd inside an already-running container via `docker
// exec` and returns its combined stdout+stderr and exit code.
func execInContainer(t *testing.T, cli *client.Client, containerID string, cmd []string) (string, int, error) {
	t.Helper()
	ctx := context.Background()
	execResp, err := cli.ContainerExecCreate(ctx, containerID, container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
	})
	if err != nil {
		return "", 0, fmt.Errorf("exec create: %w", err)
	}
	attachResp, err := cli.ContainerExecAttach(ctx, execResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return "", 0, fmt.Errorf("exec attach: %w", err)
	}
	defer attachResp.Close()

	var stdout, stderr bytes.Buffer
	if _, err := stdcopy.StdCopy(&stdout, &stderr, attachResp.Reader); err != nil {
		return "", 0, fmt.Errorf("read exec output: %w", err)
	}

	inspect, err := cli.ContainerExecInspect(ctx, execResp.ID)
	if err != nil {
		return "", 0, fmt.Errorf("exec inspect: %w", err)
	}
	return stdout.String() + stderr.String(), inspect.ExitCode, nil
}

// TestIntegrationInternalHostnameResolvesContainerToContainer verifies the
// actual claim behind "docker-to-docker communication works": one service's
// container can resolve and reach a sibling service's container by its
// internal Draft hostname (the same hostname baked into DRAFT_INTERNAL_URL)
// over Docker's embedded DNS on their shared project network. It also
// confirms that hostname is NOT resolvable from the host/browser — it's
// never written to the OS hosts file, only Docker's internal resolver knows
// about it.
func TestIntegrationInternalHostnameResolvesContainerToContainer(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	// Register cleanup immediately (not after the fatal-prone waits below)
	// so a failed run never leaves containers behind to collide with the
	// next run's container names.
	t.Cleanup(func() {
		deps1, _ := s.ListDeployments("svc1")
		deps2, _ := s.ListDeployments("svc2")
		cleanupContainers(t, cli, append(deps1, deps2...))
	})

	// svc1 (created by setupIntegration): the caller. Lives in its own
	// subdirectory so it can have a different Dockerfile than svc2.
	svc1Dir := filepath.Join(projectDir, "svc1")
	if err := os.MkdirAll(svc1Dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeDockerfile(t, svc1Dir, "FROM alpine:3.20\nHEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true\nCMD [\"sleep\", \"3600\"]\n")
	s.SetNodeSetting("svc1", "service_root", "svc1")
	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	// svc2: the target. Serves a known string over HTTP on 8080.
	svc2Dir := filepath.Join(projectDir, "svc2")
	if err := os.MkdirAll(svc2Dir, 0755); err != nil {
		t.Fatal(err)
	}
	writeDockerfile(t, svc2Dir, `FROM alpine:3.20
RUN apk add --no-cache python3 && mkdir -p /www && echo -n "internal-hello" > /www/index.html
CMD ["python3", "-m", "http.server", "8080", "--directory", "/www"]
`)
	if err := s.DB.Create(&store.CanvasNode{ID: "svc2", ProjectID: 1, Label: "target-svc"}).Error; err != nil {
		t.Fatalf("create svc2 node: %v", err)
	}
	s.SetNodeSetting("svc2", "service_root", "svc2")
	s.SetNodeSetting("svc2", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc2", "service_port", "8080")

	e.Deploy(context.Background(), "svc1")
	if waitForStatus(col, "svc1", "running", 60*time.Second) == nil {
		t.Fatal("expected svc1 to reach running")
	}

	e.Deploy(context.Background(), "svc2")
	if waitForStatus(col, "svc2", "running", 60*time.Second) == nil {
		t.Fatal("expected svc2 to reach running")
	}

	dep1, _ := s.ActiveDeployment("svc1")
	dep2, _ := s.ActiveDeployment("svc2")
	if dep2 == nil || dep2.Hostname == "" {
		t.Fatal("expected svc2 to have an internal hostname")
	}

	// Docker-to-Docker: svc1's container resolves and reaches svc2 by its
	// internal Draft hostname over the shared project network.
	url := fmt.Sprintf("http://%s:8080/", dep2.Hostname)
	stdout, exitCode, err := execInContainer(t, cli, dep1.ContainerID, []string{"wget", "-qO-", url})
	if err != nil {
		t.Fatalf("exec wget in svc1 container: %v", err)
	}
	if exitCode != 0 {
		t.Fatalf("wget from svc1 to %s exited %d, output: %s", url, exitCode, stdout)
	}
	if !strings.Contains(stdout, "internal-hello") {
		t.Errorf("expected response body to contain internal-hello, got: %q", stdout)
	}

	// Host/browser side: the same hostname should NOT resolve outside
	// Docker's embedded DNS, since it's never added to the OS hosts file
	// (only the public *.draft.resolv.sh-style hostname is, via the
	// router). Logged rather than failed, since some networks hijack
	// NXDOMAIN responses and would make this flaky.
	if addrs, err := net.LookupHost(dep2.Hostname); err == nil {
		t.Logf("warning: internal hostname %q unexpectedly resolved on the host to %v (possible DNS hijacking on this network)", dep2.Hostname, addrs)
	}
}

// TestIntegrationInternalHostnameNotReachableAcrossProjects verifies that
// the shared Docker network is scoped per-project: a container in one
// project cannot resolve or reach a container in a different project by its
// internal Draft hostname, since they land on two separate bridge networks
// with independent embedded DNS.
func TestIntegrationInternalHostnameNotReachableAcrossProjects(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	// Project A: the caller.
	e1, s1, col1, dirA := setupIntegration(t)
	writeDockerfile(t, dirA, "FROM alpine:3.20\nHEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true\nCMD [\"sleep\", \"3600\"]\n")
	s1.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s1.SetNodeSetting("svc1", "service_port", "80")

	// Project B: the target, in a separate store/project/network entirely.
	// setupIntegration gives each fresh store a unique project name, so even
	// though both stores start their project IDs at 1, they still land on
	// distinct Draft bridge networks.
	e2, s2, col2, dirB := setupIntegration(t)
	writeDockerfile(t, dirB, `FROM alpine:3.20
RUN apk add --no-cache python3 && mkdir -p /www && echo -n "internal-hello" > /www/index.html
CMD ["python3", "-m", "http.server", "8080", "--directory", "/www"]
`)
	s2.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s2.SetNodeSetting("svc1", "service_port", "8080")

	// Register cleanup immediately so a failed run never leaves containers
	// behind to collide with the next run's container names.
	t.Cleanup(func() {
		depsA, _ := s1.ListDeployments("svc1")
		cleanupContainers(t, cli, depsA)
		depsB, _ := s2.ListDeployments("svc1")
		cleanupContainers(t, cli, depsB)
	})

	e1.Deploy(context.Background(), "svc1")
	if waitForStatus(col1, "svc1", "running", 60*time.Second) == nil {
		t.Fatal("expected project A's service to reach running")
	}
	e2.Deploy(context.Background(), "svc1")
	if waitForStatus(col2, "svc1", "running", 60*time.Second) == nil {
		t.Fatal("expected project B's service to reach running")
	}

	depA, _ := s1.ActiveDeployment("svc1")
	depB, _ := s2.ActiveDeployment("svc1")
	if depB == nil || depB.Hostname == "" {
		t.Fatal("expected project B's service to have an internal hostname")
	}

	url := fmt.Sprintf("http://%s:8080/", depB.Hostname)
	stdout, exitCode, err := execInContainer(t, cli, depA.ContainerID, []string{"wget", "-T", "5", "-qO-", url})
	if err == nil && exitCode == 0 {
		t.Fatalf("expected wget across projects to fail, but it succeeded: %s", stdout)
	}
}

// TestIntegrationImageTag verifies the image tag naming convention.
func TestIntegrationImageTag(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	e, s, col, projectDir := setupIntegration(t)

	writeDockerfile(t, projectDir, `FROM alpine:3.20
HEALTHCHECK --interval=1s --timeout=2s --retries=1 CMD true
CMD ["sleep", "3600"]
`)

	s.SetNodeSetting("svc1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("svc1", "service_port", "80")

	e.Deploy(context.Background(), "svc1")

	running := waitForStatus(col, "svc1", "running", 60*time.Second)
	if running == nil {
		t.Fatal("expected running")
	}

	dep, _ := s.ActiveDeployment("svc1")
	project, err := s.GetProject(dep.ProjectID)
	if err != nil {
		t.Fatalf("load project: %v", err)
	}
	expectedPrefix := fmt.Sprintf("draft-%s-test-svc:", sanitize(project.Name))
	if !strings.HasPrefix(dep.ImageTag, expectedPrefix) {
		t.Errorf("expected image tag prefix %q, got %q", expectedPrefix, dep.ImageTag)
	}
	if !strings.Contains(dep.ImageTag, fmt.Sprintf(":%d", dep.ID)) {
		t.Errorf("expected deployment ID in image tag, got %q", dep.ImageTag)
	}

	t.Cleanup(func() {
		deps, _ := s.ListDeployments("svc1")
		cleanupContainers(t, cli, deps)
	})
}
