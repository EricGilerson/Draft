package store

import (
	"context"
	"path/filepath"
	"strings"

	"Draft/internal/gitsrc"
)

const gitRepoRootSettingKey = "git_repo_root"

// CachedGitRepoRoot returns the last resolved repo root cached for the node, or
// "" when no cache exists yet.
func (s *Store) CachedGitRepoRoot(nodeID string) (string, error) {
	root, err := s.GetNodeSetting(nodeID, gitRepoRootSettingKey)
	if err != nil {
		return "", nil
	}
	root = strings.TrimSpace(root)
	if root == "" {
		return "", nil
	}
	return filepath.Clean(root), nil
}

// SetCachedGitRepoRoot stores the resolved repo root for the node. Passing ""
// clears the cache.
func (s *Store) SetCachedGitRepoRoot(nodeID, repoRoot string) error {
	return s.SetNodeSetting(nodeID, gitRepoRootSettingKey, strings.TrimSpace(repoRoot))
}

// ResolveGitRepoRoot resolves which git repository a node should use. It
// prefers the node's service root when present, falling back to the project
// path when the service root is unset or not in a repo.
func (s *Store) ResolveGitRepoRoot(ctx context.Context, nodeID string, projectID uint) (string, error) {
	project, err := s.GetProject(projectID)
	if err != nil {
		return "", err
	}

	serviceRoot, err := s.GetServiceRoot(nodeID, projectID)
	if err != nil {
		return "", err
	}
	candidate := strings.TrimSpace(serviceRoot)
	if candidate == "" {
		candidate = project.Path
	}

	root, err := gitsrc.RepoRoot(ctx, candidate)
	if err == nil {
		_ = s.SetCachedGitRepoRoot(nodeID, root)
		return root, nil
	}
	if err != gitsrc.ErrNotRepo || candidate == project.Path {
		_ = s.SetCachedGitRepoRoot(nodeID, "")
		return "", err
	}

	root, err = gitsrc.RepoRoot(ctx, project.Path)
	if err != nil {
		_ = s.SetCachedGitRepoRoot(nodeID, "")
		return "", err
	}
	_ = s.SetCachedGitRepoRoot(nodeID, root)
	return root, nil
}
