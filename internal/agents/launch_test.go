package agents

import (
	"context"
	"testing"
)

func TestBuildLaunchPlainSkipsMCP(t *testing.T) {
	// Plain mode should not require Draft executable MCP wiring beyond LookPath of the agent.
	// We only assert the flag plumbing when the agent binary is missing — still returns clear error.
	_, _, err := BuildLaunch(context.Background(), StartSessionRequest{
		AgentID: "not-a-real-agent",
		Plain:   true,
	})
	if err == nil {
		t.Fatal("expected unknown agent error")
	}
}

func TestClaudeEphemeralMCPConfigInline(t *testing.T) {
	cfg, err := claudeEphemeralMCPConfig(`C:\Draft\Draft.exe`, []string{"mcp"})
	if err != nil {
		t.Fatal(err)
	}
	if cfg == "" || cfg[0] != '{' {
		t.Fatalf("expected inline JSON object, got %q", cfg)
	}
}

func TestCodexEphemeralFlags(t *testing.T) {
	flags := codexEphemeralFlags("/usr/local/bin/Draft", []string{"mcp"})
	if len(flags) != 4 || flags[0] != "-c" || flags[2] != "-c" {
		t.Fatalf("flags: %#v", flags)
	}
}
