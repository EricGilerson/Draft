//go:build !darwin && !windows

package appupdate

import "fmt"

func apply(job Job) error { return fmt.Errorf("automatic updates are not supported on this platform") }
