package daemon

import (
	"context"
	"log"
	"time"
)

// reconcileSandboxLifecycle keeps lifecycle state honest even after Draft was
// closed at the warning/expiry boundary. Deletion is intentionally not driven
// by this loop yet: the eventual purge policy must also remove sandbox-owned
// volumes, unlike normal environment deletion which preserves them.
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
