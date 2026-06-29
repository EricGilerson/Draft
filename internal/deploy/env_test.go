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
		NodeID:      "n1",
		ServiceName: "web",
		ProjectName: "draft",
		Environment: "default",
		Port:        "3000",
		Hostname:    "web.draft.default.abcd.draft.local",
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
	if runtime["DRAFT_PORT"] != "3000" || runtime["DRAFT_HOSTNAME"] != "web.draft.default.abcd.draft.local" {
		t.Fatalf("generated env = %+v", runtime)
	}
	if _, ok := resolved.BuildArgs["RUNTIME_ONLY"]; ok {
		t.Fatal("runtime-only var should not be a build arg")
	}
	if arg := resolved.BuildArgs["BUILD_ME"]; arg == nil || *arg != "two" {
		t.Fatalf("BUILD_ME build arg = %v", arg)
	}
}

func TestResolveDeploymentEnvRejectsReservedDraftKeys(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	if err := s.SetEnvVar("n1", "DRAFT_PORT", "1234"); err != nil {
		t.Fatal(err)
	}

	_, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:      "n1",
		ServiceName: "web",
		ProjectName: "draft",
		Port:        "3000",
		Hostname:    "web.draft.default.abcd.draft.local",
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
