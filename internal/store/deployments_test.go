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

func TestReplaceDeploymentInputsStoresDigestsOnly(t *testing.T) {
	s := openTemp(t)
	dep, err := s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "running"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDeploymentInputs(dep.ID, []DeploymentInput{{Key: "TOKEN", Scope: "runtime", Digest: "digest-one"}}); err != nil {
		t.Fatal(err)
	}
	if err := s.ReplaceDeploymentInputs(dep.ID, []DeploymentInput{{Key: "TOKEN", Scope: "runtime", Digest: "digest-two"}, {Key: "ARG", Scope: "build", Digest: "digest-three"}}); err != nil {
		t.Fatal(err)
	}
	inputs, err := s.ListDeploymentInputs(dep.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(inputs) != 2 || inputs[0].Digest != "digest-three" || inputs[1].Digest != "digest-two" {
		t.Fatalf("unexpected inputs: %+v", inputs)
	}
}

func TestUpdateNodeRenamesReferencesInAppliedAndStagedEnv(t *testing.T) {
	s := openTemp(t)
	p, err := s.CreateProject("p", "/p", "")
	if err != nil {
		t.Fatal(err)
	}
	envs, err := s.ListEnvironments(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&CanvasNode{ID: "db", ProjectID: p.ID, EnvironmentID: envs[0].ID, Label: "db"}); err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&CanvasNode{ID: "api", ProjectID: p.ID, EnvironmentID: envs[0].ID, Label: "api"}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(EnvVar{NodeID: "api", Key: "URL", Value: "@{db.DATABASE_URL}"}); err != nil {
		t.Fatal(err)
	}
	if err := s.StageEnvVarChanges("api", []EnvVarStageUpsert{{Key: "NEXT_URL", Value: "@{db.DRAFT_INTERNAL_URL}"}}, nil); err != nil {
		t.Fatal(err)
	}
	if err := s.UpdateNode("db", 1, 2, "primary-db"); err != nil {
		t.Fatal(err)
	}
	vars, err := s.ListEnvVars("api")
	if err != nil || vars[0].Value != "@{primary-db.DATABASE_URL}" {
		t.Fatalf("applied vars: %+v err=%v", vars, err)
	}
	staged, err := s.ListStagedEnvVarChanges("api")
	if err != nil || staged[0].Value != "@{primary-db.DRAFT_INTERNAL_URL}" {
		t.Fatalf("staged vars: %+v err=%v", staged, err)
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
	s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "interrupted"})

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

func TestListDeploymentsPage(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})
	for i := 0; i < 5; i++ {
		if _, err := s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "stopped"}); err != nil {
			t.Fatal(err)
		}
	}
	page1, err := s.ListDeploymentsPage("n1", 2, 0)
	if err != nil {
		t.Fatal(err)
	}
	if len(page1) != 2 {
		t.Fatalf("page1 len = %d, want 2", len(page1))
	}
	page2, err := s.ListDeploymentsPage("n1", 2, 2)
	if err != nil {
		t.Fatal(err)
	}
	if len(page2) != 2 {
		t.Fatalf("page2 len = %d, want 2", len(page2))
	}
	if page1[0].ID == page2[0].ID {
		t.Fatal("expected distinct pages")
	}
	total, err := s.CountDeployments("n1")
	if err != nil || total != 5 {
		t.Fatalf("total = %d err=%v, want 5", total, err)
	}
}

func TestCreateDeploymentAssignsPerNodeSequence(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})
	s.DB.Create(&CanvasNode{ID: "n2", ProjectID: 1, Label: "other"})

	// Interleave deploys across two services: the per-node sequence must track
	// only this service's deploys, not the app-wide deployment primary key.
	a1, _ := s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "building"})
	b1, _ := s.CreateDeployment(&Deployment{NodeID: "n2", ProjectID: 1, Status: "building"})
	b2, _ := s.CreateDeployment(&Deployment{NodeID: "n2", ProjectID: 1, Status: "running"})
	a2, _ := s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "running"})

	if a1.Sequence != 1 || a2.Sequence != 2 {
		t.Errorf("n1 sequence = %d, %d; want 1, 2", a1.Sequence, a2.Sequence)
	}
	if b1.Sequence != 1 || b2.Sequence != 2 {
		t.Errorf("n2 sequence = %d, %d; want 1, 2", b1.Sequence, b2.Sequence)
	}
	// Global IDs are still interleaved/app-wide; only the sequence is per-node.
	if a2.ID <= b2.ID {
		t.Errorf("expected app-wide ID to keep growing globally; a2.ID=%d b2.ID=%d", a2.ID, b2.ID)
	}
}

func TestCreateDeploymentPreservesExplicitSequence(t *testing.T) {
	s := openTemp(t)
	s.DB.Create(&Project{Name: "p1", Path: "/p1"})
	s.DB.Create(&CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	dep, err := s.CreateDeployment(&Deployment{NodeID: "n1", ProjectID: 1, Status: "building", Sequence: 42})
	if err != nil {
		t.Fatal(err)
	}
	if dep.Sequence != 42 {
		t.Errorf("expected explicit sequence preserved, got %d", dep.Sequence)
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
