//go:build !windows

package executil

import (
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
