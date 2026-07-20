package gitsrc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strconv"
	"strings"

	"Draft/internal/executil"
)

// PullRequest is a local-view of a GitHub pull request discovered via the
// GitHub CLI (`gh`). Draft never talks to GitHub's API directly.
type PullRequest struct {
	Number  int    `json:"number"`
	Title   string `json:"title"`
	HeadRef string `json:"headRef"`
	HeadSHA string `json:"headSha,omitempty"`
	URL     string `json:"url,omitempty"`
	Author  string `json:"author,omitempty"`
}

// PullRequestsStatus reports whether `gh pr list` can be used for repoRoot.
// Available is false when gh is missing, unauthenticated, or the directory is
// not a GitHub repository the CLI can resolve — callers should hide PR UI.
type PullRequestsStatus struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

// CheckPullRequestsAvailable probes whether listing PRs will work for path.
// It is intentionally cheap: no network list, only gh presence + repo view.
func CheckPullRequestsAvailable(ctx context.Context, path string) PullRequestsStatus {
	path = strings.TrimSpace(path)
	if path == "" || !IsRepo(path) {
		return PullRequestsStatus{Available: false, Reason: "not a git repository"}
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return PullRequestsStatus{Available: false, Reason: "GitHub CLI (gh) not found on PATH"}
	}
	// `gh repo view` fails fast when the remote is not GitHub or auth is missing.
	cmd := executil.CommandContext(ctx, "gh", "repo", "view", "--json", "nameWithOwner")
	cmd.Dir = path
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return PullRequestsStatus{Available: false, Reason: compactGHError(msg)}
	}
	return PullRequestsStatus{Available: true}
}

// ListPullRequests returns open pull requests for the GitHub repository that
// owns path, via `gh pr list`. Returns a non-nil empty slice when there are no
// open PRs. Callers should gate on CheckPullRequestsAvailable first.
func ListPullRequests(ctx context.Context, path string, limit int) ([]PullRequest, error) {
	path = strings.TrimSpace(path)
	if path == "" || !IsRepo(path) {
		return nil, ErrNotRepo
	}
	if limit <= 0 {
		limit = 30
	}
	if limit > 100 {
		limit = 100
	}
	if _, err := exec.LookPath("gh"); err != nil {
		return nil, fmt.Errorf("GitHub CLI (gh) not found on PATH")
	}

	cmd := executil.CommandContext(ctx, "gh", "pr", "list",
		"--state", "open",
		"--limit", fmt.Sprintf("%d", limit),
		"--json", "number,title,headRefName,headRefOid,url,author",
	)
	cmd.Dir = path
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("list pull requests: %s", compactGHError(msg))
	}

	var raw []struct {
		Number      int    `json:"number"`
		Title       string `json:"title"`
		HeadRefName string `json:"headRefName"`
		HeadRefOid  string `json:"headRefOid"`
		URL         string `json:"url"`
		Author      struct {
			Login string `json:"login"`
		} `json:"author"`
	}
	if err := json.Unmarshal(out, &raw); err != nil {
		return nil, fmt.Errorf("parse pull requests: %w", err)
	}
	prs := make([]PullRequest, 0, len(raw))
	for _, row := range raw {
		if row.Number <= 0 {
			continue
		}
		prs = append(prs, PullRequest{
			Number:  row.Number,
			Title:   strings.TrimSpace(row.Title),
			HeadRef: strings.TrimSpace(row.HeadRefName),
			HeadSHA: strings.TrimSpace(row.HeadRefOid),
			URL:     strings.TrimSpace(row.URL),
			Author:  strings.TrimSpace(row.Author.Login),
		})
	}
	return prs, nil
}

func compactGHError(msg string) string {
	msg = strings.TrimSpace(msg)
	// Keep the first line; gh often dumps multi-line help.
	if i := strings.IndexByte(msg, '\n'); i >= 0 {
		msg = strings.TrimSpace(msg[:i])
	}
	if len(msg) > 240 {
		msg = msg[:240] + "…"
	}
	return msg
}

