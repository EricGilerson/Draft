package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"Draft/internal/gitsrc"
	"Draft/internal/store"
)

// SandboxServiceMode controls whether a source service is independently
// copied, bridged to the source environment, or absent from a sandbox.
type SandboxServiceMode string

const (
	SandboxServiceCopy  SandboxServiceMode = "copy"
	SandboxServiceShare SandboxServiceMode = "share"
	SandboxServiceOmit  SandboxServiceMode = "omit"
)

// SandboxServiceRule is keyed by the stable source node ID, never a label.
// DataMode applies to copied stateful services; clone is the isolated default.
type SandboxServiceRule struct {
	SourceNodeID string             `json:"sourceNodeId"`
	Mode         SandboxServiceMode `json:"mode"`
	DataMode     ServiceDataMode    `json:"dataMode,omitempty"`
	Consistency  CloneConsistency   `json:"consistency,omitempty"`
}

// SandboxRepositoryRef makes branch/ref selection explicitly per repository.
// Ref is resolved to CommitSHA before creation, and the resolved value is what
// is applied to copied Git-backed services.
type SandboxRepositoryRef struct {
	RepoRoot string `json:"repoRoot"`
	Ref      string `json:"ref"`
}

// SandboxPlan is both the profile payload and the immutable resolved snapshot
// stored on Sandbox.PlanJSON. It intentionally separates service/data policy
// from repository sources so multi-repo projects never need one global branch.
type SandboxPlan struct {
	TTLHours         int                    `json:"ttlHours,omitempty"`
	WarningHours     int                    `json:"warningHours,omitempty"`
	GraceHours       int                    `json:"graceHours,omitempty"`
	SuspendIdleHours int                    `json:"suspendIdleHours,omitempty"`
	Services         []SandboxServiceRule   `json:"services,omitempty"`
	Repositories     []SandboxRepositoryRef `json:"repositories,omitempty"`
}

type SandboxCreateRequest struct {
	Name                string              `json:"name"`
	SourceEnvironmentID uint                `json:"sourceEnvironmentId"`
	ProfileID           uint                `json:"profileId,omitempty"`
	Plan                SandboxPlan         `json:"plan"`
	Links               []store.SandboxLink `json:"links,omitempty"`
}

type SandboxPreview struct {
	ProjectID           uint                            `json:"projectId"`
	SourceEnvironmentID uint                            `json:"sourceEnvironmentId"`
	ProfileID           uint                            `json:"profileId,omitempty"`
	Plan                SandboxPlan                     `json:"plan"`
	Repositories        []store.SandboxRepositorySource `json:"repositories"`
	Services            []SandboxServiceRule            `json:"services"`
	ExpiresAt           time.Time                       `json:"expiresAt"`
	WarnAt              time.Time                       `json:"warnAt"`
	GraceEndsAt         time.Time                       `json:"graceEndsAt"`
}

// SandboxDetail is the inspectable record shown after creation. It exposes the
// immutable resolved plan and manual context rather than re-evaluating current
// profiles or branch names against a running sandbox.
type SandboxDetail struct {
	Sandbox      store.Sandbox                   `json:"sandbox"`
	Source       store.Environment               `json:"source"`
	Links        []store.SandboxLink             `json:"links"`
	Repositories []store.SandboxRepositorySource `json:"repositories"`
	Plan         SandboxPlan                     `json:"plan"`
}

func (e *Engine) GetSandboxDetail(sandboxID uint) (*SandboxDetail, error) {
	sandbox, err := e.store.GetSandbox(sandboxID)
	if err != nil {
		return nil, err
	}
	source, err := e.store.GetEnvironment(sandbox.SourceEnvironmentID)
	if err != nil {
		return nil, err
	}
	links, err := e.store.ListSandboxLinks(sandboxID)
	if err != nil {
		return nil, err
	}
	repositories, err := e.store.ListSandboxRepositorySources(sandboxID)
	if err != nil {
		return nil, err
	}
	var plan SandboxPlan
	if err := json.Unmarshal([]byte(sandbox.PlanJSON), &plan); err != nil {
		return nil, fmt.Errorf("read sandbox plan: %w", err)
	}
	return &SandboxDetail{Sandbox: *sandbox, Source: *source, Links: links, Repositories: repositories, Plan: plan}, nil
}

