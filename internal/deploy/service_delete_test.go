package deploy

import (
	"context"
	"strings"
	"testing"
)

func TestListServiceDependents(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	project, api, db := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(api.ID, "DATABASE_URL", "@{db.DRAFT_INTERNAL_URL}"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvVar(api.ID, "DB_HOST", "@{db.DRAFT_INTERNAL_HOSTNAME}"); err != nil {
		t.Fatal(err)
	}

	deps, err := e.ListServiceDependents(db.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(deps) != 2 {
		t.Fatalf("dependents = %+v", deps)
	}
	if deps[0].SourceNodeID != api.ID || deps[0].SourceLabel != "api" {
		t.Fatalf("first dependent = %+v", deps[0])
	}

	// Nothing references api.
	apiDeps, err := e.ListServiceDependents(api.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(apiDeps) != 0 {
		t.Fatalf("expected no dependents for api, got %+v", apiDeps)
	}
	_ = project
}

func TestListNodesWithReferenceIssues(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	project, api, db := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(api.ID, "GHOST", "@{missing.DRAFT_INTERNAL_URL}"); err != nil {
		t.Fatal(err)
	}

	ids, err := ListNodesWithReferenceIssues(s, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != api.ID {
		t.Fatalf("ids = %+v", ids)
	}

	if err := e.DeleteService(context.Background(), db.ID); err != nil {
		t.Fatal(err)
	}

	ids, err = ListNodesWithReferenceIssues(s, project.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(ids) != 1 || ids[0] != api.ID {
		t.Fatalf("expected api after db delete, ids = %+v", ids)
	}
}

func TestListReferenceIssues(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	_, api, db := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(api.ID, "GHOST", "@{missing.DRAFT_INTERNAL_URL}"); err != nil {
		t.Fatal(err)
	}
	if err := s.SetEnvVar(api.ID, "BAD_ATTR", "@{db.NOT_A_VAR}"); err != nil {
		t.Fatal(err)
	}
	_ = db

	issues, err := e.ListReferenceIssues(api.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 2 {
		t.Fatalf("issues = %+v", issues)
	}
	foundMissing := false
	foundAttr := false
	for _, issue := range issues {
		if strings.Contains(issue.Reason, "no service named") {
			foundMissing = true
		}
		if strings.Contains(issue.Reason, "no variable named") {
			foundAttr = true
		}
	}
	if !foundMissing || !foundAttr {
		t.Fatalf("issues = %+v", issues)
	}
}

func TestDeleteServiceRemovesNodeAndLeavesDependentValues(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	project, api, db := setupRefTestNodes(t, s)

	if err := s.SetEnvVar(api.ID, "DATABASE_URL", "@{db.DRAFT_INTERNAL_URL}"); err != nil {
		t.Fatal(err)
	}

	preview, err := e.PreviewDeleteService(context.Background(), db.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(preview.Dependents) != 1 || preview.Dependents[0].VarKey != "DATABASE_URL" {
		t.Fatalf("preview = %+v", preview)
	}

	if err := e.DeleteService(context.Background(), db.ID); err != nil {
		t.Fatal(err)
	}
	if _, err := s.GetNode(db.ID); err == nil {
		t.Fatal("expected db node to be deleted")
	}

	v, err := s.GetEnvVar(api.ID, "DATABASE_URL")
	if err != nil {
		t.Fatal(err)
	}
	if v.Value != "@{db.DRAFT_INTERNAL_URL}" {
		t.Fatalf("dependent value changed to %q", v.Value)
	}

	issues, err := e.ListReferenceIssues(api.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(issues) != 1 || !strings.Contains(issues[0].Reason, "no service named") {
		t.Fatalf("issues = %+v", issues)
	}
	_ = project
}
