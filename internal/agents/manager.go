package agents

import (
	"context"
	"encoding/base64"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
)

const (
	recentOutputCap   = 8 * 1024
	maxAutoRestarts   = 3
	restartWindow     = 2 * time.Minute
	restartSettleDelay = 800 * time.Millisecond
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

	mu       sync.Mutex
	pty      ptySession
	closed   atomic.Bool
	stopping atomic.Bool
	lastCols uint16
	lastRows uint16

	req StartSessionRequest

	recentMu sync.Mutex
	recent   []byte

	readerWG sync.WaitGroup

	restartMu    sync.Mutex
	restartTimes []time.Time
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
	req.Cols = cols
	req.Rows = rows

	id := uuid.NewString()
	sessInfo := SessionInfo{
		ID:        id,
		AgentID:   info.ID,
		AgentName: info.Name,
		Cwd:       plan.Cwd,
		Ephemeral: plan.Ephemeral,
		Plain:     plan.Plain,
		StartedAt: time.Now().Unix(),
		Status:    "starting",
		Command:   plan.Display,
	}

	pty, err := startPTY(plan, cols, rows)
	if err != nil {
		return nil, fmt.Errorf("start PTY: %w", err)
	}

	s := &Session{
		Info:     sessInfo,
		pty:      pty,
		lastCols: cols,
		lastRows: rows,
		req:      req,
	}
	s.Info.Status = "running"

	m.mu.Lock()
	m.sessions[id] = s
	m.mu.Unlock()

	m.startIO(s)

	cp := s.Info
	return &cp, nil
}

func (m *Manager) startIO(s *Session) {
	s.readerWG.Add(1)
	go func() {
		defer s.readerWG.Done()
		m.readLoop(s)
	}()
	go m.waitLoop(s)
}

func (m *Manager) readLoop(s *Session) {
	buf := make([]byte, 32*1024)
	var (
		mu             sync.Mutex
		pending        []byte
		flushScheduled bool
	)

	flush := func() {
		mu.Lock()
		data := pending
		pending = nil
		flushScheduled = false
		mu.Unlock()
		if len(data) == 0 || m.onOutput == nil {
			return
		}
		m.onOutput(OutputEvent{
			SessionID: s.Info.ID,
			Data:      base64.StdEncoding.EncodeToString(data),
		})
	}

	scheduleFlush := func() {
		mu.Lock()
		if flushScheduled {
			mu.Unlock()
			return
		}
		flushScheduled = true
		mu.Unlock()
		time.AfterFunc(16*time.Millisecond, flush)
	}

	for {
		if s.closed.Load() || s.stopping.Load() {
			flush()
			return
		}
		n, err := s.pty.Read(buf)
		if n > 0 {
			s.noteOutput(buf[:n])
			mu.Lock()
			pending = append(pending, buf[:n]...)
			large := len(pending) >= 24*1024
			mu.Unlock()
			if large {
				flush()
			} else {
				scheduleFlush()
			}
		}
		if err != nil {
			flush()
			return
		}
	}
}

func (s *Session) noteOutput(chunk []byte) {
	s.recentMu.Lock()
	defer s.recentMu.Unlock()
	s.recent = append(s.recent, chunk...)
	if len(s.recent) > recentOutputCap {
		s.recent = append([]byte(nil), s.recent[len(s.recent)-recentOutputCap:]...)
	}
}

func (s *Session) recentOutput() []byte {
	s.recentMu.Lock()
	defer s.recentMu.Unlock()
	if len(s.recent) == 0 {
		return nil
	}
	out := make([]byte, len(s.recent))
	copy(out, s.recent)
	return out
}

func (s *Session) clearRecent() {
	s.recentMu.Lock()
	s.recent = nil
	s.recentMu.Unlock()
}

func (s *Session) canAutoRestart() bool {
	s.restartMu.Lock()
	defer s.restartMu.Unlock()
	now := time.Now()
	kept := s.restartTimes[:0]
	for _, t := range s.restartTimes {
		if now.Sub(t) < restartWindow {
			kept = append(kept, t)
		}
	}
	s.restartTimes = kept
	return len(s.restartTimes) < maxAutoRestarts
}

