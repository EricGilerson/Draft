package store

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
)

func TestSeedBuiltinsPopulatesLibrary(t *testing.T) {
	s := openTemp(t)

	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	if len(list) == 0 {
		t.Fatal("expected built-in templates to be seeded on Open, got none")
	}
	names := map[string]bool{}
	for _, tpl := range list {
		if !tpl.Builtin {
			t.Errorf("template %q is not builtin right after seed", tpl.Name)
		}
		names[tpl.Name] = true
	}
	for _, want := range []string{"Next.js", "FastAPI / Uvicorn", "PostgreSQL"} {
		if !names[want] {
			t.Errorf("missing seeded template %q", want)
		}
	}
}

func TestSeedBuiltinsReconcilesExistingBuiltin(t *testing.T) {
	s := openTemp(t)

	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	var next *ServiceTemplate
	for i := range list {
		if list[i].Name == "Next.js" {
			next = &list[i]
			break
		}
	}
	if next == nil {
		t.Fatal("Next.js template not seeded")
	}
	original := next.Dockerfile
	if !strings.Contains(original, "next start") && !strings.Contains(original, `"npm", "start"`) {
		t.Fatalf("unexpected baseline dockerfile: %q", original)
	}
	// Corrupt the stored built-in to simulate a stale seed from an older version.
	next.Dockerfile = "FROM node:20-alpine\nCMD [\"npm\", \"run\", \"dev\"]\n"
	if err := s.DB.Save(next).Error; err != nil {
		t.Fatalf("tamper: %v", err)
	}

	if err := s.SeedBuiltins(); err != nil {
		t.Fatalf("SeedBuiltins (reconcile): %v", err)
	}

	got, err := s.GetTemplate(next.ID)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.Dockerfile != original {
		t.Errorf("SeedBuiltins did not reconcile stale built-in: got %q, want %q", got.Dockerfile, original)
	}
	if !got.Builtin {
		t.Error("reconciled template lost Builtin flag")
	}
}

func TestSeedBuiltinsLeavesUserTemplateWithNameCollision(t *testing.T) {
	s := openTemp(t)

	// A user-owned template that happens to share a name with a built-in must
	// not be clobbered or have its content overwritten by SeedBuiltins.
	user, err := s.CreateTemplate(&ServiceTemplate{Name: "Vite", Port: 5173, Dockerfile: "FROM scratch"})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if err := s.SeedBuiltins(); err != nil {
		t.Fatalf("SeedBuiltins: %v", err)
	}
	got, err := s.GetTemplate(user.ID)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.Builtin {
		t.Error("user template was promoted to builtin")
	}
	if got.Dockerfile != "FROM scratch" {
		t.Errorf("user template content was overwritten: got %q", got.Dockerfile)
	}
}

func TestSeedBuiltinsIsIdempotent(t *testing.T) {
	s := openTemp(t)
	before, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates before: %v", err)
	}
	if err := s.SeedBuiltins(); err != nil {
		t.Fatalf("SeedBuiltins (second run): %v", err)
	}
	after, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates after: %v", err)
	}
	if len(before) != len(after) {
		t.Errorf("seed duplicated templates: before=%d after=%d", len(before), len(after))
	}
}

func TestListTemplatesBuiltinFirstThenName(t *testing.T) {
	s := openTemp(t)

	if _, err := s.CreateTemplate(&ServiceTemplate{Name: "Zeta", Port: 4000}); err != nil {
		t.Fatalf("CreateTemplate Zeta: %v", err)
	}
	if _, err := s.CreateTemplate(&ServiceTemplate{Name: "Alpha", Port: 4000}); err != nil {
		t.Fatalf("CreateTemplate Alpha: %v", err)
	}

	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	// Built-ins come first; then user templates by name (Alpha before Zeta).
	firstUser := -1
	for i, tpl := range list {
		if !tpl.Builtin {
			firstUser = i
			break
		}
	}
	if firstUser < 0 {
		t.Fatal("expected at least one user template")
	}
	if !list[firstUser].Builtin && list[firstUser-1].Builtin == false {
		t.Error("built-ins should precede user templates")
	}
	if list[firstUser].Name != "Alpha" {
		t.Errorf("first user template = %q, want Alpha", list[firstUser].Name)
	}
}

