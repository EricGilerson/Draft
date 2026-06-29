package main

import "Draft/internal/store"

// GetEnvVars reads the .env file for a node's service root.
func (a *App) GetEnvVars(nodeID string) ([]store.EnvVar, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.GetEnvVars(a.ctx, nodeID)
}

// SetEnvVar writes or updates a key in the .env file.
func (a *App) SetEnvVar(nodeID, key, value string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.SetEnvVar(a.ctx, nodeID, key, value)
}

func (a *App) ImportEnvFile(nodeID, path string) (store.EnvFileSyncResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return store.EnvFileSyncResult{}, err
	}
	if c == nil {
		return store.EnvFileSyncResult{}, errNoStore
	}
	return c.ImportEnvFile(a.ctx, nodeID, path)
}

func (a *App) RefreshEnvFile(nodeID string) (store.EnvFileSyncResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return store.EnvFileSyncResult{}, err
	}
	if c == nil {
		return store.EnvFileSyncResult{}, errNoStore
	}
	return c.RefreshEnvFile(a.ctx, nodeID)
}

func (a *App) ExportEnvFile(nodeID string) (store.EnvFileSyncResult, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return store.EnvFileSyncResult{}, err
	}
	if c == nil {
		return store.EnvFileSyncResult{}, errNoStore
	}
	return c.ExportEnvFile(a.ctx, nodeID)
}

// SuggestEnvFile returns the absolute path to a .env file found in the service root, if one exists.
func (a *App) SuggestEnvFile(nodeID string, projectID int) (string, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", errNoStore
	}
	return c.SuggestEnvFile(a.ctx, nodeID, uint(projectID))
}
