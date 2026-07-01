package deploy

import (
	"strings"
	"testing"

	"Draft/internal/store"
)

func setupRefTestNodes(t *testing.T, s *store.Store) (project *store.Project, api, db *store.CanvasNode) {
	t.Helper()
	project, err := s.CreateProject("myapp", "/tmp/myapp", "")
	if err != nil {
		t.Fatal(err)
	}
	api, err = s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, Label: "api"})
	if err != nil {
		t.Fatal(err)
	}
	db, err = s.CreateNode(&store.CanvasNode{ID: "db", ProjectID: project.ID, Label: "db"})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeSetting(db.ID, "service_port", "5432"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeSetting(api.ID, "service_port", "3000"); err != nil {
		t.Fatal(err)
	}
	return project, api, db
}

func TestResolveValueGeneratedAttribute(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	project, api, _ := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(api.ID, "DATABASE_URL", "postgres://@{db.DRAFT_INTERNAL_HOSTNAME}:@{db.DRAFT_SERVICE_PORT}/app"); err != nil {
		t.Fatal(err)
	}

	deployEnv, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:      api.ID,
		ProjectID:   project.ID,
		ServiceName: "api",
		ProjectName: "myapp",
		ServicePort: "3000",
	})
	if err != nil {
		t.Fatal(err)
	}

	var dbURL string
	for _, item := range deployEnv.RuntimeEnv {
		if k, v, ok := splitEnv(item); ok && k == "DATABASE_URL" {
			dbURL = v
		}
	}
	if !strings.HasPrefix(dbURL, "postgres://db.myapp.default.") || !strings.HasSuffix(dbURL, ":5432/app") {
		t.Fatalf("DATABASE_URL = %q", dbURL)
	}
}

func TestResolveValueCustomVarReference(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	project, api, db := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(db.ID, "PASSWORD", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvVar(api.ID, "DB_PASSWORD", "@{db.PASSWORD}"); err != nil {
		t.Fatal(err)
	}

	deployEnv, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:      api.ID,
		ProjectID:   project.ID,
		ServiceName: "api",
		ProjectName: "myapp",
		ServicePort: "3000",
	})
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, item := range deployEnv.RuntimeEnv {
		if k, v, ok := splitEnv(item); ok && k == "DB_PASSWORD" {
			found = true
			if v != "hunter2" {
				t.Fatalf("DB_PASSWORD = %q", v)
			}
		}
	}
	if !found {
		t.Fatal("DB_PASSWORD not present in resolved env")
	}
}

func TestResolveValueDetectsCycle(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	project, api, db := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(api.ID, "A", "@{db.B}"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvVar(db.ID, "B", "@{api.A}"); err != nil {
		t.Fatal(err)
	}

	_, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:      api.ID,
		ProjectID:   project.ID,
		ServiceName: "api",
		ProjectName: "myapp",
		ServicePort: "3000",
	})
	if err == nil || !strings.Contains(err.Error(), "circular") {
		t.Fatalf("expected circular reference error, got %v", err)
	}
}

func TestResolveValueMissingReference(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	project, api, _ := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(api.ID, "MISSING", "@{ghost.DRAFT_INTERNAL_URL}"); err != nil {
		t.Fatal(err)
	}

	_, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:      api.ID,
		ProjectID:   project.ID,
		ServiceName: "api",
		ProjectName: "myapp",
		ServicePort: "3000",
	})
	if err == nil || !strings.Contains(err.Error(), "no service named") {
		t.Fatalf("expected missing service error, got %v", err)
	}
}

func TestGetProjectConnectionsAndReferenceTargets(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	project, api, db := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(api.ID, "DATABASE_URL", "@{db.DRAFT_INTERNAL_URL}"); err != nil {
		t.Fatal(err)
	}

	conns, err := e.GetProjectConnections(project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(conns) != 1 || conns[0].SourceNodeID != api.ID || conns[0].TargetNodeID != db.ID || conns[0].TargetAttr != "DRAFT_INTERNAL_URL" {
		t.Fatalf("connections = %+v", conns)
	}

	// api already references db (api -> db), so db must NOT offer api as a
	// target: db -> api would close the loop back through the existing
	// api -> db reference.
	dbTargets, err := e.ListReferenceTargets(db.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range dbTargets {
		if target.NodeID == api.ID {
			t.Fatalf("api should be excluded from db's reference targets (would cycle), targets = %+v", dbTargets)
		}
	}

	// Nothing references api yet, so api can still (redundantly or not)
	// pick db as a target — reusing an existing reference isn't a cycle.
	apiTargets, err := e.ListReferenceTargets(api.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundDB := false
	for _, target := range apiTargets {
		if target.NodeID == db.ID {
			foundDB = true
		}
	}
	if !foundDB {
		t.Fatalf("expected api to still be able to reference db, targets = %+v", apiTargets)
	}
}
