package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"Draft/internal/gitsrc"
	"Draft/internal/networking"
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
// Ref is the human branch/PR head/tag stored on sandbox copies as git_branch so
// Settings stays readable and redeploys can follow tip. CommitSHA is resolved
// at create (and on refresh) for audit / "same SHA" rebuilds; it is not written
// to git_branch unless the pin is intentionally frozen.
// CommitSHA may also be supplied when the caller already knows the tip (for
// example a GitHub PR head from `gh`) and the local object store may not have
// fetched that commit yet.
//
// PRNumber, when set, lets Draft fetch refs/pull/<n>/head when the PR head
// branch name is not present locally (common for fork PRs).
type SandboxRepositoryRef struct {
	RepoRoot  string `json:"repoRoot"`
	Ref       string `json:"ref"`
	CommitSHA string `json:"commitSha,omitempty"`
	PRNumber  int    `json:"prNumber,omitempty"`
}

// SandboxPurpose distinguishes preview sandboxes (human-driven PR/feature
// copies) from testing sandboxes (recipe + commands, optimized for rerun).
type SandboxPurpose string

const (
	SandboxPurposePreview SandboxPurpose = "preview"
	SandboxPurposeTest    SandboxPurpose = "test"
)

// SandboxOnComplete controls what happens after a testing-sandbox step suite
// finishes. leave keeps the short-lived stack for inspection; delete tears it
// down immediately; suspend stops services but preserves volumes.
type SandboxOnComplete string

const (
	SandboxOnCompleteLeave   SandboxOnComplete = "leave"
	SandboxOnCompleteDelete  SandboxOnComplete = "delete"
	SandboxOnCompleteSuspend SandboxOnComplete = "suspend"
)

// SandboxStep is one command executed inside a running sandbox service.
// ServiceLabel is resolved against the sandbox environment (not source IDs)
// so recipes stay readable after node IDs change across copies.
type SandboxStep struct {
	Name              string   `json:"name,omitempty"`
	ServiceLabel      string   `json:"serviceLabel"`
	Cmd               []string `json:"cmd"`
	WorkDir           string   `json:"workDir,omitempty"`
	ExpectedExitCodes []int    `json:"expectedExitCodes,omitempty"`
	OutputContains    string   `json:"outputContains,omitempty"`
}

// SandboxPlan is both the profile payload and the immutable resolved snapshot
// stored on Sandbox.PlanJSON. It intentionally separates service/data policy
// from repository sources so multi-repo projects never need one global branch.
//
// Purpose/steps/onComplete apply to testing sandboxes. Preview sandboxes leave
// them empty and behave as before.
type SandboxPlan struct {
	TTLHours         int                    `json:"ttlHours,omitempty"`
	WarningHours     int                    `json:"warningHours,omitempty"`
	GraceHours       int                    `json:"graceHours,omitempty"`
	SuspendIdleHours int                    `json:"suspendIdleHours,omitempty"`
	Purpose          SandboxPurpose         `json:"purpose,omitempty"`
	Steps            []SandboxStep          `json:"steps,omitempty"`
	OnComplete       SandboxOnComplete      `json:"onComplete,omitempty"`
	Services         []SandboxServiceRule   `json:"services,omitempty"`
	Repositories     []SandboxRepositoryRef `json:"repositories,omitempty"`
}

type SandboxCreateRequest struct {
	Name                string              `json:"name"`
	SourceEnvironmentID uint                `json:"sourceEnvironmentId"`
	ProfileID           uint                `json:"profileId,omitempty"`
	Plan                SandboxPlan         `json:"plan"`
	Links               []store.SandboxLink `json:"links,omitempty"`
	// StartOnCreate deploys every service in the new sandbox after materialize.
	// Preview sandboxes default to true in the UI; testing runs start via their
	// own path. Create still returns when start fails — see SandboxCreateResult.
	StartOnCreate bool `json:"startOnCreate,omitempty"`
}

// SandboxCreateResult is returned by CreateSandbox so the UI can open the new
// environment and surface stack start outcomes without a second round-trip.
type SandboxCreateResult struct {
	Sandbox *store.Sandbox          `json:"sandbox"`
	Stack   *EnvironmentStackResult `json:"stack,omitempty"`
	Started bool                    `json:"started"`
	// StartError is set when materialize succeeded but stack start failed.
	StartError string `json:"startError,omitempty"`
}

// SandboxSourceRepo describes one git repository used by services in a source
// environment, for the create-sandbox source picker.
type SandboxSourceRepo struct {
	RepoRoot              string               `json:"repoRoot"`
	DefaultRef            string               `json:"defaultRef"`
	ServiceLabels         []string             `json:"serviceLabels"`
	NodeIDs               []string             `json:"nodeIds"`
	Branches              []string             `json:"branches,omitempty"`
	PullRequestsAvailable bool                 `json:"pullRequestsAvailable"`
	PullRequestsError     string               `json:"pullRequestsError,omitempty"`
	PullRequests          []gitsrc.PullRequest `json:"pullRequests,omitempty"`
}

// SandboxSourceRepos is the create-dialog payload for branch/PR selection.
type SandboxSourceRepos struct {
	ProjectID           uint                `json:"projectId"`
	SourceEnvironmentID uint                `json:"sourceEnvironmentId"`
	Repositories        []SandboxSourceRepo `json:"repositories"`
}

// SandboxRefreshMode controls how an existing sandbox's source pins are updated.
type SandboxRefreshMode string