// PreferDeployableRef returns a git ref that resolves in the local object store
// for the given human branch/PR head name. PR head branches from forks often
// exist only on the remote (or only as pull/N/head), so a bare headRefName
// fails `git rev-parse` even though the PR is valid.
//
// Order: bare ref → PreferLocalRef → <remote>/<branch> for each remote.
// Returns the original ref unchanged when nothing resolves (caller may still
// fall back to an explicit SHA or EnsurePullRequestRef).
func PreferDeployableRef(ctx context.Context, path, ref string) string {
	ref = strings.TrimSpace(ref)
	if ref == "" || !IsRepo(path) {
		return ref
	}
	if _, err := ResolveSHA(ctx, path, ref); err == nil {
		return PreferLocalRef(ctx, path, ref)
	}
	local := PreferLocalRef(ctx, path, ref)
	if local != ref {
		if _, err := ResolveSHA(ctx, path, local); err == nil {
			return local
		}
	}
	// Strip a leading remote/ if present so we can re-try under each remote.
	branch := ref
	for _, remote := range repoRemotes(ctx, path) {
		prefix := remote + "/"
		if strings.HasPrefix(ref, prefix) {
			branch = strings.TrimPrefix(ref, prefix)
			break
		}
	}
	for _, remote := range repoRemotes(ctx, path) {
		candidate := remote + "/" + branch
		if _, err := ResolveSHA(ctx, path, candidate); err == nil {
			return candidate
		}
	}
	return ref
}

// EnsurePullRequestRef fetches refs/pull/<n>/head into
// refs/remotes/<remote>/pr/<n> so sandbox services can pin a stable, tip-
// following ref that works for fork PRs (where headRefName is not a local
// branch). Returns the short ref name suitable for git_branch (e.g. origin/pr/12).
func EnsurePullRequestRef(ctx context.Context, path string, prNumber int) (string, error) {
	path = strings.TrimSpace(path)
	if path == "" || !IsRepo(path) {
		return "", ErrNotRepo
	}
	if prNumber <= 0 {
		return "", fmt.Errorf("invalid pull request number %d", prNumber)
	}
	remotes := repoRemotes(ctx, path)
	remote := "origin"
	if len(remotes) > 0 {
		remote = remotes[0]
	}
	shortRef := fmt.Sprintf("%s/pr/%d", remote, prNumber)
	// Destination must be under refs/remotes so it behaves like a remote-tracking branch.
	dest := fmt.Sprintf("refs/remotes/%s/pr/%d", remote, prNumber)
	src := fmt.Sprintf("pull/%d/head", prNumber)

	// Already have it?
	if _, err := ResolveSHA(ctx, path, shortRef); err == nil {
		return shortRef, nil
	}

	cmd := executil.CommandContext(ctx, "git", "-C", path, "fetch", "--no-tags", remote, src+":"+dest)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", fmt.Errorf("fetch pull/%d/head: %s", prNumber, compactGHError(msg))
	}
	if _, err := ResolveSHA(ctx, path, shortRef); err != nil {
		return "", fmt.Errorf("fetch pull/%d/head succeeded but %s is not resolvable", prNumber, shortRef)
	}
	return shortRef, nil
}

// ParsePRNumber extracts a pull request number from common freeform tokens
// ("412", "pr:412", "pr-412", "pull/412", "origin/pr/412"). Returns 0 when
// not a PR token.
func ParsePRNumber(value string) int {
	value = strings.TrimSpace(strings.ToLower(value))
	if value == "" {
		return 0
	}
	// origin/pr/12 or remotes/origin/pr/12 (what EnsurePullRequestRef stores)
	if i := strings.LastIndex(value, "/pr/"); i >= 0 {
		value = value[i+len("/pr/"):]
	} else {
		for _, prefix := range []string{"pr:", "pr-", "pull/", "pull:"} {
			if strings.HasPrefix(value, prefix) {
				value = strings.TrimSpace(value[len(prefix):])
				break
			}
		}
	}
	// Strip any trailing path junk.
	if i := strings.IndexAny(value, "/\\ \t"); i >= 0 {
		value = value[:i]
	}
	n, err := strconv.Atoi(value)
	if err != nil || n <= 0 {
		return 0
	}
	return n
}
