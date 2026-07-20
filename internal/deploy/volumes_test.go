package deploy

import (
	"path/filepath"
	"strings"
	"testing"

	"Draft/internal/store"
)

func TestSpecsToMounts_BindBackCompat(t *testing.T) {
	// Legacy rows without a "type" field and using "hostPath" must still
	// produce a bind mount.
	specs := ParseVolumeSpecs(`[{"hostPath":"/host","containerPath":"/app","readOnly":true}]`)
	mounts := SpecsToMounts(specs)
	if len(mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(mounts))
	}
	if mounts[0].Type != "bind" {
		t.Errorf("type = %v, want bind", mounts[0].Type)
	}
	if mounts[0].Source != "/host" || mounts[0].Target != "/app" || !mounts[0].ReadOnly {
		t.Errorf("mount = %+v", mounts[0])
	}
}

func TestSpecsToMounts_AutoVolumeHasEmptySource(t *testing.T) {
	// Auto-named volumes leave Source empty here; ensureNamedVolumes fills it
	// at deploy time. This keeps parsing pure (no Docker client needed).
	specs := ParseVolumeSpecs(`[{"type":"volume","containerPath":"/var/lib/postgresql/data"}]`)
	mounts := SpecsToMounts(specs)
	if len(mounts) != 1 {
		t.Fatalf("expected 1 mount, got %d", len(mounts))
	}
	if mounts[0].Type != "volume" {
		t.Errorf("type = %v, want volume", mounts[0].Type)
	}
	if mounts[0].Source != "" {
		t.Errorf("auto volume source should be empty until deploy time, got %q", mounts[0].Source)
	}
	if mounts[0].Target != "/var/lib/postgresql/data" {
		t.Errorf("target = %q", mounts[0].Target)
	}
}

func TestSpecsToMounts_ExplicitVolumeNamePassesThrough(t *testing.T) {
	specs := ParseVolumeSpecs(`[{"type":"volume","source":"shared-data","containerPath":"/data"}]`)
	mounts := SpecsToMounts(specs)
	if len(mounts) != 1 || mounts[0].Source != "shared-data" {
		t.Fatalf("explicit volume name should pass through, got %+v", mounts)
	}
}

func TestSpecsToMounts_DropsIncompleteEntries(t *testing.T) {
	specs := ParseVolumeSpecs(`[{"type":"volume","containerPath":""},{"type":"bind","containerPath":"/x"}]`)
	mounts := SpecsToMounts(specs)
	if len(mounts) != 0 {
		t.Errorf("expected 0 mounts from incomplete entries, got %d (%+v)", len(mounts), mounts)
	}
}

func TestResolveBindSourceUnderProject(t *testing.T) {
	project := t.TempDir()
	got := store.ResolveUnderProject(project, "data/cache")
	want := filepath.Join(project, "data", "cache")
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	abs := filepath.Join(project, "abs-data")
	if store.ResolveUnderProject(project, abs) != filepath.Clean(abs) {
		t.Fatal("absolute bind source should pass through")
	}
}

func TestDraftVolumeName_StableAndIsolating(t *testing.T) {
	a := DraftVolumeName(7, "My Project", "default", "abcd", "/var/lib/postgresql/data")
	b := DraftVolumeName(7, "My Project", "default", "abcd", "/var/lib/postgresql/data")
	if a != b {
		t.Errorf("same inputs should yield same name: %q vs %q", a, b)
	}
	// Different node UID => different volume (per-service isolation).
	c := DraftVolumeName(7, "My Project", "default", "ef01", "/var/lib/postgresql/data")
	if a == c {
		t.Errorf("different UID should yield different volume name: %q", a)
	}
	// Different environment => different volume (future multi-env isolation).
	d := DraftVolumeName(7, "My Project", "staging", "abcd", "/var/lib/postgresql/data")
	if a == d {
		t.Errorf("different env should yield different volume name: %q", a)
	}
	// Sanity: looks like a Draft-managed name.
	if !strings.HasPrefix(a, "draft-7-my-project-default-abcd-") {
		t.Errorf("unexpected name shape: %q", a)
	}
}

func TestDraftVolumeName_LengthBounded(t *testing.T) {
	// Absurdly long target + project must still produce a name within Docker's
	// practical ~64-char volume name limit.
	long := strings.Repeat("a", 200)
	name := DraftVolumeName(1, long, long, "abcd", "/"+long)
	if len(name) > 63 {
		t.Errorf("volume name too long: %d (%q)", len(name), name)
	}
}