func (s *Session) recordAutoRestart() {
	s.restartMu.Lock()
	s.restartTimes = append(s.restartTimes, time.Now())
	s.restartMu.Unlock()
}

func (m *Manager) waitLoop(s *Session) {
	code, err := s.pty.Wait()

	stopping := s.stopping.Load()
	recent := s.recentOutput()
	wantRestart := !stopping &&
		err == nil &&
		shouldAutoRestart(s.Info.AgentID, code, recent) &&
		s.canAutoRestart()

	if wantRestart {
		if m.tryRestart(s, code) {
			return
		}
	}

	s.closed.Store(true)
	_ = s.pty.Close()
	s.readerWG.Wait()

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

func (m *Manager) tryRestart(s *Session, priorCode int) bool {
	s.recordAutoRestart()

	s.mu.Lock()
	s.Info.Status = "restarting"
	s.mu.Unlock()

	if m.onExit != nil {
		m.onExit(ExitEvent{
			SessionID:  s.Info.ID,
			ExitCode:   priorCode,
			Restarting: true,
		})
	}
	m.emitText(s, "\r\n\r\n── Draft: restarting after update ──\r\n\r\n")

	s.closed.Store(true)
	_ = s.pty.Close()
	s.readerWG.Wait()

	time.Sleep(restartSettleDelay)

	if s.stopping.Load() {
		s.mu.Lock()
		s.Info.Status = "exited"
		s.mu.Unlock()
		return false
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()

	req := s.req
	req.Cols = s.lastCols
	req.Rows = s.lastRows
	plan, info, err := BuildLaunch(ctx, req)
	if err != nil {
		s.mu.Lock()
		s.Info.Status = "error"
		s.Info.Error = "restart after update failed: " + err.Error()
		code := priorCode
		s.Info.ExitCode = &code
		ev := ExitEvent{SessionID: s.Info.ID, ExitCode: priorCode, Error: s.Info.Error}
		s.mu.Unlock()
		if m.onExit != nil {
			m.onExit(ev)
		}
		return true // handled; do not fall through to duplicate exit
	}

	pty, err := startPTY(plan, req.Cols, req.Rows)
	if err != nil {
		s.mu.Lock()
		s.Info.Status = "error"
		s.Info.Error = "restart after update failed: " + err.Error()
		code := priorCode
		s.Info.ExitCode = &code
		ev := ExitEvent{SessionID: s.Info.ID, ExitCode: priorCode, Error: s.Info.Error}
		s.mu.Unlock()
		if m.onExit != nil {
			m.onExit(ev)
		}
		return true
	}

	s.clearRecent()
	s.closed.Store(false)
	s.mu.Lock()
	s.pty = pty
	s.Info.Status = "running"
	s.Info.ExitCode = nil
	s.Info.Error = ""
	s.Info.Command = plan.Display
	s.Info.AgentName = info.Name
	s.Info.Ephemeral = plan.Ephemeral
	s.Info.Plain = plan.Plain
	s.Info.Cwd = plan.Cwd
	s.mu.Unlock()

	m.startIO(s)
	return true
}

func (m *Manager) emitText(s *Session, text string) {
	if m.onOutput == nil || text == "" {
		return
	}
	m.onOutput(OutputEvent{
		SessionID: s.Info.ID,
		Data:      base64.StdEncoding.EncodeToString([]byte(text)),
	})
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
	if s.closed.Load() || s.stopping.Load() {
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
	if s.closed.Load() || s.stopping.Load() {
		return nil
	}
	if cols < 2 || rows < 2 {
		return nil
	}
	s.mu.Lock()
	if s.lastCols == cols && s.lastRows == rows {
		s.mu.Unlock()
		return nil
	}
	s.lastCols = cols
	s.lastRows = rows
	s.mu.Unlock()
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
	s.stopping.Store(true)
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
