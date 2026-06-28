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