func TestCreateTemplateForcesNotBuiltin(t *testing.T) {
	s := openTemp(t)

	tpl, err := s.CreateTemplate(&ServiceTemplate{Name: "Sneaky", Builtin: true, Port: 4000})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if tpl.Builtin {
		t.Error("CreateTemplate must force Builtin=false")
	}
}

func TestUpdateAndDeleteRejectBuiltin(t *testing.T) {
	s := openTemp(t)

	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	var builtin *ServiceTemplate
	for i := range list {
		if list[i].Builtin {
			builtin = &list[i]
			break
		}
	}
	if builtin == nil {
		t.Fatal("no builtin template found")
	}
	builtin.Description = "tampered"
	if err := s.UpdateTemplate(builtin); !errors.Is(err, ErrTemplateBuiltin) {
		t.Errorf("UpdateTemplate on builtin: got %v, want ErrTemplateBuiltin", err)
	}
	if err := s.DeleteTemplate(builtin.ID); !errors.Is(err, ErrTemplateBuiltin) {
		t.Errorf("DeleteTemplate on builtin: got %v, want ErrTemplateBuiltin", err)
	}
}

func TestUpdateDeleteUserTemplate(t *testing.T) {
	s := openTemp(t)

	tpl, err := s.CreateTemplate(&ServiceTemplate{Name: "Worker", Port: 4000})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	tpl.Port = 5151
	if err := s.UpdateTemplate(tpl); err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	got, err := s.GetTemplate(tpl.ID)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.Port != 5151 {
		t.Errorf("port = %d, want 5151", got.Port)
	}
	if err := s.DeleteTemplate(tpl.ID); err != nil {
		t.Fatalf("DeleteTemplate: %v", err)
	}
	if _, err := s.GetTemplate(tpl.ID); !errors.Is(err, ErrTemplateNotFound) {
		t.Errorf("GetTemplate after delete: got %v, want ErrTemplateNotFound", err)
	}
}

func TestCloneTemplate(t *testing.T) {
	s := openTemp(t)

	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	var builtin *ServiceTemplate
	for i := range list {
		if list[i].Builtin {
			builtin = &list[i]
			break
		}
	}
	if builtin == nil {
		t.Fatal("no builtin template found")
	}

	clone, err := s.CloneTemplate(builtin.ID)
	if err != nil {
		t.Fatalf("CloneTemplate: %v", err)
	}
	if clone.Builtin {
		t.Error("clone must not be builtin")
	}
	if clone.ID == builtin.ID {
		t.Error("clone must have a new ID")
	}
	if !strings.HasPrefix(clone.Name, builtin.Name) {
		t.Errorf("clone name = %q, want prefix %q", clone.Name, builtin.Name)
	}
	if !strings.Contains(clone.Name, "(copy)") {
		t.Errorf("clone name = %q, want '(copy)' suffix", clone.Name)
	}
	// Cloning again should not collide on the "(copy)" name.
	clone2, err := s.CloneTemplate(builtin.ID)
	if err != nil {
		t.Fatalf("CloneTemplate (second): %v", err)
	}
	if clone2.Name == clone.Name {
		t.Errorf("second clone name %q collides with first %q", clone2.Name, clone.Name)
	}
}

func TestCreateTemplateValidatesName(t *testing.T) {
	s := openTemp(t)
	if _, err := s.CreateTemplate(&ServiceTemplate{Name: "   "}); !errors.Is(err, ErrInvalidTemplate) {
		t.Errorf("blank name: got %v, want ErrInvalidTemplate", err)
	}
}

