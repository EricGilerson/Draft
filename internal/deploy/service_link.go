package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"

	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/api/types/network"
	"github.com/docker/docker/client"
	"github.com/docker/docker/errdefs"
)

// SettingServiceLink is the node_settings key for a virtualized (shared) service.
// Value is JSON ServiceLink. Empty/missing means the node is a normal root.
const SettingServiceLink = "service_link"

// ServiceLink points an alias node at a root service in another (or same) env.
// Chains are forbidden: rootNodeId must itself have no service_link.
type ServiceLink struct {
	RootNodeID        string `json:"rootNodeId"`
	RootEnvironmentID uint   `json:"rootEnvironmentId"`
}

// ServiceDataMode is how a stateful service is handled when duplicating an env.
type ServiceDataMode string

const (
	ServiceDataFresh ServiceDataMode = "fresh"
	ServiceDataShare ServiceDataMode = "share"
	ServiceDataClone ServiceDataMode = "clone"
)

// CloneConsistency controls whether the source is stopped during a volume copy.
type CloneConsistency string

const (
	CloneConsistent CloneConsistency = "consistent"
	CloneQuick      CloneConsistency = "quick"
)

// ServiceDataChoice is one row in the duplicate-environment wizard.
type ServiceDataChoice struct {
	SourceNodeID string           `json:"sourceNodeId"`
	Mode         ServiceDataMode  `json:"mode"`
	Consistency  CloneConsistency `json:"consistency,omitempty"`
}

// StatefulServiceSummary describes a source service shown in the duplicate-
// environment wizard. Volume-backed services can also choose clone; every
// non-alias root can choose fresh (independent copy) or share (service link).
type StatefulServiceSummary struct {
	NodeID      string   `json:"nodeId"`
	Label       string   `json:"label"`
	TemplateID  uint     `json:"templateId"`
	Volumes     []string `json:"volumes"` // container paths; empty => no clone option
	WarningKind string   `json:"warningKind"`
	Warning     string   `json:"warning"`
}

// RootServiceSummary is a shareable/cloneable root in a project.
type RootServiceSummary struct {
	NodeID        string `json:"nodeId"`
	Label         string `json:"label"`
	EnvironmentID uint   `json:"environmentId"`
	EnvName       string `json:"envName"`
	EnvSlug       string `json:"envSlug"`
	TemplateID    uint   `json:"templateId"`
	HasVolumes    bool   `json:"hasVolumes"`
	WarningKind   string `json:"warningKind,omitempty"`
	Warning       string `json:"warning,omitempty"`
	// MatchReason is set on ListShareTargets matched roots (label+template, etc.).
	MatchReason string `json:"matchReason,omitempty"`
}

// ShareTargetEnvironment groups shareable roots in one other environment, with
// an optional best "same service" match for the source node.
type ShareTargetEnvironment struct {
	EnvironmentID uint                 `json:"environmentId"`
	EnvName       string               `json:"envName"`
	EnvSlug       string               `json:"envSlug"`
	MatchedRoot   *RootServiceSummary  `json:"matchedRoot,omitempty"`
	Roots         []RootServiceSummary `json:"roots"`
}

// VolumeDisposition controls what happens to this node's managed volumes when
// converting it into a shared (linked) alias.
type VolumeDisposition string

const (
	VolumeOrphan VolumeDisposition = "orphan"
	VolumeDelete VolumeDisposition = "delete"
)

// LinkToSharedRootPreview describes the effects of LinkToSharedRoot.
type LinkToSharedRootPreview struct {
	NodeLabel           string `json:"nodeLabel"`
	IsRunning           bool   `json:"isRunning"`
	ManagedVolumeCount  int    `json:"managedVolumeCount"`
	RootNodeID          string `json:"rootNodeId"`
	RootLabel           string `json:"rootLabel"`
	RootEnvName         string `json:"rootEnvName"`
	WarningKind         string `json:"warningKind,omitempty"`
	Warning             string `json:"warning,omitempty"`
}

// LinkedServiceInfo is returned for UI (badge, overview).
type LinkedServiceInfo struct {
	IsLinked          bool   `json:"isLinked"`
	RootNodeID        string `json:"rootNodeId,omitempty"`
	RootLabel         string `json:"rootLabel,omitempty"`
	RootEnvironmentID uint   `json:"rootEnvironmentId,omitempty"`
	RootEnvName       string `json:"rootEnvName,omitempty"`
	// HasVolumes is true when the root has managed named volumes (clone promote is meaningful).
	HasVolumes bool `json:"hasVolumes,omitempty"`
}

