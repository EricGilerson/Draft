package deploy

import (
	"archive/tar"
	"bytes"
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"Draft/internal/gitsrc"
)

func runGitRepo(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=t@t",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=t@t",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

// submoduleFixture builds a parent repo that vendors child at vendor/lib with
// a Dockerfile that fails the build unless vendor/lib/marker.txt is present.
func submoduleFixture(t *testing.T) (parent, child string) {
	t.Helper()
	child = t.TempDir()
	runGitRepo(t, child, "init", "-b", "main", "-q")
	runGitRepo(t, child, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(child, "marker.txt"), []byte("from-sub-committed\n"), 0o644); err != nil {
		t.Fatalf("write marker: %v", err)
	}
	runGitRepo(t, child, "add", ".")
	runGitRepo(t, child, "commit", "-q", "-m", "sub")

	parent = t.TempDir()
	runGitRepo(t, parent, "init", "-b", "main", "-q")
	runGitRepo(t, parent, "config", "core.autocrlf", "false")
	runGitRepo(t, parent, "config", "protocol.file.allow", "always")
	dockerfile := `FROM alpine:3.20
COPY vendor/lib/marker.txt /marker.txt
RUN grep -q from-sub-committed /marker.txt
EXPOSE 8080
CMD ["true"]
`
	if err := os.WriteFile(filepath.Join(parent, "Dockerfile"), []byte(dockerfile), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	runGitRepo(t, parent, "add", ".")
	runGitRepo(t, parent, "commit", "-q", "-m", "root")
	runGitRepo(t, parent, "submodule", "add", child, "vendor/lib")
	runGitRepo(t, parent, "commit", "-q", "-m", "add sub")
	return parent, child
}

func removeModulesCache(t *testing.T, repo string) {
	t.Helper()
	cmd := exec.Command("git", "-C", repo, "rev-parse", "--absolute-git-dir")
	out, err := cmd.Output()
	if err != nil {
		t.Fatalf("git-dir: %v", err)
	}
	gitDir := strings.TrimSpace(string(out))
	if err := os.RemoveAll(filepath.Join(gitDir, "modules")); err != nil {
		t.Fatalf("remove modules: %v", err)
	}
	_ = os.RemoveAll(filepath.Join(repo, "vendor", "lib"))
}

func TestResolvePinnedGitSource_StreamSpliceWhenLocal(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	parent, _ := submoduleFixture(t)

	var logs []string
	pinned, err := e.resolvePinnedGitSource(context.Background(), "n1", parent, parent, "main", "Dockerfile", "", true, func(line string) {
		logs = append(logs, line)
	})
	if err != nil {
		t.Fatalf("resolvePinnedGitSource: %v", err)
	}
	if pinned.Cleanup != nil {
		defer pinned.Cleanup()
	}
	if !pinned.Stream {
		t.Fatal("expected Stream=true when module objects are local")
	}
	if pinned.Mode != "stream-splice" {
		t.Fatalf("mode = %q, want stream-splice", pinned.Mode)
	}
	if !pinned.HasSubmodules || !pinned.LocalSubmodules {
		t.Fatalf("submodule flags: has=%v local=%v", pinned.HasSubmodules, pinned.LocalSubmodules)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "splicing from local") {
		t.Fatalf("expected splice log, got:\n%s", joined)
	}
}

func TestResolvePinnedGitSource_WorktreeWhenModulesMissing(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	parent, _ := submoduleFixture(t)
	removeModulesCache(t, parent)

	var logs []string
	pinned, err := e.resolvePinnedGitSource(context.Background(), "n1", parent, parent, "main", "Dockerfile", "", true, func(line string) {
		logs = append(logs, line)
	})
	if err != nil {
		t.Fatalf("resolvePinnedGitSource: %v", err)
	}
	if pinned.Cleanup == nil {
		t.Fatal("expected cleanup for worktree")
	}
	defer pinned.Cleanup()

	if pinned.Stream {
		t.Fatal("expected Stream=false when falling back to worktree")
	}
	if pinned.Mode != "worktree-update" {
		t.Fatalf("mode = %q, want worktree-update", pinned.Mode)
	}
	data, err := os.ReadFile(filepath.Join(pinned.SourcePath, "vendor", "lib", "marker.txt"))
	if err != nil {
		t.Fatalf("worktree missing submodule marker: %v", err)
	}
	if !strings.Contains(string(data), "from-sub-committed") {
		t.Fatalf("marker content = %q", data)
	}
	joined := strings.Join(logs, "\n")
	if !strings.Contains(joined, "worktree") {
		t.Fatalf("expected worktree log, got:\n%s", joined)
	}
}

func TestResolvePinnedGitSource_CheckoutMaterialize(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	parent, _ := submoduleFixture(t)

	pinned, err := e.resolvePinnedGitSource(context.Background(), "n1", parent, parent, "main", "Dockerfile", "", false, func(string) {})
	if err != nil {
		t.Fatalf("resolvePinnedGitSource: %v", err)
	}
	if pinned.Cleanup == nil {
		t.Fatal("expected cleanup")
	}
	defer pinned.Cleanup()

	if pinned.Stream {
		t.Fatal("expected non-stream checkout")
	}
	if pinned.Mode != "checkout-materialize" {
		t.Fatalf("mode = %q, want checkout-materialize", pinned.Mode)
	}
	data, err := os.ReadFile(filepath.Join(pinned.SourcePath, "vendor", "lib", "marker.txt"))
	if err != nil {
		t.Fatalf("materialized source missing marker: %v", err)
	}
	if !strings.Contains(string(data), "from-sub-committed") {
		t.Fatalf("marker = %q", data)
	}
}

func TestResolvePinnedGitSource_IgnoresDirtyWorkingTree(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	parent, _ := submoduleFixture(t)

	// Poison the working tree — git-pinned source must still use committed content.
	if err := os.WriteFile(filepath.Join(parent, "vendor", "lib", "marker.txt"), []byte("DIRTY-WORKING-TREE\n"), 0o644); err != nil {
		t.Fatalf("poison wt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(parent, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("poison Dockerfile: %v", err)
	}

	pinned, err := e.resolvePinnedGitSource(context.Background(), "n1", parent, parent, "main", "Dockerfile", "", false, func(string) {})
	if err != nil {
		t.Fatalf("resolvePinnedGitSource: %v", err)
	}
	defer pinned.Cleanup()

	marker, err := os.ReadFile(filepath.Join(pinned.SourcePath, "vendor", "lib", "marker.txt"))
	if err != nil {
		t.Fatalf("read marker: %v", err)
	}
	if strings.Contains(string(marker), "DIRTY") {
		t.Fatalf("dirty working tree leaked into pinned source: %q", marker)
	}
	if !strings.Contains(string(marker), "from-sub-committed") {
		t.Fatalf("expected committed marker, got %q", marker)
	}
	df, err := os.ReadFile(filepath.Join(pinned.SourcePath, "Dockerfile"))
	if err != nil {
		t.Fatalf("read Dockerfile: %v", err)
	}
	if !strings.Contains(string(df), "from-sub-committed") {
		t.Fatalf("expected committed Dockerfile (with submodule check), got %q", df)
	}
}

func TestResolvePinnedGitSource_StreamSpliceTarContainsSubmodule(t *testing.T) {
	parent, _ := submoduleFixture(t)
	var buf bytes.Buffer
	if err := gitsrc.WriteArchiveWithSubmodules(context.Background(), parent, "main", "", &buf); err != nil {
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
			t.Fatalf("tar: %v", err)
		}
		if hdr.Typeflag == tar.TypeReg {
			names[hdr.Name] = true
		}
	}
	if !names["vendor/lib/marker.txt"] || !names["Dockerfile"] {
		t.Fatalf("expected Dockerfile + vendor/lib/marker.txt in splice tar, got %v", names)
	}
}

func TestResolvePinnedGitSource_NoSubmodulesStream(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	repo := gitRepoWithCommit(t)

	pinned, err := e.resolvePinnedGitSource(context.Background(), "n1", repo, repo, "main", "Dockerfile", "", true, func(string) {})
	if err != nil {
		t.Fatalf("resolve: %v", err)
	}
	if pinned.Cleanup != nil {
		defer pinned.Cleanup()
	}
	if !pinned.Stream || pinned.HasSubmodules || pinned.Mode != "stream" {
		t.Fatalf("got stream=%v hasSubs=%v mode=%q", pinned.Stream, pinned.HasSubmodules, pinned.Mode)
	}
}

func TestPlainArchiveOmitsSubmoduleMarker(t *testing.T) {
	parent, _ := submoduleFixture(t)
	dest := t.TempDir()
	if err := gitsrc.ArchiveToDir(context.Background(), parent, "main", dest); err != nil {
		t.Fatalf("ArchiveToDir: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dest, "vendor", "lib", "marker.txt")); !os.IsNotExist(err) {
		t.Fatalf("plain archive unexpectedly has marker (err=%v)", err)
	}
}
