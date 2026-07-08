package store

import (
	"fmt"
	"strings"
)

// ListProjectEnvVars returns the project-level env vars for projectID, ordered
// by key for stable display.
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

// ProjectSecretEntry is a project_env_var with secret=true plus project metadata.
type ProjectSecretEntry struct {
	ProjectID   uint   `json:"projectId"`
	ProjectName string `json:"projectName"`
	Key         string `json:"key"`
	Value       string `json:"value"`
	Scope       string `json:"scope"`
	Secret      bool   `json:"secret"`
}

// ListAllProjectSecrets returns every project secret across all projects.
func (s *Store) ListAllProjectSecrets() ([]ProjectSecretEntry, error) {
	var rows []ProjectEnvVar
	if err := s.DB.Where("secret = ?", true).Order("project_id asc, key asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		return nil, nil
	}
	projects, err := s.ListProjects()
	if err != nil {
		return nil, err
	}
	nameByID := make(map[uint]string, len(projects))
	for _, p := range projects {
		nameByID[p.ID] = p.Name
	}
	out := make([]ProjectSecretEntry, len(rows))
	for i, row := range rows {
		out[i] = ProjectSecretEntry{
			ProjectID:   row.ProjectID,
			ProjectName: nameByID[row.ProjectID],
			Key:         row.Key,
			Value:       row.Value,
			Scope:       row.Scope,
			Secret:      row.Secret,
		}
	}
	return out, nil
}

// DeleteProjectEnvVars removes every project-level env var for a project.
func (s *Store) DeleteProjectEnvVars(projectID uint) error {
	return s.DB.Where("project_id = ?", projectID).Delete(&ProjectEnvVar{}).Error
}

// SetProjectEnvVarSecret toggles the secret flag on a project-level env var.
func (s *Store) SetProjectEnvVarSecret(projectID uint, key string, secret bool) error {
	return s.DB.Model(&ProjectEnvVar{}).
		Where("project_id = ? AND key = ?", projectID, key).
		Update("secret", secret).Error
}

// SetProjectEnvVarValue updates just the value of a project-level env var.
func (s *Store) SetProjectEnvVarValue(projectID uint, key, value string) error {
	return s.DB.Model(&ProjectEnvVar{}).
		Where("project_id = ? AND key = ?", projectID, key).
		Update("value", value).Error
}