// ParseServiceLink decodes the service_link setting. Empty/malformed → nil.
func ParseServiceLink(raw string) *ServiceLink {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var link ServiceLink
	if json.Unmarshal([]byte(raw), &link) != nil {
		return nil
	}
	if strings.TrimSpace(link.RootNodeID) == "" {
		return nil
	}
	return &link
}

// GetServiceLink returns the parsed link for a node, or nil if it is a root.
func (e *Engine) GetServiceLink(nodeID string) (*ServiceLink, error) {
	settings, err := e.store.GetNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}
	return ParseServiceLink(settings[SettingServiceLink]), nil
}

// IsLinkedService reports whether nodeID is an alias of another service.
func (e *Engine) IsLinkedService(nodeID string) (bool, error) {
	link, err := e.GetServiceLink(nodeID)
	if err != nil {
		return false, err
	}
	return link != nil, nil
}

// GetLinkedServiceInfo returns UI-facing link metadata for a node.
func (e *Engine) GetLinkedServiceInfo(nodeID string) (*LinkedServiceInfo, error) {
	link, err := e.GetServiceLink(nodeID)
	if err != nil {
		return nil, err
	}
	if link == nil {
		return &LinkedServiceInfo{IsLinked: false}, nil
	}
	info := &LinkedServiceInfo{
		IsLinked:          true,
		RootNodeID:        link.RootNodeID,
		RootEnvironmentID: link.RootEnvironmentID,
	}
	if root, err := e.store.GetNode(link.RootNodeID); err == nil {
		info.RootLabel = root.Label
	}
	if env, err := e.store.GetEnvironment(link.RootEnvironmentID); err == nil {
		info.RootEnvName = env.Name
	}
	if settings, err := e.store.GetNodeSettings(link.RootNodeID); err == nil {
		info.HasVolumes = len(managedVolumePaths(settings)) > 0
	}
	return info, nil
}

// ListLinkers returns every alias node that points at rootNodeID.
func (e *Engine) ListLinkers(rootNodeID string) ([]store.CanvasNode, error) {
	// service_link is in node_settings; scan project nodes is fine at local scale.
	root, err := e.store.GetNode(rootNodeID)
	if err != nil {
		return nil, err
	}
	nodes, err := e.store.ListNodes(root.ProjectID)
	if err != nil {
		return nil, err
	}
	var out []store.CanvasNode
	for _, n := range nodes {
		if n.ID == rootNodeID {
			continue
		}
		settings, err := e.store.GetNodeSettings(n.ID)
		if err != nil {
			return nil, err
		}
		link := ParseServiceLink(settings[SettingServiceLink])
		if link != nil && link.RootNodeID == rootNodeID {
			out = append(out, n)
		}
	}
	return out, nil
}

// validateLinkTarget ensures rootNodeID is a real root (not itself linked).
func (e *Engine) validateLinkTarget(rootNodeID string) (*store.CanvasNode, error) {
	root, err := e.store.GetNode(rootNodeID)
	if err != nil {
		return nil, fmt.Errorf("root service not found: %w", err)
	}
	settings, err := e.store.GetNodeSettings(rootNodeID)
	if err != nil {
		return nil, err
	}
	if ParseServiceLink(settings[SettingServiceLink]) != nil {
		return nil, fmt.Errorf("cannot link to %q: it is already a linked service (no chain linking)", root.Label)
	}
	return root, nil
}

// SetServiceLink writes a validated service_link on aliasNodeID and clears local
// volume_mounts (aliases do not own volumes). Does not touch Docker networks;
// call EnsureServiceLinkNetworks after the root is running.
func (e *Engine) SetServiceLink(aliasNodeID, rootNodeID string) error {
	if aliasNodeID == rootNodeID {
		return fmt.Errorf("a service cannot link to itself")
	}
	alias, err := e.store.GetNode(aliasNodeID)
	if err != nil {
		return fmt.Errorf("alias service not found: %w", err)
	}
	root, err := e.validateLinkTarget(rootNodeID)
	if err != nil {
		return err
	}
	if alias.ProjectID != root.ProjectID {
		return fmt.Errorf("can only share services within the same project")
	}
	// Alias must not already be a root that other aliases depend on.
	linkers, err := e.ListLinkers(aliasNodeID)
	if err != nil {
		return err
	}
	if len(linkers) > 0 {
		return fmt.Errorf("cannot convert %q to a link: other environments still link to it", alias.Label)
	}

	payload, err := json.Marshal(ServiceLink{
		RootNodeID:        root.ID,
		RootEnvironmentID: root.EnvironmentID,
	})
	if err != nil {
		return err
	}
	if err := e.store.SetNodeSetting(aliasNodeID, SettingServiceLink, string(payload)); err != nil {
		return err
	}
	// Aliases do not deploy local volumes.
	_ = e.store.SetNodeSetting(aliasNodeID, "volume_mounts", "[]")
	return nil
}

