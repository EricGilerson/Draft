package deploy

import (
	"context"
	"testing"

	"Draft/internal/store"
)

func TestPreviewSyncSettingsAndEnv(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	e := New(s, nil, t.TempDir(), nil)

	project, err := s.CreateProject("sync-proj", t.TempDir(), "")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	mainEnv, err := s.GetDefaultEnvironment(project.ID)
	if err != nil {
		t.Fatalf("main env: %v", err)
	}
	staging, err := s.CreateEnvironment(project.ID, "Staging")
	if err != nil {
		t.Fatalf("staging: %v", err)
	}

	src, err := s.CreateNode(&store.CanvasNode{
		ID: "src-api", ProjectID: project.ID, EnvironmentID: mainEnv.ID, Label: "api",
	})
	if err != nil {
		t.Fatalf("src node: %v", err)
	}
	tgt, err := s.CreateNode(&store.CanvasNode{
		ID: "tgt-api", ProjectID: project.ID, EnvironmentID: staging.ID, Label: "api",
	})
	if err != nil {
		t.Fatalf("tgt node: %v", err)
	}

	_ = s.SetNodeSetting(src.ID, "service_port", "8080")
	_ = s.SetNodeSetting(src.ID, "dockerfile", "Dockerfile")
	_ = s.SetNodeSetting(src.ID, "git_branch", "main")
	_ = s.SetNodeSetting(tgt.ID, "service_port", "3000")
	_ = s.SetNodeSetting(tgt.ID, "dockerfile", "Dockerfile")
	_ = s.SetNodeSetting(tgt.ID, "git_branch", "develop")

	_ = s.UpsertEnvVar(store.EnvVar{
		NodeID: src.ID, Key: "NODE_ENV", Value: "production", Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
	})
	_ = s.UpsertEnvVar(store.EnvVar{
		NodeID: tgt.ID, Key: "NODE_ENV", Value: "development", Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
	})
	_ = s.UpsertEnvVar(store.EnvVar{
		NodeID: src.ID, Key: "SECRET_KEY", Value: "src-secret", Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual, Secret: true,
	})
	_ = s.UpsertEnvVar(store.EnvVar{
		NodeID: tgt.ID, Key: "SECRET_KEY", Value: "tgt-secret", Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual, Secret: true,
	})

	preview, err := e.PreviewSync(SyncRequest{
		Scope:               SyncScopeEnvironment,
		SourceEnvironmentID: mainEnv.ID,
		TargetEnvironmentID: staging.ID,
		IncludeSettings:     true,
		IncludeEnv:          true,
	})
	if err != nil {
		t.Fatalf("PreviewSync: %v", err)
	}
	if preview.ActionableCount == 0 {
		t.Fatal("expected actionable changes")
	}
	if len(preview.Services) != 1 {
		t.Fatalf("services = %d", len(preview.Services))
	}
	svc := preview.Services[0]

	var portDiff, gitDiff *SyncSettingDiff
	for i := range svc.Settings {
		d := &svc.Settings[i]
		if d.Key == "service_port" {
			portDiff = d
		}
		if d.Key == "git_branch" {
			gitDiff = d
		}
	}
	if portDiff == nil || portDiff.Action != SyncActionSet {
		t.Fatalf("service_port diff = %+v", portDiff)
	}
	if gitDiff == nil || gitDiff.Action != SyncActionSkip {
		t.Fatalf("git_branch should be skipped, got %+v", gitDiff)
	}

	var nodeEnv, secretDiff *SyncEnvDiff
	for i := range svc.Env {
		d := &svc.Env[i]
		if d.Key == "NODE_ENV" {
			nodeEnv = d
		}
		if d.Key == "SECRET_KEY" {
			secretDiff = d
		}
	}
	if nodeEnv == nil || nodeEnv.Action != SyncActionSet {
		t.Fatalf("NODE_ENV = %+v", nodeEnv)
	}
	if secretDiff == nil || secretDiff.Action != SyncActionSkip {
		t.Fatalf("SECRET_KEY should skip overwrite, got %+v", secretDiff)
	}

	// Stage only
	result, err := e.ApplySync(context.Background(), SyncRequest{
		Scope:               SyncScopeEnvironment,
		SourceEnvironmentID: mainEnv.ID,
		TargetEnvironmentID: staging.ID,
		IncludeSettings:     true,
		IncludeEnv:          true,
	}, SyncModeStage)
	if err != nil {
		t.Fatalf("ApplySync: %v", err)
	}
	if len(result.Results) != 1 || !result.Results[0].Staged {
		t.Fatalf("apply result = %+v", result.Results)
	}

	eff, err := s.EffectiveNodeSettings(tgt.ID)
	if err != nil {
		t.Fatalf("effective: %v", err)
	}
	if eff["service_port"] != "8080" {
		t.Errorf("staged port = %q", eff["service_port"])
	}
	// Applied still old until deploy promote
	applied, _ := s.GetNodeSettings(tgt.ID)
	if applied["service_port"] != "3000" {
		t.Errorf("applied port should stay 3000 until deploy, got %q", applied["service_port"])
	}

	envEff, err := s.EffectiveEnvVars(tgt.ID)
	if err != nil {
		t.Fatalf("env effective: %v", err)
	}
	byKey := map[string]store.EnvVar{}
	for _, v := range envEff {
		byKey[v.Key] = v
	}
	if byKey["NODE_ENV"].Value != "production" {
		t.Errorf("NODE_ENV effective = %q", byKey["NODE_ENV"].Value)
	}
	if byKey["SECRET_KEY"].Value != "tgt-secret" {
		t.Errorf("secret should remain target value, got %q", byKey["SECRET_KEY"].Value)
	}
}

