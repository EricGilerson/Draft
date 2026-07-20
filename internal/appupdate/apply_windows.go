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

func apply(job Job) error {
	if err := waitForExit(job.AppPID); err != nil {
		return err
	}
	if err := waitForExit(job.DaemonPID); err != nil {
		return err
	}
	installDir := filepath.Dir(job.AppPath)
	cmd := exec.Command(job.Artifact, "/S", "/D="+installDir)
	if err := cmd.Run(); err != nil {
		return err
	}
	return exec.Command(job.AppPath).Start()
}
