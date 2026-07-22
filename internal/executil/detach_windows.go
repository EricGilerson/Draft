//go:build windows

package executil

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

// CREATE_NEW_PROCESS_GROUP / DETACHED_PROCESS / CREATE_BREAKAWAY_FROM_JOB —
// breakaway helps under WebView2 job objects; some hosts (CI runners) deny it.
const (
	createNewProcessGroup  = 0x00000200
	detachedProcess        = 0x00000008
	createBreakawayFromJob = 0x01000000
	errorAccessDenied      = syscall.Errno(5)
)

// Detach configures cmd so it survives parent exit (new group + detached).
// Prefer StartDetached, which retries without CREATE_BREAKAWAY_FROM_JOB when
// the host job object forbids breakaway (Access is denied).
func Detach(cmd *exec.Cmd) {
	detachWith(cmd, true)
}

func detachWith(cmd *exec.Cmd, breakaway bool) {
	if cmd == nil {
		return
	}
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	// CREATE_NO_WINDOW conflicts with DETACHED_PROCESS.
	cmd.SysProcAttr.CreationFlags &^= createNoWindow
	flags := uint32(createNewProcessGroup | detachedProcess)
	if breakaway {
		flags |= createBreakawayFromJob
	} else {
		cmd.SysProcAttr.CreationFlags &^= createBreakawayFromJob
	}
	cmd.SysProcAttr.CreationFlags |= flags
}

// StartDetached applies Detach, starts cmd, and Releases the process handle.
// Retries without CREATE_BREAKAWAY_FROM_JOB when CreateProcess returns access denied.
func StartDetached(cmd *exec.Cmd) error {
	if cmd == nil {
		return errors.New("executil: nil command")
	}
	detachWith(cmd, true)
	if err := cmd.Start(); err != nil {
		if !isAccessDenied(err) {
			return err
		}
		detachWith(cmd, false)
		if err := cmd.Start(); err != nil {
			return err
		}
	}
	return cmd.Process.Release()
}

func isAccessDenied(err error) bool {
	for err != nil {
		if errno, ok := err.(syscall.Errno); ok {
			return errno == errorAccessDenied
		}
		if pe, ok := err.(*os.PathError); ok {
			err = pe.Err
			continue
		}
		if pe, ok := err.(*os.LinkError); ok {
			err = pe.Err
			continue
		}
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return false
		}
		err = unwrapped
	}
	return false
}
