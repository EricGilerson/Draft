package deploy

import (
	"fmt"
	"strings"
)

// SecretUsage describes one service affected by an app or project secret.
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

// ListAppSecretUsages returns every service whose env vars (or project env vars)
// reference {{secret.key}}.
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
		pvars, err := e.store.ListProjectEnvVars(project.ID)
		if err != nil {
			return nil, err
		}
		for _, pv := range pvars {
			if strings.Contains(pv.Value, token) {
				// Project var references app secret — all services inherit unless overridden.
				nodes, err := e.store.ListNodes(project.ID)
				if err != nil {
					return nil, err
				}
				for _, node := range nodes {
					usageKey := fmt.Sprintf("%s:%s", node.ID, pv.Key)
					if seen[usageKey] {
						continue
					}
					overridden := false
					if nv, err := e.store.GetEnvVar(node.ID, pv.Key); err == nil && nv.Value != "" {
						overridden = true
					}
					running, _ := e.isNodeRunning(node.ID)
					out = append(out, SecretUsage{
						ProjectID:            project.ID,
						ProjectName:          project.Name,
						NodeID:               node.ID,
						NodeLabel:            node.Label,
						VarKey:               pv.Key,
						IsRunning:            running,
						Overridden:           overridden,
						ReceivesViaInjection: !overridden,
					})
					seen[usageKey] = true
				}
			}
		}
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

// ListProjectSecretUsages returns services in projectID affected by a project
// secret key injected via project_env_vars.
func (e *Engine) ListProjectSecretUsages(projectID uint, key string) ([]SecretUsage, error) {
	key = strings.TrimSpace(key)
	if projectID == 0 || key == "" {
		return nil, fmt.Errorf("projectID and key are required")
	}
	project, err := e.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	if _, err := e.store.GetProjectEnvVar(projectID, key); err != nil {
		return nil, fmt.Errorf("project secret %s not found", key)
	}
	nodes, err := e.store.ListNodes(projectID)
	if err != nil {
		return nil, err
	}
	var out []SecretUsage
	for _, node := range nodes {
		overridden := false
		if _, err := e.store.GetEnvVar(node.ID, key); err == nil {
			overridden = true
		}
		running, _ := e.isNodeRunning(node.ID)
		out = append(out, SecretUsage{
			ProjectID:            projectID,
			ProjectName:          project.Name,
			NodeID:               node.ID,
			NodeLabel:            node.Label,
			VarKey:               key,
			IsRunning:            running,
			Overridden:           overridden,
			ReceivesViaInjection: !overridden,
		})
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
