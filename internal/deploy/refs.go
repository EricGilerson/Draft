package deploy

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"Draft/internal/networking"
	"Draft/internal/store"

	"gorm.io/gorm"
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
	ServiceName string
	ProjectName string
	// Environment is the raw environment slug (DRAFT_ENVIRONMENT, hostname env label).
	Environment string
	// DockerEnvironment is the env segment for Docker network/image/container/volume
	// names. Sandboxes use sand-{slug}; normal envs use the raw slug.
	DockerEnvironment string
	// Sandbox is true when the node's environment is a disposable sandbox.
	Sandbox          bool
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
	sandbox := false
	if env, err := e.store.GetEnvironment(node.EnvironmentID); err == nil {
		environment = env.Slug
		sandbox, err = e.isSandboxEnvironment(env.ID)
		if err != nil {
			return NodeAddress{}, err
		}
	}
	dockerEnv := networking.DockerEnvironment(environment, sandbox)
	portStr := settings["service_port"]
	protocol := strings.TrimSpace(settings["route_protocol"])

	hostname := networking.FormatHostname(serviceName, projectName, environment, uid, sandbox)
	publicHostname := networking.PublicHostname(hostname)
	localDomain := networking.LocalDomainStatus{}
	if e.router != nil {
		localDomain = e.router.LocalDomainStatus()
		if localDomain.DraftEnabled && localDomain.DNSVerified {
			publicHostname = networking.LocalHostname(hostname)
		}
	}
	// HTTP: reverse-proxy public URL. TCP: scheme-less host:port so @{refs}
	// and inject never invent http:// for wire protocols. Prefer the leased
	// route/deployment host port when known; otherwise the preferred host_port
	// (or service_port) so pre-deploy stamps still have a usable guess.
	internalURL := networking.ServiceInternalURL(hostname, portStr, protocol)
	publicURL := ""
	if networking.IsTCPProtocol(protocol) {
		if n := e.leasedTCPHostPort(node.ID, hostname); n > 0 {
			publicURL = fmt.Sprintf("%s:%d", publicHostname, n)
		} else {
			pubPort := portStr
			if hp := strings.TrimSpace(settings["host_port"]); hp != "" && hp != "0" {
				pubPort = hp
			}
			if n, err := strconv.Atoi(pubPort); err == nil && n > 0 {
				publicURL = fmt.Sprintf("%s:%d", publicHostname, n)
			}
		}
	} else if e.router != nil {
		if localDomain.ProxyPort == 80 {
			publicURL = "http://" + publicHostname
		} else if localDomain.ProxyPort > 0 {
			publicURL = fmt.Sprintf("http://%s:%d", publicHostname, localDomain.ProxyPort)
		}
	}

	return NodeAddress{
		ServiceName:       serviceName,
		ProjectName:       projectName,
		Environment:       environment,
		DockerEnvironment: dockerEnv,
		Sandbox:           sandbox,
		ServicePort:       portStr,
		InternalHostname:  hostname,
		InternalURL:       internalURL,
		PublicHostname:    publicHostname,
		PublicURL:         publicURL,
	}, nil
}

// leasedTCPHostPort returns the host port Draft actually bound for a TCP
// service, preferring the persisted route (stable across restarts) and falling
// back to the active deployment. Zero means "not leased yet" — callers should
// use the preferred host_port setting instead.
func (e *Engine) leasedTCPHostPort(nodeID, hostname string) int {
	if e.store == nil {
		return 0
	}
	if hostname != "" {
		if route, err := e.store.GetRoute(hostname); err == nil && route != nil {
			if networking.IsTCPProtocol(route.Protocol) && route.HostPort > 0 {
				return route.HostPort
			}
		}
	}
	if routes, err := e.store.ListRoutesByNode(nodeID); err == nil {
		for _, route := range routes {
			if networking.IsTCPProtocol(route.Protocol) && route.HostPort > 0 {
				return route.HostPort
			}
		}
	}
	if dep, err := e.store.ActiveDeployment(nodeID); err == nil && dep != nil && dep.HostPort > 0 {
		settings, err := e.store.GetNodeSettings(nodeID)
		if err == nil && networking.IsTCPProtocol(settings["route_protocol"]) {
			return dep.HostPort
		}
	}
	return 0
}

