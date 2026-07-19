package deploy

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"Draft/internal/store"
)

// EnvironmentStackAction is a bulk lifecycle action over every service node in
// an environment. Start/redeploy run in dependency waves inferred from
// @{Service.ATTR} env connections (dependents wait for dependencies). Stop
// runs in reverse dependency order so consumers go down before providers.
type EnvironmentStackAction string

const (
	StackStart    EnvironmentStackAction = "start"
	StackStop     EnvironmentStackAction = "stop"
	StackRedeploy EnvironmentStackAction = "redeploy"
)

// EnvironmentStackNodeResult is the per-service outcome of a stack action.
type EnvironmentStackNodeResult struct {
	NodeID string `json:"nodeId"`
	Label  string `json:"label"`
	Error  string `json:"error,omitempty"`
}

// EnvironmentStackResult summarizes a bulk start/stop/redeploy.
type EnvironmentStackResult struct {
	Action    string                       `json:"action"`
	Total     int                          `json:"total"`
	Succeeded int                          `json:"succeeded"`
	Failed    int                          `json:"failed"`
	Results   []EnvironmentStackNodeResult `json:"results"`
}

const stackNodeReadyTimeout = 5 * time.Minute

// RunEnvironmentStack applies action to every canvas node in environmentID.
// Deploy/redeploy kick off asynchronously (same as single-service Deploy) but
// wait for each dependency wave to become ready before starting the next.
// Failures on one node do not cancel others in the same wave.
func (e *Engine) RunEnvironmentStack(ctx context.Context, environmentID uint, action EnvironmentStackAction) (*EnvironmentStackResult, error) {
	switch action {
	case StackStart, StackStop, StackRedeploy:
	default:
		return nil, fmt.Errorf("unknown stack action %q", action)
	}

	if _, err := e.store.GetEnvironment(environmentID); err != nil {
		return nil, err
	}
	nodes, err := e.store.ListNodesByEnvironment(environmentID)
	if err != nil {
		return nil, err
	}

	out := &EnvironmentStackResult{
		Action:  string(action),
		Total:   len(nodes),
		Results: make([]EnvironmentStackNodeResult, len(nodes)),
	}
	if len(nodes) == 0 {
		return out, nil
	}

	indexByID := make(map[string]int, len(nodes))
	for i, node := range nodes {
		indexByID[node.ID] = i
		out.Results[i] = EnvironmentStackNodeResult{NodeID: node.ID, Label: node.Label}
	}

	waves := stackDependencyWaves(nodes, e.stackDependsOn(environmentID, nodes))
	if action == StackStop {
		waves = reverseWaves(waves)
	}

	for _, wave := range waves {
		var wg sync.WaitGroup
		for _, nodeID := range wave {
			i := indexByID[nodeID]
			node := nodes[i]
			wg.Add(1)
			go func(i int, node store.CanvasNode) {
				defer wg.Done()
				var opErr error
				switch action {
				case StackStart, StackRedeploy:
					opErr = e.Deploy(ctx, node.ID)
					if opErr == nil {
						opErr = e.waitStackNodeReady(ctx, node.ID, stackNodeReadyTimeout)
					}
				case StackStop:
					opErr = e.Stop(ctx, node.ID)
				}
				if opErr != nil {
					out.Results[i].Error = opErr.Error()
				}
			}(i, node)
		}
		wg.Wait()
	}

	for _, r := range out.Results {
		if r.Error != "" {
			out.Failed++
		} else {
			out.Succeeded++
		}
	}
	return out, nil
}

