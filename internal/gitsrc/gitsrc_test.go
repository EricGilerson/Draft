package gitsrc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

// runGit runs git in dir, failing the test on error.
func runGit(t *testing.T, dir string, args ...string) string {
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

// newTestRepo creates a repo with an initial commit on main containing
// README.md, then returns the repo dir.
func newTestRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	runGit(t, dir, "init", "-b", "main", "-q")
	// Keep line endings byte-exact regardless of the host's global git config
	// so archived content can be compared precisely.
	runGit(t, dir, "config", "core.autocrlf", "false")
	writeFile(t, filepath.Join(dir, "README.md"), "hello\n")
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "initial commit")
	return dir
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write %s: %v", path, err)
	}
}

func TestIsRepo(t *testing.T) {
	repo := newTestRepo(t)
	if !IsRepo(repo) {
		t.Fatalf("expected %s to be detected as a repo", repo)
	}

	notRepo := t.TempDir()
	if IsRepo(notRepo) {
		t.Fatalf("expected %s to NOT be detected as a repo", notRepo)
	}
}

func TestListBranches(t *testing.T) {
	repo := newTestRepo(t)
	runGit(t, repo, "branch", "feature/x")
	runGit(t, repo, "branch", "feature/y")

	branches, err := ListBranches(context.Background(), repo)
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}

	want := map[string]bool{"main": false, "feature/x": false, "feature/y": false}
	for _, b := range branches {
		if strings.HasSuffix(b, "/HEAD") {
			t.Fatalf("unexpected HEAD entry in branch list: %v", branches)
		}
		if _, ok := want[b]; ok {
			want[b] = true
		}
	}
	for name, found := range want {
		if !found {
			t.Fatalf("expected branch %q in %v", name, branches)
		}
	}
}

func TestListBranches_NotARepo(t *testing.T) {
	if _, err := ListBranches(context.Background(), t.TempDir()); err != ErrNotRepo {
		t.Fatalf("expected ErrNotRepo, got %v", err)
	}
}

func TestListBranches_IncludesRemotes(t *testing.T) {
	// Bare "remote" repo.
	remote := t.TempDir()
	runGit(t, remote, "init", "--bare", "-b", "main", "-q")

	repo := newTestRepo(t)
	runGit(t, repo, "remote", "add", "origin", remote)
	runGit(t, repo, "push", "-q", "origin", "main")

	branches, err := ListBranches(context.Background(), repo)
	if err != nil {
		t.Fatalf("ListBranches: %v", err)
	}
	found := false
	for _, b := range branches {
		if b == "origin/main" {
			found = true
		}
	}
	if !found {
		t.Fatalf("expected origin/main in %v", branches)
	}
}

func TestVerifyRef(t *testing.T) {
	repo := newTestRepo(t)
	runGit(t, repo, "branch", "feature/x")

	if err := VerifyRef(context.Background(), repo, "feature/x"); err != nil {
		t.Fatalf("VerifyRef(feature/x): %v", err)
	}
	if err := VerifyRef(context.Background(), repo, "main"); err != nil {
		t.Fatalf("VerifyRef(main): %v", err)
	}
	if err := VerifyRef(context.Background(), repo, "does-not-exist"); err == nil {
		t.Fatalf("expected error for nonexistent ref")
	}
}

