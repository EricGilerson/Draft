package agents

import (
	"encoding/json"
	"os"
)

func cursorMCPConfigured(draftCmd string, draftArgs []string) (bool, error) {
	paths := []string{}
	if p, err := homeFile(".cursor", "mcp.json"); err == nil {
		paths = append(paths, p)
	}
	for _, path := range paths {
		ok, err := mcpJSONHasDraft(path, draftCmd, draftArgs)
		if err == nil && ok {
			return true, nil
		}
	}
	return false, nil
}

func cursorMCPAdd(draftCmd string, draftArgs []string) error {
	path, err := homeFile(".cursor", "mcp.json")
	if err != nil {
		return err
	}
	return upsertMCPJSON(path, draftCmd, draftArgs)
}

func geminiMCPConfigured(draftCmd string, draftArgs []string) (bool, error) {
	path, err := homeFile(".gemini", "settings.json")
	if err != nil {
		return false, err
	}
	return mcpJSONHasDraft(path, draftCmd, draftArgs)
}

func geminiMCPAdd(draftCmd string, draftArgs []string) error {
	path, err := homeFile(".gemini", "settings.json")
	if err != nil {
		return err
	}
	return upsertMCPJSON(path, draftCmd, draftArgs)
}

func mcpJSONHasDraft(path, draftCmd string, draftArgs []string) (bool, error) {
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
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		return false, nil
	}
	raw, ok := servers[DraftMCPServerName]
	if !ok {
		return false, nil
	}
	return matchLooseEntry(raw, draftCmd, draftArgs), nil
}

func upsertMCPJSON(path, draftCmd string, draftArgs []string) error {
	root := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &root)
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[DraftMCPServerName] = draftEntry(draftCmd, draftArgs)
	root["mcpServers"] = servers
	return writeJSONFile(path, root)
}
