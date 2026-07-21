//go:build integration

package deploy

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Draft/internal/networking"
	"Draft/internal/store"
)

func setupSubmoduleDeploy(t *testing.T, parentDir string) (*Engine, *store.Store, *eventCollector, string) {
	t.Helper()
	cli := requireDocker(t)
	t.Cleanup(func() { _ = cli.Close() })

	s := openTestStore(t)
	r := networking.NewRouter(s, "127.0.0.1:0")
	if err := r.Start(); err != nil {
		t.Fatalf("start router: %v", err)
	}
	t.Cleanup(func() { r.Stop() })

	logDir := filepath.Join(t.TempDir(), "logs")
	col := &eventCollector{}
	e := New(s, r, logDir, col.emit)

	projectName := integrationProjectName("submod")
	project, err := s.CreateProject(projectName, parentDir, "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	envID := defaultEnvID(t, s, project.ID)
	nodeID := "submod-svc"
	if _, err := s.CreateNode(&store.CanvasNode{
		ID:            nodeID,
		ProjectID:     project.ID,
		EnvironmentID: envID,
		Label:         "submod-svc",
	}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	_ = s.SetNodeSetting(nodeID, "dockerfile", "Dockerfile")
	_ = s.SetNodeSetting(nodeID, "service_port", "8080")
	_ = s.SetNodeSetting(nodeID, "git_branch", "main")
	_ = s.SetNodeSetting(nodeID, "use_buildkit_local_context", "false")

	t.Cleanup(func() {
		deps, _ := s.ListDeployments(nodeID)
		cleanupContainers(t, cli, deps)
		cleanupProjectResources(t, cli, s, project.ID, project.Name)
	})

	return e, s, col, nodeID
}

func collectBuildLog(col *eventCollector, nodeID string) string {
	var b strings.Builder
	for _, ev := range col.get() {
		if ev.Name != "build:log" {
			continue
		}
		m, ok := ev.Data.(map[string]any)
		if !ok || m["nodeId"] != nodeID {
			continue
		}
		switch data := m["line"].(type) {
		case LogLine:
			b.WriteString(data.Line)
			b.WriteByte('\n')
		case string:
			b.WriteString(data)
			b.WriteByte('\n')
		}
	}
	return b.String()
}

func assertBuiltWithSubmodules(t *testing.T, e *Engine, s *store.Store, col *eventCollector, nodeID, wantLogSnippet string) {
	t.Helper()
	built := waitForStatus(col, nodeID, "built", 120*time.Second)
	if built == nil {
		fail := col.findStatus(nodeID, "failed")
		msg := "no events"
		if fail != nil {
			msg = fail.Data.(StatusEvent).Error
		}
		t.Fatalf("expected built, got: %s\nlog:\n%s", msg, collectBuildLog(col, nodeID))
	}

	// Prefer Draft emit logs (submodule mode messages). Docker JSON from GetBuildLog
	// is still useful as proof the image build ran.
	draftLog := collectBuildLog(col, nodeID)
	dockerLog, _ := e.GetBuildLog(built.DeploymentID)
	combined := draftLog + "\n" + dockerLog

	if wantLogSnippet != "" && !strings.Contains(combined, wantLogSnippet) {
		t.Fatalf("expected %q in build log:\n--- draft ---\n%s\n--- docker ---\n%s", wantLogSnippet, draftLog, dockerLog)
	}
	// Hard proof: Docker actually COPYed and verified the submodule marker.
	if !strings.Contains(dockerLog, "COPY vendor/lib/marker.txt") {
		t.Fatalf("expected Dockerfile COPY of submodule marker in docker log:\n%s", dockerLog)
	}
	if !strings.Contains(dockerLog, "Successfully built") && !strings.Contains(dockerLog, "Successfully tagged") {
		t.Fatalf("expected successful docker build:\n%s", dockerLog)
	}

	running := waitForStatus(col, nodeID, "running", 45*time.Second)
	if running == nil {
		// CMD ["true"] may exit quickly; built is the critical gate for submodule inclusion.
		t.Logf("note: running status not observed (container may have exited); built succeeded")
	}
	_ = s
}

// TestSubmoduleDeploy_StreamSplice builds from a pinned branch with local
// submodule objects via the stream path. The Dockerfile COPYs the submodule
// marker — so an empty gitlink placeholder would fail the build.
func TestSubmoduleDeploy_StreamSplice(t *testing.T) {
	parent, _ := submoduleFixture(t)
	e, s, col, nodeID := setupSubmoduleDeploy(t, parent)
	_ = s.SetNodeSetting(nodeID, "git_stream", "true")

	// Poison working tree so a mistaken WT build would fail the Dockerfile check.
	_ = os.WriteFile(filepath.Join(parent, "vendor", "lib", "marker.txt"), []byte("DIRTY\n"), 0o644)

	e.Deploy(context.Background(), nodeID)
	assertBuiltWithSubmodules(t, e, s, col, nodeID, "splicing from local")
}

// TestSubmoduleDeploy_WorktreeFallback removes the modules cache so stream
// cannot splice, forcing worktree + submodule update, then verifies the build.
func TestSubmoduleDeploy_WorktreeFallback(t *testing.T) {
	parent, _ := submoduleFixture(t)
	removeModulesCache(t, parent)

	e, s, col, nodeID := setupSubmoduleDeploy(t, parent)
	_ = s.SetNodeSetting(nodeID, "git_stream", "true") // prefer stream; must fall back

	e.Deploy(context.Background(), nodeID)
	assertBuiltWithSubmodules(t, e, s, col, nodeID, "worktree")
}

// TestSubmoduleDeploy_CheckoutMaterialize forces git_stream=false with local
// modules and verifies materialize + build.
func TestSubmoduleDeploy_CheckoutMaterialize(t *testing.T) {
	parent, _ := submoduleFixture(t)
	e, s, col, nodeID := setupSubmoduleDeploy(t, parent)
	_ = s.SetNodeSetting(nodeID, "git_stream", "false")

	e.Deploy(context.Background(), nodeID)
	assertBuiltWithSubmodules(t, e, s, col, nodeID, "Expanding")
}
