package store

import (
	"context"
	"errors"
	"strings"

	"gorm.io/gorm"
)

// ErrInvalidProject is returned when required project fields are missing.
var ErrInvalidProject = errors.New("project name and path are required")

// CreateProject inserts a new project after trimming and validating input,
// along with its default "Main" environment.
func (s *Store) CreateProject(name, path, description string) (*Project, error) {
	name = strings.TrimSpace(name)
	path = strings.TrimSpace(path)
	if name == "" || path == "" {
		return nil, ErrInvalidProject
	}

	p := &Project{Name: name, Path: path, Description: strings.TrimSpace(description)}
	err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(p).Error; err != nil {
			return err
		}
		env := &Environment{ProjectID: p.ID, Name: "Main", Slug: "main", IsDefault: true}
		return tx.Create(env).Error
	})
	if err != nil {
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

// UpdateProject edits a project's identity (name/description). Path is not
// editable here — it's the project's on-disk identity and changing it would
// orphan every service root, git repo, and .env path resolved against it.
func (s *Store) UpdateProject(id uint, name, description string) error {
	name = strings.TrimSpace(name)
	if name == "" {
		return ErrInvalidProject
	}
	return s.DB.Model(&Project{}).Where("id = ?", id).Updates(map[string]any{
		"name":        name,
		"description": strings.TrimSpace(description),
	}).Error
}

// DeleteProject cascades a project out of the store: every node and its
// settings/env/deployments, every route and port lease, project-level env
// vars, and finally the project row. The caller (engine) is responsible for
// stopping containers and removing Docker volumes/images first; this only
// handles the DB side.
func (s *Store) DeleteProject(id uint) error {
	nodes, err := s.ListNodes(id)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := s.DeleteNode(n.ID); err != nil {
			return err
		}
		if err := s.DeleteNodeSettings(n.ID); err != nil {
			return err
		}
		if err := s.DeleteEnvVarsByNode(n.ID); err != nil {
			return err
		}
	}
	if err := s.DB.Where("project_id = ?", id).Delete(&Deployment{}).Error; err != nil {
		return err
	}
	if err := s.DB.Where("project_id = ?", id).Delete(&Route{}).Error; err != nil {
		return err
	}
	if err := s.DB.Where("project_id = ?", id).Delete(&PortLease{}).Error; err != nil {
		return err
	}
	if err := s.DeleteProjectEnvVars(id); err != nil {
		return err
	}
	var sandboxIDs []uint
	if err := s.DB.Model(&Sandbox{}).Where("project_id = ?", id).Pluck("id", &sandboxIDs).Error; err != nil {
		return err
	}
	if len(sandboxIDs) > 0 {
		if err := s.DB.Where("sandbox_id IN ?", sandboxIDs).Delete(&SandboxLink{}).Error; err != nil {
			return err
		}
		if err := s.DB.Where("sandbox_id IN ?", sandboxIDs).Delete(&SandboxRepositorySource{}).Error; err != nil {
			return err
		}
	}
	if err := s.DB.Where("project_id = ?", id).Delete(&SandboxTestRun{}).Error; err != nil {
		return err
	}
	if err := s.DB.Where("project_id = ?", id).Delete(&Sandbox{}).Error; err != nil {
		return err
	}
	if err := s.DB.Where("project_id = ?", id).Delete(&SandboxProfile{}).Error; err != nil {
		return err
	}
	if err := s.DB.Delete(&SandboxProjectSettings{}, "project_id = ?", id).Error; err != nil {
		return err
	}
	if err := s.DB.Where("project_id = ?", id).Delete(&Environment{}).Error; err != nil {
		return err
	}
	return s.DB.Delete(&Project{}, "id = ?", id).Error
}
