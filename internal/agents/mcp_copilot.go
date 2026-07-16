package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// GitHub Copilot CLI user MCP config: ~/.copilot/mcp-config.json
//
//	{
//	  "mcpServers": {
//	    "draft": {
//	      "type": "local",
//	      "command": "...",
//	      "args": ["mcp"],
//	      "tools": ["*"]
//	    }
//	  }
//	}

func copilotMCPConfigured(ctx context.Context, agentBin, draftCmd string, draftArgs []string) (bool, error) {
	cmd := exec.CommandContext(ctx, agentBin, "mcp", "list")
	cmd.Env = childEnv()
	if out, err := cmd.CombinedOutput(); err == nil {
		text := strings.ToLower(string(out))
		if strings.Contains(text, DraftMCPServerName) {
			if ok, _ := copilotConfigHasDraft(draftCmd, draftArgs); ok {
				return true, nil
			}
			return true, nil
		}
	}
	return copilotConfigHasDraft(draftCmd, draftArgs)
}

func copilotConfigHasDraft(draftCmd string, draftArgs []string) (bool, error) {
	path, err := homeFile(".copilot", "mcp-config.json")
	if err != nil {
		return false, err
	}
	return mcpJSONHasDraft(path, draftCmd, draftArgs)
}

func copilotMCPAdd(ctx context.Context, agentBin, draftCmd string, draftArgs []string) error {
	args := []string{"mcp", "add", DraftMCPServerName, "--", draftCmd}
	args = append(args, draftArgs...)
	cmd := exec.CommandContext(ctx, agentBin, args...)
	cmd.Env = childEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ferr := upsertCopilotJSON(draftCmd, draftArgs); ferr == nil {
			return nil
		}
		return fmt.Errorf("copilot mcp add: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func upsertCopilotJSON(draftCmd string, draftArgs []string) error {
	path, err := homeFile(".copilot", "mcp-config.json")
	if err != nil {
		return err
	}
	root := map[string]any{}
	if data, err := os.ReadFile(path); err == nil && len(data) > 0 {
		_ = json.Unmarshal(data, &root)
	}
	servers, _ := root["mcpServers"].(map[string]any)
	if servers == nil {
		servers = map[string]any{}
	}
	servers[DraftMCPServerName] = map[string]any{
		"type":    "local",
		"command": draftCmd,
		"args":    draftArgs,
		"tools":   []string{"*"},
	}
	root["mcpServers"] = servers
	return writeJSONFile(path, root)
}
