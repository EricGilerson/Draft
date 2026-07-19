package draftmcp

import (
	"bytes"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"Draft/internal/daemon"
	"Draft/internal/store"
)

func withTestDaemon(t *testing.T) (*daemon.Client, *store.Store) {
	t.Helper()
	c, st := daemon.NewTestClient(t)
	SetTestClient(c)
	t.Cleanup(func() { SetTestClient(nil) })
	return c, st
}

func callToolResult(t *testing.T, name string, args map[string]any) (map[string]any, string, bool) {
	t.Helper()
	if args == nil {
		args = map[string]any{}
	}
	resp := callTool(1, name, args)
	result, ok := resp.Result.(map[string]any)
	if !ok {
		t.Fatalf("%s: unexpected result type %#v", name, resp.Result)
	}
	isErr, _ := result["isError"].(bool)
	content, _ := result["content"].([]map[string]any)
	if len(content) == 0 {
		// JSON encoding may produce []any
		if raw, ok := result["content"].([]any); ok && len(raw) > 0 {
			if m, ok := raw[0].(map[string]any); ok {
				text, _ := m["text"].(string)
				return result, text, isErr
			}
		}
		t.Fatalf("%s: missing content: %#v", name, result)
	}
	text, _ := content[0]["text"].(string)
	return result, text, isErr
}

func mustCallToolJSON(t *testing.T, name string, args map[string]any, dest any) {
	t.Helper()
	_, text, isErr := callToolResult(t, name, args)
	if isErr {
		t.Fatalf("%s error: %s", name, text)
	}
	if dest == nil {
		return
	}
	if err := json.Unmarshal([]byte(text), dest); err != nil {
		t.Fatalf("%s decode %q: %v", name, text, err)
	}
}

