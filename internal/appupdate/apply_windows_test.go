//go:build windows

package appupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

func TestIsElevationRequired(t *testing.T) {
	if !isElevationRequired(syscall.Errno(windows.ERROR_ELEVATION_REQUIRED)) {
		t.Fatal("expected elevation errno to match")
	}
	if !isElevationRequired(fmt.Errorf("wrap: %w", syscall.Errno(windows.ERROR_ELEVATION_REQUIRED))) {
		t.Fatal("expected wrapped elevation errno to match")
	}
	if isElevationRequired(syscall.Errno(windows.ERROR_ACCESS_DENIED)) {
		t.Fatal("access denied should not count as elevation required")
	}
}

func TestApplyReplaceAndRelaunch(t *testing.T) {
	root := t.TempDir()
	installDir := filepath.Join(root, "install")
	if err := os.MkdirAll(installDir, 0o755); err != nil {
		t.Fatal(err)
	}
	appPath := filepath.Join(installDir, "Draft.exe")
	if err := os.WriteFile(appPath, []byte("old"), 0o755); err != nil {
		t.Fatal(err)
	}

	installer := buildStubInstaller(t, root)
	relaunchPath := filepath.Join(root, "relaunched")
	prev := relaunch
	relaunch = func(path string) error {
		return os.WriteFile(relaunchPath, []byte(path), 0o600)
	}
	t.Cleanup(func() { relaunch = prev })

	job := Job{AppPID: 0, DaemonPID: 0, Artifact: installer, AppPath: appPath}
	logf := func(string, ...any) {}
	if err := apply(job, logf); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(installDir, "updated.marker"))
	if err != nil {
		t.Fatal(err)
	}
	if string(got) != "new" {
		t.Fatalf("marker = %q, want new", got)
	}
	relaunched, err := os.ReadFile(relaunchPath)
	if err != nil {
		t.Fatal(err)
	}
	if string(relaunched) != appPath {
		t.Fatalf("relaunched path = %q, want %q", relaunched, appPath)
	}
}

func buildStubInstaller(t *testing.T, dir string) string {
	t.Helper()
	src := filepath.Join(dir, "stub_installer.go")
	code := `package main
import (
  "os"
  "path/filepath"
  "strings"
)
func main() {
  var installDir string
  for _, a := range os.Args[1:] {
    if strings.HasPrefix(a, "/D=") {
      installDir = strings.TrimPrefix(a, "/D=")
    }
  }
  if installDir == "" {
    os.Exit(2)
  }
  if err := os.WriteFile(filepath.Join(installDir, "updated.marker"), []byte("new"), 0o644); err != nil {
    os.Exit(1)
  }
}
`
	if err := os.WriteFile(src, []byte(code), 0o644); err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(dir, "Draft-stub-payload.exe")
	cmd := exec.Command("go", "build", "-o", out, src)
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if b, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build stub payload: %v: %s", err, b)
	}
	if !strings.HasSuffix(out, ".exe") {
		t.Fatal("expected .exe")
	}
	return out
}
