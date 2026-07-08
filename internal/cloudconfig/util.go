package cloudconfig

import (
	"time"

	"github.com/docker/go-units"
)

// unitsRAMInBytes parses a memory string ("512M", "1g", "256mb", "512Mi") to
// bytes using Docker's binary-unit rules, the same parser Draft uses.
func unitsRAMInBytes(s string) (int64, error) {
	return units.RAMInBytes(s)
}

// parseDurationSeconds parses a Go duration string ("30s", "1m30s") into whole
// seconds, rounding down. A bare integer is treated as seconds.
func parseDurationSeconds(s string) (int, error) {
	if d, err := time.ParseDuration(s); err == nil {
		return int(d.Seconds()), nil
	}
	// Fall back to a bare number of seconds.
	d, err := time.ParseDuration(s + "s")
	if err != nil {
		return 0, err
	}
	return int(d.Seconds()), nil
}
