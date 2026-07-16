// Package draftmcp implements a minimal Draft MCP server over stdio.
// Tools are intentionally bare for now; the Agents tab + wiring is the product.
package draftmcp

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
)

const protocolVersion = "2024-11-05"
const serverName = "draft"
const serverVersion = "0.1.0"

type rpcRequest struct {
	JSONRPC string          `json:"jsonrpc"`
	ID      json.RawMessage `json:"id,omitempty"`
	Method  string          `json:"method"`
	Params  json.RawMessage `json:"params,omitempty"`
}

type rpcResponse struct {
	JSONRPC string      `json:"jsonrpc"`
	ID      any         `json:"id,omitempty"`
	Result  any         `json:"result,omitempty"`
	Error   *rpcError   `json:"error,omitempty"`
}

type rpcError struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type toolDef struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
}

// RunStdio serves MCP on stdin/stdout until EOF.
func RunStdio() error {
	return Serve(os.Stdin, os.Stdout)
}

// Serve runs the MCP JSON-RPC loop on the given streams.
func Serve(in io.Reader, out io.Writer) error {
	enc := json.NewEncoder(out)
	sc := bufio.NewScanner(in)
	// Agent tool payloads / large initialize blobs can exceed the default token size.
	buf := make([]byte, 0, 64*1024)
	sc.Buffer(buf, 8*1024*1024)

	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req rpcRequest
		if err := json.Unmarshal(line, &req); err != nil {
			continue
		}
		if req.Method == "" {
			continue
		}
		// Notifications have no id — acknowledge by doing work, no response.
		if len(req.ID) == 0 || string(req.ID) == "null" {
			handleNotification(req.Method)
			continue
		}
		var id any
		_ = json.Unmarshal(req.ID, &id)
		resp := handleRequest(req.Method, req.Params, id)
		if err := enc.Encode(resp); err != nil {
			return err
		}
	}
	return sc.Err()
}

func handleNotification(method string) {
	switch method {
	case "notifications/initialized", "initialized":
		// no-op
	}
}

func handleRequest(method string, params json.RawMessage, id any) rpcResponse {
	switch method {
	case "initialize":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]any{
				"protocolVersion": protocolVersion,
				"capabilities": map[string]any{
					"tools": map[string]any{},
				},
				"serverInfo": map[string]any{
					"name":    serverName,
					"version": serverVersion,
				},
			},
		}
	case "ping":
		return rpcResponse{JSONRPC: "2.0", ID: id, Result: map[string]any{}}
	case "tools/list":
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]any{
				"tools": []toolDef{
					{
						Name:        "draft_ping",
						Description: "Confirm the Draft MCP server is reachable. Returns a short status string. Prefer this before other Draft tools once they exist.",
						InputSchema: map[string]any{
							"type":       "object",
							"properties": map[string]any{},
						},
					},
				},
			},
		}
	case "tools/call":
		return handleToolCall(params, id)
	default:
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &rpcError{Code: -32601, Message: fmt.Sprintf("method not found: %s", method)},
		}
	}
}

func handleToolCall(params json.RawMessage, id any) rpcResponse {
	var p struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := json.Unmarshal(params, &p); err != nil {
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Error:   &rpcError{Code: -32602, Message: "invalid tools/call params"},
		}
	}
	switch p.Name {
	case "draft_ping":
		text := "Draft MCP is running (stub). More Draft tools will land here; Agents tab wiring is active."
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": text},
				},
			},
		}
	default:
		return rpcResponse{
			JSONRPC: "2.0",
			ID:      id,
			Result: map[string]any{
				"content": []map[string]any{
					{"type": "text", "text": fmt.Sprintf("unknown tool: %s", p.Name)},
				},
				"isError": true,
			},
		}
	}
}
