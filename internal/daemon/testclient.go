package daemon

import (
	"net"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Draft/internal/deploy"
	"Draft/internal/dockerwatch"
	"Draft/internal/networking"
	"Draft/internal/store"
)

// NewTestClient starts an in-process daemon HTTP API with Docker disabled and
// returns an authenticated Client plus the backing store. Intended for MCP and
// daemon integration tests.
func NewTestClient(t *testing.T) (*Client, *store.Store) {
	t.Helper()
	srv, st, _ := newTestServer(t)
	ts := newIPv4Server(t, srv.routes())
	return clientForHTTPServer(t, ts, srv.state.Token), st
}

func newTestServer(t *testing.T) (*Server, *store.Store, string) {
	t.Helper()
	s, err := store.Open(store.FileDSN(filepath.Join(t.TempDir(), "draft.db")))
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })

	logDir := filepath.Join(t.TempDir(), "logs")
	hub := newEventHub()
	engine := deploy.New(s, networking.NewRouter(s, "127.0.0.1:0"), logDir, hub.publish)
	srv := &Server{
		store:         s,
		engine:        engine,
		hub:           hub,
		watch:         dockerwatch.New(),
		state:         State{Token: "test-token", PID: os.Getpid()},
		disableDocker: true,
		disableIdle:   true,
	}
	return srv, s, logDir
}

func clientForHTTPServer(t *testing.T, ts *httptest.Server, token string) *Client {
	t.Helper()
	addr := strings.TrimPrefix(ts.URL, "http://")
	return &Client{
		state: State{Addr: addr, Token: token, PID: os.Getpid()},
		http:  ts.Client(),
	}
}

func newIPv4Server(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()

	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen on IPv4 loopback: %v", err)
	}

	ts := httptest.NewUnstartedServer(handler)
	ts.Listener = ln
	ts.Start()
	t.Cleanup(ts.Close)
	return ts
}
