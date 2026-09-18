package deploy

import (
	"context"
	"path/filepath"
	"testing"

	"Draft/internal/store"
)

func TestNodeLooksDeployableStagedBlankService(t *testing.T) {
	s := openTestStore(t)
	p, err := s.CreateProject("p", filepath.Join(t.TempDir(), "p"), "")
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CreateNode(&store.CanvasNode{
		ID: "blank-1", ProjectID: p.ID, EnvironmentID: defaultEnvID(t, s, p.ID), Label: "blank",
	})
	if err != nil {
		t.Fatal(err)
	}

	applied, err := s.GetNodeSettings(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if nodeLooksDeployable(applied) {
		t.Fatal("blank node applied settings should not look deployable")
	}

	if err := s.StageNodeSettings(n.ID, map[string]string{
		"dockerfile":   "Dockerfile",
		"service_port": "3000",
	}); err != nil {
		t.Fatal(err)
	}

	e, _ := newTestEngine(t, s)
	if !nodeLooksDeployable(e.effectiveSettingsOrEmpty(n.ID)) {
		t.Fatal("staged dockerfile+port should look deployable")
	}
	appliedAfter, err := s.GetNodeSettings(n.ID)
	if err != nil {
		t.Fatal(err)
	}
	if nodeLooksDeployable(appliedAfter) {
		t.Fatal("applied settings must stay empty until promote")
	}
}

func TestNodeLooksDeployableImageMode(t *testing.T) {
	if nodeLooksDeployable(map[string]string{"image": "redis:7", "service_port": "6379"}) {
		// ok
	} else {
		t.Fatal("image+port should be deployable")
	}
	if nodeLooksDeployable(map[string]string{"dockerfile": "Dockerfile"}) {
		t.Fatal("dockerfile without port should not be deployable")
	}
	if nodeLooksDeployable(map[string]string{"service_port": "3000"}) {
		t.Fatal("port without image/dockerfile should not be deployable")
	}
}

func TestTryResumeStoppedSkipsWhenStaged(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	p, err := s.CreateProject("p", filepath.Join(t.TempDir(), "p"), "")
	if err != nil {
		t.Fatal(err)
	}
	n, err := s.CreateNode(&store.CanvasNode{
		ID: "svc-resume", ProjectID: p.ID, EnvironmentID: defaultEnvID(t, s, p.ID), Label: "app",
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDeployment(&store.Deployment{
		NodeID: n.ID, ProjectID: p.ID, Status: "stopped", ContainerID: "missing",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.StageNodeSettings(n.ID, map[string]string{"service_port": "8080"}); err != nil {
		t.Fatal(err)
	}

	if e.tryResumeStopped(context.Background(), n.ID, map[string]string{}) {
		t.Fatal("staged changes must skip resume so the next deploy rebuilds")
	}
}
