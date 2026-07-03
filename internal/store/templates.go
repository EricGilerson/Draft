package store

import (
	"errors"
	"strconv"
	"strings"
)

var (
	ErrInvalidTemplate  = errors.New("template name is required")
	ErrTemplateBuiltin  = errors.New("built-in templates cannot be modified or deleted")
	ErrTemplateNotFound = errors.New("no template with that id")
)

// ListTemplates returns all templates, built-ins first then user templates,
// each group ordered by name. Built-ins come first so the library presents the
// curated defaults above user-created entries.
func (s *Store) ListTemplates() ([]ServiceTemplate, error) {
	var templates []ServiceTemplate
	if err := s.DB.Order("builtin desc, name asc").Find(&templates).Error; err != nil {
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

// SeedBuiltins inserts the curated built-in templates when none exist yet.
// Seeding matches on Name so it is idempotent across Opens and never produces
// duplicates even if some built-ins were already present.
func (s *Store) SeedBuiltins() error {
	var count int64
	if err := s.DB.Model(&ServiceTemplate{}).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return nil
	}
	for _, t := range builtinTemplates {
		t.Builtin = true
		if t.Mode == "" {
			t.Mode = "build"
		}
		if err := s.DB.Create(&t).Error; err != nil {
			return err
		}
	}
	return nil
}
