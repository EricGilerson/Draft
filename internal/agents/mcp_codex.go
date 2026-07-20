package agents

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"Draft/internal/executil"
)

func codexMCPConfigured(ctx context.Context, agentBin, draftCmd string, draftArgs []string) (bool, error) {
	cmd := executil.CommandContext(ctx, agentBin, "mcp", "list")
	cmd.Env = childEnv()
	out, err := cmd.CombinedOutput()
	if err == nil {
		text := strings.ToLower(string(out))
		if strings.Contains(text, DraftMCPServerName) {
			return true, nil
		}
	}
	return codexConfigHasDraft(draftCmd, draftArgs)
}

func codexConfigHasDraft(draftCmd string, draftArgs []string) (bool, error) {
	path, err := homeFile(".codex", "config.toml")
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
	text := string(data)
	section := "[mcp_servers." + DraftMCPServerName + "]"
	if !strings.Contains(text, section) && !strings.Contains(text, "[mcp_servers."+DraftMCPServerName+"]") {
		// also accept quoted keys
		if !strings.Contains(text, "mcp_servers."+DraftMCPServerName) {
			return false, nil
		}
	}
	// Best-effort: presence of section is enough; command path may vary by install.
	_ = draftCmd
	_ = draftArgs
	return strings.Contains(text, section) || strings.Contains(strings.ToLower(text), "mcp_servers."+DraftMCPServerName), nil
}

func codexMCPAdd(ctx context.Context, agentBin, draftCmd string, draftArgs []string) error {
	args := []string{"mcp", "add", DraftMCPServerName, "--", draftCmd}
	args = append(args, draftArgs...)
	cmd := executil.CommandContext(ctx, agentBin, args...)
	cmd.Env = childEnv()
	out, err := cmd.CombinedOutput()
	if err != nil {
		if ferr := mergeCodexTOML(draftCmd, draftArgs); ferr == nil {
			return nil
		}
		return fmt.Errorf("codex mcp add: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	return nil
}

func mergeCodexTOML(draftCmd string, draftArgs []string) error {
	path, err := homeFile(".codex", "config.toml")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	existing := ""
	if data, err := os.ReadFile(path); err == nil {
		existing = string(data)
	}
	section := "[mcp_servers." + DraftMCPServerName + "]"
	if strings.Contains(existing, section) {
		// Replace whole section naively: strip old draft section then append.
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
	return os.WriteFile(path, []byte(b.String()), 0o644)
}

func stripTOMLSection(text, header string) string {
	lines := strings.Split(text, "\n")
	var out []string
	skip := false
	for _, line := range lines {
		trim := strings.TrimSpace(line)
		if strings.HasPrefix(trim, "[") && strings.HasSuffix(trim, "]") {
			skip = trim == header
		}
		if skip {
			continue
		}
		out = append(out, line)
	}
	return strings.Join(out, "\n")
}
