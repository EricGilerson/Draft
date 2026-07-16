package main

import (
	"encoding/base64"
	"errors"

	"Draft/internal/agents"
)

// ListAgents probes PATH for Claude Code, Codex, Cursor Agent, Gemini, and Grok.
func (a *App) ListAgents() ([]agents.AgentInfo, error) {
	return agents.ListAgents(a.ctx)
}

// StartAgentSession ensures Draft MCP (ephemeral or persistent), then opens a PTY session.
func (a *App) StartAgentSession(req agents.StartSessionRequest) (*agents.SessionInfo, error) {
	if a.agentManager == nil {
		return nil, errors.New("agent manager is not available")
	}
	return a.agentManager.Start(a.ctx, req)
}

// ListAgentSessions returns live Agents-tab sessions.
func (a *App) ListAgentSessions() ([]agents.SessionInfo, error) {
	if a.agentManager == nil {
		return nil, nil
	}
	return a.agentManager.List(), nil
}

// StopAgentSession kills a session and removes it from the tab list.
func (a *App) StopAgentSession(sessionID string) error {
	if a.agentManager == nil {
		return errors.New("agent manager is not available")
	}
	return a.agentManager.Stop(sessionID)
}

// WriteAgentSession writes raw keystrokes (base64) to the session PTY.
func (a *App) WriteAgentSession(sessionID string, dataBase64 string) error {
	if a.agentManager == nil {
		return errors.New("agent manager is not available")
	}
	raw, err := base64.StdEncoding.DecodeString(dataBase64)
	if err != nil {
		return err
	}
	return a.agentManager.Write(sessionID, raw)
}

// ResizeAgentSession updates the PTY size for a session.
func (a *App) ResizeAgentSession(sessionID string, cols, rows uint16) error {
	if a.agentManager == nil {
		return errors.New("agent manager is not available")
	}
	return a.agentManager.Resize(sessionID, cols, rows)
}