// PreviewSandbox resolves project/source-environment defaults into the exact
// immutable plan that CreateSandbox will persist. It does not touch Docker.
func (e *Engine) PreviewSandbox(ctx context.Context, req SandboxCreateRequest) (*SandboxPreview, error) {
	if strings.TrimSpace(req.Name) == "" {
		return nil, fmt.Errorf("sandbox name is required")
	}
	source, err := e.store.GetEnvironment(req.SourceEnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("source environment not found: %w", err)
	}
	plan, profileID, err := e.resolveSandboxPlan(req, source)
	if err != nil {
		return nil, err
	}
	nodes, err := e.store.ListNodesByEnvironment(source.ID)
	if err != nil {
		return nil, err
	}
	plan.Services = resolveSandboxServiceRules(nodes, plan.Services, e.store)
	repos, err := e.resolveSandboxRepositories(ctx, source.ProjectID, nodes, plan.Repositories)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	expires, warn, grace := sandboxTimes(now, plan)
	return &SandboxPreview{ProjectID: source.ProjectID, SourceEnvironmentID: source.ID, ProfileID: profileID, Plan: plan, Repositories: repos, Services: plan.Services, ExpiresAt: expires, WarnAt: warn, GraceEndsAt: grace}, nil
}

// CreateSandbox materializes a resolved plan through the existing environment
// duplication path. This means isolated copies use Draft's normal network,
// UID, hostname, volume clone, and shared-service network bridge mechanics.
func (e *Engine) CreateSandbox(ctx context.Context, req SandboxCreateRequest) (*store.Sandbox, error) {
	preview, err := e.PreviewSandbox(ctx, req)
	if err != nil {
		return nil, err
	}
	choices := make([]ServiceDataChoice, 0, len(preview.Services))
	omit := map[string]bool{}
	for _, rule := range preview.Services {
		switch rule.Mode {
		case SandboxServiceOmit:
			omit[rule.SourceNodeID] = true
		case SandboxServiceShare:
			choices = append(choices, ServiceDataChoice{SourceNodeID: rule.SourceNodeID, Mode: ServiceDataShare})
		default:
			mode := rule.DataMode
			if mode == "" {
				mode = ServiceDataClone
			}
			choices = append(choices, ServiceDataChoice{SourceNodeID: rule.SourceNodeID, Mode: mode, Consistency: rule.Consistency})
		}
	}
	env, err := e.DuplicateEnvironmentWithChoices(ctx, preview.SourceEnvironmentID, req.Name, choices)
	if err != nil {
		return nil, err
	}
	cleanup := func() { _ = e.store.DeleteEnvironment(env.ID) }

	// Source labels are unique per environment, so they safely identify the
	// duplicate for branch pinning and omit handling.
	sourceNodes, _ := e.store.ListNodesByEnvironment(preview.SourceEnvironmentID)
	targetNodes, err := e.store.ListNodesByEnvironment(env.ID)
	if err != nil {
		cleanup()
		return nil, err
	}
	targetByLabel := make(map[string]store.CanvasNode, len(targetNodes))
	for _, n := range targetNodes {
		targetByLabel[n.Label] = n
	}
	for _, sourceNode := range sourceNodes {
		target, ok := targetByLabel[sourceNode.Label]
		if !ok {
			cleanup()
			return nil, fmt.Errorf("sandbox duplicate missing service %q", sourceNode.Label)
		}
		if omit[sourceNode.ID] {
			if err := DeleteServiceFromStore(e.store, target.ID); err != nil {
				cleanup()
				return nil, err
			}
			continue
		}
		if err := e.pinSandboxNodeToRepository(ctx, target.ID, sourceNode.ID, preview.Repositories); err != nil {
			cleanup()
			return nil, err
		}
	}
	planJSON, err := json.Marshal(preview.Plan)
	if err != nil {
		cleanup()
		return nil, err
	}
	sandbox, err := e.store.CreateSandbox(&store.Sandbox{ProjectID: preview.ProjectID, EnvironmentID: env.ID, SourceEnvironmentID: preview.SourceEnvironmentID, ProfileID: preview.ProfileID, Name: strings.TrimSpace(req.Name), Status: "active", PlanJSON: string(planJSON), ExpiresAt: preview.ExpiresAt, WarnAt: preview.WarnAt, GraceEndsAt: preview.GraceEndsAt}, req.Links, preview.Repositories)
	if err != nil {
		cleanup()
		return nil, err
	}
	return sandbox, nil
}

