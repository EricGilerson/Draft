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
