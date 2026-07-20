//go:build windows

package executil

import (
	"os/exec"
	"syscall"
)

// CREATE_NO_WINDOW — not exported by syscall on all Go versions.
const createNoWindow = 0x08000000

// Prepare configures cmd so a console child does not flash a visible window
// when the parent is a GUI process (Wails -H windowsgui).
func Prepare(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags |= createNoWindow
}
