package gitsrc

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// newRepoWithSubmodule creates a parent repo that vendors a child repo at
// vendor/child via git submodule. The child is initialized so .git/modules
// holds the recorded commit.
func newRepoWithSubmodule(t *testing.T) (parent, child string) {
	t.Helper()
	child = t.TempDir()
	runGit(t, child, "init", "-b", "main", "-q")
	runGit(t, child, "config", "core.autocrlf", "false")
	writeFile(t, filepath.Join(child, "file.txt"), "hello from submodule\n")
	runGit(t, child, "add", ".")
	runGit(t, child, "commit", "-q", "-m", "sub init")

	parent = t.TempDir()
	runGit(t, parent, "init", "-b", "main", "-q")
	runGit(t, parent, "config", "core.autocrlf", "false")
	runGit(t, parent, "config", "protocol.file.allow", "always")
	writeFile(t, filepath.Join(parent, "root.txt"), "root\n")
	runGit(t, parent, "add", ".")
	runGit(t, parent, "commit", "-q", "-m", "root")

	runGit(t, parent, "submodule", "add", child, "vendor/child")
	runGit(t, parent, "commit", "-q", "-m", "add sub")
	return parent, child
}

func TestListGitlinks(t *testing.T) {
	parent, _ := newRepoWithSubmodule(t)
	links, err := ListGitlinks(context.Background(), parent, "main")
	if err != nil {
		t.Fatalf("ListGitlinks: %v", err)
	}
	if len(links) != 1 {
		t.Fatalf("got %d gitlinks, want 1: %+v", len(links), links)
	}
	if links[0].Path != "vendor/child" {
		t.Fatalf("path = %q, want vendor/child", links[0].Path)
	}
	if links[0].SHA == "" {
		t.Fatal("expected non-empty SHA")
	}
}

