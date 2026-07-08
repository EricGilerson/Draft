package deploy

import (
	"fmt"
	"strings"
)

// SecretUsage describes one service affected by an app secret.
type SecretUsage struct {
	ProjectID            uint   `json:"projectId"`
	ProjectName          string `json:"projectName"`
	NodeID               string `json:"nodeId"`
	NodeLabel            string `json:"nodeLabel"`
	VarKey               string `json:"varKey"`
	IsRunning            bool   `json:"isRunning"`
	Overridden           bool   `json:"overridden,omitempty"`
	ReceivesViaInjection bool   `json:"receivesViaInjection,omitempty"`
}

// ListAppSecretUsages returns every service whose env var values reference
// {{secret.key}} directly.
func (e *Engine) ListAppSecretUsages(key string) ([]SecretUsage, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil, fmt.Errorf("key is required")
	}
	token := secretExprToken(key)
	projects, err := e.store.ListProjects()
	if err != nil {
		return nil, err
	}
	var out []SecretUsage
	seen := make(map[string]bool)
	for _, project := range projects {
		nodes, err := e.store.ListNodes(project.ID)
		if err != nil {
			return nil, err
		}
		for _, node := range nodes {
			vars, err := e.store.ListEnvVars(node.ID)
			if err != nil {
				return nil, err
			}
			for _, v := range vars {
				if !strings.Contains(v.Value, token) {
					continue
				}
				usageKey := fmt.Sprintf("%s:%s", node.ID, v.Key)
				if seen[usageKey] {
					continue
				}
				running, _ := e.isNodeRunning(node.ID)
				out = append(out, SecretUsage{
					ProjectID:   project.ID,
					ProjectName: project.Name,
					NodeID:      node.ID,
					NodeLabel:   node.Label,
					VarKey:      v.Key,
					IsRunning:   running,
				})
				seen[usageKey] = true
			}
		}
	}
	return out, nil
}

func (e *Engine) isNodeRunning(nodeID string) (bool, error) {
	dep, err := e.store.ActiveDeployment(nodeID)
	if err != nil {
		return false, err
	}
	return dep != nil && dep.Status == "running", nil
}

// CountAppSecretReferences returns how many distinct service usages reference key.
func (e *Engine) CountAppSecretReferences(key string) (int, error) {
	usages, err := e.ListAppSecretUsages(key)
	if err != nil {
		return 0, err
	}
	return len(usages), nil
}

// ListProjectEnvVarUsages returns services in projectID whose env var values
// reference {{project.key}}.
func (e *Engine) ListProjectEnvVarUsages(projectID uint, key string) ([]SecretUsage, error) {
	key = strings.TrimSpace(key)
	if projectID == 0 || key == "" {
		return nil, fmt.Errorf("projectID and key are required")
	}
	project, err := e.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	if _, err := e.store.GetProjectEnvVar(projectID, key); err != nil {
		return nil, fmt.Errorf("project value %s not found", key)
	}

	token := projectExprToken(key)
	nodes, err := e.store.ListNodes(projectID)
	if err != nil {
		return nil, err
	}
	var out []SecretUsage
	seen := make(map[string]bool)
	for _, node := range nodes {
		vars, err := e.store.ListEnvVars(node.ID)
		if err != nil {
			return nil, err
		}
		for _, v := range vars {
			if !strings.Contains(v.Value, token) {
				continue
			}
			usageKey := fmt.Sprintf("%s:%s", node.ID, v.Key)
			if seen[usageKey] {
				continue
			}
			running, _ := e.isNodeRunning(node.ID)
			out = append(out, SecretUsage{
				ProjectID:   projectID,
				ProjectName: project.Name,
				NodeID:      node.ID,
				NodeLabel:   node.Label,
				VarKey:      v.Key,
				IsRunning:   running,
			})
			seen[usageKey] = true
		}
	}
	return out, nil
}

// CountProjectEnvVarReferences returns how many distinct service usages reference key.
func (e *Engine) CountProjectEnvVarReferences(projectID uint, key string) (int, error) {
	usages, err := e.ListProjectEnvVarUsages(projectID, key)
	if err != nil {
		return 0, err
	}
	return len(usages), nil
}
