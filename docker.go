package main

import (
	"context"
	"time"

	"github.com/docker/docker/client"
)

// DockerStatus describes the reachability of the local Docker daemon.
type DockerStatus struct {
	// State is one of: "running" | "stopped".
	State string `json:"state"`
	// APIVersion is the negotiated daemon API version when running.
	APIVersion string `json:"apiVersion,omitempty"`
	// Error holds the underlying connection error when stopped.
	Error string `json:"error,omitempty"`
}

// CheckDocker pings the local Docker daemon and reports whether it is reachable.
func (a *App) CheckDocker() DockerStatus {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return DockerStatus{State: "stopped", Error: err.Error()}
	}
	defer cli.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()

	ping, err := cli.Ping(ctx)
	if err != nil {
		return DockerStatus{State: "stopped", Error: err.Error()}
	}

	return DockerStatus{State: "running", APIVersion: ping.APIVersion}
}