// ExtendSandbox is deliberately an explicit lifecycle action. It restores an
// expired/warning sandbox to active and recalculates warning/grace windows from
// the immutable creation plan, rather than from any profile that has changed
// since the sandbox was created.
func (e *Engine) ExtendSandbox(sandboxID uint, ttlHours int) (*store.Sandbox, error) {
	if ttlHours <= 0 {
		return nil, fmt.Errorf("sandbox extension must be greater than zero hours")
	}
	sandbox, err := e.store.GetSandbox(sandboxID)
	if err != nil {
		return nil, err
	}
	var plan SandboxPlan
	if err := json.Unmarshal([]byte(sandbox.PlanJSON), &plan); err != nil {
		return nil, fmt.Errorf("read sandbox plan: %w", err)
	}
	plan.TTLHours = ttlHours
	if plan.WarningHours == 0 {
		plan.WarningHours = 24
	}
	if plan.GraceHours == 0 {
		plan.GraceHours = 72
	}
	expires, warn, grace := sandboxTimes(time.Now().UTC(), plan)
	if err := e.store.ExtendSandbox(sandboxID, expires, warn, grace); err != nil {
		return nil, err
	}
	return e.store.GetSandbox(sandboxID)
}

// SuspendSandbox stops every sandbox-owned service but preserves its plan and
// volumes. ResumeSandbox starts the same environment again; no source or data
// plan is re-evaluated during either operation.
func (e *Engine) SuspendSandbox(ctx context.Context, sandboxID uint) (*store.Sandbox, error) {
	sandbox, err := e.store.GetSandbox(sandboxID)
	if err != nil {
		return nil, err
	}
	if sandbox.Status == "suspended" {
		return sandbox, nil
	}
	if _, err := e.RunEnvironmentStack(ctx, sandbox.EnvironmentID, StackStop); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	if err := e.store.UpdateSandboxStatus(sandboxID, "suspended", &now); err != nil {
		return nil, err
	}
	return e.store.GetSandbox(sandboxID)
}

func (e *Engine) ResumeSandbox(ctx context.Context, sandboxID uint) (*store.Sandbox, error) {
	sandbox, err := e.store.GetSandbox(sandboxID)
	if err != nil {
		return nil, err
	}
	if sandbox.Status != "suspended" {
		return sandbox, nil
	}
	if _, err := e.RunEnvironmentStack(ctx, sandbox.EnvironmentID, StackStart); err != nil {
		return nil, err
	}
	if err := e.store.UpdateSandboxStatus(sandboxID, "active", nil); err != nil {
		return nil, err
	}
	return e.store.GetSandbox(sandboxID)
}

// DeleteSandbox is deliberately more destructive than DeleteEnvironment: a
// sandbox is disposable, so its Draft-managed volumes are removed alongside
// containers, images, routes, and its Docker network. Explicit user-managed
// volumes and bind mounts are never selected by this cleanup path.
func (e *Engine) DeleteSandbox(ctx context.Context, sandboxID uint) error {
	sandbox, err := e.store.GetSandbox(sandboxID)
	if err != nil {
		return err
	}
	nodes, err := e.store.ListNodesByEnvironment(sandbox.EnvironmentID)
	if err != nil {
		return err
	}
	for _, node := range nodes {
		if err := e.guardRootDelete(node.ID); err != nil {
			return err
		}
	}
	volumesByNode := map[string][]ManagedVolume{}
	for _, node := range nodes {
		volumes, err := e.ListManagedVolumes(ctx, &sandbox.ProjectID, node.ID)
		if err != nil {
			_ = e.store.UpdateSandboxStatus(sandboxID, "cleanup_failed", nil)
			return err
		}
		volumesByNode[node.ID] = volumes
	}
	for _, node := range nodes {
		if err := e.DeleteService(ctx, node.ID); err != nil {
			_ = e.store.UpdateSandboxStatus(sandboxID, "cleanup_failed", nil)
			return err
		}
		for _, volume := range volumesByNode[node.ID] {
			if err := e.DeleteManagedVolume(ctx, volume.Name, true); err != nil {
				_ = e.store.UpdateSandboxStatus(sandboxID, "cleanup_failed", nil)
				return fmt.Errorf("remove sandbox volume %q: %w", volume.Name, err)
			}
		}
	}
	project, err := e.store.GetProject(sandbox.ProjectID)
	if err != nil {
		return err
	}
	env, err := e.store.GetEnvironment(sandbox.EnvironmentID)
	if err != nil {
		return err
	}
	if err := e.RemoveNetwork(ctx, draftNetworkName(project.ID, project.Name, env.Slug)); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
		_ = e.store.UpdateSandboxStatus(sandboxID, "cleanup_failed", nil)
		return err
	}
	return e.store.DeleteEnvironment(sandbox.EnvironmentID)
}

