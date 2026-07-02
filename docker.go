package main

import (
	"Draft/internal/dockerdesktop"
	"Draft/internal/dockerwatch"
)

// CheckDocker returns the last known Docker daemon status from the watcher.
// The frontend calls this once on mount for an immediate value, then relies on
// the "docker:status" Wails event for subsequent changes (no polling).
func (a *App) CheckDocker() dockerwatch.DaemonStatus {
	c, err := a.ensureDaemon()
	if err != nil || c == nil {
		return dockerwatch.DaemonStatus{State: "stopped", Error: errString(err)}
	}
	status, err := c.CheckDocker(a.ctx)
	if err != nil {
		return dockerwatch.DaemonStatus{State: "stopped", Error: err.Error()}
	}
	return status
}

// StartDocker launches the local Docker runtime so the watcher can reconnect
// and publish the normal "docker:status" transition once the daemon comes up.
func (a *App) StartDocker() error {
	return dockerdesktop.Start(a.ctx)
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
