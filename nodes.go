package main

import "Draft/internal/store"

func (a *App) CreateNode(id, label string, projectID uint, x, y float64) (*store.CanvasNode, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	node := &store.CanvasNode{
		ID:        id,
		Label:     label,
		ProjectID: projectID,
		X:         x,
		Y:         y,
	}
	return a.store.CreateNode(node)
}

func (a *App) UpdateNode(id string, x, y float64, label string) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.UpdateNode(id, x, y, label)
}

func (a *App) DeleteNode(id string) error {
	if a.store == nil {
		return errNoStore
	}
	return a.store.DeleteNode(id)
}

func (a *App) ListNodes(projectID uint) ([]store.CanvasNode, error) {
	if a.store == nil {
		return nil, errNoStore
	}
	return a.store.ListNodes(projectID)
}
