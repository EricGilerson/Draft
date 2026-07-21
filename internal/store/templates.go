package store

import (
	"errors"
	"strconv"
	"strings"

	"gorm.io/gorm"
)

var (
	ErrInvalidTemplate  = errors.New("template name is required")
	ErrTemplateBuiltin  = errors.New("built-in templates cannot be modified or deleted")
	ErrTemplateNotFound = errors.New("no template with that id")
)

// ListTemplates returns all templates, built-ins first then user templates,
// each group ordered by name. Built-ins come first so the library presents the
// curated defaults above user-created entries. Dockerfile blobs are omitted —
// load them via GetTemplate when editing/stamping.
func (s *Store) ListTemplates() ([]ServiceTemplate, error) {
	var templates []ServiceTemplate
	if err := s.DB.Omit("Dockerfile").Order("builtin desc, name asc").Find(&templates).Error; err != nil {
		return nil, err
	}
	return templates, nil
}

// GetTemplate returns a single template by ID.
func (s *Store) GetTemplate(id uint) (*ServiceTemplate, error) {
	var t ServiceTemplate
	if err := s.DB.First(&t, id).Error; err != nil {
		return nil, ErrTemplateNotFound
	}
	return &t, nil
}

// normalizeTemplateSchema validates the Schema column on a template about to be
// saved: it must parse as a TemplateSchema, and any unset mode fields are filled
// from the default for the template's Mode. The encoded result is returned. A
// malformed schema is rejected with a clear error so a bad template can't ship
// a zero schema into the wizard.
func normalizeTemplateSchema(raw, mode string) (string, error) {
	s, err := ParseTemplateSchema(raw)
	if err != nil {
		return "", err
	}
	s = NormalizeSchema(s, mode)
	return EncodeTemplateSchema(s)
}

// CreateTemplate inserts a new user template. Builtin is forced false so the
// curated defaults can never be impersonated or overwritten via this path.
func (s *Store) CreateTemplate(t *ServiceTemplate) (*ServiceTemplate, error) {
	if t == nil {
		return nil, ErrInvalidTemplate
	}
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return nil, ErrInvalidTemplate
	}
	t.Builtin = false
	if t.Mode == "" {
		t.Mode = "build"
	}
	normalized, err := normalizeTemplateSchema(t.Schema, t.Mode)
	if err != nil {
		return nil, err
	}
	t.Schema = normalized
	if t.ImageTags, err = NormalizeImageTags(t.ImageTags); err != nil {
		return nil, err
	}
	if t.Volumes, err = NormalizeTemplateVolumes(t.Volumes); err != nil {
		return nil, err
	}
	if t.DefaultSettings, err = NormalizeDefaultSettings(t.DefaultSettings); err != nil {
		return nil, err
	}
	if err := s.DB.Create(t).Error; err != nil {
		return nil, err
	}
	return t, nil
}

// UpdateTemplate updates a user template. Built-ins are rejected so the
// curated defaults stay stable; users clone-to-customize instead.
func (s *Store) UpdateTemplate(t *ServiceTemplate) error {
	if t == nil {
		return ErrInvalidTemplate
	}
	t.Name = strings.TrimSpace(t.Name)
	if t.Name == "" {
		return ErrInvalidTemplate
	}
	existing, err := s.GetTemplate(t.ID)
	if err != nil {
		return err
	}
	if existing.Builtin {
		return ErrTemplateBuiltin
	}
	// Builtin is immutable from this path regardless of the input.
	t.Builtin = false
	if t.Mode == "" {
		t.Mode = "build"
	}
	normalized, err := normalizeTemplateSchema(t.Schema, t.Mode)
	if err != nil {
		return err
	}
	t.Schema = normalized
	if t.ImageTags, err = NormalizeImageTags(t.ImageTags); err != nil {
		return err
	}
	if t.Volumes, err = NormalizeTemplateVolumes(t.Volumes); err != nil {
		return err
	}
	if t.DefaultSettings, err = NormalizeDefaultSettings(t.DefaultSettings); err != nil {
		return err
	}
	return s.DB.Save(t).Error
}

// DeleteTemplate removes a user template. Built-ins are rejected.
func (s *Store) DeleteTemplate(id uint) error {
	existing, err := s.GetTemplate(id)
	if err != nil {
		return err
	}
	if existing.Builtin {
		return ErrTemplateBuiltin
	}
	return s.DB.Delete(&ServiceTemplate{}, id).Error
}

// CloneTemplate produces a user-owned copy of a template (built-in or user),
// with Builtin=false and " (copy)" appended to the name. The clone is returned
// so the caller can open it for editing immediately.
func (s *Store) CloneTemplate(id uint) (*ServiceTemplate, error) {
	src, err := s.GetTemplate(id)
	if err != nil {
		return nil, err
	}
	clone := *src
	clone.ID = 0
	clone.Builtin = false
	clone.Name = s.uniqueCloneName(src.Name)
	return s.CreateTemplate(&clone)
}

// uniqueCloneName appends " (copy)" and, if that name already exists, a numeric
// suffix so cloning never silently collides.
func (s *Store) uniqueCloneName(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "Template"
	}
	candidate := base + " (copy)"
	for i := 2; ; i++ {
		var count int64
		_ = s.DB.Model(&ServiceTemplate{}).Where("name = ?", candidate).Count(&count).Error
		if count == 0 {
			return candidate
		}
		candidate = base + " (copy " + strconv.Itoa(i) + ")"
	}
}

// SeedBuiltins reconciles the curated built-in templates against the store.
// Missing built-ins are inserted; existing built-ins with the same Name are
// updated in place so edits to the curated definitions (e.g. Dockerfile
// changes) land on the next Open without producing duplicates. User-owned
// templates are never touched here.
func (s *Store) SeedBuiltins() error {
	for _, t := range builtinTemplates {
		t.Name = strings.TrimSpace(t.Name)
		if t.Name == "" {
			continue
		}
		t.Builtin = true
		if t.Mode == "" {
			t.Mode = "build"
		}
		var existing ServiceTemplate
		err := s.DB.Where("name = ?", t.Name).First(&existing).Error
		if err == nil {
			if !existing.Builtin {
				// A user-owned template claims this name; leave it alone.
				continue
			}
			existing.Description = t.Description
			existing.Category = t.Category
			existing.Icon = t.Icon
			existing.Color = t.Color
			existing.Mode = t.Mode
			existing.Image = t.Image
			existing.Port = t.Port
			existing.Dockerfile = t.Dockerfile
			existing.CmdOverride = t.CmdOverride
			existing.Entrypoint = t.Entrypoint
			existing.WorkingDir = t.WorkingDir
			existing.EnvVars = t.EnvVars
			existing.Labels = t.Labels
		existing.Schema = t.Schema
		existing.ImageTags = t.ImageTags
		existing.Volumes = t.Volumes
		existing.DefaultSettings = t.DefaultSettings
		existing.Builtin = true
			if err := s.DB.Save(&existing).Error; err != nil {
				return err
			}
			continue
		}
		if !errors.Is(err, ErrTemplateNotFound) && !isRecordNotFound(err) {
			return err
		}
		if err := s.DB.Create(&t).Error; err != nil {
			return err
		}
	}
	return nil
}

// isRecordNotFound reports whether err is GORM's record-not-found error.
func isRecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
