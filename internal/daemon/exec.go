package daemon

import (
	"context"
	"io"
	"log"
	"net/http"

	"github.com/gorilla/websocket"
)

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

	// WS -> container stdin. Each message's payload is written verbatim to the
	// hijacked TTY stream. A close-control frame ends the session.
	go func() {
		defer cancel()
		for {
			_, payload, err := ws.ReadMessage()
			if err != nil {
				return
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
