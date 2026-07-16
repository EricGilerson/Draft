package agents

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestClaudeEphemeralMCPConfigInline(t *testing.T) {
	cfg, err := claudeEphemeralMCPConfig(`C:\Draft\Draft.exe`, []string{"mcp"})
	if err != nil {
		t.Fatal(err)
	}
	var root map[string]any
	if err := json.Unmarshal([]byte(cfg), &root); err != nil {
		t.Fatalf("config must be valid JSON: %v (%s)", err, cfg)
	}
	servers := root["mcpServers"].(map[string]any)
	entry := servers[DraftMCPServerName].(map[string]any)
	if entry["command"] != `C:\Draft\Draft.exe` {
		t.Fatalf("command: %v", entry["command"])
	}
}

func TestCodexEphemeralFlags(t *testing.T) {
	flags := codexEphemeralFlags("/usr/local/bin/Draft", []string{"mcp"})
	if len(flags) != 4 || flags[0] != "-c" || flags[2] != "-c" {
		t.Fatalf("flags: %#v", flags)
	}
	if !strings.Contains(flags[1], "mcp_servers.draft.command=") {
		t.Fatalf("cmd flag: %s", flags[1])
	}
	if !strings.Contains(flags[3], "mcp_servers.draft.args=") {
		t.Fatalf("args flag: %s", flags[3])
	}
}
