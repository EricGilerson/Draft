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
)

// errNoStore is returned to the frontend when the database failed to open.
var errNoStore = errors.New("database is not available")

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

func (a *App) CreateNode(id, label string, projectID uint, x, y float64) (*store.CanvasNode, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	node := &store.CanvasNode{
		ID:        id,
		Label:     label,
		ProjectID: projectID,
		X:         x,
		Y:         y,
	}
	return a.store.CreateNode(node)
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
	if err := a.store.DeleteNode(id); err != nil {
		return err
	}
	if err := a.store.DeleteNodeSettings(id); err != nil {
		return err
	}
	if repoRoot != "" {
		return githooks.ReconcileRepoHooks(a.ctx, a.store, repoRoot)
	}
	return nil
}

func (a *App) ListNodes(projectID uint) ([]store.CanvasNode, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.ListNodes(projectID)
}

func (a *App) ListProjectServices(projectID uint) ([]ProjectService, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	services, err := deploy.ListProjectServices(a.store, projectID)
	if err != nil {
		return nil, err
	}
	out := make([]ProjectService, 0, len(services))
	for _, svc := range services {
		out = append(out, ProjectService{
			ID:          svc.ID,
			ProjectID:   svc.ProjectID,
			Name:        svc.Name,
			Type:        svc.Type,
			Image:       svc.Image,
			Port:        svc.Port,
			Status:      svc.Status,
			Hostname:    svc.Hostname,
			HostPort:    svc.HostPort,
			Dockerfile:  svc.Dockerfile,
			ServiceRoot: svc.ServiceRoot,
			UpdatedAt:   svc.UpdatedAt,
		})
	}
	return out, nil
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

// ListReferenceTargets returns the other services in nodeID's project that can
// be referenced from it (excluding ones that would create a reference cycle),
// along with what's available to reference on each: address attributes plus
// custom variable keys.
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

// GetProjectConnections returns the read-only edges derived from variable
// references across every node in the project, for the canvas to render.
func (a *App) GetProjectConnections(projectID int) ([]deploy.Connection, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.GetProjectConnections(a.ctx, uint(projectID))
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
		return networking.LocalDomainStatus{
			Mode:           "localhost-port",
			HostsError:     errString(err),
			PublicSuffix:   networking.PublicSuffix,
			LoopbackSuffix: networking.PublicSuffix,
		}
	}
	status, err := c.LocalDomainStatus(a.ctx)
	if err != nil {
		return networking.LocalDomainStatus{
			Mode:           "localhost-port",
			HostsError:     err.Error(),
			PublicSuffix:   networking.PublicSuffix,
			LoopbackSuffix: networking.PublicSuffix,
		}
	}
	return status
}

// GetNodeSettings returns all settings for a node as a map.
func (a *App) GetNodeSettings(nodeID string) (map[string]string, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.GetNodeSettings(nodeID)
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