// isSandboxEnvironment reports whether environmentID belongs to a sandbox.
// Used for DNS (.sand segment) and Docker identity (sand-{slug}).
// Not-found is false; other store errors are returned so callers do not mint
// non-sandbox identity on a transient failure.
func (e *Engine) isSandboxEnvironment(environmentID uint) (bool, error) {
	if environmentID == 0 {
		return false, nil
	}
	_, err := e.store.GetSandboxByEnvironment(environmentID)
	if err == nil {
		return true, nil
	}
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, nil
	}
	return false, err
}

// resolveValue substitutes every {{draft.X}} template expression (resolved
// against the owning node's identity) and every @{Label.ATTR} reference token
// in raw with its resolved value. Template expressions expand first, against
// selfNodeID's identity, so a value like "@{db.DATABASE_URL}" picks up the db's
// own already-expanded {{draft.*}} connection string when it recurses.
// projectID is the (constant) project scope for {{project.*}} lookups;
// environmentID is the (constant) environment scope for @{Label.ATTR} sibling
// lookups, so a reference never resolves across environments; selfNodeID is
// the node whose value is being resolved and changes per recursion level.
// visited tracks node IDs on the current reference path (seeded with the
// starting node) so a reference cycle fails fast with a readable error instead
// of recursing forever.
func (e *Engine) resolveValue(selfNodeID string, projectID, environmentID uint, raw string, visited map[string]bool) (string, error) {
	return e.resolveValueOpts(selfNodeID, projectID, environmentID, raw, visited, false)
}

func (e *Engine) resolveValueForExport(selfNodeID string, projectID, environmentID uint, raw string, visited map[string]bool) (string, error) {
	return e.resolveValueOpts(selfNodeID, projectID, environmentID, raw, visited, true)
}

func (e *Engine) resolveValueOpts(selfNodeID string, projectID, environmentID uint, raw string, visited map[string]bool, preserveSecretExprs bool) (string, error) {
	if strings.Contains(raw, "{{draft.") {
		in, err := e.nodeExprInput(selfNodeID)
		if err != nil {
			return "", fmt.Errorf("resolve draft expressions for %q: %w", selfNodeID, err)
		}
		expanded, err := resolveTemplateExprs(in, raw)
		if err != nil {
			return "", err
		}
		raw = expanded
	}

	if containsSecretExpr(raw) {
		expanded, err := e.resolveSecretExprs(raw, preserveSecretExprs)
		if err != nil {
			return "", err
		}
		raw = expanded
	}

	if containsProjectExpr(raw) {
		expanded, err := e.resolveProjectExprs(selfNodeID, projectID, environmentID, raw, visited, preserveSecretExprs)
		if err != nil {
			return "", err
		}
		raw = expanded
	}

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
		resolved, err := e.resolveNodeAttr(selfNodeID, projectID, environmentID, label, attrName, visited)
		if err != nil {
			return "", fmt.Errorf("%s: %w", raw[m[0]:m[1]], err)
		}
		out.WriteString(resolved)
		last = m[1]
	}
	out.WriteString(raw[last:])
	return out.String(), nil
}

func (e *Engine) resolveNodeAttr(selfNodeID string, projectID, environmentID uint, label, attrName string, visited map[string]bool) (string, error) {
	node, err := e.store.GetNodeByLabel(environmentID, label)
	if err != nil {
		return "", fmt.Errorf("no service named %q", label)
	}

	if isGeneratedAttr(attrName) {
		// Generated address attrs stay on the alias so DNS names match the
		// multi-attached aliases on the linker environment network.
		addr, err := e.computeNodeAddress(node)
		if err != nil {
			return "", fmt.Errorf("resolving %q on %q: %w", attrName, label, err)
		}
		value, _ := addr.attr(attrName)
		return value, nil
	}

	// Non-generated env vars (passwords, connection strings, etc.) follow one
	// hop to the root when this node is a linked service, so credentials stay
	// consistent with the running container.
	aliasNode := node
	resolveNode := node
	resolveEnvID := environmentID
	if settings, err := e.store.GetNodeSettings(node.ID); err == nil {
		if link := ParseServiceLink(settings[SettingServiceLink]); link != nil {
			if root, err := e.store.GetNode(link.RootNodeID); err == nil {
				resolveNode = root
				resolveEnvID = root.EnvironmentID
			}
		}
	}

	if visited[resolveNode.ID] {
		return "", fmt.Errorf("circular variable reference involving %q", label)
	}

	v, err := e.store.GetEnvVar(resolveNode.ID, attrName)
	if err != nil {
		return "", fmt.Errorf("%q has no variable named %q", label, attrName)
	}

	visited[resolveNode.ID] = true
	defer delete(visited, resolveNode.ID)
	// The referenced node owns this value, so its draft expressions resolve
	// against the referenced node's identity, not the caller's.
	value, err := e.resolveValue(resolveNode.ID, projectID, resolveEnvID, v.Value, visited)
	if err != nil {
		return "", err
	}
	// Root-stamped connection strings embed the root's internal hostname.
	// Consumers in the linker env need the alias hostname (Docker multi-attach
	// DNS). Swap known root address strings for the alias's — not URL parsing.
	if resolveNode.ID != aliasNode.ID {
		value = e.rewriteRootAddressesToAlias(value, resolveNode, aliasNode)
	}
	return value, nil
}

