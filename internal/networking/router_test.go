package networking

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Draft/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := store.FileDSN(filepath.Join(t.TempDir(), "test.db"))
	s, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

// newTestRouter creates a Router that won't touch the real hosts file.
func newTestRouter(t *testing.T, s *store.Store) *Router {
	t.Helper()
	r := NewRouter(s, "127.0.0.1:0")
	r.syncHostsTo = func(entries []HostsEntry) error { return nil }
	return r
}

func TestRouterRegisterHTTP(t *testing.T) {
	s := openTestStore(t)
	r := newTestRouter(t, s)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	result, err := r.Register(RegisterRequest{
		Service:    "api",
		Project:    "myapp",
		ProjectID:  1,
		NodeID:     "svc-0001",
		UID:        "a3f2",
		Protocol:   "http",
		TargetHost: "127.0.0.1",
		TargetPort: urlPort(t, upstream.URL),
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	wantHostname := "api.myapp.default.a3f2.draft.local"
	if result.Hostname != wantHostname {
		t.Errorf("Hostname = %q, want %q", result.Hostname, wantHostname)
	}
	if result.HostPort != 0 {
		t.Errorf("HostPort = %d, want 0 for HTTP", result.HostPort)
	}

	// Verify the proxy actually routes to the upstream
	req, _ := http.NewRequest("GET", "http://"+r.proxy.Addr()+"/", nil)
	req.Host = wantHostname

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("proxy request: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "ok" {
		t.Errorf("proxy response = %q, want 'ok'", body)
	}

	// Verify the route is persisted in the database
	route, err := r.Lookup(wantHostname)
	if err != nil {
		t.Fatalf("Lookup: %v", err)
	}
	if route.Protocol != "http" {
		t.Errorf("Protocol = %q, want http", route.Protocol)
	}
	if route.NodeID != "svc-0001" {
		t.Errorf("NodeID = %q, want svc-0001", route.NodeID)
	}
}

func TestRouterRegisterTCP(t *testing.T) {
	s := openTestStore(t)
	r := newTestRouter(t, s)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	result, err := r.Register(RegisterRequest{
		Service:    "postgres",
		Project:    "myapp",
		ProjectID:  1,
		NodeID:     "db-0001",
		UID:        "a3f2",
		Protocol:   "tcp",
		TargetHost: "127.0.0.1",
		TargetPort: 5432,
		PreferPort: 15432,
	})
	if err != nil {
		t.Fatalf("Register: %v", err)
	}

	wantHostname := "postgres.myapp.default.a3f2.draft.local"
	if result.Hostname != wantHostname {
		t.Errorf("Hostname = %q, want %q", result.Hostname, wantHostname)
	}
	if result.HostPort == 0 {
		t.Error("HostPort should be assigned for TCP")
	}

	// TCP services should NOT be in the proxy routing table
	if r.proxy.HasRoute(wantHostname) {
		t.Error("TCP route should not be in HTTP proxy")
	}

	// Port should be leased in the database
	lease, err := s.GetPortLease(result.HostPort)
	if err != nil {
		t.Fatalf("GetPortLease: %v", err)
	}
	if lease.NodeID != "db-0001" {
		t.Errorf("lease NodeID = %q, want db-0001", lease.NodeID)
	}
}

func TestRouterRegisterPreferredPortTaken(t *testing.T) {
	s := openTestStore(t)
	r := newTestRouter(t, s)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	r1, err := r.Register(RegisterRequest{
		Service: "db1", Project: "p1", ProjectID: 1, NodeID: "n1", UID: "aaaa",
		Protocol: "tcp", TargetHost: "127.0.0.1", TargetPort: 5432, PreferPort: 15432,
	})
	if err != nil {
		t.Fatal(err)
	}

	r2, err := r.Register(RegisterRequest{
		Service: "db2", Project: "p1", ProjectID: 1, NodeID: "n2", UID: "aaaa",
		Protocol: "tcp", TargetHost: "127.0.0.1", TargetPort: 5432, PreferPort: 15432,
	})
	if err != nil {
		t.Fatal(err)
	}

	if r2.HostPort == r1.HostPort {
		t.Errorf("second service got same port %d — should have been reassigned", r2.HostPort)
	}
	if r2.HostPort == 0 {
		t.Error("second service should still get a port")
	}
}

func TestRouterUnregisterHTTP(t *testing.T) {
	s := openTestStore(t)
	r := newTestRouter(t, s)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	result, _ := r.Register(RegisterRequest{
		Service: "api", Project: "myapp", ProjectID: 1, NodeID: "n1", UID: "a3f2",
		Protocol: "http", TargetHost: "127.0.0.1", TargetPort: urlPort(t, upstream.URL),
	})

	if err := r.Unregister(result.Hostname); err != nil {
		t.Fatalf("Unregister: %v", err)
	}

	if r.proxy.HasRoute(result.Hostname) {
		t.Error("route should be removed from proxy after Unregister")
	}
	if _, err := r.Lookup(result.Hostname); err == nil {
		t.Error("route should be deleted from database after Unregister")
	}
}

func TestRouterUnregisterTCPFreesPort(t *testing.T) {
	s := openTestStore(t)
	r := newTestRouter(t, s)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	result, _ := r.Register(RegisterRequest{
		Service: "redis", Project: "myapp", ProjectID: 1, NodeID: "n1", UID: "a3f2",
		Protocol: "tcp", TargetHost: "127.0.0.1", TargetPort: 6379, PreferPort: 16379,
	})

	port := result.HostPort
	if err := r.Unregister(result.Hostname); err != nil {
		t.Fatal(err)
	}

	if _, err := s.GetPortLease(port); err == nil {
		t.Error("port lease should be deleted after Unregister")
	}
}

func TestRouterUnregisterNode(t *testing.T) {
	s := openTestStore(t)
	r := newTestRouter(t, s)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		w.Write([]byte("ok"))
	}))
	defer upstream.Close()

	r.Register(RegisterRequest{
		Service: "api", Project: "myapp", ProjectID: 1, NodeID: "n1", UID: "a3f2",
		Protocol: "http", TargetHost: "127.0.0.1", TargetPort: urlPort(t, upstream.URL),
		Environment: "default",
	})
	r.Register(RegisterRequest{
		Service: "api", Project: "myapp", ProjectID: 1, NodeID: "n1", UID: "a3f2",
		Protocol: "tcp", TargetHost: "127.0.0.1", TargetPort: 5432,
		Environment: "staging",
	})
	r.Register(RegisterRequest{
		Service: "web", Project: "myapp", ProjectID: 1, NodeID: "n2", UID: "a3f2",
		Protocol: "http", TargetHost: "127.0.0.1", TargetPort: urlPort(t, upstream.URL),
	})

	if err := r.UnregisterNode("n1"); err != nil {
		t.Fatal(err)
	}

	n1Routes, _ := s.ListRoutesByNode("n1")
	if len(n1Routes) != 0 {
		t.Errorf("n1 should have 0 routes, got %d", len(n1Routes))
	}

	n2Routes, _ := s.ListRoutesByNode("n2")
	if len(n2Routes) != 1 {
		t.Errorf("n2 should have 1 route, got %d", len(n2Routes))
	}
}

