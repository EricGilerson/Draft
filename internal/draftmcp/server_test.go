package draftmcp_test

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"

	"Draft/internal/draftmcp"
)

func TestServeInitializeAndTools(t *testing.T) {
	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"draft_ping","arguments":{}}}`,
	}, "\n") + "\n")
	var out bytes.Buffer
	if err := draftmcp.Serve(in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("expected 3 responses, got %d: %q", len(lines), out.String())
	}
	var tools struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &tools); err != nil {
		t.Fatal(err)
	}
	if len(tools.Result.Tools) != 1 || tools.Result.Tools[0].Name != "draft_ping" {
		t.Fatalf("unexpected tools: %+v", tools.Result.Tools)
	}
}