// PreviewLinkToSharedRoot describes what LinkToSharedRoot will do.
func (e *Engine) PreviewLinkToSharedRoot(nodeID, rootNodeID string) (*LinkToSharedRootPreview, error) {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}
	if link, err := e.GetServiceLink(nodeID); err != nil {
		return nil, err
	} else if link != nil {
		return nil, fmt.Errorf("service is already linked")
	}
	root, err := e.validateLinkTarget(rootNodeID)
	if err != nil {
		return nil, err
	}
	if node.ProjectID != root.ProjectID {
		return nil, fmt.Errorf("can only share services within the same project")
	}
	if node.EnvironmentID == root.EnvironmentID {
		return nil, fmt.Errorf("can only share a service from another environment")
	}

	preview := &LinkToSharedRootPreview{
		NodeLabel:  node.Label,
		RootNodeID: root.ID,
		RootLabel:  root.Label,
	}
	if env, err := e.store.GetEnvironment(root.EnvironmentID); err == nil {
		preview.RootEnvName = env.Name
	}
	rootSettings, err := e.store.GetNodeSettings(root.ID)
	if err != nil {
		return nil, err
	}
	preview.WarningKind, preview.Warning = shareWarningForNode(e.store, root, rootSettings)

	settings, err := e.store.GetNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}
	preview.ManagedVolumeCount = len(managedVolumePaths(settings))
	if dep, _ := e.store.ActiveDeployment(nodeID); dep != nil &&
		(dep.Status == "running" || dep.Status == "starting" || dep.Status == "building") {
		preview.IsRunning = true
	}
	return preview, nil
}

// LinkToSharedRoot converts a local root into a linked alias of rootNodeID.
// volumes is "orphan" (default) or "delete" for this node's managed volumes.
func (e *Engine) LinkToSharedRoot(ctx context.Context, nodeID, rootNodeID string, volumes VolumeDisposition) error {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return fmt.Errorf("service not found: %w", err)
	}
	if link, err := e.GetServiceLink(nodeID); err != nil {
		return err
	} else if link != nil {
		return fmt.Errorf("service is already linked")
	}
	root, err := e.validateLinkTarget(rootNodeID)
	if err != nil {
		return err
	}
	if node.ProjectID != root.ProjectID {
		return fmt.Errorf("can only share services within the same project")
	}
	if node.EnvironmentID == root.EnvironmentID {
		return fmt.Errorf("can only share a service from another environment")
	}
	if volumes == "" {
		volumes = VolumeOrphan
	}
	if volumes != VolumeOrphan && volumes != VolumeDelete {
		return fmt.Errorf("volumes must be %q or %q", VolumeOrphan, VolumeDelete)
	}

	settings, err := e.store.GetNodeSettings(nodeID)
	if err != nil {
		return err
	}
	paths := managedVolumePaths(settings)

	// Stop local runtime before clearing mounts / deleting volumes.
	if err := e.stopNodeContainers(ctx, nodeID); err != nil {
		return fmt.Errorf("stop local service: %w", err)
	}

	if volumes == VolumeDelete && len(paths) > 0 {
		for _, p := range paths {
			name, err := e.resolveVolumeNameForPath(node, settings, p)
			if err != nil {
				continue
			}
			if err := e.DeleteManagedVolume(ctx, name, true); err != nil {
				// Best-effort: volume may not exist yet if never deployed.
				if !strings.Contains(err.Error(), "no Draft-managed volume") {
					return fmt.Errorf("delete volume %s: %w", p, err)
				}
			}
		}
	}

	if err := e.SetServiceLink(nodeID, rootNodeID); err != nil {
		return err
	}
	_ = e.store.DiscardStagedNodeSettings(nodeID)
	_ = e.store.DiscardStagedEnvVars(nodeID)

	// Attach root into this environment when it's already running.
	if err := e.EnsureServiceLinkNetworks(ctx, rootNodeID); err != nil {
		e.emitBuildLog(nodeID, fmt.Sprintf("    Warning: could not attach shared network yet: %v", err))
	}
	e.emitStatus(nodeID, StatusEvent{Status: "stopped"})
	e.emitBuildLog(nodeID, fmt.Sprintf("==> Now sharing %s from another environment", root.Label))
	return nil
}

