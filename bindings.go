package main

import (
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"Draft/internal/deploy"
	"Draft/internal/dockerdesktop"
	"Draft/internal/dockerfile"
	"Draft/internal/dockerwatch"
	"Draft/internal/githooks"
	"Draft/internal/gitsrc"
	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types"
)

// errNoStore is returned to the frontend when the database failed to open.
var errNoStore = errors.New("database is not available")

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

// EnvironmentServices mirrors deploy.EnvironmentServices for Wails bindings.
type EnvironmentServices struct {
	ID         uint             `json:"id"`
	Name       string           `json:"name"`
	Slug       string           `json:"slug"`
	IsDefault  bool             `json:"isDefault"`
	Services   []ProjectService `json:"services"`
	Running    int              `json:"running"`
	Stopped    int              `json:"stopped"`
	Building   int              `json:"building"`
	Failed     int              `json:"failed"`
	Status     string           `json:"status"`
	LastActive *time.Time       `json:"lastActive,omitempty"`
}

// ProjectServicesSummary mirrors deploy.ProjectServicesSummary for Wails.
type ProjectServicesSummary struct {
	ProjectID    uint                  `json:"projectId"`
	Status       string                `json:"status"`
	Environments []EnvironmentServices `json:"environments"`
	Services     []ProjectService      `json:"services"`
	LastActive   *time.Time            `json:"lastActive,omitempty"`
}

// AppSettings is the persisted app preferences surface.
type AppSettings struct {
	CompactSidebar        bool   `json:"compactSidebar"`
	LocalDomainPreference string `json:"localDomainPreference"`
}

