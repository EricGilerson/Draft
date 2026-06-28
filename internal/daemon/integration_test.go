package daemon

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"Draft/internal/deploy"
	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/image"
	"github.com/docker/docker/client"
)

func requireDocker(t *testing.T) *client.Client {
	t.Helper()
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("Docker client unavailable: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := cli.Ping(ctx); err != nil {
		cli.Close()
		t.Skipf("Docker daemon unavailable: %v", err)
	}
	return cli
}

func TestIntegrationDaemonDeploysThroughClient(t *testing.T) {
	cli := requireDocker(t)
	defer cli.Close()

	cfgDir := t.TempDir()
	oldConfigDir := userConfigDir
	userConfigDir = func() (string, error) { return cfgDir, nil }
	t.Cleanup(func() { userConfigDir = oldConfigDir })

	projectDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(projectDir, "Dockerfile"), []byte(`FROM alpine:3.20
CMD ["sleep", "3600"]
`), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := store.Open(store.FileDSN(filepath.Join(t.TempDir(), "draft.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	project, err := s.CreateProject("Daemon Integration", projectDir, "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "svc1", ProjectID: project.ID, Label: "web"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeSetting("svc1", "dockerfile", "Dockerfile"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeSetting("svc1", "service_port", "80"); err != nil {
		t.Fatal(err)
	}

	router := networking.NewRouter(s, "127.0.0.1:0")
	if err := router.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = router.Stop() })

	srv, _, logDir := newTestServer(t)
	srv.store = s
	srv.router = router
	srv.engine = deploy.New(s, router, logDir, srv.hub.publish)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	c := waitForClient(t)
	if err := c.Deploy(context.Background(), "svc1"); err != nil {
		t.Fatal(err)
	}

	dep := waitForDeploymentStatus(t, s, "svc1", "running", 45*time.Second)
	if dep.ContainerID == "" || dep.HostPort == 0 {
		t.Fatalf("running deployment missing runtime fields: %+v", dep)
	}
	log, err := c.GetBuildLog(context.Background(), dep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if log == "" {
		t.Fatal("expected persisted build log")
	}

	t.Cleanup(func() {
		timeout := 1
		_ = cli.ContainerStop(context.Background(), dep.ContainerID, container.StopOptions{Timeout: &timeout})
		_ = cli.ContainerRemove(context.Background(), dep.ContainerID, container.RemoveOptions{Force: true})
		if dep.ImageTag != "" {
			_, _ = cli.ImageRemove(context.Background(), dep.ImageTag, image.RemoveOptions{Force: true})
		}
	})

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server run returned error: %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("server did not shut down")
	}
}

func waitForClient(t *testing.T) *Client {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		c, err := NewClientFromState()
		if err == nil && c.Ping(context.Background()) == nil {
			return c
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("daemon client did not become ready")
	return nil
}

func waitForDeploymentStatus(t *testing.T, s *store.Store, nodeID, status string, timeout time.Duration) *store.Deployment {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var last *store.Deployment
	for time.Now().Before(deadline) {
		deps, err := s.ListDeployments(nodeID)
		if err == nil && len(deps) > 0 {
			last = &deps[0]
			if deps[0].Status == status {
				return &deps[0]
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("deployment did not reach %s, last=%+v", status, last)
	return nil
}
