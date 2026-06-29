package main

import "Draft/internal/deploy"

func (a *App) GetServiceMetrics(nodeID string) (deploy.ServiceMetrics, error) {
	c, err := a.ensureDaemon()
	if err != nil {
		return deploy.ServiceMetrics{}, err
	}
	if c == nil {
		return deploy.ServiceMetrics{}, errNoStore
	}
	return c.GetServiceMetrics(a.ctx, nodeID)
}
