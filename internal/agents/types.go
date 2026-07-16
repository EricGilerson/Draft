package agents

// AgentID identifies a supported coding-agent CLI.
type AgentID string

const (
	AgentClaude   AgentID = "claude"
	AgentCodex    AgentID = "codex"
	AgentCursor   AgentID = "cursor"
	AgentGemini   AgentID = "gemini"
	AgentGrok     AgentID = "grok"
	AgentOpenCode AgentID = "opencode"
	AgentCopilot  AgentID = "copilot"
)

const DraftMCPServerName = "draft"

// AgentInfo is a detected (or known-but-missing) agent on the machine.
type AgentInfo struct {
	ID              AgentID `json:"id"`
	Name            string  `json:"name"`
	Binary          string  `json:"binary"`
	Path            string  `json:"path"`
	Version         string  `json:"version,omitempty"`
	Installed       bool    `json:"installed"`
	SupportsEphemeral bool  `json:"supportsEphemeral"`
	MCPConfigured   bool    `json:"mcpConfigured"`
	Error           string  `json:"error,omitempty"`
}

// StartSessionRequest starts an agent in a new Agents-tab session.
type StartSessionRequest struct {
	AgentID   AgentID `json:"agentId"`
	Cwd       string  `json:"cwd"`
	Ephemeral bool    `json:"ephemeral"`
	// Plain skips Draft MCP inject/ensure entirely — run the agent like a normal terminal.
	// Any MCP the user already configured outside Draft may still load; we just don't touch it.
	Plain bool `json:"plain"`
	Cols  uint16 `json:"cols"`
	Rows  uint16 `json:"rows"`
}

// SessionInfo is a running (or recently exited) agent session.
type SessionInfo struct {
	ID        string  `json:"id"`
	AgentID   AgentID `json:"agentId"`
	AgentName string  `json:"agentName"`
	Cwd       string  `json:"cwd"`
	Ephemeral bool    `json:"ephemeral"`
	Plain     bool    `json:"plain"`
	StartedAt int64   `json:"startedAt"`
	Status    string  `json:"status"` // starting | running | restarting | exited | error
	ExitCode  *int    `json:"exitCode,omitempty"`
	Error     string  `json:"error,omitempty"`
	Command   string  `json:"command,omitempty"`
}

// OutputEvent is emitted to the frontend as agent:session:output.
type OutputEvent struct {
	SessionID string `json:"sessionId"`
	Data      string `json:"data"` // base64 of raw PTY bytes
}

// ExitEvent is emitted as agent:session:exit.
type ExitEvent struct {
	SessionID string `json:"sessionId"`
	ExitCode  int    `json:"exitCode"`
	Error     string `json:"error,omitempty"`
	// Restarting is true when Draft will relaunch this session in place
	// (e.g. Codex exited after a self-update). The frontend should not treat
	// this as a final exit (no "[session exited]" banner).
	Restarting bool `json:"restarting,omitempty"`
}
