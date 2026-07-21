//go:build !darwin && !windows

package networking

import "fmt"

func installLocalHTTPSRoot(_, _ string) error {
	return fmt.Errorf("local HTTPS trust installation is supported on macOS and Windows")
}
func removeLocalHTTPSRoot(_, _ string) error {
	return fmt.Errorf("local HTTPS trust installation is supported on macOS and Windows")
}
func localHTTPSRootTrusted(_, _ string) (bool, error) { return false, nil }