// ClearServiceLink removes the link, making the node a normal deployable root.
// volume_mounts is left for the caller (promote) to restore.
func (e *Engine) ClearServiceLink(nodeID string) error {
	return e.store.SetNodeSetting(nodeID, SettingServiceLink, "")
}

// shareWarningForNode returns a runtime-coupling warning for Share mode.
func shareWarningForNode(s *store.Store, node *store.CanvasNode, settings map[string]string) (kind, msg string) {
	name := strings.ToLower(node.Label)
	image := strings.ToLower(settings["image"])
	if node.TemplateID > 0 {
		if tpl, err := s.GetTemplate(node.TemplateID); err == nil && tpl != nil {
			name = strings.ToLower(tpl.Name + " " + name)
			image = strings.ToLower(tpl.Image + " " + image)
		}
	}
	blob := name + " " + image
	switch {
	case strings.Contains(blob, "rabbit"):
		return "rabbitmq", "Queues and consumers are shared. Jobs from one environment may be handled by another, and message traffic will mix."
	case strings.Contains(blob, "redis"):
		return "redis", "Keys and pub/sub channels are shared across environments; expect collisions unless you namespace carefully."
	case strings.Contains(blob, "minio") || strings.Contains(blob, "s3"):
		return "minio", "Object keys live in one store; environments can overwrite each other's objects unless you use separate buckets or prefixes."
	case strings.Contains(blob, "postgres") || strings.Contains(blob, "mysql") || strings.Contains(blob, "mongo") || strings.Contains(blob, "clickhouse"):
		return "database", "All linked environments will share this database. Schema and data changes affect every linked environment."
	case strings.Contains(blob, "meilisearch"):
		return "search", "Search indexes are shared; reindexing or deletes from one environment affect the others."
	default:
		return "generic", "Linked environments share this running service and its runtime state."
	}
}

// managedVolumePaths returns container paths for type=volume mounts.
func managedVolumePaths(settings map[string]string) []string {
	var paths []string
	for _, spec := range ParseVolumeSpecs(settings["volume_mounts"]) {
		t := spec.Type
		if t == "" {
			t = VolumeTypeBind
		}
		if t != VolumeTypeVolume {
			continue
		}
		if p := strings.TrimSpace(spec.ContainerPath); p != "" {
			paths = append(paths, p)
		}
	}
	return paths
}

// PreviewEnvironmentDuplicate lists every non-alias root in the source
// environment for the duplicate wizard (fresh / share; clone when volumes exist).
func (e *Engine) PreviewEnvironmentDuplicate(sourceEnvironmentID uint) ([]StatefulServiceSummary, error) {
	if _, err := e.store.GetEnvironment(sourceEnvironmentID); err != nil {
		return nil, fmt.Errorf("source environment not found: %w", err)
	}
	nodes, err := e.store.ListNodesByEnvironment(sourceEnvironmentID)
	if err != nil {
		return nil, err
	}
	var out []StatefulServiceSummary
	for _, n := range nodes {
		settings, err := e.store.GetNodeSettings(n.ID)
		if err != nil {
			return nil, err
		}
		// Skip existing aliases in the source — they are not roots.
		if ParseServiceLink(settings[SettingServiceLink]) != nil {
			continue
		}
		paths := managedVolumePaths(settings)
		kind, msg := shareWarningForNode(e.store, &n, settings)
		out = append(out, StatefulServiceSummary{
			NodeID:      n.ID,
			Label:       n.Label,
			TemplateID:  n.TemplateID,
			Volumes:     paths,
			WarningKind: kind,
			Warning:     msg,
		})
	}
	return out, nil
}

