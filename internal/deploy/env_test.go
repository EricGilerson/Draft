package deploy

import (
	"strings"
	"testing"

	"Draft/internal/store"
)

func TestResolveDeploymentEnvRuntimeBuildArgsAndGenerated(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	if err := s.UpsertEnvVar(store.EnvVar{NodeID: "n1", Key: "RUNTIME_ONLY", Value: "one", Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{NodeID: "n1", Key: "BUILD_ME", Value: "two", Scope: store.EnvScopeBoth, Source: store.EnvSourceManual}); err != nil {
		t.Fatal(err)
	}

	resolved, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:           "n1",
		ServiceName:      "web",
		ProjectName:      "draft",
		Environment:      "default",
		ServicePort:      "3000",
		InternalHostname: "web.draft.default.abcd.draft.local",
		InternalURL:      "http://web.draft.default.abcd.draft.local:3000",
		PublicHostname:   "web.draft.default.abcd.draft.resolv.sh",
		PublicURL:        "http://web.draft.default.abcd.draft.resolv.sh:53888",
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

	if runtime["RUNTIME_ONLY"] != "one" || runtime["BUILD_ME"] != "two" {
		t.Fatalf("runtime env = %+v", runtime)
	}
	if runtime["DRAFT_SERVICE_PORT"] != "3000" ||
		runtime["DRAFT_INTERNAL_HOSTNAME"] != "web.draft.default.abcd.draft.local" ||
		runtime["DRAFT_INTERNAL_URL"] != "http://web.draft.default.abcd.draft.local:3000" ||
		runtime["DRAFT_PUBLIC_HOSTNAME"] != "web.draft.default.abcd.draft.resolv.sh" ||
		runtime["DRAFT_PUBLIC_URL"] != "http://web.draft.default.abcd.draft.resolv.sh:53888" {
		t.Fatalf("generated env = %+v", runtime)
	}
	if _, ok := resolved.BuildArgs["RUNTIME_ONLY"]; ok {
		t.Fatal("runtime-only var should not be a build arg")
	}
	if arg := resolved.BuildArgs["BUILD_ME"]; arg == nil || *arg != "two" {
		t.Fatalf("BUILD_ME build arg = %v", arg)
	}
}

func TestResolveDeploymentEnvProjectVarsNotAutoInjected(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	project, err := s.CreateProject("shared", "/tmp/shared", "")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, EnvironmentID: defaultEnvID(t, s, project.ID), Label: "api"}); err != nil {
		t.Fatal(err)
	}

	if err := s.SetProjectEnvVar(project.ID, "SHARED_RUNTIME", "rt", store.EnvScopeRuntime, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectEnvVar(project.ID, "SHARED_BUILD", "bd", store.EnvScopeBuild, false); err != nil {
		t.Fatal(err)
	}
	if err := s.SetProjectEnvVar(project.ID, "SHARED_BOTH", "bo", store.EnvScopeBoth, false); err != nil {
		t.Fatal(err)
	}

	resolved, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:      "api",
		ProjectID:   project.ID,
		ServiceName: "api",
		ProjectName: "shared",
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

	for _, key := range []string{"SHARED_RUNTIME", "SHARED_BOTH", "SHARED_BUILD"} {
		if _, ok := runtime[key]; ok {
			t.Fatalf("%s should not be auto-injected, got %q", key, runtime[key])
		}
	}
	if len(resolved.BuildArgs) != 0 {
		t.Fatalf("expected no build args without explicit references, got %+v", resolved.BuildArgs)
	}
}

func TestResolveDeploymentEnvRejectsReservedDraftKeys(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	if err := s.SetEnvVar("n1", "DRAFT_PORT", "1234"); err != nil {
		t.Fatal(err)
	}

	_, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:           "n1",
		ServiceName:      "web",
		ProjectName:      "draft",
		ServicePort:      "3000",
		InternalHostname: "web.draft.default.abcd.draft.local",
	})
	if err == nil {
		t.Fatal("expected reserved key error")
	}
}

