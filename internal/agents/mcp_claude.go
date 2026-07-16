package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

// IsDraftMCPConfigured reports whether the agent already points at Draft's MCP.
func IsDraftMCPConfigured(ctx context.Context, id AgentID, agentBin, draftCmd string, draftArgs []string) (bool, error) {
	switch id {
	case AgentClaude:
		return claudeMCPConfigured(ctx, agentBin, draftCmd, draftArgs)
	case AgentCodex:
		return codexMCPConfigured(ctx, agentBin, draftCmd, draftArgs)
	case AgentCursor:
		return cursorMCPConfigured(draftCmd, draftArgs)
	case AgentGemini:
		return geminiMCPConfigured(draftCmd, draftArgs)
	case AgentGrok:
		return grokMCPConfigured(ctx, agentBin, draftCmd, draftArgs)
	case AgentOpenCode:
		return openCodeMCPConfigured(draftCmd, draftArgs)
	case AgentCopilot:
		return copilotMCPConfigured(ctx, agentBin, draftCmd, draftArgs)
	default:
		return false, fmt.Errorf("unknown agent %s", id)
	}
}

// EnsureDraftMCP installs Draft MCP into the agent's persistent config when missing.
// No-op when already configured correctly.
func EnsureDraftMCP(ctx context.Context, id AgentID, agentBin, draftCmd string, draftArgs []string) error {
	ok, err := IsDraftMCPConfigured(ctx, id, agentBin, draftCmd, draftArgs)
	if err == nil && ok {
		return nil
	}
	switch id {
	case AgentClaude:
		return claudeMCPAdd(ctx, agentBin, draftCmd, draftArgs)
	case AgentCodex:
		return codexMCPAdd(ctx, agentBin, draftCmd, draftArgs)
	case AgentCursor:
		return cursorMCPAdd(draftCmd, draftArgs)
	case AgentGemini:
		return geminiMCPAdd(draftCmd, draftArgs)
	case AgentGrok:
		return grokMCPAdd(ctx, agentBin, draftCmd, draftArgs)
	case AgentOpenCode:
		return openCodeMCPAdd(draftCmd, draftArgs)
	case AgentCopilot:
		return copilotMCPAdd(ctx, agentBin, draftCmd, draftArgs)
	default:
		return fmt.Errorf("unknown agent %s", id)
	}
}

func claudeMCPConfigured(ctx context.Context, agentBin, draftCmd string, draftArgs []string) (bool, error) {
	// Prefer CLI list when available.
	cmd := exec.CommandContext(ctx, agentBin, "mcp", "list")
	cmd.Env = childEnv()
	out, err := cmd.CombinedOutput()
	if err == nil {
		text := strings.ToLower(string(out))
		if strings.Contains(text, DraftMCPServerName) {
			// Also accept file check for exact command match.
			if ok, _ := claudeConfigHasDraft(draftCmd, draftArgs); ok {
				return true, nil
			}
			// Listed as draft — good enough (user may have aliased path).
			return true, nil
		}
	}
	return claudeConfigHasDraft(draftCmd, draftArgs)
}

func claudeConfigHasDraft(draftCmd string, draftArgs []string) (bool, error) {
	path, err := homeFile(".claude.json")
	if err != nil {
		return false, err
	}
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
	if servers, ok := root["mcpServers"].(map[string]any); ok {
		if entry, ok := servers[DraftMCPServerName]; ok {
			return matchLooseEntry(entry, draftCmd, draftArgs), nil
		}
	}
	return false, nil
}

func claudeMCPAdd(ctx context.Context, agentBin, draftCmd string, draftArgs []string) error {
	args := []string{"mcp", "add", "--scope", "user", DraftMCPServerName, "--", draftCmd}
	args = append(args, draftArgs...)
	cmd := exec.CommandContext(ctx, agentBin, args...)
	cmd.Env = childEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: merge into ~/.claude.json
		if ferr := mergeClaudeJSON(draftCmd, draftArgs); ferr == nil {
			return nil
		}
		return fmt.Errorf("claude mcp add: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func mergeClaudeJSON(draftCmd string, draftArgs []string) error {
	path, err := homeFile(".claude.json")
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
	servers[DraftMCPServerName] = draftEntry(draftCmd, draftArgs)
	root["mcpServers"] = servers
	return writeJSONFile(path, root)
}

func matchLooseEntry(raw any, draftCmd string, draftArgs []string) bool {
	b, err := json.Marshal(raw)
	if err != nil {
		return false
	}
	var entry mcpServerEntry
	if err := json.Unmarshal(b, &entry); err != nil {
		return false
	}
	return sameMCPCommand(entry, draftCmd, draftArgs)
}
