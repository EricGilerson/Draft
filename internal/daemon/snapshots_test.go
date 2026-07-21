package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"Draft/internal/store"
)

// Regression: active deployment snapshots must write to the SSE response, not
// the hub channel. Filling the hub first used to deadlock handleEvents when
// many live deployments each emitted two snapshot events.
func TestEventsStreamSnapshotsWithFullHub(t *testing.T) {
	srv, s, _ := newTestServer(t)
	ts := newIPv4Server(t, srv.routes())

	for i := 0; i < 80; i++ {
		if _, err := s.CreateDeployment(&store.Deployment{
			NodeID:    fmt.Sprintf("svc-%d", i),
			ProjectID: 1,
			Status:    "running",
			HostPort:  3000 + i,
		}); err != nil {
			t.Fatal(err)
		}
	}

	ch, unsub := srv.hub.subscribe()
	defer unsub()
	for len(ch) < cap(ch) {
		srv.hub.publish("fill", len(ch))
	}
	if len(ch) != cap(ch) {
		t.Fatalf("hub not full: %d/%d", len(ch), cap(ch))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ts.URL+"/events", nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header.Set(tokenHeader, srv.state.Token)

	done := make(chan error, 1)
	go func() {
		resp, err := ts.Client().Do(req)
		if err != nil {
			done <- err
			return
		}
		defer resp.Body.Close()
		scanner := bufio.NewScanner(resp.Body)
		saw := 0
		for scanner.Scan() {
			line := scanner.Text()
			if !strings.HasPrefix(line, "data: ") {
				continue
			}
			var ev Event
			if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
				done <- err
				return
			}
			if strings.HasPrefix(ev.Name, "deploy:status") {
				saw++
				if saw >= 2 {
					done <- nil
					return
				}
			}
		}
		done <- scanner.Err()
	}()

	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("snapshot stream failed: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("timed out — snapshots likely blocked on full hub channel")
	}
}
