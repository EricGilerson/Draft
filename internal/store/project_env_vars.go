package store

import (
	"fmt"
	"strings"
)

// ListProjectEnvVars returns project-level env vars for projectID, ordered by
// key for stable display.
func (s *Store) ListProjectEnvVars(projectID uint) ([]ProjectEnvVar, error) {
	var vars []ProjectEnvVar
	if err := s.DB.Where("project_id = ?", projectID).Order("key asc").Find(&vars).Error; err != nil {
		return nil, err
	}
	return vars, nil
}

// SetProjectEnvVar upserts a single project-level env var. An empty value is
// allowed (and meaningful). The key is trimmed; a blank key is rejected.
func (s *Store) SetProjectEnvVar(projectID uint, key, value, scope string, secret bool) error {
	key = strings.TrimSpace(key)
	if err := validateEnvKey(key); err != nil {
		return err
	}
	scope = strings.TrimSpace(scope)
	if scope == "" {
		scope = EnvScopeRuntime
	}
	switch scope {
	case EnvScopeRuntime, EnvScopeBuild, EnvScopeBoth:
	default:
		return fmt.Errorf("invalid env scope %q", scope)
	}
	v := ProjectEnvVar{ProjectID: projectID, Key: key, Value: value, Scope: scope, Secret: secret}
	return s.DB.Save(&v).Error
}

// DeleteProjectEnvVar removes a single project-level env var by key.
func (s *Store) DeleteProjectEnvVar(projectID uint, key string) error {
	return s.DB.Where("project_id = ? AND key = ?", projectID, key).Delete(&ProjectEnvVar{}).Error
}

// GetProjectEnvVar returns a single project-level env var.
func (s *Store) GetProjectEnvVar(projectID uint, key string) (ProjectEnvVar, error) {
	var v ProjectEnvVar
	err := s.DB.Where("project_id = ? AND key = ?", projectID, key).First(&v).Error
	return v, err
}

// DeleteProjectEnvVars removes every project-level env var for a project.
func (s *Store) DeleteProjectEnvVars(projectID uint) error {
	return s.DB.Where("project_id = ?", projectID).Delete(&ProjectEnvVar{}).Error
}

// SetProjectEnvVarValue updates just the value of a project-level env var.
func (s *Store) SetProjectEnvVarValue(projectID uint, key, value string) error {
	return s.DB.Model(&ProjectEnvVar{}).
		Where("project_id = ? AND key = ?", projectID, key).
		Update("value", value).Error
}
