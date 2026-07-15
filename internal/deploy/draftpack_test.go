package deploy

import (
	"os"
	"path/filepath"
	"testing"

	"Draft/internal/draftpack"
	"Draft/internal/store"
)

func TestPreviewAndImportDraftPackJSON(t *testing.T) {
	s := openTestStore(t)
	e := &Engine{store: s}

	srcRoot := t.TempDir()
	srcProj, err := s.CreateProject("json-src", srcRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	srcEnv, _ := s.GetDefaultEnvironment(srcProj.ID)
	node, err := s.CreateNode(&store.CanvasNode{
		ID: "svc-json", Label: "web", ProjectID: srcProj.ID, EnvironmentID: srcEnv.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting(node.ID, "image", "nginx:alpine")
	_ = s.SetNodeSetting(node.ID, "service_port", "80")

	exRes, err := e.ExportDraftPackService(node.ID, DefaultDraftPackExportOptions())
	if err != nil {
		t.Fatal(err)
	}
	if exRes.JSON == "" {
		t.Fatal("export JSON empty")
	}

	dstRoot := t.TempDir()
	dstProj, err := s.CreateProject("json-dst", dstRoot, "")
	if err != nil {
		t.Fatal(err)
	}
	dstEnv, _ := s.GetDefaultEnvironment(dstProj.ID)

	prev, err := e.PreviewDraftPackJSON([]byte(exRes.JSON), draftpack.PreviewOptions{
		Mode: draftpack.ImportIntoProject, ProjectID: dstProj.ID, EnvironmentID: dstEnv.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if prev.PackScope != draftpack.ScopeService {
		t.Fatalf("scope=%q", prev.PackScope)
	}
	if len(prev.Services) != 1 {
		t.Fatalf("services=%d", len(prev.Services))
	}

	res, err := e.ImportDraftPackJSON([]byte(exRes.JSON), draftpack.ImportOptions{
		Mode: draftpack.ImportIntoProject, ProjectID: dstProj.ID, EnvironmentID: dstEnv.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.NodeIDs) != 1 {
		t.Fatalf("imported nodes=%d", len(res.NodeIDs))
	}
	got, err := s.GetNode(res.NodeIDs[0])
	if err != nil {
		t.Fatal(err)
	}
	if got.Label != "web" {
		t.Fatalf("label=%q", got.Label)
	}
	if got.EnvironmentID != dstEnv.ID {
		t.Fatalf("env=%d want %d", got.EnvironmentID, dstEnv.ID)
	}
	_ = os.RemoveAll(filepath.Join(dstRoot, "unused"))
}
