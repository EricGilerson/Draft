package deploy

import (
	"strings"
	"testing"

	"Draft/internal/store"
)

func sampleInput() templateExprInput {
	return templateExprInput{
		ServiceName:      "web",
		ProjectName:      "acme",
		Environment:      "default",
		UID:              "ab12",
		ServicePort:      "5432",
		InternalHostname: "web.acme.default.ab12.draft.local",
		InternalURL:      "http://web.acme.default.ab12.draft.local:5432",
		PublicHostname:   "web.acme.default.ab12.draft.resolv.sh",
		PublicURL:        "http://web.acme.default.ab12.draft.resolv.sh:53888",
		UUIDFunc:         func() string { return "deadbeefcafef00d" },
	}
}

func TestResolveTemplateExprsAddressTokens(t *testing.T) {
	in := sampleInput()
	raw := "host={{draft.internal_hostname}} url={{draft.internal_url}} pub={{draft.public_hostname}} purl={{draft.public_url}} port={{draft.service_port}} svc={{draft.service}} proj={{draft.project}} env={{draft.environment}} uid={{draft.uid}}"
	got, err := resolveTemplateExprs(in, raw)
	if err != nil {
		t.Fatal(err)
	}
	want := "host=web.acme.default.ab12.draft.local url=http://web.acme.default.ab12.draft.local:5432 pub=web.acme.default.ab12.draft.resolv.sh purl=http://web.acme.default.ab12.draft.resolv.sh:53888 port=5432 svc=web proj=acme env=default uid=ab12"
	if got != want {
		t.Fatalf("got %q\nwant %q", got, want)
	}
}

func TestResolveTemplateExprsPassesThroughWithoutExpressions(t *testing.T) {
	got, err := resolveTemplateExprs(sampleInput(), "postgres://user:pass@host:5432/db")
	if err != nil {
		t.Fatal(err)
	}
	if got != "postgres://user:pass@host:5432/db" {
		t.Fatalf("unexpected %q", got)
	}
}

func TestResolveTemplateExprsComposesURL(t *testing.T) {
	in := sampleInput()
	in.ServiceName = "db"
	in.InternalHostname = "db.acme.default.ab12.draft.local"
	raw := "postgres://{{draft.db_user}}:{{draft.password}}@{{draft.internal_hostname}}:{{draft.service_port}}/{{draft.db_name}}"
	got, err := resolveTemplateExprs(in, raw)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got, "postgres://acme_default_db:") {
		t.Fatalf("expected db_user prefix, got %q", got)
	}
	if !strings.HasSuffix(got, "@db.acme.default.ab12.draft.local:5432/acme_default_db") {
		t.Fatalf("expected hostname/db suffix, got %q", got)
	}
}

func TestResolveTemplateExprsDeterministic(t *testing.T) {
	in := sampleInput()
	a, _ := resolveTemplateExprs(in, "{{draft.password}}")
	b, _ := resolveTemplateExprs(in, "{{draft.password}}")
	if a != b {
		t.Fatalf("password not deterministic: %q vs %q", a, b)
	}
	if len(a) != 24 {
		t.Fatalf("password len = %d, want 24", len(a))
	}
	dbA, _ := resolveTemplateExprs(in, "{{draft.db_name}}")
	dbB, _ := resolveTemplateExprs(in, "{{draft.db_name}}")
	if dbA != dbB || dbA != "acme_default_web" {
		t.Fatalf("db_name not deterministic/expected: %q vs %q", dbA, dbB)
	}
}

func TestResolveTemplateExprsUniquenessPerIdentity(t *testing.T) {
	base := sampleInput()
	a, _ := resolveTemplateExprs(base, "{{draft.password}}")

	differentProject := base
	differentProject.ProjectName = "other"
	b, _ := resolveTemplateExprs(differentProject, "{{draft.password}}")
	if a == b {
		t.Fatal("password should differ across projects")
	}

	differentEnv := base
	differentEnv.Environment = "staging"
	c, _ := resolveTemplateExprs(differentEnv, "{{draft.password}}")
	if a == c {
		t.Fatal("password should differ across environments")
	}

	differentService := base
	differentService.ServiceName = "cache"
	d, _ := resolveTemplateExprs(differentService, "{{draft.password}}")
	if a == d {
		t.Fatal("password should differ across services")
	}

	differentUID := base
	differentUID.UID = "zz99"
	e, _ := resolveTemplateExprs(differentUID, "{{draft.password}}")
	if a == e {
		t.Fatal("password should differ across uids")
	}

	// db_name differs across project/env/service (uid intentionally excluded so
	// two same-named services in different envs get distinct dbs).
	dbA, _ := resolveTemplateExprs(base, "{{draft.db_name}}")
	dbEnv, _ := resolveTemplateExprs(differentEnv, "{{draft.db_name}}")
	if dbA == dbEnv {
		t.Fatalf("db_name should differ across envs: %q", dbA)
	}
}

