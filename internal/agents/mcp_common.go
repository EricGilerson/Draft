package agents

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// DraftMCPCommand returns the absolute executable and args to run Draft's MCP server.
func DraftMCPCommand() (command string, args []string, err error) {
	exe, err := os.Executable()
	if err != nil {
		return "", nil, err
	}
	exe, err = filepath.EvalSymlinks(exe)
	if err != nil {
		// Non-fatal; use the unresolved path.
		exe, _ = os.Executable()
	}
	return exe, []string{"mcp"}, nil
}

// mcpServerEntry is the shared JSON shape used by Claude/Cursor/Gemini-style configs.
type mcpServerEntry struct {
	Command string            `json:"command"`
	Args    []string          `json:"args,omitempty"`
	Env     map[string]string `json:"env,omitempty"`
	URL     string            `json:"url,omitempty"`
	Type    string            `json:"type,omitempty"`
}

func sameMCPCommand(entry mcpServerEntry, command string, args []string) bool {
	if entry.URL != "" {
		return false
	}
	if !samePath(entry.Command, command) {
		return false
	}
	return equalStringSlices(entry.Args, args)
}

func samePath(a, b string) bool {
	a = filepath.Clean(a)
	b = filepath.Clean(b)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

func equalStringSlices(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func homeFile(parts ...string) (string, error) {
	home, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(append([]string{home}, parts...)...), nil
}

func readJSONFile(path string, dest any) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return fmt.Errorf("empty config")
	}
	return json.Unmarshal(data, dest)
}

func writeJSONFile(path string, v any) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	data, err := json.MarshalIndent(v, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return os.WriteFile(path, data, 0o644)
}

func draftEntry(command string, args []string) mcpServerEntry {
	return mcpServerEntry{
		Command: command,
		Args:    args,
	}
}