// templateEnvKeys parses a template's EnvVars JSON and returns the set of keys.
func templateEnvKeys(t *testing.T, tpl *ServiceTemplate) map[string]string {
	t.Helper()
	var entries []struct {
		Key   string `json:"key"`
		Value string `json:"value"`
	}
	if tpl.EnvVars == "" {
		return map[string]string{}
	}
	if err := json.Unmarshal([]byte(tpl.EnvVars), &entries); err != nil {
		t.Fatalf("template %q has invalid EnvVars JSON: %v", tpl.Name, err)
	}
	out := make(map[string]string, len(entries))
	for _, e := range entries {
		out[e.Key] = e.Value
	}
	return out
}

func TestBuiltinDBTemplatesExposeFullVarSet(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	byName := map[string]*ServiceTemplate{}
	for i := range list {
		byName[list[i].Name] = &list[i]
	}

	cases := []struct {
		name        string
		mustHave    []string
		mustExpr    []string // keys whose value must be a {{draft.*}} expression
		mustLiteral map[string]string
		mustNotLit  []string // keys whose value must NOT be the old "draft" literal
	}{
		{
			name:        "PostgreSQL",
			mustHave:    []string{"POSTGRES_USER", "POSTGRES_PASSWORD", "POSTGRES_DB", "POSTGRES_HOST_AUTH_METHOD", "PGDATA", "DATABASE_URL", "PUBLIC_DATABASE_URL"},
			mustExpr:    []string{"POSTGRES_PASSWORD", "DATABASE_URL", "PUBLIC_DATABASE_URL"},
			mustLiteral: map[string]string{"POSTGRES_USER": "postgres", "POSTGRES_DB": "postgres"},
			mustNotLit:  []string{"POSTGRES_PASSWORD"},
		},
		{
			name:        "MySQL",
			mustHave:    []string{"MYSQL_ROOT_PASSWORD", "MYSQL_DATABASE", "MYSQL_USER", "MYSQL_PASSWORD", "MYSQL_ROOT_HOST", "MYSQL_LOG_CONSOLE", "DATABASE_URL", "PUBLIC_DATABASE_URL"},
			mustExpr:    []string{"MYSQL_ROOT_PASSWORD", "MYSQL_PASSWORD", "DATABASE_URL", "PUBLIC_DATABASE_URL"},
			mustLiteral: map[string]string{"MYSQL_DATABASE": "appdb", "MYSQL_USER": "mysql"},
			mustNotLit:  []string{"MYSQL_ROOT_PASSWORD", "MYSQL_PASSWORD"},
		},
		{
			name:       "Redis",
			mustHave:   []string{"REDIS_PASSWORD", "REDIS_URL", "PUBLIC_REDIS_URL"},
			mustExpr:   []string{"REDIS_PASSWORD", "REDIS_URL", "PUBLIC_REDIS_URL"},
			mustNotLit: []string{"REDIS_PASSWORD"},
		},
		{
			name:        "MongoDB",
			mustHave:    []string{"MONGO_INITDB_ROOT_USERNAME", "MONGO_INITDB_ROOT_PASSWORD", "MONGO_INITDB_DATABASE", "DATABASE_URL", "PUBLIC_DATABASE_URL"},
			mustExpr:    []string{"MONGO_INITDB_ROOT_PASSWORD", "DATABASE_URL", "PUBLIC_DATABASE_URL"},
			mustLiteral: map[string]string{"MONGO_INITDB_ROOT_USERNAME": "root", "MONGO_INITDB_DATABASE": "appdb"},
			mustNotLit:  []string{"MONGO_INITDB_ROOT_PASSWORD"},
		},
	}
	for _, c := range cases {
		tpl, ok := byName[c.name]
		if !ok {
			t.Errorf("missing built-in %q", c.name)
			continue
		}
		keys := templateEnvKeys(t, tpl)
		for _, k := range c.mustHave {
			if _, ok := keys[k]; !ok {
				t.Errorf("%q missing required env var %q", c.name, k)
			}
		}
		for _, k := range c.mustExpr {
			if v := keys[k]; !strings.Contains(v, "{{draft.") {
				t.Errorf("%q env var %q should use a {{draft.*}} expression, got %q", c.name, k, v)
			}
		}
		for k, want := range c.mustLiteral {
			if v := keys[k]; v != want {
				t.Errorf("%q env var %q = %q, want literal %q", c.name, k, v, want)
			}
		}
		for _, k := range c.mustNotLit {
			if v := keys[k]; v == "draft" {
				t.Errorf("%q env var %q still uses hardcoded literal %q", c.name, k, v)
			}
		}
	}
}

