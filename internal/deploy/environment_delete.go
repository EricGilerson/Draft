package deploy

import (
	"context"
	"log"
)

// DeleteEnvironment tears down every node in an environment (stops
// containers, removes images/routes, deletes rows — same as DeleteService)
// and then removes the environment itself. Draft-managed Docker volumes are
// intentionally left in place, consistent with DeleteService/DeleteProject,
// so data isn't destroyed by an environment removal. Fails on the project's
// default environment or its only remaining one (store.DeleteEnvironment
// enforces this).
func (e *Engine) DeleteEnvironment(ctx context.Context, environmentID uint) error {
	nodes, err := e.store.ListNodesByEnvironment(environmentID)
	if err != nil {
		return err
	}
	for _, n := range nodes {
		if err := e.DeleteService(ctx, n.ID); err != nil {
			// Keep going: a single service failing to tear down shouldn't block
			// environment removal. The store cleanup below removes its rows.
			log.Printf("[deploy] delete service %s during environment delete: %v", n.ID, err)
		}
	}
	return e.store.DeleteEnvironment(environmentID)
}
