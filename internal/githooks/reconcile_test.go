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
	if _, err := s.CreateNode(&store.CanvasNode{ID: "svc-1", ProjectID: project.ID, Label: "svc"}); err != nil {
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
