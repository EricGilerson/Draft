package store

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

var ErrInvalidSandboxProfile = errors.New("sandbox profile name is required")
var ErrSandboxNotFound = errors.New("sandbox not found")

func (s *Store) GetSandboxProjectSettings(projectID uint) (*SandboxProjectSettings, error) {
	settings := &SandboxProjectSettings{ProjectID: projectID, DefaultTTLHours: 168, WarningHours: 24, GraceHours: 72}
	err := s.DB.FirstOrCreate(settings, SandboxProjectSettings{ProjectID: projectID}).Error
	return settings, err
}

func (s *Store) SaveSandboxProjectSettings(settings SandboxProjectSettings) (*SandboxProjectSettings, error) {
	if settings.ProjectID == 0 || settings.DefaultTTLHours <= 0 || settings.WarningHours < 0 || settings.GraceHours < 0 || settings.SuspendIdleHours < 0 {
		return nil, errors.New("invalid sandbox project settings")
	}
	if err := s.DB.Save(&settings).Error; err != nil {
		return nil, err
	}
	return &settings, nil
}

func (s *Store) ListSandboxProfiles(projectID uint) ([]SandboxProfile, error) {
	var profiles []SandboxProfile
	err := s.DB.Where("project_id = ?", projectID).Order("source_environment_id desc, is_default desc, name asc").Find(&profiles).Error
	return profiles, err
}

func (s *Store) GetSandboxProfile(id uint) (*SandboxProfile, error) {
	var profile SandboxProfile
	if err := s.DB.First(&profile, id).Error; err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *Store) SaveSandboxProfile(profile SandboxProfile) (*SandboxProfile, error) {
	profile.Name = strings.TrimSpace(profile.Name)
	profile.Description = strings.TrimSpace(profile.Description)
	profile.PlanJSON = strings.TrimSpace(profile.PlanJSON)
	if profile.ProjectID == 0 || profile.Name == "" {
		return nil, ErrInvalidSandboxProfile
	}
	if profile.PlanJSON == "" {
		profile.PlanJSON = "{}"
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if profile.IsDefault {
			if err := tx.Model(&SandboxProfile{}).Where("project_id = ? AND source_environment_id = ?", profile.ProjectID, profile.SourceEnvironmentID).Update("is_default", false).Error; err != nil {
				return err
			}
		}
		return tx.Save(&profile).Error
	}); err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *Store) DeleteSandboxProfile(id uint) error { return s.DB.Delete(&SandboxProfile{}, id).Error }

// DefaultSandboxProfile prefers a source-environment-specific default and
// falls back to the project-wide default.
func (s *Store) DefaultSandboxProfile(projectID, sourceEnvironmentID uint) (*SandboxProfile, error) {
	var profile SandboxProfile
	err := s.DB.Where("project_id = ? AND source_environment_id = ? AND is_default = ?", projectID, sourceEnvironmentID, true).First(&profile).Error
	if err == nil {
		return &profile, nil
	}
	if !errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, err
	}
	err = s.DB.Where("project_id = ? AND source_environment_id = ? AND is_default = ?", projectID, 0, true).First(&profile).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return &profile, nil
}

func (s *Store) CreateSandbox(sandbox *Sandbox, links []SandboxLink, repositories []SandboxRepositorySource) (*Sandbox, error) {
	if sandbox.ProjectID == 0 || sandbox.EnvironmentID == 0 || sandbox.SourceEnvironmentID == 0 || strings.TrimSpace(sandbox.Name) == "" || strings.TrimSpace(sandbox.PlanJSON) == "" || sandbox.ExpiresAt.IsZero() {
		return nil, errors.New("invalid sandbox")
	}
	if sandbox.Status == "" {
		sandbox.Status = "active"
	}
	if err := s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Create(sandbox).Error; err != nil {
			return err
		}
		for i := range links {
			links[i].SandboxID = sandbox.ID
			links[i].Kind = strings.TrimSpace(links[i].Kind)
			links[i].Value = strings.TrimSpace(links[i].Value)
			if links[i].Kind == "" || links[i].Value == "" {
				return errors.New("sandbox links require kind and value")
			}
			if err := tx.Create(&links[i]).Error; err != nil {
				return err
			}
		}
		for i := range repositories {
			repositories[i].SandboxID = sandbox.ID
			repositories[i].RepoRoot = strings.TrimSpace(repositories[i].RepoRoot)
			if repositories[i].RepoRoot == "" {
				return errors.New("sandbox repository source requires repo root")
			}
			if err := tx.Create(&repositories[i]).Error; err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return nil, err
	}
	return sandbox, nil
}

func (s *Store) GetSandbox(id uint) (*Sandbox, error) {
	var sandbox Sandbox
	if err := s.DB.First(&sandbox, id).Error; err != nil {
		return nil, err
	}
	return &sandbox, nil
}
func (s *Store) GetSandboxByEnvironment(environmentID uint) (*Sandbox, error) {
	var sandbox Sandbox
	if err := s.DB.Where("environment_id = ?", environmentID).First(&sandbox).Error; err != nil {
		return nil, err
	}
	return &sandbox, nil
}
func (s *Store) ListSandboxes(projectID uint) ([]Sandbox, error) {
	var sandboxes []Sandbox
	err := s.DB.Where("project_id = ?", projectID).Order("expires_at asc").Find(&sandboxes).Error
	return sandboxes, err
}
func (s *Store) ListSandboxLinks(sandboxID uint) ([]SandboxLink, error) {
	var links []SandboxLink
	err := s.DB.Where("sandbox_id = ?", sandboxID).Order("created_at asc").Find(&links).Error
	return links, err
}
func (s *Store) ListSandboxRepositorySources(sandboxID uint) ([]SandboxRepositorySource, error) {
	var rows []SandboxRepositorySource
	err := s.DB.Where("sandbox_id = ?", sandboxID).Order("repo_root asc").Find(&rows).Error
	return rows, err
}
func (s *Store) UpdateSandboxStatus(id uint, status string, suspendedAt *time.Time) error {
	return s.DB.Model(&Sandbox{}).Where("id = ?", id).Updates(map[string]any{"status": status, "suspended_at": suspendedAt}).Error
}
func (s *Store) ExtendSandbox(id uint, expiresAt, warnAt, graceEndsAt time.Time) error {
	return s.DB.Model(&Sandbox{}).Where("id = ?", id).Updates(map[string]any{"status": "active", "expires_at": expiresAt, "warn_at": warnAt, "grace_ends_at": graceEndsAt, "suspended_at": nil}).Error
}
func (s *Store) DueSandboxes(now time.Time) ([]Sandbox, error) {
	var rows []Sandbox
	err := s.DB.Where("status IN ? AND expires_at <= ?", []string{"active", "warning"}, now).Order("expires_at asc").Find(&rows).Error
	return rows, err
}
func (s *Store) DeleteSandboxByEnvironment(environmentID uint) error {
	sandbox, err := s.GetSandboxByEnvironment(environmentID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil
	}
	if err != nil {
		return err
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("sandbox_id = ?", sandbox.ID).Delete(&SandboxLink{}).Error; err != nil {
			return err
		}
		if err := tx.Where("sandbox_id = ?", sandbox.ID).Delete(&SandboxRepositorySource{}).Error; err != nil {
			return err
		}
		return tx.Delete(&Sandbox{}, sandbox.ID).Error
	})
}