func (e *Engine) resolveSandboxPlan(req SandboxCreateRequest, source *store.Environment) (SandboxPlan, uint, error) {
	settings, err := e.store.GetSandboxProjectSettings(source.ProjectID)
	if err != nil {
		return SandboxPlan{}, 0, err
	}
	var plan SandboxPlan
	var profileID uint
	if req.ProfileID != 0 {
		profile, err := e.store.GetSandboxProfile(req.ProfileID)
		if err != nil {
			return plan, 0, err
		}
		if profile.ProjectID != source.ProjectID || (profile.SourceEnvironmentID != 0 && profile.SourceEnvironmentID != source.ID) {
			return plan, 0, fmt.Errorf("sandbox profile does not apply to source environment")
		}
		if err := json.Unmarshal([]byte(profile.PlanJSON), &plan); err != nil {
			return plan, 0, fmt.Errorf("read sandbox profile: %w", err)
		}
		profileID = profile.ID
	} else if profile, err := e.store.DefaultSandboxProfile(source.ProjectID, source.ID); err != nil {
		return plan, 0, err
	} else if profile != nil {
		if err := json.Unmarshal([]byte(profile.PlanJSON), &plan); err != nil {
			return plan, 0, fmt.Errorf("read sandbox profile: %w", err)
		}
		profileID = profile.ID
	}
	plan = mergeSandboxPlan(plan, req.Plan)
	if plan.TTLHours == 0 {
		plan.TTLHours = settings.DefaultTTLHours
	}
	if plan.WarningHours == 0 {
		plan.WarningHours = settings.WarningHours
	}
	if plan.GraceHours == 0 {
		plan.GraceHours = settings.GraceHours
	}
	if plan.SuspendIdleHours == 0 {
		plan.SuspendIdleHours = settings.SuspendIdleHours
	}
	if plan.TTLHours <= 0 || plan.WarningHours < 0 || plan.GraceHours < 0 {
		return plan, 0, fmt.Errorf("invalid sandbox lifecycle settings")
	}
	return plan, profileID, nil
}

func mergeSandboxPlan(base, override SandboxPlan) SandboxPlan {
	if override.TTLHours != 0 {
		base.TTLHours = override.TTLHours
	}
	if override.WarningHours != 0 {
		base.WarningHours = override.WarningHours
	}
	if override.GraceHours != 0 {
		base.GraceHours = override.GraceHours
	}
	if override.SuspendIdleHours != 0 {
		base.SuspendIdleHours = override.SuspendIdleHours
	}
	if override.Services != nil {
		base.Services = override.Services
	}
	if override.Repositories != nil {
		base.Repositories = override.Repositories
	}
	return base
}

func resolveSandboxServiceRules(nodes []store.CanvasNode, supplied []SandboxServiceRule, s *store.Store) []SandboxServiceRule {
	byNode := map[string]SandboxServiceRule{}
	for _, r := range supplied {
		byNode[r.SourceNodeID] = r
	}
	out := make([]SandboxServiceRule, 0, len(nodes))
	for _, node := range nodes {
		r, ok := byNode[node.ID]
		if !ok {
			r = SandboxServiceRule{SourceNodeID: node.ID, Mode: SandboxServiceCopy}
		}
		if r.Mode == "" {
			r.Mode = SandboxServiceCopy
		}
		if r.Mode == SandboxServiceCopy && r.DataMode == "" {
			settings, _ := s.GetNodeSettings(node.ID)
			if len(managedVolumePaths(settings)) > 0 {
				r.DataMode = ServiceDataClone
				r.Consistency = CloneConsistent
			} else {
				r.DataMode = ServiceDataFresh
			}
		}
		out = append(out, r)
	}
	return out
}

