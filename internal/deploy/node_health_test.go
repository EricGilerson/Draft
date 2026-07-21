package deploy

import (
	"context"
	"testing"

	"Draft/internal/store"
)

func TestGetNodeHealthLastDeployFailedWhileRunning(t *testing.T) {
	s := openTestStore(t)
	s.DB.Create(&store.Project{Name: "proj", Path: "/proj"})
	s.DB.Create(&store.CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	// Older successful deploy still "running".
	if _, err := s.CreateDeployment(&store.Deployment{
		NodeID:    "n1",
		ProjectID: 1,
		Status:    "running",
		Hostname:  "svc.proj.main.abc.draft.local",
		HostPort:  8080,
	}); err != nil {
		t.Fatal(err)
	}
	// Newer rebuild failed — must surface without hiding the live container.
	failed, err := s.CreateDeployment(&store.Deployment{
		NodeID:    "n1",
		ProjectID: 1,
		Status:    "failed",
		Error:     "build failed: exit 1",
	})
	if err != nil {
		t.Fatal(err)
	}

	e, _ := newTestEngine(t, s)
	health, err := e.GetNodeHealth(context.Background(), "n1")
	if err != nil {
		t.Fatal(err)
	}
	if health.Status != "running" {
		t.Fatalf("Status = %q, want running (previous container still live)", health.Status)
	}
	if !health.LastDeployFailed {
		t.Fatal("expected LastDeployFailed")
	}
	if health.LastDeployStatus != "failed" {
		t.Fatalf("LastDeployStatus = %q, want failed", health.LastDeployStatus)
	}
	if health.LastDeployError != "build failed: exit 1" {
		t.Fatalf("LastDeployError = %q", health.LastDeployError)
	}
	if health.LastDeploymentID != failed.ID {
		t.Fatalf("LastDeploymentID = %d, want %d", health.LastDeploymentID, failed.ID)
	}
	if health.HostPort != 8080 {
		t.Fatalf("HostPort = %d, want 8080 from active deployment", health.HostPort)
	}
}

func TestGetNodeHealthLatestFailedOnly(t *testing.T) {
	s := openTestStore(t)
	s.DB.Create(&store.Project{Name: "proj", Path: "/proj"})
	s.DB.Create(&store.CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})
	if _, err := s.CreateDeployment(&store.Deployment{
		NodeID:    "n1",
		ProjectID: 1,
		Status:    "failed",
		Error:     "missing dockerfile",
	}); err != nil {
		t.Fatal(err)
	}

	e, _ := newTestEngine(t, s)
	health, err := e.GetNodeHealth(context.Background(), "n1")
	if err != nil {
		t.Fatal(err)
	}
	if health.Status != "failed" {
		t.Fatalf("Status = %q, want failed", health.Status)
	}
	if !health.LastDeployFailed {
		t.Fatal("expected LastDeployFailed")
	}
	if health.LastDeployError != "missing dockerfile" {
		t.Fatalf("LastDeployError = %q", health.LastDeployError)
	}
}
