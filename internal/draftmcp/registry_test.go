package draftmcp

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestToolRegistryInvariants(t *testing.T) {
	tools := allTools()
	if len(tools) < 80 {
		t.Fatalf("expected a deep tool catalog, got %d tools", len(tools))
	}

	seen := map[string]bool{}
	requiredConfirm := 0
	for _, tool := range tools {
		if tool.Name == "" {
			t.Fatal("tool with empty name")
		}
		if !strings.HasPrefix(tool.Name, "draft_") {
			t.Fatalf("tool %q missing draft_ prefix", tool.Name)
		}
		if tool.Description == "" {
			t.Fatalf("tool %q missing description", tool.Name)
		}
		if tool.Handler == nil {
			t.Fatalf("tool %q missing handler", tool.Name)
		}
		if tool.InputSchema == nil {
			t.Fatalf("tool %q missing inputSchema", tool.Name)
		}
		if typ, _ := tool.InputSchema["type"].(string); typ != "object" {
			t.Fatalf("tool %q schema type = %v, want object", tool.Name, tool.InputSchema["type"])
		}
		if seen[tool.Name] {
			t.Fatalf("duplicate tool name %q", tool.Name)
		}
		seen[tool.Name] = true

		props, _ := tool.InputSchema["properties"].(map[string]any)
		if props == nil {
			t.Fatalf("tool %q missing properties", tool.Name)
		}
		if _, ok := props["confirm"]; ok {
			hasConfirm := false
			for _, r := range asStringSlice(tool.InputSchema["required"]) {
				if r == "confirm" {
					hasConfirm = true
					break
				}
			}
			if !hasConfirm {
				t.Fatalf("tool %q exposes confirm but does not require it", tool.Name)
			}
			requiredConfirm++
		}
	}

	for _, name := range []string{
		"draft_help",
		"draft_status",
		"draft_set_context",
		"draft_list_projects",
		"draft_create_project",
		"draft_create_blank_service",
		"draft_list_environments",
		"draft_list_nodes",
		"draft_list_templates",
		"draft_create_service",
		"draft_deploy_service",
		"draft_stage_env",
		"draft_stage_service_settings",
		"draft_list_secrets",
		"draft_run_environment_stack",
		"draft_preview_sandbox",
		"draft_create_sandbox",
		"draft_sandbox_source_repos",
		"draft_run_command",
		"draft_docker_prune",
		"draft_import_draftpack",
		"draft_get_app_settings",
	} {
		if !seen[name] {
			t.Fatalf("missing expected tool %s", name)
		}
	}
	if requiredConfirm < 10 {
		t.Fatalf("expected many confirm-gated tools, got %d", requiredConfirm)
	}
}

func asStringSlice(v any) []string {
	switch x := v.(type) {
	case []string:
		return x
	case []any:
		out := make([]string, 0, len(x))
		for _, item := range x {
			if s, ok := item.(string); ok {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func TestAgentInstructionsContent(t *testing.T) {
	if agentInstructions == "" {
		t.Fatal("empty instructions")
	}
	for _, needle := range []string{
		"draft_list_projects",
		"draft_set_context",
		"draft_create_blank_service",
		"staged",
		"includeSecrets",
		"confirm",
		"sandbox",
		"draft_help",
		"Recipes",
	} {
		if !strings.Contains(agentInstructions, needle) {
			t.Fatalf("instructions missing %q", needle)
		}
	}
}

func TestCallUnknownTool(t *testing.T) {
	resp := callTool(1, "draft_does_not_exist", map[string]any{})
	raw, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(raw), "unknown tool") {
		t.Fatalf("expected unknown tool error, got %s", raw)
	}
	m, _ := resp.Result.(map[string]any)
	if m["isError"] != true {
		t.Fatalf("expected isError, got %#v", resp.Result)
	}
}

func TestTruncate(t *testing.T) {
	short := truncate("ok")
	if short != "ok" {
		t.Fatalf("short truncate = %q", short)
	}
	long := strings.Repeat("a", maxToolTextBytes+10)
	got := truncate(long)
	if len(got) <= maxToolTextBytes {
		t.Fatalf("truncated length %d, want > %d with suffix", len(got), maxToolTextBytes)
	}
	if !strings.Contains(got, "truncated") {
		t.Fatalf("missing truncation marker: %q", got[len(got)-40:])
	}
}

func TestCompactForMCPTruncatesLongStrings(t *testing.T) {
	payload := map[string]any{
		"name":       "Node.js",
		"dockerfile": strings.Repeat("x", maxEmbeddedStringBytes+50),
	}
	out := compactForMCP(payload).(map[string]any)
	df, _ := out["dockerfile"].(string)
	if len(df) <= maxEmbeddedStringBytes {
		t.Fatalf("expected truncated dockerfile length around %d, got %d", maxEmbeddedStringBytes, len(df))
	}
	if !strings.Contains(df, "[truncated]") {
		t.Fatalf("missing truncation marker: %q", df[len(df)-20:])
	}
	data, err := json.Marshal(out)
	if err != nil {
		t.Fatal(err)
	}
	var round map[string]any
	if err := json.Unmarshal(data, &round); err != nil {
		t.Fatal(err)
	}
}
