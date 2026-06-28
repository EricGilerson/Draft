package store

import (
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

// ListProjects returns all projects, newest first.
func (s *Store) ListProjects() ([]Project, error) {
	var projects []Project
	if err := s.DB.Order("created_at desc").Find(&projects).Error; err != nil {
		return nil, err
	}
	return projects, nil
}