// ListShareableRoots returns root services in projectID that can be share/clone sources.
// excludeEnvironmentID, when non-zero, omits that environment's nodes (typical: current env).
// Includes services without volumes (for late share); HasVolumes flags clone-capable roots.
func (e *Engine) ListShareableRoots(projectID uint, excludeEnvironmentID uint) ([]RootServiceSummary, error) {
	nodes, err := e.store.ListNodes(projectID)
	if err != nil {
		return nil, err
	}
	envs, err := e.store.ListEnvironments(projectID)
	if err != nil {
		return nil, err
	}
	envByID := map[uint]store.Environment{}
	for _, env := range envs {
		envByID[env.ID] = env
	}
	var out []RootServiceSummary
	for _, n := range nodes {
		if excludeEnvironmentID != 0 && n.EnvironmentID == excludeEnvironmentID {
			continue
		}
		settings, err := e.store.GetNodeSettings(n.ID)
		if err != nil {
			return nil, err
		}
		if ParseServiceLink(settings[SettingServiceLink]) != nil {
			continue
		}
		env := envByID[n.EnvironmentID]
		hasVolumes := len(managedVolumePaths(settings)) > 0
		kind, msg := shareWarningForNode(e.store, &n, settings)
		out = append(out, RootServiceSummary{
			NodeID:        n.ID,
			Label:         n.Label,
			EnvironmentID: n.EnvironmentID,
			EnvName:       env.Name,
			EnvSlug:       env.Slug,
			TemplateID:    n.TemplateID,
			HasVolumes:    hasVolumes,
			WarningKind:   kind,
			Warning:       msg,
		})
	}
	return out, nil
}

// scoreShareMatch ranks how likely candidate is the "same service" as source.
// Higher is better. 0 means no usable match signal.
func scoreShareMatch(source *store.CanvasNode, sourceSettings map[string]string, candidate *store.CanvasNode, candidateSettings map[string]string) (score int, reason string) {
	srcLabel := strings.TrimSpace(source.Label)
	candLabel := strings.TrimSpace(candidate.Label)
	labelMatch := srcLabel != "" && strings.EqualFold(srcLabel, candLabel)
	templateMatch := source.TemplateID > 0 && source.TemplateID == candidate.TemplateID

	if labelMatch && templateMatch {
		return 100, "label+template"
	}
	if templateMatch {
		return 70, "template"
	}
	if labelMatch {
		return 60, "label"
	}

	srcImage := strings.TrimSpace(sourceSettings["image"])
	candImage := strings.TrimSpace(candidateSettings["image"])
	if srcImage != "" && strings.EqualFold(srcImage, candImage) {
		srcPort := strings.TrimSpace(sourceSettings["service_port"])
		candPort := strings.TrimSpace(candidateSettings["service_port"])
		if srcPort != "" && srcPort == candPort {
			return 55, "image+port"
		}
		return 40, "image"
	}
	return 0, ""
}

// pickMatchedRoot chooses a unique best match in one environment.
// Ambiguous top scores (two services equally likely) yield no match.
func pickMatchedRoot(scored []struct {
	root  RootServiceSummary
	score int
}) *RootServiceSummary {
	if len(scored) == 0 {
		return nil
	}
	best := scored[0]
	for _, row := range scored[1:] {
		if row.score > best.score {
			best = row
		}
	}
	// Require a meaningful signal — image-only (40) is too weak for the
	// default "same service" path; image+port and above are accepted.
	if best.score < 55 {
		return nil
	}
	ties := 0
	for _, row := range scored {
		if row.score == best.score {
			ties++
		}
	}
	if ties > 1 {
		return nil
	}
	// Template-only / label-only must be unique at that score band already
	// covered by ties check. Copy to avoid retaining loop variable.
	matched := best.root
	return &matched
}