func TestResolveTemplateExprsUnknownTokenErrors(t *testing.T) {
	_, err := resolveTemplateExprs(sampleInput(), "{{draft.bogus}}")
	if err == nil || !strings.Contains(err.Error(), "unknown draft expression") {
		t.Fatalf("expected unknown token error, got %v", err)
	}
}

func TestResolveTemplateExprsUUID(t *testing.T) {
	got, err := resolveTemplateExprs(sampleInput(), "token-{{draft.uuid}}")
	if err != nil {
		t.Fatal(err)
	}
	if got != "token-deadbeefcafef00d" {
		t.Fatalf("got %q", got)
	}
}

func TestDeriveIdentifierSafety(t *testing.T) {
	cases := []struct{ in, want string }{
		{"Acme-App 2", "acme_app_2"},
		{"123numeric", "n123numeric"},
		{"---", "draft"},
		{"café", "caf"},
	}
	for _, c := range cases {
		got := deriveIdentifier(c.in)
		if got != c.want {
			t.Errorf("deriveIdentifier(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestDeriveDbUserTruncatesToMySQLLimit(t *testing.T) {
	long := deriveDbUser("verylongprojectname", "staging", "verylongservicename")
	if len(long) > 32 {
		t.Fatalf("db_user len = %d, want <= 32 (%q)", len(long), long)
	}
}

// TestPreviewEnvVarsExpandsDraftExprThenRefs is the end-to-end check that the
// live resolver expands {{draft.*}} against the owning node's identity and
// THEN substitutes @{Label.ATTR} cross-service references, so an app's
// @{db.DATABASE_URL} sees the db's fully-resolved connection string.
func TestPreviewEnvVarsExpandsDraftExprThenRefs(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	s.DB.Create(&store.Project{Name: "acme", Path: "/acme"})
	s.DB.Create(&store.CanvasNode{ID: "db", ProjectID: 1, Label: "postgres"})
	s.DB.Create(&store.CanvasNode{ID: "web", ProjectID: 1, Label: "web"})
	s.SetNodeSetting("db", "service_port", "5432")
	s.SetNodeSetting("web", "service_port", "3000")

	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: "db", Key: "DATABASE_URL", Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
		Value: "postgres://{{draft.db_user}}:{{draft.password}}@{{draft.internal_hostname}}:{{draft.service_port}}/{{draft.db_name}}",
	}); err != nil {
		t.Fatal(err)
	}
	if err := s.UpsertEnvVar(store.EnvVar{
		NodeID: "web", Key: "DATABASE_URL", Scope: store.EnvScopeRuntime, Source: store.EnvSourceManual,
		Value: "@{postgres.DATABASE_URL}",
	}); err != nil {
		t.Fatal(err)
	}

	preview, err := e.PreviewEnvVars("web")
	if err != nil {
		t.Fatal(err)
	}
	got := preview["DATABASE_URL"]
	if got.Error != "" {
		t.Fatalf("resolution error: %s", got.Error)
	}
	if got.Kind != "resolve" {
		t.Fatalf("kind = %q, want resolve", got.Kind)
	}
	if !strings.HasPrefix(got.Value, "postgres://acme_default_postgres:") {
		t.Fatalf("expected resolved db URL with derived user, got %q", got.Value)
	}
	if !strings.Contains(got.Value, "@postgres.acme.default.") || !strings.HasSuffix(got.Value, "/acme_default_postgres") {
		t.Fatalf("expected db's own hostname + db_name in resolved URL, got %q", got.Value)
	}
}
