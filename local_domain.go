package main

import "Draft/internal/networking"

func (a *App) GetLocalDomainStatus() networking.LocalDomainStatus {
	c, err := a.ensureDaemon()
	if err != nil || c == nil {
		return networking.LocalDomainStatus{Mode: "localhost-port", HostsError: errString(err)}
	}
	status, err := c.LocalDomainStatus(a.ctx)
	if err != nil {
		return networking.LocalDomainStatus{Mode: "localhost-port", HostsError: err.Error()}
	}
	return status
}