func TestResolveDeploymentEnvTCPUsesSchemelessEndpoints(t *testing.T) {
	// TCP nodes inject scheme-less host:port for DRAFT_*_URL (never http://).
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	resolved, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:           "db1",
		ServiceName:      "db",
		ProjectName:      "app",
		Environment:      "default",
		ServicePort:      "5432",
		InternalHostname: "db.app.default.abcd.draft.local",
		InternalURL:      "db.app.default.abcd.draft.local:5432",
		PublicHostname:   "db.app.default.abcd.draft.resolv.sh",
		PublicURL:        "db.app.default.abcd.draft.resolv.sh:5432",
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
	if runtime["DRAFT_INTERNAL_URL"] != "db.app.default.abcd.draft.local:5432" {
		t.Fatalf("DRAFT_INTERNAL_URL = %q", runtime["DRAFT_INTERNAL_URL"])
	}
	if runtime["DRAFT_PUBLIC_URL"] != "db.app.default.abcd.draft.resolv.sh:5432" {
		t.Fatalf("DRAFT_PUBLIC_URL = %q", runtime["DRAFT_PUBLIC_URL"])
	}
	if strings.HasPrefix(runtime["DRAFT_PUBLIC_URL"], "http") {
		t.Fatal("TCP DRAFT_PUBLIC_URL must not use http scheme")
	}
}

