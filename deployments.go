package main

import "Draft/internal/store"

func (a *App) DeployService(nodeID string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.Deploy(a.ctx, nodeID)
}

func (a *App) StopService(nodeID string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.Stop(a.ctx, nodeID)
}

func (a *App) RestartService(nodeID string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.Restart(a.ctx, nodeID)
}

func (a *App) GetDeployments(nodeID string) ([]store.Deployment, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.GetDeployments(a.ctx, nodeID)
}

func (a *App) GetActiveDeployment(nodeID string) (*store.Deployment, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return nil, err
	}
	if c == nil {
		return nil, errNoStore
	}
	return c.GetActiveDeployment(a.ctx, nodeID)
}

func (a *App) GetBuildLog(deploymentID uint) (string, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return "", err
	}
	if c == nil {
		return "", errNoStore
	}
	return c.GetBuildLog(a.ctx, deploymentID)
}

func (a *App) StartLogStream(nodeID string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.StartLogStream(a.ctx, nodeID)
}

func (a *App) StopLogStream(nodeID string) error {
	c, err := a.ensureDaemon()
	if err != nil {
		return err
	}
	if c == nil {
		return errNoStore
	}
	return c.StopLogStream(a.ctx, nodeID)
}
