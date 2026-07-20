package agents

import (
	"context"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"Draft/internal/executil"
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
	// Prefer cursor-agent: both Cursor and Grok ship an `agent` binary on PATH.
	{ID: AgentCursor, Name: "Cursor Agent", Binaries: []string{"cursor-agent", "agent"}, SupportsEphemeral: false},
	{ID: AgentGemini, Name: "Gemini CLI", Binaries: []string{"gemini"}, SupportsEphemeral: false},
	{ID: AgentGrok, Name: "Grok", Binaries: []string{"grok"}, SupportsEphemeral: false},
	{ID: AgentOpenCode, Name: "OpenCode", Binaries: []string{"opencode"}, SupportsEphemeral: false},
	{ID: AgentCopilot, Name: "GitHub Copilot", Binaries: []string{"copilot"}, SupportsEphemeral: false},
}

// ListAgents probes PATH for known agent CLIs and whether Draft MCP is configured.
// Agents are detected in parallel — version and MCP probes are the slow part.
func ListAgents(ctx context.Context) ([]AgentInfo, error) {
	draftCmd, draftArgs, err := DraftMCPCommand()
	if err != nil {
		// Still list agents; MCP checks may fail.
		draftCmd = ""
		_ = draftArgs
	}

	out := make([]AgentInfo, len(knownAgents))
	var wg sync.WaitGroup
	for i, spec := range knownAgents {
		wg.Add(1)
		go func(i int, spec agentSpec) {
			defer wg.Done()
			out[i] = detectAgent(ctx, spec, draftCmd, draftArgs)
		}(i, spec)
	}
	wg.Wait()
	return out, nil
}

func detectAgent(ctx context.Context, spec agentSpec, draftCmd string, draftArgs []string) AgentInfo {
	info := AgentInfo{
		ID:                spec.ID,
		Name:              spec.Name,
		SupportsEphemeral: spec.SupportsEphemeral,
	}
	path, bin := resolveBinary(ctx, spec)
	if path == "" {
		info.Binary = spec.Binaries[0]
		info.Installed = false
		return info
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
	return info
}

func resolveBinary(ctx context.Context, spec agentSpec) (path, used string) {
	for _, name := range spec.Binaries {
		for _, candidate := range lookPathAll(name) {
			if !binaryMatchesAgent(ctx, spec.ID, name, candidate) {
				continue
			}
			return candidate, name
		}
	}
	return "", ""
}

// binaryMatchesAgent rejects PATH collisions where another product owns the same
// command name (notably Cursor vs Grok both exposing `agent`).
func binaryMatchesAgent(ctx context.Context, id AgentID, binaryName, path string) bool {
	switch id {
	case AgentCursor:
		return matchesCursorBinary(ctx, binaryName, path)
	case AgentGrok:
		return matchesGrokBinary(path)
	default:
		return true
	}
}

func matchesCursorBinary(ctx context.Context, binaryName, path string) bool {
	p := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	base := agentBaseName(path)

	// Grok Build installs ~/.grok/bin/agent — never treat it as Cursor.
	if strings.Contains(p, "/.grok/") || strings.Contains(p, "/grok/bin/") {
		return false
	}

	if base == "cursor-agent" || strings.Contains(p, "/cursor-agent/") {
		return true
	}

	if binaryName == "agent" || base == "agent" {
		ver := strings.ToLower(probeVersion(ctx, path))
		if ver == "" {
			return false
		}
		if strings.Contains(ver, "grok") {
			return false
		}
		return true
	}

	return true
}

func matchesGrokBinary(path string) bool {
	p := strings.ToLower(filepath.ToSlash(filepath.Clean(path)))
	base := agentBaseName(path)
	if strings.Contains(p, "/cursor-agent/") || strings.Contains(base, "cursor") {
		return false
	}
	return base == "grok" || strings.Contains(p, "/.grok/") || strings.Contains(p, "/grok/bin/")
}

func agentBaseName(path string) string {
	base := strings.ToLower(filepath.Base(path))
	for _, ext := range []string{".exe", ".cmd", ".bat", ".ps1", ".com"} {
		if strings.HasSuffix(base, ext) {
			return strings.TrimSuffix(base, ext)
		}
	}
	return base
}

// lookPathAll returns every augmented-PATH hit for name (not just the first),
// so we can skip a colliding earlier shim (Grok's agent) and still find Cursor.
func lookPathAll(name string) []string {
	if name == "" {
		return nil
	}
	aug := AugmentedPATH()
	var exts []string
	if runtime.GOOS == "windows" {
		exts = windowsPathExts()
		lower := strings.ToLower(name)
		for _, ext := range exts {
			if strings.HasSuffix(lower, ext) {
				exts = []string{""}
				break
			}
		}
	} else {
		exts = []string{""}
	}

	seen := map[string]bool{}
	var out []string
	for _, dir := range filepath.SplitList(aug) {
		if dir == "" {
			continue
		}
		for _, ext := range exts {
			candidate := filepath.Join(dir, name+ext)
			st, err := os.Stat(candidate)
			if err != nil || st.IsDir() {
				continue
			}
			key := strings.ToLower(filepath.Clean(candidate))
			if seen[key] {
				continue
			}
			seen[key] = true
			out = append(out, candidate)
		}
	}
	return out
}

func windowsPathExts() []string {
	raw := os.Getenv("PATHEXT")
	if raw == "" {
		return []string{".com", ".exe", ".bat", ".cmd"}
	}
	parts := filepath.SplitList(raw)
	out := make([]string, 0, len(parts))
	seen := map[string]bool{}
	for _, p := range parts {
		ext := strings.ToLower(strings.TrimSpace(p))
		if ext == "" || seen[ext] {
			continue
		}
		seen[ext] = true
		out = append(out, ext)
	}
	if len(out) == 0 {
		return []string{".com", ".exe", ".bat", ".cmd"}
	}
	return out
}

func probeVersion(ctx context.Context, bin string) string {
	cctx, cancel := context.WithTimeout(ctx, 2*time.Second)
	defer cancel()
	cmd := executil.CommandContext(cctx, bin, "--version")
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
