package deploy

import (
	"fmt"
	"strings"

	"Draft/internal/store"
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

type secretUsageIndex struct {
	nodesByID    map[string]*store.CanvasNode
	nodesByLabel map[uint]map[string]*store.CanvasNode // environmentID → label → node
	envVars      map[string]map[string]string          // nodeID → key → value
	settings     map[string]map[string]string          // nodeID → settings
	projectVars  map[string]string
	active       map[string]*store.Deployment
}

// ListAppSecretUsages returns every service whose env var values reference
// {{secret.key}} directly or via @{Service}/{{project}} indirection.
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
		idx, err := e.buildSecretUsageIndex(project.ID, nodes)
		if err != nil {
			return nil, err
		}
		for i := range nodes {
			node := &nodes[i]
			for keyName, value := range idx.envVars[node.ID] {
				if !valueDependsOnTokenIndexed(idx, node, value, token, map[string]bool{node.ID: true}) {
					continue
				}
				usageKey := fmt.Sprintf("%s:%s", node.ID, keyName)
				if seen[usageKey] {
					continue
				}
				out = append(out, SecretUsage{
					ProjectID:   project.ID,
					ProjectName: project.Name,
					NodeID:      node.ID,
					NodeLabel:   node.Label,
					VarKey:      keyName,
					IsRunning:   isIndexedNodeRunning(idx, node.ID),
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
	idx, err := e.buildSecretUsageIndex(projectID, nodes)
	if err != nil {
		return nil, err
	}
	var out []SecretUsage
	seen := make(map[string]bool)
	for i := range nodes {
		node := &nodes[i]
		for keyName, value := range idx.envVars[node.ID] {
			if !valueDependsOnTokenIndexed(idx, node, value, token, map[string]bool{node.ID: true}) {
				continue
			}
			usageKey := fmt.Sprintf("%s:%s", node.ID, keyName)
			if seen[usageKey] {
				continue
			}
			out = append(out, SecretUsage{
				ProjectID:   projectID,
				ProjectName: project.Name,
				NodeID:      node.ID,
				NodeLabel:   node.Label,
				VarKey:      keyName,
				IsRunning:   isIndexedNodeRunning(idx, node.ID),
			})
			seen[usageKey] = true
		}
	}
	return out, nil
}

func (e *Engine) buildSecretUsageIndex(projectID uint, nodes []store.CanvasNode) (*secretUsageIndex, error) {
	idx := &secretUsageIndex{
		nodesByID:    make(map[string]*store.CanvasNode, len(nodes)),
		nodesByLabel: make(map[uint]map[string]*store.CanvasNode),
		envVars:      make(map[string]map[string]string, len(nodes)),
		settings:     map[string]map[string]string{},
		projectVars:  map[string]string{},
		active:       map[string]*store.Deployment{},
	}
	nodeIDs := make([]string, len(nodes))
	for i := range nodes {
		n := &nodes[i]
		nodeIDs[i] = n.ID
		idx.nodesByID[n.ID] = n
		byLabel := idx.nodesByLabel[n.EnvironmentID]
		if byLabel == nil {
			byLabel = map[string]*store.CanvasNode{}
			idx.nodesByLabel[n.EnvironmentID] = byLabel
		}
		byLabel[n.Label] = n
	}
	varsByNode, err := e.store.ListEnvVarsByNodes(nodeIDs)
	if err != nil {
		return nil, err
	}
	for nodeID, vars := range varsByNode {
		m := make(map[string]string, len(vars))
		for _, v := range vars {
			m[v.Key] = v.Value
		}
		idx.envVars[nodeID] = m
	}
	settingsByNode, err := e.store.GetNodeSettingsByNodes(nodeIDs)
	if err != nil {
		return nil, err
	}
	idx.settings = settingsByNode
	projectVars, err := e.store.ListProjectEnvVars(projectID)
	if err != nil {
		return nil, err
	}
	for _, pv := range projectVars {
		idx.projectVars[pv.Key] = pv.Value
	}
	active, err := e.store.ListActiveDeploymentsByNodes(nodeIDs)
	if err != nil {
		return nil, err
	}
	idx.active = active
	return idx, nil
}

func isIndexedNodeRunning(idx *secretUsageIndex, nodeID string) bool {
	dep := idx.active[nodeID]
	return dep != nil && dep.Status == "running"
}

func indexedReferenceEnvVarNodeID(idx *secretUsageIndex, targetNodeID string) string {
	settings := idx.settings[targetNodeID]
	if link := ParseServiceLink(settings[SettingServiceLink]); link != nil && strings.TrimSpace(link.RootNodeID) != "" {
		return link.RootNodeID
	}
	return targetNodeID
}

// valueDependsOnToken follows the same service-reference graph used during
// deployment resolution. This makes shared-value usage lists include indirect
// consumers such as api -> @{db.DATABASE_URL} -> {{secret.DB_PASSWORD}}.
func (e *Engine) valueDependsOnToken(node *store.CanvasNode, raw, token string, visited map[string]bool) bool {
	idx, err := e.buildSecretUsageIndex(node.ProjectID, []store.CanvasNode{*node})
	if err != nil {
		return strings.Contains(raw, token)
	}
	// Include sibling nodes in the same environment for @{Label} resolution.
	if peers, err := e.store.ListNodesByEnvironment(node.EnvironmentID); err == nil {
		if full, err := e.buildSecretUsageIndex(node.ProjectID, peers); err == nil {
			idx = full
		}
	}
	return valueDependsOnTokenIndexed(idx, node, raw, token, visited)
}

func valueDependsOnTokenIndexed(idx *secretUsageIndex, node *store.CanvasNode, raw, token string, visited map[string]bool) bool {
	if strings.Contains(raw, token) {
		return true
	}
	for _, match := range projectExprPattern.FindAllStringSubmatch(raw, -1) {
		marker := "project:" + match[1]
		if visited[marker] {
			continue
		}
		projectValue, ok := idx.projectVars[match[1]]
		if !ok {
			continue
		}
		visited[marker] = true
		depends := valueDependsOnTokenIndexed(idx, node, projectValue, token, visited)
		delete(visited, marker)
		if depends {
			return true
		}
	}
	for _, match := range refPattern.FindAllStringSubmatch(raw, -1) {
		target := idx.nodesByLabel[node.EnvironmentID][match[1]]
		if target == nil || visited[target.ID] {
			continue
		}
		attr := match[2]
		if isGeneratedAttr(attr) {
			continue
		}
		resolveID := indexedReferenceEnvVarNodeID(idx, target.ID)
		value, ok := idx.envVars[resolveID][attr]
		if !ok {
			continue
		}
		resolvedNode := idx.nodesByID[resolveID]
		if resolvedNode == nil {
			resolvedNode = target
		}
		visited[resolveID] = true
		depends := valueDependsOnTokenIndexed(idx, resolvedNode, value, token, visited)
		delete(visited, resolveID)
		if depends {
			return true
		}
	}
	return false
}

// CountProjectEnvVarReferences returns how many distinct service usages reference key.
func (e *Engine) CountProjectEnvVarReferences(projectID uint, key string) (int, error) {
	usages, err := e.ListProjectEnvVarUsages(projectID, key)
	if err != nil {
		return 0, err
	}
	return len(usages), nil
}
