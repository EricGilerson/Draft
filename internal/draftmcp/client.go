package draftmcp

import (
	"context"

	"Draft/internal/daemon"
)

// getClient deliberately goes through daemon.Ensure: MCP must never read the
// Draft database or bypass the daemon's authorization and lifecycle boundary.
func getClient(ctx context.Context) (*daemon.Client, error) {
	return daemon.Ensure(ctx)
}
