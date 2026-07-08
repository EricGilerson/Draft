package deploy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const e2eCompose = `
services:
  web:
    build:
      context: ./web
      dockerfile: Dockerfile
    ports:
      - "8080:3000"
    environment:
      LOG_LEVEL: info
      DATABASE_URL: postgres://db:5432/app
  db:
    image: postgres:16
    ports:
      - "5432"
    environment:
      POSTGRES_PASSWORD: secret
`

// TestImportExportEndToEnd drives a real compose file through import into a
// store-backed project and back out to Cloud Run, exercising the whole bridge
// end to end: node creation, settings/env stamping, cross-service rewrite,
// image-mode detection, and ref placeholdering on export.
func TestImportExportEndToEnd(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	dir := t.TempDir()
	composePath := filepath.Join(dir, "docker-compose.yml")
	if err := os.WriteFile(composePath, []byte(e2eCompose), 0o644); err != nil {
		t.Fatal(err)
	}

	// --- Import as a new project ---
	res, err := e.ImportConfigAsProject(composePath, "e2e")
	if err != nil {
		t.Fatalf("import: %v", err)
	}
	if len(res.Nodes) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(res.Nodes))
	}

	nodesByLabel := map[string]string{}
	for _, n := range res.Nodes {
		nodesByLabel[n.Label] = n.ID
	}
	webID, ok := nodesByLabel["web"]
	if !ok {
		t.Fatal("web node missing")
	}
	dbID := nodesByLabel["db"]

	// web is build-mode with the stamped source + target engine.
	webSettings, _ := s.GetNodeSettings(webID)
	if webSettings["dockerfile"] == "" || webSettings["service_root"] != "web" {
		t.Errorf("web build settings: %+v", webSettings)
	}
	if webSettings["service_port"] != "3000" {
		t.Errorf("web port = %q, want 3000", webSettings["service_port"])
	}
	if webSettings["target_engine"] != "compose" {
		t.Errorf("web target_engine = %q", webSettings["target_engine"])
	}
	if webSettings["source_config"] == "" || webSettings["source_config_format"] != "compose" {
		t.Errorf("web should carry source_config for round-trip")
	}

	// db is image-mode.
	dbSettings, _ := s.GetNodeSettings(dbID)
	if dbSettings["image"] != "postgres:16" || dbSettings["dockerfile"] != "" {
		t.Errorf("db should be image-mode: %+v", dbSettings)
	}

	// Cross-service rewrite: web's DATABASE_URL now references db via @{...}.
	webEnv, _ := s.ListEnvVars(webID)
	var dbURL string
	for _, v := range webEnv {
		if v.Key == "DATABASE_URL" {
			dbURL = v.Value
		}
	}
	if !strings.Contains(dbURL, "@{db.DRAFT_INTERNAL_HOSTNAME}") {
		t.Errorf("DATABASE_URL not rewired: %q", dbURL)
	}

	// Imported env carries the imported source tag.
	for _, v := range webEnv {
		if v.Source != store_EnvSourceImported {
			t.Errorf("env %q source = %q, want imported", v.Key, v.Source)
		}
	}

	// --- Export web to Cloud Run ---
	exp, err := e.ExportConfig(webID, "cloudrun")
	if err != nil {
		t.Fatalf("export: %v", err)
	}
	if len(exp.Files) == 0 {
		t.Fatal("no export files")
	}
	var yaml string
	for _, f := range exp.Files {
		yaml += f.Content
	}
	if !strings.Contains(yaml, "containerPort: 3000") {
		t.Errorf("export missing containerPort:\n%s", yaml)
	}
	// The @{db...} ref becomes a ${DB_...} placeholder on export.
	if !strings.Contains(yaml, "${DB_") {
		t.Errorf("cross-service ref not placeholdered on export:\n%s", yaml)
	}
	if exp.Report.Notes == nil {
		t.Error("expected a non-nil fidelity report")
	}
}

// store_EnvSourceImported mirrors store.EnvSourceImported without importing the
// constant name into the test's expression (keeps the assertion readable).
const store_EnvSourceImported = "imported"
