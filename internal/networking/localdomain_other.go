//go:build !darwin && !windows

package networking

import "fmt"

func localDNSAddr() string { return "127.0.0.1:53535" }
func installLocalDomainResolver() error {
	return fmt.Errorf("local .draft domains are not supported on this platform")
}
func removeLocalDomainResolver() error {
	return fmt.Errorf("local .draft domains are not supported on this platform")
}
func localDomainResolverInstalled() (bool, error) { return false, nil }
