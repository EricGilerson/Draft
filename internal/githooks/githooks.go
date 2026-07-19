// Package githooks installs and removes the small shell hooks Draft uses to
// trigger automatic deploys when a tracked branch receives a commit or push.
//
// A hook is a dormant script in the repository's hooks directory that git runs
// synchronously as part of `git commit` / `git push`. Draft's hook does one
// thing: invoke the Draft binary in `--git-hook` mode, which rings the daemon's
// doorbell and exits. It never blocks or fails the user's git command.
//
// Hooks live under the repository's git hooks directory (honoring
// core.hooksPath and worktrees), which is *not* part of the tracked working
// tree — so enabling a trigger writes only local, per-clone files and never
// modifies or commits anything in the user's project.
package githooks

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// Event identifies which git event should trigger a deploy.
type Event string

const (
	// OnCommit installs a post-commit hook: fires when a commit lands on the
	// currently checked-out branch.
	OnCommit Event = "on_commit"
	// OnPush installs a pre-push hook: fires when refs are pushed to a remote.
	OnPush Event = "on_push"
	// OnPull installs a post-merge hook: fires after `git pull` (the default
	// merge-based pull) and any `git merge` that updates the working tree.
	OnPull Event = "on_pull"
	// OnPullRewrite installs a post-rewrite hook: fires after `git pull --rebase`
	// / `git rebase` (and amend). Paired with OnPull so redeploy-on-pull covers
	// both merge and rebase pull strategies.
	OnPullRewrite Event = "on_pull_rewrite"
)

// hookFile maps an Event to the git hook filename that carries it.
func (e Event) hookFile() (string, error) {
	switch e {
	case OnCommit:
		return "post-commit", nil
	case OnPush:
		return "pre-push", nil
	case OnPull:
		return "post-merge", nil
	case OnPullRewrite:
		return "post-rewrite", nil
	default:
		return "", fmt.Errorf("unknown git trigger event %q", e)
	}
}

// marker tags scripts Draft wrote so we recognize (and safely replace or remove)
// our own hook without clobbering a user's pre-existing hook.
const marker = "draft-managed-hook"

// hooksDir resolves the repository's hooks directory, honoring core.hooksPath
// and worktree layouts via `git rev-parse --git-path hooks`.
func hooksDir(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--git-path", "hooks")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("resolve hooks dir: %s", msg)
	}
	dir := strings.TrimSpace(string(out))
	if dir == "" {
		return "", fmt.Errorf("empty hooks dir")
	}
	// `--git-path` may return a path relative to the repo root.
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repoPath, dir)
	}
	return dir, nil
}

// Status reports whether Draft's hook for the event is installed, and whether a
// foreign (non-Draft) hook of that type is present that Draft would chain to.
func Status(ctx context.Context, repoPath string, event Event) (installed, foreign bool, err error) {
	file, err := event.hookFile()
	if err != nil {
		return false, false, err
	}
	dir, err := hooksDir(ctx, repoPath)
	if err != nil {
		return false, false, err
	}
	hookPath := filepath.Join(dir, file)

	data, err := os.ReadFile(hookPath)
	if err != nil {
		if os.IsNotExist(err) {
			return false, false, nil
		}
		return false, false, err
	}
	if bytes.Contains(data, []byte(marker)) {
		// Ours. A foreign hook we chained to would sit alongside as <file>.draft-orig.
		_, statErr := os.Stat(hookPath + ".draft-orig")
		return true, statErr == nil, nil
	}
	// A hook exists but isn't ours: it's a foreign hook (not yet chained).
	return false, true, nil
}

