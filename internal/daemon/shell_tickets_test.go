package daemon

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestShellTicketMintAndConsume(t *testing.T) {
	st := newShellTicketStore()
	ticket, expires, err := st.mint("node-1", "bash")
	if err != nil {
		t.Fatal(err)
	}
	if ticket == "" {
		t.Fatal("expected ticket")
	}
	if expires.Before(time.Now().UTC()) {
		t.Fatal("ticket already expired")
	}
	if !st.consume(ticket, "node-1") {
		t.Fatal("expected consume to succeed")
	}
	if st.consume(ticket, "node-1") {
		t.Fatal("ticket must be single-use")
	}
}

func TestShellTicketRejectsWrongNode(t *testing.T) {
	st := newShellTicketStore()
	ticket, _, err := st.mint("node-1", "")
	if err != nil {
		t.Fatal(err)
	}
	if st.consume(ticket, "node-other") {
		t.Fatal("wrong nodeId must fail")
	}
	// Wrong-node attempt still burns the ticket (single-use).
	if st.consume(ticket, "node-1") {
		t.Fatal("ticket should already be consumed after wrong-node attempt")
	}
}

func TestShellTicketExpired(t *testing.T) {
	st := newShellTicketStore()
	ticket, _, err := st.mint("node-1", "")
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	entry := st.tickets[ticket]
	entry.ExpiresAt = time.Now().UTC().Add(-time.Second)
	st.tickets[ticket] = entry
	st.mu.Unlock()
	if st.consume(ticket, "node-1") {
		t.Fatal("expired ticket must fail")
	}
}

func TestShellTicketPurgeExpiredLocked(t *testing.T) {
	st := newShellTicketStore()
	ticket, _, err := st.mint("node-1", "")
	if err != nil {
		t.Fatal(err)
	}
	st.mu.Lock()
	entry := st.tickets[ticket]
	entry.ExpiresAt = time.Now().UTC().Add(-time.Second)
	st.tickets[ticket] = entry
	st.purgeExpiredLocked(time.Now().UTC())
	_, ok := st.tickets[ticket]
	st.mu.Unlock()
	if ok {
		t.Fatal("expired ticket should be purged")
	}
}

func TestExecAttachRejectsDaemonTokenInQuery(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.shellTickets = newShellTicketStore()
	ts := newIPv4Server(t, srv.routes())

	// Long-lived daemon token in ?token= must not authorize attach.
	resp, err := ts.Client().Get(ts.URL + "/exec/attach?nodeId=n1&token=" + srv.state.Token)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401 (daemon token in query rejected)", resp.StatusCode)
	}
}

func TestExecTicketRequiresHeaderAndAuthorizesAttach(t *testing.T) {
	srv, _, _ := newTestServer(t)
	srv.shellTickets = newShellTicketStore()
	ts := newIPv4Server(t, srv.routes())

	resp, err := ts.Client().Post(ts.URL+"/exec/ticket", "application/json", strings.NewReader(`{"nodeId":"n1"}`))
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.StatusCode != http.StatusUnauthorized {
		t.Fatalf("unauthenticated ticket status = %d, want 401", resp.StatusCode)
	}

	c := clientForHTTPServer(t, ts, srv.state.Token)
	ticket, err := c.MintShellTicket(t.Context(), "n1", "sh")
	if err != nil {
		t.Fatalf("MintShellTicket: %v", err)
	}
	if ticket.Ticket == "" || ticket.NodeID != "n1" {
		t.Fatalf("bad ticket response: %+v", ticket)
	}

	// Valid ticket reaches the handler (which then fails on missing container —
	// anything other than 401 proves auth passed).
	resp, err = ts.Client().Get(ts.URL + "/exec/attach?nodeId=n1&ticket=" + ticket.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode == http.StatusUnauthorized {
		t.Fatal("valid ticket should authorize /exec/attach")
	}

	resp2, err := ts.Client().Get(ts.URL + "/exec/attach?nodeId=n1&ticket=" + ticket.Ticket)
	if err != nil {
		t.Fatal(err)
	}
	defer resp2.Body.Close()
	if resp2.StatusCode != http.StatusUnauthorized {
		t.Fatalf("reused ticket status = %d, want 401", resp2.StatusCode)
	}
}
