package draftmcp

import (
	"testing"

	"Draft/internal/store"
)

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

func TestRedactEnvVarStructs(t *testing.T) {
	vars := []store.EnvVar{
		{Key: "PLAIN", Value: "ok", Secret: false},
		{Key: "DB_PASSWORD", Value: "hunter2", Secret: true},
	}
	out := redactSecrets(vars, false).([]any)
	if len(out) != 2 {
		t.Fatalf("len = %d", len(out))
	}
	plain := out[0].(map[string]any)
	secret := out[1].(map[string]any)
	if plain["value"] != "ok" {
		t.Fatalf("plain redacted: %#v", plain)
	}
	if secret["value"] != secretMask {
		t.Fatalf("secret not masked: %#v", secret)
	}
}

func TestRedactAppSecrets(t *testing.T) {
	secrets := []store.AppSecret{{Key: "API_KEY", Value: "super-secret", Description: "test"}}
	out := redactSecrets(secrets, false).([]any)
	row := out[0].(map[string]any)
	if row["value"] != secretMask {
		t.Fatalf("app secret not masked: %#v", row)
	}
	kept := redactSecrets(secrets, true).([]store.AppSecret)
	if kept[0].Value != "super-secret" {
		t.Fatalf("includeSecrets failed: %#v", kept[0])
	}
}

func TestRequireConfirm(t *testing.T) {
	if err := requireConfirm(map[string]any{}); err == nil {
		t.Fatal("expected confirmation error")
	}
	if err := requireConfirm(map[string]any{"confirm": false}); err == nil {
		t.Fatal("confirm:false should error")
	}
	if err := requireConfirm(map[string]any{"confirm": true}); err != nil {
		t.Fatal(err)
	}
}
