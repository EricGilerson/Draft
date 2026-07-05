package store

import "testing"

func TestParseImageTags(t *testing.T) {
	cases := []struct {
		raw  string
		want []string
	}{
		{"", nil},
		{"   ", nil},
		{`["16-alpine","16","latest"]`, []string{"16-alpine", "16", "latest"}},
		{`[]`, []string{}},
	}
	for _, c := range cases {
		got, err := ParseImageTags(c.raw)
		if err != nil {
			t.Errorf("ParseImageTags(%q) error: %v", c.raw, err)
			continue
		}
		if len(got) != len(c.want) {
			t.Errorf("ParseImageTags(%q) = %v, want %v", c.raw, got, c.want)
			continue
		}
		for i := range got {
			if got[i] != c.want[i] {
				t.Errorf("ParseImageTags(%q)[%d] = %q, want %q", c.raw, i, got[i], c.want[i])
			}
		}
	}
	if _, err := ParseImageTags(`{not json`); err == nil {
		t.Error("ParseImageTags should reject malformed JSON")
	}
}

func TestNormalizeImageTagsDedupesAndPreservesOrder(t *testing.T) {
	got, err := NormalizeImageTags(`["16-alpine"," 16 ","","16-alpine","15-alpine"]`)
	if err != nil {
		t.Fatalf("NormalizeImageTags: %v", err)
	}
	want := `["16-alpine","16","15-alpine"]`
	if got != want {
		t.Errorf("NormalizeImageTags = %q, want %q", got, want)
	}

	empty, err := NormalizeImageTags(`["","  "]`)
	if err != nil {
		t.Fatalf("NormalizeImageTags empty: %v", err)
	}
	if empty != "" {
		t.Errorf("NormalizeImageTags all-blank = %q, want empty", empty)
	}
}

func TestSplitImageRef(t *testing.T) {
	cases := []struct {
		ref  string
		base string
		tag  string
	}{
		{"postgres:16-alpine", "postgres", "16-alpine"},
		{"postgres", "postgres", "latest"},
		{"library/nginx:1.27", "library/nginx", "1.27"},
		{"myregistry.io:5000/postgres:16", "myregistry.io:5000/postgres", "16"},
		{"", "", ""},
	}
	for _, c := range cases {
		base, tag := SplitImageRef(c.ref)
		if base != c.base || tag != c.tag {
			t.Errorf("SplitImageRef(%q) = (%q,%q), want (%q,%q)", c.ref, base, tag, c.base, c.tag)
		}
	}
}
