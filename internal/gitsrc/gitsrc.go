// Package gitsrc materializes a git ref (branch, tag, or commit) into a
// plain directory on disk, without touching the caller's working tree.
//
// It is used to deploy a service from a specific branch while leaving
// whatever is currently checked out (including any uncommitted diff)
// completely untouched: everything shells out to `git archive`, which
// reads committed objects straight from the repository's object store.
package gitsrc

import (
	"archive/tar"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// ErrNotRepo is returned when the given path is not inside a git working tree.
var ErrNotRepo = errors.New("not a git repository")

// IsRepo reports whether path is inside a git working tree.
func IsRepo(path string) bool {
	cmd := exec.Command("git", "-C", path, "rev-parse", "--is-inside-work-tree")
	out, err := cmd.Output()
	return err == nil && strings.TrimSpace(string(out)) == "true"
}

// ListBranches returns the local and remote-tracking branch names for the
// repository at path (e.g. "main", "feature/x", "origin/main"). The
// synthetic "origin/HEAD" entry is omitted.
func ListBranches(ctx context.Context, path string) ([]string, error) {
	if !IsRepo(path) {
		return nil, ErrNotRepo
	}

	cmd := exec.CommandContext(ctx, "git", "-C", path, "for-each-ref",
		"--format=%(refname:short)", "refs/heads/", "refs/remotes/")
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	out, err := cmd.Output()
	if err != nil {
		msg := strings.TrimSpace(stderr.String())
		if msg == "" {
			msg = err.Error()
		}
		return nil, fmt.Errorf("list branches: %s", msg)
	}

	var branches []string
	for _, line := range strings.Split(string(out), "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasSuffix(line, "/HEAD") {
			continue
		}
		branches = append(branches, line)
	}
	return branches, nil
}

// VerifyRef checks that ref resolves to a commit in the repository at path.
func VerifyRef(ctx context.Context, path, ref string) error {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("branch or ref %q not found in repository", ref)
	}
	return nil
}

// ResolveSHA returns the full commit SHA that ref resolves to in the repository
// at path. Returns an error if ref does not resolve to a commit (e.g. a branch
// that doesn't exist, or a remote-tracking ref for a branch never pushed).
func ResolveSHA(ctx context.Context, path, ref string) (string, error) {
	if !IsRepo(path) {
		return "", ErrNotRepo
	}
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--verify", "--quiet", ref+"^{commit}")
	out, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("ref %q not found in repository", ref)
	}
	return strings.TrimSpace(string(out)), nil
}

// UpstreamRef returns the remote-tracking ref for the local branch (e.g.
// "origin/main" for "main"), following the branch's configured upstream when
// set. It falls back to "origin/<branch>" when no upstream is configured. The
// returned ref is suitable for ResolveSHA; callers should treat a resolve error
// as "nothing pushed yet" rather than fatal.
func UpstreamRef(ctx context.Context, path, branch string) string {
	cmd := exec.CommandContext(ctx, "git", "-C", path, "rev-parse", "--abbrev-ref", "--symbolic-full-name", branch+"@{upstream}")
	out, err := cmd.Output()
	if up := strings.TrimSpace(string(out)); err == nil && up != "" {
		return up
	}
	return "origin/" + branch
}

// ArchiveToDir exports the tree of ref from the repository at repoPath into
// destDir, which must already exist. It never reads or modifies repoPath's
// working tree or index — only committed content reachable from ref is
// extracted, via `git archive`.
//
// git archive writes the tarball to a temporary file which is then extracted;
// this avoids the OS-pipe buffering deadlocks that arise from streaming a
// child process's stdout while concurrently draining its stderr.
//
// treeish may be a bare ref ("main") or a ref with a sub-path
// ("main:services/api"), in which case only that subtree is exported, rooted
// at destDir.
func ArchiveToDir(ctx context.Context, repoPath, treeish, destDir string) error {
	if !IsRepo(repoPath) {
		return ErrNotRepo
	}
	// A "<ref>:<subdir>" tree-ish resolves to a tree, not a commit; verify the
	// commit-ish part so a bad branch still yields a clear error.
	ref := treeish
	if i := strings.IndexByte(treeish, ':'); i >= 0 {
		ref = treeish[:i]
	}
	if err := VerifyRef(ctx, repoPath, ref); err != nil {
		return err
	}

	tarFile, err := os.CreateTemp("", "draft-git-archive-*.tar")
	if err != nil {
		return fmt.Errorf("create archive temp file: %w", err)
	}
	tarPath := tarFile.Name()
	defer os.Remove(tarPath)
	defer tarFile.Close()

	cmd := exec.CommandContext(ctx, "git", "-C", repoPath, "archive", "--format=tar", treeish)
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
		return fmt.Errorf("rewind archive: %w", err)
	}
	if err := extractTar(tarFile, destDir); err != nil {
		return fmt.Errorf("extract git archive: %w", err)
	}
	return nil
}

func extractTar(r io.Reader, destDir string) error {
	destDir = filepath.Clean(destDir)
	tr := tar.NewReader(r)
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			return nil
		}
		if err != nil {
			return err
		}

		target := filepath.Join(destDir, filepath.FromSlash(hdr.Name))
		if !isWithinDir(destDir, target) {
			return fmt.Errorf("archive entry %q escapes destination directory", hdr.Name)
		}

		switch hdr.Typeflag {
		case tar.TypeDir:
			if err := os.MkdirAll(target, 0o755); err != nil {
				return err
			}
		case tar.TypeReg:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			f, err := os.OpenFile(target, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, os.FileMode(hdr.Mode)&0o777|0o200)
			if err != nil {
				return err
			}
			if _, err := io.Copy(f, tr); err != nil {
				f.Close()
				return err
			}
			f.Close()
		case tar.TypeSymlink:
			if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
				return err
			}
			// Best-effort: symlink creation can fail without extra
			// privileges on Windows. Skip rather than fail the deploy.
			_ = os.Symlink(hdr.Linkname, target)
		default:
			// Skip anything else (device files, fifos, etc.) — not
			// relevant to a source checkout.
		}
	}
}

func isWithinDir(dir, target string) bool {
	dir = filepath.Clean(dir)
	target = filepath.Clean(target)
	if target == dir {
		return true
	}
	return strings.HasPrefix(target, dir+string(filepath.Separator))
}
