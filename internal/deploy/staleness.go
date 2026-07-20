package deploy

import "sort"

// StaleInput describes a value whose current resolution differs from the value
// recorded for the running container. Values are deliberately never returned.
type StaleInput struct {
	Key    string `json:"key"`
	Scope  string `json:"scope"`
	Reason string `json:"reason"`
}

type ServiceStaleness struct {
	NodeID       string       `json:"nodeId"`
	Running      bool         `json:"running"`
	Stale        bool         `json:"stale"`
	NeedsRebuild bool         `json:"needsRebuild"`
	Inputs       []StaleInput `json:"inputs"`
}

// GetServiceStaleness compares the active deployment manifest with what Draft
// would supply now. It is intentionally value-based: an input changed and
// changed back is not stale.
func (e *Engine) GetServiceStaleness(nodeID string) (*ServiceStaleness, error) {
	result := &ServiceStaleness{NodeID: nodeID, Inputs: []StaleInput{}}
	dep, err := e.store.ActiveDeployment(nodeID)
	if err != nil || dep == nil || dep.Status != "running" {
		return result, err
	}
	result.Running = true
	before, err := e.store.ListDeploymentInputs(dep.ID)
	if err != nil {
		return nil, err
	}
	if len(before) == 0 { // deployments created before manifests are unknown, not falsely stale
		return result, nil
	}
	current, err := e.currentDeploymentEnv(nodeID)
	if err != nil {
		return nil, err
	}
	now := make(map[string]string, len(current.RuntimeEnv)+len(current.BuildArgs))
	for _, item := range current.RuntimeEnv {
		if key, value, ok := splitEnv(item); ok {
			now["runtime:"+key] = deploymentInputDigest(value)
		}
	}
	for key, ptr := range current.BuildArgs {
		value := ""
		if ptr != nil {
			value = *ptr
		}
		now["build:"+key] = deploymentInputDigest(value)
	}
	for _, input := range before {
		id := input.Scope + ":" + input.Key
		if now[id] == input.Digest {
			continue
		}
		reason := "resolved runtime value changed"
		if input.Scope == "build" {
			reason = "resolved build input changed"
			result.NeedsRebuild = true
		}
		result.Inputs = append(result.Inputs, StaleInput{Key: input.Key, Scope: input.Scope, Reason: reason})
	}
	sort.Slice(result.Inputs, func(i, j int) bool {
		return result.Inputs[i].Scope+result.Inputs[i].Key < result.Inputs[j].Scope+result.Inputs[j].Key
	})
	result.Stale = len(result.Inputs) > 0
	return result, nil
}

func (e *Engine) currentDeploymentEnv(nodeID string) (deploymentEnv, error) {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return deploymentEnv{}, err
	}
	addr, err := e.computeNodeAddress(node)
	if err != nil {
		return deploymentEnv{}, err
	}
	return e.resolveDeploymentEnv(deploymentEnvInput{NodeID: node.ID, ProjectID: node.ProjectID, EnvironmentID: node.EnvironmentID, ServiceName: addr.ServiceName, ProjectName: addr.ProjectName, Environment: addr.Environment, ServicePort: addr.ServicePort, InternalHostname: addr.InternalHostname, InternalURL: addr.InternalURL, PublicHostname: addr.PublicHostname, PublicURL: addr.PublicURL})
}

// AffectedServices follows service-reference edges transitively, with sources
// before consumers. It is used to present and execute a safe deploy plan.
func (e *Engine) AffectedServices(nodeID string) ([]string, error) {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	conns, err := getEnvironmentConnections(e.store, node.EnvironmentID)
	if err != nil {
		return nil, err
	}
	byTarget := map[string][]string{}
	for _, c := range conns {
		byTarget[c.TargetNodeID] = append(byTarget[c.TargetNodeID], c.SourceNodeID)
	}
	seen := map[string]bool{nodeID: true}
	order := []string{nodeID}
	for i := 0; i < len(order); i++ {
		for _, child := range byTarget[order[i]] {
			if !seen[child] {
				seen[child] = true
				order = append(order, child)
			}
		}
	}
	return order, nil
}
