package deploy

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"Draft/internal/store"
)

// recordDeploymentInputs stores a secret-safe manifest of exactly the env and
// build values handed to Docker. This is the baseline used to distinguish a
// merely pending edit from a running container whose configuration is stale.
func (e *Engine) recordDeploymentInputs(deploymentID uint, env deploymentEnv) error {
	inputs := make([]store.DeploymentInput, 0, len(env.RuntimeEnv)+len(env.BuildArgs))
	for _, item := range env.RuntimeEnv {
		key, value, ok := splitEnv(item)
		if !ok {
			continue
		}
		inputs = append(inputs, store.DeploymentInput{Key: key, Scope: "runtime", Digest: deploymentInputDigest(value)})
	}
	keys := make([]string, 0, len(env.BuildArgs))
	for key := range env.BuildArgs {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		value := ""
		if ptr := env.BuildArgs[key]; ptr != nil {
			value = *ptr
		}
		inputs = append(inputs, store.DeploymentInput{Key: key, Scope: "build", Digest: deploymentInputDigest(value)})
	}
	return e.store.ReplaceDeploymentInputs(deploymentID, inputs)
}

func deploymentInputDigest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:])
}
