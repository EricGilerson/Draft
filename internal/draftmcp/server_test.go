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
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"draft_help","arguments":{}}}`,
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
	if len(tools.Result.Tools) < 10 {
		t.Fatalf("unexpected tools: %+v", tools.Result.Tools)
	}
	names := map[string]bool{}
	for _, tool := range tools.Result.Tools {
		names[tool.Name] = true
	}
	for _, name := range []string{"draft_help", "draft_status", "draft_list_projects"} {
		if !names[name] {
			t.Fatalf("missing %s", name)
		}
	}

	var initialize struct {
		Result struct {
			Instructions string `json:"instructions"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &initialize); err != nil {
		t.Fatal(err)
	}
	if initialize.Result.Instructions == "" {
		t.Fatal("initialize did not include instructions")
	}

	var help struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
			IsError bool `json:"isError"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &help); err != nil {
		t.Fatal(err)
	}
	if help.Result.IsError || len(help.Result.Content) == 0 || !strings.Contains(help.Result.Content[0].Text, "draft_list_projects") {
		t.Fatalf("unexpected draft_help result: %+v", help.Result)
	}
	if !strings.Contains(help.Result.Content[0].Text, "recipes") && !strings.Contains(help.Result.Content[0].Text, "Recipes") {
		// tools/call returns JSON with "recipes" key
		if !strings.Contains(help.Result.Content[0].Text, `"recipes"`) {
			t.Fatalf("draft_help missing recipes: %s", help.Result.Content[0].Text[:min(200, len(help.Result.Content[0].Text))])
		}
	}
}
