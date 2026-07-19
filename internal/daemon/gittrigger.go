package daemon

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"Draft/internal/gitsrc"
)

// gitRef is a branch name paired with the commit it points at, as reported by a
// git hook (a committed branch, or a ref being pushed).
type gitRef struct {
	Name string `json:"name"` // short branch name, e.g. "main" or "feature/x"
	SHA  string `json:"sha"`
}

// recheckRequest is the payload a git hook POSTs to /hooks/recheck. Refs is the
// set of branches the event touched; when empty (e.g. a startup reconcile) the
// daemon derives the relevant sha from repository state instead.
type recheckRequest struct {
	Repo  string   `json:"repo"`
	Event string   `json:"event"` // "on_commit" | "on_push" | "on_pull"
	Refs  []gitRef `json:"refs"`
}

const (
	eventOnCommit = "on_commit"
	eventOnPush   = "on_push"
	eventOnPull   = "on_pull"
)

// FireHook is the body of `draft --git-hook`: it gathers what the git event
// touched and hands it to the daemon, starting the daemon first if it isn't
// running. It returns quickly so it never stalls the user's git command. On
// cold start the payload is spooled to disk and drained when the daemon boots.
func FireHook(ctx context.Context, repoPath, gitEvent string) error {
	req := recheckRequest{Repo: repoPath}
	switch gitEvent {
	case "post-commit":
		req.Event = eventOnCommit
		branch := gitOutput(ctx, repoPath, "symbolic-ref", "--quiet", "--short", "HEAD")
		sha := gitOutput(ctx, repoPath, "rev-parse", "HEAD")
		if branch != "" && sha != "" {
			req.Refs = append(req.Refs, gitRef{Name: branch, SHA: sha})
		}
	case "post-merge", "post-rewrite":
		// post-merge: merge-based pull / merge. post-rewrite: rebase pull / rebase / amend.
		// Both land a new HEAD tip we should redeploy against when redeploy_on_pull is on.
		req.Event = eventOnPull
		branch := gitOutput(ctx, repoPath, "symbolic-ref", "--quiet", "--short", "HEAD")
		sha := gitOutput(ctx, repoPath, "rev-parse", "HEAD")
		if branch != "" && sha != "" {
			req.Refs = append(req.Refs, gitRef{Name: branch, SHA: sha})
		}
	case "pre-push":
		req.Event = eventOnPush
		req.Refs = parsePrePush(os.Stdin)
	default:
		return nil
	}
	return deliverRecheck(ctx, req)
}

// deliverRecheck sends the payload to a running daemon, or spools it and
// launches a daemon when none is up. Spooling preserves the exact refs from
// the hook so startup does not rely solely on tip reconcile. It is a
// package-level variable so tests can stub daemon delivery.
var deliverRecheck = func(ctx context.Context, req recheckRequest) error {
	if c, err := NewClientFromState(); err == nil {
		pingCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
		pingErr := c.Ping(pingCtx)
		cancel()
		if pingErr == nil {
			return c.postJSON(ctx, "/hooks/recheck", req, nil)
		}
	}
	if err := spoolRecheck(req); err != nil {
		log.Printf("[git-hook] spool recheck: %v", err)
	}
	return launchDaemon()
}

func pendingRecheckDir() (string, error) {
	cfg, err := ConfigDir()
	if err != nil {
		return "", err
	}
	dir := filepath.Join(cfg, "pending-rechecks")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	return dir, nil
}

func spoolRecheck(req recheckRequest) error {
	dir, err := pendingRecheckDir()
	if err != nil {
		return err
	}
	data, err := json.Marshal(req)
	if err != nil {
		return err
	}
	name := fmt.Sprintf("%d.json", time.Now().UnixNano())
	return os.WriteFile(filepath.Join(dir, name), data, 0o600)
}

// drainPendingRechecks applies any hook payloads spooled while the daemon was
// down, then removes them. Safe to call on every startup.
func (s *Server) drainPendingRechecks(ctx context.Context) {
	dir, err := pendingRecheckDir()
	if err != nil {
		return
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return
	}
	for _, ent := range entries {
		if ent.IsDir() || !strings.HasSuffix(ent.Name(), ".json") {
			continue
		}
		path := filepath.Join(dir, ent.Name())
		data, err := os.ReadFile(path)
		if err != nil {
			continue
		}
		var req recheckRequest
		if err := json.Unmarshal(data, &req); err != nil {
			_ = os.Remove(path)
			continue
		}
		log.Printf("[git-trigger] draining spooled %s for %s (%d refs)", req.Event, req.Repo, len(req.Refs))
		s.reconcileGitTriggers(ctx, req)
		_ = os.Remove(path)
	}
}

