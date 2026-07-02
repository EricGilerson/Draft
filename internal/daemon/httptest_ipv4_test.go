package daemon

import (
	"net"
	"net/http"
	"net/http/httptest"
	"testing"
)

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
