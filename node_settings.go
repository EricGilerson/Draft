package main

import (
	"fmt"
	"path/filepath"
	"strings"
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
