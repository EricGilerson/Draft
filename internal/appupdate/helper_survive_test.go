package appupdate

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"Draft/internal/executil"
)

func detachAndStart(t *testing.T, cmd *exec.Cmd) {
	t.Helper()
	executil.Prepare(cmd)
	if err := executil.StartDetached(cmd); err != nil {
		t.Fatalf("start helper: %v", err)
	}
}

func TestHelperSurvivesParentExit(t *testing.T) {
	switch os.Getenv("DRAFT_UPDATE_MODE") {
	case "helper":
		time.Sleep(800 * time.Millisecond)
		if err := os.WriteFile(os.Getenv("DRAFT_UPDATE_MARKER"), []byte("ok"), 0o600); err != nil {
			os.Exit(1)
		}
		os.Exit(0)
	case "parent":
		cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSurvivesParentExit$", "-test.count=1")
		cmd.Env = append(os.Environ(),
			"DRAFT_UPDATE_MODE=helper",
			"DRAFT_UPDATE_MARKER="+os.Getenv("DRAFT_UPDATE_MARKER"),
		)
		detachAndStart(t, cmd)
		os.Exit(0)
	}

	markerPath := filepath.Join(t.TempDir(), "helper-alive")
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSurvivesParentExit$", "-test.count=1")
	cmd.Env = append(os.Environ(),
		"DRAFT_UPDATE_MODE=parent",
		"DRAFT_UPDATE_MARKER="+markerPath,
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("parent: %v: %s", err, out)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(markerPath); err == nil && string(data) == "ok" {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("helper did not write marker after parent exit (path %s)", markerPath)
}
