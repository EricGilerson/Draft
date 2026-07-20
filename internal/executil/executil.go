// Package executil wraps os/exec so GUI builds on Windows do not flash
// console windows for short-lived child processes (git, powershell, agent CLIs).
package executil

import (
	"context"
	"os/exec"
)

// Command is like exec.Command, but hides the console window on Windows.
func Command(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	Prepare(cmd)
	return cmd
}

// CommandContext is like exec.CommandContext, but hides the console window on Windows.
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	Prepare(cmd)
	return cmd
}