const (
	// SandboxRefreshTip re-resolves each stored human ref (branch/PR head) to
	// the current tip, keeps git_branch on that ref, and redeploys.
	SandboxRefreshTip SandboxRefreshMode = "tip"
	// SandboxRefreshSame freezes git_branch to the recorded commit SHA and
	// redeploys that exact object (true rebuild-from-SHA for sandbox copies).
	// The human ref is still kept on the pin record for display / later tip refresh.
	SandboxRefreshSame SandboxRefreshMode = "same"
)

// SandboxRefreshRequest rebuilds sandbox service source pins and redeploys.
type SandboxRefreshRequest struct {
	SandboxID uint               `json:"sandboxId"`
	Mode      SandboxRefreshMode `json:"mode"`
}

// SandboxRefreshResult reports the new pins and stack redeploy outcome.
type SandboxRefreshResult struct {
	Sandbox      *store.Sandbox                  `json:"sandbox"`
	Repositories []store.SandboxRepositorySource `json:"repositories"`
	Stack        *EnvironmentStackResult         `json:"stack,omitempty"`
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
	LatestRun    *store.SandboxTestRun           `json:"latestRun,omitempty"`
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
	detail := &SandboxDetail{Sandbox: *sandbox, Source: *source, Links: links, Repositories: repositories, Plan: plan}
	if run, err := e.store.LatestSandboxTestRun(sandboxID); err == nil {
		detail.LatestRun = run
	}
	return detail, nil
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
	plan.Services = resolveSandboxServiceRules(nodes, plan.Services, e.store, plan.Purpose)
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
//
// The sandbox row is written before nodes are duplicated so hostname / Docker
// identity generation can insert the sand marker during {{draft.*}} re-resolve
// and shared-service network attach. Omitted source services are never
// duplicated (avoids create-then-delete and leftover shared-network attaches).
//
// When StartOnCreate is true, copied services are deployed after pin (async
// per-node, same as environment stack start). Materialize success is always
// returned; start failures land on SandboxCreateResult.StartError.
func (e *Engine) CreateSandbox(ctx context.Context, req SandboxCreateRequest) (*SandboxCreateResult, error) {
	preview, err := e.PreviewSandbox(ctx, req)
	if err != nil {
		return nil, err
	}
	omit := map[string]bool{}
	choiceBySource := map[string]ServiceDataChoice{}
	for _, rule := range preview.Services {
		switch rule.Mode {
		case SandboxServiceOmit:
			omit[rule.SourceNodeID] = true
		case SandboxServiceShare:
			choiceBySource[rule.SourceNodeID] = ServiceDataChoice{SourceNodeID: rule.SourceNodeID, Mode: ServiceDataShare}
		default:
			mode := rule.DataMode
			if mode == "" {
				mode = ServiceDataClone
			}
			choiceBySource[rule.SourceNodeID] = ServiceDataChoice{SourceNodeID: rule.SourceNodeID, Mode: mode, Consistency: rule.Consistency}
		}
	}

	// Create the environment + sandbox record first so isSandboxEnvironment is
	// true for every subsequent identity computation in this env.
	env, err := e.store.CreateEnvironment(preview.ProjectID, strings.TrimSpace(req.Name))
	if err != nil {
		return nil, err
	}
	cleanup := func() { e.cleanupFailedSandbox(ctx, env) }

	// Persist resolved repository refs (including human branch/PR names) on the
	// plan so detail/refresh UIs do not only see raw SHAs on node settings.
	plan := preview.Plan
	plan.Repositories = make([]SandboxRepositoryRef, 0, len(preview.Repositories))
	for _, repo := range preview.Repositories {
		plan.Repositories = append(plan.Repositories, SandboxRepositoryRef{
			RepoRoot:  repo.RepoRoot,
			Ref:       repo.Ref,
			CommitSHA: repo.CommitSHA,
		})
	}
	planJSON, err := json.Marshal(plan)
	if err != nil {
		cleanup()
		return nil, err
	}
	purpose := string(preview.Plan.Purpose)
	if purpose == "" {
		purpose = string(SandboxPurposePreview)
	}
	sandbox, err := e.store.CreateSandbox(&store.Sandbox{
		ProjectID:           preview.ProjectID,
		EnvironmentID:       env.ID,
		SourceEnvironmentID: preview.SourceEnvironmentID,
		ProfileID:           preview.ProfileID,
		Name:                strings.TrimSpace(req.Name),
		Purpose:             purpose,
		Status:              "active",
		PlanJSON:            string(planJSON),
		ExpiresAt:           preview.ExpiresAt,
		WarnAt:              preview.WarnAt,
		GraceEndsAt:         preview.GraceEndsAt,
	}, req.Links, preview.Repositories)
	if err != nil {
		cleanup()
		return nil, err
	}

	sourceNodes, err := e.store.ListNodesByEnvironment(preview.SourceEnvironmentID)
	if err != nil {
		cleanup()
		return nil, err
	}
	// Skip omit up front so shared roots are never attached for excluded services.
	toDuplicate := make([]store.CanvasNode, 0, len(sourceNodes))
	for _, n := range sourceNodes {
		if omit[n.ID] {
			continue
		}
		toDuplicate = append(toDuplicate, n)
	}
	if err := e.duplicateNodesInto(ctx, toDuplicate, env, choiceBySource); err != nil {
		cleanup()
		return nil, err
	}

	if err := e.pinCopiedNodesToRepositories(ctx, toDuplicate, env.ID, preview.Repositories); err != nil {
		cleanup()
		return nil, err
	}

	out := &SandboxCreateResult{Sandbox: sandbox}
	if req.StartOnCreate {
		stack, startErr := e.RunEnvironmentStack(ctx, sandbox.EnvironmentID, StackStart)
		out.Stack = stack
		if startErr != nil {
			out.StartError = startErr.Error()
		} else if stack != nil && stack.Failed > 0 {
			out.StartError = formatStackStartError(stack)
			out.Started = stack.Succeeded > 0
		} else {
			out.Started = true
		}
	}
	return out, nil
}

// ListSandboxSourceRepos discovers git repositories used by services in a
// source environment and, when available, open PRs via the GitHub CLI.
func (e *Engine) ListSandboxSourceRepos(ctx context.Context, sourceEnvironmentID uint) (*SandboxSourceRepos, error) {
	source, err := e.store.GetEnvironment(sourceEnvironmentID)
	if err != nil {
		return nil, fmt.Errorf("source environment not found: %w", err)
	}
	nodes, err := e.store.ListNodesByEnvironment(source.ID)
	if err != nil {
		return nil, err
	}

	type accum struct {
		repo     SandboxSourceRepo
		labelSet map[string]struct{}
	}
	byRoot := map[string]*accum{}
	for _, node := range nodes {
		// Shared alias nodes still resolve a repo root; include them so the
		// dialog can show which services are affected, even if the user later
		// chooses share/omit (pin only applies to copies).
		root, err := e.store.ResolveGitRepoRoot(ctx, node.ID, source.ProjectID)
		if err != nil || strings.TrimSpace(root) == "" {
			continue
		}
		root = filepath.Clean(root)
		entry, ok := byRoot[root]
		if !ok {
			defaultRef := "HEAD"
			if settings, err := e.store.GetNodeSettings(node.ID); err == nil {
				if branch := strings.TrimSpace(settings["git_branch"]); branch != "" {
					defaultRef = branch
				}
			}
			entry = &accum{
				repo: SandboxSourceRepo{
					RepoRoot:   root,
					DefaultRef: defaultRef,
				},
				labelSet: map[string]struct{}{},
			}
			byRoot[root] = entry
		}
		entry.repo.NodeIDs = append(entry.repo.NodeIDs, node.ID)
		if _, seen := entry.labelSet[node.Label]; !seen {
			entry.labelSet[node.Label] = struct{}{}
			entry.repo.ServiceLabels = append(entry.repo.ServiceLabels, node.Label)
		}
	}

	out := &SandboxSourceRepos{
		ProjectID:           source.ProjectID,
		SourceEnvironmentID: source.ID,
		Repositories:        make([]SandboxSourceRepo, 0, len(byRoot)),
	}
	for _, entry := range byRoot {
		repo := entry.repo
		sort.Strings(repo.ServiceLabels)
		sort.Strings(repo.NodeIDs)
		if branches, err := gitsrc.ListBranches(ctx, repo.RepoRoot); err == nil {
			repo.Branches = branches
		}
		status := gitsrc.CheckPullRequestsAvailable(ctx, repo.RepoRoot)
		repo.PullRequestsAvailable = status.Available
		repo.PullRequestsError = status.Reason
		if status.Available {
			if prs, err := gitsrc.ListPullRequests(ctx, repo.RepoRoot, 30); err == nil {
				repo.PullRequests = prs
			} else {
				// Probe passed but list failed (network/auth flake) — keep UI
				// on branch/ref mode and surface the reason.
				repo.PullRequestsAvailable = false
				repo.PullRequestsError = err.Error()
				repo.PullRequests = nil
			}
		}
		out.Repositories = append(out.Repositories, repo)
	}
	sort.Slice(out.Repositories, func(i, j int) bool {
		return out.Repositories[i].RepoRoot < out.Repositories[j].RepoRoot
	})
	return out, nil
}

// ResolveSandboxRef resolves a human ref (or explicit SHA) for a repo root into
// a deployable pin. PreferDeployableRef rewrites bare PR head names to
// origin/<branch> when needed. prNumber triggers a fetch of pull/<n>/head into
// origin/pr/<n> when the branch still cannot be resolved (fork PRs).
//
// When commitSHA is provided and the ref cannot be resolved locally, the
// provided SHA is accepted if it looks like a git object id — but Ref is still
// rewritten to a deployable tip-following name whenever possible so Settings
// does not store a dead branch label.
func (e *Engine) ResolveSandboxRef(ctx context.Context, repoRoot, ref, commitSHA string) (*store.SandboxRepositorySource, error) {
	return e.resolveSandboxRef(ctx, repoRoot, ref, commitSHA, 0)
}

func (e *Engine) resolveSandboxRef(ctx context.Context, repoRoot, ref, commitSHA string, prNumber int) (*store.SandboxRepositorySource, error) {
	repoRoot = filepath.Clean(strings.TrimSpace(repoRoot))
	ref = strings.TrimSpace(ref)
	commitSHA = strings.TrimSpace(commitSHA)
	if prNumber <= 0 {
		prNumber = gitsrc.ParsePRNumber(ref)
	}
	if repoRoot == "" {
		return nil, fmt.Errorf("repo root is required")
	}
	if ref == "" && commitSHA == "" && prNumber <= 0 {
		return nil, fmt.Errorf("ref or commit SHA is required")
	}
	if !gitsrc.IsRepo(repoRoot) {
		return nil, fmt.Errorf("%s is not a git repository", repoRoot)
	}

	// Prefer a ref that actually resolves in this clone (local branch or origin/*).
	if ref != "" {
		ref = gitsrc.PreferDeployableRef(ctx, repoRoot, ref)
	}

	var (
		sha string
		err error
	)
	if ref != "" {
		sha, err = gitsrc.ResolveSHA(ctx, repoRoot, ref)
	} else {
		err = fmt.Errorf("ref is empty")
	}
	if err != nil && prNumber > 0 {
		// Fork PR heads often are not present as local/remote branches. Fetch
		// GitHub's synthetic pull/<n>/head into origin/pr/<n> and pin that.
		if prRef, fetchErr := gitsrc.EnsurePullRequestRef(ctx, repoRoot, prNumber); fetchErr == nil {
			ref = prRef
			sha, err = gitsrc.ResolveSHA(ctx, repoRoot, ref)
		} else if err == nil || strings.Contains(err.Error(), "empty") {
			err = fetchErr
		} else {
			err = fmt.Errorf("%w; %v", err, fetchErr)
		}
	}
	if err != nil && commitSHA != "" {
		// Prefer verifying the explicit SHA when the human ref is missing locally.
		if verified, vErr := gitsrc.ResolveSHA(ctx, repoRoot, commitSHA); vErr == nil {
			sha = verified
			err = nil
			// Keep a tip-following name when we have a PR; otherwise leave ref
			// as the (possibly still-unresolvable) branch label only if empty.
			if ref == "" || looksLikeGitObjectID(ref) {
				if prNumber > 0 {
					if prRef, fetchErr := gitsrc.EnsurePullRequestRef(ctx, repoRoot, prNumber); fetchErr == nil {
						ref = prRef
					} else {
						ref = commitSHA
					}
				} else {
					ref = commitSHA
				}
			}
		} else if looksLikeGitObjectID(commitSHA) {
			// Last resort: pin the object id so create can proceed; deploy may
			// still need a fetch later.
			sha = commitSHA
			err = nil
			if ref == "" || looksLikeGitObjectID(ref) {
				ref = commitSHA
			}
		}
	}
	if err != nil {
		if ref == "" {
			return nil, err
		}
		return nil, fmt.Errorf("ref %q not found in repository (try fetching the branch or open the PR with gh)", ref)
	}
	if ref == "" {
		ref = sha
	}
	return &store.SandboxRepositorySource{RepoRoot: repoRoot, Ref: ref, CommitSHA: sha}, nil
}

// sandboxGitBranchValue is what we write to node_settings.git_branch.
// Prefer the human ref (branch/PR head) so Settings is readable and ordinary
// redeploys follow tip. Only fall back to the SHA when no human ref was stored
// (or when freezing for "same SHA" rebuilds).
func sandboxGitBranchValue(ref, commitSHA string, freezeToSHA bool) string {
	ref = strings.TrimSpace(ref)
	commitSHA = strings.TrimSpace(commitSHA)
	if freezeToSHA {
		if commitSHA != "" {
			return commitSHA
		}
		return ref
	}
	if ref != "" && !looksLikeGitObjectID(ref) {
		return ref
	}
	if ref != "" {
		return ref
	}
	return commitSHA
}

// RefreshSandbox re-pins sandbox copies and redeploys them.
// Mode tip re-resolves human refs and keeps git_branch on the branch/ref so
// future deploys follow tip; mode same freezes git_branch to the recorded SHA
// and redeploys that exact commit.
func (e *Engine) RefreshSandbox(ctx context.Context, req SandboxRefreshRequest) (*SandboxRefreshResult, error) {
	mode := req.Mode
	if mode == "" {
		mode = SandboxRefreshTip
	}
	if mode != SandboxRefreshTip && mode != SandboxRefreshSame {
		return nil, fmt.Errorf("invalid sandbox refresh mode %q", mode)
	}
	sandbox, err := e.store.GetSandbox(req.SandboxID)
	if err != nil {
		return nil, err
	}
	if sandbox.Status == "expired" || sandbox.Status == "cleanup_failed" {
		return nil, fmt.Errorf("sandbox %q is not live (status %s)", sandbox.Name, sandbox.Status)
	}
	repos, err := e.store.ListSandboxRepositorySources(sandbox.ID)
	if err != nil {
		return nil, err
	}
	if len(repos) == 0 {
		return nil, fmt.Errorf("sandbox %q has no repository pins to refresh", sandbox.Name)
	}

	freezeToSHA := mode == SandboxRefreshSame
	updated := make([]store.SandboxRepositorySource, 0, len(repos))
	for _, repo := range repos {
		ref := strings.TrimSpace(repo.Ref)
		sha := strings.TrimSpace(repo.CommitSHA)
		switch mode {
		case SandboxRefreshSame:
			if sha == "" {
				return nil, fmt.Errorf("sandbox pin for %s has no commit SHA", repo.RepoRoot)
			}
			// Prefer verifying the frozen SHA still exists; fall back to stored value.
			if verified, err := gitsrc.ResolveSHA(ctx, repo.RepoRoot, sha); err == nil {
				sha = verified
			}
			// Keep the original human ref on the pin record for display; only
			// git_branch is frozen to the SHA for this redeploy.
			if ref == "" {
				ref = sha
			}
		case SandboxRefreshTip:
			if ref == "" {
				ref = sha
			}
			if ref == "" {
				return nil, fmt.Errorf("sandbox pin for %s has no ref to re-resolve", repo.RepoRoot)
			}
			// Re-resolve with PR awareness (origin/<branch> or re-fetch origin/pr/N).
			resolved, err := e.resolveSandboxRef(ctx, repo.RepoRoot, ref, "", gitsrc.ParsePRNumber(ref))
			if err != nil {
				return nil, fmt.Errorf("refresh %s: %w", repo.RepoRoot, err)
			}
			ref = resolved.Ref
			sha = resolved.CommitSHA
		}
		updated = append(updated, store.SandboxRepositorySource{
			RepoRoot:  repo.RepoRoot,
			Ref:       ref,
			CommitSHA: sha,
		})
	}

	if err := e.store.ReplaceSandboxRepositorySources(sandbox.ID, updated); err != nil {
		return nil, err
	}
	// Keep PlanJSON repositories in sync so detail views stay accurate.
	var plan SandboxPlan
	if err := json.Unmarshal([]byte(sandbox.PlanJSON), &plan); err == nil {
		plan.Repositories = make([]SandboxRepositoryRef, 0, len(updated))
		for _, repo := range updated {
			plan.Repositories = append(plan.Repositories, SandboxRepositoryRef{
				RepoRoot:  repo.RepoRoot,
				Ref:       repo.Ref,
				CommitSHA: repo.CommitSHA,
			})
		}
		if planJSON, err := json.Marshal(plan); err == nil {
			_ = e.store.UpdateSandboxPlanJSON(sandbox.ID, string(planJSON))
		}
	}

	nodes, err := e.store.ListNodesByEnvironment(sandbox.EnvironmentID)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		// Skip shared aliases — their root lives in another env and must not
		// receive sandbox source pins.
		if link, _ := e.GetServiceLink(node.ID); link != nil {
			continue
		}
		root, err := e.store.ResolveGitRepoRoot(ctx, node.ID, sandbox.ProjectID)
		if err != nil || root == "" {
			continue
		}
		root = filepath.Clean(root)
		for _, repo := range updated {
			if filepath.Clean(repo.RepoRoot) == root {
				value := sandboxGitBranchValue(repo.Ref, repo.CommitSHA, freezeToSHA)
				if err := e.store.SetNodeSetting(node.ID, "git_branch", value); err != nil {
					return nil, err
				}
				break
			}
		}
	}

	if sandbox.Status == "suspended" {
		if _, err := e.ResumeSandbox(ctx, sandbox.ID); err != nil {
			return nil, err
		}
	}
	// Re-pin success is the primary outcome; stack redeploy is best-effort so
	// offline / non-deployable services still leave pins updated for the next start.
	stack, stackErr := e.RunEnvironmentStack(ctx, sandbox.EnvironmentID, StackRedeploy)
	fresh, err := e.store.GetSandbox(sandbox.ID)
	if err != nil {
		return nil, err
	}
	out := &SandboxRefreshResult{Sandbox: fresh, Repositories: updated, Stack: stack}
	if stackErr != nil {
		return out, stackErr
	}
	return out, nil
}

