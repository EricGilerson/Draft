package daemon

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"testing"

	"Draft/internal/store"
)

func TestNodeSubscribes(t *testing.T) {
	cases := []struct {
		event          string
		trigger        string
		redeployOnPull string
		want           bool
	}{
		{eventOnCommit, "on_commit", "", true},
		{eventOnCommit, "manual", "", false},
		{eventOnCommit, "on_push", "", false},
		{eventOnPush, "on_push", "", true},
		{eventOnPush, "on_commit", "", false},
		{eventOnPull, "manual", "true", true},
		{eventOnPull, "on_commit", "true", true}, // pull is orthogonal to the 3-way trigger
		{eventOnPush, "on_push", "true", true},   // push still wins for a push event
		{eventOnPull, "on_commit", "", false},
		{eventOnPull, "manual", "false", false},
		{eventOnPull, "manual", "", false},
		{"on_commit", " on_commit ", "", true}, // whitespace tolerated
		{eventOnPull, "manual", " true ", true},
	}
	for _, c := range cases {
		got := nodeSubscribes(c.event, c.trigger, c.redeployOnPull)
		if got != c.want {
			t.Errorf("nodeSubscribes(%q, %q, %q) = %v, want %v",
				c.event, c.trigger, c.redeployOnPull, got, c.want)
		}
	}
}

