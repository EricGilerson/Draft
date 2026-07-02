package store

import (
	"context"
	"errors"
	"strings"
)

// ErrInvalidProject is returned when required project fields are missing.
var ErrInvalidProject = errors.New("project name and path are required")

// CreateProject inserts a new project after trimming and validating input.
func (s *Store) CreateProject(name, path, description string) (*Project, error) {
	name = strings.TrimSpace(name)
	path = strings.TrimSpace(path)
	if name == "" || path == "" {
		return nil, ErrInvalidProject
	}

	p := &Project{Name: name, Path: path, Description: strings.TrimSpace(description)}
	if err := s.DB.Create(p).Error; err != nil {
		return nil, err
	}
	return p, nil
}

// GetProject returns a single project by ID.
func (s *Store) GetProject(id uint) (*Project, error) {
	var p Project
	if err := s.DB.First(&p, id).Error; err != nil {
		return nil, err
	}
	return &p, nil
}

// GetProjectByPath returns the project registered at the given filesystem path,
// or nil if none matches. Paths are compared case-insensitively so a hook that
// reports a differently-cased drive letter (common on Windows) still resolves.
func (s *Store) GetProjectByPath(path string) (*Project, error) {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil, nil
	}
	var projects []Project
	if err := s.DB.Find(&projects).Error; err != nil {
		return nil, err
	}
	for i := range projects {
		if samePath(projects[i].Path, path) {
			return &projects[i], nil
		}
	}
	return nil, nil
}

// NodesByRepoRoot returns all nodes whose resolved repo root matches repoRoot.
func (s *Store) NodesByRepoRoot(ctx context.Context, repoRoot string) ([]CanvasNode, error) {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" {
		return nil, nil
	}
	projects, err := s.ListProjects()
	if err != nil {
		return nil, err
	}
	var matches []CanvasNode
	for _, project := range projects {
		nodes, err := s.ListNodes(project.ID)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			root, err := s.CachedGitRepoRoot(node.ID)
			if err != nil {
				return nil, err
			}
			if root == "" {
				root, err = s.ResolveGitRepoRoot(ctx, node.ID, project.ID)
				if err != nil {
					continue
				}
			}
			if samePath(root, repoRoot) {
				matches = append(matches, node)
			}
		}
	}
	return matches, nil
}

func samePath(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(a), `/\`), strings.TrimRight(strings.TrimSpace(b), `/\`))
}

// ListProjects returns all projects, newest first.
func (s *Store) ListProjects() ([]Project, error) {
	var projects []Project
	if err := s.DB.Order("created_at desc").Find(&projects).Error; err != nil {
		return nil, err
	}
	return projects, nil
}
