package agents

import (
	"testing"
)

func TestShouldAutoRestartCodex(t *testing.T) {
	out := []byte("Updating Codex via `npm install -g @openai/codex`...\nchanged 2 packages in 5s\n🎉 Update ran successfully! Please restart Codex.\n")
	if !shouldAutoRestart(AgentCodex, 0, out) {
		t.Fatal("expected Codex update exit to restart")
	}
}

func TestShouldAutoRestartGeminiExitCode(t *testing.T) {
	if !shouldAutoRestart(AgentGemini, 199, nil) {
		t.Fatal("expected exit 199 to restart")
	}
}

func TestShouldNotRestartClaudeBanner(t *testing.T) {
	// Claude keeps running after update; this banner can still be in the scrollback
	// when the user later exits — must not trigger a false restart.
	out := []byte("✓ Update installed · Restart to apply\n\n> /exit\n")
	if shouldAutoRestart(AgentClaude, 0, out) {
		t.Fatal("Claude in-session update banner must not auto-restart")
	}
}

func TestShouldNotRestartNormalExit(t *testing.T) {
	out := []byte("Goodbye!\n")
	if shouldAutoRestart(AgentCodex, 0, out) {
		t.Fatal("normal exit should not restart")
	}
}

func TestShouldAutoRestartDespiteANSI(t *testing.T) {
	out := []byte("\x1b[32mUpdate ran successfully!\x1b[0m Please restart Codex.\n")
	if !shouldAutoRestart(AgentCodex, 0, out) {
		t.Fatal("expected ANSI-wrapped Codex message to restart")
	}
}
