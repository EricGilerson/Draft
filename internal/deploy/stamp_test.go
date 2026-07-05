package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"Draft/internal/store"
)

// createStampProject seeds a project pointing at dir and returns it.
func createStampProject(t *testing.T, s *store.Store, dir string) *store.Project {
	t.Helper()
	p, err := s.CreateProject("stamp-proj", dir, "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	return p
}

func findBuiltin(t *testing.T, s *store.Store, name string) *store.ServiceTemplate {
	t.Helper()
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	for i := range list {
		if list[i].Name == name {
			return &list[i]
		}
	}
	t.Fatalf("built-in %q not seeded", name)
	return nil
}

func TestStampFromBuildTemplate(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	tpl := findBuiltin(t, s, "Node.js")

	res, err := e.CreateNodeFromTemplate(CreateNodeFromTemplateRequest{
		ID:         "n1",
		Label:      "api",
		ProjectID:  p.ID,
		X:          10,
		Y:          20,
		TemplateID: tpl.ID,
	})
	if err != nil {
		t.Fatalf("CreateNodeFromTemplate: %v", err)
	}
	if res.Node.TemplateID != tpl.ID {
		t.Errorf("node template id = %d, want %d", res.Node.TemplateID, tpl.ID)
	}
	if res.Node.UID == "" {
		t.Error("node UID not assigned at stamp time")
	}

	settings, err := s.GetNodeSettings("n1")
	if err != nil {
		t.Fatalf("GetNodeSettings: %v", err)
	}
	if settings["dockerfile"] != "Dockerfile" {
		t.Errorf("dockerfile setting = %q, want Dockerfile", settings["dockerfile"])
	}
	if settings["service_port"] != "3000" {
		t.Errorf("service_port = %q, want 3000", settings["service_port"])
	}

	// Embedded Dockerfile should have been written to the project root.
	body, err := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if err != nil {
		t.Fatalf("Dockerfile not written: %v", err)
	}
	if !strings.Contains(string(body), "node:20-alpine") {
		t.Errorf("written Dockerfile body mismatch: %q", body)
	}

	// Env vars seeded with {{draft.*}} resolved to concrete values.
	vars, err := s.ListEnvVars("n1")
	if err != nil {
		t.Fatalf("ListEnvVars: %v", err)
	}
	byKey := map[string]store.EnvVar{}
	for _, v := range vars {
		byKey[v.Key] = v
	}
	if byKey["NODE_ENV"].Value != "production" {
		t.Errorf("NODE_ENV = %q, want production", byKey["NODE_ENV"].Value)
	}
	if byKey["NODE_ENV"].Source != store.EnvSourceGenerated {
		t.Errorf("NODE_ENV source = %q, want generated", byKey["NODE_ENV"].Source)
	}
	if strings.Contains(byKey["NODE_ENV"].Value, "{{draft.") {
		t.Errorf("NODE_ENV still contains an unresolved expression: %q", byKey["NODE_ENV"].Value)
	}
	if res.DeployStarted {
		t.Error("build-mode stamp should not auto-deploy")
	}
}

func TestStampFromImageTemplateHidesServiceRootAndStampsImage(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	tpl := findBuiltin(t, s, "PostgreSQL")

	res, err := e.CreateNodeFromTemplate(CreateNodeFromTemplateRequest{
		ID:         "db1",
		Label:      "db",
		ProjectID:  p.ID,
		TemplateID: tpl.ID,
		// A stray service root should be ignored for image templates.
		ServiceRoot: filepath.Join(dir, "should-be-ignored"),
	})
	if err != nil {
		t.Fatalf("CreateNodeFromTemplate: %v", err)
	}
	if !res.DeployStarted {
		t.Error("image-mode stamp should auto-deploy")
	}

	settings, _ := s.GetNodeSettings("db1")
	if settings["image"] != "postgres:16-alpine" {
		t.Errorf("image setting = %q, want postgres:16-alpine", settings["image"])
	}
	if settings["service_port"] != "5432" {
		t.Errorf("service_port = %q, want 5432", settings["service_port"])
	}
	if settings["dockerfile"] != "" {
		t.Errorf("image template should not stamp dockerfile, got %q", settings["dockerfile"])
	}
	if settings["service_root"] != "" {
		t.Errorf("image template should not stamp service_root, got %q", settings["service_root"])
	}

	vars, _ := s.ListEnvVars("db1")
	byKey := map[string]store.EnvVar{}
	for _, v := range vars {
		byKey[v.Key] = v
	}
	if byKey["POSTGRES_PASSWORD"].Value == "" {
		t.Error("POSTGRES_PASSWORD not seeded")
	}
	if strings.Contains(byKey["POSTGRES_PASSWORD"].Value, "{{draft.") {
		t.Errorf("POSTGRES_PASSWORD not resolved: %q", byKey["POSTGRES_PASSWORD"].Value)
	}
	if strings.Contains(byKey["DATABASE_URL"].Value, "{{draft.") {
		t.Errorf("DATABASE_URL not resolved: %q", byKey["DATABASE_URL"].Value)
	}
}

func TestStampFromImageTemplateHonorsImageOverride(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	tpl := findBuiltin(t, s, "PostgreSQL")

	if _, err := e.CreateNodeFromTemplate(CreateNodeFromTemplateRequest{
		ID:         "dbv",
		Label:      "db",
		ProjectID:  p.ID,
		TemplateID: tpl.ID,
		Overrides:  map[string]string{"image": "postgres:15-alpine"},
	}); err != nil {
		t.Fatalf("CreateNodeFromTemplate: %v", err)
	}

	settings, _ := s.GetNodeSettings("dbv")
	if settings["image"] != "postgres:15-alpine" {
		t.Errorf("image override = %q, want postgres:15-alpine (override should win over template default)", settings["image"])
	}
}

func TestStampWritesDockerfileOnlyWhenAbsent(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)
	tpl := findBuiltin(t, s, "Node.js")

	// Pre-existing Dockerfile must be preserved.
	existing := []byte("FROM scratch\n")
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), existing, 0o644); err != nil {
		t.Fatalf("seed existing: %v", err)
	}

	res, err := e.CreateNodeFromTemplate(CreateNodeFromTemplateRequest{
		ID: "n2", Label: "api2", ProjectID: p.ID, TemplateID: tpl.ID,
	})
	if err != nil {
		t.Fatalf("CreateNodeFromTemplate: %v", err)
	}
	if !hasWarning(res.Warnings, "left unchanged") {
		t.Errorf("expected 'left unchanged' warning, got %v", res.Warnings)
	}
	body, _ := os.ReadFile(filepath.Join(dir, "Dockerfile"))
	if string(body) != string(existing) {
		t.Errorf("existing Dockerfile was overwritten: %q", body)
	}
}

func TestStampRollsBackOnError(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dir := t.TempDir()
	p := createStampProject(t, s, dir)

	// Reference a non-existent template id.
	if _, err := e.CreateNodeFromTemplate(CreateNodeFromTemplateRequest{
		ID: "n3", Label: "x", ProjectID: p.ID, TemplateID: 99999,
	}); err == nil {
		t.Error("expected error for missing template, got nil")
	}
	// Node must not linger.
	if _, err := s.GetNode("n3"); err == nil {
		t.Error("rolled-back node row still present")
	}
}

func hasWarning(warnings []string, needle string) bool {
	for _, w := range warnings {
		if strings.Contains(w, needle) {
			return true
		}
	}
	return false
}