// parsePrePush reads the ref lines git feeds a pre-push hook on stdin. Each line
// is "<local-ref> <local-sha> <remote-ref> <remote-sha>". Deletions (local-sha
// all zeros) are skipped; only what's actually being pushed is reported.
func parsePrePush(r io.Reader) []gitRef {
	var refs []gitRef
	sc := bufio.NewScanner(r)
	for sc.Scan() {
		fields := strings.Fields(sc.Text())
		if len(fields) < 2 {
			continue
		}
		localRef, localSHA := fields[0], fields[1]
		if localSHA == "" || strings.Trim(localSHA, "0") == "" {
			continue // branch deletion
		}
		name := strings.TrimPrefix(localRef, "refs/heads/")
		refs = append(refs, gitRef{Name: name, SHA: localSHA})
	}
	return refs
}

func gitOutput(ctx context.Context, repoPath string, args ...string) string {
	full := append([]string{"-C", repoPath}, args...)
	out, err := exec.CommandContext(ctx, "git", full...).Output()
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(out))
}

// gitRemotes returns the configured remote names for the repo (e.g. "origin"),
// used to normalize a pinned "origin/main" ref down to the branch "main".
func gitRemotes(ctx context.Context, repoPath string) []string {
	out := gitOutput(ctx, repoPath, "remote")
	if out == "" {
		return nil
	}
	return strings.Split(out, "\n")
}

// normalizeBranch reduces any ref form to a bare branch name so a pinned
// "origin/main" (or "refs/heads/main") and a pushed "refs/heads/main" compare
// equal.
func normalizeBranch(name string, remotes []string) string {
	name = strings.TrimPrefix(name, "refs/heads/")
	name = strings.TrimPrefix(name, "refs/remotes/")
	for _, r := range remotes {
		if r != "" && strings.HasPrefix(name, r+"/") {
			return name[len(r)+1:]
		}
	}
	return name
}

// reconcileGitTriggers evaluates every node whose git trigger matches the event
// and deploys the ones whose tracked branch actually moved past the commit we
// last built. It is the single gate that ensures we deploy only for genuine
// commits/pushes/pulls of the branch each node tracks — not for unrelated
// branches.
//
// Commit and push subscribe via the 3-way deploy_trigger setting; pull
// subscribes via the independent redeploy_on_pull flag (orthogonal to
// deploy_trigger). With refs (a live hook payload), matching is exact: a node
// deploys only if its tracked branch is among the refs the event touched, and
// the pushed/committed/merged sha differs from the last deployed sha. Without
// refs (a startup reconcile), the daemon derives the relevant sha from
// repository state: the local branch tip for on_commit and on_pull, the
// remote-tracking tip for on_push.
func (s *Server) reconcileGitTriggers(ctx context.Context, req recheckRequest) {
	if !gitsrc.IsRepo(req.Repo) {
		return
	}
	nodes, err := s.store.NodesByRepoRoot(ctx, req.Repo)
	if err != nil || len(nodes) == 0 {
		return
	}
	remotes := gitRemotes(ctx, req.Repo)

	// Index the payload refs by normalized branch name for exact matching.
	payload := make(map[string]string, len(req.Refs))
	for _, ref := range req.Refs {
		payload[normalizeBranch(ref.Name, remotes)] = ref.SHA
	}

	for _, node := range nodes {
		settings, err := s.store.GetNodeSettings(node.ID)
		if err != nil {
			continue
		}
		tracked, candidateSHA, ok := s.matchNode(ctx, req.Repo, req, settings, payload, remotes)
		if !ok {
			continue
		}
		if !s.shouldDeploy(node.ID, candidateSHA) {
			continue
		}
		log.Printf("[git-trigger] %s: branch %q at %s → deploying node %s", req.Event, tracked, short(candidateSHA), node.ID)
		if err := s.engine.Deploy(ctx, node.ID); err != nil {
			log.Printf("[git-trigger] deploy %s: %v", node.ID, err)
		}
	}
}

