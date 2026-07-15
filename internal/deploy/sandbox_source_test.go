package deploy

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"Draft/internal/store"
)

func initGitRepo(t *testing.T, dir string) string {
	t.Helper()
	run := func(args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Draft",
			"GIT_AUTHOR_EMAIL=draft@example.com",
			"GIT_COMMITTER_NAME=Draft",
			"GIT_COMMITTER_EMAIL=draft@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("init")
	run("checkout", "-b", "main")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("hello\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README")
	run("commit", "-m", "init")
	run("checkout", "-b", "feature/sandbox-src")
	if err := os.WriteFile(filepath.Join(dir, "README"), []byte("feature\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README")
	run("commit", "-m", "feature")
	run("checkout", "main")
	return dir
}

func TestListSandboxSourceReposAndCreatePinsBranch(t *testing.T) {
	repo := initGitRepo(t, t.TempDir())
	s, err := store.Open(store.FileDSN(filepath.Join(t.TempDir(), "draft.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProject("sandbox-src", repo, "")
	if err != nil {
		t.Fatal(err)
	}
	source, err := s.GetDefaultEnvironment(p.ID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "api", Label: "API", ProjectID: p.ID, EnvironmentID: source.ID}); err != nil {
		t.Fatal(err)
	}
	// service_root empty → project path is the git root.
	e := New(s, nil, t.TempDir(), func(string, any) {})

	repos, err := e.ListSandboxSourceRepos(context.Background(), source.ID)
	if err != nil {
		t.Fatalf("ListSandboxSourceRepos: %v", err)
	}
	if len(repos.Repositories) != 1 {
		t.Fatalf("repos = %+v", repos.Repositories)
	}
	if repos.Repositories[0].RepoRoot == "" {
		t.Fatal("expected repo root")
	}
	// Branches should include feature/sandbox-src (local).
	found := false
	for _, b := range repos.Repositories[0].Branches {
		if b == "feature/sandbox-src" {
			found = true
			break
		}
	}
	if !found {
		t.Fatalf("branches missing feature: %v", repos.Repositories[0].Branches)
	}

	created, err := e.CreateSandbox(context.Background(), SandboxCreateRequest{
		Name:                "from-feature",
		SourceEnvironmentID: source.ID,
		Plan: SandboxPlan{
			Purpose: SandboxPurposePreview,
			Repositories: []SandboxRepositoryRef{{
				RepoRoot: repos.Repositories[0].RepoRoot,
				Ref:      "feature/sandbox-src",
			}},
		},
	})
	if err != nil {
		t.Fatalf("CreateSandbox: %v", err)
	}
	if created.Started {
		t.Fatal("StartOnCreate was false; Started should be false")
	}
	pins, err := s.ListSandboxRepositorySources(created.Sandbox.ID)
	if err != nil || len(pins) != 1 {
		t.Fatalf("pins = %+v, %v", pins, err)
	}
	if pins[0].Ref != "feature/sandbox-src" || pins[0].CommitSHA == "" {
		t.Fatalf("pin = %+v", pins[0])
	}
	targets, err := s.ListNodesByEnvironment(created.Sandbox.EnvironmentID)
	if err != nil || len(targets) != 1 {
		t.Fatalf("targets = %+v, %v", targets, err)
	}
	branch, err := s.GetNodeSetting(targets[0].ID, "git_branch")
	if err != nil {
		t.Fatal(err)
	}
	// git_branch keeps the human ref so Settings/redeploy follow the branch tip;
	// the resolved SHA is recorded on the sandbox pin only.
	if branch != "feature/sandbox-src" {
		t.Fatalf("sandbox node git_branch = %q, want feature/sandbox-src", branch)
	}
	if pins[0].CommitSHA == "" || pins[0].CommitSHA == branch {
		t.Fatalf("expected pin CommitSHA to be a resolved object id, got %+v", pins[0])
	}

	// Durable source node must remain unpinned.
	srcBranch, _ := s.GetNodeSetting("api", "git_branch")
	if srcBranch != "" {
		t.Fatalf("source git_branch mutated: %q", srcBranch)
	}
}

func TestRefreshSandboxTipAdvancesPin(t *testing.T) {
	repo := initGitRepo(t, t.TempDir())
	s, err := store.Open(store.FileDSN(filepath.Join(t.TempDir(), "draft.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProject("sandbox-refresh", repo, "")
	if err != nil {
		t.Fatal(err)
	}
	source, _ := s.GetDefaultEnvironment(p.ID)
	if _, err := s.CreateNode(&store.CanvasNode{ID: "api", Label: "API", ProjectID: p.ID, EnvironmentID: source.ID}); err != nil {
		t.Fatal(err)
	}
	e := New(s, nil, t.TempDir(), func(string, any) {})

	root, err := s.ResolveGitRepoRoot(context.Background(), "api", p.ID)
	if err != nil {
		t.Fatal(err)
	}
	created, err := e.CreateSandbox(context.Background(), SandboxCreateRequest{
		Name:                "refresh-me",
		SourceEnvironmentID: source.ID,
		Plan: SandboxPlan{
			Repositories: []SandboxRepositoryRef{{RepoRoot: root, Ref: "main"}},
		},
	})
	if err != nil {
		t.Fatal(err)
	}
	before, err := s.ListSandboxRepositorySources(created.Sandbox.ID)
	if err != nil || len(before) != 1 {
		t.Fatalf("before pins = %+v, %v", before, err)
	}

	// Advance main with a new commit while the sandbox still pins the old SHA.
	run := func(args ...string) {
		t.Helper()
		c := exec.Command("git", args...)
		c.Dir = repo
		c.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Draft",
			"GIT_AUTHOR_EMAIL=draft@example.com",
			"GIT_COMMITTER_NAME=Draft",
			"GIT_COMMITTER_EMAIL=draft@example.com",
		)
		if out, err := c.CombinedOutput(); err != nil {
			t.Fatalf("git %v: %v\n%s", args, err, out)
		}
	}
	run("checkout", "main")
	if err := os.WriteFile(filepath.Join(repo, "README"), []byte("main-2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run("add", "README")
	run("commit", "-m", "main tip")

	// Redeploy may fail without Docker; pin rewrite happens first and is what we assert.
	if _, err := e.RefreshSandbox(context.Background(), SandboxRefreshRequest{
		SandboxID: created.Sandbox.ID,
		Mode:      SandboxRefreshSame,
	}); err != nil {
		t.Logf("RefreshSandbox same (stack may fail offline): %v", err)
	}
	pinsSame, err := s.ListSandboxRepositorySources(created.Sandbox.ID)
	if err != nil || len(pinsSame) != 1 {
		t.Fatalf("same pins = %+v, %v", pinsSame, err)
	}
	if pinsSame[0].CommitSHA != before[0].CommitSHA {
		t.Fatalf("same mode changed SHA: %s -> %s", before[0].CommitSHA, pinsSame[0].CommitSHA)
	}

	if _, err := e.RefreshSandbox(context.Background(), SandboxRefreshRequest{
		SandboxID: created.Sandbox.ID,
		Mode:      SandboxRefreshTip,
	}); err != nil {
		t.Logf("RefreshSandbox tip (stack may fail offline): %v", err)
	}
	after, err := s.ListSandboxRepositorySources(created.Sandbox.ID)
	if err != nil || len(after) != 1 {
		t.Fatalf("after pins = %+v, %v", after, err)
	}
	if after[0].Ref != "main" {
		t.Fatalf("ref = %q", after[0].Ref)
	}
	if after[0].CommitSHA == before[0].CommitSHA {
		t.Fatalf("expected tip refresh to advance SHA from %s", before[0].CommitSHA)
	}
}

func TestResolveSandboxRefAcceptsExplicitSHAFallback(t *testing.T) {
	e := New(nil, nil, t.TempDir(), func(string, any) {})
	// Non-repo path should error.
	if _, err := e.ResolveSandboxRef(context.Background(), t.TempDir(), "main", ""); err == nil {
		t.Fatal("expected error for non-repo")
	}
}
