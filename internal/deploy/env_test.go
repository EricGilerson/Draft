package deploy

import (
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
	if _, err := s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, Label: "api"}); err != nil {
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

func splitEnv(item string) (string, string, bool) {
	for i, r := range item {
		if r == '=' {
			return item[:i], item[i+1:], true
		}
	}
	return "", "", false
}
