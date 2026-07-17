package draftmcp

import (
	"path/filepath"
	"testing"

	"Draft/internal/store"
)

func TestMCPSessionContextDefaults(t *testing.T) {
	withTestDaemon(t)
	t.Cleanup(clearSessionContext)

	root := t.TempDir()
	var project store.Project
	mustCallToolJSON(t, "draft_create_project", map[string]any{
		"name": "ctx-demo",
		"path": filepath.Join(root, "ctx-demo"),
	}, &project)
	var envs []store.Environment
	mustCallToolJSON(t, "draft_list_environments", map[string]any{"projectId": float64(project.ID)}, &envs)

	mustCallToolJSON(t, "draft_set_context", map[string]any{
		"projectId":     float64(project.ID),
		"environmentId": float64(envs[0].ID),
	}, &sessionContext{})

	var listed []store.Environment
	mustCallToolJSON(t, "draft_list_environments", nil, &listed)
	if len(listed) == 0 {
		t.Fatal("list_environments with context failed")
	}

	var nodes []store.CanvasNode
	mustCallToolJSON(t, "draft_list_nodes", nil, &nodes)

	var blank store.CanvasNode
	mustCallToolJSON(t, "draft_create_blank_service", map[string]any{"label": "custom"}, &blank)
	if blank.ID == "" || blank.Label != "custom" {
		t.Fatalf("blank service: %+v", blank)
	}

	var templates []templateSummary
	mustCallToolJSON(t, "draft_list_templates", nil, &templates)
	if len(templates) == 0 {
		t.Fatal("compact templates empty")
	}
	if templates[0].Name == "" {
		t.Fatalf("compact row: %+v", templates[0])
	}

	var help map[string]any
	mustCallToolJSON(t, "draft_help", nil, &help)
	if help["recipes"] == nil || help["toolsByDomain"] == nil {
		t.Fatalf("help missing recipes/toolsByDomain: %#v", help)
	}

	_, text, isErr := callToolResult(t, "draft_clear_context", nil)
	if isErr {
		t.Fatalf("clear: %s", text)
	}
	_, text, isErr = callToolResult(t, "draft_list_environments", nil)
	if !isErr {
		t.Fatal("expected list_environments to require projectId after clear")
	}
	_ = text
}
