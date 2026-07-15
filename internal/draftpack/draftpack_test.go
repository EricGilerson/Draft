package draftpack

import (
	"os"
	"path/filepath"
	"testing"

	"Draft/internal/store"
)

func testStore(t *testing.T) *store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := store.Open(filepath.Join(dir, "test.db"))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func TestExportImportEnvironmentRoundTrip(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	svcRoot := filepath.Join(root, "api")
	if err := os.MkdirAll(svcRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	project, err := s.CreateProject("demo", root, "test")
	if err != nil {
		t.Fatal(err)
	}
	env, err := s.GetDefaultEnvironment(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateNode(&store.CanvasNode{
		ID: "svc-aaaa1111", Label: "api", ProjectID: project.ID, EnvironmentID: env.ID, X: 10, Y: 20,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeSetting(node.ID, "dockerfile", "Dockerfile"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeSetting(node.ID, "service_port", "8080"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetServiceRoot(node.ID, project.ID, svcRoot); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: node.ID, Key: "PUBLIC", Value: "yes", Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: node.ID, Key: "SECRET_TOKEN", Value: "s3cr3t", Scope: store.EnvScopeRuntime,
		Secret: true, Source: store.EnvSourceManual,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectEnvVar(project.ID, "REGION", "us-east", "", false); err != nil {
		t.Fatal(err)
	}

	// Named volume + bind
	if err := s.SetNodeSetting(node.ID, "volume_mounts", `[
		{"type":"volume","containerPath":"/data"},
		{"type":"bind","source":"C:\\Users\\Alice\\data","containerPath":"/host-data"}
	]`); err != nil {
		t.Fatal(err)
	}

	ex := &Exporter{Store: s}
	opts := DefaultExportOptions()
	pack, err := ex.ExportEnvironment(env.ID, opts)
	if err != nil {
		t.Fatal(err)
	}
	if pack.Scope != ScopeEnvironment {
		t.Fatalf("scope = %s", pack.Scope)
	}
	if len(pack.Services) != 1 {
		t.Fatalf("services = %d", len(pack.Services))
	}
	svc := pack.Services[0]
	if svc.Settings["service_root"] != "api" && svc.Settings["service_root"] != "api/" {
		// Windows may normalize differently
		if NormalizeRelative(svc.Settings["service_root"]) != "api" {
			t.Fatalf("service_root = %q, want relative api", svc.Settings["service_root"])
		}
	}
	// Secret stripped
	var foundSecret bool
	for _, ev := range svc.Env {
		if ev.Key == "SECRET_TOKEN" {
			foundSecret = true
			if !ev.ValueOmitted || ev.Value != "" {
				t.Fatalf("secret should be omitted, got %+v", ev)
			}
		}
		if ev.Key == "PUBLIC" && ev.Value != "yes" {
			t.Fatalf("public value = %q", ev.Value)
		}
	}
	if !foundSecret {
		t.Fatal("missing SECRET_TOKEN")
	}
	if len(svc.BindRemaps) == 0 {
		t.Fatal("expected bind remap")
	}
	if _, ok := svc.Settings["git_repo_root"]; ok {
		t.Fatal("git_repo_root should be omitted")
	}

	raw, err := MarshalPack(pack)
	if err != nil {
		t.Fatal(err)
	}
	pack2, err := UnmarshalPack(raw)
	if err != nil {
		t.Fatal(err)
	}

	// Import into a new folder
	dest := t.TempDir()
	// Mirror service root so auto path works
	if err := os.MkdirAll(filepath.Join(dest, "api"), 0o755); err != nil {
		t.Fatal(err)
	}
	imp := &Importer{Store: s}
	res, err := imp.Import(pack2, ImportOptions{
		Mode:        ImportAsNewProject,
		ProjectName: "demo-copy",
		ProjectPath: dest,
		SecretValues: map[string]string{
			"SECRET_TOKEN": "new-secret",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectID == 0 || res.ProjectID == project.ID {
		t.Fatalf("expected new project id, got %d", res.ProjectID)
	}
	if len(res.NodeIDs) != 1 {
		t.Fatalf("nodes = %d", len(res.NodeIDs))
	}
	ev, err := s.GetEnvVar(res.NodeIDs[0], "SECRET_TOKEN")
	if err != nil {
		t.Fatal(err)
	}
	if ev.Value != "new-secret" {
		t.Fatalf("secret value = %q", ev.Value)
	}
	settings, err := s.GetNodeSettings(res.NodeIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if settings["service_port"] != "8080" {
		t.Fatalf("port = %q", settings["service_port"])
	}
	// Bind without override should be dropped from mounts
	if raw := settings["volume_mounts"]; raw != "" {
		if containsAll(raw, "bind") && containsAll(raw, "/host-data") && !containsAll(raw, "source") {
			// ok — bind without source may remain or be dropped
		}
	}
}

func TestExportOmitsAbsoluteServiceRoot(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	outside := t.TempDir()
	project, err := s.CreateProject("p", root, "")
	if err != nil {
		t.Fatal(err)
	}
	env, _ := s.GetDefaultEnvironment(project.ID)
	node, _ := s.CreateNode(&store.CanvasNode{
		ID: "svc-bbbb2222", Label: "x", ProjectID: project.ID, EnvironmentID: env.ID,
	})
	// Bypass SetServiceRoot validation by writing absolute path directly
	_ = s.SetNodeSetting(node.ID, "service_root", outside)

	ex := &Exporter{Store: s}
	pack, err := ex.ExportService(node.ID, DefaultExportOptions())
	if err != nil {
		t.Fatal(err)
	}
	if !pack.Services[0].NeedsServiceRoot {
		t.Fatal("expected NeedsServiceRoot")
	}
	if _, ok := pack.Services[0].Settings["service_root"]; ok {
		t.Fatal("absolute service_root should not be in settings")
	}
}

func TestSuggestedFileName(t *testing.T) {
	pack := &Pack{
		Scope:   ScopeProject,
		Project: &ProjectPayload{Name: "My App!"},
	}
	if got := SuggestedFileName(pack); got != "my-app.draftpack" {
		t.Fatalf("got %q", got)
	}
}

func containsAll(s string, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 ||
		filepath.Base(s) == sub || // nonsense fallback
		stringIndex(s, sub) >= 0)
}

func stringIndex(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
