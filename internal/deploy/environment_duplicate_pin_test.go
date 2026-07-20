package deploy

import (
	"context"
	"path/filepath"
	"testing"

	"Draft/internal/store"
)

func TestDuplicateEnvironmentPinsGitBranchOnCopies(t *testing.T) {
	repo := initGitRepo(t, t.TempDir())
	s, err := store.Open(store.FileDSN(filepath.Join(t.TempDir(), "draft.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProject("dup-pin", repo, "")
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
	if err := s.SetNodeSetting("api", "git_branch", "main"); err != nil {
		t.Fatal(err)
	}
	e := New(s, nil, t.TempDir(), func(string, any) {})

	root, err := s.ResolveGitRepoRoot(context.Background(), "api", p.ID)
	if err != nil {
		t.Fatal(err)
	}

	res, err := e.DuplicateEnvironmentWithChoices(context.Background(), source.ID, "feature-env", nil, false, []SandboxRepositoryRef{{
		RepoRoot: root,
		Ref:      "feature/sandbox-src",
	}})
	if err != nil {
		t.Fatalf("DuplicateEnvironmentWithChoices: %v", err)
	}
	targets, err := s.ListNodesByEnvironment(res.Environment.ID)
	if err != nil || len(targets) != 1 {
		t.Fatalf("targets = %+v, %v", targets, err)
	}
	branch, err := s.GetNodeSetting(targets[0].ID, "git_branch")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "feature/sandbox-src" {
		t.Fatalf("copied git_branch = %q, want feature/sandbox-src", branch)
	}

	srcBranch, err := s.GetNodeSetting("api", "git_branch")
	if err != nil {
		t.Fatal(err)
	}
	if srcBranch != "main" {
		t.Fatalf("source git_branch mutated: %q", srcBranch)
	}
}

func TestDuplicateEnvironmentKeepsSourcePinWhenRepositoriesOmitted(t *testing.T) {
	repo := initGitRepo(t, t.TempDir())
	s, err := store.Open(store.FileDSN(filepath.Join(t.TempDir(), "draft.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProject("dup-keep", repo, "")
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
	if err := s.SetNodeSetting("api", "git_branch", "main"); err != nil {
		t.Fatal(err)
	}
	e := New(s, nil, t.TempDir(), func(string, any) {})

	res, err := e.DuplicateEnvironmentWithChoices(context.Background(), source.ID, "copy", nil, false, nil)
	if err != nil {
		t.Fatalf("DuplicateEnvironmentWithChoices: %v", err)
	}
	targets, err := s.ListNodesByEnvironment(res.Environment.ID)
	if err != nil || len(targets) != 1 {
		t.Fatalf("targets = %+v, %v", targets, err)
	}
	branch, err := s.GetNodeSetting(targets[0].ID, "git_branch")
	if err != nil {
		t.Fatal(err)
	}
	if branch != "main" {
		t.Fatalf("copied git_branch = %q, want main (kept from source)", branch)
	}
}

func TestDuplicateEnvironmentSkipsPinOnSharedAlias(t *testing.T) {
	repo := initGitRepo(t, t.TempDir())
	s, err := store.Open(store.FileDSN(filepath.Join(t.TempDir(), "draft.db")))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	p, err := s.CreateProject("dup-share-pin", repo, "")
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
	if err := s.SetNodeSetting("api", "git_branch", "main"); err != nil {
		t.Fatal(err)
	}
	e := New(s, nil, t.TempDir(), func(string, any) {})

	root, err := s.ResolveGitRepoRoot(context.Background(), "api", p.ID)
	if err != nil {
		t.Fatal(err)
	}

	res, err := e.DuplicateEnvironmentWithChoices(context.Background(), source.ID, "shared", []ServiceDataChoice{{
		SourceNodeID: "api",
		Mode:         ServiceDataShare,
	}}, false, []SandboxRepositoryRef{{
		RepoRoot: root,
		Ref:      "feature/sandbox-src",
	}})
	if err != nil {
		t.Fatalf("DuplicateEnvironmentWithChoices: %v", err)
	}
	targets, err := s.ListNodesByEnvironment(res.Environment.ID)
	if err != nil || len(targets) != 1 {
		t.Fatalf("targets = %+v, %v", targets, err)
	}
	link, err := e.GetServiceLink(targets[0].ID)
	if err != nil || link == nil {
		t.Fatalf("expected shared alias, link=%+v err=%v", link, err)
	}
	branch, err := s.GetNodeSetting(targets[0].ID, "git_branch")
	if err != nil {
		t.Fatal(err)
	}
	// Share copies settings then links; pin must not apply the branch override.
	if branch != "main" {
		t.Fatalf("shared alias git_branch = %q, want copied source pin main", branch)
	}
}
