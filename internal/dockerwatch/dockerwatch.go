// Package dockerwatch maintains a single, long-lived connection to the local
// Docker daemon's event stream and fans events out to registered subscribers.
//
// It is deliberately Wails-agnostic: the hub speaks plain Go (channels are
// internal, delivery is via callbacks), so the main package can bridge it to
// Wails runtime events without this package importing any UI concerns.
//
// Today the only consumer is the daemon up/down status indicator, so we only
// surface daemon liveness. The stream itself already carries container, image,
// network and volume lifecycle events — future consumers (the React Flow node
// canvas, port manager, .env sync) register via Subscribe and filter on
// Event.Kind / Event.Raw without opening their own connections.
package dockerwatch

import (
	"context"
	"sync"
	"time"

	"github.com/docker/docker/api/types/events"
	"github.com/docker/docker/client"
)

// reconnectDelay is how long we wait before retrying the connection while the
// daemon is unreachable. This is the *only* polling in the system: while the
// daemon is up the event stream blocks with zero requests; we retry solely to
// detect the daemon coming back (nothing emits events while it is off).
const reconnectDelay = 3 * time.Second

// DaemonStatus describes the reachability of the local Docker daemon.
type DaemonStatus struct {
	// State is one of: "running" | "stopped".
	State string `json:"state"`
	// APIVersion is the negotiated daemon API version when running.
	APIVersion string `json:"apiVersion,omitempty"`
	// Error holds the underlying connection error when stopped.
	Error string `json:"error,omitempty"`
}

// Event is a normalized item delivered to subscribers.
type Event struct {
	// Kind mirrors the Docker event type ("container", "image", "network", …)
	// or "daemon" for synthetic daemon up/down transitions.
	Kind string
	// Daemon is set when Kind == "daemon".
	Daemon *DaemonStatus
	// Raw is the underlying Docker event, set for stream-sourced events.
	Raw *events.Message
}

// Hub owns the single Docker event stream and broadcasts to subscribers.
type Hub struct {
	mu          sync.RWMutex
	subscribers map[int]func(Event)
	nextID      int
	last        DaemonStatus
}

// New creates an unstarted Hub. Call Run (typically in a goroutine) to begin.
func New() *Hub {
	return &Hub{subscribers: make(map[int]func(Event))}
}

// Subscribe registers a handler and returns an unsubscribe function. If a
// daemon status is already known, it is delivered to the new subscriber
// immediately so late subscribers don't have to wait for the next change.
func (h *Hub) Subscribe(fn func(Event)) func() {
	h.mu.Lock()
	id := h.nextID
	h.nextID++
	h.subscribers[id] = fn
	last := h.last
	h.mu.Unlock()

	if last.State != "" {
		fn(Event{Kind: "daemon", Daemon: &last})
	}

	return func() {
		h.mu.Lock()
		delete(h.subscribers, id)
		h.mu.Unlock()
	}
}

// CurrentDaemon returns the last known daemon status for synchronous queries.
func (h *Hub) CurrentDaemon() DaemonStatus {
	h.mu.RLock()
	defer h.mu.RUnlock()
	return h.last
}

func (h *Hub) broadcast(ev Event) {
	h.mu.RLock()
	subs := make([]func(Event), 0, len(h.subscribers))
	for _, fn := range h.subscribers {
		subs = append(subs, fn)
	}
	h.mu.RUnlock()

	for _, fn := range subs {
		fn(ev)
	}
}

// setDaemon records a new daemon status and broadcasts only on change.
func (h *Hub) setDaemon(s DaemonStatus) {
	h.mu.Lock()
	changed := h.last.State != s.State || h.last.APIVersion != s.APIVersion
	h.last = s
	h.mu.Unlock()

	if changed {
		h.broadcast(Event{Kind: "daemon", Daemon: &s})
	}
}

// Run maintains the event stream until ctx is cancelled. While connected it
// blocks on the stream (no polling); when the daemon is down it retries every
// reconnectDelay.
func (h *Hub) Run(ctx context.Context) {
	for ctx.Err() == nil {
		h.connectAndStream(ctx)
		if sleep(ctx, reconnectDelay) {
			return
		}
	}
}

func (h *Hub) connectAndStream(ctx context.Context) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		h.setDaemon(DaemonStatus{State: "stopped", Error: err.Error()})
		return
	}
	defer cli.Close()

	pingCtx, cancel := context.WithTimeout(ctx, 2*time.Second)
	ping, err := cli.Ping(pingCtx)
	cancel()
	if err != nil {
		h.setDaemon(DaemonStatus{State: "stopped", Error: err.Error()})
		return
	}
	h.setDaemon(DaemonStatus{State: "running", APIVersion: ping.APIVersion})

	// Long-lived push connection. Blocks here with no polling until the daemon
	// emits an event, the connection drops, or ctx is cancelled.
	msgs, errs := cli.Events(ctx, events.ListOptions{})
	for {
		select {
		case <-ctx.Done():
			return
		case msg := <-msgs:
			m := msg
			h.broadcast(Event{Kind: string(m.Type), Raw: &m})
		case err := <-errs:
			st := DaemonStatus{State: "stopped"}
			if err != nil {
				st.Error = err.Error()
			}
			h.setDaemon(st)
			return
		}
	}
}

// sleep waits for d or ctx cancellation; returns true if ctx was cancelled.
func sleep(ctx context.Context, d time.Duration) bool {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return true
	case <-t.C:
		return false
	}
}
