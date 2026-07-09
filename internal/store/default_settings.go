package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// ParseDefaultSettings decodes a template's DefaultSettings JSON object into
// a map of node_setting key → value. Empty input yields an empty map.
func ParseDefaultSettings(raw string) (map[string]string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return map[string]string{}, nil
	}
	var out map[string]string
	if err := json.Unmarshal([]byte(raw), &out); err != nil {
		return nil, fmt.Errorf("template default settings are not valid JSON: %w", err)
	}
	if out == nil {
		return map[string]string{}, nil
	}
	return out, nil
}

// EncodeDefaultSettings serializes a settings map for ServiceTemplate.DefaultSettings.
func EncodeDefaultSettings(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		return ""
	}
	return string(b)
}

// NormalizeDefaultSettings validates and re-encodes a template DefaultSettings
// JSON object. Empty input stays empty. Rejects non-object JSON.
func NormalizeDefaultSettings(raw string) (string, error) {
	m, err := ParseDefaultSettings(raw)
	if err != nil {
		return "", err
	}
	return EncodeDefaultSettings(m), nil
}

// MustEncodeDefaultSettings panics on marshal failure (for package-level literals).
func MustEncodeDefaultSettings(m map[string]string) string {
	if len(m) == 0 {
		return ""
	}
	b, err := json.Marshal(m)
	if err != nil {
		panic(err)
	}
	return string(b)
}
