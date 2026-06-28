package store

import (
	"testing"
	"time"
)

func TestCreateAndGetDeployment(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	dep := &Deployment{NodeID: "n1", ProjectID: 1, Status: "building", ImageTag: "img:1"}
	created, err := s.CreateDeployment(dep)
	if err != nil {
		t.Fatal(err)
	}
	if created.ID == 0 {
		t.Fatal("expected non-zero ID")
	}

	got, err := s.GetDeployment(created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got.NodeID != "n1" || got.Status != "building" || got.ImageTag != "img:1" {
		t.Errorf("unexpected deployment: %+v", got)
	}
}

func TestUpdateDeployment(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	dep, _ := s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "building"})
	dep.Status = "running"
	dep.ContainerID = "abc123"
	if err := s.UpdateDeployment(dep); err != nil {
		t.Fatal(err)
	}

	got, _ := s.GetDeployment(dep.ID)
	if got.Status != "running" || got.ContainerID != "abc123" {
		t.Errorf("update not persisted: %+v", got)
	}
}

func TestActiveDeployment(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	// No deployments → nil
	got, err := s.ActiveDeployment("n1")
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatal("expected nil for no deployments")
	}

	// One running deployment
	s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "running"})
	got, err = s.ActiveDeployment("n1")
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Status != "running" {
		t.Fatal("expected running deployment")
	}
}

func TestActiveDeploymentSkipsStoppedAndFailed(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "stopped"})
	s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "failed"})

	got, _ := s.ActiveDeployment("n1")
	if got != nil {
		t.Fatalf("expected nil, got %+v", got)
	}
}

func TestActiveDeploymentReturnsLatest(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "building", ImageTag: "old"})
	time.Sleep(10 * time.Millisecond)
	s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "running", ImageTag: "new"})

	got, _ := s.ActiveDeployment("n1")
	if got == nil || got.ImageTag != "new" {
		t.Fatalf("expected newest active, got %+v", got)
	}
}

func TestListDeployments(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})
	s.DB.Create(&CanvasNode{ID: "n2", ProjectID: 1, Label: "other"})

	s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "stopped"})
	s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "running"})
	s.CreateDeployment(&Deployment{NodeID: "n2", ProjectID: 1, Status: "running"})

	list, err := s.ListDeployments("n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2 deployments for n1, got %d", len(list))
	}
	// Should be newest first
	if list[0].Status != "running" {
		t.Errorf("expected running first, got %s", list[0].Status)
	}
}

func TestListDeploymentsEmpty(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListDeployments("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 0 {
		t.Fatalf("expected 0, got %d", len(list))
	}
}

func TestDeploymentFinishedAt(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	dep, _ := s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "building"})
	if dep.FinishedAt != nil {
		t.Fatal("expected nil FinishedAt initially")
	}

	now := time.Now()
	dep.FinishedAt = &now
	dep.Status = "built"
	s.UpdateDeployment(dep)

	got, _ := s.GetDeployment(dep.ID)
	if got.FinishedAt == nil {
		t.Fatal("expected FinishedAt to be set")
	}
}

func TestGetDeploymentNotFound(t *testing.T) {
	s := openTemp(t)
	_, err := s.GetDeployment(999)
	if err == nil {
		t.Fatal("expected error for missing deployment")
	}
}
