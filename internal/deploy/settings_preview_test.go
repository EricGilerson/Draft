package deploy

import (
	"path/filepath"
	"testing"

	"Draft/internal/store"
)

func openPreviewTestStore(t *testing.T) (*store.Store, *store.CanvasNode) {
	t.Helper()
	s, err := store.Open(store.FileDSN(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })
	projectPath := t.TempDir()
	p, err := s.CreateProject("preview-proj", projectPath, "")
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CreateNode(&store.CanvasNode{ID: "node-preview", ProjectID: p.ID, EnvironmentID: defaultEnvID(t, s, p.ID), Label: "api", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}
	return s, n
}

func TestPreviewStagedChangesIgnoresUnrelatedVolumeMounts(t *testing.T) {
	s, n := openPreviewTestStore(t)
	// Empty array is valid "no mounts" but previously failed blanket validation.
	if err := s.SetNodeSetting(n.ID, "volume_mounts", "[]"); err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewStagedChangesFromStore(s, n.ID, map[string]string{
		"git_branch": "main",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Errors) > 0 {
		t.Fatalf("unexpected errors: %+v", preview.Errors)
	}
}

func TestPreviewStagedChangesValidatesVolumeMountsWhenStagingThem(t *testing.T) {
	s, n := openPreviewTestStore(t)

	preview, err := PreviewStagedChangesFromStore(s, n.ID, map[string]string{
		"volume_mounts": "not-json",
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Errors) == 0 {
		t.Fatal("expected validation error for bad volume_mounts")
	}
}

func TestValidateVolumeMountsJSONAcceptsEmptyArray(t *testing.T) {
	if err := validateVolumeMountsJSON("[]"); err != nil {
		t.Fatalf("[] should be valid: %v", err)
	}
}

func TestPreviewStagedChangesEmptyProposedIsNoOp(t *testing.T) {
	s, n := openPreviewTestStore(t)
	if err := s.SetNodeSetting(n.ID, "volume_mounts", "garbage"); err != nil {
		t.Fatal(err)
	}

	preview, err := PreviewStagedChangesFromStore(s, n.ID, map[string]string{})
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Errors) > 0 || len(preview.Warnings) > 0 {
		t.Fatalf("expected no preview output for empty proposed batch, got %+v", preview)
	}
}