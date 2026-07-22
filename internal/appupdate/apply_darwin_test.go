//go:build darwin

package appupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestApplyReplaceAndRelaunch(t *testing.T) {
	root := t.TempDir()
	installed := filepath.Join(root, "Draft.app")
	if err := writeFakeApp(installed, "old"); err != nil {
		t.Fatal(err)
	}
	if err := adhocSign(installed); err != nil {
		t.Fatal(err)
	}

	staging := filepath.Join(root, "staging", "Draft.app")
	if err := writeFakeApp(staging, "new"); err != nil {
		t.Fatal(err)
	}
	if err := adhocSign(staging); err != nil {
		t.Fatal(err)
	}
	zipPath := filepath.Join(root, "Draft_test_universal.app.zip")
	if out, err := exec.Command("/usr/bin/ditto", "-c", "-k", "--keepParent", staging, zipPath).CombinedOutput(); err != nil {
		t.Fatalf("zip: %v: %s", err, out)
	}

	relaunchPath := filepath.Join(root, "relaunched")
	prev := relaunch
	relaunch = func(appPath string) error {
		return os.WriteFile(relaunchPath, []byte(appPath), 0o600)
	}
	t.Cleanup(func() { relaunch = prev })

	job := Job{AppPID: 0, DaemonPID: 0, Artifact: zipPath, AppPath: installed}
	logf := func(string, ...any) {}
	if err := apply(job, logf); err != nil {
		t.Fatal(err)
	}

	got, err := os.ReadFile(filepath.Join(installed, "Contents", "MacOS", "marker"))
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
	if string(relaunched) != installed {
		t.Fatalf("relaunched path = %q, want %q", relaunched, installed)
	}
	if _, err := os.Stat(installed + ".previous"); !os.IsNotExist(err) {
		t.Fatalf("backup should be removed, err=%v", err)
	}
}

func writeFakeApp(appPath, marker string) error {
	macOS := filepath.Join(appPath, "Contents", "MacOS")
	if err := os.MkdirAll(macOS, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(macOS, "Draft"), []byte("#!/bin/sh\nexit 0\n"), 0o755); err != nil {
		return err
	}
	plist := `<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
  <key>CFBundleExecutable</key><string>Draft</string>
  <key>CFBundleIdentifier</key><string>com.ericgilerson.draft.test</string>
</dict></plist>`
	if err := os.WriteFile(filepath.Join(appPath, "Contents", "Info.plist"), []byte(plist), 0o644); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(macOS, "marker"), []byte(marker), 0o644)
}

func adhocSign(appPath string) error {
	out, err := exec.Command("/usr/bin/codesign", "--force", "-s", "-", appPath).CombinedOutput()
	if err != nil {
		return fmt.Errorf("codesign: %w: %s", err, out)
	}
	return nil
}