func samePath(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(a), `/\`), strings.TrimRight(strings.TrimSpace(b), `/\`))
}

type GitHookStatus struct {
	Supported     bool `json:"supported"`
	CommitForeign bool `json:"commitForeign"`
	PushForeign   bool `json:"pushForeign"`
	PullForeign   bool `json:"pullForeign"`
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

// CreateProject creates a project from the create-project dialog.
func (a *App) CreateProject(name, path, description string) (*store.Project, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.CreateProject(name, path, description)
}

// ListProjects returns all registered projects.
func (a *App) ListProjects() ([]store.Project, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.ListProjects()
}

func (a *App) CreateNode(id, label string, projectID, environmentID uint, x, y float64) (*store.CanvasNode, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	node := &store.CanvasNode{
		ID:            id,
		Label:         label,
		ProjectID:     projectID,
		EnvironmentID: environmentID,
		X:             x,
		Y:             y,
	}
	return a.store.CreateNode(node)
}

// CreateEnvironment adds a new, non-default environment to a project.
func (a *App) CreateEnvironment(projectID uint, name string) (*store.Environment, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.CreateEnvironment(projectID, name)
}

// ListEnvironments returns every environment in a project, default first.
func (a *App) ListEnvironments(projectID uint) ([]store.Environment, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.ListEnvironments(projectID)
}

// GetDefaultEnvironment returns a project's default ("Main") environment.
func (a *App) GetDefaultEnvironment(projectID uint) (*store.Environment, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.GetDefaultEnvironment(projectID)
}

// RenameEnvironment updates an environment's display name.
func (a *App) RenameEnvironment(id uint, name string) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.RenameEnvironment(id, name)
}

// SetDefaultEnvironment marks the given environment as the project default.
func (a *App) SetDefaultEnvironment(environmentID uint) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.SetDefaultEnvironment(environmentID)
}

// StartEnvironment deploys every service in the environment (no start-order gating).
func (a *App) StartEnvironment(environmentID uint) (*deploy.EnvironmentStackResult, error) {
	return a.runEnvironmentStack(environmentID, deploy.StackStart)
}

// StopEnvironment stops every service in the environment.
func (a *App) StopEnvironment(environmentID uint) (*deploy.EnvironmentStackResult, error) {
	return a.runEnvironmentStack(environmentID, deploy.StackStop)
}

// RedeployEnvironment redeploys every service in the environment.
func (a *App) RedeployEnvironment(environmentID uint) (*deploy.EnvironmentStackResult, error) {
	return a.runEnvironmentStack(environmentID, deploy.StackRedeploy)
}

func (a *App) runEnvironmentStack(environmentID uint, action deploy.EnvironmentStackAction) (*deploy.EnvironmentStackResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.RunEnvironmentStack(a.ctx, environmentID, string(action))
}

// PreviewSync returns a read-only settings/env diff between source and target.
func (a *App) PreviewSync(req deploy.SyncRequest) (*deploy.SyncPreview, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.PreviewSync(a.ctx, req)
}

// ApplySync stages (and optionally redeploys) config from source onto target.
// mode is "stage" or "stageAndRedeploy".
func (a *App) ApplySync(req deploy.SyncRequest, mode string) (*deploy.SyncApplyResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ApplySync(a.ctx, req, mode)
}

// DeleteEnvironment stops/removes the environment's containers and network,
// then deletes it and everything scoped to it. Fails on the default or only
// remaining environment in a project.
func (a *App) DeleteEnvironment(environmentID uint) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.DeleteEnvironment(a.ctx, environmentID)
}

// DuplicateEnvironment clones every node (settings + env vars, fresh UIDs) in
// sourceEnvironmentID into a brand new environment named newName. choices
// control per-stateful-service data mode (fresh / share / clone); nil or empty
// means all fresh. Deployment history, routes, and port leases are not copied.
func (a *App) DuplicateEnvironment(sourceEnvironmentID uint, newName string, choices []deploy.ServiceDataChoice) (*store.Environment, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.DuplicateEnvironment(a.ctx, sourceEnvironmentID, newName, choices)
}

// PreviewEnvironmentDuplicate lists stateful services for the new-env data wizard.
func (a *App) PreviewEnvironmentDuplicate(sourceEnvironmentID uint) ([]deploy.StatefulServiceSummary, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.PreviewEnvironmentDuplicate(a.ctx, sourceEnvironmentID)
}

// Sandbox configuration is stored at project scope; a profile can optionally
// be scoped to a particular source environment.
func (a *App) GetSandboxProjectSettings(projectID uint) (*store.SandboxProjectSettings, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.GetSandboxProjectSettings(projectID)
}

func (a *App) SaveSandboxProjectSettings(settings store.SandboxProjectSettings) (*store.SandboxProjectSettings, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.SaveSandboxProjectSettings(settings)
}

func (a *App) ListSandboxProfiles(projectID uint) ([]store.SandboxProfile, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.ListSandboxProfiles(projectID)
}

func (a *App) SaveSandboxProfile(profile store.SandboxProfile) (*store.SandboxProfile, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.SaveSandboxProfile(profile)
}

func (a *App) DeleteSandboxProfile(profileID uint) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.DeleteSandboxProfile(profileID)
}

func (a *App) ListSandboxes(projectID uint) ([]store.Sandbox, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.ListSandboxes(projectID)
}

func (a *App) PreviewSandbox(req deploy.SandboxCreateRequest) (*deploy.SandboxPreview, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.PreviewSandbox(a.ctx, req)
}

func (a *App) CreateSandbox(req deploy.SandboxCreateRequest) (*store.Sandbox, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.CreateSandbox(a.ctx, req)
}

// ExtendSandbox pushes a sandbox expiry forward by ttlHours from now.
func (a *App) ExtendSandbox(sandboxID uint, ttlHours int) (*store.Sandbox, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ExtendSandbox(a.ctx, sandboxID, ttlHours)
}

// DeleteSandbox removes every sandbox-owned Docker resource, including
// Draft-managed volumes; unlike ordinary environment deletion, no data is kept.
func (a *App) DeleteSandbox(sandboxID uint) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.DeleteSandbox(a.ctx, sandboxID)
}

func (a *App) GetSandboxDetail(sandboxID uint) (*deploy.SandboxDetail, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.GetSandboxDetail(a.ctx, sandboxID)
}

func (a *App) SuspendSandbox(sandboxID uint) (*store.Sandbox, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.SuspendSandbox(a.ctx, sandboxID)
}

func (a *App) ResumeSandbox(sandboxID uint) (*store.Sandbox, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ResumeSandbox(a.ctx, sandboxID)
}

// GetLinkedServiceInfo returns whether a node is a virtualized link and its root.
func (a *App) GetLinkedServiceInfo(nodeID string) (*deploy.LinkedServiceInfo, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.GetLinkedServiceInfo(a.ctx, nodeID)
}

// PromoteLinkedService turns a linked alias into a real local service.
// seed is "empty" or "clone"; consistency is "consistent" or "quick" when cloning.
func (a *App) PromoteLinkedService(nodeID, seed, consistency string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.PromoteLinkedService(a.ctx, nodeID, seed, deploy.CloneConsistency(consistency))
}

// UnlinkService removes a service link. become is "fresh" or "delete".
func (a *App) UnlinkService(nodeID, become string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.UnlinkService(a.ctx, nodeID, become)
}

// ListShareableRoots returns root services that can be share/clone sources.
func (a *App) ListShareableRoots(projectID, excludeEnvironmentID uint) ([]deploy.RootServiceSummary, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ListShareableRoots(a.ctx, projectID, excludeEnvironmentID)
}

// PreviewCloneVolume describes a late volume clone before confirmation.
func (a *App) PreviewCloneVolume(targetNodeID, sourceNodeID, containerPath string) (*deploy.CloneVolumePreview, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.PreviewCloneVolume(a.ctx, targetNodeID, sourceNodeID, containerPath)
}

// CloneVolumeData copies a root volume into the target service (safe staging promote).
func (a *App) CloneVolumeData(targetNodeID, sourceNodeID, containerPath, consistency string) (*deploy.CloneVolumeResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.CloneVolumeData(a.ctx, targetNodeID, sourceNodeID, containerPath, deploy.CloneConsistency(consistency))
}

// CreateNodeFromTemplate stamps a new service node out of a template via the
// daemon (which owns the deploy engine that resolves {{draft.*}} at stamp
// time). Returns the created node plus non-fatal warnings (e.g. an existing
// Dockerfile was left unchanged).
func (a *App) CreateNodeFromTemplate(req deploy.CreateNodeFromTemplateRequest) (*deploy.CreateNodeFromTemplateResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.CreateNodeFromTemplate(a.ctx, req)
}

// ImportConfigPreview parses a cloud config file (docker-compose, Cloud Run,
// ECS, Container Apps) and reports what would be imported, writing nothing.
func (a *App) ImportConfigPreview(path string) (*deploy.ImportPreview, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ImportConfigPreview(a.ctx, path)
}

// ImportConfigAsProject creates a new project from a config file and stamps one
// service node per service in the file.
func (a *App) ImportConfigAsProject(path, projectName string) (*deploy.ImportResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ImportConfigAsProject(a.ctx, path, projectName)
}

// ImportConfigIntoProject stamps a config file's services as nodes into an
// existing project's environment, anchored at the given canvas coordinates.
func (a *App) ImportConfigIntoProject(projectID, environmentID uint, path string, x, y float64) (*deploy.ImportResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ImportConfigIntoProject(a.ctx, projectID, environmentID, path, x, y)
}

// ExportConfig serializes a single service to the requested cloud format,
// returning the generated file(s) and a fidelity report.
func (a *App) ExportConfig(nodeID, format string) (*deploy.ExportResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ExportConfig(a.ctx, nodeID, format)
}

// ExportProjectConfig serializes every service in a project to the requested
// cloud format.
func (a *App) ExportProjectConfig(projectID uint, format string) (*deploy.ExportResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ExportProjectConfig(a.ctx, projectID, format)
}

// ExportConfigToPath exports a service and writes the generated file(s) into
// destDir.
func (a *App) ExportConfigToPath(nodeID, format, destDir string) (*deploy.ExportResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ExportConfigToPath(a.ctx, nodeID, format, destDir)
}

func (a *App) UpdateNode(id string, x, y float64, label string) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.UpdateNode(id, x, y, label)
}

func (a *App) DeleteNode(id string) error {
	if a.store == nil {
		return errNoStore
	}
	node, err := a.store.GetNode(id)
	if err != nil {
		return err
	}
	repoRoot, _ := a.store.CachedGitRepoRoot(id)
	if repoRoot == "" {
		repoRoot, _ = a.store.ResolveGitRepoRoot(a.ctx, id, node.ProjectID)
	}
	c, err := a.ensureDaemon()
	if err != nil {
		return a.deleteNodeWithStoreFallback(id, repoRoot, err)
	}
	if c == nil {
		return errNoStore
	}
	if err := c.DeleteService(a.ctx, id); err != nil {
		if err2 := a.deleteNodeWithStoreFallback(id, repoRoot, err); err2 == nil {
			return nil
		}
		return err
	}
	if repoRoot != "" {
		return githooks.ReconcileRepoHooks(a.ctx, a.store, repoRoot)
	}
	return nil
}

func (a *App) deleteNodeWithStoreFallback(id, repoRoot string, cause error) error {
	if cause != nil && !isDaemonUnavailable(cause) {
		return cause
	}
	if err := deploy.DeleteServiceFromStore(a.store, id); err != nil {
		return err
	}
	if repoRoot != "" {
		return githooks.ReconcileRepoHooks(a.ctx, a.store, repoRoot)
	}
	return nil
}

func isDaemonUnavailable(err error) bool {
	if err == nil {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "404") ||
		strings.Contains(msg, "connection refused") ||
		strings.Contains(msg, "daemon did not become ready")
}

// PreviewDeleteService summarizes what deleting a service will stop, remove,
// and leave behind — especially other services whose variables still reference
// this one.
func (a *App) PreviewDeleteService(nodeID string) (*deploy.DeleteServicePreview, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	preview, err := deploy.PreviewDeleteServiceFromStore(a.store, nodeID)
	if err != nil {
		return nil, err
	}
	// Prefer live Docker volume count when the daemon is up-to-date.
	if c, err := a.ensureDaemon(); err == nil && c != nil {
		if live, err := c.PreviewDeleteService(a.ctx, nodeID); err == nil && live != nil {
			preview.ManagedVolumeCount = live.ManagedVolumeCount
			preview.IsRunning = live.IsRunning
			if live.Dependents != nil {
				preview.Dependents = live.Dependents
			}
		}
	}
	return preview, nil
}

// ListReferenceIssues returns unresolved @{Label.ATTR} tokens in nodeID's env
// vars (unknown service or missing variable on a known service).
func (a *App) ListReferenceIssues(nodeID string) ([]deploy.ReferenceIssue, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return deploy.ListReferenceIssuesFromStore(a.store, nodeID)
}

// ListNodesWithReferenceIssues returns node IDs in the environment whose env
// vars contain at least one broken @{Label.ATTR} reference.
func (a *App) ListNodesWithReferenceIssues(environmentID int) ([]string, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return deploy.ListNodesWithReferenceIssues(a.store, uint(environmentID))
}

// ListNodes returns every node in a single environment (not the whole
// project — use ListEnvironments to enumerate a project's environments).
func (a *App) ListNodes(environmentID uint) ([]store.CanvasNode, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.ListNodesByEnvironment(environmentID)
}

// GetNode returns a single canvas node by id, including its TemplateID so the
// frontend can render the template icon and apply the template's schema.
func (a *App) GetNode(id string) (*store.CanvasNode, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.GetNode(id)
}

func (a *App) ListProjectServices(projectID uint) ([]ProjectService, error) {
	summary, err := a.ListProjectServicesSummary(projectID)
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

// ListProjectServicesSummary returns every environment's services for dashboard cards.
func (a *App) ListProjectServicesSummary(projectID uint) (*ProjectServicesSummary, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	summary, err := deploy.ListProjectServicesSummary(a.store, projectID)
	if err != nil {
		return nil, err
	}
	return mapProjectServicesSummary(summary), nil
}

func mapProjectService(svc deploy.ProjectService) ProjectService {
	return ProjectService{
		ID:              svc.ID,
		ProjectID:       svc.ProjectID,
		EnvironmentID:   svc.EnvironmentID,
		EnvironmentName: svc.EnvironmentName,
		Name:            svc.Name,
		Type:            svc.Type,
		Image:           svc.Image,
		Port:            svc.Port,
		Status:          svc.Status,
		Hostname:        svc.Hostname,
		HostPort:        svc.HostPort,
		Dockerfile:      svc.Dockerfile,
		ServiceRoot:     svc.ServiceRoot,
		UpdatedAt:       svc.UpdatedAt,
	}
}

func mapProjectServicesSummary(summary *deploy.ProjectServicesSummary) *ProjectServicesSummary {
	if summary == nil {
		return &ProjectServicesSummary{Environments: []EnvironmentServices{}, Services: []ProjectService{}}
	}
	out := &ProjectServicesSummary{
		ProjectID:    summary.ProjectID,
		Status:       summary.Status,
		LastActive:   summary.LastActive,
		Environments: make([]EnvironmentServices, 0, len(summary.Environments)),
		Services:     make([]ProjectService, 0, len(summary.Services)),
	}
	for _, env := range summary.Environments {
		mapped := EnvironmentServices{
			ID:         env.ID,
			Name:       env.Name,
			Slug:       env.Slug,
			IsDefault:  env.IsDefault,
			Running:    env.Running,
			Stopped:    env.Stopped,
			Building:   env.Building,
			Failed:     env.Failed,
			Status:     env.Status,
			LastActive: env.LastActive,
			Services:   make([]ProjectService, 0, len(env.Services)),
		}
		for _, svc := range env.Services {
			mapped.Services = append(mapped.Services, mapProjectService(svc))
		}
		out.Environments = append(out.Environments, mapped)
	}
	for _, svc := range summary.Services {
		out.Services = append(out.Services, mapProjectService(svc))
	}
	return out
}

func (a *App) DeployService(nodeID string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.Deploy(a.ctx, nodeID)
}

// ListManagedVolumes returns Draft-managed Docker volumes. When nodeID is empty,
// all Draft-managed volumes in the (optional) project scope are returned; when
// nodeID is set, only that node's volumes. nodeId filtering matches the
// draft.node label stamped at volume creation, so it still works after a node
// row is deleted — that's how the "keep and surface" deletion policy finds
// orphaned volumes later.
func (a *App) ListManagedVolumes(projectID int, nodeID string) ([]deploy.ManagedVolume, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	var pid *uint
	if projectID > 0 {
		p := uint(projectID)
		pid = &p
	}
	return c.ListManagedVolumes(a.ctx, pid, nodeID)
}

// ListVolumesOverview returns every Draft-managed Docker volume across all
// projects, enriched with its owning node's label and an orphaned flag (the
// node was deleted but Draft kept the data). Backs the global Volumes tab.
func (a *App) ListVolumesOverview() ([]deploy.VolumeOverview, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ListVolumesOverview(a.ctx)
}

// GetDockerDiskUsage returns the daemon-wide disk usage breakdown (images,
// containers, volumes, build cache). Backs the Docker tab's summary bar.
func (a *App) GetDockerDiskUsage() (types.DiskUsage, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return types.DiskUsage{}, err
	}
	if c == nil {
		return types.DiskUsage{}, errNoStore
	}
	return c.SystemDF(a.ctx)
}

// ListDockerContainers returns every container on the daemon, Draft-managed
// or not. Backs the Docker tab's Containers section.
func (a *App) ListDockerContainers() ([]deploy.ContainerSummary, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ListContainers(a.ctx)
}

func (a *App) StartDockerContainer(id string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.StartContainer(a.ctx, id)
}

func (a *App) StopDockerContainer(id string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.StopContainer(a.ctx, id)
}

func (a *App) RestartDockerContainer(id string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.RestartContainer(a.ctx, id)
}

func (a *App) RemoveDockerContainer(id string, force bool) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.RemoveContainer(a.ctx, id, force)
}

// ListDockerImages returns every image on the daemon. Backs the Docker tab's
// Images section.
func (a *App) ListDockerImages() ([]deploy.ImageSummary, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ListImages(a.ctx)
}

func (a *App) RemoveDockerImage(id string, force bool) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.RemoveImage(a.ctx, id, force)
}

// ListDockerNetworks returns every network on the daemon. Backs the Docker
// tab's Networks section.
func (a *App) ListDockerNetworks() ([]deploy.NetworkSummary, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ListNetworks(a.ctx)
}

func (a *App) RemoveDockerNetwork(id string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.RemoveNetwork(a.ctx, id)
}

// ListAllDockerVolumes returns every volume on the daemon, Draft-managed or
// not — the unrestricted counterpart to ListVolumesOverview. Backs the Docker
// tab's Volumes section (the standalone Volumes nav tab keeps using
// ListVolumesOverview for its Draft-only, orphan-aware view).
func (a *App) ListAllDockerVolumes() ([]deploy.VolumeOverview, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ListAllVolumes(a.ctx)
}

// RemoveDockerVolume removes any Docker volume by name, unlike
// DeleteManagedVolume which only allows removal of Draft-managed volumes.
func (a *App) RemoveDockerVolume(name string, force bool) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.RemoveVolume(a.ctx, name, force)
}

// PruneDocker runs a scoped or unscoped prune for one resource type
// ("containers" | "images" | "networks" | "volumes" | "buildcache").
// draftOnly restricts removal to Draft-managed/Draft-built resources where
// that distinction is meaningful.
func (a *App) PruneDocker(resource string, draftOnly bool) (deploy.PruneReport, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return deploy.PruneReport{}, err
	}
	if c == nil {
		return deploy.PruneReport{}, errNoStore
	}
	return c.PruneDocker(a.ctx, resource, draftOnly)
}

// RouteRow is the frontend-facing route row: a Route enriched with the owning
// project name and service label. Defined here in package main (rather than
// reusing daemon.RouteRow) so the generated Wails model lands in the `main`
// namespace alongside the other frontend bindings.
type RouteRow struct {
	Hostname    string `json:"hostname"`
	ProjectID   uint   `json:"projectId"`
	NodeID      string `json:"nodeId"`
	Environment string `json:"environment"`
	Protocol    string `json:"protocol"`
	TargetHost  string `json:"targetHost"`
	TargetPort  int    `json:"targetPort"`
	HostPort    int    `json:"hostPort"`
	ProjectName string `json:"projectName"`
	ServiceName string `json:"serviceName"`
}

// ListRoutes returns every Route, optionally filtered by projectId. Each row is
// enriched with the owning project name and service label so the Routes tab can
// render a single table without a per-row lookup.
func (a *App) ListRoutes(projectId *uint) ([]RouteRow, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	rows, err := c.ListRoutes(a.ctx, projectId)
	if err != nil {
		return nil, err
	}
	out := make([]RouteRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, RouteRow{
			Hostname:    r.Hostname,
			ProjectID:   r.ProjectID,
			NodeID:      r.NodeID,
			Environment: r.Environment,
			Protocol:    r.Protocol,
			TargetHost:  r.TargetHost,
			TargetPort:  r.TargetPort,
			HostPort:    r.HostPort,
			ProjectName: r.ProjectName,
			ServiceName: r.ServiceName,
		})
	}
	return out, nil
}

// DeleteManagedVolume removes a Draft-managed Docker volume by name. Only
// volumes labelled draft.managed=true may be removed through this path, so an
// arbitrary Docker volume can't be nuked by name. force removes the volume even
// if a container still references it; callers should default to false.
func (a *App) DeleteManagedVolume(name string, force bool) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.DeleteManagedVolume(a.ctx, name, force)
}

func (a *App) StopService(nodeID string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.Stop(a.ctx, nodeID)
}

func (a *App) RestartService(nodeID string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.Restart(a.ctx, nodeID)
}

func (a *App) GetDeployments(nodeID string) ([]store.Deployment, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.GetDeployments(a.ctx, nodeID)
}

func (a *App) GetActiveDeployment(nodeID string) (*store.Deployment, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.GetActiveDeployment(a.ctx, nodeID)
}

func (a *App) GetBuildLog(deploymentID uint) (string, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", errNoStore
	}
	return c.GetBuildLog(a.ctx, deploymentID)
}

func (a *App) StartLogStream(nodeID string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.StartLogStream(a.ctx, nodeID)
}

func (a *App) StopLogStream(nodeID string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.StopLogStream(a.ctx, nodeID)
}

// GetEnvVars returns Draft-managed environment variables for a node.
func (a *App) GetEnvVars(nodeID string) ([]store.EnvVar, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.GetEnvVars(a.ctx, nodeID)
}

// SetEnvVar writes or updates a Draft-managed environment variable.
func (a *App) SetEnvVar(nodeID, key, value string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.SetEnvVar(a.ctx, nodeID, key, value)
}

// DeleteEnvVar removes a single Draft-managed environment variable.
func (a *App) DeleteEnvVar(nodeID, key string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.DeleteEnvVar(a.ctx, nodeID, key)
}

func (a *App) SetEnvVarScope(nodeID, key, scope string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.SetEnvVarScope(a.ctx, nodeID, key, scope)
}

func (a *App) ExportEnvFile(nodeID string) (store.EnvFileSyncResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return store.EnvFileSyncResult{}, err
	}
	if c == nil {
		return store.EnvFileSyncResult{}, errNoStore
	}
	return c.ExportEnvFile(a.ctx, nodeID)
}

func (a *App) ImportEnvFile(nodeID, path string) (store.EnvFileSyncResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return store.EnvFileSyncResult{}, err
	}
	if c == nil {
		return store.EnvFileSyncResult{}, errNoStore
	}
	return c.ImportEnvFile(a.ctx, nodeID, path)
}

func (a *App) RefreshEnvFile(nodeID string) (store.EnvFileSyncResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return store.EnvFileSyncResult{}, err
	}
	if c == nil {
		return store.EnvFileSyncResult{}, errNoStore
	}
	return c.RefreshEnvFile(a.ctx, nodeID)
}

// SuggestEnvFile returns the absolute path to a .env file found in the service root, if one exists.
func (a *App) SuggestEnvFile(nodeID string, projectID int) (string, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", errNoStore
	}
	return c.SuggestEnvFile(a.ctx, nodeID, uint(projectID))
}

// PreviewEnvVars resolves nodeID's env vars (including @{Label.ATTR} variable
// references to other services) for read-only display, tolerating per-key
// errors so one broken reference doesn't hide the rest.
func (a *App) PreviewEnvVars(nodeID string) (map[string]deploy.EnvPreview, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.PreviewEnvVars(a.ctx, nodeID)
}

// ListReferenceTargets returns the other services in nodeID's project along with
// the address attributes and custom variable keys that can be referenced from
// it without creating a circular dependency.
func (a *App) ListReferenceTargets(nodeID string) ([]deploy.ReferenceTarget, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ListReferenceTargets(a.ctx, nodeID)
}

// GetEnvironmentConnections returns the read-only edges derived from variable
// references across every node in the environment, for the canvas to render.
func (a *App) GetEnvironmentConnections(environmentID int) ([]deploy.Connection, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.GetEnvironmentConnections(a.ctx, uint(environmentID))
}

func (a *App) GetServiceMetrics(nodeID string) (deploy.ServiceMetrics, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return deploy.ServiceMetrics{}, err
	}
	if c == nil {
		return deploy.ServiceMetrics{}, errNoStore
	}
	return c.GetServiceMetrics(a.ctx, nodeID)
}

// GetNodeHealth returns a lightweight health + URL snapshot for a service node
// (one Docker inspect, no reachability probe). Used by the canvas to render a
// health chip and a clickable URL on each node without the full metrics call.
func (a *App) GetNodeHealth(nodeID string) (deploy.NodeHealth, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return deploy.NodeHealth{}, err
	}
	if c == nil {
		return deploy.NodeHealth{}, errNoStore
	}
	return c.GetNodeHealth(a.ctx, nodeID)
}

// ReapplyTemplate re-stamps a node from the template it was created from,
// flowing template updates through while preserving user-owned env vars.
func (a *App) ReapplyTemplate(nodeID string) (deploy.CreateNodeFromTemplateResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return deploy.CreateNodeFromTemplateResult{}, err
	}
	if c == nil {
		return deploy.CreateNodeFromTemplateResult{}, errNoStore
	}
	return c.ReapplyTemplate(a.ctx, nodeID)
}

// RollbackDeployment re-runs a historical deployment's image, creating a new
// deployment row visible in history.
func (a *App) RollbackDeployment(deploymentID uint) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.RollbackDeployment(a.ctx, deploymentID)
}

// RollbackEligibility reports per-deployment whether rollback is possible and
// why, so the UI can enable/disable per-row redeploy buttons.
func (a *App) RollbackEligibility(nodeID string) ([]deploy.RollbackEligibility, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.RollbackEligibility(a.ctx, nodeID)
}

// RunCommand executes a one-shot command in the node's active container and
// returns captured output + exit code. Used by the per-service "Run" bar.
func (a *App) RunCommand(nodeID string, cmd []string, workDir string) (deploy.RunCommandResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return deploy.RunCommandResult{}, err
	}
	if c == nil {
		return deploy.RunCommandResult{}, errNoStore
	}
	return c.RunCommand(a.ctx, nodeID, cmd, workDir)
}

// UpdateProject edits a project's name/description (path is not editable).
func (a *App) UpdateProject(id uint, name, description string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.UpdateProject(a.ctx, id, name, description)
}

// DeleteProject removes a project and every service in it. Draft-managed
// volumes are left in place and surface as orphans in the Volumes tab.
func (a *App) DeleteProject(id uint) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.DeleteProject(a.ctx, id)
}

// ListProjectEnvVars returns project-level shared values referenced via
// {{project.KEY}} from services in the project.
func (a *App) ListProjectEnvVars(projectID uint) ([]store.ProjectEnvVar, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ListProjectEnvVars(a.ctx, projectID)
}

// SetProjectEnvVar upserts a project-level env var.
func (a *App) SetProjectEnvVar(projectID uint, key, value, scope string, secret bool) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.SetProjectEnvVar(a.ctx, projectID, key, value, scope, secret)
}

// DeleteProjectEnvVar removes a project-level env var.
func (a *App) DeleteProjectEnvVar(projectID uint, key string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.DeleteProjectEnvVar(a.ctx, projectID, key)
}

// DaemonConnection returns the daemon's 127.0.0.1 address and auth token so the
// frontend can open a direct WebSocket to it (the interactive shell). Browsers
// cannot set custom headers on a WS handshake, so the token is passed as a query
// param on those endpoints.
func (a *App) DaemonConnection() (DaemonConnectionInfo, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return DaemonConnectionInfo{}, err
	}
	if c == nil {
		return DaemonConnectionInfo{}, errNoStore
	}
	state := c.State()
	return DaemonConnectionInfo{Addr: state.Addr, Token: state.Token}, nil
}

// DaemonConnectionInfo is the address + auth token needed to talk to the
// daemon directly (e.g. opening a WebSocket for the interactive shell).
type DaemonConnectionInfo struct {
	Addr  string `json:"addr"`
	Token string `json:"token"`
}

// CheckDocker returns the last known Docker daemon status from the watcher.
// The frontend calls this once on mount for an immediate value, then relies on
// the "docker:status" Wails event for subsequent changes (no polling).
func (a *App) CheckDocker() dockerwatch.DaemonStatus {
	c, err := a.ensureDaemon()
	if err != nil || c == nil {
		return dockerwatch.DaemonStatus{State: "stopped", Error: errString(err)}
	}
	status, err := c.CheckDocker(a.ctx)
	if err != nil {
		return dockerwatch.DaemonStatus{State: "stopped", Error: err.Error()}
	}
	return status
}

// StartDocker launches the local Docker runtime so the watcher can reconnect
// and publish the normal "docker:status" transition once the daemon comes up.
func (a *App) StartDocker() error {
	return dockerdesktop.Start(a.ctx)
}

func (a *App) GetLocalDomainStatus() networking.LocalDomainStatus {
	c, err := a.ensureDaemon()
	if err != nil || c == nil {
		status := networking.LocalDomainStatus{
			Mode:           "localhost-port",
			HostsError:     errString(err),
			PublicSuffix:   networking.PublicSuffix,
			LoopbackSuffix: networking.PublicSuffix,
		}
		return a.applyLocalDomainPreference(status)
	}
	status, err := c.LocalDomainStatus(a.ctx)
	if err != nil {
		status = networking.LocalDomainStatus{
			Mode:           "localhost-port",
			HostsError:     err.Error(),
			PublicSuffix:   networking.PublicSuffix,
			LoopbackSuffix: networking.PublicSuffix,
		}
	}
	return a.applyLocalDomainPreference(status)
}

func (a *App) applyLocalDomainPreference(status networking.LocalDomainStatus) networking.LocalDomainStatus {
	if a.store == nil {
		return status
	}
	pref, err := a.store.GetAppSetting(store.AppSettingLocalDomainPreference)
	if err != nil || pref == "" || pref == store.LocalDomainPrefAuto {
		return status
	}
	switch pref {
	case store.LocalDomainPrefLocalhost:
		status.Mode = "localhost-port"
	case store.LocalDomainPrefPublic:
		// Only claim public mode when the proxy actually has a port.
		if status.ProxyPort > 0 {
			status.Mode = "public-hostname-port"
		} else {
			status.Mode = "localhost-port"
		}
	}
	return status
}

// GetAppSettings returns persisted app preferences with defaults filled in.
func (a *App) GetAppSettings() (*AppSettings, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	all, err := a.store.ListAppSettings()
	if err != nil {
		return nil, err
	}
	return &AppSettings{
		CompactSidebar:        all[store.AppSettingCompactSidebar] == "true",
		LocalDomainPreference: all[store.AppSettingLocalDomainPreference],
	}, nil
}

// SetAppSettings merges the provided preferences into the store.
func (a *App) SetAppSettings(settings AppSettings) (*AppSettings, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	compact := "false"
	if settings.CompactSidebar {
		compact = "true"
	}
	pref := settings.LocalDomainPreference
	if pref == "" {
		pref = store.LocalDomainPrefAuto
	}
	if err := a.store.SetAppSettings(map[string]string{
		store.AppSettingCompactSidebar:        compact,
		store.AppSettingLocalDomainPreference: pref,
	}); err != nil {
		return nil, err
	}
	return a.GetAppSettings()
}

// GetNodeSettings returns all settings for a node as a map.
func (a *App) GetNodeSettings(nodeID string) (map[string]string, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.GetNodeSettings(nodeID)
}

// GetNodeConfigStatus returns applied settings, staged overrides, and deploy status.
func (a *App) GetNodeConfigStatus(nodeID string) (*deploy.NodeConfigStatus, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return deploy.NodeConfigStatusFromStore(a.store, nodeID)
}

// InspectDockerfileBuildInfo returns the ARG/build-step structure of a node's
// Dockerfile so the Variables tab can warn about build-time env wiring
// (client-inlined vars left runtime-only, or build args the Dockerfile never
// declares). Returns BuildMode=false for image-mode services.
func (a *App) InspectDockerfileBuildInfo(nodeID string) (*deploy.DockerfileBuildInfo, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return deploy.InspectDockerfileBuildInfo(a.store, nodeID)
}

// StageNodeSettings persists setting overrides until the next successful deploy.
// Git/deploy-automation keys are applied immediately instead of staged.
func (a *App) StageNodeSettings(nodeID string, projectID uint, settings map[string]string) error {
	if a.store == nil {
		return errNoStore
	}
	if len(settings) == 0 {
		return nil
	}
	staged := map[string]string{}
	for key, value := range settings {
		if store.IsImmediateSetting(key) {
			if err := a.applyImmediateNodeSetting(nodeID, projectID, key, value); err != nil {
				return err
			}
			continue
		}
		staged[key] = value
	}
	if len(staged) == 0 {
		return nil
	}
	if root, ok := staged["service_root"]; ok {
		project, err := a.store.GetProject(projectID)
		if err != nil {
			return fmt.Errorf("project not found: %w", err)
		}
		if err := store.ValidateInsideProject(project.Path, root); err != nil {
			return err
		}
		rel, _ := filepath.Rel(project.Path, root)
		staged["service_root"] = rel
	}
	return a.store.StageNodeSettings(nodeID, staged)
}

func (a *App) applyImmediateNodeSetting(nodeID string, projectID uint, key, value string) error {
	switch key {
	case "deploy_trigger":
		trigger := value
		if trigger == "" {
			trigger = "manual"
		}
		return a.SetDeployTrigger(nodeID, projectID, trigger)
	case "redeploy_on_pull":
		return a.SetRedeployOnPull(nodeID, projectID, value == "true")
	case "service_root":
		return a.SetServiceRoot(nodeID, projectID, value)
	default:
		return a.store.SetNodeSetting(nodeID, key, value)
	}
}

// StageEnvVarChanges persists env var upserts and deletions until deploy.
func (a *App) StageEnvVarChanges(nodeID string, upserts []store.EnvVarStageUpsert, deleteKeys []string) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.StageEnvVarChanges(nodeID, upserts, deleteKeys)
}

// DiscardStagedChanges clears all staged settings and env rows for a node.
func (a *App) DiscardStagedChanges(nodeID string) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.DiscardAllStagedChanges(nodeID)
}

// PreviewStagedChanges returns warnings/errors for proposed staged settings.
func (a *App) PreviewStagedChanges(nodeID string, proposedSettings map[string]string) (*deploy.StagedChangePreview, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return deploy.PreviewStagedChangesFromStore(a.store, nodeID, proposedSettings)
}

// SetNodeSetting upserts a single setting for a node.
func (a *App) SetNodeSetting(nodeID, key, value string) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.SetNodeSetting(nodeID, key, value)
}

// SetServiceRoot validates that the given root path is inside the project
// folder, then persists it as the "service_root" setting for the node.
func (a *App) SetServiceRoot(nodeID string, projectID uint, rootPath string) error {
	if a.store == nil {
		return errNoStore
	}
	oldRoot, _ := a.store.CachedGitRepoRoot(nodeID)
	if oldRoot == "" {
		oldRoot, _ = a.store.ResolveGitRepoRoot(a.ctx, nodeID, projectID)
	}
	if err := a.store.SetServiceRoot(nodeID, projectID, rootPath); err != nil {
		return err
	}
	newRoot, err := a.store.ResolveGitRepoRoot(a.ctx, nodeID, projectID)
	if err != nil && err != gitsrc.ErrNotRepo {
		return err
	}
	if oldRoot != "" && !samePath(oldRoot, newRoot) {
		if err := githooks.ReconcileRepoHooks(a.ctx, a.store, oldRoot); err != nil {
			return err
		}
	}
	if newRoot != "" {
		return githooks.ReconcileRepoHooks(a.ctx, a.store, newRoot)
	}
	return nil
}

// GetServiceRoot returns the resolved absolute path of the service root,
// or empty string if not set.
func (a *App) GetServiceRoot(nodeID string, projectID uint) (string, error) {
	if a.store == nil {
		return "", errNoStore
	}
	return a.store.GetServiceRoot(nodeID, projectID)
}

// SelectServiceRoot opens a native folder picker scoped to the project
// directory. Returns the selected path or empty if cancelled. Validates
// the selection is inside the project folder.
func (a *App) SelectServiceRoot(projectID uint) (string, error) {
	if a.store == nil {
		return "", errNoStore
	}

	project, err := a.store.GetProject(projectID)
	if err != nil {
		return "", fmt.Errorf("project not found: %w", err)
	}

	selected, err := a.selectFolder(project.Path)
	if err != nil {
		return "", err
	}
	if selected == "" {
		return "", nil
	}

	if err := store.ValidateInsideProject(project.Path, selected); err != nil {
		return "", err
	}

	return selected, nil
}

// ParseDockerfileExpose reads a Dockerfile and returns its EXPOSE ports.
// The Dockerfile path is resolved relative to the project root if not absolute.
func (a *App) ParseDockerfileExpose(dockerfilePath string, projectID uint) ([]dockerfile.ExposePort, error) {
	if a.store == nil {
		return nil, errNoStore
	}

	absPath := dockerfilePath
	if !filepath.IsAbs(dockerfilePath) {
		project, err := a.store.GetProject(projectID)
		if err != nil {
			return nil, fmt.Errorf("project not found: %w", err)
		}
		absPath = filepath.Join(project.Path, dockerfilePath)
	}

	ports, err := dockerfile.ParseExposePorts(absPath)
	if err != nil {
		return nil, err
	}
	if ports == nil {
		return []dockerfile.ExposePort{}, nil
	}
	return ports, nil
}

// IsGitRepo reports whether the given node resolves to a git repository.
// Used by the UI to decide whether to offer git-branch deploys.
func (a *App) IsGitRepo(nodeID string, projectID uint) (bool, error) {
	if a.store == nil {
		return false, errNoStore
	}
	repoRoot, err := a.store.ResolveGitRepoRoot(a.ctx, nodeID, projectID)
	if err == gitsrc.ErrNotRepo {
		return false, nil
	}
	return err == nil && gitsrc.IsRepo(repoRoot), err
}

// ListGitBranches returns the local and remote-tracking branch names for the
// node's resolved git repository, for populating the branch picker. Returns an
// empty slice (not an error) if the node is not in a git repository.
func (a *App) ListGitBranches(nodeID string, projectID uint) ([]string, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	repoRoot, err := a.store.ResolveGitRepoRoot(a.ctx, nodeID, projectID)
	if err == gitsrc.ErrNotRepo {
		return []string{}, nil
	}
	if err != nil || !gitsrc.IsRepo(repoRoot) {
		return []string{}, nil
	}
	branches, err := gitsrc.ListBranches(a.ctx, repoRoot)
	if err != nil {
		return nil, err
	}
	if branches == nil {
		return []string{}, nil
	}
	return branches, nil
}

// SetDeployTrigger records how a node should be deployed — manually, on every
// commit to its tracked branch, or on push of its tracked branch.
func (a *App) SetDeployTrigger(nodeID string, projectID uint, trigger string) error {
	if a.store == nil {
		return errNoStore
	}
	return githooks.SetDeployTrigger(a.ctx, a.store, nodeID, projectID, trigger)
}

// SetRedeployOnPull toggles the independent "redeploy on pull" behavior for a
// node: when enabled, Draft installs a post-merge git hook so the service
// redeploys whenever a `git pull` (or merge) updates its tracked branch. It is
// orthogonal to the 3-way deploy trigger.
func (a *App) SetRedeployOnPull(nodeID string, projectID uint, enabled bool) error {
	if a.store == nil {
		return errNoStore
	}
	return githooks.SetRedeployOnPull(a.ctx, a.store, nodeID, projectID, enabled)
}

// GetGitHookStatus reports whether the node's resolved repo supports
// git-triggered deploys and whether foreign hooks are present.
func (a *App) GetGitHookStatus(nodeID string, projectID uint) (GitHookStatus, error) {
	if a.store == nil {
		return GitHookStatus{}, errNoStore
	}
	_ = projectID
	status, err := githooks.StatusForNode(a.ctx, a.store, nodeID)
	if err != nil {
		return GitHookStatus{}, err
	}
	return GitHookStatus{
		Supported:     status.Supported,
		CommitForeign: status.CommitForeign,
		PushForeign:   status.PushForeign,
		PullForeign:   status.PullForeign,
	}, nil
}

// ── Service templates ───────────────────────────────────────────────────────

// ListServiceTemplates returns the global template library: built-ins first,
// then user templates, each group ordered by name.
func (a *App) ListServiceTemplates() ([]store.ServiceTemplate, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.ListTemplates()
}

// GetServiceTemplate returns a single template by ID.
func (a *App) GetServiceTemplate(id uint) (*store.ServiceTemplate, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.GetTemplate(id)
}

// CreateServiceTemplate inserts a new user template. Builtin is forced false.
func (a *App) CreateServiceTemplate(t store.ServiceTemplate) (*store.ServiceTemplate, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.CreateTemplate(&t)
}

// UpdateServiceTemplate updates a user template. Built-ins are rejected.
func (a *App) UpdateServiceTemplate(t store.ServiceTemplate) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.UpdateTemplate(&t)
}

// DeleteServiceTemplate removes a user template. Built-ins are rejected.
func (a *App) DeleteServiceTemplate(id uint) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.DeleteTemplate(id)
}

// CloneServiceTemplate produces a user-owned copy of a template (built-in or
// user) with a " (copy)" name suffix, and returns it for immediate editing.
func (a *App) CloneServiceTemplate(id uint) (*store.ServiceTemplate, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.CloneTemplate(id)
}

func (a *App) ListAppSecrets() ([]store.AppSecret, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ListAppSecrets(a.ctx)
}

func (a *App) SetAppSecret(key, value, description string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.SetAppSecret(a.ctx, key, value, description)
}

func (a *App) DeleteAppSecret(key string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.DeleteAppSecret(a.ctx, key)
}

func (a *App) ListAppSecretUsages(key string) ([]deploy.SecretUsage, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ListAppSecretUsages(a.ctx, key)
}

func (a *App) ListProjectEnvVarUsages(projectID uint, key string) ([]deploy.SecretUsage, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.ListProjectEnvVarUsages(a.ctx, projectID, key)
}
