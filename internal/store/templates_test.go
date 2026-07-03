package store

import (
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
