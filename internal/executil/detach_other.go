//go:build !windows

package executil

import (
	"errors"
	"os/exec"
	"syscall"
)

// Detach configures cmd so it survives parent exit (new process group).
func Detach(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.Setpgid = true
}

// StartDetached applies Detach, starts cmd, and Releases the process handle.
func StartDetached(cmd *exec.Cmd) error {
	if cmd == nil {
		return errors.New("executil: nil command")
	}
	Detach(cmd)
	if err := cmd.Start(); err != nil {
		return err
	}
	return cmd.Process.Release()
}
