package networking

import (
	"fmt"

	"Draft/internal/store"
)

func BestDeploymentURL(dep *store.Deployment, local LocalDomainStatus) string {
	if dep == nil {
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
