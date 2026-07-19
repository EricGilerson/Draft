package deploy

import (
	"strings"
	"time"

	"Draft/internal/networking"
)

const sandboxProxyTouchMinInterval = time.Minute

// NoteProxyHostAccess records sandbox activity when the reverse proxy serves
// a request for a sandbox hostname. Throttled per node to avoid DB chatter.
func (e *Engine) NoteProxyHostAccess(hostname string) {
	hostname = strings.TrimSpace(hostname)
	if hostname == "" || e.store == nil {
		return
	}
	internal := networking.InternalHostnameFromAlias(hostname)
	parsed := networking.ParseHostname(internal)
	if parsed == nil || !parsed.Sandbox {
		return
	}
	route, err := e.store.GetRoute(internal)
	if err != nil || route == nil || route.NodeID == "" {
		return
	}
	node, err := e.store.GetNode(route.NodeID)
	if err != nil || node == nil {
		return
	}
	sandbox, err := e.store.GetSandboxByEnvironment(node.EnvironmentID)
	if err != nil || sandbox == nil {
		return
	}
	if sandbox.Status != "active" && sandbox.Status != "warning" {
		return
	}

	now := time.Now().UTC()
	e.activityMu.Lock()
	if last, ok := e.lastProxyTouch[route.NodeID]; ok && now.Sub(last) < sandboxProxyTouchMinInterval {
		e.activityMu.Unlock()
		return
	}
	e.lastProxyTouch[route.NodeID] = now
	e.activityMu.Unlock()

	_ = e.store.TouchSandboxActivity(sandbox.ID, now)
}