// ListShareTargets lists other environments' roots for converting nodeID into a
// shared alias. MatchedRoot is the best "same service" counterpart when unique.
func (e *Engine) ListShareTargets(nodeID string) ([]ShareTargetEnvironment, error) {
	source, err := e.store.GetNode(nodeID)
	if err != nil {
		return nil, fmt.Errorf("service not found: %w", err)
	}
	if link, err := e.GetServiceLink(nodeID); err != nil {
		return nil, err
	} else if link != nil {
		return nil, fmt.Errorf("service is already linked")
	}
	sourceSettings, err := e.store.GetNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}

	nodes, err := e.store.ListNodes(source.ProjectID)
	if err != nil {
		return nil, err
	}
	envs, err := e.store.ListEnvironments(source.ProjectID)
	if err != nil {
		return nil, err
	}
	envByID := map[uint]store.Environment{}
	for _, env := range envs {
		envByID[env.ID] = env
	}

	type cand struct {
		root     RootServiceSummary
		score    int
		settings map[string]string
		node     store.CanvasNode
	}
	byEnv := map[uint][]cand{}
	for _, n := range nodes {
		if n.EnvironmentID == source.EnvironmentID {
			continue
		}
		settings, err := e.store.GetNodeSettings(n.ID)
		if err != nil {
			return nil, err
		}
		if ParseServiceLink(settings[SettingServiceLink]) != nil {
			continue
		}
		env := envByID[n.EnvironmentID]
		kind, msg := shareWarningForNode(e.store, &n, settings)
		score, reason := scoreShareMatch(source, sourceSettings, &n, settings)
		summary := RootServiceSummary{
			NodeID:        n.ID,
			Label:         n.Label,
			EnvironmentID: n.EnvironmentID,
			EnvName:       env.Name,
			EnvSlug:       env.Slug,
			TemplateID:    n.TemplateID,
			HasVolumes:    len(managedVolumePaths(settings)) > 0,
			WarningKind:   kind,
			Warning:       msg,
			MatchReason:   reason,
		}
		byEnv[n.EnvironmentID] = append(byEnv[n.EnvironmentID], cand{
			root: summary, score: score, settings: settings, node: n,
		})
	}

	out := make([]ShareTargetEnvironment, 0, len(byEnv))
	for envID, cands := range byEnv {
		if len(cands) == 0 {
			continue
		}
		env := envByID[envID]
		roots := make([]RootServiceSummary, 0, len(cands))
		scored := make([]struct {
			root  RootServiceSummary
			score int
		}, 0, len(cands))
		for _, c := range cands {
			roots = append(roots, c.root)
			if c.score > 0 {
				scored = append(scored, struct {
					root  RootServiceSummary
					score int
				}{root: c.root, score: c.score})
			}
		}
		// Stable-ish order for UI.
		sort.Slice(roots, func(i, j int) bool {
			return strings.ToLower(roots[i].Label) < strings.ToLower(roots[j].Label)
		})
		target := ShareTargetEnvironment{
			EnvironmentID: envID,
			EnvName:       env.Name,
			EnvSlug:       env.Slug,
			Roots:         roots,
			MatchedRoot:   pickMatchedRoot(scored),
		}
		out = append(out, target)
	}
	sort.Slice(out, func(i, j int) bool {
		return strings.ToLower(out[i].EnvName) < strings.ToLower(out[j].EnvName)
	})
	return out, nil
}

// ReconcileServiceLinkNetworks re-applies multi-network attachments for every
// root that has at least one linker. Safe to call on daemon startup after
// Docker reconcile so shared services remain reachable after a restart.
func (e *Engine) ReconcileServiceLinkNetworks(ctx context.Context) error {
	// Collect distinct root IDs from service_link settings across all projects.
	var settings []store.NodeSetting
	if err := e.store.DB.Where("key = ? AND value <> ''", SettingServiceLink).Find(&settings).Error; err != nil {
		return err
	}
	seen := map[string]bool{}
	var firstErr error
	for _, row := range settings {
		link := ParseServiceLink(row.Value)
		if link == nil || seen[link.RootNodeID] {
			continue
		}
		seen[link.RootNodeID] = true
		if err := e.EnsureServiceLinkNetworks(ctx, link.RootNodeID); err != nil && firstErr == nil {
			firstErr = err
		}
	}
	return firstErr
}

