package deploy

import (
	"context"
	"fmt"
	"sync"
)

// EnvironmentStackAction is a bulk lifecycle action over every service node in
// an environment. Actions run independently per node — no start-order or
// depends-on gating.
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

// RunEnvironmentStack applies action to every canvas node in environmentID.
// Deploy/redeploy kick off asynchronously (same as single-service Deploy);
// stop runs synchronously per node. Failures on one node do not cancel others.
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

	var wg sync.WaitGroup
	for i, node := range nodes {
		i, node := i, node
		wg.Add(1)
		go func() {
			defer wg.Done()
			res := EnvironmentStackNodeResult{NodeID: node.ID, Label: node.Label}
			var opErr error
			switch action {
			case StackStart, StackRedeploy:
				// Deploy cancels any in-flight build for the node and starts a
				// new one. Redeploy is the same path as start for Draft.
				opErr = e.Deploy(ctx, node.ID)
			case StackStop:
				opErr = e.Stop(ctx, node.ID)
			}
			if opErr != nil {
				res.Error = opErr.Error()
			}
			out.Results[i] = res
		}()
	}
	wg.Wait()

	for _, r := range out.Results {
		if r.Error != "" {
			out.Failed++
		} else {
			out.Succeeded++
		}
	}
	return out, nil
}
