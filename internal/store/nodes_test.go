package store

import (
	"errors"
	"testing"
)

func TestCreateNodeGeneratesUID(t *testing.T) {
	s := openTemp(t)

	node, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, EnvironmentID: 1, Label: "api"})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if node.UID == "" {
		t.Error("expected CreateNode to generate a non-empty UID")
	}
}

func TestCreateNodePreservesExplicitUID(t *testing.T) {
	s := openTemp(t)

	node, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, EnvironmentID: 1, Label: "api", UID: "abcd"})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if node.UID != "abcd" {
		t.Errorf("UID = %q, want %q", node.UID, "abcd")
	}
}

func TestEnsureNodeUIDIsStableAcrossCalls(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, EnvironmentID: 1, Label: "api"}); err != nil {
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

	if err := s.DB.Create(&CanvasNode{ID: "legacy", ProjectID: 1, EnvironmentID: 1, Label: "worker", UID: ""}).Error; err != nil {
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

func TestCreateNodeRetriesOnUIDCollision(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, EnvironmentID: 1, Label: "api", UID: "aaaa"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	// Force the generator to return the colliding value first, then a
	// unique one, to deterministically exercise the retry loop.
	origGenerateUID := generateUID
	t.Cleanup(func() { generateUID = origGenerateUID })
	calls := 0
	generateUID = func() string {
		calls++
		if calls == 1 {
			return "aaaa"
		}
		return "bbbb"
	}

	node, err := s.CreateNode(&CanvasNode{ID: "n2", ProjectID: 1, EnvironmentID: 1, Label: "worker"})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if calls < 2 {
		t.Fatalf("expected generateUID to be retried after a collision, called %d time(s)", calls)
	}
	if node.UID != "bbbb" {
		t.Errorf("UID = %q, want %q", node.UID, "bbbb")
	}
}

func TestCreateNodeUIDCollisionCheckIsScopedPerEnvironment(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, EnvironmentID: 1, Label: "api", UID: "aaaa"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	origGenerateUID := generateUID
	t.Cleanup(func() { generateUID = origGenerateUID })
	calls := 0
	generateUID = func() string {
		calls++
		return "aaaa"
	}

	// Different environment: "aaaa" is free there, so it should be accepted
	// without retrying, even though it's taken in environment 1.
	node, err := s.CreateNode(&CanvasNode{ID: "n2", ProjectID: 1, EnvironmentID: 2, Label: "api"})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if node.UID != "aaaa" {
		t.Errorf("UID = %q, want %q", node.UID, "aaaa")
	}
	if calls != 1 {
		t.Errorf("expected generateUID called once (no retry needed cross-environment), got %d", calls)
	}
}

func TestEnsureNodeUIDUnknownNode(t *testing.T) {
	s := openTemp(t)

	if _, err := s.EnsureNodeUID("does-not-exist"); err == nil {
		t.Error("expected error for unknown node, got nil")
	}
}

func TestCreateNodeRejectsDuplicateLabelInSameEnvironment(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, EnvironmentID: 1, Label: "api"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if _, err := s.CreateNode(&CanvasNode{ID: "n2", ProjectID: 1, EnvironmentID: 1, Label: "api"}); !errors.Is(err, ErrDuplicateNodeLabel) {
		t.Errorf("got %v, want ErrDuplicateNodeLabel", err)
	}
}

func TestCreateNodeRejectsDuplicateLabelAfterSanitization(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, EnvironmentID: 1, Label: "My Api"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	// Different raw string, but sanitizes to the same Docker service name
	// ("my-api") — must still be rejected, since that's the actual
	// collision that matters downstream (network alias, hostname).
	if _, err := s.CreateNode(&CanvasNode{ID: "n2", ProjectID: 1, EnvironmentID: 1, Label: "my_api"}); !errors.Is(err, ErrDuplicateNodeLabel) {
		t.Errorf("got %v, want ErrDuplicateNodeLabel", err)
	}
}

func TestCreateNodeAllowsSameLabelInDifferentEnvironments(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, EnvironmentID: 1, Label: "api"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if _, err := s.CreateNode(&CanvasNode{ID: "n2", ProjectID: 1, EnvironmentID: 2, Label: "api"}); err != nil {
		t.Errorf("expected same label to be allowed in a different environment, got: %v", err)
	}
}

func TestUpdateNodeRejectsDuplicateLabel(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, EnvironmentID: 1, Label: "api"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if _, err := s.CreateNode(&CanvasNode{ID: "n2", ProjectID: 1, EnvironmentID: 1, Label: "worker"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	if err := s.UpdateNode("n2", 1, 2, "api"); !errors.Is(err, ErrDuplicateNodeLabel) {
		t.Errorf("got %v, want ErrDuplicateNodeLabel", err)
	}

	// Original label and position must be unchanged after the rejected rename.
	node, err := s.GetNode("n2")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.Label != "worker" || node.X != 0 || node.Y != 0 {
		t.Errorf("node mutated despite rejected update: %+v", node)
	}
}

func TestUpdateNodeAllowsUnchangedLabelOnPositionMove(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, EnvironmentID: 1, Label: "api"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}

	// Simulates a drag: same label, new coordinates. Must not conflict
	// with itself.
	if err := s.UpdateNode("n1", 100, 200, "api"); err != nil {
		t.Errorf("UpdateNode: unexpected error on self-move: %v", err)
	}

	node, err := s.GetNode("n1")
	if err != nil {
		t.Fatalf("GetNode: %v", err)
	}
	if node.X != 100 || node.Y != 200 {
		t.Errorf("position not updated: %+v", node)
	}
}

func TestUpdateNodeRejectsEmptyLabel(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: 1, EnvironmentID: 1, Label: "api"}); err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := s.UpdateNode("n1", 0, 0, "   "); !errors.Is(err, ErrInvalidNode) {
		t.Errorf("got %v, want ErrInvalidNode", err)
	}
}
