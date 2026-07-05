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

	// api -> db only references db's generated DRAFT_INTERNAL_URL attr, which
	// resolves immediately with no recursion (see resolveNodeAttr's
	// isGeneratedAttr short-circuit). That can never form a real cycle, so db
	// must still be able to offer api as a reference target.
	dbTargets, err := e.ListReferenceTargets(db.ID)
	if err != nil {
		t.Fatal(err)
	}
	foundAPI := false
	for _, target := range dbTargets {
		if target.NodeID == api.ID {
			foundAPI = true
		}
	}
	if !foundAPI {
		t.Fatalf("expected db to still be able to reference api (generated-attr refs can't cycle), targets = %+v", dbTargets)
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

func TestListReferenceTargetsBlocksCustomKeyCycle(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	_, api, db := setupRefTestNodes(t, s)

	// api references one of db's own custom variables (not a generated
	// attr), so that reference does recurse through db's value at resolve
	// time. db must not be offered api as a target, since picking it could
	// close a real cycle.
	if err := s.SetEnvVar(db.ID, "PASSWORD", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvVar(api.ID, "DB_PASSWORD", "@{db.PASSWORD}"); err != nil {
		t.Fatal(err)
	}

	dbTargets, err := e.ListReferenceTargets(db.ID)
	if err != nil {
		t.Fatal(err)
	}
	for _, target := range dbTargets {
		if target.NodeID == api.ID {
			t.Fatalf("api should be excluded from db's reference targets (would cycle through a custom key), targets = %+v", dbTargets)
		}
	}
}
