package deploy

import (
	"fmt"
	"regexp"
	"sort"
	"strings"

	"Draft/internal/networking"
	"Draft/internal/store"
)

// refPattern matches @{Label.ATTR} tokens inside an env var value, letting
// one service's variable reference another service's address or variables
// (e.g. "postgres://@{db.DRAFT_INTERNAL_HOSTNAME}:5432/app"). Label matches
// up to the last '.' so it can contain dots itself only if ATTR still parses
// as a trailing identifier; node labels are otherwise unrestricted, so this
// is a best-effort split, not a hard guarantee.
var refPattern = regexp.MustCompile(`@\{([^{}]+)\.([A-Za-z0-9_]+)\}`)

// generatedAttrs are the address attributes every node exposes to referencing
// services, independent of whether the node has ever been deployed (hostname
// and port are deterministic — see computeNodeAddress). Named to match the
// DRAFT_* vars Draft injects into that same node's own container, so
// referencing another service's address reads the same as the variable it
// corresponds to there.
var generatedAttrs = []string{"DRAFT_INTERNAL_HOSTNAME", "DRAFT_INTERNAL_URL", "DRAFT_PUBLIC_HOSTNAME", "DRAFT_PUBLIC_URL", "DRAFT_SERVICE_PORT"}

// NodeAddress holds the deterministic, pre-deploy-computable address info for
// a node. It's used both to inject this node's own DRAFT_* vars and to
// resolve other services' references to this node.
type NodeAddress struct {
	ServiceName      string
	ProjectName      string
	Environment      string
	ServicePort      string
	InternalHostname string
	InternalURL      string
	PublicHostname   string
	PublicURL        string
}

func (a NodeAddress) attr(name string) (string, bool) {
	switch name {
	case "DRAFT_INTERNAL_HOSTNAME":
		return a.InternalHostname, true
	case "DRAFT_INTERNAL_URL":
		return a.InternalURL, true
	case "DRAFT_PUBLIC_HOSTNAME":
		return a.PublicHostname, true
	case "DRAFT_PUBLIC_URL":
		return a.PublicURL, true
	case "DRAFT_SERVICE_PORT":
		return a.ServicePort, true
	default:
		return "", false
	}
}

// computeNodeAddress derives a node's address without requiring it to be
// deployed or running: hostname is built from the node's permanent UID, and
// the port comes from its configured node settings.
func (e *Engine) computeNodeAddress(node *store.CanvasNode) (NodeAddress, error) {
	project, err := e.store.GetProject(node.ProjectID)
	if err != nil {
		return NodeAddress{}, fmt.Errorf("project not found for %q: %w", node.Label, err)
	}
	settings, err := e.store.GetNodeSettings(node.ID)
	if err != nil {
		return NodeAddress{}, err
	}
	uid, err := e.store.EnsureNodeUID(node.ID)
	if err != nil {
		return NodeAddress{}, err
	}

	serviceName := sanitize(node.Label)
	projectName := sanitize(project.Name)
	environment := "default"
	portStr := settings["service_port"]

	hostname := networking.Hostname(serviceName, projectName, environment, uid)
	publicHostname := networking.PublicHostname(hostname)
	publicURL := ""
	if e.router != nil {
		publicURL = networking.PublicURL(hostname, e.router.LocalDomainStatus().ProxyPort)
	}

	return NodeAddress{
		ServiceName:      serviceName,
		ProjectName:      projectName,
		Environment:      environment,
		ServicePort:      portStr,
		InternalHostname: hostname,
		InternalURL:      networking.InternalURL(hostname, portStr),
		PublicHostname:   publicHostname,
		PublicURL:        publicURL,
	}, nil
}

// resolveValue substitutes every @{Label.ATTR} token in raw with its resolved
// value, recursing into a referenced node's own variables when ATTR isn't a
// generated address attribute. visited tracks node IDs on the current
// resolution path (seeded with the starting node) so a reference cycle fails
// fast with a readable error instead of recursing forever.
func (e *Engine) resolveValue(projectID uint, raw string, visited map[string]bool) (string, error) {
	matches := refPattern.FindAllStringSubmatchIndex(raw, -1)
	if matches == nil {
		return raw, nil
	}

	var out strings.Builder
	last := 0
	for _, m := range matches {
		out.WriteString(raw[last:m[0]])
		label := raw[m[2]:m[3]]
		attrName := raw[m[4]:m[5]]
		resolved, err := e.resolveNodeAttr(projectID, label, attrName, visited)
		if err != nil {
			return "", fmt.Errorf("%s: %w", raw[m[0]:m[1]], err)
		}
		out.WriteString(resolved)
		last = m[1]
	}
	out.WriteString(raw[last:])
	return out.String(), nil
}

func (e *Engine) resolveNodeAttr(projectID uint, label, attrName string, visited map[string]bool) (string, error) {
	node, err := e.store.GetNodeByLabel(projectID, label)
	if err != nil {
		return "", fmt.Errorf("no service named %q", label)
	}

	if isGeneratedAttr(attrName) {
		addr, err := e.computeNodeAddress(node)
		if err != nil {
			return "", fmt.Errorf("resolving %q on %q: %w", attrName, label, err)
		}
		value, _ := addr.attr(attrName)
		return value, nil
	}

	if visited[node.ID] {
		return "", fmt.Errorf("circular variable reference involving %q", label)
	}

	v, err := e.store.GetEnvVar(node.ID, attrName)
	if err != nil {
		return "", fmt.Errorf("%q has no variable named %q", label, attrName)
	}

	visited[node.ID] = true
	defer delete(visited, node.ID)
	return e.resolveValue(projectID, v.Value, visited)
}

func isGeneratedAttr(attrName string) bool {
	for _, a := range generatedAttrs {
		if a == attrName {
			return true
		}
	}
	return false
}

