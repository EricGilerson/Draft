package deploy

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"Draft/internal/gitsrc"
)

// pinnedGitSource is the resolved on-disk / stream plan for a git-pinned deploy.
type pinnedGitSource struct {
	// SourcePath is the filesystem root used for non-stream builds (ephemeral
	// archive/worktree). Empty when Stream is true.
	SourcePath string
	// DockerfilePath is rebased under SourcePath when a checkout was materialized.
	DockerfilePath string
	// Stream is true when the build should pipe git archive (with submodule
	// splice when needed) straight to Docker.
	Stream bool
	// RepoRoot is the git repository root used for archive/stream commands.
	RepoRoot string
	// Branch is the preferred local ref being built.
	Branch string
	// ContextRel is the build-context path relative to RepoRoot (slash form).
	ContextRel string
	// ModulePrefix is ContextRel when the context is a subtree (for modules lookup).
	ModulePrefix string
	// Treeish is Branch or Branch:ContextRel for ListGitlinks / archive.
	Treeish string
	// HasSubmodules reports whether Treeish contains gitlinks.
	HasSubmodules bool
	// LocalSubmodules is true when all submodule objects are in .git/modules.
	LocalSubmodules bool
	// Mode describes how submodules (if any) will be included.
	// "", "stream-splice", "checkout-materialize", "worktree-update"
	Mode string
	// Cleanup releases ephemeral worktrees / archive dirs. May be nil.
	Cleanup func()
}

// resolvePinnedGitSource decides stream vs checkout vs worktree for a pinned
// git branch, materializing an on-disk source when needed. The parent working
// tree is never modified. Caller must invoke Cleanup when non-nil.
func (e *Engine) resolvePinnedGitSource(ctx context.Context, nodeID, projectPath, repoRoot, gitBranch, dockerfilePath, serviceRoot string, preferStream bool, logFn func(string)) (pinnedGitSource, error) {
	out := pinnedGitSource{
		RepoRoot:       repoRoot,
		Branch:         gitBranch,
		DockerfilePath: dockerfilePath,
		Stream:         preferStream,
	}
	if gitBranch == "" {
		out.SourcePath = projectPath
		out.Stream = false
		return out, nil
	}

	basePlan, err := resolveBuildContextPlan(projectPath, serviceRoot, dockerfilePath)
	if err != nil {
		return out, err
	}
	contextRel, err := filepath.Rel(repoRoot, basePlan.ContextRoot)
	if err != nil {
		return out, err
	}
	contextRel = filepath.ToSlash(contextRel)
	out.ContextRel = contextRel
	out.Treeish = gitBranch
	if contextRel != "" && contextRel != "." {
		out.Treeish = gitBranch + ":" + contextRel
		out.ModulePrefix = contextRel
	}

	links, err := gitsrc.ListGitlinks(ctx, repoRoot, out.Treeish)
	if err != nil {
		return out, err
	}
	out.HasSubmodules = len(links) > 0
	if out.HasSubmodules {
		for _, link := range links {
			logFn(fmt.Sprintf("    Submodule %s @ %s", link.Path, shortSHA(link.SHA)))
		}
		local, err := gitsrc.SubmoduleObjectsAvailable(ctx, repoRoot, out.Treeish, out.ModulePrefix)
		if err != nil {
			return out, err
		}
		out.LocalSubmodules = local
	}

	needCheckout := !preferStream || (out.HasSubmodules && !out.LocalSubmodules)
	if out.HasSubmodules && preferStream && out.LocalSubmodules {
		out.Mode = "stream-splice"
		logFn("    Submodules: splicing from local .git/modules cache into stream")
	}
	if out.HasSubmodules && !out.LocalSubmodules {
		logFn("    Submodules: local module objects missing; using worktree + submodule update")
		out.Stream = false
		needCheckout = true
		out.Mode = "worktree-update"
	}

	if !needCheckout {
		// Stream path: keep project path for plan math; archive comes from git.
		out.SourcePath = projectPath
		out.Stream = true
		if out.Mode == "" {
			out.Mode = "stream"
		}
		return out, nil
	}

	out.Stream = false
	var archiveDir string
	var cleanup func()

	if out.HasSubmodules && !out.LocalSubmodules {
		wt, wtCleanup, wtErr := gitsrc.CheckoutWithSubmodules(ctx, repoRoot, gitBranch)
		if wtErr != nil {
			return out, wtErr
		}
		cleanup = wtCleanup
		archiveDir = wt
		out.Mode = "worktree-update"
		logFn(fmt.Sprintf("    Checked out %q with submodules into an ephemeral worktree", gitBranch))
	} else {
		dir, prepErr := e.prepareGitSource(ctx, nodeID, repoRoot, gitBranch, contextRel)
		if prepErr != nil {
			if out.HasSubmodules && errors.Is(prepErr, gitsrc.ErrSubmoduleObjectsMissing) {
				logFn("    Submodules: archive splice failed; falling back to worktree + submodule update")
				wt, wtCleanup, wtErr := gitsrc.CheckoutWithSubmodules(ctx, repoRoot, gitBranch)
				if wtErr != nil {
					return out, wtErr
				}
				cleanup = wtCleanup
				archiveDir = wt
				out.Mode = "worktree-update"
				logFn(fmt.Sprintf("    Checked out %q with submodules into an ephemeral worktree", gitBranch))
			} else {
				return out, prepErr
			}
		} else {
			archiveDir = dir
			cleanup = func() { os.RemoveAll(archiveDir) }
			if out.HasSubmodules {
				out.Mode = "checkout-materialize"
			} else {
				out.Mode = "checkout"
			}
		}
	}

	out.SourcePath = archiveDir
	out.Cleanup = cleanup
	rebased, err := rebaseUnderSource(projectPath, archiveDir, dockerfilePath)
	if err != nil {
		if cleanup != nil {
			cleanup()
		}
		return out, err
	}
	out.DockerfilePath = rebased
	return out, nil
}

// gitContextTreeish returns the archive tree-ish and module path prefix for a
// build context under repoRoot.
func gitContextTreeish(repoRoot, contextRoot, ref string) (treeish, modulePrefix string, err error) {
	contextRel, err := filepath.Rel(repoRoot, contextRoot)
	if err != nil {
		return "", "", err
	}
	contextRel = filepath.ToSlash(contextRel)
	if contextRel == "" || contextRel == "." {
		return ref, "", nil
	}
	if contextRel == ".." || strings.HasPrefix(contextRel, "../") {
		return "", "", fmt.Errorf("build context %q is outside the repository", contextRoot)
	}
	return ref + ":" + contextRel, contextRel, nil
}