// EnsureServiceLinkNetworks multi-attaches the root container onto every linker
// environment network with aliases matching each alias node's identity.
func (e *Engine) EnsureServiceLinkNetworks(ctx context.Context, rootNodeID string) error {
	root, err := e.store.GetNode(rootNodeID)
	if err != nil {
		return err
	}
	linkers, err := e.ListLinkers(rootNodeID)
	if err != nil {
		return err
	}
	if len(linkers) == 0 {
		return nil
	}
	dep, err := e.store.ActiveDeployment(rootNodeID)
	if err != nil {
		return err
	}
	if dep == nil || dep.ContainerID == "" {
		return nil // root not running yet; attach on next deploy
	}
	project, err := e.store.GetProject(root.ProjectID)
	if err != nil {
		return err
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return fmt.Errorf("connect to docker: %w", err)
	}
	defer cli.Close()

	for _, alias := range linkers {
		if err := e.attachRootToAliasNetwork(ctx, cli, dep.ContainerID, project, &alias); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) attachRootToAliasNetwork(
	ctx context.Context,
	cli *client.Client,
	rootContainerID string,
	project *store.Project,
	alias *store.CanvasNode,
) error {
	env, err := e.store.GetEnvironment(alias.EnvironmentID)
	if err != nil {
		return err
	}
	sand, err := e.isSandboxEnvironment(env.ID)
	if err != nil {
		return err
	}
	dockerEnv := networking.DockerEnvironment(env.Slug, sand)
	netName := draftNetworkName(project.ID, project.Name, dockerEnv)
	if err := ensureDraftNetwork(ctx, cli, netName, project.ID, project.Name, dockerEnv); err != nil {
		return err
	}
	addr, err := e.computeNodeAddress(alias)
	if err != nil {
		return err
	}
	aliases := internalNetworkAliases(addr.ServiceName, addr.InternalHostname)

	// Disconnect first so alias list can be refreshed on redeploy.
	_ = cli.NetworkDisconnect(ctx, netName, rootContainerID, true)

	err = cli.NetworkConnect(ctx, netName, rootContainerID, &network.EndpointSettings{
		Aliases: aliases,
	})
	if err != nil {
		// Already connected with same config is fine in some daemon versions.
		if !strings.Contains(err.Error(), "already exists") {
			return fmt.Errorf("attach root to network %s: %w", netName, err)
		}
	}
	return nil
}

// DisconnectServiceLinkNetwork removes the root container from the alias env network.
func (e *Engine) DisconnectServiceLinkNetwork(ctx context.Context, aliasNodeID string) error {
	link, err := e.GetServiceLink(aliasNodeID)
	if err != nil || link == nil {
		return err
	}
	alias, err := e.store.GetNode(aliasNodeID)
	if err != nil {
		return err
	}
	rootDep, err := e.store.ActiveDeployment(link.RootNodeID)
	if err != nil || rootDep == nil || rootDep.ContainerID == "" {
		return err
	}
	project, err := e.store.GetProject(alias.ProjectID)
	if err != nil {
		return err
	}
	env, err := e.store.GetEnvironment(alias.EnvironmentID)
	if err != nil {
		return err
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()
	sand, err := e.isSandboxEnvironment(env.ID)
	if err != nil {
		return err
	}
	dockerEnv := networking.DockerEnvironment(env.Slug, sand)
	netName := draftNetworkName(project.ID, project.Name, dockerEnv)
	if err := cli.NetworkDisconnect(ctx, netName, rootDep.ContainerID, true); err != nil && !errdefs.IsNotFound(err) {
		// Container may already be gone.
		if !strings.Contains(err.Error(), "is not connected") {
			return err
		}
	}
	return nil
}

// ensureLinkedDeploy "deploys" an alias by ensuring the root is multi-attached.
func (e *Engine) ensureLinkedDeploy(ctx context.Context, nodeID string, link *ServiceLink) {
	e.emitBuildLog(nodeID, "==> Linked service — no local container")
	root, err := e.store.GetNode(link.RootNodeID)
	if err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "linked root not found: " + err.Error()})
		return
	}
	e.emitBuildLog(nodeID, fmt.Sprintf("    Root: %s (node %s)", root.Label, root.ID))
	if err := e.EnsureServiceLinkNetworks(ctx, link.RootNodeID); err != nil {
		e.emitStatus(nodeID, StatusEvent{Status: "failed", Error: "network attach failed: " + err.Error()})
		return
	}
	// Mirror root status for the alias UI.
	dep, _ := e.store.ActiveDeployment(link.RootNodeID)
	status := "stopped"
	if dep != nil {
		status = dep.Status
	}
	e.emitStatus(nodeID, StatusEvent{Status: status})
	e.emitBuildLog(nodeID, fmt.Sprintf("    Root status: %s", status))
}