func TestPreviewSyncNoChangesDisables(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	e := New(s, nil, t.TempDir(), nil)

	project, err := s.CreateProject("sync-same", t.TempDir(), "")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	mainEnv, _ := s.GetDefaultEnvironment(project.ID)
	staging, _ := s.CreateEnvironment(project.ID, "Staging")

	src, _ := s.CreateNode(&store.CanvasNode{ID: "a", ProjectID: project.ID, EnvironmentID: mainEnv.ID, Label: "web"})
	tgt, _ := s.CreateNode(&store.CanvasNode{ID: "b", ProjectID: project.ID, EnvironmentID: staging.ID, Label: "web"})
	_ = s.SetNodeSetting(src.ID, "service_port", "8080")
	_ = s.SetNodeSetting(tgt.ID, "service_port", "8080")

	preview, err := e.PreviewSync(SyncRequest{
		Scope:               SyncScopeService,
		SourceEnvironmentID: mainEnv.ID,
		TargetEnvironmentID: staging.ID,
		SourceNodeID:        src.ID,
		TargetNodeID:        tgt.ID,
		IncludeSettings:     true,
		IncludeEnv:          false,
	})
	if err != nil {
		t.Fatalf("PreviewSync: %v", err)
	}
	if preview.ActionableCount != 0 {
		t.Fatalf("expected 0 actionable, got %d (%+v)", preview.ActionableCount, preview.Services)
	}
}

