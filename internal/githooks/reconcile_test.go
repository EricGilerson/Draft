package githooks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"Draft/internal/store"
)

func runGitTest(t *testing.T, dir string, args ...string) string {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
	return string(out)
}

func newGitRepo(t *testing.T) string {
	t.Helper()
	repo := t.TempDir()
	runGitTest(t, repo, "init", "-b", "main", "-q")
	runGitTest(t, repo, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(repo, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGitTest(t, repo, "add", ".")
	runGitTest(t, repo, "commit", "-q", "-m", "initial commit")
	return repo
}

func TestReconcileAllProjectsInstallsHooksAndNormalizesBranch(t *testing.T) {
	repo := newGitRepo(t)
	remote := t.TempDir()
	runGitTest(t, remote, "init", "--bare", "-b", "main", "-q")
	runGitTest(t, repo, "remote", "add", "origin", remote)
	runGitTest(t, repo, "push", "-q", "origin", "main")

	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject("Repo", repo, "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	env, err := s.GetDefaultEnvironment(project.ID)
	if err != nil {
		t.Fatalf("get default environment: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "svc-1", ProjectID: project.ID, EnvironmentID: env.ID, Label: "svc"}); err != nil {
		t.Fatalf("create node: %v", err)
	}
	if err := s.SetNodeSetting("svc-1", "git_branch", "origin/main"); err != nil {
		t.Fatalf("set git_branch: %v", err)
	}
	if err := s.SetNodeSetting("svc-1", "deploy_trigger", "on_commit"); err != nil {
		t.Fatalf("set deploy_trigger: %v", err)
	}

	if err := ReconcileAllProjects(context.Background(), s); err != nil {
		t.Fatalf("ReconcileAllProjects: %v", err)
	}

	hookPath := filepath.Join(repo, ".git", "hooks", "post-commit")
	data, err := os.ReadFile(hookPath)
	if err != nil {
		t.Fatalf("read post-commit hook: %v", err)
	}
	if !strings.Contains(string(data), "--git-hook") {
		t.Fatalf("expected Draft hook to be installed, got:\n%s", data)
	}

	branch, err := s.GetNodeSetting("svc-1", "git_branch")
	if err != nil {
		t.Fatalf("get git_branch: %v", err)
	}
	if branch != "main" {
		t.Fatalf("git_branch = %q, want main", branch)
	}
}

func TestReconcileRepoHooks_ReferenceCountsAcrossProjects(t *testing.T) {
	repo := newGitRepo(t)

	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	projectA, err := s.CreateProject("Repo-A", repo, "")
	if err != nil {
		t.Fatalf("create project A: %v", err)
	}
	projectB, err := s.CreateProject("Repo-B", repo, "")
	if err != nil {
		t.Fatalf("create project B: %v", err)
	}
	envA, err := s.GetDefaultEnvironment(projectA.ID)
	if err != nil {
		t.Fatalf("get default environment A: %v", err)
	}
	envB, err := s.GetDefaultEnvironment(projectB.ID)
	if err != nil {
		t.Fatalf("get default environment B: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "svc-a", ProjectID: projectA.ID, EnvironmentID: envA.ID, Label: "svc-a"}); err != nil {
		t.Fatalf("create node A: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "svc-b", ProjectID: projectB.ID, EnvironmentID: envB.ID, Label: "svc-b"}); err != nil {
		t.Fatalf("create node B: %v", err)
	}
	for _, nodeID := range []string{"svc-a", "svc-b"} {
		if err := s.SetNodeSetting(nodeID, "git_branch", "main"); err != nil {
			t.Fatalf("set git_branch %s: %v", nodeID, err)
		}
		if err := s.SetNodeSetting(nodeID, "deploy_trigger", "on_commit"); err != nil {
			t.Fatalf("set deploy_trigger %s: %v", nodeID, err)
		}
		if _, err := s.ResolveGitRepoRoot(context.Background(), nodeID, map[string]uint{"svc-a": projectA.ID, "svc-b": projectB.ID}[nodeID]); err != nil {
			t.Fatalf("ResolveGitRepoRoot %s: %v", nodeID, err)
		}
	}

	if err := ReconcileAllHooks(context.Background(), s); err != nil {
		t.Fatalf("ReconcileAllHooks: %v", err)
	}
	hookPath := filepath.Join(repo, ".git", "hooks", "post-commit")
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("expected shared post-commit hook installed: %v", err)
	}

	if err := SetDeployTrigger(context.Background(), s, "svc-a", projectA.ID, "manual"); err != nil {
		t.Fatalf("SetDeployTrigger svc-a manual: %v", err)
	}
	if _, err := os.Stat(hookPath); err != nil {
		t.Fatalf("hook should stay installed while project B still needs it: %v", err)
	}

	if err := SetDeployTrigger(context.Background(), s, "svc-b", projectB.ID, "manual"); err != nil {
		t.Fatalf("SetDeployTrigger svc-b manual: %v", err)
	}
	if _, err := os.Stat(hookPath); !os.IsNotExist(err) {
		t.Fatalf("expected hook removed after last subscriber disabled it, stat err = %v", err)
	}
}
