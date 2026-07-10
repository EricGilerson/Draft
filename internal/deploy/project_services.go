package deploy

import (
	"strconv"
	"strings"
	"time"

	"Draft/internal/store"
)

type ProjectService struct {
	ID              string    `json:"id"`
	ProjectID       uint      `json:"projectId"`
	EnvironmentID   uint      `json:"environmentId"`
	EnvironmentName string    `json:"environmentName"`
	Name            string    `json:"name"`
	Type            string    `json:"type"`
	Image           string    `json:"image"`
	Port            int       `json:"port"`
	Status          string    `json:"status"`
	Hostname        string    `json:"hostname"`
	HostPort        int       `json:"hostPort"`
	Dockerfile      string    `json:"dockerfile"`
	ServiceRoot     string    `json:"serviceRoot"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

// EnvironmentServices is the per-environment rollup used on project cards and
// the Overview dashboard.
type EnvironmentServices struct {
	ID        uint             `json:"id"`
	Name      string           `json:"name"`
	Slug      string           `json:"slug"`
	IsDefault bool             `json:"isDefault"`
	Services  []ProjectService `json:"services"`
	Running   int              `json:"running"`
	Stopped   int              `json:"stopped"`
	Building  int              `json:"building"`
	Failed    int              `json:"failed"`
	// Status is active|partial|stopped for this environment alone.
	Status     string     `json:"status"`
	LastActive *time.Time `json:"lastActive,omitempty"`
}

// ProjectServicesSummary is the multi-environment projection for a project.
type ProjectServicesSummary struct {
	ProjectID    uint                  `json:"projectId"`
	Status       string                `json:"status"` // active|partial|stopped
	Environments []EnvironmentServices `json:"environments"`
	// Services is a flat list of every service across environments (activity
	// resolution, total counts). Prefer Environments for UI grouping.
	Services   []ProjectService `json:"services"`
	LastActive *time.Time       `json:"lastActive,omitempty"`
}

// ListProjectServices projects the project's default environment only.
// Prefer ListProjectServicesSummary for dashboard cards that must show all envs.
func ListProjectServices(s *store.Store, projectID uint) ([]ProjectService, error) {
	summary, err := ListProjectServicesSummary(s, projectID)
	if err != nil {
		return nil, err
	}
	for _, env := range summary.Environments {
		if env.IsDefault {
			return env.Services, nil
		}
	}
	if len(summary.Environments) > 0 {
		return summary.Environments[0].Services, nil
	}
	return []ProjectService{}, nil
}

// ListProjectServicesSummary projects every environment in the project with
// per-env service status and overall project status.
func ListProjectServicesSummary(s *store.Store, projectID uint) (*ProjectServicesSummary, error) {
	envs, err := s.ListEnvironments(projectID)
	if err != nil {
		return nil, err
	}
	out := &ProjectServicesSummary{
		ProjectID:    projectID,
		Environments: make([]EnvironmentServices, 0, len(envs)),
		Services:     []ProjectService{},
		Status:       "stopped",
	}
	var latest *time.Time
	nonEmpty := 0
	fullyRunning := 0
	anyLive := false
	anyPartial := false

	for _, env := range envs {
		envSum, err := listEnvironmentServices(s, env)
		if err != nil {
			return nil, err
		}
		out.Environments = append(out.Environments, envSum)
		out.Services = append(out.Services, envSum.Services...)
		if envSum.LastActive != nil && (latest == nil || envSum.LastActive.After(*latest)) {
			t := *envSum.LastActive
			latest = &t
		}
		if len(envSum.Services) == 0 {
			continue
		}
		nonEmpty++
		switch envSum.Status {
		case "active":
			fullyRunning++
			anyLive = true
		case "partial":
			anyLive = true
			anyPartial = true
		}
	}
	out.LastActive = latest
	out.Status = summarizeProjectStatus(nonEmpty, fullyRunning, anyLive, anyPartial)
	return out, nil
}

func listEnvironmentServices(s *store.Store, env store.Environment) (EnvironmentServices, error) {
	nodes, err := s.ListNodesByEnvironment(env.ID)
	if err != nil {
		return EnvironmentServices{}, err
	}
	sum := EnvironmentServices{
		ID:        env.ID,
		Name:      env.Name,
		Slug:      env.Slug,
		IsDefault: env.IsDefault,
		Services:  make([]ProjectService, 0, len(nodes)),
		Status:    "stopped",
	}
	var latest *time.Time
	for _, node := range nodes {
		settings, err := s.GetNodeSettings(node.ID)
		if err != nil {
			return EnvironmentServices{}, err
		}
		dep, err := s.ActiveDeployment(node.ID)
		if err != nil {
			return EnvironmentServices{}, err
		}
		svc := projectServiceFromNode(node, env, settings, dep)
		sum.Services = append(sum.Services, svc)
		switch svc.Status {
		case "running":
			sum.Running++
		case "failed", "error":
			sum.Failed++
			sum.Stopped++
		case "building", "built", "starting", "pending":
			sum.Building++
		default:
			sum.Stopped++
		}
		t := svc.UpdatedAt
		if latest == nil || t.After(*latest) {
			latest = &t
		}
	}
	sum.LastActive = latest
	sum.Status = summarizeServicesStatus(sum.Services)
	return sum, nil
}

func projectServiceFromNode(node store.CanvasNode, env store.Environment, settings map[string]string, dep *store.Deployment) ProjectService {
	port, _ := strconv.Atoi(strings.TrimSpace(settings["service_port"]))
	svc := ProjectService{
		ID:              node.ID,
		ProjectID:       node.ProjectID,
		EnvironmentID:   env.ID,
		EnvironmentName: env.Name,
		Name:            node.Label,
		Type:            serviceTypeFromPort(port),
		Image:           strings.TrimSpace(settings["dockerfile"]),
		Port:            port,
		Status:          "stopped",
		Dockerfile:      strings.TrimSpace(settings["dockerfile"]),
		ServiceRoot:     strings.TrimSpace(settings["service_root"]),
		UpdatedAt:       node.UpdatedAt,
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

func isLiveStatus(status string) bool {
	switch status {
	case "running", "starting", "building", "built", "pending":
		return true
	default:
		return false
	}
}

func summarizeServicesStatus(services []ProjectService) string {
	if len(services) == 0 {
		return "stopped"
	}
	running := 0
	live := 0
	for _, svc := range services {
		if svc.Status == "running" {
			running++
		}
		if isLiveStatus(svc.Status) {
			live++
		}
	}
	if live == 0 {
		return "stopped"
	}
	if running == len(services) {
		return "active"
	}
	return "partial"
}

// summarizeProjectStatus:
//   - stopped: nothing live in any non-empty env
//   - active: every non-empty env is fully running
//   - partial: anything else with activity (or mixed up/down)
func summarizeProjectStatus(nonEmpty, fullyRunning int, anyLive, anyPartial bool) string {
	if !anyLive {
		return "stopped"
	}
	if nonEmpty > 0 && fullyRunning == nonEmpty && !anyPartial {
		return "active"
	}
	return "partial"
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