// nodeSubscribes reports whether a node opts into the event being reconciled.
// Commit and push subscribe via the 3-way deploy_trigger; pull subscribes via
// the independent redeploy_on_pull flag, which is orthogonal to deploy_trigger.
func nodeSubscribes(event, trigger, redeployOnPull string) bool {
	switch event {
	case eventOnPull:
		return strings.TrimSpace(redeployOnPull) == "true"
	default:
		return strings.TrimSpace(trigger) == event
	}
}

// matchNode evaluates a single node against a reconcile request. It returns the
// normalized tracked branch, the candidate sha to compare against the node's
// last deployment, and ok=false when the node does not subscribe to the event,
// has no pinned branch, or its tracked branch was not touched by the event.
func (s *Server) matchNode(ctx context.Context, repoRoot string, req recheckRequest, settings map[string]string, payload map[string]string, remotes []string) (tracked, candidateSHA string, ok bool) {
	branch := strings.TrimSpace(settings["git_branch"])
	if branch == "" {
		return "", "", false
	}
	trigger := strings.TrimSpace(settings["deploy_trigger"])
	if !nodeSubscribes(req.Event, trigger, settings["redeploy_on_pull"]) {
		return "", "", false
	}
	tracked = normalizeBranch(branch, remotes)

	if len(req.Refs) > 0 {
		// Hook payload: the tracked branch must be among the touched refs.
		sha, hit := payload[tracked]
		if !hit {
			return tracked, "", false
		}
		return tracked, sha, true
	}
	// Startup reconcile: derive the sha from repo state. Pull (like commit)
	// follows the local branch tip — a pull updates the local branch, not just
	// the remote-tracking ref.
	ref := "refs/heads/" + tracked
	if req.Event == eventOnPush {
		ref = gitsrc.UpstreamRef(ctx, repoRoot, tracked)
	}
	sha, err := gitsrc.ResolveSHA(ctx, repoRoot, ref)
	if err != nil {
		return tracked, "", false
	}
	return tracked, sha, true
}

// shouldDeploy reports whether candidateSHA differs from the commit the node was
// last deployed from. An empty candidate never deploys; a node never deployed
// from git deploys on the first matching event.
func (s *Server) shouldDeploy(nodeID, candidateSHA string) bool {
	if candidateSHA == "" {
		return false
	}
	last, err := s.store.LatestDeployment(nodeID)
	if err != nil {
		return false
	}
	if last == nil {
		return true
	}
	return last.SourceSHA != candidateSHA
}

// reconcileAllGitTriggersOnStartup re-evaluates every project's git-triggered
// nodes once when the daemon boots, catching commits/pushes that happened while
// the daemon was down (including the very event whose hook just launched it).
func (s *Server) reconcileAllGitTriggersOnStartup(ctx context.Context) {
	// Prefer exact spooled hook payloads first (refs from the cold-start fire).
	s.drainPendingRechecks(ctx)

	projects, err := s.store.ListProjects()
	if err != nil {
		return
	}
	seen := map[string]bool{}
	for _, p := range projects {
		nodes, err := s.store.ListNodes(p.ID)
		if err != nil {
			continue
		}
		for _, node := range nodes {
			settings, err := s.store.GetNodeSettings(node.ID)
			if err != nil {
				continue
			}
			repoRoot, err := s.store.ResolveGitRepoRoot(ctx, node.ID, p.ID)
			if err != nil || repoRoot == "" || !gitsrc.IsRepo(repoRoot) {
				continue
			}
			trigger := strings.TrimSpace(settings["deploy_trigger"])
			if trigger == eventOnCommit || trigger == eventOnPush {
				key := repoRoot + "|" + trigger
				if !seen[key] {
					seen[key] = true
					s.reconcileGitTriggers(ctx, recheckRequest{Repo: repoRoot, Event: trigger})
				}
			}
			// Pull is independent of deploy_trigger; fire it once if any node
			// in the project opted into redeploy-on-pull.
			if strings.TrimSpace(settings["redeploy_on_pull"]) == "true" {
				key := repoRoot + "|" + eventOnPull
				if !seen[key] {
					seen[key] = true
					s.reconcileGitTriggers(ctx, recheckRequest{Repo: repoRoot, Event: eventOnPull})
				}
			}
		}
	}
}

func short(sha string) string {
	if len(sha) > 8 {
		return sha[:8]
	}
	return sha
}
