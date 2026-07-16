package agents

import (
	"context"
	"encoding/base64"
	"fmt"
	"io"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

// OutputHandler receives base64-encoded PTY output chunks.
type OutputHandler func(OutputEvent)

// ExitHandler receives session exit notifications.
type ExitHandler func(ExitEvent)

// Manager owns live agent PTY sessions for the Agents tab.
type Manager struct {
	mu       sync.Mutex
	sessions map[string]*Session
	onOutput OutputHandler
	onExit   ExitHandler
}

// Session is one running agent process.
type Session struct {
	Info SessionInfo

	mu     sync.Mutex
	pty    ptySession
	closed atomic.Bool
}

type ptySession interface {
	Read(p []byte) (int, error)
	Write(p []byte) (int, error)
	Resize(cols, rows uint16) error
	Wait() (int, error)
	Close() error
}

// NewManager creates an empty session manager.
func NewManager(onOutput OutputHandler, onExit ExitHandler) *Manager {
	return &Manager{
		sessions: map[string]*Session{},
		onOutput: onOutput,
		onExit:   onExit,
	}
}

// Start builds a launch plan, opens a PTY, and begins streaming output.
func (m *Manager) Start(ctx context.Context, req StartSessionRequest) (*SessionInfo, error) {
	plan, info, err := BuildLaunch(ctx, req)
	if err != nil {
		return nil, err
	}
	cols, rows := req.Cols, req.Rows
	if cols == 0 {
		cols = 120
	}
	if rows == 0 {
		rows = 32
	}

	id := uuid.NewString()
	sessInfo := SessionInfo{
		ID:        id,
		AgentID:   info.ID,
		AgentName: info.Name,
		Cwd:       plan.Cwd,
		Ephemeral: plan.Ephemeral,
		StartedAt: time.Now().Unix(),
		Status:    "starting",
		Command:   plan.Display,
	}

	pty, err := startPTY(plan, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("start PTY: %w", err)
	}

	s := &Session{Info: sessInfo, pty: pty}
	s.Info.Status = "running"

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()

	go m.readLoop(s)
	go m.waitLoop(s)

	cp := s.Info
	return &cp, nil
}

func (m *Manager) readLoop(s *Session) {
	buf := make([]byte, 32*1024)
	for {
		if s.closed.Load() {
			return
		}
		n, err := s.pty.Read(buf)
		if n > 0 && m.onOutput != nil {
			m.onOutput(OutputEvent{
				SessionID: s.Info.ID,
				Data:      base64.StdEncoding.EncodeToString(buf[:n]),
			})
		}
		if err != nil {
			if err != io.EOF && !s.closed.Load() {
				// surface as exit error if wait hasn't fired yet
			}
			return
		}
	}
}

func (m *Manager) waitLoop(s *Session) {
	code, err := s.pty.Wait()
	s.closed.Store(true)
	_ = s.pty.Close()

	s.mu.Lock()
	s.Info.Status = "exited"
	s.Info.ExitCode = &code
	if err != nil {
		s.Info.Error = err.Error()
		s.Info.Status = "error"
	}
	ev := ExitEvent{SessionID: s.Info.ID, ExitCode: code, Error: s.Info.Error}
	s.mu.Unlock()

	if m.onExit != nil {
		m.onExit(ev)
	}
}

// List returns a snapshot of sessions.
func (m *Manager) List() []SessionInfo {
	m.mu.Lock()
	defer m.mu.Unlock()
	out := make([]SessionInfo, 0, len(m.sessions))
	for _, s := range m.sessions {
		s.mu.Lock()
		out = append(out, s.Info)
		s.mu.Unlock()
	}
	return out
}

// Get returns one session.
func (m *Manager) Get(id string) (*SessionInfo, error) {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return nil, fmt.Errorf("session not found")
	}
	s.mu.Lock()
	cp := s.Info
	s.mu.Unlock()
	return &cp, nil
}

// Write sends bytes to the session PTY stdin.
func (m *Manager) Write(id string, data []byte) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("session not found")
	}
	if s.closed.Load() {
		return fmt.Errorf("session exited")
	}
	_, err := s.pty.Write(data)
	return err
}

// Resize updates the PTY size.
func (m *Manager) Resize(id string, cols, rows uint16) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("session not found")
	}
	if s.closed.Load() {
		return nil
	}
	return s.pty.Resize(cols, rows)
}

// Stop terminates a session and removes it from the manager.
func (m *Manager) Stop(id string) error {
	m.mu.Lock()
	s, ok := m.sessions[id]
	if ok {
		delete(m.sessions, id)
	}
	m.mu.Unlock()
	if !ok {
		return fmt.Errorf("session not found")
	}
	s.closed.Store(true)
	return s.pty.Close()
}

// CloseAll stops every session (app shutdown).
func (m *Manager) CloseAll() {
	m.mu.Lock()
	ids := make([]string, 0, len(m.sessions))
	for id := range m.sessions {
		ids = append(ids, id)
	}
	m.mu.Unlock()
	for _, id := range ids {
		_ = m.Stop(id)
	}
}
