package main

import (
	"fmt"
	"path/filepath"
	"strings"

	"Draft/internal/dockerfile"
	"Draft/internal/gitsrc"
)

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

	project, err := a.store.GetProject(projectID)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}

	if err := validateInsideProject(project.Path, rootPath); err != nil {
		return err
	}

	rel, _ := filepath.Rel(project.Path, rootPath)
	return a.store.SetNodeSetting(nodeID, "service_root", rel)
}

// GetServiceRoot returns the resolved absolute path of the service root,
// or empty string if not set.
func (a *App) GetServiceRoot(nodeID string, projectID uint) (string, error) {
	if a.store == nil {
		return "", errNoStore
	}

	rel, err := a.store.GetNodeSetting(nodeID, "service_root")
	if err != nil {
		return "", nil
	}
	if rel == "" {
		return "", nil
	}

	project, err := a.store.GetProject(projectID)
	if err != nil {
		return "", err
	}

	return filepath.Join(project.Path, rel), nil
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

	if err := validateInsideProject(project.Path, selected); err != nil {
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

// IsGitRepo reports whether the given project's directory is a git repository.
// Used by the UI to decide whether to offer git-branch deploys.
func (a *App) IsGitRepo(projectID uint) (bool, error) {
	if a.store == nil {
		return false, errNoStore
	}
	project, err := a.store.GetProject(projectID)
	if err != nil {
		return false, fmt.Errorf("project not found: %w", err)
	}
	return gitsrc.IsRepo(project.Path), nil
}

// ListGitBranches returns the local and remote-tracking branch names for the
// given project's git repository, for populating the branch picker. Returns an
// empty slice (not an error) if the project is not a git repository.
func (a *App) ListGitBranches(projectID uint) ([]string, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	project, err := a.store.GetProject(projectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	if !gitsrc.IsRepo(project.Path) {
		return []string{}, nil
	}
	branches, err := gitsrc.ListBranches(a.ctx, project.Path)
	if err != nil {
		return nil, err
	}
	if branches == nil {
		return []string{}, nil
	}
	return branches, nil
}

func validateInsideProject(projectPath, targetPath string) error {
	absProject, err := filepath.Abs(projectPath)
	if err != nil {
		return fmt.Errorf("resolve project path: %w", err)
	}
	absTarget, err := filepath.Abs(targetPath)
	if err != nil {
		return fmt.Errorf("resolve target path: %w", err)
	}

	absProject = filepath.Clean(absProject)
	absTarget = filepath.Clean(absTarget)

	if !strings.EqualFold(absTarget, absProject) &&
		!strings.HasPrefix(strings.ToLower(absTarget), strings.ToLower(absProject)+string(filepath.Separator)) {
		return fmt.Errorf("path must be inside the project folder (%s)", absProject)
	}
	return nil
}