// rewriteRootAddressesToAlias replaces concrete root address strings in value
// with the corresponding alias (linker-env) addresses. Longer strings first so
// full URLs rewrite before bare hostnames. No-ops when either address is missing
// or when root and alias already share the same strings.
func (e *Engine) rewriteRootAddressesToAlias(value string, root, alias *store.CanvasNode) string {
	if value == "" || root == nil || alias == nil || root.ID == alias.ID {
		return value
	}
	rootAddr, err := e.computeNodeAddress(root)
	if err != nil {
		return value
	}
	aliasAddr, err := e.computeNodeAddress(alias)
	if err != nil {
		return value
	}
	return rewriteAddressStrings(value, rootAddr, aliasAddr)
}

// rewriteAddressStrings performs literal root→alias substitutions ordered by
// old-string length (longest first) so nested forms rewrite cleanly.
func rewriteAddressStrings(value string, root, alias NodeAddress) string {
	type pair struct{ old, new string }
	candidates := []pair{
		{root.PublicURL, alias.PublicURL},
		{root.InternalURL, alias.InternalURL},
		{root.PublicHostname, alias.PublicHostname},
		{root.InternalHostname, alias.InternalHostname},
	}
	var pairs []pair
	for _, p := range candidates {
		if p.old == "" || p.new == "" || p.old == p.new {
			continue
		}
		pairs = append(pairs, p)
	}
	sort.Slice(pairs, func(i, j int) bool {
		return len(pairs[i].old) > len(pairs[j].old)
	})
	for _, p := range pairs {
		value = strings.ReplaceAll(value, p.old, p.new)
	}
	return value
}

// rewriteValueIfLinked rewrites root address strings to this node's alias
// addresses when nodeID is a linked service. Used for preview/export of the
// alias's own env rows (copied values still embed the root hostname).
func (e *Engine) rewriteValueIfLinked(nodeID, value string) string {
	link, err := e.GetServiceLink(nodeID)
	if err != nil || link == nil {
		return value
	}
	alias, err := e.store.GetNode(nodeID)
	if err != nil {
		return value
	}
	root, err := e.store.GetNode(link.RootNodeID)
	if err != nil {
		return value
	}
	return e.rewriteRootAddressesToAlias(value, root, alias)
}

func isGeneratedAttr(attrName string) bool {
	for _, a := range generatedAttrs {
		if a == attrName {
			return true
		}
	}
	return false
}

// referenceEnvVarNodeID returns the node whose custom env vars should be
// listed or validated for a reference target. Linked aliases follow the same
// one-hop-to-root rule as resolveNodeAttr so UI pickers and warnings match deploy.
func referenceEnvVarNodeID(s *store.Store, targetNodeID string) (string, error) {
	settings, err := s.GetNodeSettings(targetNodeID)
	if err != nil {
		return "", err
	}
	if link := ParseServiceLink(settings[SettingServiceLink]); link != nil && strings.TrimSpace(link.RootNodeID) != "" {
		return link.RootNodeID, nil
	}
	return targetNodeID, nil
}

// ResolveEnvVars returns nodeID's env vars with service reference tokens
// expanded for .env export. {{secret.*}} and {{project.*}} tokens are preserved literally.
// Linked aliases rewrite root hostnames to the alias DNS names used on the
// linker environment network.
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
		value, err := e.resolveValueForExport(node.ID, node.ProjectID, node.EnvironmentID, v.Value, map[string]bool{nodeID: true})
		if err != nil {
			return nil, fmt.Errorf("%s: %w", v.Key, err)
		}
		v.Value = e.rewriteValueIfLinked(nodeID, value)
		resolved[i] = v
	}
	return resolved, nil
}

