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
	return ReconcileProjectHooks(ctx, s, projectID)
}

// SetRedeployOnPull toggles the independent "redeploy on pull" behavior for a
// node. Unlike SetDeployTrigger, this is a boolean that is orthogonal to the
// 3-way deploy trigger: a node may be Manual and still redeploy on pull, or
// On push and also redeploy on pull. It installs/uninstalls the repo's
// post-merge hook to match.
func SetRedeployOnPull(ctx context.Context, s *store.Store, nodeID string, projectID uint, enabled bool) error {
	value := ""
	if enabled {
		value = "true"
	}
	if err := s.SetNodeSetting(nodeID, "redeploy_on_pull", value); err != nil {
		return err
	}
	return ReconcileProjectHooks(ctx, s, projectID)
}

// ReconcileProjectHooks ensures the project's installed hooks match what its
// nodes actually need: a hook exists iff at least one node has the trigger and
// a pinned branch.
func ReconcileProjectHooks(ctx context.Context, s *store.Store, projectID uint) error {
	project, err := s.GetProject(projectID)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}
	if !gitsrc.IsRepo(project.Path) {
		// Setting can still be stored, but no hooks are installed in non-git dirs.
		return nil
	}

	nodes, err := s.ListNodes(projectID)
	if err != nil {
		return err
	}

	wantCommit, wantPush, wantPull := false, false, false
	for _, node := range nodes {
		settings, err := s.GetNodeSettings(node.ID)
		if err != nil {
			continue
		}
		branch := strings.TrimSpace(settings["git_branch"])
		if branch == "" {
			continue
		}
		if canonical := gitsrc.PreferLocalRef(ctx, project.Path, branch); canonical != "" && canonical != branch {
			if err := s.SetNodeSetting(node.ID, "git_branch", canonical); err != nil {
				return err
			}
			settings["git_branch"] = canonical
		}
		switch settings["deploy_trigger"] {
		case "on_commit":
			wantCommit = true
		case "on_push":
			wantPush = true
		}
		// Redeploy-on-pull is independent of the 3-way deploy trigger: any
		// node with a pinned branch and this flag on installs the post-merge
		// hook for the repo.
		if strings.TrimSpace(settings["redeploy_on_pull"]) == "true" {
			wantPull = true
		}
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	if err := applyHook(ctx, project.Path, exe, OnCommit, wantCommit); err != nil {
		return err
	}
	if err := applyHook(ctx, project.Path, exe, OnPush, wantPush); err != nil {
		return err
	}
	return applyHook(ctx, project.Path, exe, OnPull, wantPull)
}

func applyHook(ctx context.Context, repoPath, exe string, event Event, want bool) error {
	if want {
		return Install(ctx, repoPath, exe, event)
	}
	return Uninstall(ctx, repoPath, event)
}

// StatusForProject reports whether git-triggered deploys are supported and
// whether foreign hooks are present for commit/push events.
func StatusForProject(ctx context.Context, s *store.Store, projectID uint) (GitHookStatus, error) {
	project, err := s.GetProject(projectID)
	if err != nil {
		return GitHookStatus{}, fmt.Errorf("project not found: %w", err)
	}
	if !gitsrc.IsRepo(project.Path) {
		return GitHookStatus{Supported: false}, nil
	}
	st := GitHookStatus{Supported: true}
	if _, foreign, err := Status(ctx, project.Path, OnCommit); err == nil {
		st.CommitForeign = foreign
	}
	if _, foreign, err := Status(ctx, project.Path, OnPush); err == nil {
		st.PushForeign = foreign
	}
	if _, foreign, err := Status(ctx, project.Path, OnPull); err == nil {
		st.PullForeign = foreign
	}
	return st, nil
}

// ReconcileAllProjects re-evaluates every saved project's hook needs. It
// continues through per-project failures and returns the first one encountered.
func ReconcileAllProjects(ctx context.Context, s *store.Store) error {
	projects, err := s.ListProjects()
	if err != nil {
		return fmt.Errorf("list projects: %w", err)
	}
	var firstErr error
	for _, project := range projects {
		if err := ReconcileProjectHooks(ctx, s, project.ID); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("sync project %d (%s): %w", project.ID, project.Path, err)
		}
	}
	return firstErr
}
