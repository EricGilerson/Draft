//go:build darwin

package appupdate

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"time"
)

func waitForExit(pid int) error {
	deadline := time.Now().Add(2 * time.Minute)
	for time.Now().Before(deadline) {
		if pid <= 0 || syscall.Kill(pid, 0) != nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return fmt.Errorf("timed out waiting for Draft to close")
}

func apply(job Job) error {
	if err := waitForExit(job.AppPID); err != nil {
		return err
	}
	if err := waitForExit(job.DaemonPID); err != nil {
		return err
	}
	parent := filepath.Dir(job.AppPath)
	if _, err := os.Stat(parent); err != nil {
		return err
	}
	tmp, err := os.MkdirTemp(parent, ".draft-update-")
	if err != nil {
		return err
	}
	defer os.RemoveAll(tmp)
	if out, err := exec.Command("/usr/bin/ditto", "-x", "-k", job.Artifact, tmp).CombinedOutput(); err != nil {
		return fmt.Errorf("unpack update: %w: %s", err, out)
	}
	candidate := filepath.Join(tmp, "Draft.app")
	if out, err := exec.Command("/usr/bin/codesign", "--verify", "--deep", "--strict", candidate).CombinedOutput(); err != nil {
		return fmt.Errorf("verify update signature: %w: %s", err, out)
	}
	backup := job.AppPath + ".previous"
	_ = os.RemoveAll(backup)
	if err := os.Rename(job.AppPath, backup); err != nil {
		return err
	}
	if err := os.Rename(candidate, job.AppPath); err != nil {
		_ = os.Rename(backup, job.AppPath)
		return err
	}
	if err := exec.Command("/usr/bin/open", job.AppPath).Start(); err != nil {
		return err
	}
	return os.RemoveAll(backup)
}
