//go:build windows

package executil

import (
	"os/exec"
	"syscall"
)

// CREATE_NEW_PROCESS_GROUP / DETACHED_PROCESS / CREATE_BREAKAWAY_FROM_JOB —
// needed so WebView2 job objects do not kill the updater helper when the UI exits.
const (
	createNewProcessGroup  = 0x00000200
	detachedProcess        = 0x00000008
	createBreakawayFromJob = 0x01000000
)

// Detach configures cmd so it survives parent exit (break away from job + new group).
// Clears CREATE_NO_WINDOW if Prepare set it — that flag conflicts with DETACHED_PROCESS.
func Detach(cmd *exec.Cmd) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags &^= createNoWindow
	cmd.SysProcAttr.CreationFlags |= createNewProcessGroup | detachedProcess | createBreakawayFromJob
}
