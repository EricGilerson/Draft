package networking

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strconv"
	"sync"

	"Draft/internal/store"
)

var ErrInvalidRestoreRoute = errors.New("restore route requires hostname, project ID, node ID, and target port")

// Router is the high-level coordinator for Draft's local networking. It owns
// the reverse proxy, manages port leases, generates hostnames, and keeps the
// host-facing route table. Consumers call Register/Unregister and the Router
// handles the rest.
type Router struct {
	store       *store.Store
	proxy       *Proxy
	syncHostsTo func([]HostsEntry) error
	mu          sync.RWMutex
	hostsError  string
}

type LocalDomainStatus struct {
	ProxyAddr       string `json:"proxyAddr"`
	ProxyPort       int    `json:"proxyPort"`
	ProxyOnDefault  bool   `json:"proxyOnDefault"`
	HostsConfigured bool   `json:"hostsConfigured"`
	HostsError      string `json:"hostsError"`
	Mode            string `json:"mode"` // public-hostname-port|localhost-port
	PublicSuffix    string `json:"publicSuffix"`
	LoopbackSuffix  string `json:"loopbackSuffix"` // Deprecated: use PublicSuffix.
}

// NewRouter creates a Router backed by the given store. The proxy listens on
// proxyAddr (e.g. "127.0.0.1:8080").
func NewRouter(s *store.Store, proxyAddr string) *Router {
	return &Router{
		store:       s,
		proxy:       NewProxy(proxyAddr),
		syncHostsTo: SyncHostsFile,
	}
}

// Start boots the reverse proxy and reloads persisted routes into memory.
func (r *Router) Start() error {
	if err := r.proxy.Start(); err != nil {
		return err
	}
	if err := r.reloadRoutes(); err != nil {
		return err
	}
	return nil
}

// Stop shuts down the proxy gracefully.
func (r *Router) Stop() error {
	return r.proxy.Stop()
}

// RegisterRequest describes a service that needs a hostname and (for TCP) a
// host port.
type RegisterRequest struct {
	Service     string
	Project     string
	ProjectID   uint
	NodeID      string
	Environment string
	UID         string // 4-char hex, generated at project creation
	Protocol    string // "http" or "tcp"
	TargetHost  string // container-reachable host (e.g. "127.0.0.1")
	TargetPort  int    // port inside the container
	PreferPort  int    // preferred host port for TCP (0 = auto-assign)
}

// RegisterResult is returned after a successful registration.
type RegisterResult struct {
	Hostname string
	HostPort int // 0 for HTTP services (they go through the proxy)
}

// Register creates a route for a service: generates the internal hostname,
// allocates a host port (TCP only), persists to the database, and updates the
// proxy routing table.
func (r *Router) Register(req RegisterRequest) (*RegisterResult, error) {
	env := req.Environment
	if env == "" {
		env = "default"
	}
	protocol := req.Protocol
	if protocol == "" {
		protocol = "http"
	}

	hostname := Hostname(req.Service, req.Project, env, req.UID)

	var hostPort int
	if protocol == "tcp" {
		excluded, err := r.leasedPorts()
		if err != nil {
			return nil, fmt.Errorf("list leased ports: %w", err)
		}

		port, err := FindFreePortNear(req.PreferPort, excluded)
		if err != nil {
			return nil, err
		}
		hostPort = port

		if _, err := r.store.CreatePortLease(&store.PortLease{
			Port:      port,
			ProjectID: req.ProjectID,
			NodeID:    req.NodeID,
		}); err != nil {
			return nil, fmt.Errorf("create port lease: %w", err)
		}
	}

	route := &store.Route{
		Hostname:    hostname,
		ProjectID:   req.ProjectID,
		NodeID:      req.NodeID,
		Environment: env,
		Protocol:    protocol,
		TargetHost:  req.TargetHost,
		TargetPort:  req.TargetPort,
		HostPort:    hostPort,
	}
	if _, err := r.store.CreateRoute(route); err != nil {
		return nil, fmt.Errorf("create route: %w", err)
	}

	if protocol == "http" {
		r.setHTTPRouteAliases(hostname, ProxyTarget{Host: req.TargetHost, Port: req.TargetPort})
	}

	return &RegisterResult{
		Hostname: hostname,
		HostPort: hostPort,
	}, nil
}

// RestoreHTTPRoute recreates a known HTTP route from durable deployment state.
// It is used during daemon startup reconciliation when Docker still has the
// container but the in-memory proxy table needs to be rebuilt.
func (r *Router) RestoreHTTPRoute(hostname string, projectID uint, nodeID string, targetHost string, targetPort int) error {
	if hostname == "" || projectID == 0 || nodeID == "" || targetPort == 0 {
		return ErrInvalidRestoreRoute
	}
	if _, err := r.store.GetRoute(hostname); err != nil {
		if _, createErr := r.store.CreateRoute(&store.Route{
			Hostname:   hostname,
			ProjectID:  projectID,
			NodeID:     nodeID,
			Protocol:   "http",
			TargetHost: targetHost,
			TargetPort: targetPort,
		}); createErr != nil {
			return createErr
		}
	}
	r.setHTTPRouteAliases(hostname, ProxyTarget{Host: targetHost, Port: targetPort})
	return nil
}