func TestBuiltinRedisTemplateEnforcesAuthViaCmdOverride(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	var redis *ServiceTemplate
	for i := range list {
		if list[i].Name == "Redis" {
			redis = &list[i]
			break
		}
	}
	if redis == nil {
		t.Fatal("Redis built-in not seeded")
	}
	if !strings.Contains(redis.CmdOverride, "--requirepass") {
		t.Errorf("Redis CmdOverride must enforce auth via --requirepass, got %q", redis.CmdOverride)
	}
	if !strings.Contains(redis.CmdOverride, "{{draft.password}}") {
		t.Errorf("Redis CmdOverride must use {{draft.password}}, got %q", redis.CmdOverride)
	}
}

func TestBuiltinMongoTemplateAuthViaEnvWithAuthSource(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	var mongo *ServiceTemplate
	for i := range list {
		if list[i].Name == "MongoDB" {
			mongo = &list[i]
			break
		}
	}
	if mongo == nil {
		t.Fatal("MongoDB built-in not seeded")
	}
	// The official mongo entrypoint auto-enables --auth when both ROOT_* vars
	// are set, so — unlike Redis — no CmdOverride should be present.
	if mongo.CmdOverride != "" {
		t.Errorf("MongoDB should rely on env-driven auth, got CmdOverride %q", mongo.CmdOverride)
	}
	keys := templateEnvKeys(t, mongo)
	if keys["MONGO_INITDB_ROOT_USERNAME"] != "root" {
		t.Errorf("MONGO_INITDB_ROOT_USERNAME = %q, want root", keys["MONGO_INITDB_ROOT_USERNAME"])
	}
	if keys["MONGO_INITDB_ROOT_PASSWORD"] != "{{draft.password}}" {
		t.Errorf("MONGO_INITDB_ROOT_PASSWORD = %q, want {{draft.password}}", keys["MONGO_INITDB_ROOT_PASSWORD"])
	}
	// Root user lives in the `admin` db, so the connection URL must carry
	// authSource=admin or auth fails.
	if !strings.Contains(keys["DATABASE_URL"], "authSource=admin") {
		t.Errorf("DATABASE_URL must include authSource=admin, got %q", keys["DATABASE_URL"])
	}
	if !strings.Contains(keys["PUBLIC_DATABASE_URL"], "authSource=admin") {
		t.Errorf("PUBLIC_DATABASE_URL must include authSource=admin, got %q", keys["PUBLIC_DATABASE_URL"])
	}
}

func TestBuiltinTemplatesCarrySchema(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	for _, tpl := range list {
		if tpl.Schema == "" {
			t.Errorf("built-in %q has empty Schema", tpl.Name)
			continue
		}
		schema, err := ParseTemplateSchema(tpl.Schema)
		if err != nil {
			t.Errorf("built-in %q has invalid Schema: %v", tpl.Name, err)
			continue
		}
		switch tpl.Mode {
		case ModeImage:
			if schema.ServiceRoot != SchemaHidden || schema.Dockerfile != SchemaHidden {
				t.Errorf("image template %q should hide serviceRoot+dockerfile, got %+v", tpl.Name, schema)
			}
		case ModeBuild:
			if schema.ServiceRoot != SchemaOptional {
				t.Errorf("build template %q serviceRoot should be optional, got %q", tpl.Name, schema.ServiceRoot)
			}
		}
	}
}

