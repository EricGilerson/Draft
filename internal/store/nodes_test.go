package store

import "testing"

func TestCreateNodeGeneratesUID(t *testing.T) {
	s := openTemp(t)

	node, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, Label: "api"})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if node.UID == "" {
		t.Error("expected CreateNode to generate a non-empty UID")
	}
}

func TestCreateNodePreservesExplicitUID(t *testing.T) {
	s := openTemp(t)

	node, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, Label: "api", UID: "abcd"})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if node.UID != "abcd" {
		t.Errorf("UID = %q, want %q", node.UID, "abcd")
	}
}

func TestEnsureNodeUIDIsStableAcrossCalls(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, Label: "api"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	first, err := s.EnsureNodeUID("n1")
	if err != nil {
		t.Fatalf("EnsureNodeUID: %v", err)
	}
	second, err := s.EnsureNodeUID("n1")
	if err != nil {
		t.Fatalf("EnsureNodeUID: %v", err)
	}
	if first != second {
		t.Errorf("UID changed across calls: %q != %q", first, second)
	}

	node, err := s.GetNode("n1")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.UID != first {
		t.Errorf("stored UID = %q, want %q", node.UID, first)
	}
}

// TestEnsureNodeUIDBackfillsLegacyRows simulates a node row created before
// the UID column existed (empty UID) and verifies EnsureNodeUID generates
// and persists one instead of returning empty each time.
func TestEnsureNodeUIDBackfillsLegacyRows(t *testing.T) {
	s := openTemp(t)

	if err := s.DB.Create(&CanvasNode{ID: "legacy", ProjectID: 1, Label: "worker", UID: ""}).Error; err != nil {
		t.Fatalf("seed legacy node: %v", err)
	}

	uid, err := s.EnsureNodeUID("legacy")
	if err != nil {
		t.Fatalf("EnsureNodeUID: %v", err)
	}
	if uid == "" {
		t.Fatal("expected a generated UID, got empty string")
	}

	node, err := s.GetNode("legacy")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.UID != uid {
		t.Errorf("persisted UID = %q, want %q", node.UID, uid)
	}

	again, err := s.EnsureNodeUID("legacy")
	if err != nil {
		t.Fatalf("EnsureNodeUID (second call): %v", err)
	}
	if again != uid {
		t.Errorf("UID changed on second call: %q != %q", again, uid)
	}
}

func TestEnsureNodeUIDUnknownNode(t *testing.T) {
	s := openTemp(t)

	if _, err := s.EnsureNodeUID("does-not-exist"); err == nil {
		t.Error("expected error for unknown node, got nil")
	}
}
