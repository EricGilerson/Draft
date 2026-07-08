package deploy

import (
	"strings"
	"testing"

	"Draft/internal/store"
)

func TestDuplicateEnvironmentClonesNodesWithFreshUIDsAndRefs(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	tpl := findBuiltin(t, s, "PostgreSQL")
	mainEnv := defaultEnvID(t, s, p.ID)

	dbRes, err := e.CreateNodeFromTemplate(CreateNodeFromTemplateRequest{
		ID:            "db1",
		Label:         "db",
		ProjectID:     p.ID,
		EnvironmentID: mainEnv,
		TemplateID:    tpl.ID,
	})
	if err != nil {
		t.Fatalf("CreateNodeFromTemplate: %v", err)
	}
	apiNode, err := s.CreateNode(&store.CanvasNode{ID: "api1", ProjectID: p.ID, EnvironmentID: mainEnv, Label: "api"})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: apiNode.ID, Key: "DATABASE_URL", Value: "@{db.DRAFT_INTERNAL_URL}",
		Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
	}); err != nil {
		t.Fatalf("set DATABASE_URL: %v", err)
	}

	sourcePassword := ""
	for _, v := range mustListEnvVars(t, s, dbRes.Node.ID) {
		if v.Key == "POSTGRES_PASSWORD" {
			sourcePassword = v.Value
		}
		if strings.Contains(v.Value, "{{draft.") {
			t.Fatalf("template env vars should already be resolved at stamp time, got %q = %q", v.Key, v.Value)
		}
	}
	if sourcePassword == "" {
		t.Fatal("source POSTGRES_PASSWORD not seeded")
	}

	newEnv, err := e.DuplicateEnvironment(mainEnv, "Staging")
	if err != nil {
		t.Fatalf("DuplicateEnvironment: %v", err)
	}
	if newEnv.IsDefault {
		t.Fatal("duplicated environment must not be default")
	}

	newNodes, err := s.ListNodesByEnvironment(newEnv.ID)
	if err != nil {
		t.Fatalf("ListNodesByEnvironment: %v", err)
	}
	if len(newNodes) != 2 {
		t.Fatalf("expected 2 duplicated nodes, got %d", len(newNodes))
	}

	var newDB, newAPI *store.CanvasNode
	for i := range newNodes {
		switch newNodes[i].Label {
		case "db":
			newDB = &newNodes[i]
		case "api":
			newAPI = &newNodes[i]
		}
	}
	if newDB == nil || newAPI == nil {
		t.Fatalf("expected db and api nodes duplicated, got %+v", newNodes)
	}
	if newDB.ID == dbRes.Node.ID || newAPI.ID == apiNode.ID {
		t.Fatal("duplicated nodes must have fresh IDs")
	}
	if newDB.UID == "" || newDB.UID == dbRes.Node.UID {
		t.Errorf("expected fresh UID on duplicated db node, got %q (source %q)", newDB.UID, dbRes.Node.UID)
	}

	// Template-resolved values (e.g. the generated password) are already
	// concrete data by the time they're stamped onto the source node, so
	// duplication carries them over as-is — same as any other env var. What
	// matters is that nothing is left as a stale, unresolved {{draft.*}}
	// literal after the copy.
	newPassword := ""
	for _, v := range mustListEnvVars(t, s, newDB.ID) {
		if v.Key == "POSTGRES_PASSWORD" {
			newPassword = v.Value
		}
		if strings.Contains(v.Value, "{{draft.") {
			t.Errorf("duplicated env var %q still has unresolved draft expression: %q", v.Key, v.Value)
		}
	}
	if newPassword != sourcePassword {
		t.Errorf("expected duplicated POSTGRES_PASSWORD to be copied as-is, got %q, want %q", newPassword, sourcePassword)
	}

	// @{Label.ATTR} references must resolve within the new environment only
	// (to the duplicated db, not the source db).
	newAPIVars, err := s.ListEnvVars(newAPI.ID)
	if err != nil {
		t.Fatalf("ListEnvVars: %v", err)
	}
	found := false
	for _, v := range newAPIVars {
		if v.Key == "DATABASE_URL" {
			found = true
			if v.Value != "@{db.DRAFT_INTERNAL_URL}" {
				t.Errorf("reference token should be preserved literally, got %q", v.Value)
			}
		}
	}
	if !found {
		t.Fatal("duplicated api node missing DATABASE_URL")
	}
	resolved, err := e.PreviewEnvVars(newAPI.ID)
	if err != nil {
		t.Fatalf("PreviewEnvVars: %v", err)
	}
	if resolved["DATABASE_URL"].Error != "" {
		t.Fatalf("DATABASE_URL failed to resolve: %s", resolved["DATABASE_URL"].Error)
	}
	if !strings.Contains(resolved["DATABASE_URL"].Value, newDB.UID) {
		t.Errorf("DATABASE_URL should resolve to the duplicated db's own hostname, got %q", resolved["DATABASE_URL"].Value)
	}

	// No deployments, routes, or port leases are carried over.
	deps, err := s.ListDeployments(newDB.ID)
	if err != nil {
		t.Fatalf("ListDeployments: %v", err)
	}
	if len(deps) != 0 {
		t.Errorf("expected no deployments for duplicated node, got %d", len(deps))
	}
}

func mustListEnvVars(t *testing.T, s *store.Store, nodeID string) []store.EnvVar {
	t.Helper()
	vars, err := s.ListEnvVars(nodeID)
	if err != nil {
		t.Fatalf("ListEnvVars: %v", err)
	}
	return vars
}
