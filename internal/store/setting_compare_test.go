package store

import "testing"

func TestSettingValuesEqualBooleanNormalization(t *testing.T) {
	cases := []struct {
		a, b string
		want bool
	}{
		{"", "", true},
		{"false", "", true},
		{"", "false", true},
		{"true", "true", true},
		{"true", "", false},
		{"false", "true", false},
		{"5432", "5432", true},
		{"5432", "5433", false},
	}
	for _, tc := range cases {
		if got := settingValuesEqual(tc.a, tc.b); got != tc.want {
			t.Fatalf("settingValuesEqual(%q, %q) = %v, want %v", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestStageNodeSettingsRevertClearsStaged(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProject("p", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	envID, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: p.ID, EnvironmentID: envID.ID, Label: "app", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetNodeSetting(n.ID, "build_no_cache", ""); err != nil {
		t.Fatal(err)
	}
	if err := s.StageNodeSettings(n.ID, map[string]string{"build_no_cache": "true"}); err != nil {
		t.Fatal(err)
	}
	has, _ := s.HasStagedChanges(n.ID)
	if !has {
		t.Fatal("expected staged after toggling on")
	}

	if err := s.StageNodeSettings(n.ID, map[string]string{"build_no_cache": ""}); err != nil {
		t.Fatal(err)
	}
	has, _ = s.HasStagedChanges(n.ID)
	if has {
		t.Fatal("expected no staged changes after reverting to applied value")
	}
	staged, _ := s.GetStagedNodeSettings(n.ID)
	if len(staged) > 0 {
		t.Fatalf("staged map should be empty, got %v", staged)
	}
}

func TestStageNodeSettingsFalseMatchesAppliedEmpty(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProject("p", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	envID, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: p.ID, EnvironmentID: envID.ID, Label: "app", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.SetNodeSetting(n.ID, "use_dockerignore", "false"); err != nil {
		t.Fatal(err)
	}
	if err := s.StageNodeSettings(n.ID, map[string]string{"use_dockerignore": "true"}); err != nil {
		t.Fatal(err)
	}
	if err := s.StageNodeSettings(n.ID, map[string]string{"use_dockerignore": "false"}); err != nil {
		t.Fatal(err)
	}
	has, _ := s.HasStagedChanges(n.ID)
	if has {
		t.Fatal("false should match applied false and clear staged row")
	}
}

func TestStageEnvVarRevertClearsStaged(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProject("p", t.TempDir(), "")
	if err != nil {
		t.Fatal(err)
	}
	envID, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: p.ID, EnvironmentID: envID.ID, Label: "app", X: 0, Y: 0})
	if err != nil {
		t.Fatal(err)
	}

	if err := s.UpsertEnvVar(EnvVar{NodeID: n.ID, Key: "FOO", Value: "1", Scope: EnvScopeRuntime, Source: EnvSourceManual}); err != nil {
		t.Fatal(err)
	}
	if err := s.StageEnvVarChanges(n.ID, []EnvVarStageUpsert{{Key: "FOO", Value: "2", Scope: EnvScopeRuntime}}, nil); err != nil {
		t.Fatal(err)
	}
	has, _ := s.HasStagedChanges(n.ID)
	if !has {
		t.Fatal("expected staged env change")
	}

	if err := s.StageEnvVarChanges(n.ID, []EnvVarStageUpsert{{Key: "FOO", Value: "1", Scope: EnvScopeRuntime}}, nil); err != nil {
		t.Fatal(err)
	}
	has, _ = s.HasStagedChanges(n.ID)
	if has {
		t.Fatal("reverting env var to applied should clear staged row")
	}
}
