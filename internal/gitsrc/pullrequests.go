package gitsrc

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"os/exec"
	"strings"
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
	cmd := exec.CommandContext(ctx, "gh", "repo", "view", "--json", "nameWithOwner")
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

	cmd := exec.CommandContext(ctx, "gh", "pr", "list",
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
