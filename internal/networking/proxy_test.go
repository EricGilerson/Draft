package networking

import (
	"io"
	"net/http"
	"net/url"
	"strconv"
	"testing"
)

func TestProxyRouting(t *testing.T) {
	upstream := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Write([]byte("hello from upstream"))
	}))

	p := NewProxy("127.0.0.1:0")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	p.SetRoute("api.myapp.default.a3f2.draft.local", ProxyTarget{
		Host: "127.0.0.1",
		Port: urlPort(t, upstream.URL),
	})

	req, _ := http.NewRequest("GET", "http://"+p.server.Addr+"/test", nil)
	req.Host = "api.myapp.default.a3f2.draft.local"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)

	if string(body) != "hello from upstream" {
		t.Errorf("body = %q, want 'hello from upstream'", body)
	}
}

func TestProxyNoRoute(t *testing.T) {
	p := NewProxy("127.0.0.1:0")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()

	req, _ := http.NewRequest("GET", "http://"+p.server.Addr+"/", nil)
	req.Host = "unknown.host.draft.local"

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("status = %d, want 502", resp.StatusCode)
	}
}

func TestProxySetRemoveRoute(t *testing.T) {
	p := NewProxy("127.0.0.1:0")
	p.SetRoute("a.draft.local", ProxyTarget{Host: "127.0.0.1", Port: 3000})
	if !p.HasRoute("a.draft.local") {
		t.Error("route should exist")
	}
	p.RemoveRoute("a.draft.local")
	if p.HasRoute("a.draft.local") {
		t.Error("route should be removed")
	}
}

func TestProxyRoutesSnapshot(t *testing.T) {
	p := NewProxy("127.0.0.1:0")
	p.SetRoute("a.draft.local", ProxyTarget{Host: "127.0.0.1", Port: 3000})
	p.SetRoute("b.draft.local", ProxyTarget{Host: "127.0.0.1", Port: 4000})

	routes := p.Routes()
	if len(routes) != 2 {
		t.Errorf("len(Routes()) = %d, want 2", len(routes))
	}

	delete(routes, "a.draft.local")
	if !p.HasRoute("a.draft.local") {
		t.Error("deleting from snapshot should not affect proxy")
	}
}

func urlPort(t *testing.T, rawURL string) int {
	t.Helper()
	u, err := url.Parse(rawURL)
	if err != nil {
		t.Fatalf("parse URL %q: %v", rawURL, err)
	}
	port, err := strconv.Atoi(u.Port())
	if err != nil {
		t.Fatalf("parse port from %q: %v", rawURL, err)
	}
	return port
}
