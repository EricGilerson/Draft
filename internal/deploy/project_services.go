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

func ListProjectServices(s *store.Store, projectID uint) ([]ProjectService, error) {
	nodes, err := s.ListNodes(projectID)
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
		Type:        inferServiceType(node.Label, settings),
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

func inferServiceType(label string, settings map[string]string) string {
	value := strings.ToLower(label + " " + settings["dockerfile"] + " " + settings["service_root"])
	switch {
	case strings.Contains(value, "postgres"), strings.Contains(value, "mysql"), strings.Contains(value, "database"), strings.Contains(value, " db"):
		return "database"
	case strings.Contains(value, "redis"), strings.Contains(value, "cache"):
		return "cache"
	case strings.Contains(value, "worker"), strings.Contains(value, "queue"), strings.Contains(value, "job"):
		return "worker"
	default:
		return "web"
	}
}

func ServiceStatusFromDeployment(status string) string {
	switch status {
	case "running":
		return "running"
	case "failed":
		return "error"
	case "building", "built", "starting", "pending":
		return "starting"
	default:
		return "stopped"
	}
}