// Install writes Draft's hook for the event into the repository, wiring it to
// invoke exePath in --git-hook mode. If a foreign hook already occupies the
// slot, it is preserved as <file>.draft-orig and chained from Draft's script so
// the user's existing tooling keeps running. If a foreign hook has changed
// since we preserved it (e.g. a husky re-init rewrote its wrapper), the backup
// is refreshed so we chain to the foreign tool's current hook and Uninstall
// restores the current one rather than a stale copy.
func Install(ctx context.Context, repoPath, exePath string, event Event) error {
	file, err := event.hookFile()
	if err != nil {
		return err
	}
	dir, err := hooksDir(ctx, repoPath)
	if err != nil {
		return err
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	hookPath := filepath.Join(dir, file)
	origPath := hookPath + ".draft-orig"

	// Preserve a pre-existing foreign hook. If our script is already in the
	// slot we leave any prior .draft-orig untouched — the foreign tool can't
	// have rewritten the slot while our marker is on it. If a foreign hook
	// occupies the slot and a backup already exists, refresh the backup when
	// the foreign hook has changed since we preserved it.
	if data, err := os.ReadFile(hookPath); err == nil {
		if !bytes.Contains(data, []byte(marker)) {
			if prev, perr := os.ReadFile(origPath); os.IsNotExist(perr) {
				if err := os.Rename(hookPath, origPath); err != nil {
					return fmt.Errorf("back up existing %s hook: %w", file, err)
				}
			} else if perr == nil && !bytes.Equal(prev, data) {
				if err := os.WriteFile(origPath, data, 0o755); err != nil {
					return fmt.Errorf("refresh %s hook backup: %w", file, err)
				}
			}
		}
	} else if !os.IsNotExist(err) {
		return err
	}

	script := renderScript(event, exePath, repoPath, origPath)
	if err := os.WriteFile(hookPath, []byte(script), 0o755); err != nil {
		return err
	}
	// os.WriteFile does not apply the exec bit through umask reliably on all
	// platforms; make it explicit on Unix. No-op semantics on Windows.
	if runtime.GOOS != "windows" {
		_ = os.Chmod(hookPath, 0o755)
	}
	// When the hooks directory lives inside the worktree (e.g. husky-style
	// core.hooksPath pointing at .husky/_), our managed hook and its .draft-orig
	// backup would otherwise surface as untracked files in `git status`. Hide
	// them via .git/info/exclude — a per-clone, untracked, never-committed file,
	// the same class of local artifact as .git/hooks itself — so we never touch
	// the user's committed .gitignore.
	if hookPat, origPat, ok := excludePatterns(ctx, repoPath, hookPath); ok {
		if err := addExcludes(ctx, repoPath, event, hookPat, origPat); err != nil {
			return fmt.Errorf("hide %s hook from git status: %w", file, err)
		}
	}
	return nil
}

// Uninstall removes Draft's hook for the event. If a foreign hook was chained
// (preserved as <file>.draft-orig), it is restored to its original slot. If our
// managed hook is already gone but a backup remains (orphaned by a foreign tool
// or the user removing our script), the backup is restored so the user's
// tooling keeps running. Draft's .git/info/exclude entries for the event are
// always cleaned up.
func Uninstall(ctx context.Context, repoPath string, event Event) error {
	file, err := event.hookFile()
	if err != nil {
		return err
	}
	dir, err := hooksDir(ctx, repoPath)
	if err != nil {
		return err
	}
	hookPath := filepath.Join(dir, file)
	origPath := hookPath + ".draft-orig"

	data, err := os.ReadFile(hookPath)
	if err != nil {
		if !os.IsNotExist(err) {
			return err
		}
		// Our managed hook is already gone. If a chained foreign backup
		// remains, restore it to the canonical slot so the user's tooling
		// keeps working.
		if _, statErr := os.Stat(origPath); statErr == nil {
			if rerr := os.Rename(origPath, hookPath); rerr != nil {
				return rerr
			}
		}
		return removeExcludes(ctx, repoPath, event)
	}
	// A hook exists but isn't ours: never touch the slot. But if a .draft-orig
	// backup is also present, we once managed this slot and a foreign tool has
	// since reclaimed it, leaving a stale backup — discard the backup along
	// with our exclude entries.
	if !bytes.Contains(data, []byte(marker)) {
		if _, statErr := os.Stat(origPath); statErr == nil {
			_ = os.Remove(origPath)
		}
		return removeExcludes(ctx, repoPath, event)
	}
	if err := os.Remove(hookPath); err != nil {
		return err
	}
	// Restore any chained foreign hook.
	if _, statErr := os.Stat(origPath); statErr == nil {
		if rerr := os.Rename(origPath, hookPath); rerr != nil {
			return rerr
		}
	}
	return removeExcludes(ctx, repoPath, event)
}

// shellQuote wraps s in single quotes for a POSIX sh script, escaping any
// embedded single quotes. Hook scripts run via sh on macOS and via Git's
// bundled sh on Windows, so single-quoting is safe on both.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// toHookPath normalizes a path before it is embedded into a hook script.
//
// On Windows, os.Executable and filepath.Join produce backslash-separated
// paths like C:\Users\...\draft.exe. Inside single quotes sh treats backslashes
// as literal, so the script would hand cygwin/MSYS2 a backslash-style path.
// Cygwin usually accepts those, and MSYS2's automatic path conversion for
// native-Windows arguments usually leaves already-Windows paths alone — but
// "usually" is not good enough for something that fails silently (the hook
// redirects to /dev/null and exits 0). Forward slashes are simultaneously valid
// Windows paths (accepted by the native Go binary) and unambiguous to cygwin,
// so we normalize on Windows only.
//
// This MUST be a no-op on Unix, where backslash is a legal filename character
// and must not be rewritten.
func toHookPath(s string) string {
	if runtime.GOOS == "windows" {
		return strings.ReplaceAll(s, `\`, "/")
	}
	return s
}

// renderScript builds the hook body. Draft's invocation always exits 0 on its
// own line so a Draft-side failure never blocks the user's git command; a
// chained foreign hook (origPath) runs afterward via exec, preserving its
// stdin, arguments, and exit code (important for pre-push, which can veto a
// push).
func renderScript(event Event, exePath, repoPath, origPath string) string {
	file, _ := event.hookFile()
	qExe := shellQuote(toHookPath(exePath))
	qRepo := shellQuote(toHookPath(repoPath))
	qOrig := shellQuote(toHookPath(origPath))

	var invoke string
	switch event {
	case OnPush:
		// pre-push receives the pushed refs on stdin. Buffer it so we can feed
		// the same data to Draft (to learn what's being pushed) and to any
		// chained hook. Draft is run with a short leash; it returns fast. A
		// chained foreign hook runs afterward and its exit code is propagated,
		// so it retains the ability to veto the push.
		invoke = "input=$(cat)\n" +
			"printf '%s' \"$input\" | " + qExe + " --git-hook --repo " + qRepo + " --event " + file + " >/dev/null 2>&1\n" +
			// NOTE: the [ -x ] guard is a faithful Unix proxy for "this hook is
			// runnable" because git on Unix will not execute a hook without the
			// exec bit. On Windows, git-for-Windows runs hooks via its bundled
			// sh regardless of the (simulated) exec bit, so a foreign hook
			// without a #! shebang — which cygwin's -x heuristic does not flag
			// as executable — would be skipped here even though git would have
			// run it. This is a known narrow limitation; switching to [ -f ]
			// on Windows would be more correct but would also veto pushes when
			// a foreign hook file exists but is malformed, so we keep [ -x ].
			"if [ -x " + qOrig + " ]; then printf '%s' \"$input\" | " + qOrig + " \"$@\"; exit $?; fi\n"
	default: // OnCommit / OnPull (post-merge) / OnPullRewrite (post-rewrite)
		// These hooks' exit codes are ignored by git (or non-blocking for our
		// doorbell), so exec-ing the chained hook is fine and avoids an extra fork.
		// post-rewrite receives "rebase"|"amend" on argv; Draft ignores args and
		// inspects HEAD like post-merge.
		invoke = qExe + " --git-hook --repo " + qRepo + " --event " + file + " </dev/null >/dev/null 2>&1\n" +
			"if [ -x " + qOrig + " ]; then exec " + qOrig + " \"$@\"; fi\n"
	}

	return "#!/bin/sh\n" +
		"# " + marker + " event=" + file + " — managed by Draft; remove via the app\n" +
		invoke +
		"exit 0\n"
}

// excludeBlockBegin and excludeBlockEnd bracket Draft's ignore entries in
// .git/info/exclude. Tagging both with the marker lets addExcludes replace our
// block and removeExcludes delete exactly our block, without ever touching the
// user's own ignore rules.
func excludeBlockBegin(file string) string { return "# " + marker + " begin event=" + file + "\n" }
func excludeBlockEnd(file string) string   { return "# " + marker + " end event=" + file + "\n" }

// gitDir resolves the repository's git directory (e.g. ".git") as an absolute
// path via `git rev-parse --absolute-git-dir`.
func gitDir(ctx context.Context, repoPath string) (string, error) {
	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--absolute-git-dir")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("resolve git dir: %s", msg)
	}
	return strings.TrimSpace(string(out)), nil
}

// excludeFilePath returns the path to the repo's .git/info/exclude — the
// per-clone ignore file that is never tracked or committed.
func excludeFilePath(ctx context.Context, repoPath string) (string, error) {
	dir, err := gitDir(ctx, repoPath)
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, "info", "exclude"), nil
}

// removeExcludeBlock strips every Draft-managed block for the given hook file
// from content. A malformed block (begin without a matching end) is left
// untouched rather than risk deleting user content.
func removeExcludeBlock(content, file string) string {
	begin := excludeBlockBegin(file)
	end := excludeBlockEnd(file)
	for {
		i := strings.Index(content, begin)
		if i < 0 {
			return content
		}
		rel := content[i:]
		j := strings.Index(rel, end)
		if j < 0 {
			return content
		}
		content = content[:i] + rel[j+len(end):]
	}
}

// addExcludes appends Draft's ignore block for the event to .git/info/exclude,
// creating the file (and the info/ directory) if needed. It is idempotent: an
// existing block for the same event is replaced in place rather than duplicated.
func addExcludes(ctx context.Context, repoPath string, event Event, hookPat, origPat string) error {
	file, err := event.hookFile()
	if err != nil {
		return err
	}
	exPath, err := excludeFilePath(ctx, repoPath)
	if err != nil {
		return err
	}
	body, err := os.ReadFile(exPath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	content := removeExcludeBlock(string(body), file)
	content = strings.TrimRight(content, "\n")
	if content != "" {
		content += "\n"
	}
	content += excludeBlockBegin(file) + hookPat + "\n" + origPat + "\n" + excludeBlockEnd(file)
	if err := os.MkdirAll(filepath.Dir(exPath), 0o755); err != nil {
		return err
	}
	return os.WriteFile(exPath, []byte(content), 0o644)
}

// removeExcludes strips Draft's ignore block for the event from
// .git/info/exclude. A missing file, an unresolvable git dir, or a missing
// block are all no-ops; the user's own ignore rules are never touched.
func removeExcludes(ctx context.Context, repoPath string, event Event) error {
	file, err := event.hookFile()
	if err != nil {
		return err
	}
	exPath, err := excludeFilePath(ctx, repoPath)
	if err != nil {
		// addExcludes is only reached when excludePatterns already resolved the
		// git dir, so if we can't resolve it here there is nothing to clean up.
		return nil
	}
	body, err := os.ReadFile(exPath)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	trimmed := removeExcludeBlock(string(body), file)
	if trimmed == string(body) {
		return nil
	}
	return os.WriteFile(exPath, []byte(trimmed), 0o644)
}

// resolveReal returns the symlink-free form of p, falling back to p if the path
// cannot be evaluated. macOS places temp dirs under /var which symlinks to
// /private/var, and git reports the real form via --show-toplevel while our
// hook path may carry the symlink form — normalizing keeps the two comparable.
func resolveReal(p string) string {
	if r, err := filepath.EvalSymlinks(p); err == nil {
		return r
	}
	return p
}

// pathIsUnder reports whether p is contained within base (both absolute).
func pathIsUnder(p, base string) bool {
	rel, err := filepath.Rel(base, p)
	if err != nil {
		return false
	}
	rel = filepath.ToSlash(rel)
	return rel != "." && !strings.HasPrefix(rel, "../") && rel != ".."
}

// excludePatterns reports whether the managed hook file lives inside the
// worktree but outside the git directory — the case where git would otherwise
// surface our files as untracked (e.g. husky-style core.hooksPath). When true
// it returns the gitignore patterns (relative to the worktree root, with
// forward slashes) for the managed hook and its .draft-orig backup. When the
// hooks dir is under .git (the default) or outside the worktree, ok is false
// and no excludes are needed.
func excludePatterns(ctx context.Context, repoPath, hookPath string) (hookPat, origPat string, ok bool) {
	rootOut, err := exec.CommandContext(ctx, "git", "-C", repoPath, "rev-parse", "--show-toplevel").Output()
	if err != nil {
		return "", "", false
	}
	worktree := strings.TrimSpace(string(rootOut))
	if worktree == "" {
		return "", "", false
	}
	gDir, err := gitDir(ctx, repoPath)
	if err != nil {
		return "", "", false
	}
	absHook, err := filepath.Abs(hookPath)
	if err != nil {
		return "", "", false
	}
	worktree = resolveReal(worktree)
	gDir = resolveReal(gDir)
	absHook = resolveReal(absHook)
	if !pathIsUnder(absHook, worktree) || pathIsUnder(absHook, gDir) {
		return "", "", false
	}
	rel, err := filepath.Rel(worktree, absHook)
	if err != nil {
		return "", "", false
	}
	hookPat = filepath.ToSlash(rel)
	origPat = hookPat + ".draft-orig"
	return hookPat, origPat, true
}