func TestArchiveToDir_ExtractsCommittedFiles(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "src", "app.js"), "console.log('v1')\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "add app.js")
	runGit(t, repo, "branch", "feature/x")

	dest := t.TempDir()
	if err := ArchiveToDir(context.Background(), repo, "feature/x", dest); err != nil {
		t.Fatalf("ArchiveToDir: %v", err)
	}

	readme, err := os.ReadFile(filepath.Join(dest, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if string(readme) != "hello\n" {
		t.Fatalf("unexpected README.md content: %q", readme)
	}

	app, err := os.ReadFile(filepath.Join(dest, "src", "app.js"))
	if err != nil {
		t.Fatalf("read src/app.js: %v", err)
	}
	if string(app) != "console.log('v1')\n" {
		t.Fatalf("unexpected app.js content: %q", app)
	}
}

func TestArchiveToDir_DifferentBranchesDifferentContent(t *testing.T) {
	repo := newTestRepo(t)
	runGit(t, repo, "checkout", "-q", "-b", "feature/x")
	writeFile(t, filepath.Join(repo, "only-on-feature.txt"), "feature content\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "feature commit")
	runGit(t, repo, "checkout", "-q", "main")

	mainDest := t.TempDir()
	if err := ArchiveToDir(context.Background(), repo, "main", mainDest); err != nil {
		t.Fatalf("ArchiveToDir(main): %v", err)
	}
	if _, err := os.Stat(filepath.Join(mainDest, "only-on-feature.txt")); !os.IsNotExist(err) {
		t.Fatalf("expected only-on-feature.txt to be absent from main archive")
	}

	featureDest := t.TempDir()
	if err := ArchiveToDir(context.Background(), repo, "feature/x", featureDest); err != nil {
		t.Fatalf("ArchiveToDir(feature/x): %v", err)
	}
	if _, err := os.Stat(filepath.Join(featureDest, "only-on-feature.txt")); err != nil {
		t.Fatalf("expected only-on-feature.txt in feature archive: %v", err)
	}
}

// TestArchiveToDir_IgnoresUncommittedChanges is the core safety guarantee:
// archiving must never reflect (or be affected by) a dirty working tree,
// and must never modify that working tree either.
func TestArchiveToDir_IgnoresUncommittedChanges(t *testing.T) {
	repo := newTestRepo(t)

	// Dirty the working tree: modify a tracked file and add an untracked one,
	// without committing.
	writeFile(t, filepath.Join(repo, "README.md"), "DIRTY UNCOMMITTED CHANGE\n")
	writeFile(t, filepath.Join(repo, "untracked.txt"), "should never be archived\n")

	statusBefore := runGit(t, repo, "status", "--porcelain")

	dest := t.TempDir()
	if err := ArchiveToDir(context.Background(), repo, "main", dest); err != nil {
		t.Fatalf("ArchiveToDir: %v", err)
	}

	// The archive must reflect committed content, not the dirty working tree.
	readme, err := os.ReadFile(filepath.Join(dest, "README.md"))
	if err != nil {
		t.Fatalf("read README.md: %v", err)
	}
	if string(readme) != "hello\n" {
		t.Fatalf("archive leaked uncommitted change into README.md: %q", readme)
	}
	if _, err := os.Stat(filepath.Join(dest, "untracked.txt")); !os.IsNotExist(err) {
		t.Fatalf("archive included untracked file that was never committed")
	}

	// The working tree itself must be untouched by the archive operation.
	statusAfter := runGit(t, repo, "status", "--porcelain")
	if statusBefore != statusAfter {
		t.Fatalf("working tree status changed after archive:\nbefore: %q\nafter:  %q", statusBefore, statusAfter)
	}
	dirtyReadme, err := os.ReadFile(filepath.Join(repo, "README.md"))
	if err != nil {
		t.Fatalf("read working tree README.md: %v", err)
	}
	if string(dirtyReadme) != "DIRTY UNCOMMITTED CHANGE\n" {
		t.Fatalf("working tree README.md was mutated by archive: %q", dirtyReadme)
	}
}

func TestArchiveToDir_InvalidRef(t *testing.T) {
	repo := newTestRepo(t)
	dest := t.TempDir()
	err := ArchiveToDir(context.Background(), repo, "no-such-branch", dest)
	if err == nil {
		t.Fatalf("expected error for nonexistent branch")
	}
	entries, _ := os.ReadDir(dest)
	if len(entries) != 0 {
		t.Fatalf("expected destination to remain empty on failure, found %d entries", len(entries))
	}
}

func TestArchiveToDir_NotARepo(t *testing.T) {
	notRepo := t.TempDir()
	dest := t.TempDir()
	if err := ArchiveToDir(context.Background(), notRepo, "main", dest); err != ErrNotRepo {
		t.Fatalf("expected ErrNotRepo, got %v", err)
	}
}

func TestArchiveToDir_NestedDirectories(t *testing.T) {
	repo := newTestRepo(t)
	writeFile(t, filepath.Join(repo, "a", "b", "c", "deep.txt"), "deep\n")
	runGit(t, repo, "add", ".")
	runGit(t, repo, "commit", "-q", "-m", "add nested dirs")

	dest := t.TempDir()
	if err := ArchiveToDir(context.Background(), repo, "main", dest); err != nil {
		t.Fatalf("ArchiveToDir: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "a", "b", "c", "deep.txt"))
	if err != nil {
		t.Fatalf("read nested file: %v", err)
	}
	if string(data) != "deep\n" {
		t.Fatalf("unexpected nested file content: %q", data)
	}
}
