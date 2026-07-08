package store

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func openRepoTestStore(t *testing.T) *Store {
	t.Helper()
	s, err := Open(FileDSN(filepath.Join(t.TempDir(), "test.db")))
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func runGit(t *testing.T, dir string, args ...string) {
	t.Helper()
	cmd := exec.Command("git", append([]string{"-C", dir}, args...)...)
	cmd.Env = append(os.Environ(),
		"GIT_AUTHOR_NAME=Test", "GIT_AUTHOR_EMAIL=test@example.com",
		"GIT_COMMITTER_NAME=Test", "GIT_COMMITTER_EMAIL=test@example.com",
	)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git %s: %v\n%s", strings.Join(args, " "), err, out)
	}
}

func initRepo(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("mkdir repo: %v", err)
	}
	runGit(t, dir, "init", "-b", "main", "-q")
	runGit(t, dir, "config", "core.autocrlf", "false")
	if err := os.WriteFile(filepath.Join(dir, "README.md"), []byte("hello\n"), 0o644); err != nil {
		t.Fatalf("write README: %v", err)
	}
	runGit(t, dir, "add", ".")
	runGit(t, dir, "commit", "-q", "-m", "initial")
}

func canonicalTestPath(t *testing.T, path string) string {
	t.Helper()
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil {
		return filepath.Clean(path)
	}
	return filepath.Clean(resolved)
}

func createProjectNode(t *testing.T, s *Store, projectPath string) (*Project, *CanvasNode) {
	t.Helper()
	project, err := s.CreateProject("proj-"+filepath.Base(projectPath), projectPath, "")
	if err != nil {
		t.Fatalf("CreateProject: %v", err)
	}
	env, err := s.GetDefaultEnvironment(project.ID)
	if err != nil {
		t.Fatalf("GetDefaultEnvironment: %v", err)
	}
	node, err := s.CreateNode(&CanvasNode{ID: "node-1", ProjectID: project.ID, EnvironmentID: env.ID, Label: "api"})
	if err != nil {
		t.Fatalf("CreateNode: %v", err)
	}
	return project, node
}

func TestResolveGitRepoRoot_PrefersNestedServiceRepo(t *testing.T) {
	s := openRepoTestStore(t)
	projectPath := filepath.Join(t.TempDir(), "workspace")
	serviceRoot := filepath.Join(projectPath, "services", "api")
	initRepo(t, serviceRoot)

	project, node := createProjectNode(t, s, projectPath)
	if err := s.SetServiceRoot(node.ID, project.ID, serviceRoot); err != nil {
		t.Fatalf("SetServiceRoot: %v", err)
	}

	root, err := s.ResolveGitRepoRoot(context.Background(), node.ID, project.ID)
	if err != nil {
		t.Fatalf("ResolveGitRepoRoot: %v", err)
	}
	if root != canonicalTestPath(t, serviceRoot) {
		t.Fatalf("ResolveGitRepoRoot = %q, want %q", root, canonicalTestPath(t, serviceRoot))
	}

	cached, err := s.CachedGitRepoRoot(node.ID)
	if err != nil {
		t.Fatalf("CachedGitRepoRoot: %v", err)
	}
	if cached != canonicalTestPath(t, serviceRoot) {
		t.Fatalf("CachedGitRepoRoot = %q, want %q", cached, canonicalTestPath(t, serviceRoot))
	}
}

func TestResolveGitRepoRoot_FallsBackToProjectRepoWhenServiceRootNotRepo(t *testing.T) {
	s := openRepoTestStore(t)
	projectPath := filepath.Join(t.TempDir(), "workspace")
	initRepo(t, projectPath)
	serviceRoot := filepath.Join(projectPath, "services", "api")
	if err := os.MkdirAll(serviceRoot, 0o755); err != nil {
		t.Fatalf("mkdir service root: %v", err)
	}

	project, node := createProjectNode(t, s, projectPath)
	if err := s.SetServiceRoot(node.ID, project.ID, serviceRoot); err != nil {
		t.Fatalf("SetServiceRoot: %v", err)
	}

	root, err := s.ResolveGitRepoRoot(context.Background(), node.ID, project.ID)
	if err != nil {
		t.Fatalf("ResolveGitRepoRoot: %v", err)
	}
	if root != canonicalTestPath(t, projectPath) {
		t.Fatalf("ResolveGitRepoRoot = %q, want %q", root, canonicalTestPath(t, projectPath))
	}
}

func TestResolveGitRepoRoot_FallsBackToProjectRepoWhenServiceRootEmpty(t *testing.T) {
	s := openRepoTestStore(t)
	projectPath := filepath.Join(t.TempDir(), "workspace")
	initRepo(t, projectPath)

	project, node := createProjectNode(t, s, projectPath)
	root, err := s.ResolveGitRepoRoot(context.Background(), node.ID, project.ID)
	if err != nil {
		t.Fatalf("ResolveGitRepoRoot: %v", err)
	}
	if root != canonicalTestPath(t, projectPath) {
		t.Fatalf("ResolveGitRepoRoot = %q, want %q", root, canonicalTestPath(t, projectPath))
	}
}

func TestResolveGitRepoRoot_NotRepo(t *testing.T) {
	s := openRepoTestStore(t)
	projectPath := filepath.Join(t.TempDir(), "workspace")
	serviceRoot := filepath.Join(projectPath, "services", "api")
	if err := os.MkdirAll(serviceRoot, 0o755); err != nil {
		t.Fatalf("mkdir service root: %v", err)
	}

	project, node := createProjectNode(t, s, projectPath)
	if err := s.SetServiceRoot(node.ID, project.ID, serviceRoot); err != nil {
		t.Fatalf("SetServiceRoot: %v", err)
	}

	if _, err := s.ResolveGitRepoRoot(context.Background(), node.ID, project.ID); err == nil {
		t.Fatalf("expected error for non-repo project and service root")
	}

	cached, err := s.CachedGitRepoRoot(node.ID)
	if err != nil {
		t.Fatalf("CachedGitRepoRoot: %v", err)
	}
	if cached != "" {
		t.Fatalf("expected empty cache after failed resolve, got %q", cached)
	}
}

func TestNodesByRepoRoot_FindsNestedRepoNodes(t *testing.T) {
	s := openRepoTestStore(t)
	projectPath := filepath.Join(t.TempDir(), "workspace")
	serviceRoot := filepath.Join(projectPath, "services", "api")
	initRepo(t, serviceRoot)

	project, node := createProjectNode(t, s, projectPath)
	if err := s.SetServiceRoot(node.ID, project.ID, serviceRoot); err != nil {
		t.Fatalf("SetServiceRoot: %v", err)
	}
	if _, err := s.ResolveGitRepoRoot(context.Background(), node.ID, project.ID); err != nil {
		t.Fatalf("ResolveGitRepoRoot: %v", err)
	}

	matches, err := s.NodesByRepoRoot(context.Background(), canonicalTestPath(t, serviceRoot))
	if err != nil {
		t.Fatalf("NodesByRepoRoot: %v", err)
	}
	if len(matches) != 1 || matches[0].ID != node.ID {
		t.Fatalf("NodesByRepoRoot returned %+v, want node %q", matches, node.ID)
	}
}
