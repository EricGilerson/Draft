package daemon

import (
	"strings"
	"testing"

	"Draft/internal/store"
)

func TestParsePrePush(t *testing.T) {
	// Real pre-push stdin: "<local ref> <local sha> <remote ref> <remote sha>".
	in := strings.NewReader(
		"refs/heads/main aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa refs/heads/main bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb\n" +
			"refs/heads/dead 0000000000000000000000000000000000000000 refs/heads/dead cccccccccccccccccccccccccccccccccccccccc\n" +
			"refs/heads/feature/x 1111111111111111111111111111111111111111 refs/heads/feature/x 0000000000000000000000000000000000000000\n",
	)
	refs := parsePrePush(in)
	if len(refs) != 2 {
		t.Fatalf("expected 2 refs (deletion skipped), got %d: %+v", len(refs), refs)
	}
	if refs[0].Name != "main" || refs[0].SHA != "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa" {
		t.Fatalf("ref[0] = %+v", refs[0])
	}
	if refs[1].Name != "feature/x" {
		t.Fatalf("ref[1] name = %q, want feature/x", refs[1].Name)
	}
}

func TestNormalizeBranch(t *testing.T) {
	remotes := []string{"origin", "upstream"}
	cases := map[string]string{
		"main":                    "main",
		"origin/main":             "main",
		"upstream/dev":            "dev",
		"refs/heads/main":         "main",
		"refs/remotes/origin/foo": "foo",
		"feature/x":               "feature/x", // slash that isn't a remote prefix
		"origin/feature/x":        "feature/x",
	}
	for in, want := range cases {
		if got := normalizeBranch(in, remotes); got != want {
			t.Errorf("normalizeBranch(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestShouldDeploy(t *testing.T) {
	s, err := store.Open(store.MemoryDSN())
	if err != nil {
		t.Fatalf("open store: %v", err)
	}
	defer s.Close()
	srv := &Server{store: s}

	const node = "n1"

	// No prior deployment → any non-empty sha should deploy.
	if !srv.shouldDeploy(node, "abc123") {
		t.Fatalf("expected first deploy to proceed")
	}
	// Empty candidate never deploys.
	if srv.shouldDeploy(node, "") {
		t.Fatalf("empty candidate must not deploy")
	}

	if _, err := s.CreateDeployment(&store.Deployment{NodeID: node, Status: "running", SourceSHA: "abc123"}); err != nil {
		t.Fatalf("create deployment: %v", err)
	}

	// Same sha as last deploy → no redeploy.
	if srv.shouldDeploy(node, "abc123") {
		t.Fatalf("same sha must not redeploy")
	}
	// Moved on → deploy.
	if !srv.shouldDeploy(node, "def456") {
		t.Fatalf("changed sha must deploy")
	}
}
