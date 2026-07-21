package store

import (
	"path/filepath"
	"testing"
)

func TestMergeNodeSettings(t *testing.T) {
	applied := map[string]string{"service_port": "5432", "image": "postgres:16"}
	staged := map[string]string{"service_port": "5433", "volume_mounts": `[{"type":"volume","containerPath":"/data"}]`}
	merged := MergeNodeSettings(applied, staged)
	if merged["service_port"] != "5433" {
		t.Fatalf("staged override failed: %q", merged["service_port"])
	}
	if merged["image"] != "postgres:16" {
		t.Fatalf("applied passthrough failed: %q", merged["image"])
	}
	if merged["volume_mounts"] == "" {
		t.Fatal("expected staged volume_mounts")
	}
}

func TestPromoteStagedToApplied(t *testing.T) {
	s := openTemp(t)
	projectPath := t.TempDir()
	p, err := s.CreateProject("p", projectPath, "")
	if err != nil {
		t.Fatal(err)
	}
	envID, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: p.ID, EnvironmentID: envID.ID, Label: "db", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetNodeSetting(n.ID, "service_port", "5432"); err != nil {
		t.Fatal(err)
	}
	if err := s.StageNodeSettings(n.ID, map[string]string{"service_port": "5433"}); err != nil {
		t.Fatal(err)
	}

	effective, err := s.EffectiveNodeSettings(n.ID)
	if err != nil || effective["service_port"] != "5433" {
		t.Fatalf("effective = %v err=%v", effective, err)
	}

	applied, _ := s.GetNodeSettings(n.ID)
	if applied["service_port"] != "5432" {
		t.Fatalf("applied should be unchanged before promote: %v", applied)
	}

	if err := s.PromoteStagedToApplied(n.ID); err != nil {
		t.Fatal(err)
	}
	applied, _ = s.GetNodeSettings(n.ID)
	if applied["service_port"] != "5433" {
		t.Fatalf("applied after promote = %v", applied)
	}
	has, _ := s.HasStagedChanges(n.ID)
	if has {
		t.Fatal("staged should be cleared")
	}
}

func TestDiscardStagedChangesPartial(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProject("p-partial", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	env, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CreateNode(&CanvasNode{ID: "n-partial", ProjectID: p.ID, EnvironmentID: env.ID, Label: "api", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetNodeSetting(n.ID, "service_port", "8080"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeSetting(n.ID, "dockerfile", "Dockerfile"); err != nil {
		t.Fatal(err)
	}
	if err := s.StageNodeSettings(n.ID, map[string]string{
		"service_port": "9090",
		"dockerfile":   "Dockerfile.prod",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(EnvVar{NodeID: n.ID, Key: "A", Value: "1", Scope: EnvScopeRuntime, Source: EnvSourceManual}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(EnvVar{NodeID: n.ID, Key: "B", Value: "2", Scope: EnvScopeRuntime, Source: EnvSourceManual}); err != nil {
		t.Fatal(err)
	}
	if err := s.StageEnvVarChanges(n.ID, []EnvVarStageUpsert{
		{Key: "A", Value: "10", Scope: EnvScopeRuntime},
		{Key: "B", Value: "20", Scope: EnvScopeRuntime},
	}, nil); err != nil {
		t.Fatal(err)
	}

	if err := s.DiscardStagedChangesPartial(n.ID, []string{"service_port"}, []string{"A"}); err != nil {
		t.Fatal(err)
	}

	stagedSettings, err := s.GetStagedNodeSettings(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := stagedSettings["service_port"]; ok {
		t.Fatal("service_port should be discarded")
	}
	if stagedSettings["dockerfile"] != "Dockerfile.prod" {
		t.Fatalf("dockerfile staged = %q, want kept", stagedSettings["dockerfile"])
	}

	stagedEnv, err := s.ListStagedEnvVarChanges(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	byKey := map[string]EnvVarStaged{}
	for _, ch := range stagedEnv {
		byKey[ch.Key] = ch
	}
	if _, ok := byKey["A"]; ok {
		t.Fatal("env A should be discarded")
	}
	if byKey["B"].Value != "20" {
		t.Fatalf("env B = %+v, want kept", byKey["B"])
	}
}

func TestEffectiveEnvVarsWithStagedDelete(t *testing.T) {
	s := openTemp(t)
	projectPath := filepath.Join(t.TempDir(), "p2")
	p, err := s.CreateProject("p2", projectPath, "")
	if err != nil {
		t.Fatal(err)
	}
	envID, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CreateNode(&CanvasNode{ID: "n2", ProjectID: p.ID, EnvironmentID: envID.ID, Label: "app", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpsertEnvVar(EnvVar{NodeID: n.ID, Key: "FOO", Value: "1", Scope: EnvScopeRuntime, Source: EnvSourceManual}); err != nil {
		t.Fatal(err)
	}
	if err := s.StageEnvVarChanges(n.ID, nil, []string{"FOO"}); err != nil {
		t.Fatal(err)
	}

	effective, err := s.EffectiveEnvVars(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, v := range effective {
		if v.Key == "FOO" {
			t.Fatal("FOO should be removed in effective view")
		}
	}

	if err := s.PromoteStagedToApplied(n.ID); err != nil {
		t.Fatal(err)
	}
	vars, _ := s.ListEnvVars(n.ID)
	for _, v := range vars {
		if v.Key == "FOO" {
			t.Fatal("FOO should be deleted after promote")
		}
	}
}