// Unregister removes a service's route and frees its port lease.
func (r *Router) Unregister(hostname string) error {
	route, err := r.store.GetRoute(hostname)
	if err != nil {
		return fmt.Errorf("get route: %w", err)
	}

	if route.Protocol == "tcp" && route.HostPort > 0 {
		if err := r.store.DeletePortLease(route.HostPort); err != nil {
			log.Printf("[draft-router] delete port lease %d: %v", route.HostPort, err)
		}
	}

	r.removeHTTPRouteAliases(hostname)

	if err := r.store.DeleteRoute(hostname); err != nil {
		return fmt.Errorf("delete route: %w", err)
	}

	return nil
}

// UnregisterNode removes all routes and port leases for a given node.
func (r *Router) UnregisterNode(nodeID string) error {
	routes, err := r.store.ListRoutesByNode(nodeID)
	if err != nil {
		return fmt.Errorf("list routes for node: %w", err)
	}

	for _, route := range routes {
		r.removeHTTPRouteAliases(route.Hostname)
	}

	if err := r.store.DeleteRoutesByNode(nodeID); err != nil {
		return fmt.Errorf("delete routes: %w", err)
	}
	if err := r.store.DeletePortLeasesByNode(nodeID); err != nil {
		return fmt.Errorf("delete port leases: %w", err)
	}

	return nil
}

// Lookup returns the route for a hostname, or nil if not found.
func (r *Router) Lookup(hostname string) (*store.Route, error) {
	return r.store.GetRoute(hostname)
}

func (r *Router) LocalDomainStatus() LocalDomainStatus {
	addr := r.proxy.Addr()
	port := parsePort(addr)

	r.mu.RLock()
	hostsError := r.hostsError
	r.mu.RUnlock()

	mode := "localhost-port"
	if port > 0 {
		mode = "public-hostname-port"
	}

	return LocalDomainStatus{
		ProxyAddr:       addr,
		ProxyPort:       port,
		ProxyOnDefault:  port == 80,
		HostsConfigured: false,
		HostsError:      hostsError,
		Mode:            mode,
		PublicSuffix:    PublicSuffix,
		LoopbackSuffix:  PublicSuffix,
	}
}

// reloadRoutes loads persisted routes from the database into the proxy's
// in-memory routing table. Called on startup.
func (r *Router) reloadRoutes() error {
	routes, err := r.store.ListAllRoutes()
	if err != nil {
		return fmt.Errorf("reload routes: %w", err)
	}
	for _, route := range routes {
		if route.Protocol == "http" {
			r.setHTTPRouteAliases(route.Hostname, ProxyTarget{Host: route.TargetHost, Port: route.TargetPort})
		}
	}
	log.Printf("[draft-router] loaded %d routes from database", len(routes))
	return nil
}

// syncHosts writes the current set of Draft hostnames to the system hosts file.
// It is intentionally not called by default; clean .draft.local host resolution
// should be enabled only by an explicit privileged setup flow.
func (r *Router) syncHosts() error {
	routes, err := r.store.ListAllRoutes()
	if err != nil {
		r.setHostsError(err)
		return err
	}

	entries := make([]HostsEntry, len(routes))
	for i, route := range routes {
		entries[i] = HostsEntry{
			IP:       "127.0.0.1",
			Hostname: route.Hostname,
		}
	}
	err = r.syncHostsTo(entries)
	r.setHostsError(err)
	return err
}

func (r *Router) setHostsError(err error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if err == nil {
		r.hostsError = ""
		return
	}
	r.hostsError = err.Error()
}

func parsePort(addr string) int {
	_, portStr, err := net.SplitHostPort(addr)
	if err != nil {
		return 0
	}
	port, _ := strconv.Atoi(portStr)
	return port
}

func (r *Router) setHTTPRouteAliases(hostname string, target ProxyTarget) {
	for _, alias := range HostAliases(hostname) {
		r.proxy.SetRoute(alias, target)
	}
}

func (r *Router) removeHTTPRouteAliases(hostname string) {
	for _, alias := range HostAliases(hostname) {
		r.proxy.RemoveRoute(alias)
	}
}

// leasedPorts returns a set of all currently leased ports.
func (r *Router) leasedPorts() (map[int]bool, error) {
	leases, err := r.store.ListAllPortLeases()
	if err != nil {
		return nil, err
	}
	m := make(map[int]bool, len(leases))
	for _, l := range leases {
		m[l.Port] = true
	}
	return m, nil
}
