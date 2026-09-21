package store

import (
	"errors"
	"testing"
)

func TestCreateAndListProjects(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateProject("api", `C:\code\api`, "backend service"); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if _, err := s.CreateProject("web", `C:\code\web`, ""); err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	list, err := s.ListProjects()
	if err != nil {
		t.Fatalf("ListProjects: %v", err)
	}
	if len(list) != 2 {
		t.Fatalf("got %d projects, want 2", len(list))
	}
	// Newest first.
	if list[0].Name != "web" {
		t.Errorf("first project = %q, want web", list[0].Name)
	}
	if list[1].Description != "backend service" {
		t.Errorf("description = %q, want 'backend service'", list[1].Description)
	}
}

func TestCreateProjectTrimsAndValidates(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateProject("   ", `C:\code\api`, ""); !errors.Is(err, ErrInvalidProject) {
		t.Errorf("blank name: got %v, want ErrInvalidProject", err)
	}
	if _, err := s.CreateProject("api", "  ", ""); !errors.Is(err, ErrInvalidProject) {
		t.Errorf("blank path: got %v, want ErrInvalidProject", err)
	}

	p, err := s.CreateProject("  api  ", `  C:\code\api  `, "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	if p.Name != "api" || p.Path != `C:\code\api` {
		t.Errorf("not trimmed: name=%q path=%q", p.Name, p.Path)
	}
}

func TestCreateProjectDuplicateName(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateProject("api", `C:\code\api`, ""); err != nil {
		t.Fatalf("first create: %v", err)
	}
	if _, err := s.CreateProject("api", `C:\code\api2`, ""); err == nil {
		t.Error("expected error creating project with duplicate name, got nil")
	}
}

func TestDeleteProjectFreesNamePathAndCascades(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProject("Structora", `D:\BuildOS`, "Imported from Draft pack")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	envs, err := s.ListEnvironments(p.ID)
	if err != nil || len(envs) == 0 {
		t.Fatalf("ListEnvironments: %v len=%d", err, len(envs))
	}
	node, err := s.CreateNode(&CanvasNode{
		ID: "svc-test-1", Label: "Redis", ProjectID: p.ID, EnvironmentID: envs[0].ID,
	})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := s.SetNodeSetting(node.ID, "image", "redis:7-alpine"); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(EnvVar{NodeID: node.ID, Key: "REDIS_URL", Value: "redis://x", Scope: EnvScopeRuntime}); err != nil {
		t.Fatal(err)
	}
	if err := s.StageNodeSettings(node.ID, map[string]string{"service_port": "6379"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDeployment(&Deployment{NodeID: node.ID, ProjectID: p.ID, Status: "running", ImageTag: "redis:7"}); err != nil {
		t.Fatalf("CreateDeployment: %v", err)
	}

	if err := s.DeleteProject(p.ID); err != nil {
		t.Fatalf("DeleteProject: %v", err)
	}

	list, err := s.ListProjects()
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("projects left after delete: %+v", list)
	}
	// Name and path must be free for re-import / recreate.
	p2, err := s.CreateProject("Structora", `D:\BuildOS`, "reimport")
	if err != nil {
		t.Fatalf("recreate after delete: %v", err)
	}
	if p2.ID == p.ID {
		// IDs may reuse with sqlite_sequence reset; either way create must succeed.
	}
	var nodes int64
	if err := s.DB.Model(&CanvasNode{}).Where("project_id = ?", p.ID).Count(&nodes).Error; err != nil {
		t.Fatal(err)
	}
	if nodes != 0 {
		t.Fatalf("orphan canvas_nodes for deleted project: %d", nodes)
	}
	var deps int64
	if err := s.DB.Model(&Deployment{}).Where("project_id = ?", p.ID).Count(&deps).Error; err != nil {
		t.Fatal(err)
	}
	if deps != 0 {
		t.Fatalf("orphan deployments: %d", deps)
	}
}
