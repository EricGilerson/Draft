package githooks

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Draft/internal/store"
)

// pullReconcileRepo builds a git repo with an initial commit and an "origin"
// remote (so branch normalization mirrors the real setup), returns the repo
// path and its main-branch sha.
func pullReconcileRepo(t *testing.T) (string, string) {
	t.Helper()
	repo := newGitRepo(t)
	remote := t.TempDir()
	runGitTest(t, remote, "init", "--bare", "-b", "main", "-q")
	runGitTest(t, repo, "remote", "add", "origin", remote)
	runGitTest(t, repo, "push", "-q", "origin", "main")
	sha := strings.TrimSpace(runGitTest(t, repo, "rev-parse", "main"))
	return repo, sha
}

func TestSetRedeployOnPullInstallsPostMerge(t *testing.T) {
	repo, _ := pullReconcileRepo(t)
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject("Repo", repo, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "svc-1", ProjectID: project.ID, Label: "svc"}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := s.SetNodeSetting("svc-1", "git_branch", "main"); err != nil {
		t.Fatalf("set git_branch: %v", err)
	}

	// Manual trigger + redeploy-on-pull: the 3-way trigger installs nothing,
	// but the pull toggle must install post-merge.
	if err := SetDeployTrigger(context.Background(), s, "svc-1", project.ID, "manual"); err != nil {
		t.Fatalf("SetDeployTrigger: %v", err)
	}
	if err := SetRedeployOnPull(context.Background(), s, "svc-1", project.ID, true); err != nil {
		t.Fatalf("SetRedeployOnPull: %v", err)
	}

	hp := filepath.Join(repo, ".git", "hooks", "post-merge")
	data, err := os.ReadFile(hp)
	if err != nil {
		t.Fatalf("expected post-merge hook installed: %v", err)
	}
	if !strings.Contains(string(data), "--git-hook") {
		t.Fatalf("post-merge hook missing invocation:\n%s", data)
	}
	// The 3-way manual trigger must not have installed commit/push hooks.
	for _, file := range []string{"post-commit", "pre-push"} {
		if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", file)); !os.IsNotExist(err) {
			t.Fatalf("%s should not be installed for manual trigger, stat err = %v", file, err)
		}
	}

	// Toggling off uninstalls post-merge.
	if err := SetRedeployOnPull(context.Background(), s, "svc-1", project.ID, false); err != nil {
		t.Fatalf("SetRedeployOnPull(false): %v", err)
	}
	if _, err := os.Stat(hp); !os.IsNotExist(err) {
		t.Fatalf("expected post-merge removed, stat err = %v", err)
	}
}

// A manual node with redeploy-on-pull still installs post-merge — the two
// settings are independent.
func TestRedeployOnPullIndependentOfManualTrigger(t *testing.T) {
	repo, _ := pullReconcileRepo(t)
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject("Repo", repo, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "svc-1", ProjectID: project.ID, Label: "svc"}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := s.SetNodeSetting("svc-1", "git_branch", "main"); err != nil {
		t.Fatalf("set git_branch: %v", err)
	}
	if err := s.SetNodeSetting("svc-1", "deploy_trigger", "manual"); err != nil {
		t.Fatalf("set deploy_trigger: %v", err)
	}
	if err := s.SetNodeSetting("svc-1", "redeploy_on_pull", "true"); err != nil {
		t.Fatalf("set redeploy_on_pull: %v", err)
	}

	if err := ReconcileProjectHooks(context.Background(), s, project.ID); err != nil {
		t.Fatalf("ReconcileProjectHooks: %v", err)
	}
	hp := filepath.Join(repo, ".git", "hooks", "post-merge")
	if _, err := os.Stat(hp); err != nil {
		t.Fatalf("manual + redeploy_on_pull should install post-merge, stat err = %v", err)
	}
}

// redeploy_on_pull without a pinned branch must not install the hook.
func TestRedeployOnPullWithoutBranchDoesNotInstall(t *testing.T) {
	repo, _ := pullReconcileRepo(t)
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject("Repo", repo, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "svc-1", ProjectID: project.ID, Label: "svc"}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := s.SetNodeSetting("svc-1", "deploy_trigger", "manual"); err != nil {
		t.Fatalf("set deploy_trigger: %v", err)
	}
	if err := s.SetNodeSetting("svc-1", "redeploy_on_pull", "true"); err != nil {
		t.Fatalf("set redeploy_on_pull: %v", err)
	}
	if err := ReconcileProjectHooks(context.Background(), s, project.ID); err != nil {
		t.Fatalf("ReconcileProjectHooks: %v", err)
	}
	if _, err := os.Stat(filepath.Join(repo, ".git", "hooks", "post-merge")); !os.IsNotExist(err) {
		t.Fatalf("post-merge should not be installed without a pinned branch, stat err = %v", err)
	}
}

// PullForeign is reported when a non-Draft post-merge hook exists.
func TestStatusForProjectReportsPullForeign(t *testing.T) {
	repo, _ := pullReconcileRepo(t)
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject("Repo", repo, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "svc-1", ProjectID: project.ID, Label: "svc"}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := s.SetNodeSetting("svc-1", "git_branch", "main"); err != nil {
		t.Fatalf("set git_branch: %v", err)
	}
	if err := s.SetNodeSetting("svc-1", "redeploy_on_pull", "true"); err != nil {
		t.Fatalf("set redeploy_on_pull: %v", err)
	}

	// No foreign hook yet.
	st, err := StatusForProject(context.Background(), s, project.ID)
	if err != nil {
		t.Fatalf("StatusForProject: %v", err)
	}
	if !st.Supported || st.PullForeign {
		t.Fatalf("expected supported && !pullForeign, got %+v", st)
	}

	// Drop a foreign post-merge hook and re-check.
	hp := filepath.Join(repo, ".git", "hooks", "post-merge")
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(hp, []byte("#!/bin/sh\necho other\n"), 0o755); err != nil {
		t.Fatalf("write foreign hook: %v", err)
	}
	st, err = StatusForProject(context.Background(), s, project.ID)
	if err != nil {
		t.Fatalf("StatusForProject: %v", err)
	}
	if !st.PullForeign {
		t.Fatalf("expected pullForeign=true, got %+v", st)
	}
}