func TestPreviewSyncSkipsLinkedTarget(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	e := New(s, nil, t.TempDir(), nil)

	project, err := s.CreateProject("sync-link", t.TempDir(), "")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	mainEnv, _ := s.GetDefaultEnvironment(project.ID)
	staging, _ := s.CreateEnvironment(project.ID, "Staging")

	root, _ := s.CreateNode(&store.CanvasNode{ID: "root-db", ProjectID: project.ID, EnvironmentID: mainEnv.ID, Label: "db"})
	alias, _ := s.CreateNode(&store.CanvasNode{ID: "alias-db", ProjectID: project.ID, EnvironmentID: staging.ID, Label: "db"})
	_ = s.SetNodeSetting(root.ID, "service_port", "5432")
	_ = s.SetNodeSetting(alias.ID, "service_port", "5432")
	if err := e.SetServiceLink(alias.ID, root.ID); err != nil {
		t.Fatalf("link: %v", err)
	}

	preview, err := e.PreviewSync(SyncRequest{
		Scope:               SyncScopeEnvironment,
		SourceEnvironmentID: mainEnv.ID,
		TargetEnvironmentID: staging.ID,
		IncludeSettings:     true,
		IncludeEnv:          true,
	})
	if err != nil {
		t.Fatalf("PreviewSync: %v", err)
	}
	if len(preview.Services) != 1 || !preview.Services[0].Skipped {
		t.Fatalf("expected skipped linked target, got %+v", preview.Services)
	}
	if preview.ActionableCount != 0 {
		t.Fatalf("actionable = %d", preview.ActionableCount)
	}
}

func TestPreviewSyncCreateMissingService(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	e := New(s, nil, t.TempDir(), nil)

	project, err := s.CreateProject("sync-create", t.TempDir(), "")
	if err != nil {
		t.Fatalf("project: %v", err)
	}
	mainEnv, _ := s.GetDefaultEnvironment(project.ID)
	staging, _ := s.CreateEnvironment(project.ID, "Staging")

	src, err := s.CreateNode(&store.CanvasNode{
		ID: "src-worker", ProjectID: project.ID, EnvironmentID: mainEnv.ID, Label: "worker",
	})
	if err != nil {
		t.Fatalf("src: %v", err)
	}
	_ = s.SetNodeSetting(src.ID, "service_port", "9000")
	_ = s.SetNodeSetting(src.ID, "image", "busybox:latest")
	_ = s.UpsertEnvVar(store.EnvVar{
		NodeID: src.ID, Key: "ROLE", Value: "worker", Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
	})

	// Without createMissing: unmatched only.
	preview, err := e.PreviewSync(SyncRequest{
		Scope:               SyncScopeEnvironment,
		SourceEnvironmentID: mainEnv.ID,
		TargetEnvironmentID: staging.ID,
		IncludeSettings:     true,
		IncludeEnv:          true,
		CreateMissing:       false,
	})
	if err != nil {
		t.Fatalf("PreviewSync: %v", err)
	}
	if len(preview.UnmatchedSource) != 1 || preview.UnmatchedSource[0] != "worker" {
		t.Fatalf("unmatched = %+v", preview.UnmatchedSource)
	}
	if preview.ActionableCount != 0 {
		t.Fatalf("actionable without create = %d", preview.ActionableCount)
	}

	preview, err = e.PreviewSync(SyncRequest{
		Scope:               SyncScopeEnvironment,
		SourceEnvironmentID: mainEnv.ID,
		TargetEnvironmentID: staging.ID,
		IncludeSettings:     true,
		IncludeEnv:          true,
		CreateMissing:       true,
	})
	if err != nil {
		t.Fatalf("PreviewSync create: %v", err)
	}
	if len(preview.UnmatchedSource) != 0 {
		t.Fatalf("unmatched should be empty when creating, got %+v", preview.UnmatchedSource)
	}
	if len(preview.Services) != 1 || !preview.Services[0].WillCreate {
		t.Fatalf("expected willCreate service, got %+v", preview.Services)
	}
	if preview.ActionableCount == 0 {
		t.Fatal("expected actionable create")
	}

	result, err := e.ApplySync(context.Background(), SyncRequest{
		Scope:               SyncScopeEnvironment,
		SourceEnvironmentID: mainEnv.ID,
		TargetEnvironmentID: staging.ID,
		IncludeSettings:     true,
		IncludeEnv:          true,
		CreateMissing:       true,
	}, SyncModeStage)
	if err != nil {
		t.Fatalf("ApplySync: %v", err)
	}
	if len(result.Results) != 1 || !result.Results[0].Created {
		t.Fatalf("apply = %+v", result.Results)
	}

	nodes, err := s.ListNodesByEnvironment(staging.ID)
	if err != nil || len(nodes) != 1 || nodes[0].Label != "worker" {
		t.Fatalf("target nodes = %+v err=%v", nodes, err)
	}
	eff, _ := s.EffectiveNodeSettings(nodes[0].ID)
	if eff["service_port"] != "9000" || eff["image"] != "busybox:latest" {
		t.Fatalf("settings = %+v", eff)
	}
	vars, _ := s.EffectiveEnvVars(nodes[0].ID)
	found := false
	for _, v := range vars {
		if v.Key == "ROLE" && v.Value == "worker" {
			found = true
		}
	}
	if !found {
		t.Fatalf("ROLE env missing: %+v", vars)
	}

	// Second sync: no longer a create — now a matched pair with no diffs.
	preview, err = e.PreviewSync(SyncRequest{
		Scope:               SyncScopeEnvironment,
		SourceEnvironmentID: mainEnv.ID,
		TargetEnvironmentID: staging.ID,
		IncludeSettings:     true,
		IncludeEnv:          true,
		CreateMissing:       true,
	})
	if err != nil {
		t.Fatalf("PreviewSync after: %v", err)
	}
	if len(preview.Services) != 1 || preview.Services[0].WillCreate {
		t.Fatalf("after create should match, got %+v", preview.Services)
	}
}

