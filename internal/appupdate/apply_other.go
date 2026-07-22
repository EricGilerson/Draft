//go:build !darwin && !windows

package appupdate

import "fmt"

func relaunchApp(appPath string) error {
	return fmt.Errorf("automatic updates are not supported on this platform")
}

func apply(job Job, logf func(string, ...any)) error {
	_ = job
	_ = logf
	return fmt.Errorf("automatic updates are not supported on this platform")
}
