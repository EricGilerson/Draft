package gitsrc

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"Draft/internal/executil"
)

// ErrSubmoduleObjectsMissing is returned when a submodule gitlink's commit is
// not available in the superproject's .git/modules cache, so a worktree +
// submodule update (or fetch) is required before the tree can be archived.
var ErrSubmoduleObjectsMissing = errors.New("submodule commit not available locally")

// Gitlink is a submodule pointer recorded in a tree (mode 160000).
type Gitlink struct {
	Path string // slash-separated path relative to the treeish root
	SHA  string // commit SHA the superproject pins
}

// ListGitlinks returns submodule gitlinks under treeish (a bare ref or
// "ref:subdir"). Paths are relative to that treeish root.
func ListGitlinks(ctx context.Context, repoPath, treeish string) ([]Gitlink, error) {
	gitDir, err := resolveGitDir(ctx, repoPath)
	if err != nil {
		return nil, err
	}
	return listGitlinksAt(ctx, gitDir, treeish)
}

func listGitlinksAt(ctx context.Context, gitDir, treeish string) ([]Gitlink, error) {
	cmd := executil.CommandContext(ctx, "git", "--git-dir", gitDir, "ls-tree", "-r", treeish)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("list gitlinks %s: %s", treeish, msg)
	}

	var links []Gitlink
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		// e.g. "160000 commit abcdef...	vendor/child"
		parts := strings.SplitN(line, "\t", 2)
		if len(parts) != 2 {
			continue
		}
		meta := strings.Fields(parts[0])
		if len(meta) < 3 || meta[0] != "160000" {
			continue
		}
		path := filepath.ToSlash(parts[1])
		if path == "" {
			continue
		}
		links = append(links, Gitlink{Path: path, SHA: meta[2]})
	}
	return links, nil
}

// HasGitlinks reports whether treeish contains any submodule gitlinks.
func HasGitlinks(ctx context.Context, repoPath, treeish string) (bool, error) {
	links, err := ListGitlinks(ctx, repoPath, treeish)
	if err != nil {
		return false, err
	}
	return len(links) > 0, nil
}

// SubmoduleGitDir returns the on-disk git directory for a submodule path under
// the superproject at repoPath (typically <git-dir>/modules/<path>).
func SubmoduleGitDir(ctx context.Context, repoPath, subPath string) (string, error) {
	gitDir, err := resolveGitDir(ctx, repoPath)
	if err != nil {
		return "", err
	}
	subPath = filepath.ToSlash(strings.Trim(subPath, "/"))
	if subPath == "" || strings.Contains(subPath, "..") {
		return "", fmt.Errorf("invalid submodule path %q", subPath)
	}
	candidate := filepath.Join(gitDir, "modules", filepath.FromSlash(subPath))
	if st, err := os.Stat(candidate); err == nil && st.IsDir() {
		return candidate, nil
	}
	return "", fmt.Errorf("%w: no module git dir for %s", ErrSubmoduleObjectsMissing, subPath)
}

// HasCommit reports whether sha resolves to a commit in the given git directory.
func HasCommit(ctx context.Context, gitDir, sha string) bool {
	sha = strings.TrimSpace(sha)
	if sha == "" || gitDir == "" {
		return false
	}
	cmd := executil.CommandContext(ctx, "git", "--git-dir", gitDir, "cat-file", "-e", sha+"^{commit}")
	return cmd.Run() == nil
}

// SubmoduleObjectsAvailable reports whether every gitlink under treeish
// (recursively) has its commit available in the local modules cache.
//
// modulePathPrefix is the repo-root-relative path of treeish when treeish is a
// subtree (e.g. "svc" for "main:svc"), used to locate .git/modules/<full-path>.
// Pass "" when treeish is a bare ref at the repository root.
func SubmoduleObjectsAvailable(ctx context.Context, repoPath, treeish, modulePathPrefix string) (bool, error) {
	gitDir, err := resolveGitDir(ctx, repoPath)
	if err != nil {
		return false, err
	}
	return submoduleObjectsAvailableAt(ctx, repoPath, gitDir, treeish, modulePathPrefix)
}

