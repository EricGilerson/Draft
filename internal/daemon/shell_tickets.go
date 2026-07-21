package daemon

import (
	"sync"
	"time"
)

const shellTicketTTL = 60 * time.Second

// shellTicket is a short-lived, single-use credential for /exec/attach.
// Minted over header-authenticated HTTP so the long-lived daemon token never
// appears in a browser WebSocket URL.
type shellTicket struct {
	NodeID    string
	Shell     string
	ExpiresAt time.Time
}

type shellTicketStore struct {
	mu      sync.Mutex
	tickets map[string]shellTicket
}

func newShellTicketStore() *shellTicketStore {
	st := &shellTicketStore{tickets: make(map[string]shellTicket)}
	go st.purgeLoop()
	return st
}

func (st *shellTicketStore) purgeLoop() {
	ticker := time.NewTicker(shellTicketTTL)
	defer ticker.Stop()
	for range ticker.C {
		st.mu.Lock()
		st.purgeExpiredLocked(time.Now().UTC())
		st.mu.Unlock()
	}
}

func (st *shellTicketStore) mint(nodeID, shell string) (ticket string, expiresAt time.Time, err error) {
	token, err := randomToken()
	if err != nil {
		return "", time.Time{}, err
	}
	expiresAt = time.Now().UTC().Add(shellTicketTTL)
	st.mu.Lock()
	defer st.mu.Unlock()
	st.purgeExpiredLocked(time.Now().UTC())
	st.tickets[token] = shellTicket{
		NodeID:    nodeID,
		Shell:     shell,
		ExpiresAt: expiresAt,
	}
	return token, expiresAt, nil
}

// consume validates and removes a ticket. nodeID must match the mint.
func (st *shellTicketStore) consume(ticket, nodeID string) bool {
	if ticket == "" || nodeID == "" {
		return false
	}
	now := time.Now().UTC()
	st.mu.Lock()
	defer st.mu.Unlock()
	st.purgeExpiredLocked(now)
	t, ok := st.tickets[ticket]
	if !ok {
		return false
	}
	delete(st.tickets, ticket)
	if t.ExpiresAt.Before(now) {
		return false
	}
	return t.NodeID == nodeID
}

func (st *shellTicketStore) purgeExpiredLocked(now time.Time) {
	for k, t := range st.tickets {
		if t.ExpiresAt.Before(now) {
			delete(st.tickets, k)
		}
	}
}
