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
	if strings.TrimSpace(sandbox.Purpose) == "" {
		sandbox.Purpose = "preview"
	}
	if sandbox.LastActivityAt == nil {
		now := time.Now().UTC()
		sandbox.LastActivityAt = &now
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

// ReplaceSandboxRepositorySources rewrites the frozen repo pins for a sandbox
// (used by refresh-to-tip). Links and the sandbox row itself are left alone.
func (s *Store) ReplaceSandboxRepositorySources(sandboxID uint, repositories []SandboxRepositorySource) error {
	if sandboxID == 0 {
		return errors.New("sandbox id required")
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("sandbox_id = ?", sandboxID).Delete(&SandboxRepositorySource{}).Error; err != nil {
			return err
		}
		for i := range repositories {
			repositories[i].ID = 0
			repositories[i].SandboxID = sandboxID
			repositories[i].RepoRoot = strings.TrimSpace(repositories[i].RepoRoot)
			if repositories[i].RepoRoot == "" {
				return errors.New("sandbox repository source requires repo root")
			}
			if err := tx.Create(&repositories[i]).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// UpdateSandboxPlanJSON replaces the immutable-at-create plan snapshot after a
// source refresh rewrites repository refs inside the plan.
func (s *Store) UpdateSandboxPlanJSON(sandboxID uint, planJSON string) error {
	planJSON = strings.TrimSpace(planJSON)
	if sandboxID == 0 || planJSON == "" {
		return errors.New("invalid sandbox plan update")
	}
	return s.DB.Model(&Sandbox{}).Where("id = ?", sandboxID).Update("plan_json", planJSON).Error
}
func (s *Store) UpdateSandboxStatus(id uint, status string, suspendedAt *time.Time) error {
	return s.DB.Model(&Sandbox{}).Where("id = ?", id).Updates(map[string]any{"status": status, "suspended_at": suspendedAt}).Error
}

// SaveSandboxCleanupFailure marks a sandbox cleanup_failed with inventory detail.
func (s *Store) SaveSandboxCleanupFailure(id uint, inventoryJSON, cleanupError string) error {
	now := time.Now().UTC()
	return s.DB.Model(&Sandbox{}).Where("id = ?", id).Updates(map[string]any{
		"status":                 "cleanup_failed",
		"cleanup_inventory_json": inventoryJSON,
		"cleanup_error":          cleanupError,
		"cleanup_attempted_at":   now,
	}).Error
}

// ClearSandboxCleanupDetail resets cleanup diagnostic fields after a successful purge.
func (s *Store) ClearSandboxCleanupDetail(id uint) error {
	return s.DB.Model(&Sandbox{}).Where("id = ?", id).Updates(map[string]any{
		"cleanup_inventory_json": "",
		"cleanup_error":          "",
		"cleanup_attempted_at":   nil,
	}).Error
}

// TouchSandboxActivity records recent use for idle auto-suspend.
func (s *Store) TouchSandboxActivity(id uint, at time.Time) error {
	return s.DB.Model(&Sandbox{}).Where("id = ?", id).Update("last_activity_at", at.UTC()).Error
}

func (s *Store) ExtendSandbox(id uint, expiresAt, warnAt, graceEndsAt time.Time) error {
	now := time.Now().UTC()
	return s.DB.Model(&Sandbox{}).Where("id = ?", id).Updates(map[string]any{
		"status":           "active",
		"expires_at":       expiresAt,
		"warn_at":          warnAt,
		"grace_ends_at":    graceEndsAt,
		"suspended_at":     nil,
		"last_activity_at": now,
	}).Error
}
func (s *Store) DueSandboxes(now time.Time) ([]Sandbox, error) {
	var rows []Sandbox
	err := s.DB.Where("status IN ? AND expires_at <= ?", []string{"active", "warning"}, now).Order("expires_at asc").Find(&rows).Error
	return rows, err
}

// ListSandboxesForLifecycle returns sandboxes that may need a reconcile pass:
// live ones (for warning/expiry/idle-suspend) plus expired/cleanup_failed for purge/retry.
func (s *Store) ListSandboxesForLifecycle() ([]Sandbox, error) {
	var rows []Sandbox
	err := s.DB.
		Where("status IN ?", []string{"active", "warning", "suspended", "expired", "cleanup_failed"}).
		Order("expires_at asc").
		Find(&rows).Error
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
		// Keep SandboxTestRun rows for history; only clear the live pointer.
		if err := tx.Model(&SandboxTestRun{}).Where("sandbox_id = ?", sandbox.ID).Update("sandbox_id", 0).Error; err != nil {
			return err
		}
		return tx.Delete(&Sandbox{}, sandbox.ID).Error
	})
}

func (s *Store) CreateSandboxTestRun(run *SandboxTestRun) (*SandboxTestRun, error) {
	if run.ProjectID == 0 || run.SourceEnvironmentID == 0 || strings.TrimSpace(run.Name) == "" {
		return nil, errors.New("invalid sandbox test run")
	}
	if run.Status == "" {
		run.Status = "running"
	}
	if run.Mode == "" {
		run.Mode = "fresh"
	}
	if run.PlanJSON == "" {
		run.PlanJSON = "{}"
	}
	if run.StepsJSON == "" {
		run.StepsJSON = "[]"
	}
	if run.StartedAt.IsZero() {
		run.StartedAt = time.Now().UTC()
	}
	if err := s.DB.Create(run).Error; err != nil {
		return nil, err
	}
	return run, nil
}

func (s *Store) UpdateSandboxTestRun(run *SandboxTestRun) error {
	if run == nil || run.ID == 0 {
		return errors.New("invalid sandbox test run")
	}
	return s.DB.Save(run).Error
}

func (s *Store) GetSandboxTestRun(id uint) (*SandboxTestRun, error) {
	var run SandboxTestRun
	if err := s.DB.First(&run, id).Error; err != nil {
		return nil, err
	}
	return &run, nil
}

func (s *Store) ListSandboxTestRuns(projectID uint, limit int) ([]SandboxTestRun, error) {
	if limit <= 0 {
		limit = 50
	}
	var rows []SandboxTestRun
	err := s.DB.Where("project_id = ?", projectID).Order("started_at desc").Limit(limit).Find(&rows).Error
	return rows, err
}

func (s *Store) LatestSandboxTestRun(sandboxID uint) (*SandboxTestRun, error) {
	if sandboxID == 0 {
		return nil, gorm.ErrRecordNotFound
	}
	var run SandboxTestRun
	err := s.DB.Where("sandbox_id = ?", sandboxID).Order("started_at desc").First(&run).Error
	if err != nil {
		return nil, err
	}
	return &run, nil
}