func (e *Engine) resolveSandboxRepositories(ctx context.Context, projectID uint, nodes []store.CanvasNode, overrides []SandboxRepositoryRef) ([]store.SandboxRepositorySource, error) {
	refs := map[string]string{}
	for _, r := range overrides {
		refs[filepath.Clean(strings.TrimSpace(r.RepoRoot))] = strings.TrimSpace(r.Ref)
	}
	seen := map[string]store.SandboxRepositorySource{}
	for _, node := range nodes {
		root, err := e.store.ResolveGitRepoRoot(ctx, node.ID, projectID)
		if err != nil || root == "" {
			continue
		}
		root = filepath.Clean(root)
		ref := refs[root]
		if ref == "" {
			settings, _ := e.store.GetNodeSettings(node.ID)
			ref = strings.TrimSpace(settings["git_branch"])
		}
		if ref == "" {
			ref = "HEAD"
		}
		sha, err := gitsrc.ResolveSHA(ctx, root, ref)
		if err != nil {
			return nil, fmt.Errorf("resolve sandbox source for %s: %w", root, err)
		}
		seen[root] = store.SandboxRepositorySource{RepoRoot: root, Ref: ref, CommitSHA: sha}
	}
	out := make([]store.SandboxRepositorySource, 0, len(seen))
	for _, source := range seen {
		out = append(out, source)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RepoRoot < out[j].RepoRoot })
	return out, nil
}

func (e *Engine) pinSandboxNodeToRepository(ctx context.Context, targetNodeID, sourceNodeID string, repositories []store.SandboxRepositorySource) error {
	target, err := e.store.GetNode(targetNodeID)
	if err != nil {
		return err
	}
	root, err := e.store.ResolveGitRepoRoot(ctx, sourceNodeID, target.ProjectID)
	if err != nil || root == "" {
		return nil
	}
	for _, repo := range repositories {
		if filepath.Clean(repo.RepoRoot) == filepath.Clean(root) {
			return e.store.SetNodeSetting(targetNodeID, "git_branch", repo.CommitSHA)
		}
	}
	return nil
}

func sandboxTimes(now time.Time, plan SandboxPlan) (time.Time, time.Time, time.Time) {
	expires := now.Add(time.Duration(plan.TTLHours) * time.Hour)
	warn := expires.Add(-time.Duration(plan.WarningHours) * time.Hour)
	if warn.Before(now) {
		warn = now
	}
	return expires, warn, expires.Add(time.Duration(plan.GraceHours) * time.Hour)
}

// ReconcileSandboxLifecycle advances persisted lifecycle status. It is safe to
// call at daemon startup and periodically. Expired sandboxes are purged after
// their configured grace period, including Draft-managed volume data.
func (e *Engine) ReconcileSandboxLifecycle(ctx context.Context, now time.Time) error {
	projects, err := e.store.ListProjects()
	if err != nil {
		return err
	}
	for _, project := range projects {
		sandboxes, err := e.store.ListSandboxes(project.ID)
		if err != nil {
			return err
		}
		for _, sandbox := range sandboxes {
			if sandbox.Status == "active" && !sandbox.WarnAt.After(now) {
				if err := e.store.UpdateSandboxStatus(sandbox.ID, "warning", nil); err != nil {
					return err
				}
			}
			if (sandbox.Status == "active" || sandbox.Status == "warning") && !sandbox.ExpiresAt.After(now) {
				if err := e.store.UpdateSandboxStatus(sandbox.ID, "expired", nil); err != nil {
					return err
				}
			}
			if sandbox.Status == "expired" && !sandbox.GraceEndsAt.After(now) {
				if err := e.DeleteSandbox(ctx, sandbox.ID); err != nil {
					return err
				}
			}
		}
	}
	return nil
}