// PromoteLinkedService turns an alias into a real deployable service.
// seed: "empty" | "clone". consistency applies when seed is clone.
func (e *Engine) PromoteLinkedService(ctx context.Context, aliasNodeID, seed string, consistency CloneConsistency) error {
	link, err := e.GetServiceLink(aliasNodeID)
	if err != nil {
		return err
	}
	if link == nil {
		return fmt.Errorf("service is not linked")
	}
	alias, err := e.store.GetNode(aliasNodeID)
	if err != nil {
		return err
	}
	root, err := e.store.GetNode(link.RootNodeID)
	if err != nil {
		return fmt.Errorf("root service not found: %w", err)
	}
	rootSettings, err := e.store.GetNodeSettings(root.ID)
	if err != nil {
		return err
	}

	// Disconnect from root network before becoming independent.
	_ = e.DisconnectServiceLinkNetwork(ctx, aliasNodeID)

	if err := e.ClearServiceLink(aliasNodeID); err != nil {
		return err
	}

	// Restore volume mounts from root paths as auto-named local volumes.
	paths := managedVolumePaths(rootSettings)
	if len(paths) == 0 {
		// Fall back to whatever root had in volume_mounts raw parse including binds? only volumes.
		_ = e.store.SetNodeSetting(aliasNodeID, "volume_mounts", "[]")
	} else {
		specs := make([]VolumeSpec, 0, len(paths))
		for _, p := range paths {
			specs = append(specs, VolumeSpec{Type: VolumeTypeVolume, ContainerPath: p})
		}
		raw, err := json.Marshal(specs)
		if err != nil {
			return err
		}
		if err := e.store.SetNodeSetting(aliasNodeID, "volume_mounts", string(raw)); err != nil {
			return err
		}
	}

	if err := e.restampTemplateOwnedGeneratedValues(aliasNodeID, root.ID); err != nil {
		return err
	}

	if seed == "clone" && len(paths) > 0 {
		if consistency == "" {
			consistency = CloneConsistent
		}
		for _, p := range paths {
			if _, err := e.CloneVolumeData(ctx, aliasNodeID, root.ID, p, consistency); err != nil {
				return fmt.Errorf("clone volume %s: %w", p, err)
			}
		}
	}

	_ = alias // silence if unused in future
	return nil
}

// UnlinkService removes a link. become "fresh" keeps the node as empty root;
// "delete" removes the alias node entirely.
func (e *Engine) UnlinkService(ctx context.Context, aliasNodeID, become string) error {
	link, err := e.GetServiceLink(aliasNodeID)
	if err != nil {
		return err
	}
	if link == nil {
		return fmt.Errorf("service is not linked")
	}
	_ = e.DisconnectServiceLinkNetwork(ctx, aliasNodeID)
	if become == "delete" {
		return e.DeleteService(ctx, aliasNodeID)
	}
	// fresh: clear link, empty volumes
	if err := e.ClearServiceLink(aliasNodeID); err != nil {
		return err
	}
	if err := e.store.SetNodeSetting(aliasNodeID, "volume_mounts", "[]"); err != nil {
		return err
	}
	return e.restampTemplateOwnedGeneratedValues(aliasNodeID, link.RootNodeID)
}

// errRootHasLinkers is returned when deleting a root that still has aliases.
type errRootHasLinkers struct {
	RootLabel string
	Linkers   []string
}

func (e errRootHasLinkers) Error() string {
	return fmt.Sprintf("cannot delete %q: linked from %s — unlink or promote those services first",
		e.RootLabel, strings.Join(e.Linkers, ", "))
}

// guardRootDelete returns an error if nodeID is a root with active linkers.
func (e *Engine) guardRootDelete(nodeID string) error {
	linkers, err := e.ListLinkers(nodeID)
	if err != nil {
		return err
	}
	if len(linkers) == 0 {
		return nil
	}
	node, _ := e.store.GetNode(nodeID)
	label := nodeID
	if node != nil {
		label = node.Label
	}
	names := make([]string, 0, len(linkers))
	for _, l := range linkers {
		envName := ""
		if env, err := e.store.GetEnvironment(l.EnvironmentID); err == nil {
			envName = env.Name + "/"
		}
		names = append(names, envName+l.Label)
	}
	return errRootHasLinkers{RootLabel: label, Linkers: names}
}

// stopNodeContainers stops and removes containers for a node without full Stop
// image cleanup — used before volume clone.
func (e *Engine) stopNodeContainers(ctx context.Context, nodeID string) error {
	e.mu.Lock()
	if cancel, ok := e.active[nodeID]; ok {
		cancel()
		delete(e.active, nodeID)
	}
	e.mu.Unlock()

	dep, err := e.store.ActiveDeployment(nodeID)
	if err != nil {
		return err
	}
	if dep == nil || dep.ContainerID == "" {
		return nil
	}
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return err
	}
	defer cli.Close()
	settings, _ := e.store.GetNodeSettings(nodeID)
	timeout := stopTimeoutForSettings(settings)
	now := time.Now()
	markDeploymentStopped(dep, now)
	if err := e.store.UpdateDeployment(dep); err != nil {
		return err
	}
	_ = cli.ContainerStop(ctx, dep.ContainerID, container.StopOptions{Timeout: &timeout})
	if err := removeContainerAndWait(ctx, cli, dep.ContainerID); err != nil {
		return err
	}
	if dep.Hostname != "" && e.router != nil {
		_ = e.router.Unregister(dep.Hostname)
	}
	e.emitStatus(nodeID, StatusEvent{DeploymentID: dep.ID, Status: "stopped"})
	return nil
}
