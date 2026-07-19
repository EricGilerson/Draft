package githooks

import (
	"context"
	"fmt"
	"os"
	"strings"

	"Draft/internal/gitsrc"
	"Draft/internal/store"
)

// GitHookStatus tells the UI whether git-triggered deploys are usable for a
// project and whether pre-existing (foreign) git hooks are present that Draft
// will chain to rather than replace.
type GitHookStatus struct {
	Supported     bool `json:"supported"`     // project directory is a git repository
	CommitForeign bool `json:"commitForeign"` // a non-Draft post-commit hook exists
	PushForeign   bool `json:"pushForeign"`   // a non-Draft pre-push hook exists
	PullForeign   bool `json:"pullForeign"`   // a non-Draft post-merge hook exists
}

var validTriggers = map[string]bool{"manual": true, "on_commit": true, "on_push": true}

// SetDeployTrigger records how a node should be deployed and reconciles the
// repository hooks needed to honor the setting.
func SetDeployTrigger(ctx context.Context, s *store.Store, nodeID string, projectID uint, trigger string) error {
	if !validTriggers[trigger] {
		return fmt.Errorf("invalid deploy trigger %q", trigger)
	}
	if err := s.SetNodeSetting(nodeID, "deploy_trigger", trigger); err != nil {
		return err
	}
	return reconcileNodeRepoHooks(ctx, s, nodeID, projectID)
}

// SetRedeployOnPull toggles the independent "redeploy on pull" behavior for a
// node. Unlike SetDeployTrigger, this is a boolean that is orthogonal to the
// 3-way deploy trigger: a node may be Manual and still redeploy on pull, or
// On push and also redeploy on pull. It installs/uninstalls the repo's
// post-merge and post-rewrite hooks so both merge and rebase pulls fire.
func SetRedeployOnPull(ctx context.Context, s *store.Store, nodeID string, projectID uint, enabled bool) error {
	value := ""
	if enabled {
		value = "true"
	}
	if err := s.SetNodeSetting(nodeID, "redeploy_on_pull", value); err != nil {
		return err
	}
	return reconcileNodeRepoHooks(ctx, s, nodeID, projectID)
}

// ReconcileProjectHooks ensures the project's installed hooks match what its
// nodes actually need: a hook exists iff at least one node has the trigger and
// a pinned branch.
func ReconcileProjectHooks(ctx context.Context, s *store.Store, projectID uint) error {
	nodes, err := s.ListNodes(projectID)
	if err != nil {
		return err
	}
	seen := map[string]bool{}
	for _, node := range nodes {
		root, err := s.CachedGitRepoRoot(node.ID)
		if err != nil {
			return err
		}
		if root == "" {
			root, err = s.ResolveGitRepoRoot(ctx, node.ID, node.ProjectID)
			if err != nil {
				continue
			}
		}
		if root == "" || seen[root] {
			continue
		}
		seen[root] = true
		if err := ReconcileRepoHooks(ctx, s, root); err != nil {
			return err
		}
	}
	return nil
}

// ReconcileRepoHooks ensures the given repo's installed hooks match what all
// nodes across all projects need from that repository.
func ReconcileRepoHooks(ctx context.Context, s *store.Store, repoRoot string) error {
	repoRoot = strings.TrimSpace(repoRoot)
	if repoRoot == "" || !gitsrc.IsRepo(repoRoot) {
		return nil
	}
	projects, err := s.ListProjects()
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}

	wantCommit, wantPush, wantPull := false, false, false
	for _, project := range projects {
		nodes, err := s.ListNodes(project.ID)
		if err != nil {
			return err
		}
		for _, node := range nodes {
			root, err := s.ResolveGitRepoRoot(ctx, node.ID, project.ID)
			if err != nil || !storePathEqual(root, repoRoot) {
				continue
			}
			settings, err := s.GetNodeSettings(node.ID)
			if err != nil {
				continue
			}
			branch := strings.TrimSpace(settings["git_branch"])
			if branch == "" {
				continue
			}
			if canonical := gitsrc.PreferLocalRef(ctx, repoRoot, branch); canonical != "" && canonical != branch {
				if err := s.SetNodeSetting(node.ID, "git_branch", canonical); err != nil {
					return err
				}
				settings["git_branch"] = canonical
			}
			switch strings.TrimSpace(settings["deploy_trigger"]) {
			case "on_commit":
				wantCommit = true
			case "on_push":
				wantPush = true
			}
			if strings.TrimSpace(settings["redeploy_on_pull"]) == "true" {
				wantPull = true
			}
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}
	if err := applyHook(ctx, repoRoot, exe, OnCommit, wantCommit); err != nil {
		return err
	}
	if err := applyHook(ctx, repoRoot, exe, OnPush, wantPush); err != nil {
		return err
	}
	if err := applyHook(ctx, repoRoot, exe, OnPull, wantPull); err != nil {
		return err
	}
	return applyHook(ctx, repoRoot, exe, OnPullRewrite, wantPull)
}

