package deploy

import (
	"fmt"
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
	envID := defaultEnvID(t, s, project.ID)
	api, err = s.CreateNode(&store.CanvasNode{ID: "api", ProjectID: project.ID, EnvironmentID: envID, Label: "api"})
	if err != nil {
		t.Fatal(err)
	}
	db, err = s.CreateNode(&store.CanvasNode{ID: "db", ProjectID: project.ID, EnvironmentID: envID, Label: "db"})
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
		NodeID:        api.ID,
		ProjectID:     project.ID,
		EnvironmentID: api.EnvironmentID,
		ServiceName:   "api",
		ProjectName:   "myapp",
		ServicePort:   "3000",
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
	if !strings.HasPrefix(dbURL, "postgres://db.myapp.main.") || !strings.HasSuffix(dbURL, ":5432/app") {
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
		NodeID:        api.ID,
		ProjectID:     project.ID,
		EnvironmentID: api.EnvironmentID,
		ServiceName:   "api",
		ProjectName:   "myapp",
		ServicePort:   "3000",
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
		NodeID:        api.ID,
		ProjectID:     project.ID,
		EnvironmentID: api.EnvironmentID,
		ServiceName:   "api",
		ProjectName:   "myapp",
		ServicePort:   "3000",
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
		NodeID:        api.ID,
		ProjectID:     project.ID,
		EnvironmentID: api.EnvironmentID,
		ServiceName:   "api",
		ProjectName:   "myapp",
		ServicePort:   "3000",
	})
	if err == nil || !strings.Contains(err.Error(), "no service named") {
		t.Fatalf("expected missing service error, got %v", err)
	}
}

