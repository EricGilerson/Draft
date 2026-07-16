package agents

import (
	"encoding/json"
	"os"
	"path/filepath"
)

// OpenCode stores MCP under mcp.<name> with command as a single argv array:
//
//	{ "type": "local", "command": ["Draft", "mcp"], "enabled": true }
//
// Global config: ~/.config/opencode/opencode.json (also checked under UserConfigDir).

func openCodeMCPConfigured(draftCmd string, draftArgs []string) (bool, error) {
	for _, path := range openCodeConfigPaths() {
		ok, err := openCodeJSONHasDraft(path, draftCmd, draftArgs)
		if err == nil && ok {
			return true, nil
		}
	}
	return false, nil
}

func openCodeMCPAdd(draftCmd string, draftArgs []string) error {
	path := openCodeWritePath()
	return upsertOpenCodeJSON(path, draftCmd, draftArgs)
}

func openCodeConfigPaths() []string {
	var paths []string
	seen := map[string]bool{}
	add := func(p string) {
		if p == "" || seen[p] {
			return
		}
		seen[p] = true
		paths = append(paths, p)
	}
	if p, err := homeFile(".config", "opencode", "opencode.json"); err == nil {
		add(p)
	}
	if cfg, err := os.UserConfigDir(); err == nil {
		add(filepath.Join(cfg, "opencode", "opencode.json"))
	}
	return paths
}

func openCodeWritePath() string {
	if p, err := homeFile(".config", "opencode", "opencode.json"); err == nil {
		return p
	}
	if cfg, err := os.UserConfigDir(); err == nil {
		return filepath.Join(cfg, "opencode", "opencode.json")
	}
	return filepath.Join("opencode.json")
}

func openCodeJSONHasDraft(path, draftCmd string, draftArgs []string) (bool, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return false, nil
		}
		return false, err
	}
	var root map[string]any
	if err := json.Unmarshal(data, &root); err != nil {
		return false, err
	}
	mcp, _ := root["mcp"].(map[string]any)
	if mcp == nil {
		return false, nil
	}
	raw, ok := mcp[DraftMCPServerName]
	if !ok {
		return false, nil
	}
	return matchOpenCodeEntry(raw, draftCmd, draftArgs), nil
}

func upsertOpenCodeJSON(path, draftCmd string, draftArgs []string) error {
	root := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &root)
	}
	mcp, _ := root["mcp"].(map[string]any)
	if mcp == nil {
		mcp = map[string]any{}
	}
	cmd := append([]string{draftCmd}, draftArgs...)
	mcp[DraftMCPServerName] = map[string]any{
		"type":    "local",
		"command": cmd,
		"enabled": true,
	}
	root["mcp"] = mcp
	if _, ok := root["$schema"]; !ok {
		root["$schema"] = "https://opencode.ai/config.json"
	}
	return writeJSONFile(path, root)
}

func matchOpenCodeEntry(raw any, draftCmd string, draftArgs []string) bool {
	b, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	var entry struct {
		Type    string   `json:"type"`
		Command []string `json:"command"`
		Enabled *bool    `json:"enabled"`
		URL     string   `json:"url"`
	}
	if err := json.Unmarshal(b, &entry); err != nil {
		return false
	}
	if entry.URL != "" {
		return false
	}
	if entry.Enabled != nil && !*entry.Enabled {
		return false
	}
	if len(entry.Command) == 0 {
		return false
	}
	want := append([]string{draftCmd}, draftArgs...)
	if len(entry.Command) != len(want) {
		return false
	}
	if !samePath(entry.Command[0], want[0]) {
		return false
	}
	return equalStringSlices(entry.Command[1:], want[1:])
}
