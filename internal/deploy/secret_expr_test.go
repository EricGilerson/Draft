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
	node, err := s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, EnvironmentID: defaultEnvID(t, s, project.ID), Label: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAppSecret("TOKEN", "secret-value", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{NodeID: node.ID, Key: "TOKEN", Value: "{{secret.TOKEN}}", Scope: store.EnvScopeRuntime}); err != nil {
		t.Fatal(err)
	}

	got, err := e.resolveValueForExport(node.ID, project.ID, node.EnvironmentID, "{{secret.TOKEN}}", map[string]bool{node.ID: true})
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
	node, err := s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, EnvironmentID: defaultEnvID(t, s, project.ID), Label: "api"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetAppSecret("TOKEN", "secret-value", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{NodeID: node.ID, Key: "TOKEN", Value: "{{secret.TOKEN}}", Scope: store.EnvScopeRuntime}); err != nil {
		t.Fatal(err)
	}

	got, err := e.resolveValue(node.ID, project.ID, node.EnvironmentID, "{{secret.TOKEN}}", map[string]bool{node.ID: true})
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
	node, err := s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, EnvironmentID: defaultEnvID(t, s, project.ID), Label: "api"})
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

func TestListAppSecretUsagesIncludesTransitiveServiceReferences(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	project, err := s.CreateProject("p", "/tmp/p", "")
	if err != nil { t.Fatal(err) }
	envID := defaultEnvID(t, s, project.ID)
	db, err := s.CreateNode(&store.CanvasNode{ID: "db", ProjectID: project.ID, EnvironmentID: envID, Label: "db"})
	if err != nil { t.Fatal(err) }
	api, err := s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, EnvironmentID: envID, Label: "api"})
	if err != nil { t.Fatal(err) }
	if err := s.SetAppSecret("DB_PASSWORD", "secret", ""); err != nil { t.Fatal(err) }
	if err := s.UpsertEnvVar(store.EnvVar{NodeID: db.ID, Key: "DATABASE_URL", Value: "postgres://{{secret.DB_PASSWORD}}", Scope: store.EnvScopeRuntime}); err != nil { t.Fatal(err) }
	if err := s.UpsertEnvVar(store.EnvVar{NodeID: api.ID, Key: "DATABASE_URL", Value: "@{db.DATABASE_URL}", Scope: store.EnvScopeRuntime}); err != nil { t.Fatal(err) }
	usages, err := e.ListAppSecretUsages("DB_PASSWORD")
	if err != nil { t.Fatal(err) }
	if len(usages) != 2 { t.Fatalf("got %+v, want db and api", usages) }
	if usages[0].NodeID != "db" || usages[1].NodeID != "api" { t.Fatalf("unexpected usages: %+v", usages) }
}

func TestDeleteAppSecretBlockedWhenReferenced(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	project, err := s.CreateProject("p", "/tmp/p", "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, EnvironmentID: defaultEnvID(t, s, project.ID), Label: "api"})
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
