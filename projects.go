package main

import (
	"errors"

	"Draft/internal/store"
)

// errNoStore is returned to the frontend when the database failed to open.
var errNoStore = errors.New("database is not available")

// CreateProject creates a project from the create-project dialog.
func (a *App) CreateProject(name, path, description string) (*store.Project, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.CreateProject(name, path, description)
}

// ListProjects returns all registered projects.
func (a *App) ListProjects() ([]store.Project, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.ListProjects()
}
