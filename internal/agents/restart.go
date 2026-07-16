package agents

import (
	"regexp"
	"strings"
	"unicode"
)

// geminiRelaunchExitCode is Gemini CLI's RELAUNCH_EXIT_CODE (processUtils.ts).
// Their own parent watchdog normally consumes this; if the whole process exits
// with 199 (e.g. parent died), Draft should relaunch the session.
const geminiRelaunchExitCode = 199

var ansiEscape = regexp.MustCompile(`\x1b\[[0-9;?]*[a-zA-Z]|\x1b\][^\x07\x1b]*(?:\x07|\x1b\\)|\x1b.`)

// shouldAutoRestart reports whether Draft should relaunch the agent after exit.
//
// Codex: prints "Update ran successfully! Please restart Codex." then exits to
// the shell — unlike Claude (stays running with "Restart to apply") or Gemini
// (self-watchdog on exit 199). We only match phrases that imply the CLI exited
// *because* of an update, not Claude's in-session banner that can linger until
// a later manual /exit.
func shouldAutoRestart(agent AgentID, exitCode int, recent []byte) bool {
	if exitCode == geminiRelaunchExitCode {
		return true
	}
	text := strings.ToLower(stripANSI(string(recent)))
	text = collapseSpace(text)
	if text == "" {
		return false
	}

	// Codex (primary case).
	if strings.Contains(text, "please restart codex") {
		return true
	}
	if strings.Contains(text, "update ran successfully") && strings.Contains(text, "please restart") {
		return true
	}

	// Other CLIs that exit and ask for a manual restart after an update.
	// Deliberately exclude Claude's "restart to apply" (session stays up).
	switch agent {
	case AgentCursor, AgentGrok, AgentGemini, AgentOpenCode, AgentCopilot:
		if strings.Contains(text, "please restart") &&
			(strings.Contains(text, "update") || strings.Contains(text, "updated") || strings.Contains(text, "upgrade")) {
			return true
		}
	}
	return false
}

func stripANSI(s string) string {
	return ansiEscape.ReplaceAllString(s, "")
}

func collapseSpace(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	prevSpace := false
	for _, r := range s {
		if unicode.IsSpace(r) {
			if !prevSpace {
				b.WriteByte(' ')
				prevSpace = true
			}
			continue
		}
		prevSpace = false
		b.WriteRune(r)
	}
	return strings.TrimSpace(b.String())
}