func TestCreateTemplateNormalizesSchema(t *testing.T) {
	s := openTemp(t)
	// Empty schema on a build template should normalize to the default build schema.
	tpl, err := s.CreateTemplate(&ServiceTemplate{Name: "Custom Build", Mode: "build", Port: 4000})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	schema, err := ParseTemplateSchema(tpl.Schema)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if schema.ServiceRoot != SchemaOptional || schema.Dockerfile != SchemaOptional {
		t.Errorf("default build schema not applied: %+v", schema)
	}
	if len(schema.WizardSteps) != 4 {
		t.Errorf("expected 4 wizard steps, got %d", len(schema.WizardSteps))
	}

	// Image template with empty schema should hide source/dockerfile.
	img, err := s.CreateTemplate(&ServiceTemplate{Name: "Custom DB", Mode: "image", Image: "foo:1", Port: 5000})
	if err != nil {
		t.Fatalf("CreateTemplate image: %v", err)
	}
	imgSchema, err := ParseTemplateSchema(img.Schema)
	if err != nil {
		t.Fatalf("parse image: %v", err)
	}
	if imgSchema.ServiceRoot != SchemaHidden {
		t.Errorf("image serviceRoot should be hidden, got %q", imgSchema.ServiceRoot)
	}
}

func TestCreateTemplateRejectsMalformedSchema(t *testing.T) {
	s := openTemp(t)
	if _, err := s.CreateTemplate(&ServiceTemplate{Name: "Bad", Mode: "build", Schema: "{not json"}); err == nil {
		t.Error("expected error for malformed schema, got nil")
	}
}

func TestCloneTemplateCarriesSchema(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	var pg *ServiceTemplate
	for i := range list {
		if list[i].Name == "PostgreSQL" {
			pg = &list[i]
			break
		}
	}
	if pg == nil {
		t.Fatal("PostgreSQL not seeded")
	}
	clone, err := s.CloneTemplate(pg.ID)
	if err != nil {
		t.Fatalf("CloneTemplate: %v", err)
	}
	if clone.Schema != pg.Schema {
		t.Errorf("clone schema = %q, want %q (clone must carry schema)", clone.Schema, pg.Schema)
	}
	if clone.ImageTags != pg.ImageTags {
		t.Errorf("clone imageTags = %q, want %q (clone must carry imageTags)", clone.ImageTags, pg.ImageTags)
	}
}

// statelessDatastores are image-mode "datastore" built-ins with no on-disk
// data directory by design (pure in-memory caches), so they're exempt from
// the "must ship a default volume" rule below.
var statelessDatastores = map[string]bool{
	"Memcached": true,
}

// TestBuiltinDBTemplatesCarryVolumes asserts each datastore built-in ships a
// Draft-managed named volume for its data directory, so a freshly created
// database persists across redeploys. Build-mode built-ins have no volumes.
func TestBuiltinDBTemplatesCarryVolumes(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	for _, tpl := range list {
		vols, err := ParseTemplateVolumes(tpl.Volumes)
		if err != nil {
			t.Errorf("built-in %q has invalid Volumes: %v", tpl.Name, err)
			continue
		}
		if tpl.Mode == ModeImage && tpl.Category == "datastore" && !statelessDatastores[tpl.Name] {
			if len(vols) == 0 {
				t.Errorf("datastore built-in %q should ship at least one default volume", tpl.Name)
				continue
			}
			for _, v := range vols {
				if v.Type != "volume" {
					t.Errorf("built-in %q volume %q should be type volume, got %q", tpl.Name, v.ContainerPath, v.Type)
				}
				if v.ContainerPath == "" {
					t.Errorf("built-in %q has a volume with no containerPath", tpl.Name)
				}
			}
		} else if tpl.Mode != ModeImage && len(vols) != 0 {
			t.Errorf("build built-in %q should have no default volumes, got %d", tpl.Name, len(vols))
		}
	}
}

