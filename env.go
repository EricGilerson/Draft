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
