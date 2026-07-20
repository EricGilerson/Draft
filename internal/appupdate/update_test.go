package appupdate

import "testing"

func TestCompareVersion(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.2.0", "1.1.9", 1},
		{"v1.2.0", "1.2.0", 0},
		{"1.2.0", "1.2.1", -1},
		{"1.10.0", "1.9.9", 1},
	}
	for _, tc := range cases {
		got := compareVersion(tc.a, tc.b)
		if (got > 0) != (tc.want > 0) || (got < 0) != (tc.want < 0) {
			t.Fatalf("compareVersion(%q, %q) = %d, want sign %d", tc.a, tc.b, got, tc.want)
		}
	}
}

func TestAssetURL(t *testing.T) {
	assets := []Asset{{Name: "one", URL: "https://example.test/one"}}
	if got := assetURL(assets, "one"); got != "https://example.test/one" {
		t.Fatalf("assetURL = %q", got)
	}
	if got := assetURL(assets, "missing"); got != "" {
		t.Fatalf("missing asset = %q", got)
	}
}