func TestArchiveToDir_OmitsSubmoduleContents(t *testing.T) {
	parent, _ := newRepoWithSubmodule(t)
	dest := t.TempDir()
	if err := ArchiveToDir(context.Background(), parent, "main", dest); err != nil {
		t.Fatalf("ArchiveToDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "vendor", "child", "file.txt")); !os.IsNotExist(err) {
		t.Fatalf("plain archive should not include submodule files, err=%v", err)
	}
}

func TestMaterializeSubmodules_IncludesPinnedTree(t *testing.T) {
	parent, _ := newRepoWithSubmodule(t)
	dest := t.TempDir()
	if err := ArchiveToDir(context.Background(), parent, "main", dest); err != nil {
		t.Fatalf("ArchiveToDir: %v", err)
	}
	if err := MaterializeSubmodules(context.Background(), parent, "main", dest, ""); err != nil {
		t.Fatalf("MaterializeSubmodules: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "vendor", "child", "file.txt"))
	if err != nil {
		t.Fatalf("read submodule file: %v", err)
	}
	if got := normalizeNewlines(string(data)); got != "hello from submodule\n" {
		t.Fatalf("content = %q", data)
	}

	// Dirty parent working tree must not affect the archive.
	writeFile(t, filepath.Join(parent, "root.txt"), "DIRTY\n")
	writeFile(t, filepath.Join(parent, "vendor", "child", "file.txt"), "DIRTY SUB\n")
	dest2 := t.TempDir()
	if err := ArchiveToDir(context.Background(), parent, "main", dest2); err != nil {
		t.Fatalf("ArchiveToDir: %v", err)
	}
	if err := MaterializeSubmodules(context.Background(), parent, "main", dest2, ""); err != nil {
		t.Fatalf("MaterializeSubmodules: %v", err)
	}
	root, _ := os.ReadFile(filepath.Join(dest2, "root.txt"))
	sub, _ := os.ReadFile(filepath.Join(dest2, "vendor", "child", "file.txt"))
	if got := normalizeNewlines(string(root)); got != "root\n" {
		t.Fatalf("parent dirty leaked: %q", root)
	}
	if got := normalizeNewlines(string(sub)); got != "hello from submodule\n" {
		t.Fatalf("submodule dirty leaked: %q", sub)
	}
}

func normalizeNewlines(s string) string {
	return strings.ReplaceAll(s, "\r\n", "\n")
}

func TestWriteArchiveWithSubmodules_SplicesContents(t *testing.T) {
	parent, _ := newRepoWithSubmodule(t)
	var buf bytes.Buffer
	if err := WriteArchiveWithSubmodules(context.Background(), parent, "main", "", &buf); err != nil {
		t.Fatalf("WriteArchiveWithSubmodules: %v", err)
	}
	names := map[string]bool{}
	tr := tar.NewReader(&buf)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("read tar: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			names[hdr.Name] = true
		}
	}
	if !names["root.txt"] {
		t.Fatal("missing root.txt")
	}
	if !names["vendor/child/file.txt"] {
		t.Fatalf("missing spliced submodule file; got %v", names)
	}
}

func TestSubmoduleObjectsAvailable_FalseWhenModulesMissing(t *testing.T) {
	parent, _ := newRepoWithSubmodule(t)
	// Remove the modules cache to simulate a clone without submodule init.
	gitDir, err := resolveGitDir(context.Background(), parent)
	if err != nil {
		t.Fatalf("resolveGitDir: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(gitDir, "modules")); err != nil {
		t.Fatalf("remove modules: %v", err)
	}
	ok, err := SubmoduleObjectsAvailable(context.Background(), parent, "main", "")
	if err != nil {
		t.Fatalf("SubmoduleObjectsAvailable: %v", err)
	}
	if ok {
		t.Fatal("expected objects unavailable after removing modules cache")
	}
	dest := t.TempDir()
	_ = ArchiveToDir(context.Background(), parent, "main", dest)
	err = MaterializeSubmodules(context.Background(), parent, "main", dest, "")
	if !errors.Is(err, ErrSubmoduleObjectsMissing) {
		t.Fatalf("expected ErrSubmoduleObjectsMissing, got %v", err)
	}
}

func TestCheckoutWithSubmodules_FetchesMissingModules(t *testing.T) {
	parent, _ := newRepoWithSubmodule(t)
	gitDir, err := resolveGitDir(context.Background(), parent)
	if err != nil {
		t.Fatalf("resolveGitDir: %v", err)
	}
	if err := os.RemoveAll(filepath.Join(gitDir, "modules")); err != nil {
		t.Fatalf("remove modules: %v", err)
	}
	// Also clear the working-tree submodule checkout so update must re-clone.
	_ = os.RemoveAll(filepath.Join(parent, "vendor", "child"))

	wt, cleanup, err := CheckoutWithSubmodules(context.Background(), parent, "main")
	if err != nil {
		t.Fatalf("CheckoutWithSubmodules: %v", err)
	}
	defer cleanup()

	data, err := os.ReadFile(filepath.Join(wt, "vendor", "child", "file.txt"))
	if err != nil {
		t.Fatalf("read worktree submodule file: %v", err)
	}
	if got := normalizeNewlines(string(data)); got != "hello from submodule\n" {
		t.Fatalf("content = %q", data)
	}
	// Parent worktree must remain without requiring us to have restored modules
	// into the caller's checkout for this assertion — cleanup should leave parent usable.
	if _, err := os.Stat(filepath.Join(parent, "root.txt")); err != nil {
		t.Fatalf("parent root missing: %v", err)
	}
}

func TestMaterializeSubmodules_Nested(t *testing.T) {
	// grand := leaf, child embeds grand, parent embeds child
	grand := t.TempDir()
	runGit(t, grand, "init", "-b", "main", "-q")
	runGit(t, grand, "config", "core.autocrlf", "false")
	writeFile(t, filepath.Join(grand, "leaf.txt"), "leaf\n")
	runGit(t, grand, "add", ".")
	runGit(t, grand, "commit", "-q", "-m", "leaf")

	child := t.TempDir()
	runGit(t, child, "init", "-b", "main", "-q")
	runGit(t, child, "config", "core.autocrlf", "false")
	runGit(t, child, "config", "protocol.file.allow", "always")
	writeFile(t, filepath.Join(child, "mid.txt"), "mid\n")
	runGit(t, child, "add", ".")
	runGit(t, child, "commit", "-q", "-m", "mid")
	runGit(t, child, "submodule", "add", grand, "nested")
	runGit(t, child, "commit", "-q", "-m", "add nested")

	parent := t.TempDir()
	runGit(t, parent, "init", "-b", "main", "-q")
	runGit(t, parent, "config", "core.autocrlf", "false")
	runGit(t, parent, "config", "protocol.file.allow", "always")
	writeFile(t, filepath.Join(parent, "root.txt"), "root\n")
	runGit(t, parent, "add", ".")
	runGit(t, parent, "commit", "-q", "-m", "root")
	runGit(t, parent, "submodule", "add", child, "vendor/child")
	runGit(t, parent, "commit", "-q", "-m", "add child")

	// Ensure nested module objects are present under parent’s modules cache.
	runGit(t, parent, "submodule", "update", "--init", "--recursive")

	dest := t.TempDir()
	if err := ArchiveToDir(context.Background(), parent, "main", dest); err != nil {
		t.Fatalf("ArchiveToDir: %v", err)
	}
	if err := MaterializeSubmodules(context.Background(), parent, "main", dest, ""); err != nil {
		t.Fatalf("MaterializeSubmodules: %v", err)
	}
	leaf, err := os.ReadFile(filepath.Join(dest, "vendor", "child", "nested", "leaf.txt"))
	if err != nil {
		t.Fatalf("read nested leaf: %v", err)
	}
	if got := normalizeNewlines(string(leaf)); got != "leaf\n" {
		t.Fatalf("leaf = %q", leaf)
	}
}

func TestMaterializeSubmodules_ContextSubtreePrefix(t *testing.T) {
	parent, child := newRepoWithSubmodule(t)
	// Move layout: put the submodule under svc/ via a fresh parent structure.
	_ = parent
	_ = child

	svcParent := t.TempDir()
	runGit(t, svcParent, "init", "-b", "main", "-q")
	runGit(t, svcParent, "config", "core.autocrlf", "false")
	runGit(t, svcParent, "config", "protocol.file.allow", "always")
	writeFile(t, filepath.Join(svcParent, "svc", "app.txt"), "app\n")
	runGit(t, svcParent, "add", ".")
	runGit(t, svcParent, "commit", "-q", "-m", "svc")
	runGit(t, svcParent, "submodule", "add", child, "svc/vendor/lib")
	runGit(t, svcParent, "commit", "-q", "-m", "add lib under svc")

	dest := t.TempDir()
	if err := ArchiveToDir(context.Background(), svcParent, "main:svc", dest); err != nil {
		t.Fatalf("ArchiveToDir: %v", err)
	}
	if err := MaterializeSubmodules(context.Background(), svcParent, "main:svc", dest, "svc"); err != nil {
		t.Fatalf("MaterializeSubmodules: %v", err)
	}
	data, err := os.ReadFile(filepath.Join(dest, "vendor", "lib", "file.txt"))
	if err != nil {
		t.Fatalf("read lib file: %v", err)
	}
	if !strings.Contains(string(data), "hello from submodule") {
		t.Fatalf("content = %q", data)
	}
}