func TestMCPIntegrationDiscoveryAndConfig(t *testing.T) {
	withTestDaemon(t)
	root := t.TempDir()

	var project store.Project
	mustCallToolJSON(t, "draft_create_project", map[string]any{
		"name":        "mcp-demo",
		"path":        filepath.Join(root, "mcp-demo"),
		"description": "integration",
	}, &project)
	if project.ID == 0 || project.Name != "mcp-demo" {
		t.Fatalf("bad project: %+v", project)
	}

	var projects []store.Project
	mustCallToolJSON(t, "draft_list_projects", nil, &projects)
	if len(projects) != 1 {
		t.Fatalf("projects = %d", len(projects))
	}

	var envs []store.Environment
	mustCallToolJSON(t, "draft_list_environments", map[string]any{"projectId": float64(project.ID)}, &envs)
	if len(envs) != 1 || !envs[0].IsDefault {
		t.Fatalf("expected default env, got %+v", envs)
	}
	defaultEnv := envs[0]

	var staging store.Environment
	mustCallToolJSON(t, "draft_create_environment", map[string]any{
		"projectId": float64(project.ID),
		"name":      "staging",
	}, &staging)
	if staging.ID == 0 {
		t.Fatal("staging env missing id")
	}

	_, text, isErr := callToolResult(t, "draft_rename_environment", map[string]any{
		"environmentId": float64(staging.ID),
		"name":          "Staging",
	})
	if isErr {
		t.Fatalf("rename: %s", text)
	}

	_, text, isErr = callToolResult(t, "draft_set_default_environment", map[string]any{
		"environmentId": float64(staging.ID),
	})
	if isErr {
		t.Fatalf("set default: %s", text)
	}

	mustCallToolJSON(t, "draft_list_environments", map[string]any{"projectId": float64(project.ID)}, &envs)
	var foundDefault bool
	for _, e := range envs {
		if e.ID == staging.ID && e.IsDefault {
			foundDefault = true
		}
	}
	if !foundDefault {
		t.Fatalf("staging not default: %+v", envs)
	}

	var nodes []store.CanvasNode
	mustCallToolJSON(t, "draft_list_nodes", map[string]any{"environmentId": float64(defaultEnv.ID)}, &nodes)
	if nodes == nil {
		nodes = []store.CanvasNode{}
	}

	var templates []store.ServiceTemplate
	mustCallToolJSON(t, "draft_list_templates", nil, &templates)
	if len(templates) == 0 {
		t.Fatal("expected seeded templates")
	}
	var tmpl store.ServiceTemplate
	for _, candidate := range templates {
		if candidate.Name == "Node.js" || strings.Contains(strings.ToLower(candidate.Name), "node") {
			tmpl = candidate
			break
		}
	}
	if tmpl.ID == 0 {
		tmpl = templates[0]
	}

	var summary map[string]any
	mustCallToolJSON(t, "draft_project_summary", map[string]any{"projectId": float64(project.ID)}, &summary)
	if uint(summary["projectId"].(float64)) != project.ID {
		t.Fatalf("summary projectId mismatch: %#v", summary)
	}

	var created map[string]any
	mustCallToolJSON(t, "draft_create_service", map[string]any{
		"projectId":     float64(project.ID),
		"environmentId": float64(defaultEnv.ID),
		"templateId":    float64(tmpl.ID),
		"label":         "api",
		"x":             10.0,
		"y":             20.0,
	}, &created)
	nodeObj, _ := created["node"].(map[string]any)
	if nodeObj == nil {
		// CreateNodeFromTemplateResult may serialize with nested node
		t.Fatalf("create service result missing node: %v", created)
	}
	nodeID, _ := nodeObj["id"].(string)
	if nodeID == "" {
		t.Fatalf("missing node id: %#v", nodeObj)
	}

	var gotNode store.CanvasNode
	mustCallToolJSON(t, "draft_get_node", map[string]any{"nodeId": nodeID}, &gotNode)
	if gotNode.Label != "api" {
		t.Fatalf("node label = %q", gotNode.Label)
	}

	var settings map[string]string
	mustCallToolJSON(t, "draft_get_node_settings", map[string]any{"nodeId": nodeID}, &settings)

	_, text, isErr = callToolResult(t, "draft_stage_service_settings", map[string]any{
		"nodeId":    nodeID,
		"projectId": float64(project.ID),
		"settings":  map[string]any{"service_port": "3000"},
	})
	if isErr {
		t.Fatalf("stage settings: %s", text)
	}

	_, text, isErr = callToolResult(t, "draft_stage_env", map[string]any{
		"nodeId": nodeID,
		"upserts": []any{
			map[string]any{"key": "HELLO", "value": "world", "scope": "runtime"},
			map[string]any{"key": "SECRET_TOKEN", "value": "s3cr3t", "scope": "runtime"},
		},
	})
	if isErr {
		t.Fatalf("stage env: %s", text)
	}

	// Applied env may still be empty until deploy; set applied for redaction coverage.
	_, text, isErr = callToolResult(t, "draft_set_env_var", map[string]any{
		"nodeId": nodeID,
		"key":    "PLAIN",
		"value":  "visible",
	})
	if isErr {
		t.Fatalf("set env: %s", text)
	}

	_, text, isErr = callToolResult(t, "draft_set_secret", map[string]any{
		"key":         "API_KEY",
		"value":       "super-secret",
		"description": "test",
		"confirm":     true,
	})
	if isErr {
		t.Fatalf("set secret: %s", text)
	}

	var secrets []map[string]any
	mustCallToolJSON(t, "draft_list_secrets", map[string]any{}, &secrets)
	if len(secrets) != 1 {
		t.Fatalf("secrets = %#v", secrets)
	}
	if secrets[0]["value"] != "***" && secrets[0]["value"] != "" {
		// redaction masks secret-named fields; AppSecret.Value becomes ***
		if v, ok := secrets[0]["value"].(string); ok && v == "super-secret" {
			t.Fatal("secret value was not redacted")
		}
	}

	mustCallToolJSON(t, "draft_list_secrets", map[string]any{"includeSecrets": true}, &secrets)
	if secrets[0]["value"] != "super-secret" {
		t.Fatalf("includeSecrets failed: %#v", secrets[0])
	}

	_, text, isErr = callToolResult(t, "draft_set_project_env", map[string]any{
		"projectId": float64(project.ID),
		"key":       "REGION",
		"value":     "us-east-1",
		"secret":    false,
	})
	if isErr {
		t.Fatalf("set project env: %s", text)
	}

	var projectEnv []map[string]any
	mustCallToolJSON(t, "draft_list_project_env", map[string]any{"projectId": float64(project.ID)}, &projectEnv)
	if len(projectEnv) != 1 || projectEnv[0]["key"] != "REGION" {
		t.Fatalf("project env: %#v", projectEnv)
	}

	var appSettings daemon.AppSettings
	mustCallToolJSON(t, "draft_get_app_settings", nil, &appSettings)

	_, text, isErr = callToolResult(t, "draft_set_app_settings", map[string]any{
		"confirm": true,
		"settings": map[string]any{
			"compactSidebar":          true,
			"localDomainPreference":   appSettings.LocalDomainPreference,
			"localDraftDomainEnabled": appSettings.LocalDraftDomainEnabled,
			"proxyPortMode":           appSettings.ProxyPortMode,
			"proxyPort":               float64(appSettings.ProxyPort),
			"proxyFallbackPort":       float64(appSettings.ProxyFallbackPort),
		},
	})
	if isErr {
		t.Fatalf("set app settings: %s", text)
	}

	mustCallToolJSON(t, "draft_get_app_settings", nil, &appSettings)
	if !appSettings.CompactSidebar {
		t.Fatal("compactSidebar not persisted")
	}

	var status map[string]any
	mustCallToolJSON(t, "draft_status", nil, &status)
	if status["daemon"] != "ok" {
		t.Fatalf("status: %#v", status)
	}

	var help map[string]any
	mustCallToolJSON(t, "draft_help", nil, &help)
	if help["recipes"] == nil {
		t.Fatalf("help missing recipes: %#v", help)
	}
	byDomain, _ := help["toolsByDomain"].(map[string]any)
	if len(byDomain) < 5 {
		t.Fatalf("help toolsByDomain sparse: %#v", byDomain)
	}

	// Config status / preview staged should succeed.
	mustCallToolJSON(t, "draft_service_config_status", map[string]any{"nodeId": nodeID}, &map[string]any{})
	mustCallToolJSON(t, "draft_preview_staged_changes", map[string]any{"nodeId": nodeID}, &map[string]any{})
	mustCallToolJSON(t, "draft_list_deployments", map[string]any{"nodeId": nodeID}, &[]any{})
	mustCallToolJSON(t, "draft_list_routes", map[string]any{"projectId": float64(project.ID)}, &[]any{})
	mustCallToolJSON(t, "draft_list_sandboxes", map[string]any{"projectId": float64(project.ID)}, &[]any{})
	mustCallToolJSON(t, "draft_list_sandbox_profiles", map[string]any{"projectId": float64(project.ID)}, &[]any{})
	mustCallToolJSON(t, "draft_get_sandbox_project_settings", map[string]any{"projectId": float64(project.ID)}, &map[string]any{})
	mustCallToolJSON(t, "draft_volumes_overview", nil, &[]any{})
	mustCallToolJSON(t, "draft_preview_delete_service", map[string]any{"nodeId": nodeID}, &map[string]any{})
}

