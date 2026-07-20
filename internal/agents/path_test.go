package agents

import (
	"strings"
	"testing"
)

func TestChildEnvSuppliesTerminalCapabilitiesWhenLaunchedWithoutTerminal(t *testing.T) {
	t.Setenv("TERM", "")
	t.Setenv("COLORTERM", "")

	env := childEnv()
	if got := envValue(env, "TERM"); got != "xterm-256color" {
		t.Fatalf("TERM = %q, want xterm-256color", got)
	}
	if got := envValue(env, "COLORTERM"); got != "truecolor" {
		t.Fatalf("COLORTERM = %q, want truecolor", got)
	}
}

func envValue(env []string, key string) string {
	prefix := key + "="
	for _, entry := range env {
		if strings.HasPrefix(entry, prefix) {
			return strings.TrimPrefix(entry, prefix)
		}
	}
	return ""
}
