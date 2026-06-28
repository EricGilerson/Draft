package networking

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestBuildBlockEmpty(t *testing.T) {
	b := buildBlock(nil)
	if !strings.Contains(b, markerStart) || !strings.Contains(b, markerEnd) {
		t.Errorf("empty block missing markers: %q", b)
	}
}

func TestBuildBlockEntries(t *testing.T) {
	entries := []HostsEntry{
		{Hostname: "api.myapp.default.a3f2.draft.local"},
		{IP: "127.0.0.1", Hostname: "db.myapp.default.a3f2.draft.local"},
	}
	b := buildBlock(entries)
	if !strings.Contains(b, "127.0.0.1  api.myapp.default.a3f2.draft.local") {
		t.Error("missing api entry (should default to 127.0.0.1)")
	}
	if !strings.Contains(b, "127.0.0.1  db.myapp.default.a3f2.draft.local") {
		t.Error("missing db entry")
	}
}

func TestReplaceBlockAppends(t *testing.T) {
	existing := "127.0.0.1  localhost\n::1  localhost"
	result := replaceBlock(existing, []HostsEntry{
		{Hostname: "api.test.default.aaaa.draft.local"},
	})

	if !strings.Contains(result, "127.0.0.1  localhost") {
		t.Error("existing entries should be preserved")
	}
	if !strings.Contains(result, markerStart) {
		t.Error("marker should be added")
	}
	if !strings.Contains(result, "api.test.default.aaaa.draft.local") {
		t.Error("new entry should be present")
	}
}

func TestReplaceBlockUpdates(t *testing.T) {
	existing := "127.0.0.1  localhost\n" +
		markerStart + "\n" +
		"127.0.0.1  old.entry.default.aaaa.draft.local\n" +
		markerEnd + "\n" +
		"::1  localhost"

	result := replaceBlock(existing, []HostsEntry{
		{Hostname: "new.entry.default.bbbb.draft.local"},
	})

	if strings.Contains(result, "old.entry") {
		t.Error("old entry should be removed")
	}
	if !strings.Contains(result, "new.entry.default.bbbb.draft.local") {
		t.Error("new entry should be present")
	}
	if !strings.Contains(result, "127.0.0.1  localhost") {
		t.Error("pre-block content should be preserved")
	}
	if !strings.Contains(result, "::1  localhost") {
		t.Error("post-block content should be preserved")
	}
}

func TestParseManagedBlock(t *testing.T) {
	content := "127.0.0.1  localhost\n" +
		markerStart + "\n" +
		"127.0.0.1  api.test.default.aaaa.draft.local\n" +
		"127.0.0.1  db.test.default.aaaa.draft.local\n" +
		markerEnd + "\n"

	entries := parseManagedBlock(content)
	if len(entries) != 2 {
		t.Fatalf("got %d entries, want 2", len(entries))
	}
	if entries[0].Hostname != "api.test.default.aaaa.draft.local" {
		t.Errorf("entry[0].Hostname = %q", entries[0].Hostname)
	}
	if entries[1].Hostname != "db.test.default.aaaa.draft.local" {
		t.Errorf("entry[1].Hostname = %q", entries[1].Hostname)
	}
}

func TestSyncHostsFileAt(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "hosts")
	if err := os.WriteFile(path, []byte("127.0.0.1  localhost\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	entries := []HostsEntry{
		{Hostname: "api.myapp.default.a3f2.draft.local"},
	}
	if err := syncHostsFileAt(path, entries); err != nil {
		t.Fatal(err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	content := string(data)
	if !strings.Contains(content, "127.0.0.1  localhost") {
		t.Error("existing content lost")
	}
	if !strings.Contains(content, "api.myapp.default.a3f2.draft.local") {
		t.Error("new entry not written")
	}

	// Second sync replaces the block
	entries2 := []HostsEntry{
		{Hostname: "db.myapp.default.a3f2.draft.local"},
	}
	if err := syncHostsFileAt(path, entries2); err != nil {
		t.Fatal(err)
	}
	data, _ = os.ReadFile(path)
	content = string(data)
	if strings.Contains(content, "api.myapp") {
		t.Error("old entry should be gone after re-sync")
	}
	if !strings.Contains(content, "db.myapp") {
		t.Error("new entry should be present")
	}
}
