package store

import (
	"fmt"
	"path/filepath"
	"strings"
)

// SetServiceRoot validates that rootPath is inside the project folder, then
// persists it as the node's "service_root" setting (stored as project-relative).
func (s *Store) SetServiceRoot(nodeID string, projectID uint, rootPath string) error {
	project, err := s.GetProject(projectID)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}

	if err := ValidateInsideProject(project.Path, rootPath); err != nil {
		return err
	}

	rel, _ := filepath.Rel(project.Path, rootPath)
	return s.SetNodeSetting(nodeID, "service_root", rel)
}

// GetServiceRoot resolves and returns the absolute service root path for the
// node, or an empty string when no service_root setting is stored.
func (s *Store) GetServiceRoot(nodeID string, projectID uint) (string, error) {
	rel, err := s.GetNodeSetting(nodeID, "service_root")
	if err != nil {
		return "", nil
	}
	if rel == "" {
		return "", nil
	}

	project, err := s.GetProject(projectID)
	if err != nil {
		return "", err
	}

	return ResolveUnderProject(project.Path, rel), nil
}

// ResolveUnderProject joins a project-relative path with the project root.
// Absolute inputs are returned cleaned; an empty setting returns the project path.
func ResolveUnderProject(projectPath, relOrAbs string) string {
	relOrAbs = strings.TrimSpace(relOrAbs)
	if relOrAbs == "" {
		return filepath.Clean(projectPath)
	}
	if filepath.IsAbs(relOrAbs) {
		return filepath.Clean(relOrAbs)
	}
	return filepath.Clean(filepath.Join(projectPath, relOrAbs))
}

// ValidateInsideProject checks that targetPath resolves to projectPath or a
// descendant directory within it.
func ValidateInsideProject(projectPath, targetPath string) error {
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
