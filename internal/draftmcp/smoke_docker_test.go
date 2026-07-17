package draftmcp

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"Draft/internal/daemon"
	"Draft/internal/store"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/filters"
	"github.com/docker/docker/api/types/image"
	dockernetwork "github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
)

// Live Docker smoke: blank service → stage alpine image → deploy → running → stop.
// Skips when Docker is unavailable.
func TestMCPDockerSmokeBlankServiceDeploy(t *testing.T) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		t.Skipf("Docker client unavailable: %v", err)
	}
	defer cli.Close()
	pingCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := cli.Ping(pingCtx); err != nil {
		t.Skipf("Docker daemon unavailable: %v", err)
	}

	c, st := daemon.NewDockerTestClient(t)
	SetTestClient(c)
	t.Cleanup(func() {
		SetTestClient(nil)
		clearSessionContext()
	})

	root := t.TempDir()
	name := fmt.Sprintf("mcp-smoke-%08x", uint32(time.Now().UnixNano()))

	var project store.Project
	mustCallToolJSON(t, "draft_create_project", map[string]any{
		"name": name,
		"path": filepath.Join(root, name),
	}, &project)

	var envs []store.Environment
	mustCallToolJSON(t, "draft_list_environments", map[string]any{"projectId": float64(project.ID)}, &envs)
	if len(envs) == 0 {
		t.Fatal("no default environment")
	}
	env := envs[0]

	mustCallToolJSON(t, "draft_set_context", map[string]any{
		"projectId":     float64(project.ID),
		"environmentId": float64(env.ID),
	}, &sessionContext{})

	var node store.CanvasNode
	mustCallToolJSON(t, "draft_create_blank_service", map[string]any{
		"label": "sleeper",
	}, &node)
	if node.ID == "" {
		t.Fatal("blank service missing id")
	}

	t.Cleanup(func() {
		deps, _ := st.ListDeployments(node.ID)
		for _, dep := range deps {
			if dep.ContainerID != "" {
				timeout := 1
				_ = cli.ContainerStop(context.Background(), dep.ContainerID, container.StopOptions{Timeout: &timeout})
				_ = cli.ContainerRemove(context.Background(), dep.ContainerID, container.RemoveOptions{Force: true})
			}
			if dep.ImageTag != "" {
				_, _ = cli.ImageRemove(context.Background(), dep.ImageTag, image.RemoveOptions{Force: true})
			}
		}
		networks, err := cli.NetworkList(context.Background(), dockernetwork.ListOptions{
			Filters: filters.NewArgs(
				filters.Arg("label", "draft.managed=true"),
				filters.Arg("label", fmt.Sprintf("draft.project=%d", project.ID)),
			),
		})
		if err == nil {
			for _, n := range networks {
				_ = cli.NetworkRemove(context.Background(), n.ID)
			}
		}
	})

	_, text, isErr := callToolResult(t, "draft_stage_service_settings", map[string]any{
		"nodeId":    node.ID,
		"projectId": float64(project.ID),
		"settings": map[string]any{
			"image":        "alpine:3.20",
			"service_port": "80",
			"cmd_override": "sleep 3600",
		},
	})
	if isErr {
		t.Fatalf("stage settings: %s", text)
	}

	_, text, isErr = callToolResult(t, "draft_deploy_service", map[string]any{"nodeId": node.ID})
	if isErr {
		t.Fatalf("deploy: %s", text)
	}

	dep := waitMCPDeploymentStatus(t, st, node.ID, "running", 90*time.Second)
	if dep.ContainerID == "" {
		t.Fatalf("running deployment missing container: %+v", dep)
	}

	var health map[string]any
	mustCallToolJSON(t, "draft_service_health", map[string]any{"nodeId": node.ID}, &health)
	if health["status"] != "running" && health["status"] != "starting" {
		t.Fatalf("unexpected health status: %#v", health)
	}

	_, text, isErr = callToolResult(t, "draft_stop_service", map[string]any{"nodeId": node.ID})
	if isErr {
		t.Fatalf("stop: %s", text)
	}
	_ = waitMCPDeploymentStatus(t, st, node.ID, "stopped", 30*time.Second)
}

func waitMCPDeploymentStatus(t *testing.T, s *store.Store, nodeID, status string, timeout time.Duration) *store.Deployment {
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
			if deps[0].Status == "failed" {
				logPath := ""
				_ = logPath
				t.Fatalf("deployment failed: %+v", deps[0])
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	t.Fatalf("deployment did not reach %s, last=%+v (cwd=%s)", status, last, mustGetwd())
	return nil
}

func mustGetwd() string {
	wd, _ := os.Getwd()
	return wd
}
