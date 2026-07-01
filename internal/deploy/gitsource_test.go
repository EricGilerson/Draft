package deploy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func gitRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@t",
			"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@t",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
		}
	}
	run("init", "-b", "main", "-q")
	run("config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("committed\n"), 0o644); err != nil {
		t.Fatalf("write app.txt: %v", err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	return dir
}

func TestPrepareGitSource_MaterializesBranch(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	repo := gitRepoWithCommit(t)

	dir, err := e.prepareGitSource(context.Background(), "node-1", repo, "main")
	if err != nil {
		t.Fatalf("prepareGitSource: %v", err)
	}
	defer os.RemoveAll(dir)

	if dir == repo {
		t.Fatalf("expected an ephemeral dir distinct from the repo, got the repo path")
	}
	data, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	if err != nil {
		t.Fatalf("read materialized app.txt: %v", err)
	}
	if string(data) != "committed\n" {
		t.Fatalf("unexpected content: %q", data)
	}
	// The archive must not carry the .git directory into the build context.
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("expected no .git in archived source")
	}
}

func TestPrepareGitSource_LeavesWorkingTreeUntouched(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	repo := gitRepoWithCommit(t)

	// Dirty the working tree with an uncommitted edit + untracked file.
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("DIRTY\n"), 0o644); err != nil {
		t.Fatalf("dirty app.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scratch.tmp"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}

	dir, err := e.prepareGitSource(context.Background(), "node-1", repo, "main")
	if err != nil {
		t.Fatalf("prepareGitSource: %v", err)
	}
	defer os.RemoveAll(dir)

	// Materialized source reflects the commit, not the dirty tree.
	data, _ := os.ReadFile(filepath.Join(dir, "app.txt"))
	if string(data) != "committed\n" {
		t.Fatalf("archive leaked uncommitted edit: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "scratch.tmp")); !os.IsNotExist(err) {
		t.Fatalf("archive included untracked file")
	}

	// Working tree edits survive untouched.
	wt, _ := os.ReadFile(filepath.Join(repo, "app.txt"))
	if string(wt) != "DIRTY\n" {
		t.Fatalf("working tree was mutated: %q", wt)
	}
	if _, err := os.Stat(filepath.Join(repo, "scratch.tmp")); err != nil {
		t.Fatalf("untracked working-tree file was removed: %v", err)
	}
}

func TestPrepareGitSource_NotARepo(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	_, err := e.prepareGitSource(context.Background(), "node-1", t.TempDir(), "main")
	if err == nil {
		t.Fatalf("expected error for non-git project directory")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareGitSource_BadBranch(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	repo := gitRepoWithCommit(t)

	_, err := e.prepareGitSource(context.Background(), "node-1", repo, "no-such-branch")
	if err == nil {
		t.Fatalf("expected error for nonexistent branch")
	}
}

func TestRebaseUnderSource(t *testing.T) {
	// Use real OS-absolute paths so filepath.IsAbs behaves as in production
	// (on Windows a path needs a volume like C:\ to count as absolute).
	project := t.TempDir()
	source := t.TempDir()
	outside := t.TempDir()

	// Relative paths are untouched — they already resolve against the source root.
	if got, err := rebaseUnderSource(project, source, "Dockerfile"); err != nil || got != "Dockerfile" {
		t.Fatalf("relative path: got %q, err %v", got, err)
	}
	if got, err := rebaseUnderSource(project, source, filepath.FromSlash("svc/Dockerfile")); err != nil || got != filepath.FromSlash("svc/Dockerfile") {
		t.Fatalf("relative subpath: got %q, err %v", got, err)
	}
	// Empty is passed through.
	if got, err := rebaseUnderSource(project, source, ""); err != nil || got != "" {
		t.Fatalf("empty path: got %q, err %v", got, err)
	}
	// Absolute path inside the project is re-anchored under source.
	absIn := filepath.Join(project, "svc", "Dockerfile")
	want := filepath.Join(source, "svc", "Dockerfile")
	if got, err := rebaseUnderSource(project, source, absIn); err != nil || got != want {
		t.Fatalf("absolute in-project: got %q want %q, err %v", got, want, err)
	}
	// Absolute path outside the project is rejected.
	absOut := filepath.Join(outside, "Dockerfile")
	if _, err := rebaseUnderSource(project, source, absOut); err == nil {
		t.Fatalf("expected error for absolute path outside project")
	}
}

// TestGitDeploy_AbsoluteDockerfileResolves reproduces the original "stuck at
// Exported…" bug: an absolute Dockerfile path (as stored by the file picker)
// must still resolve to a valid build context when deploying from a branch.
func TestGitDeploy_AbsoluteDockerfileResolves(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	repo := gitRepoWithCommit(t) // commits a Dockerfile at repo root

	absDockerfile := filepath.Join(repo, "Dockerfile")

	source, err := e.prepareGitSource(context.Background(), "node-1", repo, "main")
	if err != nil {
		t.Fatalf("prepareGitSource: %v", err)
	}
	defer os.RemoveAll(source)

	rebased, err := rebaseUnderSource(repo, source, absDockerfile)
	if err != nil {
		t.Fatalf("rebaseUnderSource: %v", err)
	}

	plan, err := resolveBuildContextPlan(source, "", rebased)
	if err != nil {
		t.Fatalf("resolveBuildContextPlan (git mode, absolute dockerfile): %v", err)
	}
	if plan.RelativeDockerfile != "Dockerfile" {
		t.Fatalf("expected RelativeDockerfile=Dockerfile, got %q", plan.RelativeDockerfile)
	}
	if plan.ContextRoot != filepath.Clean(source) {
		t.Fatalf("expected context root %q, got %q", filepath.Clean(source), plan.ContextRoot)
	}
	// The Dockerfile the plan points at must actually exist in the archive.
	if _, err := os.Stat(plan.DockerfilePath); err != nil {
		t.Fatalf("resolved Dockerfile does not exist in archive: %v", err)
	}
}
