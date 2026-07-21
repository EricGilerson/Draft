package networking

import (
	"context"
	"crypto/tls"
	"fmt"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"time"
)

// ProxyTarget is a single upstream that the reverse proxy routes to.
type ProxyTarget struct {
	Host string // target host (e.g. "127.0.0.1")
	Port int    // target port
}

// Proxy is an HTTP reverse proxy that routes requests by Host header to the
// correct container backend. It runs on a single port and serves all
// Draft-managed HTTP services.
type Proxy struct {
	mu        sync.RWMutex
	routes    map[string]ProxyTarget // hostname → target
	server    *http.Server
	tlsServer *http.Server
	addr      string
	tlsAddr   string
	onAccess  func(hostname string)
	// httpsRedirectPort upgrades requests received by the legacy HTTP listener
	// once local HTTPS is trusted. Zero preserves plain HTTP compatibility.
	httpsRedirectPort int
}

// NewProxy creates a reverse proxy that will listen on the given address
// (e.g. "127.0.0.1:8080" or ":80").
func NewProxy(addr string) *Proxy {
	p := &Proxy{
		routes: make(map[string]ProxyTarget),
		addr:   addr,
	}

	rp := &httputil.ReverseProxy{
		Director: p.director,
		ErrorHandler: func(w http.ResponseWriter, r *http.Request, err error) {
			http.Error(w, fmt.Sprintf("Draft proxy: upstream unreachable (%v)", err), http.StatusBadGateway)
		},
	}

	p.server = &http.Server{
		Addr: addr,
		Handler: http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			host := stripPort(r.Host)
			p.mu.RLock()
			_, ok := p.routes[host]
			onAccess := p.onAccess
			httpsRedirectPort := p.httpsRedirectPort
			p.mu.RUnlock()
			if !ok {
				// Without a route the ReverseProxy director leaves URL.Scheme
				// empty, which surfaces as a confusing 502
				// ("unsupported protocol scheme \"\""). Report unknown hosts
				// clearly instead of attempting an upstream dial.
				http.Error(w, fmt.Sprintf("Draft proxy: no route for host %q", host), http.StatusNotFound)
				return
			}
			if onAccess != nil {
				onAccess(host)
			}
			if r.TLS == nil && httpsRedirectPort > 0 {
				httpsHost := host
				if httpsRedirectPort != 443 {
					httpsHost = fmt.Sprintf("%s:%d", host, httpsRedirectPort)
				}
				http.Redirect(w, r, "https://"+httpsHost+r.URL.RequestURI(), http.StatusPermanentRedirect)
				return
			}
			rp.ServeHTTP(w, r)
		}),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 60 * time.Second,
	}

	return p
}

// SetAccessHandler registers a callback invoked on each proxied request with
// the Host header (without port). Used for sandbox idle-activity tracking.
func (p *Proxy) SetAccessHandler(fn func(hostname string)) {
	p.mu.Lock()
	p.onAccess = fn
	p.mu.Unlock()
}

// director rewrites the incoming request to point at the matched upstream.
func (p *Proxy) director(req *http.Request) {
	host := stripPort(req.Host)

	p.mu.RLock()
	target, ok := p.routes[host]
	p.mu.RUnlock()

	if !ok {
		return
	}

	upstream := &url.URL{
		Scheme: "http",
		Host:   fmt.Sprintf("%s:%d", target.Host, target.Port),
	}

	req.URL.Scheme = upstream.Scheme
	req.URL.Host = upstream.Host
	req.Header.Set("X-Forwarded-Host", req.Host)
	req.Header.Set("X-Draft-Service", host)
}

// SetRoute adds or updates a hostname→target mapping.
func (p *Proxy) SetRoute(hostname string, target ProxyTarget) {
	p.mu.Lock()
	p.routes[hostname] = target
	p.mu.Unlock()
}

// RemoveRoute deletes a hostname mapping.
func (p *Proxy) RemoveRoute(hostname string) {
	p.mu.Lock()
	delete(p.routes, hostname)
	p.mu.Unlock()
}

// Routes returns a snapshot of the current routing table.
func (p *Proxy) Routes() map[string]ProxyTarget {
	p.mu.RLock()
	defer p.mu.RUnlock()
	out := make(map[string]ProxyTarget, len(p.routes))
	for k, v := range p.routes {
		out[k] = v
	}
	return out
}

// HasRoute checks whether a hostname is currently routed.
func (p *Proxy) HasRoute(hostname string) bool {
	p.mu.RLock()
	_, ok := p.routes[hostname]
	p.mu.RUnlock()
	return ok
}

// Start begins serving in the background. Returns once the listener is bound
// or an error occurs.
func (p *Proxy) Start() error {
	ln, err := net.Listen("tcp", p.addr)
	if err != nil {
		return fmt.Errorf("proxy listen on %s: %w", p.addr, err)
	}
	p.addr = ln.Addr().String()
	p.server.Addr = p.addr
	log.Printf("[draft-proxy] listening on %s", p.addr)

	go func() {
		if err := p.server.Serve(ln); err != nil && err != http.ErrServerClosed {
			log.Printf("[draft-proxy] serve error: %v", err)
		}
	}()
	return nil
}

// StartTLS exposes the same hostname-routing handler over TLS on a separate
// loopback listener. Certificates are selected per SNI name by getCertificate.
func (p *Proxy) StartTLS(addr string, getCertificate func(*tls.ClientHelloInfo) (*tls.Certificate, error)) error {
	ln, err := net.Listen("tcp", addr)
	if err != nil {
		return fmt.Errorf("TLS proxy listen on %s: %w", addr, err)
	}
	config := &tls.Config{MinVersion: tls.VersionTLS12, GetCertificate: getCertificate}
	p.tlsServer = &http.Server{Addr: addr, Handler: p.server.Handler, ReadTimeout: 30 * time.Second, WriteTimeout: 60 * time.Second}
	p.tlsAddr = ln.Addr().String()
	log.Printf("[draft-proxy] TLS listening on %s", p.tlsAddr)
	go func() {
		if err := p.tlsServer.Serve(tls.NewListener(ln, config)); err != nil && err != http.ErrServerClosed {
			log.Printf("[draft-proxy] TLS serve error: %v", err)
		}
	}()
	return nil
}

// Stop gracefully shuts down the proxy with a 5-second deadline.
func (p *Proxy) Stop() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if p.tlsServer != nil {
		if err := p.tlsServer.Shutdown(ctx); err != nil {
			return err
		}
	}
	return p.server.Shutdown(ctx)
}

// Addr returns the address the proxy is listening on. After Start(), this
// reflects the actual bound address (useful when port 0 was requested).
func (p *Proxy) Addr() string {
	return p.addr
}

func (p *Proxy) TLSAddr() string { return p.tlsAddr }

// SetHTTPSRedirect upgrades known hostnames received on the HTTP listener to
// the TLS listener. Pass zero to keep serving HTTP normally.
func (p *Proxy) SetHTTPSRedirect(port int) {
	p.mu.Lock()
	p.httpsRedirectPort = port
	p.mu.Unlock()
}

func stripPort(hostport string) string {
	host, _, err := net.SplitHostPort(hostport)
	if err != nil {
		return hostport
	}
	return host
}
