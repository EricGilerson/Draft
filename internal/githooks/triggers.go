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

	wantCommit, wantPush := false, false
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
	}

	exe, err := os.Executable()
	if err != nil {
		return fmt.Errorf("resolve executable path: %w", err)
	}

	if err := applyHook(ctx, project.Path, exe, OnCommit, wantCommit); err != nil {
		return err
	}
	return applyHook(ctx, project.Path, exe, OnPush, wantPush)
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
