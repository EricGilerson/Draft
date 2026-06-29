package envfile

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadKeepsMultilineJSONValues(t *testing.T) {
	path := filepath.Join(t.TempDir(), ".env")
	content := `PLAIN=ok
SERVICE_ACCOUNT_JSON={
  "type": "service_account",
  "private_key": "-----BEGIN PRIVATE KEY-----\nabc=def\n-----END PRIVATE KEY-----\n",
  "client_email": "svc@example.com"
}
AFTER=done
`
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	values, err := Read(path)
	if err != nil {
		t.Fatal(err)
	}
	if values["PLAIN"] != "ok" || values["AFTER"] != "done" {
		t.Fatalf("simple values = %+v", values)
	}
	got := values["SERVICE_ACCOUNT_JSON"]
	if !strings.Contains(got, `"private_key"`) || !strings.Contains(got, `abc=def`) || !strings.HasSuffix(strings.TrimSpace(got), "}") {
		t.Fatalf("json value was not preserved: %q", got)
	}
	if _, ok := values[`"private_key": "-----BEGIN PRIVATE KEY-----\nabc`]; ok {
		t.Fatal("json body line was parsed as an env key")
	}
}
