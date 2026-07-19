package githooks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestOnPullMapsToPostMerge(t *testing.T) {
	file, err := OnPull.hookFile()
	if err != nil {
		t.Fatalf("hookFile: %v", err)
	}
	if file != "post-merge" {
		t.Fatalf("OnPull hookFile = %q, want post-merge", file)
	}
}

func TestOnPullRewriteMapsToPostRewrite(t *testing.T) {
	file, err := OnPullRewrite.hookFile()
	if err != nil {
		t.Fatalf("hookFile: %v", err)
	}
	if file != "post-rewrite" {
		t.Fatalf("OnPullRewrite hookFile = %q, want post-rewrite", file)
	}
}

func TestInstallAndUninstallPostMerge(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	if err := Install(ctx, repo, "/opt/draft/draft", OnPull); err != nil {
		t.Fatalf("Install: %v", err)
	}
	installed, foreign, err := Status(ctx, repo, OnPull)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !installed || foreign {
		t.Fatalf("expected installed && !foreign, got installed=%v foreign=%v", installed, foreign)
	}

	data, err := os.ReadFile(hookPath(t, repo, "post-merge"))
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, marker) {
		t.Fatalf("hook missing marker:\n%s", body)
	}
	if !strings.Contains(body, "--git-hook") || !strings.Contains(body, "--event post-merge") {
		t.Fatalf("hook missing post-merge invocation:\n%s", body)
	}

	if err := Uninstall(ctx, repo, OnPull); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(hookPath(t, repo, "post-merge")); !os.IsNotExist(err) {
		t.Fatalf("expected hook removed, stat err = %v", err)
	}
}

func TestPostMergeHookChainsForeign(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	foreignBody := "#!/bin/sh\necho my-merge-hook\n"
	hp := hookPath(t, repo, "post-merge")
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(hp, []byte(foreignBody), 0o755); err != nil {
		t.Fatalf("write foreign hook: %v", err)
	}

	if err := Install(ctx, repo, "/opt/draft/draft", OnPull); err != nil {
		t.Fatalf("Install: %v", err)
	}
	backup, err := os.ReadFile(hp + ".draft-orig")
	if err != nil {
		t.Fatalf("expected backup of foreign hook: %v", err)
	}
	if string(backup) != foreignBody {
		t.Fatalf("backup body = %q, want %q", backup, foreignBody)
	}
	ours, _ := os.ReadFile(hp)
	if !strings.Contains(string(ours), ".draft-orig") {
		t.Fatalf("our hook should chain to the backup:\n%s", ours)
	}

	installed, foreign, err := Status(ctx, repo, OnPull)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !installed || !foreign {
		t.Fatalf("expected installed && foreign, got installed=%v foreign=%v", installed, foreign)
	}

	if err := Uninstall(ctx, repo, OnPull); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	restored, err := os.ReadFile(hp)
	if err != nil {
		t.Fatalf("expected foreign hook restored: %v", err)
	}
	if string(restored) != foreignBody {
		t.Fatalf("restored body = %q, want %q", restored, foreignBody)
	}
}

// TestPostMergeHookFiresViaGitMerge installs a post-merge hook pointing at a
// stub "binary" that records its args, then performs a real fast-forward--free
// merge and verifies git ran our hook with --git-hook --event post-merge.
func TestPostMergeHookFiresViaGitMerge(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := newRepo(t)
	ctx := context.Background()

	// Stub stands in for the Draft binary.
	stubDir := t.TempDir()
	logFile := filepath.Join(stubDir, "invocation.log")
	stub := filepath.Join(stubDir, "draft-stub")
	stubBody := "#!/bin/sh\necho \"args: $*\" >> " + shellQuote(logFile) + "\n"
	if err := os.WriteFile(stub, []byte(stubBody), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	if err := Install(ctx, repo, stub, OnPull); err != nil {
		t.Fatalf("Install: %v", err)
	}

	gitEnv := append(os.Environ(),
		"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.com",
		"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.com",
	)
	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = gitEnv
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}

	// Seed an initial commit on main so the branch exists, then build a
	// divergent history so the merge produces a real merge commit and fires
	// post-merge (a true merge guarantees it across git versions).
	if err := os.WriteFile(filepath.Join(repo, "base.txt"), []byte("base\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "base commit")

	git("checkout", "-q", "-b", "feature")
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("feature\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "feature commit")
	git("checkout", "-q", "main")
	if err := os.WriteFile(filepath.Join(repo, "g.txt"), []byte("main\n"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "main commit")
	git("merge", "--no-ff", "-q", "-m", "merge feature", "feature")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("post-merge hook did not run (no log): %v", err)
	}
	got := string(data)
	// The hook script normalizes backslash-separated Windows paths to forward
	// slashes before embedding them (see toHookPath) so cygwin/MSYS2 never
	// misparses them; compare against that same normalized form.
	if !strings.Contains(got, "--git-hook") ||
		!strings.Contains(got, "--repo "+toHookPath(repo)) ||
		!strings.Contains(got, "--event post-merge") {
		t.Fatalf("post-merge hook invoked with unexpected args: %q", got)
	}
}
