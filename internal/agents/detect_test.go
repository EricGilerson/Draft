package agents

import (
	"context"
	"testing"
)

func TestMatchesCursorBinaryRejectsGrokAgent(t *testing.T) {
	ctx := context.Background()
	cases := []string{
		`C:\Users\ericg\.grok\bin\agent.exe`,
		`/home/ericg/.grok/bin/agent`,
		`/opt/grok/bin/agent`,
	}
	for _, path := range cases {
		if matchesCursorBinary(ctx, "agent", path) {
			t.Fatalf("Cursor should not claim Grok agent at %s", path)
		}
	}
}

func TestMatchesCursorBinaryAcceptsCursorPaths(t *testing.T) {
	ctx := context.Background()
	cases := []string{
		`C:\Users\ericg\AppData\Local\cursor-agent\cursor-agent.cmd`,
		`C:\Users\ericg\AppData\Local\cursor-agent\agent.cmd`,
		`/home/ericg/.local/share/cursor-agent/versions/2026.07.01/cursor-agent`,
		`/home/ericg/.local/bin/cursor-agent`,
	}
	for _, path := range cases {
		name := "agent"
		if agentBaseName(path) == "cursor-agent" {
			name = "cursor-agent"
		}
		if !matchesCursorBinary(ctx, name, path) {
			t.Fatalf("Cursor should accept %s", path)
		}
	}
}

func TestMatchesGrokBinaryRejectsCursor(t *testing.T) {
	if matchesGrokBinary(`C:\Users\ericg\AppData\Local\cursor-agent\agent.cmd`) {
		t.Fatal("Grok should not claim Cursor agent")
	}
	if !matchesGrokBinary(`C:\Users\ericg\.grok\bin\grok.exe`) {
		t.Fatal("Grok should accept ~/.grok/bin/grok.exe")
	}
	if !matchesGrokBinary(`/usr/local/bin/grok`) {
		t.Fatal("Grok should accept a plain grok binary")
	}
}

func TestCursorSpecPrefersCursorAgentName(t *testing.T) {
	spec, ok := findSpec(AgentCursor)
	if !ok {
		t.Fatal("missing cursor spec")
	}
	if len(spec.Binaries) < 2 || spec.Binaries[0] != "cursor-agent" {
		t.Fatalf("expected cursor-agent first, got %#v", spec.Binaries)
	}
}
