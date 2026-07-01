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
)

// runGitIn runs git in dir, failing the test on error.
func runGitIn(t *testing.T, dir string, args ...string) {
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

// tarEntryNames returns the set of file entry names in a tar byte stream.
func tarEntryNames(t *testing.T, data []byte) map[string]bool {
	t.Helper()
	names := map[string]bool{}
	tr := tar.NewReader(bytes.NewReader(data))
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
	return names
}

func gitRepoWithCommit(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	run := func(args ...string) { runGitIn(t, dir, args...) }
	run("init", "-b", "main", "-q")
	run("config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write Dockerfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "app.txt"), []byte("committed\n"), 0o644); err != nil {
		t.Fatalf("write app.txt: %v", err)
	}
	run("add", ".")
	run("commit", "-q", "-m", "initial")
	return dir
}

func TestPrepareGitSource_MaterializesBranch(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	repo := gitRepoWithCommit(t)

	dir, err := e.prepareGitSource(context.Background(), "node-1", repo, "main", ".")
	if err != nil {
		t.Fatalf("prepareGitSource: %v", err)
	}
	defer os.RemoveAll(dir)

	if dir == repo {
		t.Fatalf("expected an ephemeral dir distinct from the repo, got the repo path")
	}
	data, err := os.ReadFile(filepath.Join(dir, "app.txt"))
	if err != nil {
		t.Fatalf("read materialized app.txt: %v", err)
	}
	if string(data) != "committed\n" {
		t.Fatalf("unexpected content: %q", data)
	}
	// The archive must not carry the .git directory into the build context.
	if _, err := os.Stat(filepath.Join(dir, ".git")); !os.IsNotExist(err) {
		t.Fatalf("expected no .git in archived source")
	}
}

// TestPrepareGitSource_SubtreeOnly is the monorepo optimization: with a
// contextRel of "svc", only that subtree is exported (mirrored at
// <workspace>/svc), and sibling top-level content is not materialized — so a
// service in a large repo doesn't drag the whole tree onto disk.
func TestPrepareGitSource_SubtreeOnly(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	repo := gitRepoWithCommit(t) // Dockerfile + app.txt at root

	// Add a service subdir plus a large sibling that must NOT be materialized.
	if err := os.WriteFile(filepath.Join(repo, "svc", "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		_ = os.MkdirAll(filepath.Join(repo, "svc"), 0o755)
		if err2 := os.WriteFile(filepath.Join(repo, "svc", "Dockerfile"), []byte("FROM scratch\n"), 0o644); err2 != nil {
			t.Fatalf("write svc/Dockerfile: %v", err2)
		}
	}
	if err := os.WriteFile(filepath.Join(repo, "svc", "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write svc/main.go: %v", err)
	}
	_ = os.MkdirAll(filepath.Join(repo, "huge-assets"), 0o755)
	if err := os.WriteFile(filepath.Join(repo, "huge-assets", "blob.bin"), make([]byte, 1<<20), 0o644); err != nil {
		t.Fatalf("write huge-assets/blob.bin: %v", err)
	}
	runGitIn(t, repo, "add", ".")
	runGitIn(t, repo, "commit", "-q", "-m", "add svc and assets")

	dir, err := e.prepareGitSource(context.Background(), "node-1", repo, "main", "svc")
	if err != nil {
		t.Fatalf("prepareGitSource: %v", err)
	}
	defer os.RemoveAll(dir)

	// The svc subtree is present at the mirrored location <workspace>/svc.
	if _, err := os.Stat(filepath.Join(dir, "svc", "Dockerfile")); err != nil {
		t.Fatalf("expected svc/Dockerfile in workspace: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "svc", "main.go")); err != nil {
		t.Fatalf("expected svc/main.go in workspace: %v", err)
	}
	// Sibling top-level content must NOT have been materialized.
	if _, err := os.Stat(filepath.Join(dir, "huge-assets")); !os.IsNotExist(err) {
		t.Fatalf("huge-assets should not be materialized for a svc-only export")
	}
	if _, err := os.Stat(filepath.Join(dir, "app.txt")); !os.IsNotExist(err) {
		t.Fatalf("root app.txt should not be materialized for a svc-only export")
	}
}

func TestPrepareGitSource_LeavesWorkingTreeUntouched(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	repo := gitRepoWithCommit(t)

	// Dirty the working tree with an uncommitted edit + untracked file.
	if err := os.WriteFile(filepath.Join(repo, "app.txt"), []byte("DIRTY\n"), 0o644); err != nil {
		t.Fatalf("dirty app.txt: %v", err)
	}
	if err := os.WriteFile(filepath.Join(repo, "scratch.tmp"), []byte("x\n"), 0o644); err != nil {
		t.Fatalf("write untracked: %v", err)
	}

	dir, err := e.prepareGitSource(context.Background(), "node-1", repo, "main", ".")
	if err != nil {
		t.Fatalf("prepareGitSource: %v", err)
	}
	defer os.RemoveAll(dir)

	// Materialized source reflects the commit, not the dirty tree.
	data, _ := os.ReadFile(filepath.Join(dir, "app.txt"))
	if string(data) != "committed\n" {
		t.Fatalf("archive leaked uncommitted edit: %q", data)
	}
	if _, err := os.Stat(filepath.Join(dir, "scratch.tmp")); !os.IsNotExist(err) {
		t.Fatalf("archive included untracked file")
	}

	// Working tree edits survive untouched.
	wt, _ := os.ReadFile(filepath.Join(repo, "app.txt"))
	if string(wt) != "DIRTY\n" {
		t.Fatalf("working tree was mutated: %q", wt)
	}
	if _, err := os.Stat(filepath.Join(repo, "scratch.tmp")); err != nil {
		t.Fatalf("untracked working-tree file was removed: %v", err)
	}
}

func TestPrepareGitSource_NotARepo(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	_, err := e.prepareGitSource(context.Background(), "node-1", t.TempDir(), "main", ".")
	if err == nil {
		t.Fatalf("expected error for non-git project directory")
	}
	if !strings.Contains(err.Error(), "not a git repository") {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestPrepareGitSource_BadBranch(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	repo := gitRepoWithCommit(t)

	_, err := e.prepareGitSource(context.Background(), "node-1", repo, "no-such-branch", ".")
	if err == nil {
		t.Fatalf("expected error for nonexistent branch")
	}
}

func TestRebaseUnderSource(t *testing.T) {
	// Use real OS-absolute paths so filepath.IsAbs behaves as in production
	// (on Windows a path needs a volume like C:\ to count as absolute).
	project := t.TempDir()
	source := t.TempDir()
	outside := t.TempDir()

	// Relative paths are untouched — they already resolve against the source root.
	if got, err := rebaseUnderSource(project, source, "Dockerfile"); err != nil || got != "Dockerfile" {
		t.Fatalf("relative path: got %q, err %v", got, err)
	}
	if got, err := rebaseUnderSource(project, source, filepath.FromSlash("svc/Dockerfile")); err != nil || got != filepath.FromSlash("svc/Dockerfile") {
		t.Fatalf("relative subpath: got %q, err %v", got, err)
	}
	// Empty is passed through.
	if got, err := rebaseUnderSource(project, source, ""); err != nil || got != "" {
		t.Fatalf("empty path: got %q, err %v", got, err)
	}
	// Absolute path inside the project is re-anchored under source.
	absIn := filepath.Join(project, "svc", "Dockerfile")
	want := filepath.Join(source, "svc", "Dockerfile")
	if got, err := rebaseUnderSource(project, source, absIn); err != nil || got != want {
		t.Fatalf("absolute in-project: got %q want %q, err %v", got, want, err)
	}
	// Absolute path outside the project is rejected.
	absOut := filepath.Join(outside, "Dockerfile")
	if _, err := rebaseUnderSource(project, source, absOut); err == nil {
		t.Fatalf("expected error for absolute path outside project")
	}
}

func TestCountingReaderFiresEOFOnce(t *testing.T) {
	data := bytes.Repeat([]byte("x"), 10<<20) // 10 MB so onProgress also fires
	var progressCalls int
	var eofCalls, eofTotal int64
	cr := &countingReader{
		r:          bytes.NewReader(data),
		onProgress: func(int64) { progressCalls++ },
		onEOF: func(sent int64) {
			eofCalls++
			eofTotal = sent
		},
	}

	// Drain fully, then attempt one more read (already at EOF).
	n, err := io.Copy(io.Discard, cr)
	if err != nil {
		t.Fatalf("copy: %v", err)
	}
	_, _ = cr.Read(make([]byte, 8))

	if n != int64(len(data)) {
		t.Fatalf("copied %d bytes, want %d", n, len(data))
	}
	if eofCalls != 1 {
		t.Fatalf("onEOF fired %d times, want exactly 1", eofCalls)
	}
	if eofTotal != int64(len(data)) {
		t.Fatalf("onEOF reported %d bytes, want %d", eofTotal, len(data))
	}
	if progressCalls == 0 {
		t.Fatalf("expected onProgress to fire for a 10MB stream")
	}
}

func TestGitStreamEnabled(t *testing.T) {
	cases := map[string]bool{
		"":      true, // default on
		"true":  true,
		"1":     true,
		"on":    true,
		"false": false,
		"0":     false,
		"off":   false,
		"FALSE": false,
	}
	for val, want := range cases {
		got := gitStreamEnabled(map[string]string{"git_stream": val})
		if got != want {
			t.Fatalf("gitStreamEnabled(%q) = %v, want %v", val, got, want)
		}
	}
	// Missing key entirely defaults to on.
	if !gitStreamEnabled(map[string]string{}) {
		t.Fatalf("gitStreamEnabled with no setting should default to true")
	}
}

func TestGitArchiveTreeish(t *testing.T) {
	project := t.TempDir()

	// Context == repo root → bare ref.
	if got, err := gitArchiveTreeish(project, project, "main"); err != nil || got != "main" {
		t.Fatalf("root context: got %q err %v", got, err)
	}

	// Context in a subdirectory → ref:subdir (forward slashes even on Windows).
	sub := filepath.Join(project, "services", "web")
	if got, err := gitArchiveTreeish(project, sub, "develop"); err != nil || got != "develop:services/web" {
		t.Fatalf("subdir context: got %q err %v", got, err)
	}

	// Context outside the repo → error.
	if _, err := gitArchiveTreeish(project, t.TempDir(), "main"); err == nil {
		t.Fatalf("expected error for context outside repo")
	}
}

// TestGitStreamTreeishMatchesArchive verifies that the tree-ish computed for a
// subdirectory build context actually produces the expected files when handed
// to `git archive` — i.e. paths are rooted at the subdir.
func TestGitStreamTreeishMatchesArchive(t *testing.T) {
	repo := gitRepoWithCommit(t) // Dockerfile + app.txt at root
	// Add a subdirectory service with its own Dockerfile.
	sub := filepath.Join(repo, "svc")
	if err := os.MkdirAll(sub, 0o755); err != nil {
		t.Fatalf("mkdir svc: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "Dockerfile"), []byte("FROM scratch\n"), 0o644); err != nil {
		t.Fatalf("write svc/Dockerfile: %v", err)
	}
	if err := os.WriteFile(filepath.Join(sub, "main.go"), []byte("package main\n"), 0o644); err != nil {
		t.Fatalf("write svc/main.go: %v", err)
	}
	runGitIn(t, repo, "add", ".")
	runGitIn(t, repo, "commit", "-q", "-m", "add svc")

	treeish, err := gitArchiveTreeish(repo, sub, "main")
	if err != nil {
		t.Fatalf("gitArchiveTreeish: %v", err)
	}
	if treeish != "main:svc" {
		t.Fatalf("unexpected treeish: %q", treeish)
	}

	// Archive that treeish and confirm the subtree is rooted at svc.
	cmd := exec.Command("git", "-C", repo, "archive", "--format=tar", treeish)
	tarBytes, err := cmd.Output()
	if err != nil {
		t.Fatalf("git archive %s: %v", treeish, err)
	}
	names := tarEntryNames(t, tarBytes)
	// Rooted at svc → entries are "Dockerfile" and "main.go", NOT "svc/...".
	if !names["Dockerfile"] || !names["main.go"] {
		t.Fatalf("expected Dockerfile and main.go at archive root, got %v", names)
	}
	if names["svc/Dockerfile"] || names["app.txt"] {
		t.Fatalf("archive should be rooted at svc and exclude root files, got %v", names)
	}
}

// TestGitDeploy_AbsoluteDockerfileResolves reproduces the original "stuck at
// Exported…" bug: an absolute Dockerfile path (as stored by the file picker)
// must still resolve to a valid build context when deploying from a branch.
func TestGitDeploy_AbsoluteDockerfileResolves(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	repo := gitRepoWithCommit(t) // commits a Dockerfile at repo root

	absDockerfile := filepath.Join(repo, "Dockerfile")

	source, err := e.prepareGitSource(context.Background(), "node-1", repo, "main", ".")
	if err != nil {
		t.Fatalf("prepareGitSource: %v", err)
	}
	defer os.RemoveAll(source)

	rebased, err := rebaseUnderSource(repo, source, absDockerfile)
	if err != nil {
		t.Fatalf("rebaseUnderSource: %v", err)
	}

	plan, err := resolveBuildContextPlan(source, "", rebased)
	if err != nil {
		t.Fatalf("resolveBuildContextPlan (git mode, absolute dockerfile): %v", err)
	}
	if plan.RelativeDockerfile != "Dockerfile" {
		t.Fatalf("expected RelativeDockerfile=Dockerfile, got %q", plan.RelativeDockerfile)
	}
	if plan.ContextRoot != filepath.Clean(source) {
		t.Fatalf("expected context root %q, got %q", filepath.Clean(source), plan.ContextRoot)
	}
	// The Dockerfile the plan points at must actually exist in the archive.
	if _, err := os.Stat(plan.DockerfilePath); err != nil {
		t.Fatalf("resolved Dockerfile does not exist in archive: %v", err)
	}
}