func TestGetProjectConnectionsAndReferenceTargets(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	_, api, db := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(api.ID, "DATABASE_URL", "@{db.DRAFT_INTERNAL_URL}"); err != nil {
		t.Fatal(err)
	}

	conns, err := e.GetEnvironmentConnections(api.EnvironmentID)
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

func TestListReferenceTargetsBlocksCyclicCustomKeysOnly(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	_, api, db := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(db.ID, "PASSWORD", "hunter2"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvVar(api.ID, "DB_PASSWORD", "@{db.PASSWORD}"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvVar(api.ID, "PUBLIC_URL", "https://example.com"); err != nil {
		t.Fatal(err)
	}

	dbTargets, err := e.ListReferenceTargets(db.ID)
	if err != nil {
		t.Fatal(err)
	}
	var apiTarget *ReferenceTarget
	for i := range dbTargets {
		if dbTargets[i].NodeID == api.ID {
			apiTarget = &dbTargets[i]
			break
		}
	}
	if apiTarget == nil {
		t.Fatalf("expected api to remain a reference target (address attrs are always safe), targets = %+v", dbTargets)
	}
	if len(apiTarget.Attributes) != len(generatedAttrs) {
		t.Fatalf("expected generated address attrs on api, got %+v", apiTarget.Attributes)
	}
	if containsString(apiTarget.CustomKeys, "DB_PASSWORD") {
		t.Fatalf("DB_PASSWORD should be excluded (resolves back into db), customKeys = %+v", apiTarget.CustomKeys)
	}
	if !containsString(apiTarget.CustomKeys, "PUBLIC_URL") {
		t.Fatalf("PUBLIC_URL should remain available, customKeys = %+v", apiTarget.CustomKeys)
	}
}

func TestListReferenceTargetsBlocksMutualCustomKeyCycle(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	_, api, db := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(api.ID, "A", "@{db.B}"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvVar(db.ID, "B", "@{api.A}"); err != nil {
		t.Fatal(err)
	}

	dbTargets, err := e.ListReferenceTargets(db.ID)
	if err != nil {
		t.Fatal(err)
	}
	var apiTarget *ReferenceTarget
	for i := range dbTargets {
		if dbTargets[i].NodeID == api.ID {
			apiTarget = &dbTargets[i]
			break
		}
	}
	if apiTarget == nil {
		t.Fatal("expected api to remain a reference target for address attrs")
	}
	if containsString(apiTarget.CustomKeys, "A") {
		t.Fatalf("api.A should be excluded from db's picker (mutual cycle), customKeys = %+v", apiTarget.CustomKeys)
	}
}

// TestReferenceResolutionIsScopedPerEnvironment builds two environments in
// the same project, each with their own "api" and "db" nodes, and confirms
// api's @{db.ATTR} reference always resolves to its own environment's db —
// never crosses into the other environment's db, even though both nodes
// share the label "db".
func TestReferenceResolutionIsScopedPerEnvironment(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	project, err := s.CreateProject("myapp", "/tmp/myapp", "")
	if err != nil {
		t.Fatal(err)
	}
	mainEnv := defaultEnvID(t, s, project.ID)
	stagingEnv, err := s.CreateEnvironment(project.ID, "Staging")
	if err != nil {
		t.Fatal(err)
	}

	mainAPI, err := s.CreateNode(&store.CanvasNode{ID: "main-api", ProjectID: project.ID, EnvironmentID: mainEnv, Label: "api"})
	if err != nil {
		t.Fatal(err)
	}
	mainDB, err := s.CreateNode(&store.CanvasNode{ID: "main-db", ProjectID: project.ID, EnvironmentID: mainEnv, Label: "db"})
	if err != nil {
		t.Fatal(err)
	}
	stagingAPI, err := s.CreateNode(&store.CanvasNode{ID: "staging-api", ProjectID: project.ID, EnvironmentID: stagingEnv.ID, Label: "api"})
	if err != nil {
		t.Fatal(err)
	}
	stagingDB, err := s.CreateNode(&store.CanvasNode{ID: "staging-db", ProjectID: project.ID, EnvironmentID: stagingEnv.ID, Label: "db"})
	if err != nil {
		t.Fatal(err)
	}
	for _, n := range []*store.CanvasNode{mainDB, stagingDB} {
		if err := s.SetNodeSetting(n.ID, "service_port", "5432"); err != nil {
			t.Fatal(err)
		}
	}
	for _, n := range []*store.CanvasNode{mainAPI, stagingAPI} {
		if err := s.UpsertEnvVar(store.EnvVar{
			NodeID: n.ID, Key: "DATABASE_URL", Value: "@{db.DRAFT_INTERNAL_HOSTNAME}",
			Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
		}); err != nil {
			t.Fatal(err)
		}
	}

	mainResolved, err := e.PreviewEnvVars(mainAPI.ID)
	if err != nil {
		t.Fatal(err)
	}
	if mainResolved["DATABASE_URL"].Error != "" {
		t.Fatalf("main DATABASE_URL failed to resolve: %s", mainResolved["DATABASE_URL"].Error)
	}
	if !strings.Contains(mainResolved["DATABASE_URL"].Value, mainDB.UID) {
		t.Errorf("main api's DATABASE_URL should resolve to main db (%s), got %q", mainDB.UID, mainResolved["DATABASE_URL"].Value)
	}
	if strings.Contains(mainResolved["DATABASE_URL"].Value, stagingDB.UID) {
		t.Errorf("main api's DATABASE_URL leaked staging db's UID: %q", mainResolved["DATABASE_URL"].Value)
	}

	stagingResolved, err := e.PreviewEnvVars(stagingAPI.ID)
	if err != nil {
		t.Fatal(err)
	}
	if stagingResolved["DATABASE_URL"].Error != "" {
		t.Fatalf("staging DATABASE_URL failed to resolve: %s", stagingResolved["DATABASE_URL"].Error)
	}
	if !strings.Contains(stagingResolved["DATABASE_URL"].Value, stagingDB.UID) {
		t.Errorf("staging api's DATABASE_URL should resolve to staging db (%s), got %q", stagingDB.UID, stagingResolved["DATABASE_URL"].Value)
	}
	if strings.Contains(stagingResolved["DATABASE_URL"].Value, mainDB.UID) {
		t.Errorf("staging api's DATABASE_URL leaked main db's UID: %q", stagingResolved["DATABASE_URL"].Value)
	}
}

func TestRewriteAddressStringsLongestFirst(t *testing.T) {
	root := NodeAddress{
		InternalHostname: "db.myapp.main.aaaa.draft.local",
		InternalURL:      "http://db.myapp.main.aaaa.draft.local:5432",
		PublicHostname:   "db.myapp.main.aaaa.draft.resolv.sh",
		PublicURL:        "http://db.myapp.main.aaaa.draft.resolv.sh:8080",
	}
	alias := NodeAddress{
		InternalHostname: "db.myapp.staging.bbbb.draft.local",
		InternalURL:      "http://db.myapp.staging.bbbb.draft.local:5432",
		PublicHostname:   "db.myapp.staging.bbbb.draft.resolv.sh",
		PublicURL:        "http://db.myapp.staging.bbbb.draft.resolv.sh:8080",
	}
	in := "postgres://u:p@db.myapp.main.aaaa.draft.local:5432/app also " + root.InternalURL
	got := rewriteAddressStrings(in, root, alias)
	if strings.Contains(got, "main.aaaa") {
		t.Fatalf("root hostname left in place: %q", got)
	}
	if !strings.Contains(got, "db.myapp.staging.bbbb.draft.local") {
		t.Fatalf("alias hostname missing: %q", got)
	}
	if !strings.Contains(got, alias.InternalURL) {
		t.Fatalf("alias internal URL missing: %q", got)
	}
}

func TestLinkedServiceDATABASE_URLRewritesHostname(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	mainEnv := defaultEnvID(t, s, p.ID)

	root, err := s.CreateNode(&store.CanvasNode{
		ID: "root-db", ProjectID: p.ID, EnvironmentID: mainEnv, Label: "db",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeSetting(root.ID, "service_port", "5432"); err != nil {
		t.Fatal(err)
	}
	rootAddr, err := e.computeNodeAddress(root)
	if err != nil {
		t.Fatal(err)
	}
	const password = "shared-secret"
	rootURL := fmt.Sprintf("postgres://postgres:%s@%s:5432/postgres", password, rootAddr.InternalHostname)
	if err := s.SetEnvVar(root.ID, "POSTGRES_PASSWORD", password); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvVar(root.ID, "DATABASE_URL", rootURL); err != nil {
		t.Fatal(err)
	}

	staging, err := s.CreateEnvironment(p.ID, "Staging")
	if err != nil {
		t.Fatal(err)
	}
	alias, err := s.CreateNode(&store.CanvasNode{
		ID: "alias-db", ProjectID: p.ID, EnvironmentID: staging.ID, Label: "db",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetNodeSetting(alias.ID, "service_port", "5432"); err != nil {
		t.Fatal(err)
	}
	// Alias may still hold a copied root-stamped URL (share/duplicate path).
	if err := s.SetEnvVar(alias.ID, "DATABASE_URL", rootURL); err != nil {
		t.Fatal(err)
	}
	if err := e.SetServiceLink(alias.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	aliasAddr, err := e.computeNodeAddress(alias)
	if err != nil {
		t.Fatal(err)
	}

	// Staging consumer references the linked service's DATABASE_URL blob.
	worker, err := s.CreateNode(&store.CanvasNode{
		ID: "worker", ProjectID: p.ID, EnvironmentID: staging.ID, Label: "worker",
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvVar(worker.ID, "DATABASE_URL", "@{db.DATABASE_URL}"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvVar(worker.ID, "DB_PASSWORD", "@{db.POSTGRES_PASSWORD}"); err != nil {
		t.Fatal(err)
	}

	deployEnv, err := e.resolveDeploymentEnv(deploymentEnvInput{
		NodeID:        worker.ID,
		ProjectID:     p.ID,
		EnvironmentID: staging.ID,
		ServiceName:   "worker",
		ProjectName:   p.Name,
		ServicePort:   "80",
	})
	if err != nil {
		t.Fatal(err)
	}
	got := map[string]string{}
	for _, item := range deployEnv.RuntimeEnv {
		if k, v, ok := splitEnv(item); ok {
			got[k] = v
		}
	}
	if got["DB_PASSWORD"] != password {
		t.Errorf("password = %q, want %q", got["DB_PASSWORD"], password)
	}
	dbURL := got["DATABASE_URL"]
	if !strings.Contains(dbURL, password) {
		t.Errorf("DATABASE_URL should keep root password: %q", dbURL)
	}
	if strings.Contains(dbURL, rootAddr.InternalHostname) {
		t.Errorf("DATABASE_URL still has root hostname: %q", dbURL)
	}
	if !strings.Contains(dbURL, aliasAddr.InternalHostname) {
		t.Errorf("DATABASE_URL missing alias hostname %q: %q", aliasAddr.InternalHostname, dbURL)
	}

	// Alias Variables preview should also show alias DNS, not root.
	preview, err := e.PreviewEnvVars(alias.ID)
	if err != nil {
		t.Fatal(err)
	}
	prev := preview["DATABASE_URL"].Value
	if strings.Contains(prev, rootAddr.InternalHostname) {
		t.Errorf("alias preview still has root hostname: %q", prev)
	}
	if !strings.Contains(prev, aliasAddr.InternalHostname) {
		t.Errorf("alias preview missing alias hostname: %q", prev)
	}
}

func containsString(list []string, want string) bool {
	for _, s := range list {
		if s == want {
			return true
		}
	}
	return false
}
