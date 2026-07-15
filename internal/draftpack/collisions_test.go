package draftpack

import (
	"os"
	"path/filepath"
	"testing"

	"Draft/internal/store"
)

func TestPreviewDetectsServiceLabelCollisionAndImportRenames(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	proj, err := s.CreateProject("p", root, "")
	if err != nil {
		t.Fatal(err)
	}
	env, _ := s.GetDefaultEnvironment(proj.ID)
	// Existing service
	if _, err := s.CreateNode(&store.CanvasNode{
		ID: "exist", Label: "api", ProjectID: proj.ID, EnvironmentID: env.ID,
	}); err != nil {
		t.Fatal(err)
	}

	// Pack for another "api"
	srcRoot := t.TempDir()
	srcProj, _ := s.CreateProject("src", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	srcNode, _ := s.CreateNode(&store.CanvasNode{
		ID: "src", Label: "api", ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
	})
	_ = s.SetNodeSetting(srcNode.ID, "image", "nginx")
	_ = s.SetNodeSetting(srcNode.ID, "service_port", "80")

	pack, err := (&Exporter{Store: s}).ExportService(srcNode.ID, DefaultExportOptions())
	if err != nil {
		t.Fatal(err)
	}

	imp := &Importer{Store: s}
	prev, err := imp.Preview(pack, PreviewOptions{
		Mode: ImportIntoProject, ProjectID: proj.ID, EnvironmentID: env.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(prev.Collisions) == 0 {
		t.Fatal("expected service_label collision")
	}
	var labelCol *Collision
	for i := range prev.Collisions {
		if prev.Collisions[i].Kind == CollisionServiceLabel {
			labelCol = &prev.Collisions[i]
			break
		}
	}
	if labelCol == nil {
		t.Fatalf("collisions=%+v", prev.Collisions)
	}
	if labelCol.Suggested == "" || labelCol.Suggested == "api" {
		t.Fatalf("suggested=%q", labelCol.Suggested)
	}

	// Import without override — should auto-rename via applyCollisionDefaults
	res, err := imp.Import(pack, ImportOptions{
		Mode: ImportIntoProject, ProjectID: proj.ID, EnvironmentID: env.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.GetNode(res.NodeIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if node.Label == "api" {
		t.Fatal("should not keep colliding label")
	}
	// Original still there
	if _, err := s.GetNodeByLabel(env.ID, "api"); err != nil {
		t.Fatal("original api should remain")
	}
}

func TestPreviewDetectsProjectNameCollision(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	if _, err := s.CreateProject("demo", root, ""); err != nil {
		t.Fatal(err)
	}

	srcRoot := t.TempDir()
	srcProj, _ := s.CreateProject("other", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	n, _ := s.CreateNode(&store.CanvasNode{
		ID: "n1", Label: "x", ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
	})
	_ = s.SetNodeSetting(n.ID, "image", "x")
	_ = s.SetNodeSetting(n.ID, "service_port", "1")
	pack, _ := (&Exporter{Store: s}).ExportService(n.ID, DefaultExportOptions())
	// Force project name in pack
	pack.Project = &ProjectPayload{Name: "demo"}

	imp := &Importer{Store: s}
	prev, err := imp.Preview(pack, PreviewOptions{
		Mode: ImportAsNewProject, ProjectName: "demo",
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range prev.Collisions {
		if c.Kind == CollisionProjectName {
			found = true
			if c.Suggested != "demo-2" && c.Suggested == "demo" {
				t.Fatalf("suggested=%q", c.Suggested)
			}
		}
	}
	if !found {
		t.Fatalf("expected project_name collision, got %+v", prev.Collisions)
	}

	// Import auto-renames project
	dest := t.TempDir()
	res, err := imp.Import(pack, ImportOptions{
		Mode: ImportAsNewProject, ProjectName: "demo", ProjectPath: dest,
	})
	if err != nil {
		t.Fatal(err)
	}
	p, err := s.GetProject(res.ProjectID)
	if err != nil {
		t.Fatal(err)
	}
	if p.Name == "demo" {
		t.Fatal("should have renamed project")
	}
}

func TestPreviewDetectsProjectPathCollision(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	if _, err := s.CreateProject("existing", root, ""); err != nil {
		t.Fatal(err)
	}
	srcRoot := t.TempDir()
	srcProj, _ := s.CreateProject("src", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	n, _ := s.CreateNode(&store.CanvasNode{
		ID: "n", Label: "s", ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
	})
	_ = s.SetNodeSetting(n.ID, "image", "i")
	_ = s.SetNodeSetting(n.ID, "service_port", "1")
	pack, _ := (&Exporter{Store: s}).ExportService(n.ID, DefaultExportOptions())

	imp := &Importer{Store: s}
	prev, err := imp.Preview(pack, PreviewOptions{
		Mode: ImportAsNewProject, ProjectName: "fresh", ProjectPath: root,
	})
	if err != nil {
		t.Fatal(err)
	}
	if !prev.HasBlockingCollision {
		t.Fatal("path collision should be blocking")
	}
	// Import must fail
	_, err = imp.Import(pack, ImportOptions{
		Mode: ImportAsNewProject, ProjectName: "fresh", ProjectPath: root,
	})
	if err == nil {
		t.Fatal("expected error for path collision")
	}
}

func TestUserLabelOverrideApplied(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	proj, _ := s.CreateProject("p", root, "")
	env, _ := s.GetDefaultEnvironment(proj.ID)
	_, _ = s.CreateNode(&store.CanvasNode{
		ID: "exist", Label: "api", ProjectID: proj.ID, EnvironmentID: env.ID,
	})

	srcRoot := t.TempDir()
	srcProj, _ := s.CreateProject("src", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	srcNode, _ := s.CreateNode(&store.CanvasNode{
		ID: "src", Label: "api", ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
	})
	_ = s.SetNodeSetting(srcNode.ID, "image", "nginx")
	_ = s.SetNodeSetting(srcNode.ID, "service_port", "80")
	pack, _ := (&Exporter{Store: s}).ExportService(srcNode.ID, DefaultExportOptions())

	res, err := (&Importer{Store: s}).Import(pack, ImportOptions{
		Mode: ImportIntoProject, ProjectID: proj.ID, EnvironmentID: env.ID,
		ServiceLabelOverrides: map[string]string{
			pack.Services[0].Key: "api-from-pack",
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	node, _ := s.GetNode(res.NodeIDs[0])
	if node.Label != "api-from-pack" {
		t.Fatalf("label=%q", node.Label)
	}
}

func TestHostPortCollisionClearedOnImport(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	proj, _ := s.CreateProject("p", root, "")
	env, _ := s.GetDefaultEnvironment(proj.ID)
	exist, _ := s.CreateNode(&store.CanvasNode{
		ID: "exist", Label: "db", ProjectID: proj.ID, EnvironmentID: env.ID,
	})
	_ = s.SetNodeSetting(exist.ID, "host_port", "5432")
	_ = s.SetNodeSetting(exist.ID, "image", "postgres")
	_ = s.SetNodeSetting(exist.ID, "service_port", "5432")

	srcRoot := t.TempDir()
	srcProj, _ := s.CreateProject("src", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	srcNode, _ := s.CreateNode(&store.CanvasNode{
		ID: "src", Label: "pg", ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
	})
	_ = s.SetNodeSetting(srcNode.ID, "image", "postgres")
	_ = s.SetNodeSetting(srcNode.ID, "service_port", "5432")
	_ = s.SetNodeSetting(srcNode.ID, "host_port", "5432")
	pack, _ := (&Exporter{Store: s}).ExportService(srcNode.ID, DefaultExportOptions())

	imp := &Importer{Store: s}
	prev, err := imp.Preview(pack, PreviewOptions{
		Mode: ImportIntoProject, ProjectID: proj.ID, EnvironmentID: env.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	foundPort := false
	for _, c := range prev.Collisions {
		if c.Kind == CollisionHostPort {
			foundPort = true
		}
	}
	if !foundPort {
		t.Fatalf("expected host_port collision, got %+v", prev.Collisions)
	}

	res, err := imp.Import(pack, ImportOptions{
		Mode: ImportIntoProject, ProjectID: proj.ID, EnvironmentID: env.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	settings, _ := s.GetNodeSettings(res.NodeIDs[0])
	if settings["host_port"] == "5432" {
		t.Fatal("colliding host_port should have been cleared")
	}
	_ = os.RemoveAll(filepath.Join(root, "unused"))
}

func TestHostPortOverrideClearsCollisionOnPreview(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	proj, _ := s.CreateProject("p", root, "")
	env, _ := s.GetDefaultEnvironment(proj.ID)
	exist, _ := s.CreateNode(&store.CanvasNode{
		ID: "exist", Label: "db", ProjectID: proj.ID, EnvironmentID: env.ID,
	})
	_ = s.SetNodeSetting(exist.ID, "host_port", "5432")

	srcRoot := t.TempDir()
	srcProj, _ := s.CreateProject("src", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	srcNode, _ := s.CreateNode(&store.CanvasNode{
		ID: "src", Label: "pg", ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
	})
	_ = s.SetNodeSetting(srcNode.ID, "image", "postgres")
	_ = s.SetNodeSetting(srcNode.ID, "service_port", "5432")
	_ = s.SetNodeSetting(srcNode.ID, "host_port", "5432")
	pack, _ := (&Exporter{Store: s}).ExportService(srcNode.ID, DefaultExportOptions())

	imp := &Importer{Store: s}
	// User cleared the port in the UI — re-preview should not report host_port.
	prev, err := imp.Preview(pack, PreviewOptions{
		Mode: ImportIntoProject, ProjectID: proj.ID, EnvironmentID: env.ID,
		HostPortOverrides: map[string]string{pack.Services[0].Key: ""},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range prev.Collisions {
		if c.Kind == CollisionHostPort {
			t.Fatalf("cleared port should not collide: %+v", c)
		}
	}
}

func TestLabelOverrideClearsCollisionOnPreview(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	proj, _ := s.CreateProject("p", root, "")
	env, _ := s.GetDefaultEnvironment(proj.ID)
	_, _ = s.CreateNode(&store.CanvasNode{
		ID: "exist", Label: "api", ProjectID: proj.ID, EnvironmentID: env.ID,
	})

	srcRoot := t.TempDir()
	srcProj, _ := s.CreateProject("src", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	srcNode, _ := s.CreateNode(&store.CanvasNode{
		ID: "src", Label: "api", ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
	})
	_ = s.SetNodeSetting(srcNode.ID, "image", "nginx")
	_ = s.SetNodeSetting(srcNode.ID, "service_port", "80")
	pack, _ := (&Exporter{Store: s}).ExportService(srcNode.ID, DefaultExportOptions())

	imp := &Importer{Store: s}
	prev, err := imp.Preview(pack, PreviewOptions{
		Mode: ImportIntoProject, ProjectID: proj.ID, EnvironmentID: env.ID,
		ServiceLabelOverrides: map[string]string{pack.Services[0].Key: "api-from-pack"},
	})
	if err != nil {
		t.Fatal(err)
	}
	for _, c := range prev.Collisions {
		if c.Kind == CollisionServiceLabel {
			t.Fatalf("renamed label should not collide: %+v", c)
		}
	}
}

func TestWithinPackHostPortCollision(t *testing.T) {
	// Two pack services claiming the same fixed host port.
	pack := &Pack{
		Version: CurrentVersion,
		Scope:   ScopeEnvironment,
		Services: []ServicePayload{
			{Key: "a", Label: "svc-a", Settings: map[string]string{"host_port": "9000", "service_port": "80"}},
			{Key: "b", Label: "svc-b", Settings: map[string]string{"host_port": "9000", "service_port": "80"}},
		},
	}
	imp := &Importer{Store: testStore(t)}
	prev, err := imp.Preview(pack, PreviewOptions{Mode: ImportAsNewProject, ProjectName: "x"})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, c := range prev.Collisions {
		if c.Kind == CollisionHostPort {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected within-pack host_port collision, got %+v", prev.Collisions)
	}
}
