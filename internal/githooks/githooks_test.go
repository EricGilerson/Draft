package githooks

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newRepo(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	cmd := exec.Command("git", "-C", dir, "init", "-b", "main", "-q")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v\n%s", err, out)
	}
	return dir
}

func hookPath(t *testing.T, repo, file string) string {
	t.Helper()
	return filepath.Join(repo, ".git", "hooks", file)
}

func TestInstallAndUninstall(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	if err := Install(ctx, repo, "/opt/draft/draft", OnCommit); err != nil {
		t.Fatalf("Install: %v", err)
	}

	installed, foreign, err := Status(ctx, repo, OnCommit)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !installed || foreign {
		t.Fatalf("expected installed && !foreign, got installed=%v foreign=%v", installed, foreign)
	}

	data, err := os.ReadFile(hookPath(t, repo, "post-commit"))
	if err != nil {
		t.Fatalf("read hook: %v", err)
	}
	body := string(data)
	if !strings.Contains(body, marker) {
		t.Fatalf("hook missing marker:\n%s", body)
	}
	if !strings.Contains(body, "--git-hook") || !strings.Contains(body, "--event post-commit") {
		t.Fatalf("hook missing invocation:\n%s", body)
	}
	if !strings.Contains(body, "/opt/draft/draft") {
		t.Fatalf("hook missing exe path:\n%s", body)
	}

	if err := Uninstall(ctx, repo, OnCommit); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(hookPath(t, repo, "post-commit")); !os.IsNotExist(err) {
		t.Fatalf("expected hook removed, stat err = %v", err)
	}
}

func TestInstallChainsForeignHook(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	// A pre-existing, foreign pre-push hook.
	foreignBody := "#!/bin/sh\necho husky\n"
	hp := hookPath(t, repo, "pre-push")
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(hp, []byte(foreignBody), 0o755); err != nil {
		t.Fatalf("write foreign hook: %v", err)
	}

	if err := Install(ctx, repo, "/opt/draft/draft", OnPush); err != nil {
		t.Fatalf("Install: %v", err)
	}

	// Foreign hook preserved and chained.
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

	installed, foreign, err := Status(ctx, repo, OnPush)
	if err != nil {
		t.Fatalf("Status: %v", err)
	}
	if !installed || !foreign {
		t.Fatalf("expected installed && foreign, got installed=%v foreign=%v", installed, foreign)
	}

	// Uninstall restores the original foreign hook.
	if err := Uninstall(ctx, repo, OnPush); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	restored, err := os.ReadFile(hp)
	if err != nil {
		t.Fatalf("expected foreign hook restored: %v", err)
	}
	if string(restored) != foreignBody {
		t.Fatalf("restored body = %q, want %q", restored, foreignBody)
	}
	if _, err := os.Stat(hp + ".draft-orig"); !os.IsNotExist(err) {
		t.Fatalf("expected backup removed after restore, stat err = %v", err)
	}
}

func TestUninstallLeavesForeignHookUntouched(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()

	foreignBody := "#!/bin/sh\necho other\n"
	hp := hookPath(t, repo, "post-commit")
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(hp, []byte(foreignBody), 0o755); err != nil {
		t.Fatalf("write: %v", err)
	}

	// Uninstalling when we never installed must not delete a foreign hook.
	if err := Uninstall(ctx, repo, OnCommit); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	data, err := os.ReadFile(hp)
	if err != nil || string(data) != foreignBody {
		t.Fatalf("foreign hook should be untouched, got %q err %v", data, err)
	}
}

// TestHookFiresViaGit installs a post-commit hook pointing at a stub "binary"
// (a shell script that records how it was invoked), then makes a real commit and
// verifies git ran our hook with the expected --git-hook --repo/--event args.
// This exercises the actual shell script git executes, not just its bytes.
func TestHookFiresViaGit(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := newRepo(t)
	ctx := context.Background()

	// Stub stands in for the Draft binary: append its args to a log next to it.
	stubDir := t.TempDir()
	logFile := filepath.Join(stubDir, "invocation.log")
	stub := filepath.Join(stubDir, "draft-stub")
	stubBody := "#!/bin/sh\necho \"args: $*\" >> " + shellQuote(logFile) + "\n"
	if err := os.WriteFile(stub, []byte(stubBody), 0o755); err != nil {
		t.Fatalf("write stub: %v", err)
	}

	if err := Install(ctx, repo, stub, OnCommit); err != nil {
		t.Fatalf("Install: %v", err)
	}

	git := func(args ...string) {
		cmd := exec.Command("git", append([]string{"-C", repo}, args...)...)
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=T", "GIT_AUTHOR_EMAIL=t@e.com",
			"GIT_COMMITTER_NAME=T", "GIT_COMMITTER_EMAIL=t@e.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "f.txt"), []byte("x"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}
	git("add", ".")
	git("commit", "-q", "-m", "test commit")

	data, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("hook did not run (no log): %v", err)
	}
	got := string(data)
	if !strings.Contains(got, "--git-hook") ||
		!strings.Contains(got, "--repo "+repo) ||
		!strings.Contains(got, "--event post-commit") {
		t.Fatalf("hook invoked with unexpected args: %q", got)
	}
}

// TestToHookPathPlatformBehavior asserts the platform contract of toHookPath:
// backslashes are rewritten to forward slashes on Windows and preserved
// everywhere else (where backslash is a legal filename character).
func TestToHookPathPlatformBehavior(t *testing.T) {
	in := `C:\Users\eric\AppData\Local\Draft\draft.exe`
	out := toHookPath(in)
	switch runtime.GOOS {
	case "windows":
		if strings.Contains(out, `\`) {
			t.Fatalf("windows: expected backslashes normalized, got %q", out)
		}
		if !strings.Contains(out, "C:/Users/eric/AppData/Local/Draft/draft.exe") {
			t.Fatalf("windows: unexpected normalized path %q", out)
		}
	default:
		if out != in {
			t.Fatalf("non-windows: backslash must be preserved, got %q want %q", out, in)
		}
	}
}

// TestRenderScriptNormalizesWindowsPaths checks that when the host is Windows,
// backslash-style exe/repo/orig paths are embedded with forward slashes so
// cygwin/MSYS2 never has to interpret backslashes inside the quoted strings.
// On non-Windows hosts the path is preserved verbatim.
func TestRenderScriptNormalizesWindowsPaths(t *testing.T) {
	exe := `/opt/draft/draft`
	repo := `/home/eric/proj`
	orig := `/home/eric/proj/.git/hooks/post-commit.draft-orig`
	if runtime.GOOS == "windows" {
		exe = `C:\Program Files\Draft\draft.exe`
		repo = `C:\Users\eric\proj`
		orig = `C:\Users\eric\proj\.git\hooks\post-commit.draft-orig`
	}
	body := renderScript(OnCommit, exe, repo, orig)
	if !strings.Contains(body, "#!/bin/sh") {
		t.Fatalf("missing shebang:\n%s", body)
	}
	switch runtime.GOOS {
	case "windows":
		if strings.Contains(body, `\`) {
			t.Fatalf("windows: rendered hook must not contain backslashes:\n%s", body)
		}
		if !strings.Contains(body, "C:/Program Files/Draft/draft.exe") {
			t.Fatalf("windows: exe path not normalized/embedded:\n%s", body)
		}
	default:
		if !strings.Contains(body, exe) {
			t.Fatalf("unix: exe path not embedded verbatim:\n%s", body)
		}
	}
}
