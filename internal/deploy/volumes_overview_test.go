package deploy

import (
	"path/filepath"
	"testing"

	"Draft/internal/store"
)

func TestEnrichVolumesFlagsOrphans(t *testing.T) {
	s, err := store.Open(store.FileDSN(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = s.Close() })

	p, err := s.CreateProject("vol-proj", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "node-live", ProjectID: p.ID, EnvironmentID: defaultEnvID(t, s, p.ID), Label: "db"}); err != nil {
		t.Fatal(err)
	}

	vols := []ManagedVolume{
		{Name: "draft-live", NodeID: "node-live", Target: "/var/lib/postgresql/data"},
		{Name: "draft-orphan", NodeID: "node-gone", Target: "/data"},
		{Name: "draft-nonode", NodeID: "", Target: "/data"},
	}

	got := enrichVolumes(s, vols)
	if len(got) != 3 {
		t.Fatalf("expected 3 volumes, got %d", len(got))
	}

	byName := map[string]VolumeOverview{}
	for _, v := range got {
		byName[v.Name] = v
	}

	if live := byName["draft-live"]; live.Orphaned || live.NodeLabel != "db" {
		t.Fatalf("live volume: expected label=db orphaned=false, got label=%q orphaned=%v", live.NodeLabel, live.Orphaned)
	}
	if orphan := byName["draft-orphan"]; !orphan.Orphaned || orphan.NodeLabel != "" {
		t.Fatalf("deleted-node volume should be orphaned with no label, got label=%q orphaned=%v", orphan.NodeLabel, orphan.Orphaned)
	}
	if noNode := byName["draft-nonode"]; !noNode.Orphaned {
		t.Fatalf("volume with empty node id should be orphaned, got orphaned=%v", noNode.Orphaned)
	}
}
