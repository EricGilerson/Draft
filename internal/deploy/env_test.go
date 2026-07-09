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

func splitEnv(item string) (string, string, bool) {
	for i, r := range item {
		if r == '=' {
			return item[:i], item[i+1:], true
		}
	}
	return "", "", false
}
