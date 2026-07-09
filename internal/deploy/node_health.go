package deploy

import (
	"context"
	"strings"

	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/client"
)

// NodeHealth is a lightweight, canvas-friendly subset of service state: just
// enough to render a health chip and a clickable URL on a service node without
// running the full metrics + reachability probe that GetServiceMetrics does.
// DockerHealth is the raw Docker healthcheck status ("starting"|"healthy"|
// "unhealthy"|"") — empty when the container has no healthcheck configured.
// RouteProtocol is "http" (default) or "tcp" and drives how PublicURL is shaped.
type NodeHealth struct {
	NodeID        string `json:"nodeId"`
	Status        string `json:"status"`
	DockerHealth  string `json:"dockerHealth"`
	HostPort      int    `json:"hostPort"`
	Hostname      string `json:"hostname"`
	InternalURL   string `json:"internalUrl"`
	PublicURL     string `json:"publicUrl"`
	RouteProtocol string `json:"routeProtocol"`
}

// GetNodeHealth returns the live health + URL state for a single node. It does
// one Docker inspect (only when a container is running/starting) and skips the
// reachability HTTP probe and metric collection entirely, so it's safe to call
// per-node when opening a canvas.
func (e *Engine) GetNodeHealth(ctx context.Context, nodeID string) (NodeHealth, error) {
	out := NodeHealth{NodeID: nodeID, Status: "stopped"}

	settings, _ := e.store.GetNodeSettings(nodeID)
	// Linked services mirror the root container's health; hostnames stay local to the alias.
	healthNodeID := nodeID
	isLinked := false
	if link := ParseServiceLink(settings[SettingServiceLink]); link != nil {
		healthNodeID = link.RootNodeID
		isLinked = true
	}

	deployments, err := e.store.ListDeployments(healthNodeID)
	if err != nil {
		return out, err
	}
	portStr := strings.TrimSpace(settings["service_port"])
	protocol := strings.TrimSpace(settings["route_protocol"])
	if protocol == "" {
		protocol = "http"
	}
	out.RouteProtocol = protocol

	var active *store.Deployment
	for i := range deployments {
		if deployments[i].Status != "stopped" && deployments[i].Status != "failed" {
			active = &deployments[i]
			break
		}
	}
	if active == nil && len(deployments) > 0 {
		active = &deployments[0]
	}
	if active == nil {
		return out, nil
	}

	out.Status = active.Status
	out.HostPort = active.HostPort
	proxyPort := 0
	if e.router != nil {
		proxyPort = e.router.LocalDomainStatus().ProxyPort
	}
	if isLinked {
		if node, err := e.store.GetNode(nodeID); err == nil {
			if addr, err := e.computeNodeAddress(node); err == nil {
				out.Hostname = addr.InternalHostname
				out.InternalURL = networking.ServiceInternalURL(addr.InternalHostname, portStr, protocol)
				out.PublicURL = networking.ServicePublicURL(addr.InternalHostname, proxyPort, active.HostPort, protocol)
			}
		}
	} else {
		out.Hostname = active.Hostname
		out.InternalURL = networking.ServiceInternalURL(active.Hostname, portStr, protocol)
		out.PublicURL = networking.ServicePublicURL(active.Hostname, proxyPort, active.HostPort, protocol)
	}

	if active.ContainerID == "" || (active.Status != "running" && active.Status != "starting") {
		return out, nil
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return out, nil
	}
	defer cli.Close()

	inspect, err := cli.ContainerInspect(ctx, active.ContainerID)
	if err != nil {
		return out, nil
	}
	if inspect.State != nil && inspect.State.Health != nil {
		out.DockerHealth = inspect.State.Health.Status
	}
	return out, nil
}
