package store

import "testing"

func TestCreateProjectCreatesExactlyOneMainEnvironment(t *testing.T) {
	s := openTemp(t)

	project, err := s.CreateProject("p", "/tmp/p", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	envs, err := s.ListEnvironments(project.ID)
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("expected exactly one environment, got %d", len(envs))
	}
	if envs[0].Name != "Main" || envs[0].Slug != "main" || !envs[0].IsDefault {
		t.Errorf("unexpected default environment: %+v", envs[0])
	}

	def, err := s.GetDefaultEnvironment(project.ID)
	if err != nil {
		t.Fatalf("GetDefaultEnvironment: %v", err)
	}
	if def.ID != envs[0].ID {
		t.Errorf("GetDefaultEnvironment = %+v, want %+v", def, envs[0])
	}
}

func TestCreateEnvironmentGeneratesUniqueSlug(t *testing.T) {
	s := openTemp(t)
	project, err := s.CreateProject("p", "/tmp/p", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}

	staging, err := s.CreateEnvironment(project.ID, "Staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if staging.Slug != "staging" || staging.IsDefault {
		t.Errorf("unexpected environment: %+v", staging)
	}

	// A second environment with a colliding slug gets a numeric suffix.
	staging2, err := s.CreateEnvironment(project.ID, "staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	if staging2.Slug != "staging-2" {
		t.Errorf("Slug = %q, want %q", staging2.Slug, "staging-2")
	}
}

func TestDeleteEnvironmentBlocksDefaultAndOnly(t *testing.T) {
	s := openTemp(t)
	project, err := s.CreateProject("p", "/tmp/p", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	def, _ := s.GetDefaultEnvironment(project.ID)

	if err := s.DeleteEnvironment(def.ID); err != ErrCannotDeleteDefaultEnvironment {
		t.Errorf("got %v, want ErrCannotDeleteDefaultEnvironment", err)
	}

	staging, err := s.CreateEnvironment(project.ID, "Staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	// Now there are two environments, so deleting the non-default one succeeds.
	if err := s.DeleteEnvironment(staging.ID); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	envs, err := s.ListEnvironments(project.ID)
	if err != nil {
		t.Fatalf("ListEnvironments: %v", err)
	}
	if len(envs) != 1 {
		t.Fatalf("expected 1 environment remaining, got %d", len(envs))
	}
}

func TestDeleteEnvironmentCascadesNodes(t *testing.T) {
	s := openTemp(t)
	project, err := s.CreateProject("p", "/tmp/p", "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	staging, err := s.CreateEnvironment(project.ID, "Staging")
	if err != nil {
		t.Fatalf("CreateEnvironment: %v", err)
	}
	node, err := s.CreateNode(&CanvasNode{ID: "n1", ProjectID: project.ID, EnvironmentID: staging.ID, Label: "api"})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := s.SetNodeSetting(node.ID, "service_port", "3000"); err != nil {
		t.Fatalf("SetNodeSetting: %v", err)
	}

	if err := s.DeleteEnvironment(staging.ID); err != nil {
		t.Fatalf("DeleteEnvironment: %v", err)
	}
	if _, err := s.GetNode(node.ID); err == nil {
		t.Error("expected node to be deleted along with its environment")
	}
	settings, err := s.GetNodeSettings(node.ID)
	if err != nil {
		t.Fatalf("GetNodeSettings: %v", err)
	}
	if len(settings) != 0 {
		t.Errorf("expected node settings to be cleared, got %+v", settings)
	}
}
