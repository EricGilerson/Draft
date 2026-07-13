package daemon

import (
	"context"
	"log"
	"time"
)

// reconcileSandboxLifecycle keeps lifecycle state honest even after Draft was
// closed at the warning/expiry boundary. After the grace window ends it purges
// the sandbox, including Draft-managed volumes (more destructive than normal
// environment deletion, which preserves volumes for reclaim).
func (s *Server) reconcileSandboxLifecycle(ctx context.Context) {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case now := <-ticker.C:
			if err := s.engine.ReconcileSandboxLifecycle(ctx, now.UTC()); err != nil {
				log.Printf("[draft-daemon] sandbox lifecycle reconcile: %v", err)
			}
		}
	}
}
