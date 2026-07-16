package draftmcp

import (
	"encoding/json"
	"strings"
)

const secretMask = "***"

// redactSecrets returns a JSON-safe deep copy with secret-marked and
// conventionally sensitive fields masked unless includeSecrets is true.
func redactSecrets(v any, includeSecrets bool) any {
	if includeSecrets || v == nil {
		return v
	}
	// Round-trip through JSON so we only need to walk maps/slices/scalars.
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		return v
	}
	return redactNode(node, false)
}

func redactNode(v any, secret bool) any {
	switch n := v.(type) {
	case string:
		if secret {
			return secretMask
		}
		return n
	case []any:
		out := make([]any, len(n))
		for i, item := range n {
			out[i] = redactNode(item, secret)
		}
		return out
	case map[string]any:
		marked := false
		if b, ok := n["secret"].(bool); ok {
			marked = b
		} else if _, hasKey := n["key"]; hasKey {
			if _, hasVal := n["value"]; hasVal {
				// AppSecret JSON has key/value/description but no secret bool —
				// treat the value as sensitive by default.
				if _, hasDesc := n["description"]; hasDesc {
					marked = true
				}
			}
		}
		out := make(map[string]any, len(n))
		for k, val := range n {
			key := strings.ToLower(k)
			childSecret := secret || sensitiveKey(key) || (marked && key == "value")
			out[k] = redactNode(val, childSecret)
		}
		return out
	default:
		return v
	}
}

func sensitiveKey(key string) bool {
	return strings.Contains(key, "secret") || strings.Contains(key, "token") ||
		strings.Contains(key, "password") || strings.Contains(key, "credential")
}
