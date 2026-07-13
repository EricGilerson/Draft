package deploy

import (
	"context"
	"strings"
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

	// Sandbox identity: .sand DNS segment + sand-{slug} Docker env segment.
	// Slugs stay project-unique (no kind-scoped uniqueness).
	addr, err := e.computeNodeAddress(&targets[0])
	if err != nil {
		t.Fatalf("computeNodeAddress: %v", err)
	}
	if !addr.Sandbox {
		t.Fatal("expected Sandbox=true on sandbox node address")
	}
	if !strings.Contains(addr.InternalHostname, ".sand.") {
		t.Fatalf("sandbox hostname missing .sand. segment: %q", addr.InternalHostname)
	}
	if addr.DockerEnvironment != "sand-feature-123" {
		t.Fatalf("DockerEnvironment = %q, want sand-feature-123", addr.DockerEnvironment)
	}
	if addr.Environment != "feature-123" {
		t.Fatalf("Environment slug = %q, want feature-123", addr.Environment)
	}
	// feature-123 already used by the sandbox env; a durable env with the same
	// display name must uniquify (shared slug space, not kind-scoped).
	dup, err := s.CreateEnvironment(p.ID, "feature-123")
	if err != nil {
		t.Fatal(err)
	}
	if dup.Slug != "feature-123-2" {
		t.Fatalf("expected slug uniquify against sandbox env, got %q", dup.Slug)
	}
}

func TestCreateSandboxOmitsServicesWithoutDuplicating(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProject("sandbox-omit", t.TempDir(), "")
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
	if _, err := s.CreateNode(&store.CanvasNode{ID: "worker", Label: "Worker", ProjectID: p.ID, EnvironmentID: source.ID}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.SaveSandboxProfile(store.SandboxProfile{
		ProjectID:           p.ID,
		SourceEnvironmentID: source.ID,
		Name:                "Omit worker",
		IsDefault:           true,
		PlanJSON:            `{"ttlHours":24,"services":[{"sourceNodeId":"worker","mode":"omit"}]}`,
	}); err != nil {
		t.Fatal(err)
	}

	e := New(s, nil, t.TempDir(), func(string, any) {})
	sandbox, err := e.CreateSandbox(context.Background(), SandboxCreateRequest{
		Name:                "omit-demo",
		SourceEnvironmentID: source.ID,
	})
	if err != nil {
		t.Fatalf("CreateSandbox: %v", err)
	}
	targets, err := s.ListNodesByEnvironment(sandbox.EnvironmentID)
	if err != nil {
		t.Fatal(err)
	}
	if len(targets) != 1 || targets[0].Label != "API" {
		t.Fatalf("expected only API in sandbox, got %+v", targets)
	}
	for _, n := range targets {
		if n.Label == "Worker" {
			t.Fatal("omitted Worker must not appear in sandbox environment")
		}
		settings, _ := s.GetNodeSettings(n.ID)
		if ParseServiceLink(settings[SettingServiceLink]) != nil {
			t.Fatalf("unexpected service_link on %q after omit-only create", n.Label)
		}
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

func TestExtendSandboxAddsToRemainingLifetime(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, _ := s.CreateProject("sandbox-extend-add", t.TempDir(), "")
	source, _ := s.GetDefaultEnvironment(p.ID)
	env, _ := s.CreateEnvironment(p.ID, "temporary")
	now := time.Now().UTC()
	remaining := now.Add(10 * time.Hour)
	sandbox, err := s.CreateSandbox(&store.Sandbox{
		ProjectID: p.ID, EnvironmentID: env.ID, SourceEnvironmentID: source.ID,
		Name: "temporary", Status: "active", PlanJSON: `{"warningHours":2,"graceHours":4}`,
		ExpiresAt: remaining, WarnAt: remaining.Add(-2 * time.Hour), GraceEndsAt: remaining.Add(4 * time.Hour),
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := New(s, nil, t.TempDir(), func(string, any) {})
	updated, err := e.ExtendSandbox(sandbox.ID, 12)
	if err != nil {
		t.Fatal(err)
	}
	// 10h remaining + 12h extension ≈ 22h from now, not 12h from now.
	if updated.ExpiresAt.Before(remaining.Add(11*time.Hour)) || updated.ExpiresAt.After(remaining.Add(13*time.Hour)) {
		t.Fatalf("expected additive expiry around %v, got %v", remaining.Add(12*time.Hour), updated.ExpiresAt)
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