// cleanupFailedSandbox tears down a partially created sandbox: disconnect any
// shared-root network attaches, remove the sandbox Docker network if present,
// then delete the environment (and cascaded sandbox row) from the store.
func (e *Engine) cleanupFailedSandbox(ctx context.Context, env *store.Environment) {
	if env == nil {
		return
	}
	nodes, err := e.store.ListNodesByEnvironment(env.ID)
	if err == nil {
		for _, n := range nodes {
			if link, _ := e.GetServiceLink(n.ID); link != nil {
				_ = e.DisconnectServiceLinkNetwork(ctx, n.ID)
			}
		}
	}
	if project, err := e.store.GetProject(env.ProjectID); err == nil {
		dockerEnv := networking.DockerEnvironment(env.Slug, true)
		_ = e.RemoveNetwork(ctx, draftNetworkName(project.ID, project.Name, dockerEnv))
	}
	_ = e.store.DeleteEnvironment(env.ID)
}

// ExtendSandbox is deliberately an explicit lifecycle action. It restores an
// expired/warning sandbox to active and recalculates warning/grace windows from
// the immutable creation plan, rather than from any profile that has changed
// since the sandbox was created. Duration is added to the remaining lifetime
// when the sandbox has not yet expired; otherwise it starts from now.
func (e *Engine) ExtendSandbox(sandboxID uint, ttlHours int) (*store.Sandbox, error) {
	if ttlHours <= 0 {
		return nil, fmt.Errorf("sandbox extension must be greater than zero hours")
	}
	sandbox, err := e.store.GetSandbox(sandboxID)
	if err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	base := now
	if sandbox.ExpiresAt.After(now) {
		base = sandbox.ExpiresAt.UTC()
	}
	return e.extendSandboxTo(sandboxID, base.Add(time.Duration(ttlHours)*time.Hour))
}

