package draftmcp

import "testing"

func TestRedactSecrets(t *testing.T) {
	input := map[string]any{
		"plain":    "visible",
		"token":    "hidden",
		"database": map[string]any{"secret": true, "value": "password"},
	}
	out := redactSecrets(input, false).(map[string]any)
	if out["plain"] != "visible" || out["token"] != secretMask {
		t.Fatalf("unexpected redaction: %#v", out)
	}
	nested := out["database"].(map[string]any)
	if nested["value"] != secretMask {
		t.Fatalf("nested secret not masked: %#v", nested)
	}
	if got := redactSecrets(input, true).(map[string]any)["token"]; got != "hidden" {
		t.Fatalf("includeSecrets changed value: %v", got)
	}
}

func TestRequireConfirm(t *testing.T) {
	if err := requireConfirm(map[string]any{}); err == nil {
		t.Fatal("expected confirmation error")
	}
	if err := requireConfirm(map[string]any{"confirm": true}); err != nil {
		t.Fatal(err)
	}
}
