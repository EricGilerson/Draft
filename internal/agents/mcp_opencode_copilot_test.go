package agents

import (
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCodeUpsertAndMatch(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "opencode.json")
	cmd := filepath.Join(dir, "Draft")
	args := []string{"mcp"}

	if err := upsertOpenCodeJSON(path, cmd, args); err != nil {
		t.Fatal(err)
	}
	ok, err := openCodeJSONHasDraft(path, cmd, args)
	if err != nil || !ok {
		t.Fatalf("expected draft configured, ok=%v err=%v", ok, err)
	}

	// Disabled entry should not count as configured.
	root := map[string]any{
		"mcp": map[string]any{
			DraftMCPServerName: map[string]any{
				"type":    "local",
				"command": append([]string{cmd}, args...),
				"enabled": false,
			},
		},
	}
	if err := writeJSONFile(path, root); err != nil {
		t.Fatal(err)
	}
	ok, err = openCodeJSONHasDraft(path, cmd, args)
	if err != nil {
		t.Fatal(err)
	}
	if ok {
		t.Fatal("disabled MCP should not match")
	}
}

func TestCopilotUpsertAndMatch(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)

	cmd := filepath.Join(home, "Draft.exe")
	args := []string{"mcp"}
	if err := upsertCopilotJSON(cmd, args); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, ".copilot", "mcp-config.json")
	if _, err := os.Stat(path); err != nil {
		t.Fatal(err)
	}
	ok, err := mcpJSONHasDraft(path, cmd, args)
	if err != nil || !ok {
		t.Fatalf("expected draft configured, ok=%v err=%v", ok, err)
	}
}
