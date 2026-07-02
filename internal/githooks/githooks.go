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
	// merge-based pull) and any `git merge` that updates the working tree. It
	// does NOT fire for `git pull --rebase`, which uses post-rewrite instead.
	OnPull Event = "on_pull"
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
// the user's existing tooling keeps running.
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

	// Preserve a pre-existing foreign hook exactly once. If our script is
	// already there we leave any prior .draft-orig untouched.
	if data, err := os.ReadFile(hookPath); err == nil {
		if !bytes.Contains(data, []byte(marker)) {
			if _, statErr := os.Stat(origPath); os.IsNotExist(statErr) {
				if err := os.Rename(hookPath, origPath); err != nil {
					return fmt.Errorf("back up existing %s hook: %w", file, err)
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
	return nil
}

// Uninstall removes Draft's hook for the event. If a foreign hook was chained
// (preserved as <file>.draft-orig), it is restored to its original slot.
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
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	// Only remove a script that is actually ours; never touch a foreign hook.
	if !bytes.Contains(data, []byte(marker)) {
		return nil
	}
	if err := os.Remove(hookPath); err != nil {
		return err
	}
	// Restore any chained foreign hook.
	if _, statErr := os.Stat(origPath); statErr == nil {
		return os.Rename(origPath, hookPath)
	}
	return nil
}

// shellQuote wraps s in single quotes for a POSIX sh script, escaping any
// embedded single quotes. Hook scripts run via sh on macOS and via Git's
// bundled sh on Windows, so single-quoting is safe on both.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// renderScript builds the hook body. Draft's invocation always exits 0 on its
// own line so a Draft-side failure never blocks the user's git command; a
// chained foreign hook (origPath) runs afterward via exec, preserving its
// stdin, arguments, and exit code (important for pre-push, which can veto a
// push).
func renderScript(event Event, exePath, repoPath, origPath string) string {
	file, _ := event.hookFile()
	qExe := shellQuote(exePath)
	qRepo := shellQuote(repoPath)
	qOrig := shellQuote(origPath)

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
			"if [ -x " + qOrig + " ]; then printf '%s' \"$input\" | " + qOrig + " \"$@\"; exit $?; fi\n"
	default: // OnCommit (post-commit) / OnPull (post-merge) — no stdin; Draft inspects HEAD itself.
		// post-commit's and post-merge's exit codes are ignored by git, so
		// exec-ing the chained hook (replacing this process) is fine and avoids
		// an extra fork.
		invoke = qExe + " --git-hook --repo " + qRepo + " --event " + file + " </dev/null >/dev/null 2>&1\n" +
			"if [ -x " + qOrig + " ]; then exec " + qOrig + " \"$@\"; fi\n"
	}

	return "#!/bin/sh\n" +
		"# " + marker + " event=" + file + " — managed by Draft; remove via the app\n" +
		invoke +
		"exit 0\n"
}
