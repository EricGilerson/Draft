package deploy

import (
	"context"
	"log"

	"Draft/internal/githooks"
)

// DeleteProject removes a project and every service in it: stops containers,
// removes images/routes, deletes all node/env/settings/deployment rows, the
// project-level env vars, and the project row itself. Draft-managed Docker
// volumes are intentionally left in place (consistent with per-service delete)
// so data isn't destroyed by a project removal — they surface as orphans in
// the Volumes tab and can be reclaimed or deleted there.
func (e *Engine) DeleteProject(ctx context.Context, projectID uint) error {
	nodes, err := e.store.ListNodes(projectID)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := e.DeleteService(ctx, n.ID); err != nil {
			// Keep going: a single service failing to tear down shouldn't block
			// project removal. The store cleanup below removes its rows.
			log.Printf("[deploy] delete service %s during project delete: %v", n.ID, err)
		}
	}
	if err := e.store.DeleteProject(projectID); err != nil {
		return err
	}
	// Reconcile git hooks repo-wide: removing all of a repo's Draft nodes may
	// leave installed hooks with no remaining subscribers.
	_ = githooks.ReconcileAllHooks(ctx, e.store)
	return nil
}