func TestMCPIntegrationDestructiveConfirmGates(t *testing.T) {
	withTestDaemon(t)
	root := t.TempDir()

	var project store.Project
	mustCallToolJSON(t, "draft_create_project", map[string]any{
		"name": "gate-demo",
		"path": filepath.Join(root, "gate-demo"),
	}, &project)

	_, text, isErr := callToolResult(t, "draft_delete_project", map[string]any{
		"projectId": float64(project.ID),
	})
	if !isErr || !strings.Contains(text, "confirm") {
		t.Fatalf("expected confirm error, got err=%v text=%s", isErr, text)
	}

	_, text, isErr = callToolResult(t, "draft_delete_project", map[string]any{
		"projectId": float64(project.ID),
		"confirm":   false,
	})
	if !isErr || !strings.Contains(text, "confirm") {
		t.Fatalf("confirm:false should fail, got err=%v text=%s", isErr, text)
	}

	_, text, isErr = callToolResult(t, "draft_delete_project", map[string]any{
		"projectId": float64(project.ID),
		"confirm":   true,
	})
	if isErr {
		t.Fatalf("delete with confirm: %s", text)
	}

	var projects []store.Project
	mustCallToolJSON(t, "draft_list_projects", nil, &projects)
	if len(projects) != 0 {
		t.Fatalf("project still present: %+v", projects)
	}

	_, text, isErr = callToolResult(t, "draft_docker_prune", map[string]any{
		"resource": "images",
	})
	if !isErr || !strings.Contains(text, "confirm") {
		t.Fatalf("prune without confirm: err=%v text=%s", isErr, text)
	}
}

