package deploy

import (
	"context"
	"testing"
	"time"

	"Draft/internal/store"
)

func TestSandboxPreviewAndCreateUseIsolatedCopyDefaults(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProject("sandbox-project", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	source, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "api", Label: "API", ProjectID: p.ID, EnvironmentID: source.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSandboxProfile(store.SandboxProfile{ProjectID: p.ID, SourceEnvironmentID: source.ID, Name: "Full copy", IsDefault: true, PlanJSON: `{"ttlHours":48,"warningHours":6,"graceHours":12}`}); err != nil {
		t.Fatal(err)
	}

	e := New(s, nil, t.TempDir(), func(string, any) {})
	preview, err := e.PreviewSandbox(context.Background(), SandboxCreateRequest{Name: "feature-123", SourceEnvironmentID: source.ID, Links: []store.SandboxLink{{Kind: "pr", Value: "123"}, {Kind: "ticket", Value: "ENG-1"}}})
	if err != nil {
		t.Fatalf("PreviewSandbox: %v", err)
	}
	if preview.Plan.TTLHours != 48 || len(preview.Services) != 1 {
		t.Fatalf("unexpected preview: %+v", preview)
	}
	if preview.Services[0].Mode != SandboxServiceCopy || preview.Services[0].DataMode != ServiceDataFresh {
		t.Fatalf("copy default = %+v", preview.Services[0])
	}

	sandbox, err := e.CreateSandbox(context.Background(), SandboxCreateRequest{Name: "feature-123", SourceEnvironmentID: source.ID, Links: []store.SandboxLink{{Kind: "pr", Value: "123"}, {Kind: "ticket", Value: "ENG-1"}}})
	if err != nil {
		t.Fatalf("CreateSandbox: %v", err)
	}
	if sandbox.Status != "active" || sandbox.EnvironmentID == source.ID {
		t.Fatalf("unexpected sandbox: %+v", sandbox)
	}
	links, err := s.ListSandboxLinks(sandbox.ID)
	if err != nil || len(links) != 2 {
		t.Fatalf("links = %+v, %v", links, err)
	}
	targets, err := s.ListNodesByEnvironment(sandbox.EnvironmentID)
	if err != nil || len(targets) != 1 || targets[0].ID == "api" {
		t.Fatalf("sandbox nodes = %+v, %v", targets, err)
	}
}

func TestSandboxLifecycleTransitionsToWarningAndExpired(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProject("sandbox-lifecycle", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	source, _ := s.GetDefaultEnvironment(p.ID)
	env, err := s.CreateEnvironment(p.ID, "temporary")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sandbox, err := s.CreateSandbox(&store.Sandbox{ProjectID: p.ID, EnvironmentID: env.ID, SourceEnvironmentID: source.ID, Name: "temporary", PlanJSON: `{}`, ExpiresAt: now.Add(time.Hour), WarnAt: now.Add(-time.Minute), GraceEndsAt: now.Add(24 * time.Hour)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := New(s, nil, t.TempDir(), func(string, any) {})
	if err := e.ReconcileSandboxLifecycle(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSandbox(sandbox.ID)
	if got.Status != "warning" {
		t.Fatalf("status = %q, want warning", got.Status)
	}
	if err := e.ReconcileSandboxLifecycle(context.Background(), now.Add(2*time.Hour)); err != nil {
		t.Fatal(err)
	}
	got, _ = s.GetSandbox(sandbox.ID)
	if got.Status != "expired" {
		t.Fatalf("status = %q, want expired", got.Status)
	}
}

func TestExtendSandboxRestoresActiveLifecycle(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, _ := s.CreateProject("sandbox-extend", t.TempDir(), "")
	source, _ := s.GetDefaultEnvironment(p.ID)
	env, _ := s.CreateEnvironment(p.ID, "temporary")
	now := time.Now().UTC()
	sandbox, err := s.CreateSandbox(&store.Sandbox{ProjectID: p.ID, EnvironmentID: env.ID, SourceEnvironmentID: source.ID, Name: "temporary", Status: "expired", PlanJSON: `{"warningHours":2,"graceHours":4}`, ExpiresAt: now.Add(-time.Hour), WarnAt: now.Add(-2 * time.Hour), GraceEndsAt: now.Add(time.Hour)}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := New(s, nil, t.TempDir(), func(string, any) {})
	updated, err := e.ExtendSandbox(sandbox.ID, 12)
	if err != nil {
		t.Fatal(err)
	}
	if updated.Status != "active" || updated.ExpiresAt.Before(now.Add(11*time.Hour)) || updated.GraceEndsAt.Before(updated.ExpiresAt) {
		t.Fatalf("extended sandbox = %+v", updated)
	}
}

func TestGetSandboxDetailPreservesResolvedPlanAndManualLinks(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, _ := s.CreateProject("sandbox-detail", t.TempDir(), "")
	source, _ := s.GetDefaultEnvironment(p.ID)
	env, _ := s.CreateEnvironment(p.ID, "temporary")
	sandbox, err := s.CreateSandbox(&store.Sandbox{ProjectID: p.ID, EnvironmentID: env.ID, SourceEnvironmentID: source.ID, Name: "temporary", PlanJSON: `{"ttlHours":48,"services":[{"sourceNodeId":"api","mode":"share"}]}`, ExpiresAt: time.Now().Add(time.Hour), WarnAt: time.Now(), GraceEndsAt: time.Now().Add(2 * time.Hour)}, []store.SandboxLink{{Kind: "pr", Value: "412"}}, []store.SandboxRepositorySource{{RepoRoot: "/repo", Ref: "feature/x", CommitSHA: "0123456789abcdef"}})
	if err != nil {
		t.Fatal(err)
	}
	detail, err := New(s, nil, t.TempDir(), func(string, any) {}).GetSandboxDetail(sandbox.ID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.Source.ID != source.ID || len(detail.Links) != 1 || len(detail.Repositories) != 1 || len(detail.Plan.Services) != 1 {
		t.Fatalf("unexpected detail: %+v", detail)
	}
}
