package store

import (
	"encoding/json"
	"strings"
)

// ParseImageTags decodes a template's ImageTags JSON into a string slice. An
// empty string yields an empty list; malformed JSON is an error so a bad
// template can't ship a broken tag list into the wizard.
func ParseImageTags(raw string) ([]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var tags []string
	if err := json.Unmarshal([]byte(raw), &tags); err != nil {
		return nil, err
	}
	return tags, nil
}

// NormalizeImageTags trims, drops blanks, and dedupes (case-sensitive) a
// tag list, returning the JSON encoding suitable for storing on a template.
// Order is preserved so the template author's intended "first = default"
// ordering survives.
func NormalizeImageTags(raw string) (string, error) {
	tags, err := ParseImageTags(raw)
	if err != nil {
		return "", err
	}
	out := make([]string, 0, len(tags))
	seen := make(map[string]bool, len(tags))
	for _, t := range tags {
		t = strings.TrimSpace(t)
		if t == "" || seen[t] {
			continue
		}
		seen[t] = true
		out = append(out, t)
	}
	if len(out) == 0 {
		return "", nil
	}
	enc, err := json.Marshal(out)
	if err != nil {
		return "", err
	}
	return string(enc), nil
}

// SplitImageRef separates an image reference into its base name and tag. The
// tag is the portion after the last ':' that follows the last '/'; if no such
// ':' exists the tag is "latest" (Docker's implicit default). This handles
// "postgres:16-alpine", "library/nginx:1.27", and
// "myregistry.io:5000/postgres:16" correctly. The base is returned without
// the tag so callers can compose base+tag from a curated list.
func SplitImageRef(ref string) (base, tag string) {
	ref = strings.TrimSpace(ref)
	if ref == "" {
		return "", ""
	}
	if i := strings.LastIndex(ref, "/"); i >= 0 {
		head := ref[:i+1]
		tail := ref[i+1:]
		if j := strings.LastIndex(tail, ":"); j >= 0 {
			return head + tail[:j], tail[j+1:]
		}
		return head + tail, "latest"
	}
	if j := strings.LastIndex(ref, ":"); j >= 0 {
		return ref[:j], ref[j+1:]
	}
	return ref, "latest"
}
