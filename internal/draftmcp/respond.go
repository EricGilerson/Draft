package draftmcp

import (
	"encoding/json"
	"fmt"
	"math"
)

const maxToolTextBytes = 256 * 1024
const maxEmbeddedStringBytes = 4 * 1024

func toolOK(id any, v any) rpcResponse {
	compact := compactForMCP(v)
	data, err := json.Marshal(compact)
	if err != nil {
		return toolErr(id, err)
	}
	if len(data) > maxToolTextBytes {
		data, err = json.Marshal(map[string]any{
			"truncated": true,
			"bytes":     len(data),
			"message":   "response exceeded Draft MCP size limit after compacting large strings; request a narrower tool or single-resource get",
		})
		if err != nil {
			return toolErr(id, err)
		}
	}
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: map[string]any{
		"content": []map[string]any{{"type": "text", "text": string(data)}},
	}}
}

func toolErr(id any, err error) rpcResponse { return toolErrMsg(id, err.Error()) }

func toolErrMsg(id any, msg string) rpcResponse {
	return rpcResponse{JSONRPC: "2.0", ID: id, Result: map[string]any{
		"content": []map[string]any{{"type": "text", "text": truncate(msg)}},
		"isError": true,
	}}
}

func truncate(s string) string {
	if len(s) <= maxToolTextBytes {
		return s
	}
	return s[:maxToolTextBytes] + "\n… truncated by Draft MCP"
}

// compactForMCP deep-copies via JSON then shortens oversized string fields so
// list/get payloads stay valid JSON under the MCP text budget.
func compactForMCP(v any) any {
	raw, err := json.Marshal(v)
	if err != nil {
		return v
	}
	var node any
	if err := json.Unmarshal(raw, &node); err != nil {
		return v
	}
	return truncateStrings(node)
}

func truncateStrings(v any) any {
	switch n := v.(type) {
	case string:
		if len(n) > maxEmbeddedStringBytes {
			return n[:maxEmbeddedStringBytes] + "…[truncated]"
		}
		return n
	case []any:
		out := make([]any, len(n))
		for i, item := range n {
			out[i] = truncateStrings(item)
		}
		return out
	case map[string]any:
		out := make(map[string]any, len(n))
		for k, val := range n {
			out[k] = truncateStrings(val)
		}
		return out
	default:
		return v
	}
}

func requireConfirm(args map[string]any) error {
	if ok, _ := args["confirm"].(bool); !ok {
		return fmt.Errorf("this operation is destructive; repeat with confirm: true")
	}
	return nil
}

func argString(args map[string]any, key string) (string, error) {
	value, ok := args[key].(string)
	if !ok || value == "" {
		return "", fmt.Errorf("%s is required", key)
	}
	return value, nil
}

func optionalString(args map[string]any, key string) string {
	value, _ := args[key].(string)
	return value
}

func argUint(args map[string]any, key string) (uint, error) {
	value, ok := args[key]
	if !ok {
		return 0, fmt.Errorf("%s is required", key)
	}
	number, ok := value.(float64)
	if !ok || number < 1 || math.Trunc(number) != number || number > math.MaxUint {
		return 0, fmt.Errorf("%s must be a positive integer", key)
	}
	return uint(number), nil
}

func optionalUint(args map[string]any, key string) uint {
	number, _ := args[key].(float64)
	if number < 1 || math.Trunc(number) != number || number > math.MaxUint {
		return 0
	}
	return uint(number)
}

func argBool(args map[string]any, key string) (bool, error) {
	value, ok := args[key].(bool)
	if !ok {
		return false, fmt.Errorf("%s must be a boolean", key)
	}
	return value, nil
}

func optionalBool(args map[string]any, key string) bool {
	value, _ := args[key].(bool)
	return value
}

func argStringMap(args map[string]any, key string) (map[string]string, error) {
	raw, ok := args[key].(map[string]any)
	if !ok {
		return nil, fmt.Errorf("%s must be an object", key)
	}
	result := make(map[string]string, len(raw))
	for k, v := range raw {
		text, ok := v.(string)
		if !ok {
			return nil, fmt.Errorf("%s.%s must be a string", key, k)
		}
		result[k] = text
	}
	return result, nil
}

func decodeArgs(args map[string]any, dst any) error {
	data, err := json.Marshal(args)
	if err != nil {
		return err
	}
	return json.Unmarshal(data, dst)
}
