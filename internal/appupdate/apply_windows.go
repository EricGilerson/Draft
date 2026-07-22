//go:build windows

package appupdate

import (
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
)

func waitForExit(pid int) error {
	if pid <= 0 {
		return nil
	}
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(h)
	result, err := syscall.WaitForSingleObject(h, 120000)
	if err != nil || result == syscall.WAIT_TIMEOUT {
		return fmt.Errorf("timed out waiting for Draft to close")
	}
	return nil
}

func relaunchApp(appPath string) error {
	return exec.Command(appPath).Start()
}

func apply(job Job, logf func(string, ...any)) error {
	if err := waitForExit(job.AppPID); err != nil {
		return err
	}
	logf("app pid %d exited", job.AppPID)
	if err := waitForExit(job.DaemonPID); err != nil {
		return err
	}
	if job.DaemonPID > 0 {
		logf("daemon pid %d exited", job.DaemonPID)
	}
	installDir := filepath.Dir(job.AppPath)
	logf("running installer %s /S /D=%s", job.Artifact, installDir)
	cmd := exec.Command(job.Artifact, "/S", "/D="+installDir)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("installer failed: %w: %s", err, out)
	}
	if len(out) > 0 {
		logf("installer output: %s", out)
	}
	logf("relaunching %s", job.AppPath)
	return relaunch(job.AppPath)
}
