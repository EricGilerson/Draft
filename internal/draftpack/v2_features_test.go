package draftpack

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Draft/internal/store"
)

func TestContentHashRoundTripAndTamper(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	proj, _ := s.CreateProject("h", root, "")
	env, _ := s.GetDefaultEnvironment(proj.ID)
	n, _ := s.CreateNode(&store.CanvasNode{
		ID: "n", Label: "api", ProjectID: proj.ID, EnvironmentID: env.ID,
	})
	_ = s.SetNodeSetting(n.ID, "image", "nginx")
	_ = s.SetNodeSetting(n.ID, "service_port", "80")

	pack, err := (&Exporter{Store: s}).ExportService(n.ID, DefaultExportOptions())
	if err != nil {
		t.Fatal(err)
	}
	raw, err := MarshalPack(pack)
	if err != nil {
		t.Fatal(err)
	}
	if pack.ContentHash == "" {
		t.Fatal("expected content hash after marshal")
	}
	loaded, err := UnmarshalPack(raw)
	if err != nil {
		t.Fatal(err)
	}
	if loaded.ContentHash != pack.ContentHash {
		t.Fatalf("hash mismatch on reload")
	}

	// Tamper after seal.
	bad := strings.Replace(string(raw), `"label": "api"`, `"label": "hacked"`, 1)
	if _, err := UnmarshalPack([]byte(bad)); err == nil {
		t.Fatal("expected hash mismatch error")
	}
}