// ExtendSandboxUntil sets an absolute expiry time and restores the sandbox to
// active using the creation plan's warning/grace windows.
func (e *Engine) ExtendSandboxUntil(sandboxID uint, expiresAt time.Time) (*store.Sandbox, error) {
	expiresAt = expiresAt.UTC()
	if !expiresAt.After(time.Now().UTC()) {
		return nil, fmt.Errorf("sandbox expiry must be in the future")
	}
	return e.extendSandboxTo(sandboxID, expiresAt)
}

func (e *Engine) extendSandboxTo(sandboxID uint, expiresAt time.Time) (*store.Sandbox, error) {
	sandbox, err := e.store.GetSandbox(sandboxID)
	if err != nil {
		return nil, err
	}
	var plan SandboxPlan
	if err := json.Unmarshal([]byte(sandbox.PlanJSON), &plan); err != nil {
		return nil, fmt.Errorf("read sandbox plan: %w", err)
	}
	if plan.WarningHours == 0 {
		plan.WarningHours = 24
	}
	if plan.GraceHours == 0 {
		plan.GraceHours = 72
	}
	now := time.Now().UTC()
	warn := expiresAt.Add(-time.Duration(plan.WarningHours) * time.Hour)
	if warn.Before(now) {
		warn = now
	}
	grace := expiresAt.Add(time.Duration(plan.GraceHours) * time.Hour)
	if err := e.store.ExtendSandbox(sandboxID, expiresAt, warn, grace); err != nil {
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
	_ = e.store.TouchSandboxActivity(sandboxID, time.Now().UTC())
	return e.store.GetSandbox(sandboxID)
}

// DeleteSandbox is deliberately more destructive than DeleteEnvironment: a
// sandbox is disposable, so its Draft-managed volumes are removed alongside
// containers, images, routes, and its Docker network. Explicit user-managed
// volumes and bind mounts are never selected by this cleanup path.
// On partial failure the sandbox is marked cleanup_failed with a purge inventory.
func (e *Engine) DeleteSandbox(ctx context.Context, sandboxID uint) error {
	inv, err := e.PreviewSandboxPurge(ctx, sandboxID)
	if err != nil {
		return err
	}
	fail := func(last error) error {
		inv.LastError = last.Error()
		now := time.Now().UTC()
		inv.BuiltAt = now
		raw, _ := json.Marshal(inv)
		_ = e.store.SaveSandboxCleanupFailure(sandboxID, string(raw), last.Error())
		return last
	}

	for i := range inv.Items {
		item := &inv.Items[i]
		switch item.Kind {
		case SandboxPurgeNode:
			if err := e.guardRootDelete(item.ID); err != nil {
				item.Status = "failed"
				item.Error = err.Error()
				return fail(err)
			}
			if err := e.DeleteService(ctx, item.ID); err != nil {
				item.Status = "failed"
				item.Error = err.Error()
				return fail(fmt.Errorf("delete service %q: %w", item.Label, err))
			}
			item.Status = "removed"
		case SandboxPurgeVolume:
			if err := e.DeleteManagedVolume(ctx, item.ID, true); err != nil {
				item.Status = "failed"
				item.Error = err.Error()
				return fail(fmt.Errorf("remove sandbox volume %q: %w", item.ID, err))
			}
			item.Status = "removed"
		case SandboxPurgeNetwork:
			if err := e.RemoveNetwork(ctx, item.ID); err != nil && !strings.Contains(strings.ToLower(err.Error()), "not found") {
				item.Status = "failed"
				item.Error = err.Error()
				return fail(err)
			}
			item.Status = "removed"
		}
	}
	_ = e.store.ClearSandboxCleanupDetail(sandboxID)
	return e.store.DeleteEnvironment(inv.EnvironmentID)
}

// SandboxPurgeItemKind classifies rows in a sandbox purge inventory.
type SandboxPurgeItemKind string

const (
	SandboxPurgeNode    SandboxPurgeItemKind = "node"
	SandboxPurgeVolume  SandboxPurgeItemKind = "volume"
	SandboxPurgeNetwork SandboxPurgeItemKind = "network"
)

// SandboxPurgeItem is one resource that DeleteSandbox will remove.
type SandboxPurgeItem struct {
	Kind   SandboxPurgeItemKind `json:"kind"`
	ID     string               `json:"id"`
	Label  string               `json:"label,omitempty"`
	Status string               `json:"status"` // pending | removed | failed | skipped
	Error  string               `json:"error,omitempty"`
}

// SandboxPurgeInventory lists what a sandbox purge will (or did) touch.
type SandboxPurgeInventory struct {
	SandboxID     uint               `json:"sandboxId"`
	EnvironmentID uint               `json:"environmentId"`
	NetworkName   string             `json:"networkName"`
	Items         []SandboxPurgeItem `json:"items"`
	LastError     string             `json:"lastError,omitempty"`
	BuiltAt       time.Time          `json:"builtAt"`
}

// PreviewSandboxPurge builds a dry-run inventory of sandbox-owned resources.
func (e *Engine) PreviewSandboxPurge(ctx context.Context, sandboxID uint) (*SandboxPurgeInventory, error) {
	sandbox, err := e.store.GetSandbox(sandboxID)
	if err != nil {
		return nil, err
	}
	project, err := e.store.GetProject(sandbox.ProjectID)
	if err != nil {
		return nil, err
	}
	env, err := e.store.GetEnvironment(sandbox.EnvironmentID)
	if err != nil {
		return nil, err
	}
	dockerEnv := networking.DockerEnvironment(env.Slug, true)
	netName := draftNetworkName(project.ID, project.Name, dockerEnv)
	inv := &SandboxPurgeInventory{
		SandboxID:     sandbox.ID,
		EnvironmentID: sandbox.EnvironmentID,
		NetworkName:   netName,
		Items:         []SandboxPurgeItem{},
		BuiltAt:       time.Now().UTC(),
	}
	nodes, err := e.store.ListNodesByEnvironment(sandbox.EnvironmentID)
	if err != nil {
		return nil, err
	}
	for _, node := range nodes {
		inv.Items = append(inv.Items, SandboxPurgeItem{
			Kind:   SandboxPurgeNode,
			ID:     node.ID,
			Label:  node.Label,
			Status: "pending",
		})
		volumes, err := e.ListManagedVolumes(ctx, &sandbox.ProjectID, node.ID)
		if err != nil {
			return nil, fmt.Errorf("list volumes for %s: %w", node.Label, err)
		}
		for _, volume := range volumes {
			inv.Items = append(inv.Items, SandboxPurgeItem{
				Kind:   SandboxPurgeVolume,
				ID:     volume.Name,
				Label:  volume.Target,
				Status: "pending",
			})
		}
	}
	inv.Items = append(inv.Items, SandboxPurgeItem{
		Kind:   SandboxPurgeNetwork,
		ID:     netName,
		Label:  env.Name,
		Status: "pending",
	})
	return inv, nil
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
	if plan.Purpose == "" {
		plan.Purpose = SandboxPurposePreview
	}
	if plan.Purpose != SandboxPurposePreview && plan.Purpose != SandboxPurposeTest {
		return plan, 0, fmt.Errorf("invalid sandbox purpose %q", plan.Purpose)
	}
	if plan.TTLHours == 0 {
		if plan.Purpose == SandboxPurposeTest {
			plan.TTLHours = 4
		} else {
			plan.TTLHours = settings.DefaultTTLHours
		}
	}
	if plan.WarningHours == 0 {
		if plan.Purpose == SandboxPurposeTest {
			plan.WarningHours = 1
		} else {
			plan.WarningHours = settings.WarningHours
		}
	}
	if plan.GraceHours == 0 {
		if plan.Purpose == SandboxPurposeTest {
			plan.GraceHours = 2
		} else {
			plan.GraceHours = settings.GraceHours
		}
	}
	if plan.SuspendIdleHours == 0 {
		plan.SuspendIdleHours = settings.SuspendIdleHours
	}
	if plan.Purpose == SandboxPurposeTest {
		if len(plan.Steps) == 0 {
			return plan, 0, fmt.Errorf("testing sandbox requires at least one test step")
		}
		if plan.OnComplete == "" {
			plan.OnComplete = SandboxOnCompleteLeave
		}
		switch plan.OnComplete {
		case SandboxOnCompleteLeave, SandboxOnCompleteDelete, SandboxOnCompleteSuspend:
		default:
			return plan, 0, fmt.Errorf("invalid sandbox onComplete %q", plan.OnComplete)
		}
		for i, step := range plan.Steps {
			step.ServiceLabel = strings.TrimSpace(step.ServiceLabel)
			step.Name = strings.TrimSpace(step.Name)
			step.WorkDir = strings.TrimSpace(step.WorkDir)
			if step.ServiceLabel == "" {
				return plan, 0, fmt.Errorf("test step %d requires serviceLabel", i+1)
			}
			if len(step.Cmd) == 0 {
				return plan, 0, fmt.Errorf("test step %d requires cmd", i+1)
			}
			for _, part := range step.Cmd {
				if strings.TrimSpace(part) == "" {
					return plan, 0, fmt.Errorf("test step %d has empty cmd part", i+1)
				}
			}
			if step.Name == "" {
				step.Name = strings.Join(step.Cmd, " ")
			}
			step.OutputContains = strings.TrimSpace(step.OutputContains)
			for _, code := range step.ExpectedExitCodes {
				if code < 0 {
					return plan, 0, fmt.Errorf("test step %d has invalid expected exit code", i+1)
				}
			}
			plan.Steps[i] = step
		}
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
	if override.Purpose != "" {
		base.Purpose = override.Purpose
	}
	if override.OnComplete != "" {
		base.OnComplete = override.OnComplete
	}
	if override.Steps != nil {
		base.Steps = override.Steps
	}
	if override.Services != nil {
		base.Services = override.Services
	}
	if override.Repositories != nil {
		base.Repositories = override.Repositories
	}
	return base
}

func resolveSandboxServiceRules(nodes []store.CanvasNode, supplied []SandboxServiceRule, s *store.Store, purpose SandboxPurpose) []SandboxServiceRule {
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
			// Testing sandboxes default to fresh data so reruns stay isolated.
			// Preview sandboxes still clone managed volumes by default.
			if purpose == SandboxPurposeTest {
				r.DataMode = ServiceDataFresh
			} else if len(managedVolumePaths(settings)) > 0 {
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
	type override struct {
		ref      string
		sha      string
		prNumber int
	}
	refs := map[string]override{}
	for _, r := range overrides {
		root := filepath.Clean(strings.TrimSpace(r.RepoRoot))
		if root == "" || root == "." {
			continue
		}
		refs[root] = override{
			ref:      strings.TrimSpace(r.Ref),
			sha:      strings.TrimSpace(r.CommitSHA),
			prNumber: r.PRNumber,
		}
	}
	seen := map[string]store.SandboxRepositorySource{}
	for _, node := range nodes {
		root, err := e.store.ResolveGitRepoRoot(ctx, node.ID, projectID)
		if err != nil || root == "" {
			continue
		}
		root = filepath.Clean(root)
		ov := refs[root]
		ref := ov.ref
		if ref == "" {
			settings, _ := e.store.GetNodeSettings(node.ID)
			ref = strings.TrimSpace(settings["git_branch"])
		}
		if ref == "" {
			if ov.sha != "" {
				ref = ov.sha
			} else if ov.prNumber > 0 {
				ref = fmt.Sprintf("pr-%d", ov.prNumber)
			} else {
				ref = "HEAD"
			}
		}
		resolved, err := e.resolveSandboxRef(ctx, root, ref, ov.sha, ov.prNumber)
		if err != nil {
			return nil, fmt.Errorf("resolve sandbox source for %s: %w", root, err)
		}
		seen[root] = *resolved
	}
	out := make([]store.SandboxRepositorySource, 0, len(seen))
	for _, source := range seen {
		out = append(out, source)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].RepoRoot < out[j].RepoRoot })
	return out, nil
}

func looksLikeGitObjectID(s string) bool {
	if len(s) < 7 || len(s) > 40 {
		return false
	}
	for _, c := range s {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') && (c < 'A' || c > 'F') {
			return false
		}
	}
	return true
}

