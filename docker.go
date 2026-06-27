package main

import "Draft/internal/dockerwatch"

// CheckDocker returns the last known Docker daemon status from the watcher.
// The frontend calls this once on mount for an immediate value, then relies on
// the "docker:status" Wails event for subsequent changes (no polling).
func (a *App) CheckDocker() dockerwatch.DaemonStatus {
	return a.hub.CurrentDaemon()
}