func TestLayoutPreviewReportsOverlapAndShift(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	dst, _ := s.CreateProject("dst", root, "")
	dstEnv, _ := s.GetDefaultEnvironment(dst.ID)
	_, _ = s.CreateNode(&store.CanvasNode{
		ID: "exist", Label: "old", ProjectID: dst.ID, EnvironmentID: dstEnv.ID, X: 100, Y: 100,
	})

	srcRoot := t.TempDir()
	src, _ := s.CreateProject("src", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(src.ID)
	srcNode, _ := s.CreateNode(&store.CanvasNode{
		ID: "src", Label: "api", ProjectID: src.ID, EnvironmentID: srcEnv.ID, X: 100, Y: 100,
	})
	_ = s.SetNodeSetting(srcNode.ID, "image", "nginx")
	_ = s.SetNodeSetting(srcNode.ID, "service_port", "80")
	pack, _ := (&Exporter{Store: s}).ExportService(srcNode.ID, DefaultExportOptions())

	imp := &Importer{Store: s}
	// Preserve: should report overlap
	prev, err := imp.Preview(pack, PreviewOptions{
		Mode: ImportIntoProject, ProjectID: dst.ID, EnvironmentID: dstEnv.ID,
		LayoutMode: LayoutPreserve,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prev.Layout == nil {
		t.Fatal("expected layout preview")
	}
	if prev.Layout.OverlapCount == 0 {
		t.Fatal("preserve mode should detect overlap with existing node")
	}
	if !prev.Layout.WouldOverlapWithoutShift {
		t.Fatal("expected WouldOverlapWithoutShift")
	}

	// Auto: should shift clear
	prev2, err := imp.Preview(pack, PreviewOptions{
		Mode: ImportIntoProject, ProjectID: dst.ID, EnvironmentID: dstEnv.ID,
		LayoutMode: LayoutAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prev2.Layout.OverlapCount != 0 {
		t.Fatalf("auto should clear overlaps, got %d", prev2.Layout.OverlapCount)
	}
	if !prev2.Layout.Shifted {
		t.Fatal("expected Shifted")
	}
	if len(prev2.Layout.Incoming) != 1 || len(prev2.Layout.Existing) != 1 {
		t.Fatalf("incoming=%d existing=%d", len(prev2.Layout.Incoming), len(prev2.Layout.Existing))
	}
}

func TestLayoutAutoOffsetsAwayFromExisting(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	dst, _ := s.CreateProject("dst", root, "")
	dstEnv, _ := s.GetDefaultEnvironment(dst.ID)
	// Existing node at pack coordinates
	_, _ = s.CreateNode(&store.CanvasNode{
		ID: "exist", Label: "old", ProjectID: dst.ID, EnvironmentID: dstEnv.ID, X: 100, Y: 100,
	})

	srcRoot := t.TempDir()
	src, _ := s.CreateProject("src", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(src.ID)
	srcNode, _ := s.CreateNode(&store.CanvasNode{
		ID: "src", Label: "api", ProjectID: src.ID, EnvironmentID: srcEnv.ID, X: 100, Y: 100,
	})
	_ = s.SetNodeSetting(srcNode.ID, "image", "nginx")
	_ = s.SetNodeSetting(srcNode.ID, "service_port", "80")
	pack, _ := (&Exporter{Store: s}).ExportService(srcNode.ID, DefaultExportOptions())

	res, err := (&Importer{Store: s}).Import(pack, ImportOptions{
		Mode: ImportIntoProject, ProjectID: dst.ID, EnvironmentID: dstEnv.ID,
		LayoutMode: LayoutAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetNode(res.NodeIDs[0])
	if got.X == 100 && got.Y == 100 {
		t.Fatalf("expected auto layout to shift off existing node, got x=%v y=%v", got.X, got.Y)
	}
}

func TestLayoutGridWhenNoCoords(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	dst, _ := s.CreateProject("dst", root, "")
	dstEnv, _ := s.GetDefaultEnvironment(dst.ID)

	pack := &Pack{
		Format:  FormatID,
		Version: CurrentVersion,
		Scope:   ScopeEnvironment,
		Services: []ServicePayload{
			{Key: "a", Label: "a", Settings: map[string]string{"image": "x", "service_port": "1"}},
			{Key: "b", Label: "b", Settings: map[string]string{"image": "x", "service_port": "1"}},
		},
	}
	_ = SealContentHash(pack)

	res, err := (&Importer{Store: s}).Import(pack, ImportOptions{
		Mode: ImportIntoProject, ProjectID: dst.ID, EnvironmentID: dstEnv.ID,
		LayoutMode: LayoutAuto,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NodeIDs) != 2 {
		t.Fatalf("nodes=%d", len(res.NodeIDs))
	}
	n0, _ := s.GetNode(res.NodeIDs[0])
	n1, _ := s.GetNode(res.NodeIDs[1])
	if n0.X == n1.X && n0.Y == n1.Y {
		t.Fatal("grid should place services at different coords")
	}
}

func TestPartialServiceImport(t *testing.T) {
	s := testStore(t)
	root := t.TempDir()
	src, _ := s.CreateProject("src", root, "")
	srcEnv, _ := s.GetDefaultEnvironment(src.ID)
	a, _ := s.CreateNode(&store.CanvasNode{ID: "a", Label: "api", ProjectID: src.ID, EnvironmentID: srcEnv.ID})
	b, _ := s.CreateNode(&store.CanvasNode{ID: "b", Label: "web", ProjectID: src.ID, EnvironmentID: srcEnv.ID})
	_ = s.SetNodeSetting(a.ID, "image", "a")
	_ = s.SetNodeSetting(a.ID, "service_port", "1")
	_ = s.SetNodeSetting(b.ID, "image", "b")
	_ = s.SetNodeSetting(b.ID, "service_port", "1")

	pack, err := (&Exporter{Store: s}).ExportEnvironment(srcEnv.ID, DefaultExportOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Services) != 2 {
		t.Fatalf("services=%d", len(pack.Services))
	}
	only := pack.Services[0].Key

	dstRoot := t.TempDir()
	dst, _ := s.CreateProject("dst", dstRoot, "")
	dstEnv, _ := s.GetDefaultEnvironment(dst.ID)

	res, err := (&Importer{Store: s}).Import(pack, ImportOptions{
		Mode: ImportIntoProject, ProjectID: dst.ID, EnvironmentID: dstEnv.ID,
		ServiceKeys: []string{only},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NodeIDs) != 1 {
		t.Fatalf("imported=%d", len(res.NodeIDs))
	}
	nodes, _ := s.ListNodesByEnvironment(dstEnv.ID)
	if len(nodes) != 1 {
		t.Fatalf("env nodes=%d", len(nodes))
	}
}

func TestSecretAppLinkOnImport(t *testing.T) {
	s := testStore(t)
	_ = s.SetAppSecret("DB_PASSWORD", "vault-value", "")

	srcRoot := t.TempDir()
	src, _ := s.CreateProject("src", srcRoot, "")
	srcEnv, _ := s.GetDefaultEnvironment(src.ID)
	n, _ := s.CreateNode(&store.CanvasNode{ID: "n", Label: "db", ProjectID: src.ID, EnvironmentID: srcEnv.ID})
	_ = s.SetNodeSetting(n.ID, "image", "postgres")
	_ = s.SetNodeSetting(n.ID, "service_port", "5432")
	_ = s.UpsertEnvVar(store.EnvVar{NodeID: n.ID, Key: "DB_PASSWORD", Value: "secret", Secret: true, Scope: store.EnvScopeRuntime})

	pack, _ := (&Exporter{Store: s}).ExportService(n.ID, DefaultExportOptions())
	// Secret should be omitted
	omitted := false
	for _, ev := range pack.Services[0].Env {
		if ev.Key == "DB_PASSWORD" && ev.ValueOmitted {
			omitted = true
		}
	}
	if !omitted {
		t.Fatal("expected omitted secret in pack")
	}

	dstRoot := t.TempDir()
	dst, _ := s.CreateProject("dst", dstRoot, "")
	dstEnv, _ := s.GetDefaultEnvironment(dst.ID)

	prev, err := (&Importer{Store: s}).Preview(pack, PreviewOptions{
		Mode: ImportIntoProject, ProjectID: dst.ID, EnvironmentID: dstEnv.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, k := range prev.ExistingAppSecrets {
		if k == "DB_PASSWORD" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected ExistingAppSecrets to include DB_PASSWORD, got %+v", prev.ExistingAppSecrets)
	}

	res, err := (&Importer{Store: s}).Import(pack, ImportOptions{
		Mode: ImportIntoProject, ProjectID: dst.ID, EnvironmentID: dstEnv.ID,
		SecretAppLinks: map[string]string{"DB_PASSWORD": "DB_PASSWORD"},
	})
	if err != nil {
		t.Fatal(err)
	}
	vars, _ := s.ListEnvVars(res.NodeIDs[0])
	got := ""
	for _, v := range vars {
		if v.Key == "DB_PASSWORD" {
			got = v.Value
		}
	}
	if got != "{{secret.DB_PASSWORD}}" {
		t.Fatalf("value=%q", got)
	}
}

func TestRecreateEnvironmentsOnImport(t *testing.T) {
	s := testStore(t)
	srcRoot := t.TempDir()
	src, _ := s.CreateProject("src", srcRoot, "")
	srcMain, _ := s.GetDefaultEnvironment(src.ID)
	srcStg, err := s.CreateEnvironment(src.ID, "Staging")
	if err != nil {
		t.Fatal(err)
	}
	a, _ := s.CreateNode(&store.CanvasNode{ID: "a", Label: "api", ProjectID: src.ID, EnvironmentID: srcMain.ID})
	b, _ := s.CreateNode(&store.CanvasNode{ID: "b", Label: "api", ProjectID: src.ID, EnvironmentID: srcStg.ID})
	_ = s.SetNodeSetting(a.ID, "image", "x")
	_ = s.SetNodeSetting(a.ID, "service_port", "1")
	_ = s.SetNodeSetting(b.ID, "image", "x")
	_ = s.SetNodeSetting(b.ID, "service_port", "1")

	pack, err := (&Exporter{Store: s}).ExportProject(src.ID, DefaultExportOptions())
	if err != nil {
		t.Fatal(err)
	}
	if len(pack.Environments) < 2 {
		t.Fatalf("envs=%d", len(pack.Environments))
	}

	dstRoot := t.TempDir()
	dst, _ := s.CreateProject("dst", dstRoot, "")
	// Only default env exists; recreate should create Staging.

	res, err := (&Importer{Store: s}).Import(pack, ImportOptions{
		Mode: ImportIntoProject, ProjectID: dst.ID,
		EnvImportMode: EnvImportRecreate,
	})
	if err != nil {
		t.Fatal(err)
	}
	envs, _ := s.ListEnvironments(dst.ID)
	if len(envs) < 2 {
		t.Fatalf("expected recreated envs, got %d", len(envs))
	}
	if len(res.NodeIDs) != 2 {
		t.Fatalf("nodes=%d", len(res.NodeIDs))
	}
	// Nodes should not all share one environment.
	n0, _ := s.GetNode(res.NodeIDs[0])
	n1, _ := s.GetNode(res.NodeIDs[1])
	if n0.EnvironmentID == n1.EnvironmentID {
		t.Fatal("recreate should put services in different environments")
	}
	_ = os.RemoveAll(filepath.Join(dstRoot, "unused"))
}