// ResolveEnvVars returns nodeID's env vars with every reference token
// substituted for its effective value — the same resolution used at deploy
// time. Used when exporting a real .env file, where the reference syntax
// itself would be meaningless to anything reading the file.
func (e *Engine) ResolveEnvVars(nodeID string) ([]store.EnvVar, error) {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	vars, err := e.store.ListEnvVars(nodeID)
	if err != nil {
		return nil, err
	}
	resolved := make([]store.EnvVar, len(vars))
	for i, v := range vars {
		value, err := e.resolveValue(node.ProjectID, v.Value, map[string]bool{nodeID: true})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", v.Key, err)
		}
		v.Value = value
		resolved[i] = v
	}
	return resolved, nil
}

// EnvPreview is a single variable's resolved value, or the error that
// resolution hit, for read-only display (e.g. the Variables tab).
type EnvPreview struct {
	Value string `json:"value"`
	Error string `json:"error,omitempty"`
}

// PreviewEnvVars resolves nodeID's env vars per key, tolerating errors so one
// broken reference doesn't hide every other variable's preview.
func (e *Engine) PreviewEnvVars(nodeID string) (map[string]EnvPreview, error) {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	vars, err := e.store.ListEnvVars(nodeID)
	if err != nil {
		return nil, err
	}
	out := make(map[string]EnvPreview, len(vars))
	for _, v := range vars {
		value, err := e.resolveValue(node.ProjectID, v.Value, map[string]bool{nodeID: true})
		if err != nil {
			out[v.Key] = EnvPreview{Error: err.Error()}
			continue
		}
		out[v.Key] = EnvPreview{Value: value}
	}
	return out, nil
}

// ReferenceTarget describes another node in the project a variable could
// reference, and what's available on it: the generated address attributes
// every node exposes, plus its own custom variable keys.
type ReferenceTarget struct {
	NodeID     string   `json:"nodeId"`
	Label      string   `json:"label"`
	Attributes []string `json:"attributes"`
	CustomKeys []string `json:"customKeys"`
}

// ListReferenceTargets returns every other node in nodeID's project that can
// be safely referenced from it — excluding nodes that already (transitively)
// reference nodeID, since picking one of those would create a cycle.
func (e *Engine) ListReferenceTargets(nodeID string) ([]ReferenceTarget, error) {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	nodes, err := e.store.ListNodes(node.ProjectID)
	if err != nil {
		return nil, err
	}
	conns, err := e.GetProjectConnections(node.ProjectID)
	if err != nil {
		return nil, err
	}
	blocked := nodesThatReach(nodeID, conns)

	targets := make([]ReferenceTarget, 0, len(nodes))
	for _, n := range nodes {
		if n.ID == nodeID || blocked[n.ID] {
			continue
		}
		vars, err := e.store.ListEnvVars(n.ID)
		if err != nil {
			return nil, err
		}
		customKeys := make([]string, 0, len(vars))
		for _, v := range vars {
			customKeys = append(customKeys, v.Key)
		}
		sort.Strings(customKeys)
		targets = append(targets, ReferenceTarget{
			NodeID:     n.ID,
			Label:      n.Label,
			Attributes: append([]string(nil), generatedAttrs...),
			CustomKeys: customKeys,
		})
	}
	sort.Slice(targets, func(i, j int) bool { return targets[i].Label < targets[j].Label })
	return targets, nil
}

// nodesThatReach returns every node ID with a reference path (direct or
// transitive) to target, via a reverse BFS over conns.
func nodesThatReach(target string, conns []Connection) map[string]bool {
	incoming := map[string][]string{}
	for _, c := range conns {
		incoming[c.TargetNodeID] = append(incoming[c.TargetNodeID], c.SourceNodeID)
	}
	reach := map[string]bool{}
	queue := []string{target}
	for len(queue) > 0 {
		cur := queue[0]
		queue = queue[1:]
		for _, src := range incoming[cur] {
			if !reach[src] {
				reach[src] = true
				queue = append(queue, src)
			}
		}
	}
	return reach
}

// Connection is a derived, read-only edge between two nodes: sourceKey on
// sourceNodeId contains a reference to targetAttr on targetNodeId. There's no
// separate storage for this — it's parsed fresh from env var values, which
// stay the single source of truth.
type Connection struct {
	SourceNodeID string `json:"sourceNodeId"`
	SourceKey    string `json:"sourceKey"`
	TargetNodeID string `json:"targetNodeId"`
	TargetAttr   string `json:"targetAttr"`
}

// GetProjectConnections scans every node's env vars in the project for
// reference tokens and returns the resulting edges, for the canvas to render
// read-only (no drag-to-connect — connections are only created via the
// variable picker).
func (e *Engine) GetProjectConnections(projectID uint) ([]Connection, error) {
	nodes, err := e.store.ListNodes(projectID)
	if err != nil {
		return nil, err
	}
	labelToID := make(map[string]string, len(nodes))
	for _, n := range nodes {
		labelToID[strings.ToLower(strings.TrimSpace(n.Label))] = n.ID
	}

	var conns []Connection
	for _, n := range nodes {
		vars, err := e.store.ListEnvVars(n.ID)
		if err != nil {
			return nil, err
		}
		for _, v := range vars {
			for _, m := range refPattern.FindAllStringSubmatch(v.Value, -1) {
				label, attr := m[1], m[2]
				targetID, ok := labelToID[strings.ToLower(strings.TrimSpace(label))]
				if !ok {
					continue
				}
				conns = append(conns, Connection{
					SourceNodeID: n.ID,
					SourceKey:    v.Key,
					TargetNodeID: targetID,
					TargetAttr:   attr,
				})
			}
		}
	}
	return conns, nil
}
