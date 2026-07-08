package deploy

import (
	"strings"
	"testing"

	"Draft/internal/store"
)

func TestResolveSecretExprs(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	if err := s.SetAppSecret("OPENAI_API_KEY", "sk-test", ""); err != nil {
		t.Fatal(err)
	}

	got, err := e.resolveSecretExprs("prefix-{{secret.OPENAI_API_KEY}}-suffix", false)
	if err != nil {
		t.Fatal(err)
	}
	if got != "prefix-sk-test-suffix" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveSecretExprsPreservesOnExport(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	if err := s.SetAppSecret("OPENAI_API_KEY", "sk-test", ""); err != nil {
		t.Fatal(err)
	}

	raw := "{{secret.OPENAI_API_KEY}}"
	got, err := e.resolveSecretExprs(raw, true)
	if err != nil {
		t.Fatal(err)
	}
	if got != raw {
		t.Fatalf("export should preserve token, got %q", got)
	}
}

func TestResolveValueForExportPreservesSecretToken(t *testing.T) {
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
	if err := s.SetAppSecret("TOKEN", "secret-value", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{NodeID: node.ID, Key: "TOKEN", Value: "{{secret.TOKEN}}", Scope: store.EnvScopeRuntime}); err != nil {
		t.Fatal(err)
	}

	got, err := e.resolveValueForExport(node.ID, project.ID, "{{secret.TOKEN}}", map[string]bool{node.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "{{secret.TOKEN}}" {
		t.Fatalf("got %q", got)
	}
}

func TestResolveValueExpandsSecretAtDeploy(t *testing.T) {
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
	if err := s.SetAppSecret("TOKEN", "secret-value", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{NodeID: node.ID, Key: "TOKEN", Value: "{{secret.TOKEN}}", Scope: store.EnvScopeRuntime}); err != nil {
		t.Fatal(err)
	}

	got, err := e.resolveValue(node.ID, project.ID, "{{secret.TOKEN}}", map[string]bool{node.ID: true})
	if err != nil {
		t.Fatal(err)
	}
	if got != "secret-value" {
		t.Fatalf("got %q", got)
	}
}

func TestListMissingSecretExprs(t *testing.T) {
	s := openTestStore(t)
	issues := listMissingSecretExprs(s, "KEY", "use {{secret.MISSING}} here")
	if len(issues) != 1 {
		t.Fatalf("expected 1 issue, got %d", len(issues))
	}
	if !strings.Contains(issues[0].Reason, "not found") {
		t.Fatalf("unexpected reason: %q", issues[0].Reason)
	}
}

func TestListAppSecretUsages(t *testing.T) {
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
	if err := s.SetAppSecret("OPENAI_API_KEY", "sk-test", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{NodeID: node.ID, Key: "OPENAI_API_KEY", Value: "{{secret.OPENAI_API_KEY}}", Scope: store.EnvScopeRuntime}); err != nil {
		t.Fatal(err)
	}

	usages, err := e.ListAppSecretUsages("OPENAI_API_KEY")
	if err != nil {
		t.Fatal(err)
	}
	if len(usages) != 1 {
		t.Fatalf("expected 1 usage, got %d", len(usages))
	}
	if usages[0].NodeID != node.ID || usages[0].VarKey != "OPENAI_API_KEY" {
		t.Fatalf("unexpected usage: %+v", usages[0])
	}
}

func TestListProjectSecretUsages(t *testing.T) {
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
	if err := s.SetProjectEnvVar(project.ID, "JWT_SECRET", "abc", store.EnvScopeRuntime, true); err != nil {
		t.Fatal(err)
	}

	usages, err := e.ListProjectSecretUsages(project.ID, "JWT_SECRET")
	if err != nil {
		t.Fatal(err)
	}
	if len(usages) != 2 {
		t.Fatalf("expected 2 usages, got %d", len(usages))
	}
}

func TestDeleteAppSecretBlockedWhenReferenced(t *testing.T) {
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
	if err := s.SetAppSecret("KEY", "val", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{NodeID: node.ID, Key: "K", Value: "{{secret.KEY}}", Scope: store.EnvScopeRuntime}); err != nil {
		t.Fatal(err)
	}

	count, err := e.CountAppSecretReferences("KEY")
	if err != nil || count != 1 {
		t.Fatalf("count=%d err=%v", count, err)
	}
}
