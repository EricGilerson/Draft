package store

import (
	"testing"
	"time"
)

func TestGetNodeSettingsByNodes(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProject("bulk", "/bulk", "")
	if err != nil {
		t.Fatal(err)
	}
	env, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&CanvasNode{ID: "a", ProjectID: p.ID, EnvironmentID: env.ID, Label: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&CanvasNode{ID: "b", ProjectID: p.ID, EnvironmentID: env.ID, Label: "b"}); err != nil {
		t.Fatal(err)
	}
	_ = s.SetNodeSetting("a", "service_port", "3000")
	_ = s.SetNodeSetting("b", "image", "redis:7")

	got, err := s.GetNodeSettingsByNodes([]string{"a", "b", "missing"})
	if err != nil {
		t.Fatal(err)
	}
	if got["a"]["service_port"] != "3000" {
		t.Fatalf("a = %+v", got["a"])
	}
	if got["b"]["image"] != "redis:7" {
		t.Fatalf("b = %+v", got["b"])
	}
	if got["missing"] == nil {
		t.Fatal("missing node should have empty map")
	}
}

func TestListEnvVarsByNodes(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProject("bulk-env", "/bulk-env", "")
	if err != nil {
		t.Fatal(err)
	}
	env, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&CanvasNode{ID: "a", ProjectID: p.ID, EnvironmentID: env.ID, Label: "a"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&CanvasNode{ID: "b", ProjectID: p.ID, EnvironmentID: env.ID, Label: "b"}); err != nil {
		t.Fatal(err)
	}
	_ = s.UpsertEnvVar(EnvVar{NodeID: "a", Key: "A", Value: "1"})
	_ = s.UpsertEnvVar(EnvVar{NodeID: "b", Key: "B", Value: "2"})

	got, err := s.ListEnvVarsByNodes([]string{"a", "b"})
	if err != nil {
		t.Fatal(err)
	}
	if len(got["a"]) != 1 || got["a"][0].Key != "A" {
		t.Fatalf("a = %+v", got["a"])
	}
	if len(got["b"]) != 1 || got["b"][0].Key != "B" {
		t.Fatalf("b = %+v", got["b"])
	}
}

func TestListActiveDeploymentsByNodes(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})
	s.DB.Create(&CanvasNode{ID: "n2", ProjectID: 1, Label: "other"})
	s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "stopped"})
	s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "running", ImageTag: "live"})
	s.CreateDeployment(&Deployment{NodeID: "n2", ProjectID: 1, Status: "failed"})

	got, err := s.ListActiveDeploymentsByNodes([]string{"n1", "n2"})
	if err != nil {
		t.Fatal(err)
	}
	if got["n1"] == nil || got["n1"].ImageTag != "live" {
		t.Fatalf("n1 = %+v", got["n1"])
	}
	if _, ok := got["n2"]; ok {
		t.Fatalf("n2 should have no active dep, got %+v", got["n2"])
	}
}

func TestListDeploymentsLimited(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})
	for i := 0; i < 4; i++ {
		if _, err := s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "stopped"}); err != nil {
			t.Fatal(err)
		}
	}
	limited, err := s.ListDeploymentsLimited("n1", 2)
	if err != nil || len(limited) != 2 {
		t.Fatalf("limited = %d err=%v", len(limited), err)
	}
	all, err := s.ListDeploymentsLimited("n1", 0)
	if err != nil || len(all) != 4 {
		t.Fatalf("unbounded = %d err=%v", len(all), err)
	}
}

func TestListSandboxesForLifecycle(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProject("sand", "/sand", "")
	if err != nil {
		t.Fatal(err)
	}
	src, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	liveEnv, err := s.CreateEnvironment(p.ID, "Live")
	if err != nil {
		t.Fatal(err)
	}
	purgedEnv, err := s.CreateEnvironment(p.ID, "Gone")
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	if _, err := s.CreateSandbox(&Sandbox{
		ProjectID:           p.ID,
		EnvironmentID:       liveEnv.ID,
		SourceEnvironmentID: src.ID,
		Name:                "live",
		Purpose:             "preview",
		Status:              "active",
		PlanJSON:            "{}",
		ExpiresAt:           now.Add(time.Hour),
		WarnAt:              now.Add(30 * time.Minute),
		GraceEndsAt:         now.Add(2 * time.Hour),
	}, nil, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateSandbox(&Sandbox{
		ProjectID:           p.ID,
		EnvironmentID:       purgedEnv.ID,
		SourceEnvironmentID: src.ID,
		Name:                "purged",
		Purpose:             "preview",
		Status:              "purged",
		PlanJSON:            "{}",
		ExpiresAt:           now.Add(-time.Hour),
		WarnAt:              now.Add(-2 * time.Hour),
		GraceEndsAt:         now.Add(-30 * time.Minute),
	}, nil, nil); err != nil {
		t.Fatal(err)
	}

	rows, err := s.ListSandboxesForLifecycle()
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 1 || rows[0].Name != "live" {
		t.Fatalf("lifecycle rows = %+v", rows)
	}
}