func TestComputeNodeAddressTCPUsesSchemelessEndpoints(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	p, err := s.CreateProject("addr-proj", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateNode(&store.CanvasNode{
		ID:            "tcp-node",
		ProjectID:     p.ID,
		EnvironmentID: defaultEnvID(t, s, p.ID),
		Label:         "db",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting(node.ID, "service_port", "5432")
	_ = s.SetNodeSetting(node.ID, "route_protocol", "tcp")
	_ = s.SetNodeSetting(node.ID, "host_port", "5432")

	addr, err := e.computeNodeAddress(node)
	if err != nil {
		t.Fatal(err)
	}
	if strings.HasPrefix(addr.InternalURL, "http") {
		t.Errorf("InternalURL must not be http for TCP, got %q", addr.InternalURL)
	}
	if !strings.HasSuffix(addr.InternalURL, ":5432") {
		t.Errorf("InternalURL = %q, want …:5432", addr.InternalURL)
	}
	if !strings.HasSuffix(addr.PublicURL, ".draft.resolv.sh:5432") {
		t.Errorf("PublicURL = %q, want …resolv.sh:5432", addr.PublicURL)
	}
	if strings.HasPrefix(addr.PublicURL, "http") {
		t.Errorf("PublicURL must not be http for TCP, got %q", addr.PublicURL)
	}
}

func TestComputeNodeAddressTCPPrefersLeasedHostPort(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	p, err := s.CreateProject("lease-proj", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateNode(&store.CanvasNode{
		ID:            "tcp-leased",
		ProjectID:     p.ID,
		EnvironmentID: defaultEnvID(t, s, p.ID),
		Label:         "db",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting(node.ID, "service_port", "5432")
	_ = s.SetNodeSetting(node.ID, "route_protocol", "tcp")
	_ = s.SetNodeSetting(node.ID, "host_port", "5432")

	addr, err := e.computeNodeAddress(node)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertRoute(&store.Route{
		Hostname:    addr.InternalHostname,
		ProjectID:   p.ID,
		NodeID:      node.ID,
		Environment: addr.Environment,
		Protocol:    "tcp",
		TargetHost:  "127.0.0.1",
		TargetPort:  5432,
		HostPort:    32831,
	}); err != nil {
		t.Fatal(err)
	}

	got, err := e.computeNodeAddress(node)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasSuffix(got.PublicURL, ":32831") {
		t.Fatalf("PublicURL = %q, want leased …:32831", got.PublicURL)
	}
	if strings.HasSuffix(got.InternalURL, ":32831") {
		t.Fatalf("InternalURL must keep service port, got %q", got.InternalURL)
	}
}

func TestWithTCPPublicURLRewritesConnectionStrings(t *testing.T) {
	oldURL := "db.app.main.abcd.draft.resolv.sh:5432"
	newURL := "db.app.main.abcd.draft.resolv.sh:32831"
	env := deploymentEnv{
		RuntimeEnv: []string{
			"DATABASE_URL=postgres://postgres:secret@" + strings.Replace(oldURL, ".resolv.sh:5432", ".local:5432", 1) + "/postgres",
			"PUBLIC_DATABASE_URL=postgres://postgres:secret@" + oldURL + "/postgres",
			"DRAFT_PUBLIC_URL=" + oldURL,
			"OTHER=keep-me",
		},
		BuildArgs: map[string]*string{},
	}
	got := withTCPPublicURL(env, oldURL, newURL)
	byKey := map[string]string{}
	for _, item := range got.RuntimeEnv {
		k, v, ok := splitEnv(item)
		if !ok {
			t.Fatalf("bad env item %q", item)
		}
		byKey[k] = v
	}
	if byKey["DRAFT_PUBLIC_URL"] != newURL {
		t.Fatalf("DRAFT_PUBLIC_URL = %q", byKey["DRAFT_PUBLIC_URL"])
	}
	if !strings.Contains(byKey["PUBLIC_DATABASE_URL"], newURL) {
		t.Fatalf("PUBLIC_DATABASE_URL = %q", byKey["PUBLIC_DATABASE_URL"])
	}
	if strings.Contains(byKey["PUBLIC_DATABASE_URL"], ":5432") {
		t.Fatalf("PUBLIC_DATABASE_URL still has preferred port: %q", byKey["PUBLIC_DATABASE_URL"])
	}
	if !strings.Contains(byKey["DATABASE_URL"], ".local:5432") {
		t.Fatalf("internal DATABASE_URL should stay on service port, got %q", byKey["DATABASE_URL"])
	}
	if byKey["OTHER"] != "keep-me" {
		t.Fatalf("OTHER = %q", byKey["OTHER"])
	}
}

func TestRefreshTemplateGeneratedEnvVarsUsesLeasedPublicURL(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	if err := s.SeedBuiltins(); err != nil {
		t.Fatal(err)
	}
	tpl := findBuiltin(t, s, "PostgreSQL")
	dir := t.TempDir()
	p, err := s.CreateProject("pg-lease", dir, "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateNode(&store.CanvasNode{
		ID:            "pg1",
		Label:         "db",
		ProjectID:     p.ID,
		EnvironmentID: defaultEnvID(t, s, p.ID),
		TemplateID:    tpl.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureNodeUID(node.ID); err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting(node.ID, "service_port", "5432")
	_ = s.SetNodeSetting(node.ID, "route_protocol", "tcp")
	_ = s.SetNodeSetting(node.ID, "host_port", "5432")
	_ = s.SetNodeSetting(node.ID, "image", "postgres:16-alpine")

	// Stamp-like preferred-port URL (what Variables shows before a lease fallback).
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: node.ID, Key: "PUBLIC_DATABASE_URL", Scope: store.EnvScopeRuntime,
		Source: store.EnvSourceGenerated,
		Value:  "postgres://postgres:secret@db.example.draft.resolv.sh:5432/postgres",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: node.ID, Key: "DATABASE_URL", Scope: store.EnvScopeRuntime,
		Source: store.EnvSourceGenerated,
		Value:  "postgres://postgres:secret@db.example.draft.local:5432/postgres",
	}); err != nil {
		t.Fatal(err)
	}

	addr, err := e.computeNodeAddress(node)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.UpsertRoute(&store.Route{
		Hostname:    addr.InternalHostname,
		ProjectID:   p.ID,
		NodeID:      node.ID,
		Environment: addr.Environment,
		Protocol:    "tcp",
		TargetHost:  "127.0.0.1",
		TargetPort:  5432,
		HostPort:    32831,
	}); err != nil {
		t.Fatal(err)
	}
	if err := e.refreshTemplateGeneratedEnvVars(node.ID); err != nil {
		t.Fatal(err)
	}
	v, err := s.GetEnvVar(node.ID, "PUBLIC_DATABASE_URL")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(v.Value, ":32831") {
		t.Fatalf("PUBLIC_DATABASE_URL = %q, want leased port 32831", v.Value)
	}
	if strings.Contains(v.Value, ":5432/") || strings.HasSuffix(v.Value, ":5432/postgres") {
		t.Fatalf("PUBLIC_DATABASE_URL still preferred port: %q", v.Value)
	}
	internal, err := s.GetEnvVar(node.ID, "DATABASE_URL")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(internal.Value, ":5432") {
		t.Fatalf("DATABASE_URL should keep service port, got %q", internal.Value)
	}
}

func TestComputeNodeAddressHTTPKeepsURLs(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	p, err := s.CreateProject("http-proj", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	node, err := s.CreateNode(&store.CanvasNode{
		ID:            "http-node",
		ProjectID:     p.ID,
		EnvironmentID: defaultEnvID(t, s, p.ID),
		Label:         "web",
	})
	if err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting(node.ID, "service_port", "3000")
	// default route_protocol is http

	addr, err := e.computeNodeAddress(node)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(addr.InternalURL, "http://") {
		t.Errorf("InternalURL = %q, want http://…", addr.InternalURL)
	}
}
