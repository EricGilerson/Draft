package deploy

import (
	"context"
	"strings"

	"Draft/internal/networking"
)

// NodeHealth is a lightweight, canvas-friendly subset of service state: just
// enough to render a health chip and a clickable URL on a service node without
// running the full metrics + reachability probe that GetServiceMetrics does.
// DockerHealth is the raw Docker healthcheck status ("starting"|"healthy"|
// "unhealthy"|"") — empty when the container has no healthcheck configured.
// RouteProtocol is "http" (default) or "tcp" and drives how PublicURL is shaped.
//
// Status reflects the live/active deployment (a previous container may still be
// running after a failed rebuild). LastDeploy* describe the most recent attempt
// so Overview and the canvas can surface silent failures without collapsing the
// runtime status to "failed".
type NodeHealth struct {
	NodeID             string `json:"nodeId"`
	Status             string `json:"status"`
	DockerHealth       string `json:"dockerHealth"`
	HostPort           int    `json:"hostPort"`
	Hostname           string `json:"hostname"`
	InternalURL        string `json:"internalUrl"`
	PublicURL          string `json:"publicUrl"`
	RouteProtocol      string `json:"routeProtocol"`
	LastDeploymentID   uint   `json:"lastDeploymentId,omitempty"`
	LastDeployStatus   string `json:"lastDeployStatus,omitempty"`
	LastDeployError    string `json:"lastDeployError,omitempty"`
	LastDeployFailed   bool   `json:"lastDeployFailed"`
	LastDeploySequence int    `json:"lastDeploySequence,omitempty"`
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

	latest, err := e.store.LatestDeployment(healthNodeID)
	if err != nil {
		return out, err
	}
	portStr := strings.TrimSpace(settings["service_port"])
	protocol := strings.TrimSpace(settings["route_protocol"])
	if protocol == "" {
		protocol = "http"
	}
	out.RouteProtocol = protocol

	// Always expose the newest deployment attempt — ActiveDeployment skips
	// failed/stopped rows so a bad rebuild would otherwise be invisible while
	// an older container keeps running.
	if latest != nil {
		out.LastDeploymentID = latest.ID
		out.LastDeployStatus = latest.Status
		out.LastDeployError = latest.Error
		out.LastDeploySequence = latest.Sequence
		out.LastDeployFailed = latest.Status == "failed"
	}

	active, err := e.store.ActiveDeployment(healthNodeID)
	if err != nil {
		return out, err
	}
	if active == nil && latest != nil {
		active = latest
	}
	if active == nil {
		return out, nil
	}

	out.Status = active.Status
	out.HostPort = active.HostPort
	hostname := active.Hostname
	if isLinked {
		if node, err := e.store.GetNode(nodeID); err == nil {
			if addr, err := e.computeNodeAddress(node); err == nil {
				hostname = addr.InternalHostname
			}
		}
	}
	out.Hostname = hostname
	out.InternalURL = networking.ServiceInternalURL(hostname, portStr, protocol)
	if e.router != nil {
		out.PublicURL = e.router.DisplayPublicURL(hostname, active.HostPort, protocol)
	} else {
		out.PublicURL = networking.ServicePublicURL(hostname, 0, active.HostPort, protocol)
	}

	if active.ContainerID == "" || (active.Status != "running" && active.Status != "starting") {
		return out, nil
	}

	cli, err := e.dockerClient()
	if err != nil {
		return out, nil
	}

	inspect, err := cli.ContainerInspect(ctx, active.ContainerID)
	if err != nil {
		return out, nil
	}
	if inspect.State != nil && inspect.State.Health != nil {
		out.DockerHealth = inspect.State.Health.Status
	}
	return out, nil
}