// EnvPreview is a single variable's resolved value, or the error that
// resolution hit, for read-only display (e.g. the Variables tab).
// Kind is set only when Value differs from the stored string:
//   - "resolve": tokens/expressions expanded (and any linked rewrite applied after)
//   - "linked": plain stored value rewritten for a linked alias's DNS only
type EnvPreview struct {
	Value string `json:"value"`
	Error string `json:"error,omitempty"`
	Kind  string `json:"kind,omitempty"`
}

// previewKind classifies why final differs from stored using the intermediate
// resolve step — no token parsing required.
func previewKind(stored, resolved, final string) string {
	if final == stored {
		return ""
	}
	if resolved != stored {
		return "resolve"
	}
	return "linked"
}

// PreviewEnvVars resolves nodeID's env vars per key, tolerating errors so one
// broken reference doesn't hide every other variable's preview.
// Linked aliases rewrite root hostnames to alias DNS names so previews match
// what consumers resolve over the multi-attached network.
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
		resolved, err := e.resolveValue(node.ID, node.ProjectID, node.EnvironmentID, v.Value, map[string]bool{nodeID: true})
		if err != nil {
			out[v.Key] = EnvPreview{Error: err.Error()}
			continue
		}
		final := e.rewriteValueIfLinked(nodeID, resolved)
		out[v.Key] = EnvPreview{Value: final, Kind: previewKind(v.Value, resolved, final)}
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

