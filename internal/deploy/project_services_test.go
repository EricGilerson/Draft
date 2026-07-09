package deploy

import (
	"testing"
	"time"

	"Draft/internal/store"
)

func TestListProjectServicesUsesRealCanvasNodes(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()

	project, err := s.CreateProject("Real Services", "/tmp/real-services", "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	envID := defaultEnvID(t, s, project.ID)
	if _, err := s.CreateNode(&store.CanvasNode{ID: "web-1", ProjectID: project.ID, EnvironmentID: envID, Label: "web"}); err != nil {
		t.Fatalf("create web node: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "worker-1", ProjectID: project.ID, EnvironmentID: envID, Label: "queue worker"}); err != nil {
		t.Fatalf("create worker node: %v", err)
	}
	if err := s.SetNodeSetting("web-1", "service_port", "8080"); err != nil {
		t.Fatalf("set web port: %v", err)
	}
	if err := s.SetNodeSetting("web-1", "dockerfile", "services/web/Dockerfile"); err != nil {
		t.Fatalf("set web dockerfile: %v", err)
	}
	now := time.Now()
	if _, err := s.CreateDeployment(&store.Deployment{
		NodeID:    "web-1",
		ProjectID: project.ID,
		Status:    "running",
		ImageTag:  "draft-real-services-web:1",
		Hostname:  "web.real.default.test.draft.local",
		HostPort:  49152,
		UpdatedAt: now,
	}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	services, err := ListProjectServices(s, project.ID)
	if err != nil {
		t.Fatalf("ListProjectServices: %v", err)
	}
	if len(services) != 2 {
		t.Fatalf("expected 2 real services, got %d: %+v", len(services), services)
	}

	web := services[0]
	if web.ID != "web-1" || web.Name != "web" {
		t.Fatalf("unexpected web service identity: %+v", web)
	}
	if web.Port != 8080 {
		t.Errorf("web port = %d, want 8080", web.Port)
	}
	if web.Status != "running" {
		t.Errorf("web status = %q, want running", web.Status)
	}
	if web.Image != "draft-real-services-web:1" {
		t.Errorf("web image = %q, want active deployment image", web.Image)
	}
	if web.Hostname == "" || web.HostPort == 0 {
		t.Errorf("expected runtime URL fields, got hostname=%q hostPort=%d", web.Hostname, web.HostPort)
	}

	worker := services[1]
	if worker.ID != "worker-1" {
		t.Fatalf("unexpected worker service: %+v", worker)
	}
	if worker.Type != "worker" {
		t.Errorf("worker type = %q, want worker", worker.Type)
	}
	if worker.Status != "stopped" {
		t.Errorf("worker status = %q, want stopped without active deployment", worker.Status)
	}
}

func TestServiceStatusFromDeployment(t *testing.T) {
	tests := map[string]string{
		"running":     "running",
		"failed":      "failed",
		"error":       "failed",
		"building":    "building",
		"built":       "built",
		"starting":    "starting",
		"pending":     "pending",
		"interrupted": "interrupted",
		"stopped":     "stopped",
		"unknown":     "stopped",
	}
	for input, want := range tests {
		if got := ServiceStatusFromDeployment(input); got != want {
			t.Fatalf("ServiceStatusFromDeployment(%q) = %q, want %q", input, got, want)
		}
	}
}
