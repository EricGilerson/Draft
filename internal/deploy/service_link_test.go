package deploy

import (
	"strings"
	"testing"

	"Draft/internal/store"
)

func TestParseServiceLink(t *testing.T) {
	if ParseServiceLink("") != nil {
		t.Fatal("empty should be nil")
	}
	if ParseServiceLink("not-json") != nil {
		t.Fatal("bad json should be nil")
	}
	link := ParseServiceLink(`{"rootNodeId":"n1","rootEnvironmentId":2}`)
	if link == nil || link.RootNodeID != "n1" || link.RootEnvironmentID != 2 {
		t.Fatalf("got %+v", link)
	}
}

func TestSetServiceLinkRejectsChains(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	env := defaultEnvID(t, s, p.ID)

	root, err := s.CreateNode(&store.CanvasNode{ID: "root1", ProjectID: p.ID, EnvironmentID: env, Label: "db"})
	if err != nil {
		t.Fatal(err)
	}
	alias, err := s.CreateNode(&store.CanvasNode{ID: "alias1", ProjectID: p.ID, EnvironmentID: env, Label: "db-alias"})
	if err != nil {
		t.Fatal(err)
	}
	mid, err := s.CreateNode(&store.CanvasNode{ID: "mid1", ProjectID: p.ID, EnvironmentID: env, Label: "db-mid"})
	if err != nil {
		t.Fatal(err)
	}

	if err := e.SetServiceLink(alias.ID, root.ID); err != nil {
		t.Fatalf("link alias→root: %v", err)
	}
	// mid tries to link to alias (chain) — reject
	if err := e.SetServiceLink(mid.ID, alias.ID); err == nil {
		t.Fatal("expected chain link to be rejected")
	} else if !strings.Contains(err.Error(), "already a linked") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestDuplicateEnvironmentShareCreatesLink(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	tpl := findBuiltin(t, s, "PostgreSQL")
	mainEnv := defaultEnvID(t, s, p.ID)

	dbRes, err := e.CreateNodeFromTemplate(CreateNodeFromTemplateRequest{
		ID: "db1", Label: "db", ProjectID: p.ID, EnvironmentID: mainEnv, TemplateID: tpl.ID,
	})
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}

	newEnv, err := e.DuplicateEnvironment(mainEnv, "Staging", ServiceDataChoice{
		SourceNodeID: dbRes.Node.ID,
		Mode:         ServiceDataShare,
	})
	if err != nil {
		t.Fatalf("DuplicateEnvironment: %v", err)
	}

	nodes, err := s.ListNodesByEnvironment(newEnv.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(nodes) != 1 {
		t.Fatalf("expected 1 node, got %d", len(nodes))
	}
	link, err := e.GetServiceLink(nodes[0].ID)
	if err != nil || link == nil {
		t.Fatalf("expected service_link, got %v err=%v", link, err)
	}
	if link.RootNodeID != dbRes.Node.ID {
		t.Errorf("root = %q, want %q", link.RootNodeID, dbRes.Node.ID)
	}
	settings, _ := s.GetNodeSettings(nodes[0].ID)
	if paths := managedVolumePaths(settings); len(paths) != 0 {
		t.Errorf("alias should not own volumes, got %v", paths)
	}
}

func TestGuardRootDeleteWithLinkers(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	env := defaultEnvID(t, s, p.ID)
	root, _ := s.CreateNode(&store.CanvasNode{ID: "r1", ProjectID: p.ID, EnvironmentID: env, Label: "db"})
	alias, _ := s.CreateNode(&store.CanvasNode{ID: "a1", ProjectID: p.ID, EnvironmentID: env, Label: "db2"})
	if err := e.SetServiceLink(alias.ID, root.ID); err != nil {
		t.Fatal(err)
	}
	err := e.DeleteService(t.Context(), root.ID)
	if err == nil {
		t.Fatal("expected delete root to fail while linkers exist")
	}
	if !strings.Contains(err.Error(), "linked from") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPromoteLinkedServiceClearsLink(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	tpl := findBuiltin(t, s, "PostgreSQL")
	env := defaultEnvID(t, s, p.ID)

	dbRes, err := e.CreateNodeFromTemplate(CreateNodeFromTemplateRequest{
		ID: "db1", Label: "db", ProjectID: p.ID, EnvironmentID: env, TemplateID: tpl.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	alias, _ := s.CreateNode(&store.CanvasNode{
		ID: "a1", ProjectID: p.ID, EnvironmentID: env, Label: "db-copy", TemplateID: tpl.ID,
	})
	copyNodeEnvVars(t, s, dbRes.Node.ID, alias.ID)
	if err := e.SetServiceLink(alias.ID, dbRes.Node.ID); err != nil {
		t.Fatal(err)
	}
	// Copy service_port so promote is deployable later
	st, _ := s.GetNodeSettings(dbRes.Node.ID)
	_ = s.SetNodeSetting(alias.ID, "service_port", st["service_port"])
	_ = s.SetNodeSetting(alias.ID, "image", st["image"])
	sourcePassword := envValue(t, s, dbRes.Node.ID, "POSTGRES_PASSWORD")
	sourceURL := envValue(t, s, dbRes.Node.ID, "DATABASE_URL")

	if err := e.PromoteLinkedService(t.Context(), alias.ID, "empty", CloneConsistent); err != nil {
		t.Fatalf("Promote: %v", err)
	}
	link, _ := e.GetServiceLink(alias.ID)
	if link != nil {
		t.Fatal("expected link cleared")
	}
	settings, _ := s.GetNodeSettings(alias.ID)
	if len(managedVolumePaths(settings)) == 0 {
		t.Fatal("expected volume mounts restored on promote")
	}
	aliasNode, err := s.GetNode(alias.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := envValue(t, s, alias.ID, "POSTGRES_PASSWORD"); got == "" || got == sourcePassword {
		t.Fatalf("expected promoted alias password to regenerate, got %q (source %q)", got, sourcePassword)
	}
	gotURL := envValue(t, s, alias.ID, "DATABASE_URL")
	if gotURL == sourceURL {
		t.Fatalf("expected promoted alias DATABASE_URL to regenerate, still %q", gotURL)
	}
	if !strings.Contains(gotURL, aliasNode.UID) {
		t.Fatalf("expected promoted alias DATABASE_URL to point at alias hostname, got %q (uid %q)", gotURL, aliasNode.UID)
	}
}

func TestUnlinkServiceRegeneratesGeneratedEnvVars(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	tpl := findBuiltin(t, s, "PostgreSQL")
	env := defaultEnvID(t, s, p.ID)

	dbRes, err := e.CreateNodeFromTemplate(CreateNodeFromTemplateRequest{
		ID: "db1", Label: "db", ProjectID: p.ID, EnvironmentID: env, TemplateID: tpl.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	alias, _ := s.CreateNode(&store.CanvasNode{
		ID: "a1", ProjectID: p.ID, EnvironmentID: env, Label: "db-copy", TemplateID: tpl.ID,
	})
	copyNodeEnvVars(t, s, dbRes.Node.ID, alias.ID)
	if err := e.SetServiceLink(alias.ID, dbRes.Node.ID); err != nil {
		t.Fatal(err)
	}
	st, _ := s.GetNodeSettings(dbRes.Node.ID)
	_ = s.SetNodeSetting(alias.ID, "service_port", st["service_port"])
	_ = s.SetNodeSetting(alias.ID, "image", st["image"])
	sourcePassword := envValue(t, s, dbRes.Node.ID, "POSTGRES_PASSWORD")
	sourceURL := envValue(t, s, dbRes.Node.ID, "DATABASE_URL")

	if err := e.UnlinkService(t.Context(), alias.ID, "fresh"); err != nil {
		t.Fatalf("UnlinkService: %v", err)
	}
	link, _ := e.GetServiceLink(alias.ID)
	if link != nil {
		t.Fatal("expected link cleared")
	}
	settings, _ := s.GetNodeSettings(alias.ID)
	if len(managedVolumePaths(settings)) != 0 {
		t.Fatal("expected fresh unlink to clear local volumes")
	}
	aliasNode, err := s.GetNode(alias.ID)
	if err != nil {
		t.Fatal(err)
	}
	if got := envValue(t, s, alias.ID, "POSTGRES_PASSWORD"); got == "" || got == sourcePassword {
		t.Fatalf("expected unlinked alias password to regenerate, got %q (source %q)", got, sourcePassword)
	}
	gotURL := envValue(t, s, alias.ID, "DATABASE_URL")
	if gotURL == sourceURL {
		t.Fatalf("expected unlinked alias DATABASE_URL to regenerate, still %q", gotURL)
	}
	if !strings.Contains(gotURL, aliasNode.UID) {
		t.Fatalf("expected unlinked alias DATABASE_URL to point at alias hostname, got %q (uid %q)", gotURL, aliasNode.UID)
	}
}

func TestPreviewEnvironmentDuplicateListsAllRoots(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	tpl := findBuiltin(t, s, "PostgreSQL")
	env := defaultEnvID(t, s, p.ID)
	_, err := e.CreateNodeFromTemplate(CreateNodeFromTemplateRequest{
		ID: "db1", Label: "db", ProjectID: p.ID, EnvironmentID: env, TemplateID: tpl.ID,
	})
	if err != nil {
		t.Fatal(err)
	}
	_, _ = s.CreateNode(&store.CanvasNode{ID: "api1", ProjectID: p.ID, EnvironmentID: env, Label: "api"})

	preview, err := e.PreviewEnvironmentDuplicate(env)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview) != 2 {
		t.Fatalf("expected api + db, got %+v", preview)
	}
	byLabel := map[string]StatefulServiceSummary{}
	for _, row := range preview {
		byLabel[row.Label] = row
	}
	if len(byLabel["db"].Volumes) == 0 {
		t.Fatal("db should report volumes for clone")
	}
	if len(byLabel["api"].Volumes) != 0 {
		t.Fatalf("api should have no volumes, got %v", byLabel["api"].Volumes)
	}
	if byLabel["db"].Warning == "" || byLabel["db"].WarningKind == "" {
		t.Fatal("expected warning for share on db")
	}
}

func copyNodeEnvVars(t *testing.T, s *store.Store, sourceNodeID, targetNodeID string) {
	t.Helper()
	vars, err := s.ListEnvVars(sourceNodeID)
	if err != nil {
		t.Fatalf("ListEnvVars: %v", err)
	}
	for _, v := range vars {
		v.NodeID = targetNodeID
		if err := s.UpsertEnvVar(v); err != nil {
			t.Fatalf("copy env var %q: %v", v.Key, err)
		}
	}
}

func envValue(t *testing.T, s *store.Store, nodeID, key string) string {
	t.Helper()
	v, err := s.GetEnvVar(nodeID, key)
	if err != nil {
		t.Fatalf("GetEnvVar(%s): %v", key, err)
	}
	return v.Value
}