// resolveExplicitRepositoryPins resolves caller-supplied branch/PR overrides.
// Empty input yields nil (keep copied git_branch). Unlike sandbox plan
// resolution, this does not inherit source pins — omit a repo to leave it alone.
func (e *Engine) resolveExplicitRepositoryPins(ctx context.Context, overrides []SandboxRepositoryRef) ([]store.SandboxRepositorySource, error) {
	if len(overrides) == 0 {
		return nil, nil
	}
	out := make([]store.SandboxRepositorySource, 0, len(overrides))
	seen := map[string]struct{}{}
	for _, o := range overrides {
		repoRoot := filepath.Clean(strings.TrimSpace(o.RepoRoot))
		if repoRoot == "" || repoRoot == "." {
			continue
		}
		if _, ok := seen[repoRoot]; ok {
			continue
		}
		seen[repoRoot] = struct{}{}
		resolved, err := e.resolveSandboxRef(ctx, repoRoot, o.Ref, o.CommitSHA, o.PRNumber)
		if err != nil {
			return nil, fmt.Errorf("resolve %s: %w", repoRoot, err)
		}
		out = append(out, *resolved)
	}
	return out, nil
}

// pinCopiedNodesToRepositories stamps git_branch on independent copies that
// match the given repo roots. Shared/linked aliases are skipped. Source labels
// identify duplicates (unique within an environment).
func (e *Engine) pinCopiedNodesToRepositories(ctx context.Context, sourceNodes []store.CanvasNode, newEnvID uint, repositories []store.SandboxRepositorySource) error {
	if len(repositories) == 0 {
		return nil
	}
	targetNodes, err := e.store.ListNodesByEnvironment(newEnvID)
	if err != nil {
		return err
	}
	targetByLabel := make(map[string]store.CanvasNode, len(targetNodes))
	for _, n := range targetNodes {
		targetByLabel[n.Label] = n
	}
	for _, sourceNode := range sourceNodes {
		target, ok := targetByLabel[sourceNode.Label]
		if !ok {
			return fmt.Errorf("duplicate missing service %q", sourceNode.Label)
		}
		if err := e.pinSandboxNodeToRepository(ctx, target.ID, sourceNode.ID, repositories); err != nil {
			return err
		}
	}
	return nil
}