// TestBuiltinDBTemplatesShowVolumesSection asserts the image schema no longer
// hides the Volumes Settings section (datastores need it) and exposes a
// volumes wizard step.
func TestBuiltinDBTemplatesShowVolumesSection(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	found := false
	for _, tpl := range list {
		if tpl.Mode != ModeImage || tpl.Category != "datastore" {
			continue
		}
		schema, err := ParseTemplateSchema(tpl.Schema)
		if err != nil {
			t.Fatalf("built-in %q schema: %v", tpl.Name, err)
		}
		for _, hid := range schema.HideSections {
			if hid == SectionVolumes {
				t.Errorf("built-in %q should not hide the volumes section", tpl.Name)
			}
		}
		if schema.Volumes == nil || !schema.Volumes.Show {
			t.Errorf("built-in %q should expose a volumes wizard capability", tpl.Name)
		}
		hasVolumesStep := false
		for _, st := range schema.WizardSteps {
			if st.ID == "volumes" {
				hasVolumesStep = true
			}
		}
		if !hasVolumesStep {
			t.Errorf("built-in %q wizard should include a volumes step", tpl.Name)
		}
		found = true
	}
	if !found {
		t.Fatal("no image-mode built-in found to test")
	}
}

func TestCloneTemplateCarriesVolumes(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	var pg *ServiceTemplate
	for i := range list {
		if list[i].Name == "PostgreSQL" {
			pg = &list[i]
			break
		}
	}
	if pg == nil {
		t.Fatal("PostgreSQL not seeded")
	}
	clone, err := s.CloneTemplate(pg.ID)
	if err != nil {
		t.Fatalf("CloneTemplate: %v", err)
	}
	if clone.Volumes != pg.Volumes {
		t.Errorf("clone volumes = %q, want %q (clone must carry volumes)", clone.Volumes, pg.Volumes)
	}
}

