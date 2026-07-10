package networking

import (
	"fmt"

	"Draft/internal/store"
)

// BestDeploymentURL returns the preferred public access string for a
// deployment. protocol is "http" (default) or "tcp".
//
// HTTP services use the Draft reverse-proxy public URL when available.
// TCP services never use the HTTP proxy port — they use the public hostname
// with the leased host port (scheme-less host:port).
func BestDeploymentURL(dep *store.Deployment, local LocalDomainStatus, protocol string) string {
	if dep == nil {
		return ""
	}
	if IsTCPProtocol(protocol) {
		if dep.Hostname != "" && dep.HostPort > 0 && local.Mode != "localhost-port" {
			return PublicTCPEndpoint(dep.Hostname, dep.HostPort)
		}
		if dep.HostPort > 0 {
			return fmt.Sprintf("127.0.0.1:%d", dep.HostPort)
		}
		return ""
	}
	// localhost-port preference (or no working proxy) uses the mapped host port.
	if local.Mode == "localhost-port" || local.ProxyPort <= 0 {
		if dep.HostPort > 0 {
			return fmt.Sprintf("http://127.0.0.1:%d", dep.HostPort)
		}
		return ""
	}
	if dep.Hostname != "" && local.ProxyPort > 0 {
		return PublicURL(dep.Hostname, local.ProxyPort)
	}
	if dep.HostPort > 0 {
		return fmt.Sprintf("http://127.0.0.1:%d", dep.HostPort)
	}
	return ""
}
