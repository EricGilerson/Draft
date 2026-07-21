package networking

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"io"
	"math/big"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
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

func TestProxyTLSRoutingUsesSNILeaf(t *testing.T) {
	upstream := newIPv4Server(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { _, _ = w.Write([]byte("secure upstream")) }))
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	rootTemplate := &x509.Certificate{SerialNumber: bigOne(), Subject: pkix.Name{CommonName: "test Draft CA"}, NotBefore: time.Now().Add(-time.Hour), NotAfter: time.Now().Add(time.Hour), IsCA: true, BasicConstraintsValid: true, KeyUsage: x509.KeyUsageCertSign}
	der, err := x509.CreateCertificate(rand.Reader, rootTemplate, rootTemplate, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	root, err := x509.ParseCertificate(der)
	if err != nil {
		t.Fatal(err)
	}
	ca := &LocalCA{cert: root, key: key, leaf: make(map[string]*tlsCertificate)}
	host := "api.myapp.default.a3f2.draft"
	p := NewProxy("127.0.0.1:0")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	p.SetRoute(host, ProxyTarget{Host: "127.0.0.1", Port: urlPort(t, upstream.URL)})
	if err := p.StartTLS("127.0.0.1:0", func(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
		if hello.ServerName != host || !p.HasRoute(hello.ServerName) {
			t.Fatalf("unexpected SNI %q", hello.ServerName)
		}
		certPEM, keyPEM, err := ca.certificatePEM(hello.ServerName)
		if err != nil {
			return nil, err
		}
		cert, err := tls.X509KeyPair(certPEM, keyPEM)
		return &cert, err
	}); err != nil {
		t.Fatal(err)
	}
	pool := x509.NewCertPool()
	pool.AddCert(root)
	client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{RootCAs: pool}, DialContext: func(_ context.Context, _, _ string) (net.Conn, error) { return net.Dial("tcp", p.TLSAddr()) }}}
	resp, err := client.Get("https://" + host + ":38474/")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	body, _ := io.ReadAll(resp.Body)
	if string(body) != "secure upstream" {
		t.Fatalf("body = %q", body)
	}
}

func bigOne() *big.Int { return big.NewInt(1) }

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

	if resp.StatusCode != http.StatusNotFound {
		t.Errorf("status = %d, want 404", resp.StatusCode)
	}
	body, _ := io.ReadAll(resp.Body)
	if want := `no route for host "unknown.host.draft.local"`; !strings.Contains(string(body), want) {
		t.Errorf("body = %q, want substring %q", body, want)
	}
}

func TestProxyRedirectsKnownHTTPRouteToTrustedHTTPS(t *testing.T) {
	p := NewProxy("127.0.0.1:0")
	if err := p.Start(); err != nil {
		t.Fatal(err)
	}
	defer p.Stop()
	p.SetRoute("api.myapp.default.a3f2.draft", ProxyTarget{Host: "127.0.0.1", Port: 3000})
	p.SetHTTPSRedirect(38474)
	client := &http.Client{CheckRedirect: func(_ *http.Request, _ []*http.Request) error { return http.ErrUseLastResponse }}
	req, _ := http.NewRequest("GET", "http://"+p.Addr()+"/hello?draft=yes", nil)
	req.Host = "api.myapp.default.a3f2.draft"
	resp, err := client.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusPermanentRedirect {
		t.Fatalf("status = %d", resp.StatusCode)
	}
	if got, want := resp.Header.Get("Location"), "https://api.myapp.default.a3f2.draft:38474/hello?draft=yes"; got != want {
		t.Fatalf("Location = %q, want %q", got, want)
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