func applyHook(ctx context.Context, repoPath, exe string, event Event, want bool) error {
	if want {
		return Install(ctx, repoPath, exe, event)
	}
	return Uninstall(ctx, repoPath, event)
}

// StatusForNode reports whether git-triggered deploys are supported for the
// node's resolved repo root and whether foreign hooks are present there.
func StatusForNode(ctx context.Context, s *store.Store, nodeID string) (GitHookStatus, error) {
	node, err := s.GetNode(nodeID)
	if err != nil {
		return GitHookStatus{}, fmt.Errorf("node not found: %w", err)
	}
	repoRoot, err := s.ResolveGitRepoRoot(ctx, nodeID, node.ProjectID)
	if err == gitsrc.ErrNotRepo {
		return GitHookStatus{Supported: false}, nil
	}
	if err != nil {
		return GitHookStatus{}, err
	}
	st := GitHookStatus{Supported: true}
	if _, foreign, err := Status(ctx, repoRoot, OnCommit); err == nil {
		st.CommitForeign = foreign
	}
	if _, foreign, err := Status(ctx, repoRoot, OnPush); err == nil {
		st.PushForeign = foreign
	}
	if _, foreign, err := Status(ctx, repoRoot, OnPull); err == nil {
		st.PullForeign = foreign
	}
	if !st.PullForeign {
		if _, foreign, err := Status(ctx, repoRoot, OnPullRewrite); err == nil {
			st.PullForeign = foreign
		}
	}
	return st, nil
}

// StatusForProject is kept as a compatibility wrapper. When a project contains
// nodes, it reports the status for the first node's resolved repo root.
func StatusForProject(ctx context.Context, s *store.Store, projectID uint) (GitHookStatus, error) {
	nodes, err := s.ListNodes(projectID)
	if err != nil {
		return GitHookStatus{}, err
	}
	if len(nodes) == 0 {
		return GitHookStatus{Supported: false}, nil
	}
	return StatusForNode(ctx, s, nodes[0].ID)
}

// ReconcileAllHooks re-evaluates every saved node's repo-root hook needs. It
// continues through per-repo failures and returns the first one encountered.
func ReconcileAllHooks(ctx context.Context, s *store.Store) error {
	projects, err := s.ListProjects()
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	var firstErr error
	seen := map[string]bool{}
	for _, project := range projects {
		nodes, err := s.ListNodes(project.ID)
		if err != nil {
			if firstErr == nil {
				firstErr = err
			}
			continue
		}
		for _, node := range nodes {
			root, err := s.CachedGitRepoRoot(node.ID)
			if err != nil && firstErr == nil {
				firstErr = err
				continue
			}
			if root == "" {
				root, err = s.ResolveGitRepoRoot(ctx, node.ID, project.ID)
				if err != nil {
					continue
				}
			}
			if root == "" || seen[root] {
				continue
			}
			seen[root] = true
			if err := ReconcileRepoHooks(ctx, s, root); err != nil && firstErr == nil {
				firstErr = fmt.Errorf("sync repo %s: %w", root, err)
			}
		}
	}
	return firstErr
}

// ReconcileAllProjects is kept as a compatibility wrapper around the new
// repo-root-centric reconcile behavior.
func ReconcileAllProjects(ctx context.Context, s *store.Store) error {
	return ReconcileAllHooks(ctx, s)
}

func reconcileNodeRepoHooks(ctx context.Context, s *store.Store, nodeID string, projectID uint) error {
	repoRoot, err := s.ResolveGitRepoRoot(ctx, nodeID, projectID)
	if err == gitsrc.ErrNotRepo {
		return nil
	}
	if err != nil {
		return err
	}
	return ReconcileRepoHooks(ctx, s, repoRoot)
}

func storePathEqual(a, b string) bool {
	return strings.EqualFold(strings.TrimRight(strings.TrimSpace(a), `/\`), strings.TrimRight(strings.TrimSpace(b), `/\`))
}
