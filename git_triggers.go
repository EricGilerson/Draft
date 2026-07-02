package main

import (
	"context"
	"fmt"
	"os"
	"strings"

	"Draft/internal/githooks"
	"Draft/internal/gitsrc"
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

// SetDeployTrigger records how a node should be deployed — manually, on every
// commit to its tracked branch, or on push of its tracked branch — and installs
// or removes the git hooks the project needs to honor it. A trigger is only
// meaningful alongside a pinned git branch; without one the node stays manual in
// practice even if the setting is stored.
func (a *App) SetDeployTrigger(nodeID string, projectID uint, trigger string) error {
	if a.store == nil {
		return errNoStore
	}
	if !validTriggers[trigger] {
		return fmt.Errorf("invalid deploy trigger %q", trigger)
	}
	if err := a.store.SetNodeSetting(nodeID, "deploy_trigger", trigger); err != nil {
		return err
	}
	return a.syncRepoHooks(projectID)
}

// syncRepoHooks reconciles the project's installed git hooks against what its
// nodes actually need: a hook is present exactly when at least one node in the
// repo has a matching trigger and a pinned branch. This reference-counting keeps
// one node switching back to manual from tearing down a hook another node still
// relies on.
func (a *App) syncRepoHooks(projectID uint) error {
	project, err := a.store.GetProject(projectID)
	if err != nil {
		return fmt.Errorf("project not found: %w", err)
	}
	if !gitsrc.IsRepo(project.Path) {
		// Nothing to install into; the setting is stored but inert.
		return nil
	}

	nodes, err := a.store.ListNodes(projectID)
	if err != nil {
		return err
	}

	wantCommit, wantPush := false, false
	for _, node := range nodes {
		settings, err := a.store.GetNodeSettings(node.ID)
		if err != nil {
			continue
		}
		branch := strings.TrimSpace(settings["git_branch"])
		if branch == "" {
			continue
		}
		if canonical := gitsrc.PreferLocalRef(a.ctx, project.Path, branch); canonical != "" && canonical != branch {
			if err := a.store.SetNodeSetting(node.ID, "git_branch", canonical); err != nil {
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

	if err := applyHook(a.ctx, project.Path, exe, githooks.OnCommit, wantCommit); err != nil {
		return err
	}
	return applyHook(a.ctx, project.Path, exe, githooks.OnPush, wantPush)
}

func applyHook(ctx context.Context, repoPath, exe string, event githooks.Event, want bool) error {
	if want {
		return githooks.Install(ctx, repoPath, exe, event)
	}
	return githooks.Uninstall(ctx, repoPath, event)
}

// GetGitHookStatus reports whether the project supports git-triggered deploys
// and whether foreign hooks are present, so the UI can surface a chaining note.
func (a *App) GetGitHookStatus(projectID uint) (GitHookStatus, error) {
	if a.store == nil {
		return GitHookStatus{}, errNoStore
	}
	project, err := a.store.GetProject(projectID)
	if err != nil {
		return GitHookStatus{}, fmt.Errorf("project not found: %w", err)
	}
	if !gitsrc.IsRepo(project.Path) {
		return GitHookStatus{Supported: false}, nil
	}
	st := GitHookStatus{Supported: true}
	if _, foreign, err := githooks.Status(a.ctx, project.Path, githooks.OnCommit); err == nil {
		st.CommitForeign = foreign
	}
	if _, foreign, err := githooks.Status(a.ctx, project.Path, githooks.OnPush); err == nil {
		st.PushForeign = foreign
	}
	return st, nil
}