// daemonGitRepo builds a one-commit git repo at main and returns its path and
// the main-branch sha. Used to exercise matchNode against real repo state.
func daemonGitRepo(t *testing.T) (string, string) {
	t.Helper()
	dir := t.TempDir()
	envs := append(os.Environ(),
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.com",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.com",
	)
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = envs
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-b", "main", "-q")
	if err := os.WriteFile(dir+"/README.md", []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	out, err := exec.Command("git", "-C", dir, "rev-parse", "main").Output()
	if err != nil {
		t.Fatalf("rev-parse: %v", err)
	}
	return dir, strings.TrimSpace(string(out))
}

// TestMatchNodePullWithPayload verifies that a node with redeploy_on_pull and a
// matching branch resolves to the payload sha, while a non-matching branch or
// a node without the flag does not.
func TestMatchNodePullWithPayload(t *testing.T) {
	repo, sha := daemonGitRepo(t)
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	project, err := s.CreateProject("Repo", repo, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	srv := &Server{store: s}
	remotes := gitRemotes(context.Background(), repo)
	payload := map[string]string{"main": sha}

	// redeploy_on_pull=true, branch=main, payload touches main → match.
	gotTracked, gotSHA, ok := srv.matchNode(
		context.Background(), project, recheckRequest{Event: eventOnPull, Refs: []gitRef{{Name: "main", SHA: sha}}},
		map[string]string{"git_branch": "main", "deploy_trigger": "manual", "redeploy_on_pull": "true"},
		payload, remotes,
	)
	if !ok || gotTracked != "main" || gotSHA != sha {
		t.Fatalf("expected match (main/%s), got tracked=%q sha=%q ok=%v", sha, gotTracked, gotSHA, ok)
	}

	// redeploy_on_pull=false → no match, even with matching branch.
	_, _, ok = srv.matchNode(
		context.Background(), project, recheckRequest{Event: eventOnPull, Refs: []gitRef{{Name: "main", SHA: sha}}},
		map[string]string{"git_branch": "main", "deploy_trigger": "on_commit", "redeploy_on_pull": ""},
		payload, remotes,
	)
	if ok {
		t.Fatalf("on_pull event must not match a node without redeploy_on_pull")
	}

	// redeploy_on_pull=true but payload touches a different branch → no match.
	_, _, ok = srv.matchNode(
		context.Background(), project, recheckRequest{Event: eventOnPull, Refs: []gitRef{{Name: "other", SHA: "deadbeef"}}},
		map[string]string{"git_branch": "main", "deploy_trigger": "manual", "redeploy_on_pull": "true"},
		map[string]string{"other": "deadbeef"}, remotes,
	)
	if ok {
		t.Fatalf("on_pull must not match when the tracked branch is not in the payload")
	}

	// No pinned branch → no match.
	_, _, ok = srv.matchNode(
		context.Background(), project, recheckRequest{Event: eventOnPull, Refs: []gitRef{{Name: "main", SHA: sha}}},
		map[string]string{"deploy_trigger": "manual", "redeploy_on_pull": "true"},
		payload, remotes,
	)
	if ok {
		t.Fatalf("on_pull must not match a node with no pinned branch")
	}
}

// TestMatchNodePullStartupDerivesLocalTip verifies that a payload-less (startup)
// on_pull reconcile derives the candidate sha from the local branch tip.
func TestMatchNodePullStartupDerivesLocalTip(t *testing.T) {
	repo, sha := daemonGitRepo(t)
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	project, err := s.CreateProject("Repo", repo, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	srv := &Server{store: s}
	remotes := gitRemotes(context.Background(), repo)

	_, gotSHA, ok := srv.matchNode(
		context.Background(), project, recheckRequest{Event: eventOnPull},
		map[string]string{"git_branch": "main", "deploy_trigger": "manual", "redeploy_on_pull": "true"},
		nil, remotes,
	)
	if !ok {
		t.Fatalf("expected startup on_pull match")
	}
	if gotSHA != sha {
		t.Fatalf("startup on_pull candidate sha = %q, want %q", gotSHA, sha)
	}
}

// TestReconcileGitTriggersPullNoOpWhenSameSHA runs the full reconcile path for
// an on_pull event against a real repo where the node is already deployed at
// the current sha; it must be a no-op (no panic, no new deployment).
func TestReconcileGitTriggersPullNoOpWhenSameSHA(t *testing.T) {
	repo, sha := daemonGitRepo(t)
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
	if _, err := s.CreateDeployment(&store.Deployment{NodeID: "svc-1", Status: "running", SourceSHA: sha}); err != nil {
		t.Fatalf("seed deployment: %v", err)
	}
	srv := &Server{store: s} // engine is nil; must never reach Deploy

	srv.reconcileGitTriggers(context.Background(), recheckRequest{
		Repo:  repo,
		Event: eventOnPull,
		Refs:  []gitRef{{Name: "main", SHA: sha}},
	})

	deps, err := s.ListDeployments("svc-1")
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if len(deps) != 1 {
		t.Fatalf("expected exactly 1 deployment (no-op), got %d", len(deps))
	}
}

// TestReconcileGitTriggersPullSkipsUnrelatedBranch verifies that a pull on a
// branch the node doesn't track does not deploy even when the sha would differ.
func TestReconcileGitTriggersPullSkipsUnrelatedBranch(t *testing.T) {
	repo, _ := daemonGitRepo(t)
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
	srv := &Server{store: s}

	srv.reconcileGitTriggers(context.Background(), recheckRequest{
		Repo:  repo,
		Event: eventOnPull,
		Refs:  []gitRef{{Name: "feature", SHA: "ffffffffffffffffffffffffffffffffffffffff"}},
	})
	deps, err := s.ListDeployments("svc-1")
	if err != nil {
		t.Fatalf("list deployments: %v", err)
	}
	if len(deps) != 0 {
		t.Fatalf("expected no deployment for unrelated branch, got %d", len(deps))
	}
}

// TestFireHookPostMergeBuildsPullRequest verifies that FireHook translates a
// "post-merge" git event into an on_pull recheckRequest carrying HEAD's branch
// and sha. It stubs daemon delivery so no real daemon is needed.
func TestFireHookPostMergeBuildsPullRequest(t *testing.T) {
	repo, sha := daemonGitRepo(t)

	orig := deliverRecheck
	t.Cleanup(func() { deliverRecheck = orig })
	var got recheckRequest
	deliverRecheck = func(ctx context.Context, req recheckRequest) error {
		got = req
		return nil
	}

	if err := FireHook(context.Background(), repo, "post-merge"); err != nil {
		t.Fatalf("FireHook: %v", err)
	}
	if got.Event != eventOnPull {
		t.Fatalf("event = %q, want %q", got.Event, eventOnPull)
	}
	if len(got.Refs) != 1 {
		t.Fatalf("expected 1 ref, got %d", len(got.Refs))
	}
	if got.Refs[0].Name != "main" || got.Refs[0].SHA != sha {
		t.Fatalf("ref = %+v, want {main %s}", got.Refs[0], sha)
	}
}