func TestPreviewSyncServiceScopeCreateMissing(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer s.Close()
	e := New(s, nil, t.TempDir(), nil)

	project, _ := s.CreateProject("sync-svc-create", t.TempDir(), "")
	mainEnv, _ := s.GetDefaultEnvironment(project.ID)
	staging, _ := s.CreateEnvironment(project.ID, "Staging")
	src, _ := s.CreateNode(&store.CanvasNode{
		ID: "src-api", ProjectID: project.ID, EnvironmentID: mainEnv.ID, Label: "api",
	})
	_ = s.SetNodeSetting(src.ID, "service_port", "8080")

	preview, err := e.PreviewSync(SyncRequest{
		Scope:               SyncScopeService,
		SourceEnvironmentID: mainEnv.ID,
		TargetEnvironmentID: staging.ID,
		SourceNodeID:        src.ID,
		CreateMissing:       true,
		IncludeSettings:     true,
	})
	if err != nil {
		t.Fatalf("PreviewSync: %v", err)
	}
	if len(preview.Services) != 1 || !preview.Services[0].WillCreate {
		t.Fatalf("expected create preview, got %+v", preview.Services)
	}

	result, err := e.ApplySync(context.Background(), SyncRequest{
		Scope:               SyncScopeService,
		SourceEnvironmentID: mainEnv.ID,
		TargetEnvironmentID: staging.ID,
		SourceNodeID:        src.ID,
		CreateMissing:       true,
		IncludeSettings:     true,
	}, SyncModeStage)
	if err != nil {
		t.Fatalf("ApplySync: %v", err)
	}
	if !result.Results[0].Created {
		t.Fatalf("result = %+v", result.Results)
	}
}

func TestNormalizeVolumeMountsForSync(t *testing.T) {
	raw := `[{"type":"volume","source":"draft-1-main-abcd-data","containerPath":"/data"},{"type":"bind","source":"/host/path","containerPath":"/cfg"}]`
	got := normalizeVolumeMountsForSync(raw)
	if !containsAll(got, `"type":"volume"`, `"/data"`, `"type":"bind"`, `"/host/path"`) {
		t.Fatalf("normalized = %s", got)
	}
	if containsAll(got, "draft-1-main") {
		t.Fatalf("should strip managed volume source name: %s", got)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, p := range parts {
		if !stringContains(s, p) {
			return false
		}
	}
	return true
}

func stringContains(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(sub) == 0 || indexOf(s, sub) >= 0)
}

func indexOf(s, sub string) int {
	for i := 0; i+len(sub) <= len(s); i++ {
		if s[i:i+len(sub)] == sub {
			return i
		}
	}
	return -1
}
