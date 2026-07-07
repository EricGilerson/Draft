package deploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
)

// RotateEnvSecret replaces a secret env var's value with a fresh strong random
// string, preserving its scope/source/envFile. If the service has a running
// deployment, it is redeployed so the new value takes effect. Returns the new
// value so the UI can surface it once to the user (it is stored, not printed
// again later).
func (e *Engine) RotateEnvSecret(ctx context.Context, nodeID, key string) (string, error) {
	nodeID = strings.TrimSpace(nodeID)
	key = strings.TrimSpace(key)
	if nodeID == "" || key == "" {
		return "", fmt.Errorf("nodeID and key are required")
	}

	existing, err := e.store.GetEnvVar(nodeID, key)
	if err != nil {
		return "", err
	}
	if !existing.Secret {
		return "", fmt.Errorf("only secret variables can be rotated")
	}

	newValue, err := generateSecretValue(32)
	if err != nil {
		return "", err
	}
	if err := e.store.SetEnvVarValue(nodeID, key, newValue); err != nil {
		return "", err
	}

	// Apply live if the service is currently running. Rotation is destructive
	// for datastores (the stored password won't match the already-initialized
	// database), so the UI must confirm before calling.
	if dep, err := e.store.ActiveDeployment(nodeID); err == nil && dep != nil && dep.Status == "running" {
		_ = e.Deploy(ctx, nodeID)
	}

	return newValue, nil
}

// generateSecretValue returns a 2*n-char hex string from crypto/rand.
func generateSecretValue(n int) (string, error) {
	b := make([]byte, n)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}

// RotateProjectEnvSecret replaces a project-level secret env var's value with a
// fresh strong random string and redeploys every currently-running service in
// the project so the new value takes effect. Returns the new value once for the
// UI to surface to the user.
func (e *Engine) RotateProjectEnvSecret(ctx context.Context, projectID uint, key string) (string, error) {
	key = strings.TrimSpace(key)
	if projectID == 0 || key == "" {
		return "", fmt.Errorf("projectID and key are required")
	}
	vars, err := e.store.ListProjectEnvVars(projectID)
	if err != nil {
		return "", err
	}
	isSecret := false
	found := false
	for i := range vars {
		if vars[i].Key == key {
			isSecret = vars[i].Secret
			found = true
			break
		}
	}
	if !found {
		return "", fmt.Errorf("project env var %s not found", key)
	}
	if !isSecret {
		return "", fmt.Errorf("only secret variables can be rotated")
	}

	newValue, err := generateSecretValue(32)
	if err != nil {
		return "", err
	}
	if err := e.store.SetProjectEnvVarValue(projectID, key, newValue); err != nil {
		return "", err
	}

	// Redeploy every running service in the project — project vars are injected
	// into all of them, so a rotated shared secret affects all of them.
	nodes, err := e.store.ListNodes(projectID)
	if err == nil {
		for _, n := range nodes {
			if dep, err := e.store.ActiveDeployment(n.ID); err == nil && dep != nil && dep.Status == "running" {
				_ = e.Deploy(ctx, n.ID)
			}
		}
	}
	return newValue, nil
}

// storeProjectEnvVarShim is a tiny local adapter to avoid pulling the store
// type name inline; it only carries the field RotateProjectEnvSecret needs.
type storeProjectEnvVarShim struct {
	secret bool
}
