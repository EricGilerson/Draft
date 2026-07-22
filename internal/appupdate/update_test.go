package appupdate

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.1.9", 1},
		{"v1.2.0", "1.2.0", 0},
		{"1.2.0", "1.2.1", -1},
		{"1.10.0", "1.9.9", 1},
	}
	for _, tc := range cases {
		got := compareVersion(tc.a, tc.b)
		if (got > 0) != (tc.want > 0) || (got < 0) != (tc.want < 0) {
			t.Fatalf("compareVersion(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestAssetURL(t *testing.T) {
	assets := []Asset{{Name: "one", URL: "https://example.test/one"}}
	if got := assetURL(assets, "one"); got != "https://example.test/one" {
		t.Fatalf("assetURL = %q", got)
	}
	if got := assetURL(assets, "missing"); got != "" {
		t.Fatalf("missing asset = %q", got)
	}
}

func writeReadyStatus(t *testing.T, dir, version, artifact string) {
	t.Helper()
	data, err := json.Marshal(persisted{
		Status:   Status{State: "ready", Version: version},
		Artifact: artifact,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "status.json"), data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadIgnoresAlreadyInstalledReady(t *testing.T) {
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "1.2.0")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(artifactDir, "Draft-installer.exe")
	if err := os.WriteFile(artifact, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeReadyStatus(t, dir, "1.2.0", artifact)

	m := &Manager{currentVersion: "1.2.0", dir: dir}
	m.load()
	if m.state.Status.State == "ready" {
		t.Fatalf("load restored ready for already-installed version: %+v", m.state.Status)
	}
	if _, err := os.Stat(filepath.Join(dir, "status.json")); !os.IsNotExist(err) {
		t.Fatalf("stale status.json still present: %v", err)
	}
	if _, err := os.Stat(artifactDir); !os.IsNotExist(err) {
		t.Fatalf("stale artifact dir still present: %v", err)
	}
}

func TestLoadKeepsNewerReady(t *testing.T) {
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "1.3.0")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(artifactDir, "Draft-installer.exe")
	if err := os.WriteFile(artifact, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeReadyStatus(t, dir, "1.3.0", artifact)

	m := &Manager{currentVersion: "1.2.0", dir: dir}
	m.load()
	if m.state.Status.State != "ready" || m.state.Status.Version != "1.3.0" {
		t.Fatalf("load = %+v, want ready 1.3.0", m.state.Status)
	}
}

func TestClearAppliedUpdate(t *testing.T) {
	dir := t.TempDir()
	artifactDir := filepath.Join(dir, "1.3.0")
	if err := os.MkdirAll(artifactDir, 0o700); err != nil {
		t.Fatal(err)
	}
	artifact := filepath.Join(artifactDir, "Draft-installer.exe")
	if err := os.WriteFile(artifact, []byte("payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	writeReadyStatus(t, dir, "1.3.0", artifact)
	if err := os.WriteFile(filepath.Join(dir, "apply-job.json"), []byte(`{}`), 0o600); err != nil {
		t.Fatal(err)
	}

	clearAppliedUpdate(dir, artifact)
	if _, err := os.Stat(filepath.Join(dir, "status.json")); !os.IsNotExist(err) {
		t.Fatalf("status.json: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "apply-job.json")); !os.IsNotExist(err) {
		t.Fatalf("apply-job.json: %v", err)
	}
	if _, err := os.Stat(artifactDir); !os.IsNotExist(err) {
		t.Fatalf("artifact dir: %v", err)
	}
}
