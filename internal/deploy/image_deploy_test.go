package deploy

import "testing"

func TestRenderPullLine(t *testing.T) {
	cases := []struct {
		status, id, progress, want string
	}{
		{"Pulling fs layer", "abc123", "", "    abc123: Pulling fs layer"},
		{"Downloading", "abc123", "[>                                                  ] 1 B/2 MB", "    abc123: Downloading [>                                                  ] 1 B/2 MB"},
		{"Status: Image is up to date", "", "", "Status: Image is up to date"},
		{"Digest: sha256:xyz", "", "", "Digest: sha256:xyz"},
	}
	for _, c := range cases {
		got := renderPullLine(c.status, c.id, c.progress)
		if got != c.want {
			t.Errorf("renderPullLine(%q,%q,%q) = %q, want %q", c.status, c.id, c.progress, got, c.want)
		}
	}
}
