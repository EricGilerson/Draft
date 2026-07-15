package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"Draft/internal/draftpack"
	"Draft/internal/store"
)

// Engine-level tests: same entrypoints the daemon/Wails UI call for pack
// export/import (including file I/O).

func TestEngineServicePackIntoProjectCreatesService(t *testing.T) {
	s, err := store.Open(store.FileDSN(filepath.Join(t.TempDir(), "draft.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	srcRoot := t.TempDir()
	dstRoot := t.TempDir()
	_ = os.MkdirAll(filepath.Join(srcRoot, "web"), 0o755)
	_ = os.MkdirAll(filepath.Join(dstRoot, "web"), 0o755)

	srcProj, err := s.CreateProject("from", srcRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	srcEnv, err := s.GetDefaultEnvironment(srcProj.ID)
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateNode(&store.CanvasNode{
		ID: "svc-web1", Label: "web", ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID, X: 40, Y: 60,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting(node.ID, "dockerfile", "Dockerfile")
	_ = s.SetNodeSetting(node.ID, "service_port", "5173")
	if err := s.SetServiceRoot(node.ID, srcProj.ID, filepath.Join(srcRoot, "web")); err != nil {
		t.Fatal(err)
	}
	_ = s.UpsertEnvVar(store.EnvVar{
		NodeID: node.ID, Key: "VITE_API", Value: "http://localhost",
		Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
	})

	e := New(s, nil, t.TempDir(), nil)

	// Export via engine
	exp, err := e.ExportDraftPackService(node.ID, DefaultDraftPackExportOptions())
	if err != nil {
		t.Fatal(err)
	}
	if exp.JSON == "" || exp.FileName == "" {
		t.Fatalf("export incomplete: %+v", exp)
	}

	// Write pack file (UI SaveDraftPackFile + ExportDraftPackToPath)
	packPath := filepath.Join(t.TempDir(), exp.FileName)
	out, err := e.ExportDraftPackToPath(draftpack.ScopeService, node.ID, 0, 0, DefaultDraftPackExportOptions(), packPath)
	if err != nil {
		t.Fatal(err)
	}
	if out.Path == "" {
		t.Fatal("expected path written")
	}
	if _, err := os.Stat(out.Path); err != nil {
		t.Fatal(err)
	}

	// Destination project
	dstProj, err := s.CreateProject("to", dstRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	dstEnv, err := s.GetDefaultEnvironment(dstProj.ID)
	if err != nil {
		t.Fatal(err)
	}

	// Preview (ImportDraftPackDialog step 1)
	prev, err := e.PreviewDraftPackImport(out.Path, draftpack.PreviewOptions{
		Mode: draftpack.ImportIntoProject, ProjectID: dstProj.ID, EnvironmentID: dstEnv.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prev.PackScope != draftpack.ScopeService || len(prev.Services) != 1 {
		t.Fatalf("preview=%+v", prev)
	}
	if prev.Services[0].Label != "web" {
		t.Fatalf("label=%s", prev.Services[0].Label)
	}

	// Import into current project/env (canvas path)
	res, err := e.ImportDraftPack(out.Path, draftpack.ImportOptions{
		Mode:          draftpack.ImportIntoProject,
		ProjectID:     dstProj.ID,
		EnvironmentID: dstEnv.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectID != dstProj.ID || len(res.NodeIDs) != 1 {
		t.Fatalf("result=%+v", res)
	}

	// Service actually exists in the destination environment
	nodes, err := s.ListNodesByEnvironment(dstEnv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("nodes in dest env=%d", len(nodes))
	}
	got := nodes[0]
	if got.Label != "web" || got.ID == node.ID {
		t.Fatalf("got node=%+v", got)
	}
	if got.UID == "" {
		t.Fatal("missing UID")
	}
	if got.X != 40 || got.Y != 60 {
		t.Fatalf("layout x=%v y=%v", got.X, got.Y)
	}

	settings, err := s.EffectiveNodeSettings(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if settings["service_port"] != "5173" || settings["dockerfile"] != "Dockerfile" {
		t.Fatalf("settings=%v", settings)
	}
	if draftpack.NormalizeRelative(settings["service_root"]) != "web" {
		t.Fatalf("service_root=%q", settings["service_root"])
	}

	envs, err := s.ListEnvVars(got.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(envs) != 1 || envs[0].Key != "VITE_API" || envs[0].Value != "http://localhost" {
		t.Fatalf("env=%+v", envs)
	}
}

func TestEngineNewProjectFromEnvironmentPack(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	srcRoot := t.TempDir()
	srcProj, _ := s.CreateProject("stack", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	for _, label := range []string{"api", "db"} {
		n, err := s.CreateNode(&store.CanvasNode{
			ID: "n-" + label, Label: label, ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
		})
		if err != nil {
			t.Fatal(err)
		}
		_ = s.SetNodeSetting(n.ID, "image", "img:"+label)
		_ = s.SetNodeSetting(n.ID, "service_port", "80")
	}

	e := New(s, nil, t.TempDir(), nil)
	exp, err := e.ExportDraftPackEnvironment(srcEnv.ID, DefaultDraftPackExportOptions())
	if err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	written, err := e.ExportDraftPackToPath(draftpack.ScopeEnvironment, "", 0, srcEnv.ID, DefaultDraftPackExportOptions(), dir)
	if err != nil {
		t.Fatal(err)
	}
	if written.Path == "" {
		// Engine may write fileName into dir
		t.Fatalf("path empty; exp fileName=%s", exp.FileName)
	}

	dest := t.TempDir()
	res, err := e.ImportDraftPack(written.Path, draftpack.ImportOptions{
		Mode:        draftpack.ImportAsNewProject,
		ProjectName: "stack-copy",
		ProjectPath: dest,
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.ProjectID == srcProj.ID {
		t.Fatal("should create new project")
	}
	nodes, err := s.ListNodesByEnvironment(res.EnvironmentIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 2 {
		t.Fatalf("nodes=%d want 2", len(nodes))
	}
}