func TestRouterReloadOnStart(t *testing.T) {
	s := openTestStore(t)

	s.CreateRoute(&store.Route{
		Hostname:    "api.myapp.default.a3f2.draft.local",
		ProjectID:   1,
		NodeID:      "n1",
		Environment: "default",
		Protocol:    "http",
		TargetHost:  "127.0.0.1",
		TargetPort:  3000,
	})

	r := newTestRouter(t, s)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	if !r.proxy.HasRoute("api.myapp.default.a3f2.draft.local") {
		t.Error("proxy should have the pre-existing route after Start")
	}
}

func TestRouterDefaultEnvironment(t *testing.T) {
	s := openTestStore(t)
	r := newTestRouter(t, s)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	result, err := r.Register(RegisterRequest{
		Service: "api", Project: "myapp", ProjectID: 1, NodeID: "n1", UID: "a3f2",
		Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}

	if result.Hostname != "api.myapp.default.a3f2.draft.local" {
		t.Errorf("Hostname = %q, should default to 'default' environment", result.Hostname)
	}
}

func TestRouterHostsFileSync(t *testing.T) {
	s := openTestStore(t)
	r := NewRouter(s, "127.0.0.1:0")

	hostsPath := filepath.Join(t.TempDir(), "hosts")
	os.WriteFile(hostsPath, []byte("127.0.0.1  localhost\n"), 0o644)

	r.syncHostsTo = func(entries []HostsEntry) error {
		return syncHostsFileAt(hostsPath, entries)
	}

	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	r.Register(RegisterRequest{
		Service: "api", Project: "myapp", ProjectID: 1, NodeID: "n1", UID: "a3f2",
		Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3000,
	})

	data, _ := os.ReadFile(hostsPath)
	content := string(data)
	if !strings.Contains(content, "api.myapp.default.a3f2.draft.local") {
		t.Error("hosts file should contain the registered hostname")
	}
	if !strings.Contains(content, "127.0.0.1  localhost") {
		t.Error("existing hosts content should be preserved")
	}

	r.Unregister("api.myapp.default.a3f2.draft.local")
	data, _ = os.ReadFile(hostsPath)
	content = string(data)
	if strings.Contains(content, "api.myapp.default.a3f2.draft.local") {
		t.Error("hostname should be removed from hosts file after Unregister")
	}
}

func TestRouterMultipleProjectsIsolated(t *testing.T) {
	s := openTestStore(t)
	r := newTestRouter(t, s)
	if err := r.Start(); err != nil {
		t.Fatal(err)
	}
	defer r.Stop()

	r1, err := r.Register(RegisterRequest{
		Service: "api", Project: "app-one", ProjectID: 1, NodeID: "n1", UID: "aaaa",
		Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3000,
	})
	if err != nil {
		t.Fatal(err)
	}
	r2, err := r.Register(RegisterRequest{
		Service: "api", Project: "app-two", ProjectID: 2, NodeID: "n2", UID: "bbbb",
		Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 4000,
	})
	if err != nil {
		t.Fatal(err)
	}

	if r1.Hostname == r2.Hostname {
		t.Error("different projects should produce different hostnames")
	}

	// Unregistering one shouldn't affect the other
	r.Unregister(r1.Hostname)
	if _, err := r.Lookup(r2.Hostname); err != nil {
		t.Error("project 2's route should survive project 1's unregister")
	}
}
