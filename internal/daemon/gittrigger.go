package daemon

import (
	"bufio"
	"context"
	"io"
	"log"
	"os"
	"os/exec"
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
	Event string   `json:"event"` // "on_commit" | "on_push"
	Refs  []gitRef `json:"refs"`
}

const (
	eventOnCommit = "on_commit"
	eventOnPush   = "on_push"
)

// FireHook is the body of `draft --git-hook`: it gathers what the git event
// touched and hands it to the daemon, starting the daemon first if it isn't
// running. It is designed to return quickly so it never stalls the user's git
// command — in the cold-start case it launches the daemon and returns without
// waiting, relying on the daemon's startup reconciliation to catch up.
func FireHook(ctx context.Context, repoPath, gitEvent string) error {
	req := recheckRequest{Repo: repoPath}
	switch gitEvent {
	case "post-commit":
		req.Event = eventOnCommit
		// post-commit carries no ref data; the commit landed on HEAD's branch.
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

// deliverRecheck sends the payload to a running daemon, or launches one if none
// is up. When it has to launch, it does not wait for readiness — startup
// reconciliation handles the just-fired event.
func deliverRecheck(ctx context.Context, req recheckRequest) error {
	if c, err := NewClientFromState(); err == nil {
		pingCtx, cancel := context.WithTimeout(ctx, 800*time.Millisecond)
		pingErr := c.Ping(pingCtx)
		cancel()
		if pingErr == nil {
			return c.postJSON(ctx, "/hooks/recheck", req, nil)
		}
	}
	// No live daemon: start it detached and return immediately.
	return launchDaemon()
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
// commits/pushes of the branch each node tracks — not for unrelated branches.
//
// With refs (a live hook payload), matching is exact: a node deploys only if
// its tracked branch is among the refs the event touched, and the pushed/
// committed sha differs from the last deployed sha. Without refs (a startup
// reconcile), the daemon derives the relevant sha from repository state: the
// local branch tip for on_commit, the remote-tracking tip for on_push.
func (s *Server) reconcileGitTriggers(ctx context.Context, req recheckRequest) {
	project, err := s.store.GetProjectByPath(req.Repo)
	if err != nil || project == nil {
		return
	}
	if !gitsrc.IsRepo(project.Path) {
		return
	}
	nodes, err := s.store.ListNodes(project.ID)
	if err != nil {
		return
	}
	remotes := gitRemotes(ctx, project.Path)

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
		trigger := strings.TrimSpace(settings["deploy_trigger"])
		branch := strings.TrimSpace(settings["git_branch"])
		if branch == "" || trigger != req.Event {
			continue
		}
		tracked := normalizeBranch(branch, remotes)

		var candidateSHA string
		if len(req.Refs) > 0 {
			// Hook payload: the tracked branch must be among the touched refs.
			sha, ok := payload[tracked]
			if !ok {
				continue
			}
			candidateSHA = sha
		} else {
			// Startup reconcile: derive the sha from repo state.
			ref := "refs/heads/" + tracked
			if req.Event == eventOnPush {
				ref = gitsrc.UpstreamRef(ctx, project.Path, tracked)
			}
			sha, err := gitsrc.ResolveSHA(ctx, project.Path, ref)
			if err != nil {
				continue
			}
			candidateSHA = sha
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
	projects, err := s.store.ListProjects()
	if err != nil {
		return
	}
	for _, p := range projects {
		if !gitsrc.IsRepo(p.Path) {
			continue
		}
		nodes, err := s.store.ListNodes(p.ID)
		if err != nil {
			continue
		}
		// Fire a payload-less reconcile per event kind present on the project.
		seen := map[string]bool{}
		for _, node := range nodes {
			settings, err := s.store.GetNodeSettings(node.ID)
			if err != nil {
				continue
			}
			trigger := strings.TrimSpace(settings["deploy_trigger"])
			if (trigger == eventOnCommit || trigger == eventOnPush) && !seen[trigger] {
				seen[trigger] = true
				s.reconcileGitTriggers(ctx, recheckRequest{Repo: p.Path, Event: trigger})
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
