package agents

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
)

// LaunchPlan is the argv + env used to start an agent session.
type LaunchPlan struct {
	Path      string
	Args      []string
	Env       []string
	Cwd       string
	Ephemeral bool
	Plain     bool
	Display   string // human-readable command line for UI
}

// BuildLaunch prepares the process invocation, ensuring MCP as needed.
// For Claude/Codex with ephemeral=true, Draft MCP is injected only for this process.
// Plain=true skips all Draft MCP wiring (no inject, no ensure/install).
func BuildLaunch(ctx context.Context, req StartSessionRequest) (*LaunchPlan, *AgentInfo, error) {
	spec, ok := findSpec(req.AgentID)
	if !ok {
		return nil, nil, fmt.Errorf("unknown agent %q", req.AgentID)
	}
	path, bin := resolveBinary(ctx, spec)
	if path == "" {
		return nil, nil, fmt.Errorf("%s is not installed (not found on PATH)", spec.Name)
	}
	info := &AgentInfo{
		ID:                spec.ID,
		Name:              spec.Name,
		Binary:            bin,
		Path:              path,
		Installed:         true,
		SupportsEphemeral: spec.SupportsEphemeral,
	}

	plain := req.Plain
	ephemeral := !plain && req.Ephemeral && spec.SupportsEphemeral
	args := []string{}

	if !plain {
		draftCmd, draftArgs, err := DraftMCPCommand()
		if err != nil {
			return nil, info, fmt.Errorf("resolve Draft MCP command: %w", err)
		}

		if !ephemeral {
			// Persistent path: ensure Draft MCP is configured, then start.
			if err := EnsureDraftMCP(ctx, spec.ID, path, draftCmd, draftArgs); err != nil {
				return nil, info, fmt.Errorf("ensure Draft MCP: %w", err)
			}
			info.MCPConfigured = true
		} else {
			switch spec.ID {
			case AgentClaude:
				cfg, err := claudeEphemeralMCPConfig(draftCmd, draftArgs)
				if err != nil {
					return nil, info, err
				}
				args = append(args, "--mcp-config", cfg)
			case AgentCodex:
				args = append(args, codexEphemeralFlags(draftCmd, draftArgs)...)
			}
		}
	} else {
		// Plain: don't inject or install. Best-effort note if MCP already exists.
		if draftCmd, draftArgs, err := DraftMCPCommand(); err == nil {
			if ok, _ := IsDraftMCPConfigured(ctx, spec.ID, path, draftCmd, draftArgs); ok {
				info.MCPConfigured = true
			}
		}
	}

	cwd := strings.TrimSpace(req.Cwd)
	display := shellQuote(path)
	for _, a := range args {
		display += " " + shellQuote(a)
	}

	return &LaunchPlan{
		Path:      path,
		Args:      args,
		Env:       childEnv(),
		Cwd:       cwd,
		Ephemeral: ephemeral,
		Plain:     plain,
		Display:   display,
	}, info, nil
}

// claudeEphemeralMCPConfig returns inline JSON for --mcp-config (no temp file).
func claudeEphemeralMCPConfig(draftCmd string, draftArgs []string) (string, error) {
	payload := map[string]any{
		"mcpServers": map[string]any{
			DraftMCPServerName: draftEntry(draftCmd, draftArgs),
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	return string(b), nil
}

func codexEphemeralFlags(draftCmd string, draftArgs []string) []string {
	// codex -c 'mcp_servers.draft.command="..."' -c 'mcp_servers.draft.args=["mcp"]'
	cmdFlag := fmt.Sprintf("mcp_servers.%s.command=%s", DraftMCPServerName, strconv.Quote(draftCmd))
	argsJSON, _ := json.Marshal(draftArgs)
	argsFlag := fmt.Sprintf("mcp_servers.%s.args=%s", DraftMCPServerName, string(argsJSON))
	return []string{"-c", cmdFlag, "-c", argsFlag}
}

func shellQuote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"'`$&|;<>(){}[]") {
		return s
	}
	return strconv.Quote(s)
}
