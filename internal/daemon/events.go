package daemon

import "sync"

type Event struct {
	Name string `json:"name"`
	Data any    `json:"data"`
}

type eventHub struct {
	mu   sync.RWMutex
	subs map[chan Event]struct{}
}

func newEventHub() *eventHub {
	return &eventHub{subs: make(map[chan Event]struct{})}
}

func (h *eventHub) subscribe() (chan Event, func()) {
	ch := make(chan Event, 128)
	h.mu.Lock()
	h.subs[ch] = struct{}{}
	h.mu.Unlock()
	return ch, func() {
		h.mu.Lock()
		delete(h.subs, ch)
		close(ch)
		h.mu.Unlock()
	}
}

func (h *eventHub) publish(name string, data any) {
	ev := Event{Name: name, Data: data}
	h.mu.RLock()
	defer h.mu.RUnlock()
	for ch := range h.subs {
		select {
		case ch <- ev:
		default:
		}
	}
}