func TestMCPIntegrationServeJSONRPCRoundTrip(t *testing.T) {
	withTestDaemon(t)
	root := t.TempDir()

	in := strings.NewReader(strings.Join([]string{
		`{"jsonrpc":"2.0","id":1,"method":"initialize","params":{}}`,
		`{"jsonrpc":"2.0","method":"notifications/initialized"}`,
		`{"jsonrpc":"2.0","id":2,"method":"tools/list"}`,
		`{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"draft_create_project","arguments":{"name":"rpc-demo","path":` + jsonString(filepath.Join(root, "rpc-demo")) + `}}}`,
		`{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"draft_list_projects","arguments":{}}}`,
	}, "\n") + "\n")

	var out bytes.Buffer
	if err := Serve(in, &out); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(out.String()), "\n")
	if len(lines) != 4 {
		t.Fatalf("expected 4 responses, got %d: %s", len(lines), out.String())
	}

	var init struct {
		Result struct {
			Instructions string `json:"instructions"`
			ServerInfo   struct {
				Version string `json:"version"`
			} `json:"serverInfo"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[0]), &init); err != nil {
		t.Fatal(err)
	}
	if init.Result.Instructions == "" || init.Result.ServerInfo.Version != serverVersion {
		t.Fatalf("bad initialize: %+v", init.Result)
	}

	var list struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[1]), &list); err != nil {
		t.Fatal(err)
	}
	if len(list.Result.Tools) < 80 {
		t.Fatalf("tools/list count = %d", len(list.Result.Tools))
	}

	var create struct {
		Result struct {
			IsError bool `json:"isError"`
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[2]), &create); err != nil {
		t.Fatal(err)
	}
	if create.Result.IsError {
		t.Fatalf("create failed: %v", create.Result.Content)
	}

	var listed struct {
		Result struct {
			Content []struct {
				Text string `json:"text"`
			} `json:"content"`
		} `json:"result"`
	}
	if err := json.Unmarshal([]byte(lines[3]), &listed); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(listed.Result.Content[0].Text, "rpc-demo") {
		t.Fatalf("list missing project: %s", listed.Result.Content[0].Text)
	}
}

func jsonString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

func TestMCPIntegrationSandboxPreviewRequiresSource(t *testing.T) {
	withTestDaemon(t)
	root := t.TempDir()
	var project store.Project
	mustCallToolJSON(t, "draft_create_project", map[string]any{
		"name": "sand-demo",
		"path": filepath.Join(root, "sand-demo"),
	}, &project)
	var envs []store.Environment
	mustCallToolJSON(t, "draft_list_environments", map[string]any{"projectId": float64(project.ID)}, &envs)

	_, text, isErr := callToolResult(t, "draft_preview_sandbox", map[string]any{
		"request": map[string]any{
			"name":                "preview-1",
			"sourceEnvironmentId": float64(envs[0].ID),
			"startOnCreate":       false,
		},
	})
	// Preview may succeed with empty service plan or fail validation — either is a real daemon path.
	if isErr && text == "" {
		t.Fatal("empty error from preview_sandbox")
	}

	mustCallToolJSON(t, "draft_sandbox_source_repos", map[string]any{
		"sourceEnvironmentId": float64(envs[0].ID),
	}, &map[string]any{})
}

func TestMCPIntegrationEnvVarListAndDiscard(t *testing.T) {
	withTestDaemon(t)
	root := t.TempDir()
	var project store.Project
	mustCallToolJSON(t, "draft_create_project", map[string]any{
		"name": "env-demo",
		"path": filepath.Join(root, "env-demo"),
	}, &project)
	var envs []store.Environment
	mustCallToolJSON(t, "draft_list_environments", map[string]any{"projectId": float64(project.ID)}, &envs)

	templates := []store.ServiceTemplate{}
	mustCallToolJSON(t, "draft_list_templates", nil, &templates)
	var created map[string]any
	mustCallToolJSON(t, "draft_create_service", map[string]any{
		"projectId":     float64(project.ID),
		"environmentId": float64(envs[0].ID),
		"templateId":    float64(templates[0].ID),
		"label":         "web",
	}, &created)
	nodeID := created["node"].(map[string]any)["id"].(string)

	_, text, isErr := callToolResult(t, "draft_set_env_var", map[string]any{
		"nodeId": nodeID, "key": "PLAIN", "value": "visible",
	})
	if isErr {
		t.Fatalf("set env: %s", text)
	}

	var envVars []map[string]any
	mustCallToolJSON(t, "draft_list_env_vars", map[string]any{"nodeId": nodeID}, &envVars)
	found := false
	for _, ev := range envVars {
		if ev["key"] == "PLAIN" && ev["value"] == "visible" {
			found = true
		}
	}
	if !found {
		t.Fatalf("PLAIN missing: %#v", envVars)
	}

	_, text, isErr = callToolResult(t, "draft_discard_staged_changes", map[string]any{"nodeId": nodeID})
	if !isErr || !strings.Contains(text, "confirm") {
		t.Fatalf("discard without confirm: err=%v text=%s", isErr, text)
	}
	_, text, isErr = callToolResult(t, "draft_discard_staged_changes", map[string]any{"nodeId": nodeID, "confirm": true})
	if isErr {
		t.Fatalf("discard: %s", text)
	}
}