func TestCreateTemplateNormalizesVolumes(t *testing.T) {
	s := openTemp(t)
	tpl, err := s.CreateTemplate(&ServiceTemplate{
		Name:    "Vol Template",
		Mode:    "image",
		Image:   "foo:1",
		Port:    5000,
		Volumes: `[{"type":"volume","containerPath":"/data"},{"containerPath":"  "},{"type":"volume","containerPath":"/cache","sizeHint":"  10g  "}]`,
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	vols, err := ParseTemplateVolumes(tpl.Volumes)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(vols) != 2 {
		t.Fatalf("expected 2 volumes after normalize, got %d", len(vols))
	}
	if vols[0].ContainerPath != "/data" || vols[0].Type != "volume" {
		t.Errorf("vol[0] = %+v", vols[0])
	}
	if vols[1].SizeHint != "10g" {
		t.Errorf("sizeHint not trimmed: %q", vols[1].SizeHint)
	}
	if _, err := s.CreateTemplate(&ServiceTemplate{Name: "Bad Vol", Mode: "image", Image: "x:1", Port: 1, Volumes: "{not json"}); err == nil {
		t.Error("expected error for malformed volumes JSON, got nil")
	}
}

// TestBuiltinDBTemplatesCarryImageTags asserts each datastore built-in ships a
// curated tag list, and that the tag of the default Image ref is the FIRST tag
// (so the version picker's default option matches the template default).
func TestBuiltinDBTemplatesCarryImageTags(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	for _, tpl := range list {
		if tpl.Mode != ModeImage || tpl.Category != "datastore" {
			continue
		}
		tags, err := ParseImageTags(tpl.ImageTags)
		if err != nil {
			t.Errorf("built-in %q has invalid ImageTags: %v", tpl.Name, err)
			continue
		}
		if len(tags) == 0 {
			t.Errorf("built-in %q has no curated ImageTags", tpl.Name)
			continue
		}
		_, defaultTag := SplitImageRef(tpl.Image)
		if tags[0] != defaultTag {
			t.Errorf("built-in %q first ImageTag %q should match default image tag %q", tpl.Name, tags[0], defaultTag)
		}
	}
}

// TestPrebuiltImageBuiltin asserts the generic "Prebuilt Image" built-in exists
// in image mode with no default image, no curated tags, no default port, and a
// schema that hides the build/source sections (so users get a clean Settings
// view for their own image) while leaving runtime/volumes/etc. available.
func TestPrebuiltImageBuiltin(t *testing.T) {
	s := openTemp(t)
	list, err := s.ListTemplates()
	if err != nil {
		t.Fatalf("ListTemplates: %v", err)
	}
	var tpl *ServiceTemplate
	for i := range list {
		if list[i].Name == "Prebuilt Image" {
			tpl = &list[i]
			break
		}
	}
	if tpl == nil {
		t.Fatal("Prebuilt Image built-in not seeded")
	}
	if tpl.Mode != ModeImage {
		t.Errorf("Mode = %q, want image", tpl.Mode)
	}
	if tpl.Image != "" {
		t.Errorf("Image = %q, want empty (user supplies it)", tpl.Image)
	}
	if tpl.Port != 0 {
		t.Errorf("Port = %d, want 0 (user supplies it)", tpl.Port)
	}
	if tags, _ := ParseImageTags(tpl.ImageTags); len(tags) != 0 {
		t.Errorf("ImageTags = %v, want empty", tags)
	}
	schema, err := ParseTemplateSchema(tpl.Schema)
	if err != nil {
		t.Fatalf("schema: %v", err)
	}
	hidden := map[string]bool{}
	for _, h := range schema.HideSections {
		hidden[h] = true
	}
	for _, want := range []string{SectionSource, SectionDockerfile, SectionBuildContext, SectionBuildConfig} {
		if !hidden[want] {
			t.Errorf("schema should hide %q for prebuilt image", want)
		}
	}
	for _, notWant := range []string{SectionVolumes, SectionRuntimeCommand, SectionRestart, SectionHealthcheck} {
		if hidden[notWant] {
			t.Errorf("schema should NOT hide %q for prebuilt image", notWant)
		}
	}
}

// TestCreateTemplateNormalizesImageTags verifies the tag list is trimmed +
// deduped on create and update, and malformed JSON is rejected.
func TestCreateTemplateNormalizesImageTags(t *testing.T) {
	s := openTemp(t)
	tpl, err := s.CreateTemplate(&ServiceTemplate{
		Name:      "Custom DB",
		Mode:      "image",
		Image:     "foo:1",
		Port:      5000,
		ImageTags: `["1"," 1 ","","2"]`,
	})
	if err != nil {
		t.Fatalf("CreateTemplate: %v", err)
	}
	if tpl.ImageTags != `["1","2"]` {
		t.Errorf("ImageTags not normalized: %q", tpl.ImageTags)
	}
	if err := s.UpdateTemplate(&ServiceTemplate{
		ID:        tpl.ID,
		Name:      "Custom DB",
		Mode:      "image",
		Image:     "foo:1",
		ImageTags: `["3","3"]`,
	}); err != nil {
		t.Fatalf("UpdateTemplate: %v", err)
	}
	got, err := s.GetTemplate(tpl.ID)
	if err != nil {
		t.Fatalf("GetTemplate: %v", err)
	}
	if got.ImageTags != `["3"]` {
		t.Errorf("ImageTags after update = %q, want [\"3\"]", got.ImageTags)
	}
	if _, err := s.CreateTemplate(&ServiceTemplate{Name: "Bad Tags", Mode: "image", Image: "x:1", ImageTags: "{not json"}); err == nil {
		t.Error("expected error for malformed ImageTags, got nil")
	}
}
