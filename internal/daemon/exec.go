package daemon

import (
	"context"
	"encoding/json"
	"io"
	"log"
	"net/http"
	"strings"
	"time"

	"github.com/gorilla/websocket"
)

// execControlMessage is a JSON control frame the client may send instead of
// raw keystrokes, e.g. {"type":"resize","cols":80,"rows":24} sent by xterm.js
// on connect and whenever the terminal is resized.
type execControlMessage struct {
	Type string `json:"type"`
	Cols uint   `json:"cols"`
	Rows uint   `json:"rows"`
}

var execUpgrader = websocket.Upgrader{
	// The daemon listens on 127.0.0.1 only and the token check already ran in
	// the auth middleware, so any origin reaching this handler is trusted.
	CheckOrigin: func(r *http.Request) bool { return true },
}

// handleExecAttach upgrades to a WebSocket and bridges it to an interactive
// exec TTY in the node's active container. The frontend (xterm.js) sends typed
// keystrokes as WS messages; the container's stdout/stderr stream back. The
// shell is chosen by the client (?shell=bash|sh|...) and defaults to sh.
func (s *Server) handleExecAttach(w http.ResponseWriter, r *http.Request) {
	nodeID := r.URL.Query().Get("nodeId")
	shell := r.URL.Query().Get("shell")
	if nodeID == "" {
		http.Error(w, "nodeId is required", http.StatusBadRequest)
		return
	}

	session, err := s.engine.ExecAttach(r.Context(), nodeID, shell)
	if err != nil {
		http.Error(w, err.Error(), http.StatusBadRequest)
		return
	}
	defer session.Close()

	ws, err := execUpgrader.Upgrade(w, r, nil)
	if err != nil {
		log.Printf("[daemon] exec ws upgrade: %v", err)
		return
	}
	defer ws.Close()

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	// WS -> container stdin. Text frames that decode as a resize control
	// message adjust the pty instead of being written to stdin; everything
	// else (raw keystrokes) is written verbatim to the hijacked TTY stream.
	go func() {
		defer cancel()
		for {
			msgType, payload, err := ws.ReadMessage()
			if err != nil {
				return
			}
			if msgType == websocket.TextMessage {
				var ctrl execControlMessage
				if json.Unmarshal(payload, &ctrl) == nil && ctrl.Type == "resize" {
					_ = session.Resize(ctx, ctrl.Cols, ctrl.Rows)
					continue
				}
			}
			if _, err := session.Conn.Write(payload); err != nil {
				return
			}
		}
	}()

	// container stdout/stderr -> WS. The hijacked conn is a single TTY stream,
	// so reads already carry merged output.
	go func() {
		defer cancel()
		buf := make([]byte, 4096)
		for {
			n, err := session.Conn.Read(buf)
			if n > 0 {
				if werr := ws.WriteMessage(websocket.TextMessage, buf[:n]); werr != nil {
					return
				}
			}
			if err != nil {
				if err != io.EOF {
					log.Printf("[daemon] exec read: %v", err)
				}
				return
			}
		}
	}()

	<-ctx.Done()
}

// execTicketRequest is the body for /exec/ticket.
type execTicketRequest struct {
	NodeID string `json:"nodeId"`
	Shell  string `json:"shell,omitempty"`
}

// execTicketResponse is returned by /exec/ticket.
type execTicketResponse struct {
	Ticket    string    `json:"ticket"`
	NodeID    string    `json:"nodeId"`
	ExpiresAt time.Time `json:"expiresAt"`
}

// handleExecTicket mints a short-lived single-use ticket for /exec/attach.
// Requires header auth (X-Draft-Token); never accepts query credentials.
func (s *Server) handleExecTicket(w http.ResponseWriter, r *http.Request) {
	var req execTicketRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	nodeID := strings.TrimSpace(req.NodeID)
	if nodeID == "" {
		http.Error(w, "nodeId is required", http.StatusBadRequest)
		return
	}
	ticket, expiresAt, err := s.ensureShellTickets().mint(nodeID, strings.TrimSpace(req.Shell))
	if err != nil {
		http.Error(w, "failed to mint ticket", http.StatusInternalServerError)
		return
	}
	writeJSON(w, execTicketResponse{Ticket: ticket, NodeID: nodeID, ExpiresAt: expiresAt})
}

// execRunRequest is the body for /exec/run: a one-shot, non-interactive command.
type execRunRequest struct {
	NodeID  string   `json:"nodeId"`
	Cmd     []string `json:"cmd"`
	WorkDir string   `json:"workDir"`
}

// execRunResult is the JSON response for /exec/run.
type execRunResult struct {
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
}

// handleExecRun runs a one-shot command in the node's active container and
// returns the captured stdout/stderr plus exit code. Used by the "Run" bar.
func (s *Server) handleExecRun(w http.ResponseWriter, r *http.Request) {
	var req execRunRequest
	if !decodeJSON(w, r, &req) {
		return
	}
	if req.NodeID == "" {
		http.Error(w, "nodeId is required", http.StatusBadRequest)
		return
	}
	if len(req.Cmd) == 0 {
		http.Error(w, "cmd is required", http.StatusBadRequest)
		return
	}
	result, err := s.engine.RunCommand(r.Context(), req.NodeID, req.Cmd, req.WorkDir)
	if err != nil {
		writeJSON(w, execRunResult{Error: err.Error()})
		return
	}
	writeJSON(w, result)
}
