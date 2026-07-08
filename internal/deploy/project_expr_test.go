package deploy

import (
	"testing"

	"Draft/internal/store"
)

func TestResolveProjectExprAtDeploy(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	project, err := s.CreateProject("p", "/tmp/p", "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, Label: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectEnvVar(project.ID, "LOG_LEVEL", "info", store.EnvScopeRuntime, false); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: node.ID, Key: "APP_LOG", Value: "{{project.LOG_LEVEL}}",
		Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
	}); err != nil {
		t.Fatal(err)
	}

	resolved, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:      node.ID,
		ProjectID:   project.ID,
		ServiceName: "api",
		ProjectName: "p",
		ServicePort: "3000",
	})
	if err != nil {
		t.Fatal(err)
	}

	runtime := map[string]string{}
	for _, item := range resolved.RuntimeEnv {
		key, value, ok := splitEnv(item)
		if !ok {
			t.Fatalf("bad env item %q", item)
		}
		runtime[key] = value
	}
	if runtime["APP_LOG"] != "info" {
		t.Fatalf("APP_LOG = %q", runtime["APP_LOG"])
	}
	if _, ok := runtime["LOG_LEVEL"]; ok {
		t.Fatal("project values must not auto-inject by key name")
	}
}

func TestResolveProjectExprPreservesOnExport(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	project, err := s.CreateProject("p", "/tmp/p", "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, Label: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectEnvVar(project.ID, "LOG_LEVEL", "info", store.EnvScopeRuntime, false); err != nil {
		t.Fatal(err)
	}

	raw := "{{project.LOG_LEVEL}}"
	got, err := e.resolveValueForExport(node.ID, project.ID, raw, map[string]bool{node.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != raw {
		t.Fatalf("export should preserve token, got %q", got)
	}
}

func TestListProjectEnvVarUsages(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	project, err := s.CreateProject("p", "/tmp/p", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, Label: "api"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "worker", ProjectID: project.ID, Label: "worker"}); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectEnvVar(project.ID, "JWT_SECRET", "abc", store.EnvScopeRuntime, false); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: "api", Key: "TOKEN", Value: "{{project.JWT_SECRET}}",
		Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: "worker", Key: "TOKEN", Value: "{{project.JWT_SECRET}}",
		Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
	}); err != nil {
		t.Fatal(err)
	}

	usages, err := e.ListProjectEnvVarUsages(project.ID, "JWT_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if len(usages) != 2 {
		t.Fatalf("expected 2 usages, got %d", len(usages))
	}
}
