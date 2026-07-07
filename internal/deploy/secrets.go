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
