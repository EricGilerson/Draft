package deploy

import (
	"strconv"
	"strings"
	"time"

	"Draft/internal/store"
)

type ProjectService struct {
	ID          string    `json:"id"`
	ProjectID   uint      `json:"projectId"`
	Name        string    `json:"name"`
	Type        string    `json:"type"`
	Image       string    `json:"image"`
	Port        int       `json:"port"`
	Status      string    `json:"status"`
	Hostname    string    `json:"hostname"`
	HostPort    int       `json:"hostPort"`
	Dockerfile  string    `json:"dockerfile"`
	ServiceRoot string    `json:"serviceRoot"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// ListProjectServices projects the project's default (main) environment for
// dashboard cards — other environments intentionally don't roll up here.
func ListProjectServices(s *store.Store, projectID uint) ([]ProjectService, error) {
	env, err := s.GetDefaultEnvironment(projectID)
	if err != nil {
		return nil, err
	}
	nodes, err := s.ListNodesByEnvironment(env.ID)
	if err != nil {
		return nil, err
	}
	services := make([]ProjectService, 0, len(nodes))
	for _, node := range nodes {
		settings, err := s.GetNodeSettings(node.ID)
		if err != nil {
			return nil, err
		}
		dep, err := s.ActiveDeployment(node.ID)
		if err != nil {
			return nil, err
		}
		services = append(services, projectServiceFromNode(node, settings, dep))
	}
	return services, nil
}

func projectServiceFromNode(node store.CanvasNode, settings map[string]string, dep *store.Deployment) ProjectService {
	port, _ := strconv.Atoi(strings.TrimSpace(settings["service_port"]))
	svc := ProjectService{
		ID:          node.ID,
		ProjectID:   node.ProjectID,
		Name:        node.Label,
		Type:        serviceTypeFromPort(port),
		Image:       strings.TrimSpace(settings["dockerfile"]),
		Port:        port,
		Status:      "stopped",
		Dockerfile:  strings.TrimSpace(settings["dockerfile"]),
		ServiceRoot: strings.TrimSpace(settings["service_root"]),
		UpdatedAt:   node.UpdatedAt,
	}
	if dep == nil {
		return svc
	}
	svc.Status = ServiceStatusFromDeployment(dep.Status)
	svc.Hostname = dep.Hostname
	svc.HostPort = dep.HostPort
	if dep.ImageTag != "" {
		svc.Image = dep.ImageTag
	}
	if dep.UpdatedAt.After(svc.UpdatedAt) {
		svc.UpdatedAt = dep.UpdatedAt
	}
	return svc
}

// ServiceStatusFromDeployment maps a deployment lifecycle status onto the
// status string shown on project cards and canvas node pills. Real lifecycle
// states are preserved so callers can distinguish building from starting.
func ServiceStatusFromDeployment(status string) string {
	switch status {
	case "running", "building", "built", "starting", "pending", "stopped", "failed", "interrupted":
		return status
	case "error":
		return "failed"
	default:
		return "stopped"
	}
}
