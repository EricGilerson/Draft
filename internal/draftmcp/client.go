package draftmcp

import (
	"context"
	"sync"

	"Draft/internal/daemon"
)

var (
	testClientMu sync.RWMutex
	testClient   *daemon.Client
)

// SetTestClient overrides daemon.Ensure for tests. Pass nil to clear.
func SetTestClient(c *daemon.Client) {
	testClientMu.Lock()
	defer testClientMu.Unlock()
	testClient = c
}

// getClient deliberately goes through daemon.Ensure: MCP must never read the
// Draft database or bypass the daemon's authorization and lifecycle boundary.
func getClient(ctx context.Context) (*daemon.Client, error) {
	testClientMu.RLock()
	c := testClient
	testClientMu.RUnlock()
	if c != nil {
		return c, nil
	}
	return daemon.Ensure(ctx)
}
