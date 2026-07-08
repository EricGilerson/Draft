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
	// The hook script normalizes backslash-separated Windows paths to forward
	// slashes before embedding them (see toHookPath) so cygwin/MSYS2 never
	// misparses them; compare against that same normalized form.
	if !strings.Contains(got, "--git-hook") ||
		!strings.Contains(got, "--repo "+toHookPath(repo)) ||
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

// runGitInTest runs git in dir and fails the test on error.
func runGitInTest(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v\n%s", args, err, out)
	}
}

// TestInstallHidesWorktreeHooksViaExclude simulates a husky-style layout where
// core.hooksPath points into the worktree. Draft's managed hook and its
// .draft-orig backup would otherwise appear as untracked files; the install
// must add tagged entries to .git/info/exclude so they stay hidden, and
// uninstall must remove them.
func TestInstallHidesWorktreeHooksViaExclude(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := newRepo(t)
	ctx := context.Background()

	hooksDir := filepath.Join(repo, ".husky", "_")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir hooks: %v", err)
	}
	runGitInTest(t, repo, "config", "core.hooksPath", ".husky/_")

	if err := Install(ctx, repo, "/opt/draft/draft", OnCommit); err != nil {
		t.Fatalf("Install: %v", err)
	}

	hp := filepath.Join(hooksDir, "post-commit")
	if _, err := os.Stat(hp); err != nil {
		t.Fatalf("expected hook installed in worktree hooksPath: %v", err)
	}

	// The hook file must not surface in `git status` (excluded).
	out, err := exec.Command("git", "-C", repo, "status", "--porcelain").Output()
	if err != nil {
		t.Fatalf("git status: %v", err)
	}
	if strings.Contains(string(out), "post-commit") {
		t.Fatalf("hook should be excluded from git status, got:\n%s", out)
	}

	exclude, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude: %v", err)
	}
	if !strings.Contains(string(exclude), marker) || !strings.Contains(string(exclude), ".husky/_/post-commit") {
		t.Fatalf("exclude missing draft block:\n%s", exclude)
	}

	if err := Uninstall(ctx, repo, OnCommit); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	if _, err := os.Stat(hp); !os.IsNotExist(err) {
		t.Fatalf("expected hook removed, stat err = %v", err)
	}
	exclude2, err := os.ReadFile(filepath.Join(repo, ".git", "info", "exclude"))
	if err != nil {
		t.Fatalf("read exclude after uninstall: %v", err)
	}
	if strings.Contains(string(exclude2), marker) {
		t.Fatalf("exclude should not retain draft block after uninstall:\n%s", exclude2)
	}
}

// TestDefaultHooksDirIsNeverExcluded ensures that when hooks live under .git
// (the default), Draft does NOT write anything to .git/info/exclude — git
// already hides .git/ from status, so an exclude entry would be pointless noise
// in the user's per-clone ignore file.
func TestDefaultHooksDirIsNeverExcluded(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := newRepo(t)
	ctx := context.Background()

	excludePath := filepath.Join(repo, ".git", "info", "exclude")
	before, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude before: %v", err)
	}

	if err := Install(ctx, repo, "/opt/draft/draft", OnCommit); err != nil {
		t.Fatalf("Install: %v", err)
	}
	after, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude after: %v", err)
	}
	if string(after) != string(before) {
		t.Fatalf("exclude must be untouched for default .git/hooks, got:\n%s", after)
	}

	if err := Uninstall(ctx, repo, OnCommit); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	final, err := os.ReadFile(excludePath)
	if err != nil {
		t.Fatalf("read exclude final: %v", err)
	}
	if string(final) != string(before) {
		t.Fatalf("exclude must be untouched after uninstall, got:\n%s", final)
	}
}

