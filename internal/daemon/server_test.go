package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Draft/internal/deploy"
	"Draft/internal/dockerwatch"
	"Draft/internal/networking"
	"Draft/internal/store"
)

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

func TestServerAuthAllowsHealthAndProtectsOtherRoutes(t *testing.T) {
	srv, _, _ := newTestServer(t)
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()

	resp, err := ts.Client().Get(ts.URL + "/health")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("health status = %d, want 200", resp.StatusCode)
	}
	resp.Body.Close()

	resp, err = ts.Client().Get(ts.URL + "/deployments?nodeId=n1")
	if err != nil {
		t.Fatal(err)
	}
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated deployments status = %d, want 401", resp.StatusCode)
	}
	resp.Body.Close()
}

func TestClientDeploymentAndBuildLogEndpoints(t *testing.T) {
	srv, s, logDir := newTestServer(t)
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()
	c := clientForHTTPServer(t, ts, srv.state.Token)

	dep, err := s.CreateDeployment(&store.Deployment{
		NodeID:    "svc1",
		ProjectID: 1,
		Status:    "running",
		ImageTag:  "draft-test:1",
		HostPort:  43210,
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(logDir, fmt.Sprintf("%d.log", dep.ID)), []byte("line 1\nline 2\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	deps, err := c.GetDeployments(context.Background(), "svc1")
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 1 || deps[0].ID != dep.ID {
		t.Fatalf("deployments = %+v, want deployment %d", deps, dep.ID)
	}

	active, err := c.GetActiveDeployment(context.Background(), "svc1")
	if err != nil {
		t.Fatal(err)
	}
	if active == nil || active.ID != dep.ID {
		t.Fatalf("active = %+v, want deployment %d", active, dep.ID)
	}

	log, err := c.GetBuildLog(context.Background(), dep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if log != "line 1\nline 2\n" {
		t.Fatalf("build log = %q", log)
	}
}

func TestClientLocalDomainStatusEndpoint(t *testing.T) {
	srv, s, _ := newTestServer(t)
	srv.router = networking.NewRouter(s, "127.0.0.1:54321")
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()
	c := clientForHTTPServer(t, ts, srv.state.Token)

	status, err := c.LocalDomainStatus(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if status.Mode != "hostname-port" {
		t.Fatalf("Mode = %q, want hostname-port; status = %+v", status.Mode, status)
	}
	if status.ProxyPort != 54321 {
		t.Fatalf("ProxyPort = %d, want 54321; status = %+v", status.ProxyPort, status)
	}
}

func TestClientCommandsReachServer(t *testing.T) {
	srv, s, _ := newTestServer(t)
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()
	c := clientForHTTPServer(t, ts, srv.state.Token)

	if _, err := s.CreateDeployment(&store.Deployment{NodeID: "svc1", ProjectID: 1, Status: "building"}); err != nil {
		t.Fatal(err)
	}
	if err := c.Stop(context.Background(), "svc1"); err != nil {
		t.Fatal(err)
	}
	active, err := s.ActiveDeployment("svc1")
	if err != nil {
		t.Fatal(err)
	}
	if active != nil {
		t.Fatalf("expected no active deployment after stop, got %+v", active)
	}
}

func TestEventsStreamEmitsActiveSnapshotsAndLiveEvents(t *testing.T) {
	srv, s, _ := newTestServer(t)
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()
	c := clientForHTTPServer(t, ts, srv.state.Token)

	if _, err := s.CreateDeployment(&store.Deployment{
		NodeID:    "svc1",
		ProjectID: 1,
		Status:    "running",
		HostPort:  3000,
	}); err != nil {
		t.Fatal(err)
	}

	req, err := http.NewRequest(http.MethodGet, ts.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(tokenHeader, c.state.Token)
	resp, err := ts.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()

	scanner := bufio.NewScanner(resp.Body)
	deadline := time.After(2 * time.Second)
	for {
		select {
		case <-deadline:
			t.Fatal("timed out waiting for active deployment snapshot")
		default:
		}
		if !scanner.Scan() {
			t.Fatal("event stream closed before snapshot")
		}
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev Event
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			t.Fatal(err)
		}
		if ev.Name == "deploy:status:svc1" {
			return
		}
	}
}

func TestClientSubscribeEventsDecodesServerSentEvents(t *testing.T) {
	srv, _, _ := newTestServer(t)
	ts := httptest.NewServer(srv.routes())
	defer ts.Close()
	c := clientForHTTPServer(t, ts, srv.state.Token)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	got := make(chan string, 1)
	errCh := make(chan error, 1)
	go func() {
		errCh <- c.SubscribeEvents(ctx, func(event string, data any) {
			if event == "build:log" {
				got <- event
				cancel()
			}
		})
	}()

	waitForSubscribers(t, srv.hub, 1)
	srv.hub.publish("build:log", map[string]any{"nodeId": "svc1"})

	select {
	case event := <-got:
		if event != "build:log" {
			t.Fatalf("event = %q, want build:log", event)
		}
	case err := <-errCh:
		if err != nil {
			t.Fatalf("subscribe returned early: %v", err)
		}
		t.Fatal("subscribe returned before event")
	case <-time.After(2 * time.Second):
		t.Fatal("timed out waiting for decoded event")
	}
}

func waitForSubscribers(t *testing.T, hub *eventHub, want int) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		hub.mu.RLock()
		got := len(hub.subs)
		hub.mu.RUnlock()
		if got >= want {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d event subscribers", want)
}

func TestServerRunWritesStateAndClientCanPing(t *testing.T) {
	cfgDir := t.TempDir()
	oldConfigDir := userConfigDir
	userConfigDir = func() (string, error) { return cfgDir, nil }
	t.Cleanup(func() { userConfigDir = oldConfigDir })

	srv, _, _ := newTestServer(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() { errCh <- srv.Run(ctx) }()

	var c *Client
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		var err error
		c, err = NewClientFromState()
		if err == nil && c.Ping(context.Background()) == nil {
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	if c == nil {
		t.Fatal("daemon state was not written")
	}
	if err := c.Ping(context.Background()); err != nil {
		t.Fatalf("ping daemon: %v", err)
	}

	cancel()
	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("server run returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("server did not shut down")
	}
}