func (e *Engine) pinSandboxNodeToRepository(ctx context.Context, targetNodeID, sourceNodeID string, repositories []store.SandboxRepositorySource) error {
	// Shared aliases keep the root's code; do not rewrite their git_branch.
	if link, _ := e.GetServiceLink(targetNodeID); link != nil {
		return nil
	}
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
			// Prefer human ref (branch/PR head) so Settings shows the branch and
			// ordinary redeploys follow tip. SHA remains on SandboxRepositorySource
			// for detail UI + "same SHA" refresh.
			value := sandboxGitBranchValue(repo.Ref, repo.CommitSHA, false)
			if value == "" {
				return nil
			}
			return e.store.SetNodeSetting(targetNodeID, "git_branch", value)
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
//
// Status transitions are applied in-memory so a single pass can move
// active → warning → expired → deleted when timestamps land in the same
// reconcile window (for example graceHours=0). When SuspendIdleHours > 0 on
// the frozen plan, idle active/warning sandboxes are suspended before expiry.
func (e *Engine) ReconcileSandboxLifecycle(ctx context.Context, now time.Time) error {
	sandboxes, err := e.store.ListSandboxesForLifecycle()
	if err != nil {
		return err
	}
	for _, sandbox := range sandboxes {
		status := sandbox.Status
		plan, _ := parseSandboxPlan(sandbox.PlanJSON)

		// Idle auto-suspend: only while still live (not expired/suspended).
		if (status == "active" || status == "warning") && plan.SuspendIdleHours > 0 {
			last := sandbox.CreatedAt
			if sandbox.LastActivityAt != nil {
				last = *sandbox.LastActivityAt
			}
			idleFor := now.Sub(last.UTC())
			if idleFor >= time.Duration(plan.SuspendIdleHours)*time.Hour {
				if _, err := e.SuspendSandbox(ctx, sandbox.ID); err != nil {
					log.Printf("[sandbox] idle suspend %d: %v", sandbox.ID, err)
				} else {
					status = "suspended"
				}
			}
		}

		// Warning only applies while the sandbox is still considered live.
		if status == "active" && !sandbox.WarnAt.After(now) {
			if err := e.store.UpdateSandboxStatus(sandbox.ID, "warning", nil); err != nil {
				return err
			}
			status = "warning"
		}
		// Suspended sandboxes still expire on schedule so they are not left
		// around forever after the user stops them and walks away.
		if (status == "active" || status == "warning" || status == "suspended") && !sandbox.ExpiresAt.After(now) {
			if err := e.store.UpdateSandboxStatus(sandbox.ID, "expired", nil); err != nil {
				return err
			}
			status = "expired"
		}
		// cleanup_failed is retriable: a prior purge attempt may have failed
		// because Docker was down mid-delete. Continue other sandboxes on
		// failure so one stuck purge does not block the project.
		if (status == "expired" || status == "cleanup_failed") && !sandbox.GraceEndsAt.After(now) {
			if err := e.DeleteSandbox(ctx, sandbox.ID); err != nil {
				log.Printf("[sandbox] purge %d: %v", sandbox.ID, err)
			}
		}
	}
	return nil
}

func parseSandboxPlan(planJSON string) (SandboxPlan, error) {
	var plan SandboxPlan
	if strings.TrimSpace(planJSON) == "" {
		return plan, nil
	}
	err := json.Unmarshal([]byte(planJSON), &plan)
	return plan, err
}