// TestInstallRefreshesStaleForeignHookBackup covers the husky-re-init case: a
// foreign tool rewrites the canonical slot after Draft preserved it. The next
// install must refresh the .draft-orig backup to the foreign tool's current
// hook, and uninstall must restore that current hook — never a stale copy.
func TestInstallRefreshesStaleForeignHookBackup(t *testing.T) {
	if _, err := exec.LookPath("git"); err != nil {
		t.Skip("git not available")
	}
	repo := newRepo(t)
	ctx := context.Background()

	hooksDir := filepath.Join(repo, ".husky", "_")
	if err := os.MkdirAll(hooksDir, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	runGitInTest(t, repo, "config", "core.hooksPath", ".husky/_")

	hp := filepath.Join(hooksDir, "post-commit")
	orig := hp + ".draft-orig"

	v1 := "#!/bin/sh\necho husky-v1\n"
	if err := os.WriteFile(hp, []byte(v1), 0o755); err != nil {
		t.Fatalf("write foreign v1: %v", err)
	}
	if err := Install(ctx, repo, "/opt/draft/draft", OnCommit); err != nil {
		t.Fatalf("Install 1: %v", err)
	}
	if b, _ := os.ReadFile(orig); string(b) != v1 {
		t.Fatalf("backup should be v1, got %q", b)
	}

	// Foreign tool rewrites the slot (e.g. husky re-init).
	v2 := "#!/bin/sh\necho husky-v2\n"
	if err := os.WriteFile(hp, []byte(v2), 0o755); err != nil {
		t.Fatalf("overwrite foreign v2: %v", err)
	}
	if err := Install(ctx, repo, "/opt/draft/draft", OnCommit); err != nil {
		t.Fatalf("Install 2: %v", err)
	}
	if b, _ := os.ReadFile(orig); string(b) != v2 {
		t.Fatalf("backup should refresh to v2, got %q", b)
	}

	if err := Uninstall(ctx, repo, OnCommit); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	r, err := os.ReadFile(hp)
	if err != nil {
		t.Fatalf("expected foreign hook restored: %v", err)
	}
	if string(r) != v2 {
		t.Fatalf("restored hook = %q, want v2 %q", r, v2)
	}
	if _, err := os.Stat(orig); !os.IsNotExist(err) {
		t.Fatalf("expected backup removed after restore, stat err = %v", err)
	}
}

// TestUninstallRestoresOrphanedBackup covers the case where Draft's managed hook
// is removed out from under us (by a foreign tool or the user) while the
// .draft-orig backup remains. Uninstall must restore the backup to the slot so
// the user's tooling keeps running.
func TestUninstallRestoresOrphanedBackup(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	hp := hookPath(t, repo, "post-commit")
	orig := hp + ".draft-orig"

	foreign := "#!/bin/sh\necho mine\n"
	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	if err := os.WriteFile(hp, []byte(foreign), 0o755); err != nil {
		t.Fatalf("write foreign: %v", err)
	}
	if err := Install(ctx, repo, "/opt/draft/draft", OnCommit); err != nil {
		t.Fatalf("Install: %v", err)
	}
	// Simulate our managed hook being deleted while the backup is orphaned.
	if err := os.Remove(hp); err != nil {
		t.Fatalf("remove managed hook: %v", err)
	}
	if err := Uninstall(ctx, repo, OnCommit); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	r, err := os.ReadFile(hp)
	if err != nil {
		t.Fatalf("expected foreign hook restored from orphan: %v", err)
	}
	if string(r) != foreign {
		t.Fatalf("restored = %q, want %q", r, foreign)
	}
	if _, err := os.Stat(orig); !os.IsNotExist(err) {
		t.Fatalf("expected orphan removed after restore, stat err = %v", err)
	}
}

// TestUninstallCleansStaleBackupWhenForeignReclaimedSlot covers the case where a
// foreign tool has rewritten the canonical slot (no marker) while a stale
// .draft-orig from a prior Draft install remains. Uninstall must discard the
// stale backup and leave the foreign tool's current hook in place.
func TestUninstallCleansStaleBackupWhenForeignReclaimedSlot(t *testing.T) {
	repo := newRepo(t)
	ctx := context.Background()
	hp := hookPath(t, repo, "post-commit")
	orig := hp + ".draft-orig"

	if err := os.MkdirAll(filepath.Dir(hp), 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	current := "#!/bin/sh\necho foreign-current\n"
	if err := os.WriteFile(hp, []byte(current), 0o755); err != nil {
		t.Fatalf("write current foreign: %v", err)
	}
	if err := os.WriteFile(orig, []byte("#!/bin/sh\necho stale\n"), 0o755); err != nil {
		t.Fatalf("write stale backup: %v", err)
	}
	if err := Uninstall(ctx, repo, OnCommit); err != nil {
		t.Fatalf("Uninstall: %v", err)
	}
	r, err := os.ReadFile(hp)
	if err != nil || string(r) != current {
		t.Fatalf("current foreign hook must be untouched, got %q err %v", r, err)
	}
	if _, err := os.Stat(orig); !os.IsNotExist(err) {
		t.Fatalf("expected stale backup removed, stat err = %v", err)
	}
}
