package networking

import (
	"fmt"
	"strings"
)

// HostnameWithSuffix rewrites an internal *.draft.local hostname to the given
// public suffix (e.g. "draft" or "draft.resolv.sh"). Empty suffix falls back to
// the internet-resolvable PublicSuffix.
func HostnameWithSuffix(hostname, suffix string) string {
	hostname = strings.TrimSuffix(hostname, ".")
	if hostname == "" {
		return ""
	}
	if suffix == "" {
		suffix = PublicSuffix
	}
	if strings.HasSuffix(hostname, "."+Suffix) {
		return strings.TrimSuffix(hostname, "."+Suffix) + "." + suffix
	}
	return hostname
}

// PublicURLWithSuffix is PublicURL but uses an explicit public suffix.
func PublicURLWithSuffix(hostname string, proxyPort int, suffix string) string {
	return PublicURLWithScheme(hostname, proxyPort, suffix, "http")
}

// PublicURLWithScheme is PublicURLWithSuffix with an explicit HTTP scheme.
func PublicURLWithScheme(hostname string, proxyPort int, suffix, scheme string) string {
	if hostname == "" || proxyPort <= 0 {
		return ""
	}
	if scheme != "https" {
		scheme = "http"
	}
	publicHost := HostnameWithSuffix(hostname, suffix)
	if (scheme == "http" && proxyPort == 80) || (scheme == "https" && proxyPort == 443) {
		return fmt.Sprintf("%s://%s", scheme, publicHost)
	}
	return fmt.Sprintf("%s://%s:%d", scheme, publicHost, proxyPort)
}

// PublicTCPEndpointWithSuffix is PublicTCPEndpoint with an explicit suffix.
func PublicTCPEndpointWithSuffix(hostname string, hostPort int, suffix string) string {
	if hostname == "" || hostPort <= 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", HostnameWithSuffix(hostname, suffix), hostPort)
}

// LoopbackURL is the always-available host-port access string.
func LoopbackURL(hostPort int, protocol string) string {
	if hostPort <= 0 {
		return ""
	}
	if IsTCPProtocol(protocol) {
		return fmt.Sprintf("127.0.0.1:%d", hostPort)
	}
	return fmt.Sprintf("http://127.0.0.1:%d", hostPort)
}

// BestServiceURL returns the preferred UI-facing public access string using the
// cached LocalDomainStatus (no DNS probe). Preference order:
//  1. localhost-port mode → 127.0.0.1:hostPort
//  2. public hostname with status.PublicSuffix (.draft when verified, else resolv.sh)
//  3. 127.0.0.1:hostPort fallback
func BestServiceURL(hostname string, hostPort int, protocol string, local LocalDomainStatus) string {
	suffix := local.PublicSuffix
	if suffix == "" {
		suffix = PublicSuffix
	}
	if IsTCPProtocol(protocol) {
		if hostname != "" && hostPort > 0 && local.Mode != "localhost-port" {
			return PublicTCPEndpointWithSuffix(hostname, hostPort, suffix)
		}
		return LoopbackURL(hostPort, protocol)
	}
	if local.Mode == "localhost-port" || local.ProxyPort <= 0 {
		return LoopbackURL(hostPort, protocol)
	}
	if hostname != "" && local.HTTPSTrusted && local.HTTPSPort > 0 {
		return PublicURLWithScheme(hostname, local.HTTPSPort, suffix, "https")
	}
	if hostname != "" && local.ProxyPort > 0 {
		return PublicURLWithSuffix(hostname, local.ProxyPort, suffix)
	}
	return LoopbackURL(hostPort, protocol)
}