func submoduleObjectsAvailableAt(ctx context.Context, worktreeOrGitDir, gitDir, treeish, modulePathPrefix string) (bool, error) {
	links, err := listGitlinksAt(ctx, gitDir, treeish)
	if err != nil {
		return false, err
	}
	for _, link := range links {
		modDir, err := SubmoduleGitDir(ctx, worktreeOrGitDir, moduleLookupPath(modulePathPrefix, link.Path))
		if err != nil {
			if errors.Is(err, ErrSubmoduleObjectsMissing) {
				return false, nil
			}
			return false, err
		}
		if !HasCommit(ctx, modDir, link.SHA) {
			return false, nil
		}
		// Nested modules live under this module's git dir; no path prefix.
		ok, err := submoduleObjectsAvailableAt(ctx, modDir, modDir, link.SHA, "")
		if err != nil {
			return false, err
		}
		if !ok {
			return false, nil
		}
	}
	return true, nil
}

func moduleLookupPath(prefix, linkPath string) string {
	prefix = strings.Trim(filepath.ToSlash(prefix), "/")
	linkPath = strings.Trim(filepath.ToSlash(linkPath), "/")
	if prefix == "" || prefix == "." {
		return linkPath
	}
	if linkPath == "" {
		return prefix
	}
	return prefix + "/" + linkPath
}

// ArchiveSubmoduleToDir exports sha from the submodule git directory into destDir.
func ArchiveSubmoduleToDir(ctx context.Context, gitDir, sha, destDir string) error {
	if err := os.MkdirAll(destDir, 0o755); err != nil {
		return err
	}
	if !HasCommit(ctx, gitDir, sha) {
		return fmt.Errorf("%w: %s", ErrSubmoduleObjectsMissing, sha)
	}

	tarFile, err := os.CreateTemp("", "draft-submodule-archive-*.tar")
	if err != nil {
		return fmt.Errorf("create archive temp file: %w", err)
	}
	tarPath := tarFile.Name()
	defer os.Remove(tarPath)
	defer tarFile.Close()

	cmd := executil.CommandContext(ctx, "git", "--git-dir", gitDir, "archive", "--format=tar", sha)
	cmd.Stdout = tarFile
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git archive submodule %s: %s", sha, msg)
	}
	if _, err := tarFile.Seek(0, io.SeekStart); err != nil {
		return fmt.Errorf("rewind archive: %w", err)
	}
	if err := extractTar(tarFile, destDir); err != nil {
		return fmt.Errorf("extract submodule archive: %w", err)
	}
	return nil
}

// MaterializeSubmodules expands every gitlink under treeish into destDir at the
// recorded SHAs, recursively. destDir must already contain the parent tree
// (typically from ArchiveToDir). Returns ErrSubmoduleObjectsMissing when a
// commit is not present in .git/modules.
//
// modulePathPrefix is the repo-root-relative path of treeish when treeish is a
// subtree (e.g. "svc" for "main:svc"). Pass "" for a bare ref at the repo root.
func MaterializeSubmodules(ctx context.Context, repoPath, treeish, destDir, modulePathPrefix string) error {
	gitDir, err := resolveGitDir(ctx, repoPath)
	if err != nil {
		return err
	}
	return materializeSubmodulesAt(ctx, repoPath, gitDir, treeish, destDir, modulePathPrefix)
}

