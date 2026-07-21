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

	created, err := e.CreateSandbox(context.Background(), SandboxCreateRequest{Name: "feature-123", SourceEnvironmentID: source.ID, Links: []store.SandboxLink{{Kind: "pr", Value: "123"}, {Kind: "ticket", Value: "ENG-1"}}})
	if err != nil {
		t.Fatalf("CreateSandbox: %v", err)
	}
	sandbox := created.Sandbox
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
	created, err := e.CreateSandbox(context.Background(), SandboxCreateRequest{
		Name:                "omit-demo",
		SourceEnvironmentID: source.ID,
	})
	if err != nil {
		t.Fatalf("CreateSandbox: %v", err)
	}
	sandbox := created.Sandbox
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

func TestSandboxLifecycleIdleSuspend(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProject("sandbox-idle", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	source, _ := s.GetDefaultEnvironment(p.ID)
	env, err := s.CreateEnvironment(p.ID, "temporary")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	last := now.Add(-3 * time.Hour)
	sandbox, err := s.CreateSandbox(&store.Sandbox{
		ProjectID: p.ID, EnvironmentID: env.ID, SourceEnvironmentID: source.ID,
		Name: "temporary", Status: "active", PlanJSON: `{"suspendIdleHours":2,"ttlHours":48,"warningHours":24,"graceHours":72}`,
		ExpiresAt: now.Add(24 * time.Hour), WarnAt: now.Add(12 * time.Hour), GraceEndsAt: now.Add(48 * time.Hour),
		LastActivityAt: &last,
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := New(s, nil, t.TempDir(), func(string, any) {})
	if err := e.ReconcileSandboxLifecycle(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	got, _ := s.GetSandbox(sandbox.ID)
	if got.Status != "suspended" {
		t.Fatalf("status = %q, want suspended", got.Status)
	}
}

// When grace ends at the same time as expiry (graceHours=0), a single reconcile
// pass must expire and purge — not leave the row stuck as "expired" until the
// next minute tick.
func TestSandboxLifecyclePurgesAfterGraceInSamePass(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProject("sandbox-purge", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	source, _ := s.GetDefaultEnvironment(p.ID)
	env, err := s.CreateEnvironment(p.ID, "temporary")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	sandbox, err := s.CreateSandbox(&store.Sandbox{
		ProjectID: p.ID, EnvironmentID: env.ID, SourceEnvironmentID: source.ID,
		Name: "temporary", Status: "active", PlanJSON: `{"graceHours":0}`,
		ExpiresAt: now.Add(-time.Minute), WarnAt: now.Add(-time.Hour), GraceEndsAt: now.Add(-time.Minute),
	}, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	e := New(s, nil, t.TempDir(), func(string, any) {})
	if err := e.ReconcileSandboxLifecycle(context.Background(), now); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetSandbox(sandbox.ID); err == nil {
		t.Fatal("expected sandbox to be purged after grace ended")
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

func TestTestingSandboxPlanDefaultsAndStepValidation(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProject("sandbox-test-plan", t.TempDir(), "")
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
	// Managed volume would clone for preview; testing purpose should default fresh.
	if err := s.SetNodeSetting("api", "volume_mounts", `[{"type":"volume","containerPath":"/data"}]`); err != nil {
		t.Fatal(err)
	}

	e := New(s, nil, t.TempDir(), func(string, any) {})
	preview, err := e.PreviewSandbox(context.Background(), SandboxCreateRequest{
		Name:                "api-integration",
		SourceEnvironmentID: source.ID,
		Plan: SandboxPlan{
			Purpose: SandboxPurposeTest,
			Steps: []SandboxStep{
				{ServiceLabel: "API", Cmd: []string{"pytest", "-q"}},
			},
		},
	})
	if err != nil {
		t.Fatalf("PreviewSandbox: %v", err)
	}
	if preview.Plan.Purpose != SandboxPurposeTest {
		t.Fatalf("purpose = %q", preview.Plan.Purpose)
	}
	if preview.Plan.TTLHours != 4 || preview.Plan.WarningHours != 1 || preview.Plan.GraceHours != 2 {
		t.Fatalf("testing lifecycle defaults = %+v", preview.Plan)
	}
	if preview.Plan.OnComplete != SandboxOnCompleteLeave {
		t.Fatalf("onComplete = %q", preview.Plan.OnComplete)
	}
	if len(preview.Services) != 1 || preview.Services[0].DataMode != ServiceDataFresh {
		t.Fatalf("testing data default = %+v", preview.Services)
	}
	if len(preview.Plan.Steps) != 1 || preview.Plan.Steps[0].Name != "pytest -q" {
		t.Fatalf("steps = %+v", preview.Plan.Steps)
	}

	created, err := e.CreateSandbox(context.Background(), SandboxCreateRequest{
		Name:                "api-integration",
		SourceEnvironmentID: source.ID,
		Plan: SandboxPlan{
			Purpose: SandboxPurposeTest,
			Steps:   []SandboxStep{{ServiceLabel: "API", Cmd: []string{"pytest", "-q"}}},
		},
	})
	if err != nil {
		t.Fatalf("CreateSandbox: %v", err)
	}
	if created.Sandbox.Purpose != string(SandboxPurposeTest) {
		t.Fatalf("sandbox.Purpose = %q", created.Sandbox.Purpose)
	}
}

func TestTestingSandboxRejectsEmptyStepsCmd(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, _ := s.CreateProject("sandbox-test-bad", t.TempDir(), "")
	source, _ := s.GetDefaultEnvironment(p.ID)
	e := New(s, nil, t.TempDir(), func(string, any) {})
	_, err = e.PreviewSandbox(context.Background(), SandboxCreateRequest{
		Name:                "bad",
		SourceEnvironmentID: source.ID,
		Plan: SandboxPlan{
			Purpose: SandboxPurposeTest,
			Steps:   []SandboxStep{{ServiceLabel: "API", Cmd: nil}},
		},
	})
	if err == nil {
		t.Fatal("expected validation error for empty cmd")
	}
}

func TestSandboxStepAssertionsAcceptExitCodesOrLooseOutput(t *testing.T) {
	result := SandboxTestStepResult{ExitCode: 4, Output: "ready\n  with spaces"}
	if !stepPassed(SandboxStep{ExpectedExitCodes: []int{4}}, result) {
		t.Fatal("expected configured exit code to pass")
	}
	if !stepPassed(SandboxStep{OutputContains: "ready with spaces"}, result) {
		t.Fatal("expected whitespace-tolerant output match to pass")
	}
	if stepPassed(SandboxStep{ExpectedExitCodes: []int{0}, OutputContains: "missing"}, result) {
		t.Fatal("unexpected assertion pass")
	}
}

func TestRunTestingSandboxStepsRecordsHistory(t *testing.T) {
	// File-backed store: stack start fans out goroutines and the pure-Go
	// sqlite pool can open multiple connections; shared file DSN keeps one schema.
	s, err := store.Open(store.FileDSN(t.TempDir() + "/draft.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, _ := s.CreateProject("sandbox-test-run", t.TempDir(), "")
	source, _ := s.GetDefaultEnvironment(p.ID)
	if _, err := s.CreateNode(&store.CanvasNode{ID: "api", Label: "API", ProjectID: p.ID, EnvironmentID: source.ID}); err != nil {
		t.Fatal(err)
	}
	e := New(s, nil, t.TempDir(), func(string, any) {})
	created, err := e.CreateSandbox(context.Background(), SandboxCreateRequest{
		Name:                "suite",
		SourceEnvironmentID: source.ID,
		Plan: SandboxPlan{
			Purpose: SandboxPurposeTest,
			// No deployable settings — waitSandboxServicesReady skips non-deployable nodes.
			Steps: []SandboxStep{{ServiceLabel: "API", Cmd: []string{"true"}}},
		},
	})
	if err != nil {
		t.Fatalf("CreateSandbox: %v", err)
	}
	// Steps mode against a non-running service should fail the step and record a run.
	result, err := e.RunTestingSandbox(context.Background(), SandboxTestRunRequest{
		SandboxID: created.Sandbox.ID,
		Mode:      SandboxTestRunSteps,
	})
	if err != nil {
		t.Fatalf("RunTestingSandbox infrastructure error: %v", err)
	}
	if result.Run.Status != "failed" {
		t.Fatalf("status = %q, want failed (service not running)", result.Run.Status)
	}
	if len(result.Steps) == 0 {
		t.Fatal("expected at least one step result")
	}
	rows, err := e.ListSandboxTestRuns(p.ID, 10)
	if err != nil || len(rows) != 1 {
		t.Fatalf("history = %+v, %v", rows, err)
	}
}
