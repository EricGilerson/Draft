package agents

import (
	"context"
	"os/exec"
	"strings"
	"time"
)

type agentSpec struct {
	ID                AgentID
	Name              string
	Binaries          []string
	SupportsEphemeral bool
}

var knownAgents = []agentSpec{
	{ID: AgentClaude, Name: "Claude Code", Binaries: []string{"claude"}, SupportsEphemeral: true},
	{ID: AgentCodex, Name: "Codex", Binaries: []string{"codex"}, SupportsEphemeral: true},
	{ID: AgentCursor, Name: "Cursor Agent", Binaries: []string{"agent", "cursor-agent"}, SupportsEphemeral: false},
	{ID: AgentGemini, Name: "Gemini CLI", Binaries: []string{"gemini"}, SupportsEphemeral: false},
	{ID: AgentGrok, Name: "Grok", Binaries: []string{"grok"}, SupportsEphemeral: false},
	{ID: AgentOpenCode, Name: "OpenCode", Binaries: []string{"opencode"}, SupportsEphemeral: false},
	{ID: AgentCopilot, Name: "GitHub Copilot", Binaries: []string{"copilot"}, SupportsEphemeral: false},
}

// ListAgents probes PATH for known agent CLIs and whether Draft MCP is configured.
func ListAgents(ctx context.Context) ([]AgentInfo, error) {
	draftCmd, draftArgs, err := DraftMCPCommand()
	if err != nil {
		// Still list agents; MCP checks may fail.
		draftCmd = ""
		_ = draftArgs
	}

	out := make([]AgentInfo, 0, len(knownAgents))
	for _, spec := range knownAgents {
		info := AgentInfo{
			ID:                spec.ID,
			Name:              spec.Name,
			SupportsEphemeral: spec.SupportsEphemeral,
		}
		path, bin := resolveBinary(spec.Binaries)
		if path == "" {
			info.Binary = spec.Binaries[0]
			info.Installed = false
			out = append(out, info)
			continue
		}
		info.Binary = bin
		info.Path = path
		info.Installed = true
		info.Version = probeVersion(ctx, path)
		if draftCmd != "" {
			configured, cfgErr := IsDraftMCPConfigured(ctx, spec.ID, path, draftCmd, draftArgs)
			info.MCPConfigured = configured
			if cfgErr != nil {
				info.Error = cfgErr.Error()
			}
		}
		out = append(out, info)
	}
	return out, nil
}

func resolveBinary(names []string) (path, used string) {
	for _, name := range names {
		p, err := LookPath(name)
		if err == nil && p != "" {
			return p, name
		}
	}
	return "", ""
}

func probeVersion(ctx context.Context, bin string) string {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := exec.CommandContext(cctx, bin, "--version")
	cmd.Env = childEnv()
	b, err := cmd.CombinedOutput()
	if err != nil {
		return ""
	}
	line := strings.TrimSpace(string(b))
	if i := strings.IndexByte(line, '\n'); i >= 0 {
		line = line[:i]
	}
	if len(line) > 80 {
		line = line[:80]
	}
	return line
}

func findSpec(id AgentID) (agentSpec, bool) {
	for _, s := range knownAgents {
		if s.ID == id {
			return s, true
		}
	}
	return agentSpec{}, false
}