// stackDependsOn returns nodeID → set of node IDs it references via @{Label}.
// A service that points at postgres depends on postgres being ready first.
func (e *Engine) stackDependsOn(environmentID uint, nodes []store.CanvasNode) map[string]map[string]struct{} {
	deps := make(map[string]map[string]struct{}, len(nodes))
	for _, n := range nodes {
		deps[n.ID] = make(map[string]struct{})
	}
	conns, err := e.GetEnvironmentConnections(environmentID)
	if err != nil {
		return deps
	}
	for _, c := range conns {
		if c.SourceNodeID == "" || c.TargetNodeID == "" || c.SourceNodeID == c.TargetNodeID {
			continue
		}
		if _, ok := deps[c.SourceNodeID]; !ok {
			continue
		}
		if _, ok := deps[c.TargetNodeID]; !ok {
			continue
		}
		deps[c.SourceNodeID][c.TargetNodeID] = struct{}{}
	}
	return deps
}

// stackDependencyWaves returns ordered waves where each node's dependencies
// appear in an earlier wave. Cycles are broken by emitting remaining nodes as
// a final parallel wave so stack ops still make progress.
func stackDependencyWaves(nodes []store.CanvasNode, deps map[string]map[string]struct{}) [][]string {
	remaining := make(map[string]struct{}, len(nodes))
	for _, n := range nodes {
		remaining[n.ID] = struct{}{}
	}

	var waves [][]string
	for len(remaining) > 0 {
		var wave []string
		for id := range remaining {
			ready := true
			for dep := range deps[id] {
				if _, still := remaining[dep]; still {
					ready = false
					break
				}
			}
			if ready {
				wave = append(wave, id)
			}
		}
		if len(wave) == 0 {
			wave = make([]string, 0, len(remaining))
			for id := range remaining {
				wave = append(wave, id)
			}
			waves = append(waves, wave)
			break
		}
		waves = append(waves, wave)
		for _, id := range wave {
			delete(remaining, id)
		}
	}
	return waves
}

func reverseWaves(waves [][]string) [][]string {
	out := make([][]string, len(waves))
	for i := range waves {
		out[i] = waves[len(waves)-1-i]
	}
	return out
}

// waitStackNodeReady blocks until the node's latest deploy reaches running, or
// a terminal failure. Linked aliases and non-deployable nodes succeed immediately.
func (e *Engine) waitStackNodeReady(ctx context.Context, nodeID string, timeout time.Duration) error {
	settings, _ := e.store.GetNodeSettings(nodeID)
	if ParseServiceLink(settings[SettingServiceLink]) != nil {
		// Linked deploy is synchronous ensure-attach inside runDeploy; give it
		// a brief window then accept whatever status we have.
		deadline := time.Now().Add(30 * time.Second)
		for {
			if err := ctx.Err(); err != nil {
				return err
			}
			e.mu.Lock()
			_, building := e.active[nodeID]
			e.mu.Unlock()
			if !building {
				return nil
			}
			if time.Now().After(deadline) {
				return fmt.Errorf("timed out waiting for linked service attach")
			}
			select {
			case <-ctx.Done():
				return ctx.Err()
			case <-time.After(200 * time.Millisecond):
			}
		}
	}
	if !nodeLooksDeployable(settings) {
		return nil
	}

	deadline := time.Now().Add(timeout)
	var lastReason string
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		e.mu.Lock()
		stillActive := e.active[nodeID] != nil
		e.mu.Unlock()

		latest, err := e.store.LatestDeployment(nodeID)
		if err != nil {
			return err
		}
		if latest != nil {
			switch latest.Status {
			case "running":
				if latest.ContainerID != "" {
					return nil
				}
				lastReason = "running without container"
			case "failed", "interrupted", "stopped":
				if !stillActive {
					msg := latest.Status
					if strings.TrimSpace(latest.Error) != "" {
						msg = latest.Error
					}
					return fmt.Errorf("%s", msg)
				}
				lastReason = latest.Status
			default:
				lastReason = latest.Status
			}
		} else if !stillActive {
			// Deploy returned but never created a row (validation failure).
			return fmt.Errorf("deploy did not start")
		} else {
			lastReason = "starting"
		}

		if time.Now().After(deadline) {
			if lastReason == "" {
				lastReason = "did not become ready"
			}
			return fmt.Errorf("timeout waiting for service: %s", lastReason)
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(400 * time.Millisecond):
		}
	}
}
