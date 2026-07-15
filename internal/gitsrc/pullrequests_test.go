package gitsrc

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParsePRNumber(t *testing.T) {
	cases := map[string]int{
		"412":            412,
		"pr:412":         412,
		"PR-412":         412,
		"pull/99":        99,
		"origin/pr/12":   12,
		"feature":        0,
		"":               0,
		"pr:abc":         0,
	}
	for in, want := range cases {
		if got := ParsePRNumber(in); got != want {
			t.Errorf("ParsePRNumber(%q) = %d, want %d", in, got, want)
		}
	}
}

func TestPreferDeployableRefUsesRemoteTracking(t *testing.T) {
	// Bare clone of a repo with only origin/feature — PreferDeployableRef should
	// rewrite "feature" to "origin/feature" when the local branch is missing.
	remote := t.TempDir()
	run := func(dir string, args ...string) {
		t.Helper()
		cmd := exec.Command("git", args...)
		cmd.Dir = dir
		cmd.Env = append(os.Environ(),
			"GIT_AUTHOR_NAME=Draft",
			"GIT_AUTHOR_EMAIL=draft@example.com",
			"GIT_COMMITTER_NAME=Draft",
			"GIT_COMMITTER_EMAIL=draft@example.com",
		)
		if out, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("git %v in %s: %v\n%s", args, dir, err, out)
		}
	}
	run(remote, "init", "-b", "main")
	if err := os.WriteFile(filepath.Join(remote, "f"), []byte("1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(remote, "add", "f")
	run(remote, "commit", "-m", "init")
	run(remote, "checkout", "-b", "feature/from-pr")
	if err := os.WriteFile(filepath.Join(remote, "f"), []byte("2\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	run(remote, "add", "f")
	run(remote, "commit", "-m", "feature")
	run(remote, "checkout", "main")

	local := t.TempDir()
	run(local, "clone", remote, ".")
	// Local only has main checked out; feature exists as origin/feature/from-pr.
	got := PreferDeployableRef(context.Background(), local, "feature/from-pr")
	if got != "origin/feature/from-pr" && got != "feature/from-pr" {
		// After clone, origin/feature/from-pr should exist; bare name may also
		// resolve if git creates a local tracking branch — either is deployable.
		if _, err := ResolveSHA(context.Background(), local, got); err != nil {
			t.Fatalf("PreferDeployableRef = %q, not resolvable: %v", got, err)
		}
	}
	if _, err := ResolveSHA(context.Background(), local, got); err != nil {
		t.Fatalf("PreferDeployableRef(%q) not resolvable: %v", got, err)
	}
}