// ListReferenceTargets returns every other node in nodeID's project along with
// the attributes that can be referenced from it without creating a cycle.
// Generated address attrs are always offered; custom variable keys are omitted
// only when resolving them from nodeID would hit a circular reference.
func (e *Engine) ListReferenceTargets(nodeID string) ([]ReferenceTarget, error) {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	nodes, err := e.store.ListNodesByEnvironment(node.EnvironmentID)
	if err != nil {
		return nil, err
	}

	targets := make([]ReferenceTarget, 0, len(nodes))
	for _, n := range nodes {
		if n.ID == nodeID {
			continue
		}
		envNodeID, err := referenceEnvVarNodeID(e.store, n.ID)
		if err != nil {
			return nil, err
		}
		vars, err := e.store.ListEnvVars(envNodeID)
		if err != nil {
			return nil, err
		}
		customKeys := make([]string, 0, len(vars))
		for _, v := range vars {
			if e.referenceWouldCycle(nodeID, node.ProjectID, node.EnvironmentID, n.ID, v.Key) {
				continue
			}
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

// referenceWouldCycle reports whether nodeID referencing attrName on targetNodeID
// would recurse back into nodeID. Generated address attrs never cycle.
func (e *Engine) referenceWouldCycle(nodeID string, projectID, environmentID uint, targetNodeID, attrName string) bool {
	if isGeneratedAttr(attrName) {
		return false
	}
	target, err := e.store.GetNode(targetNodeID)
	if err != nil {
		return true
	}
	_, err = e.resolveNodeAttr(nodeID, projectID, environmentID, target.Label, attrName, map[string]bool{nodeID: true})
	if err == nil {
		return false
	}
	return strings.Contains(err.Error(), "circular")
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

// ReferenceIssue describes a @{Label.ATTR} token in an env var value that
// cannot be resolved — unknown service, missing variable, etc.
type ReferenceIssue struct {
	VarKey string `json:"varKey"`
	Token  string `json:"token"`
	Reason string `json:"reason"`
}

// ListReferenceIssuesFromStore scans nodeID's env vars for broken reference
// tokens using SQLite only.
func ListReferenceIssuesFromStore(s *store.Store, nodeID string) ([]ReferenceIssue, error) {
	return listReferenceIssues(s, nodeID)
}

// ListReferenceIssues scans nodeID's env vars for reference tokens that point
// at a missing service or attribute. Used for inline UI warnings without
// waiting for full recursive preview resolution.
func (e *Engine) ListReferenceIssues(nodeID string) ([]ReferenceIssue, error) {
	return listReferenceIssues(e.store, nodeID)
}

func listReferenceIssues(s *store.Store, nodeID string) ([]ReferenceIssue, error) {
	node, err := s.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	nodes, err := s.ListNodesByEnvironment(node.EnvironmentID)
	if err != nil {
		return nil, err
	}
	labelToNode := make(map[string]*store.CanvasNode, len(nodes))
	for i := range nodes {
		key := strings.ToLower(strings.TrimSpace(nodes[i].Label))
		labelToNode[key] = &nodes[i]
	}

	vars, err := s.ListEnvVars(nodeID)
	if err != nil {
		return nil, err
	}

	var issues []ReferenceIssue
	for _, v := range vars {
		issues = append(issues, listMissingSecretExprs(s, v.Key, v.Value)...)
		issues = append(issues, listMissingProjectExprs(s, node.ProjectID, v.Key, v.Value)...)
		for _, m := range refPattern.FindAllStringSubmatch(v.Value, -1) {
			label, attrName := m[1], m[2]
			token := m[0]
			target, ok := labelToNode[strings.ToLower(strings.TrimSpace(label))]
			if !ok {
				issues = append(issues, ReferenceIssue{
					VarKey: v.Key,
					Token:  token,
					Reason: fmt.Sprintf("no service named %q", label),
				})
				continue
			}
			if isGeneratedAttr(attrName) {
				continue
			}
			envNodeID, err := referenceEnvVarNodeID(s, target.ID)
			if err != nil {
				issues = append(issues, ReferenceIssue{
					VarKey: v.Key,
					Token:  token,
					Reason: fmt.Sprintf("%q has no variable named %q", label, attrName),
				})
				continue
			}
			if _, err := s.GetEnvVar(envNodeID, attrName); err != nil {
				issues = append(issues, ReferenceIssue{
					VarKey: v.Key,
					Token:  token,
					Reason: fmt.Sprintf("%q has no variable named %q", label, attrName),
				})
			}
		}
	}
	return issues, nil
}

// ListNodesWithReferenceIssues returns the IDs of every node in the
// environment that has at least one unresolved @{Label.ATTR} token in its env
// vars.
func ListNodesWithReferenceIssues(s *store.Store, environmentID uint) ([]string, error) {
	nodes, err := s.ListNodesByEnvironment(environmentID)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, n := range nodes {
		issues, err := listReferenceIssues(s, n.ID)
		if err != nil {
			return nil, err
		}
		if len(issues) > 0 {
			out = append(out, n.ID)
		}
	}
	if out == nil {
		out = []string{}
	}
	return out, nil
}

// ListServiceDependentsFromStore returns services whose env vars reference
// nodeID, using SQLite only.
func ListServiceDependentsFromStore(s *store.Store, nodeID string) ([]ReferenceDependent, error) {
	return listServiceDependents(s, nodeID)
}

// ListServiceDependents returns every other service in the project whose env
// vars still reference nodeID (by label). Reference values are not modified.
func (e *Engine) ListServiceDependents(nodeID string) ([]ReferenceDependent, error) {
	return listServiceDependents(e.store, nodeID)
}

func listServiceDependents(s *store.Store, nodeID string) ([]ReferenceDependent, error) {
	node, err := s.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	nodes, err := s.ListNodesByEnvironment(node.EnvironmentID)
	if err != nil {
		return nil, err
	}
	idToLabel := make(map[string]string, len(nodes))
	for _, n := range nodes {
		idToLabel[n.ID] = n.Label
	}

	conns, err := getEnvironmentConnections(s, node.EnvironmentID)
	if err != nil {
		return nil, err
	}

	var out []ReferenceDependent
	for _, c := range conns {
		if c.TargetNodeID != nodeID {
			continue
		}
		out = append(out, ReferenceDependent{
			SourceNodeID: c.SourceNodeID,
			SourceLabel:  idToLabel[c.SourceNodeID],
			VarKey:       c.SourceKey,
			Token:        fmt.Sprintf("@{%s.%s}", node.Label, c.TargetAttr),
		})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].SourceLabel != out[j].SourceLabel {
			return out[i].SourceLabel < out[j].SourceLabel
		}
		if out[i].VarKey != out[j].VarKey {
			return out[i].VarKey < out[j].VarKey
		}
		return out[i].Token < out[j].Token
	})
	if out == nil {
		out = []ReferenceDependent{}
	}
	return out, nil
}

// GetEnvironmentConnections scans every node's env vars in the environment for
// reference tokens and returns the resulting edges, for the canvas to render
// read-only (no drag-to-connect — connections are only created via the
// variable picker).
func (e *Engine) GetEnvironmentConnections(environmentID uint) ([]Connection, error) {
	return getEnvironmentConnections(e.store, environmentID)
}

func getEnvironmentConnections(s *store.Store, environmentID uint) ([]Connection, error) {
	nodes, err := s.ListNodesByEnvironment(environmentID)
	if err != nil {
		return nil, err
	}
	labelToID := make(map[string]string, len(nodes))
	for _, n := range nodes {
		labelToID[strings.ToLower(strings.TrimSpace(n.Label))] = n.ID
	}

	var conns []Connection
	for _, n := range nodes {
		vars, err := s.ListEnvVars(n.ID)
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
