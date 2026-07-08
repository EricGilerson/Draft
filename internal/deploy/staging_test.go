package deploy

import (
	"path/filepath"
	"testing"

	"Draft/internal/store"
)

func TestNodeConfigStatusClearsAfterPromote(t *testing.T) {
	s := openTestStore(t)
	projectPath := filepath.Join(t.TempDir(), "p")
	p, err := s.CreateProject("p", projectPath, "")
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CreateNode(&store.CanvasNode{ID: "svc-1", ProjectID: p.ID, Label: "app", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetNodeSetting(n.ID, "build_no_cache", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.StageNodeSettings(n.ID, map[string]string{"build_no_cache": "true"}); err != nil {
		t.Fatal(err)
	}

	before, err := NodeConfigStatusFromStore(s, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if !before.HasStagedChanges {
		t.Fatal("expected staged changes before promote")
	}

	if err := s.PromoteStagedToApplied(n.ID); err != nil {
		t.Fatal(err)
	}

	after, err := NodeConfigStatusFromStore(s, n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if after.HasStagedChanges {
		t.Fatalf("expected no staged changes after promote, got settings=%v env=%v", after.StagedSettings, after.StagedEnvChanges)
	}
	if after.AppliedSettings["build_no_cache"] != "true" {
		t.Fatalf("applied settings not promoted: %v", after.AppliedSettings)
	}
}
