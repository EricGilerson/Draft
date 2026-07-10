package store

import (
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

var ErrInvalidEnvironment = errors.New("environment name is required")
var ErrEnvironmentNotFound = errors.New("no environment with that id")
var ErrCannotDeleteDefaultEnvironment = errors.New("cannot delete the default environment")
var ErrCannotDeleteOnlyEnvironment = errors.New("a project must have at least one environment")

// uniqueSlugForProject generates a Docker-name-safe slug for a new
// environment in projectID, appending "-2", "-3"... on collision so two
// environments in the same project never end up with the same slug (which
// would collide on hostnames, the Docker network name, and volume names).
func (s *Store) uniqueSlugForProject(projectID uint, base string) (string, error) {
	candidate := sanitizeSlugChars(base)
	if candidate == "" {
		candidate = "env"
	}
	root := candidate
	for i := 2; ; i++ {
		var count int64
		if err := s.DB.Model(&Environment{}).Where("project_id = ? AND slug = ?", projectID, candidate).Count(&count).Error; err != nil {
			return "", err
		}
		if count == 0 {
			return candidate, nil
		}
		candidate = root + "-" + strconv.Itoa(i)
	}
}

// CreateEnvironment inserts a new, non-default environment for projectID.
// The default environment is only ever created by CreateProject.
func (s *Store) CreateEnvironment(projectID uint, name string) (*Environment, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return nil, ErrInvalidEnvironment
	}
	slug, err := s.uniqueSlugForProject(projectID, name)
	if err != nil {
		return nil, err
	}
	env := &Environment{ProjectID: projectID, Name: name, Slug: slug, IsDefault: false}
	if err := s.DB.Create(env).Error; err != nil {
		return nil, err
	}
	return env, nil
}

// ListEnvironments returns every environment in a project, default first.
func (s *Store) ListEnvironments(projectID uint) ([]Environment, error) {
	var envs []Environment
	if err := s.DB.Where("project_id = ?", projectID).Order("is_default desc, created_at asc").Find(&envs).Error; err != nil {
		return nil, err
	}
	return envs, nil
}

// GetEnvironment returns a single environment by ID.
func (s *Store) GetEnvironment(id uint) (*Environment, error) {
	var env Environment
	if err := s.DB.First(&env, id).Error; err != nil {
		return nil, err
	}
	return &env, nil
}

// GetDefaultEnvironment returns a project's default ("Main") environment.
func (s *Store) GetDefaultEnvironment(projectID uint) (*Environment, error) {
	var env Environment
	if err := s.DB.Where("project_id = ? AND is_default = ?", projectID, true).First(&env).Error; err != nil {
		return nil, err
	}
	return &env, nil
}

// RenameEnvironment updates an environment's display name. Slug is immutable
// once created — it's baked into hostnames, the Docker network name, and
// volume names, so renaming it would orphan already-running containers.
func (s *Store) RenameEnvironment(id uint, newName string) error {
	newName = strings.TrimSpace(newName)
	if newName == "" {
		return ErrInvalidEnvironment
	}
	return s.DB.Model(&Environment{}).Where("id = ?", id).Update("name", newName).Error
}

// SetDefaultEnvironment marks environmentID as the project's default and
// clears the flag on every other environment in the same project. The default
// is what project cards emphasize and what open-project lands on.
func (s *Store) SetDefaultEnvironment(environmentID uint) error {
	env, err := s.GetEnvironment(environmentID)
	if err != nil {
		return err
	}
	if env.IsDefault {
		return nil
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&Environment{}).
			Where("project_id = ? AND is_default = ?", env.ProjectID, true).
			Update("is_default", false).Error; err != nil {
			return err
		}
		return tx.Model(&Environment{}).
			Where("id = ?", environmentID).
			Update("is_default", true).Error
	})
}

// DeleteEnvironment removes an environment and every node/setting/env-var/
// deployment/route/port-lease row scoped to it. The default environment can
// never be deleted, nor can a project's only remaining environment. The
// caller (engine) is responsible for stopping containers and removing Docker
// volumes/networks first; this only handles the DB side.
func (s *Store) DeleteEnvironment(id uint) error {
	env, err := s.GetEnvironment(id)
	if err != nil {
		return err
	}
	if env.IsDefault {
		return ErrCannotDeleteDefaultEnvironment
	}
	var siblingCount int64
	if err := s.DB.Model(&Environment{}).Where("project_id = ?", env.ProjectID).Count(&siblingCount).Error; err != nil {
		return err
	}
	if siblingCount <= 1 {
		return ErrCannotDeleteOnlyEnvironment
	}

	nodes, err := s.ListNodesByEnvironment(id)
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
		if err := s.DB.Where("node_id = ?", n.ID).Delete(&Deployment{}).Error; err != nil {
			return err
		}
		if err := s.DB.Where("node_id = ?", n.ID).Delete(&Route{}).Error; err != nil {
			return err
		}
		if err := s.DB.Where("node_id = ?", n.ID).Delete(&PortLease{}).Error; err != nil {
			return err
		}
	}
	if err := s.DeleteSandboxByEnvironment(id); err != nil {
		return err
	}
	// Environment-scoped profiles cannot remain meaningful once their source is
	// gone. Project-wide profiles (source_environment_id=0) are preserved.
	if err := s.DB.Where("source_environment_id = ?", id).Delete(&SandboxProfile{}).Error; err != nil {
		return err
	}
	return s.DB.Delete(&Environment{}, "id = ?", id).Error
}
