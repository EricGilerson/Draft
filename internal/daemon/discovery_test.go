package daemon

import (
	"context"
	"path/filepath"
	"testing"
)

func TestDiscoveryListCreateProjectAndNodes(t *testing.T) {
	srv, _, _ := newTestServer(t)
	ts := newIPv4Server(t, srv.routes())
	c := clientForHTTPServer(t, ts, srv.state.Token)
	ctx := context.Background()

	root := t.TempDir()
	proj, err := c.CreateProject(ctx, "demo", filepath.Join(root, "demo"), "test")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if proj.ID == 0 || proj.Name != "demo" {
		t.Fatalf("unexpected project: %+v", proj)
	}

	projects, err := c.ListProjects(ctx)
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(projects) != 1 {
		t.Fatalf("projects = %d, want 1", len(projects))
	}

	envs, err := c.ListEnvironments(ctx, proj.ID)
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 1 || !envs[0].IsDefault {
		t.Fatalf("expected default env, got %+v", envs)
	}

	env2, err := c.CreateEnvironment(ctx, proj.ID, "staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if err := c.RenameEnvironment(ctx, env2.ID, "Staging"); err != nil {
		t.Fatalf("RenameEnvironment: %v", err)
	}
	if err := c.SetDefaultEnvironment(ctx, env2.ID); err != nil {
		t.Fatalf("SetDefaultEnvironment: %v", err)
	}

	nodes, err := c.ListNodes(ctx, envs[0].ID)
	if err != nil {
		t.Fatalf("ListNodes: %v", err)
	}
	if nodes == nil {
		t.Fatal("nodes should be empty slice, not nil decode failure")
	}

	templates, err := c.ListTemplates(ctx)
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	_ = templates

	summary, err := c.ListProjectServicesSummary(ctx, proj.ID)
	if err != nil {
		t.Fatalf("ListProjectServicesSummary: %v", err)
	}
	if summary.ProjectID != proj.ID {
		t.Fatalf("summary projectId = %d", summary.ProjectID)
	}

	settings, err := c.GetAppSettings(ctx)
	if err != nil {
		t.Fatalf("GetAppSettings: %v", err)
	}
	settings.CompactSidebar = true
	saved, err := c.SetAppSettings(ctx, *settings)
	if err != nil {
		t.Fatalf("SetAppSettings: %v", err)
	}
	if !saved.CompactSidebar {
		t.Fatal("expected compactSidebar true")
	}

	sandboxes, err := c.ListSandboxes(ctx, proj.ID)
	if err != nil {
		t.Fatalf("ListSandboxes: %v", err)
	}
	_ = sandboxes

	profiles, err := c.ListSandboxProfiles(ctx, proj.ID)
	if err != nil {
		t.Fatalf("ListSandboxProfiles: %v", err)
	}
	_ = profiles

	sbSettings, err := c.GetSandboxProjectSettings(ctx, proj.ID)
	if err != nil {
		t.Fatalf("GetSandboxProjectSettings: %v", err)
	}
	if sbSettings.ProjectID != proj.ID {
		t.Fatalf("sandbox settings projectId = %d", sbSettings.ProjectID)
	}

	node, err := c.CreateNode(ctx, "", "blank-svc", proj.ID, envs[0].ID, 1, 2)
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if node.ID == "" || node.Label != "blank-svc" {
		t.Fatalf("CreateNode result: %+v", node)
	}
}
