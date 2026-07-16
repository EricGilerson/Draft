package agents

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

func grokMCPConfigured(ctx context.Context, agentBin, draftCmd string, draftArgs []string) (bool, error) {
	cmd := exec.CommandContext(ctx, agentBin, "mcp", "list")
	cmd.Env = childEnv()
	out, err := cmd.CombinedOutput()
	if err == nil && strings.Contains(strings.ToLower(string(out)), DraftMCPServerName) {
		return true, nil
	}
	// Grok also merges Claude/Cursor configs — treat those as configured.
	if ok, _ := claudeConfigHasDraft(draftCmd, draftArgs); ok {
		return true, nil
	}
	if ok, _ := cursorMCPConfigured(draftCmd, draftArgs); ok {
		return true, nil
	}
	path, err := homeFile(".grok", "config.toml")
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
	return strings.Contains(string(data), "mcp_servers."+DraftMCPServerName) ||
		strings.Contains(string(data), "[mcp_servers."+DraftMCPServerName+"]"), nil
}

func grokMCPAdd(ctx context.Context, agentBin, draftCmd string, draftArgs []string) error {
	args := []string{"mcp", "add", DraftMCPServerName, "--", draftCmd}
	args = append(args, draftArgs...)
	cmd := exec.CommandContext(ctx, agentBin, args...)
	cmd.Env = childEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		// Fallback: write ~/.grok/config.toml section (same shape as Codex).
		if ferr := mergeGrokTOML(draftCmd, draftArgs); ferr == nil {
			return nil
		}
		return fmt.Errorf("grok mcp add: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func mergeGrokTOML(draftCmd string, draftArgs []string) error {
	// Reuse Codex TOML merger with grok path.
	path, err := homeFile(".grok", "config.toml")
	if err != nil {
		return err
	}
	// Temporarily write via shared helper by copying mergeCodexTOML pattern.
	_ = path
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}
	section := "[mcp_servers." + DraftMCPServerName + "]"
	if strings.Contains(existing, section) {
		existing = stripTOMLSection(existing, section)
	}
	var b strings.Builder
	b.WriteString(strings.TrimRight(existing, "\n"))
	if b.Len() > 0 {
		b.WriteString("\n\n")
	}
	b.WriteString(section)
	b.WriteString("\n")
	b.WriteString(fmt.Sprintf("command = %q\n", draftCmd))
	b.WriteString("args = [")
	for i, a := range draftArgs {
		if i > 0 {
			b.WriteString(", ")
		}
		b.WriteString(fmt.Sprintf("%q", a))
	}
	b.WriteString("]\n")
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(b.String()), 0o644)
}
