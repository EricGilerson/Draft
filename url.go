package main

import (
	"fmt"

	"Draft/internal/networking"
	"Draft/internal/store"
)

func BestDeploymentURL(dep *store.Deployment, local networking.LocalDomainStatus) string {
	if dep == nil {
		return ""
	}
	if dep.Hostname != "" && local.Mode == "full" {
		return "http://" + dep.Hostname
	}
	if dep.Hostname != "" && local.Mode == "hostname-port" && local.ProxyPort > 0 {
		return fmt.Sprintf("http://%s:%d", dep.Hostname, local.ProxyPort)
	}
	if dep.Hostname != "" && !local.HostsConfigured && local.ProxyPort > 0 {
		if hostname := networking.LoopbackHostname(dep.Hostname); hostname != "" {
			return fmt.Sprintf("http://%s:%d", hostname, local.ProxyPort)
		}
	}
	if dep.HostPort > 0 {
		return fmt.Sprintf("http://127.0.0.1:%d", dep.HostPort)
	}
	return ""
}
