package main

import "Draft/internal/store"

func (a *App) DeployService(nodeID string) error {
	if a.engine == nil {
		return errNoStore
	}
	return a.engine.Deploy(a.ctx, nodeID)
}

func (a *App) StopService(nodeID string) error {
	if a.engine == nil {
		return errNoStore
	}
	return a.engine.Stop(a.ctx, nodeID)
}

func (a *App) RestartService(nodeID string) error {
	if a.engine == nil {
		return errNoStore
	}
	return a.engine.Restart(a.ctx, nodeID)
}

func (a *App) GetDeployments(nodeID string) ([]store.Deployment, error) {
	if a.engine == nil {
		return nil, errNoStore
	}
	return a.engine.GetDeployments(nodeID)
}

func (a *App) GetActiveDeployment(nodeID string) (*store.Deployment, error) {
	if a.engine == nil {
		return nil, errNoStore
	}
	return a.engine.GetActiveDeployment(nodeID)
}

func (a *App) GetBuildLog(deploymentID uint) (string, error) {
	if a.engine == nil {
		return "", errNoStore
	}
	return a.engine.GetBuildLog(deploymentID)
}

func (a *App) StartLogStream(nodeID string) error {
	if a.engine == nil {
		return errNoStore
	}
	return a.engine.StartLogStream(a.ctx, nodeID)
}

func (a *App) StopLogStream(nodeID string) error {
	if a.engine == nil {
		return errNoStore
	}
	a.engine.StopLogStream(nodeID)
	return nil
}
