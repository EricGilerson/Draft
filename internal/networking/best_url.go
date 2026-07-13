package networking

import (
	"Draft/internal/store"
)

// BestDeploymentURL returns the preferred public access string for a
// deployment. protocol is "http" (default) or "tcp".
//
// Uses local.PublicSuffix from LocalDomainStatus (cached .draft vs resolv.sh)
// and falls back to 127.0.0.1:hostPort when hostname mode is unavailable.
func BestDeploymentURL(dep *store.Deployment, local LocalDomainStatus, protocol string) string {
	if dep == nil {
		return ""
	}
	return BestServiceURL(dep.Hostname, dep.HostPort, protocol, local)
}

// DisplayPublicURL is the router-backed helper for UI surfaces. It reads the
// cached local-domain status (no synchronous DNS probe on warm cache).
func (r *Router) DisplayPublicURL(hostname string, hostPort int, protocol string) string {
	if r == nil {
		return BestServiceURL(hostname, hostPort, protocol, LocalDomainStatus{
			Mode:         "localhost-port",
			PublicSuffix: PublicSuffix,
		})
	}
	return BestServiceURL(hostname, hostPort, protocol, r.LocalDomainStatus())
}

// DisplayPublicHostname rewrites an internal hostname to the effective public
// suffix from cached local-domain status.
func (r *Router) DisplayPublicHostname(hostname string) string {
	if hostname == "" {
		return ""
	}
	suffix := PublicSuffix
	if r != nil {
		status := r.LocalDomainStatus()
		if status.Mode == "localhost-port" {
			return "127.0.0.1"
		}
		if status.PublicSuffix != "" {
			suffix = status.PublicSuffix
		}
	}
	return HostnameWithSuffix(hostname, suffix)
}

// EffectivePublicSuffix returns the cached UI public suffix without forcing a
// fresh DNS probe when the verify cache is warm.
func (r *Router) EffectivePublicSuffix() string {
	if r == nil {
		return PublicSuffix
	}
	status := r.LocalDomainStatus()
	if status.PublicSuffix != "" {
		return status.PublicSuffix
	}
	return PublicSuffix
}