func materializeSubmodulesAt(ctx context.Context, worktreeOrGitDir, gitDir, treeish, destDir, modulePathPrefix string) error {
	links, err := listGitlinksAt(ctx, gitDir, treeish)
	if err != nil {
		return err
	}
	for _, link := range links {
		modDir, err := SubmoduleGitDir(ctx, worktreeOrGitDir, moduleLookupPath(modulePathPrefix, link.Path))
		if err != nil {
			return err
		}
		if !HasCommit(ctx, modDir, link.SHA) {
			return fmt.Errorf("%w: %s @ %s", ErrSubmoduleObjectsMissing, link.Path, link.SHA)
		}
		subDest := filepath.Join(destDir, filepath.FromSlash(link.Path))
		// Plain git archive leaves an empty placeholder dir for the gitlink;
		// replace it with the real tree.
		_ = os.RemoveAll(subDest)
		if err := ArchiveSubmoduleToDir(ctx, modDir, link.SHA, subDest); err != nil {
			return fmt.Errorf("materialize submodule %s: %w", link.Path, err)
		}
		if err := materializeSubmodulesAt(ctx, modDir, modDir, link.SHA, subDest, ""); err != nil {
			return err
		}
	}
	return nil
}

// WriteArchiveWithSubmodules writes a tar archive of treeish with submodule
// contents spliced in at the recorded SHAs. Requires all submodule objects to
// be available locally (see SubmoduleObjectsAvailable).
//
// modulePathPrefix is the repo-root-relative path of treeish when treeish is a
// subtree (e.g. "svc" for "main:svc"). Pass "" for a bare ref at the repo root.
func WriteArchiveWithSubmodules(ctx context.Context, repoPath, treeish, modulePathPrefix string, w io.Writer) error {
	gitDir, err := resolveGitDir(ctx, repoPath)
	if err != nil {
		return err
	}
	ok, err := submoduleObjectsAvailableAt(ctx, repoPath, gitDir, treeish, modulePathPrefix)
	if err != nil {
		return err
	}
	if !ok {
		return ErrSubmoduleObjectsMissing
	}

	tw := tar.NewWriter(w)
	defer tw.Close()
	return writeArchiveWithSubmodulesAt(ctx, repoPath, gitDir, treeish, "", modulePathPrefix, tw)
}

func writeArchiveWithSubmodulesAt(ctx context.Context, worktreeOrGitDir, gitDir, treeish, prefix, modulePathPrefix string, tw *tar.Writer) error {
	links, err := listGitlinksAt(ctx, gitDir, treeish)
	if err != nil {
		return err
	}
	linkSet := make(map[string]Gitlink, len(links))
	for _, link := range links {
		linkSet[link.Path] = link
	}

	tarFile, err := os.CreateTemp("", "draft-git-archive-*.tar")
	if err != nil {
		return fmt.Errorf("create archive temp file: %w", err)
	}
	tarPath := tarFile.Name()
	defer os.Remove(tarPath)
	defer tarFile.Close()

	cmd := executil.CommandContext(ctx, "git", "--git-dir", gitDir, "archive", "--format=tar", treeish)
	cmd.Stdout = tarFile
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	if err := cmd.Run(); err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return fmt.Errorf("git archive %s: %s", treeish, msg)
	}
	if _, err := tarFile.Seek(0, io.SeekStart); err != nil {
		return err
	}

	tr := tar.NewReader(tarFile)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			return err
		}
		name := strings.TrimSuffix(filepath.ToSlash(hdr.Name), "/")
		// Drop empty placeholder dirs/files for gitlinks; real content follows.
		if _, isLink := linkSet[name]; isLink {
			_, _ = io.Copy(io.Discard, tr)
			continue
		}
		outName := name
		if prefix != "" {
			outName = prefix + "/" + name
		}
		outHdr := *hdr
		outHdr.Name = outName
		if err := tw.WriteHeader(&outHdr); err != nil {
			return err
		}
		if hdr.Typeflag == tar.TypeReg {
			if _, err := io.Copy(tw, tr); err != nil {
				return err
			}
		}
	}

	for _, link := range links {
		modDir, err := SubmoduleGitDir(ctx, worktreeOrGitDir, moduleLookupPath(modulePathPrefix, link.Path))
		if err != nil {
			return err
		}
		subPrefix := link.Path
		if prefix != "" {
			subPrefix = prefix + "/" + link.Path
		}
		// Ensure the directory exists in the archive.
		if err := tw.WriteHeader(&tar.Header{
			Name:     subPrefix + "/",
			Typeflag: tar.TypeDir,
			Mode:     0o755,
		}); err != nil {
			return err
		}
		if err := writeArchiveWithSubmodulesAt(ctx, modDir, modDir, link.SHA, subPrefix, "", tw); err != nil {
			return fmt.Errorf("archive submodule %s: %w", link.Path, err)
		}
	}
	return nil
}

