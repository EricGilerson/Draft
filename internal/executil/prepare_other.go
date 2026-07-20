//go:build !windows

package executil

import "os/exec"

// Prepare is a no-op outside Windows.
func Prepare(cmd *exec.Cmd) {}