// CheckoutWithSubmodules creates a detached worktree at ref and runs
// `git submodule update --init --recursive` so submodule trees match the
// recorded gitlinks. The parent working tree is not modified. The caller must
// invoke cleanup when finished (removes the worktree registration and directory).
func CheckoutWithSubmodules(ctx context.Context, repoPath, ref string) (worktreePath string, cleanup func(), err error) {
	if !IsRepo(repoPath) {
		return "", nil, ErrNotRepo
	}
	ref = strings.TrimSpace(ref)
	if i := strings.IndexByte(ref, ':'); i >= 0 {
		ref = ref[:i]
	}
	if err := VerifyRef(ctx, repoPath, ref); err != nil {
		return "", nil, err
	}

	wtDir, err := os.MkdirTemp("", "draft-wt-")
	if err != nil {
		return "", nil, fmt.Errorf("create worktree dir: %w", err)
	}
	// worktree add requires the destination to not exist.
	if err := os.RemoveAll(wtDir); err != nil {
		return "", nil, err
	}

	cleanup = func() {
		cmd := executil.Command("git", "-C", repoPath, "worktree", "remove", "--force", wtDir)
		_ = cmd.Run()
		_ = os.RemoveAll(wtDir)
	}

	addCmd := executil.CommandContext(ctx, "git", "-C", repoPath, "worktree", "add", "--detach", wtDir, ref)
	var addErr bytes.Buffer
	addCmd.Stderr = &addErr
	if err := addCmd.Run(); err != nil {
		cleanup()
		msg := strings.TrimSpace(addErr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", nil, fmt.Errorf("git worktree add: %s", msg)
	}

	subCmd := executil.CommandContext(ctx, "git", "-C", wtDir, "submodule", "update", "--init", "--recursive")
	var subErr bytes.Buffer
	subCmd.Stderr = &subErr
	subCmd.Stdout = &subErr
	if err := subCmd.Run(); err != nil {
		cleanup()
		msg := strings.TrimSpace(subErr.String())
		if msg == "" {
			msg = err.Error()
		}
		return "", nil, fmt.Errorf("git submodule update: %s", msg)
	}

	return wtDir, cleanup, nil
}

func resolveGitDir(ctx context.Context, path string) (string, error) {
	cmd := executil.CommandContext(ctx, "git", "-C", path, "rev-parse", "--absolute-git-dir")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		// Bare module directories sometimes need --git-dir=. when -C is the git dir.
		cmd2 := executil.CommandContext(ctx, "git", "--git-dir", path, "rev-parse", "--absolute-git-dir")
		var stderr2 bytes.Buffer
		cmd2.Stderr = &stderr2
		out2, err2 := cmd2.Output()
		if err2 != nil {
			msg := strings.TrimSpace(stderr.String())
			if msg == "" {
				msg = err.Error()
			}
			if strings.Contains(msg, "not a git repository") {
				return "", ErrNotRepo
			}
			return "", fmt.Errorf("resolve git dir: %s", msg)
		}
		return filepath.Clean(strings.TrimSpace(string(out2))), nil
	}
	return filepath.Clean(strings.TrimSpace(string(out))), nil
}
