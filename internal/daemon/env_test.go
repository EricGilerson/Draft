package daemon

import (
	"bytes"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"

	"Draft/internal/store"
)

func TestEnvGetSetUsesStore(t *testing.T) {
	srv, st, _ := newTestServer(t)

	dir := t.TempDir()
	st.DB.Create(&store.Project{ID: 1, Name: "p", Path: dir})
	st.DB.Create(&store.CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	// GET empty
	body, _ := json.Marshal(map[string]string{"nodeId": "n1"})
	req := httptest.NewRequest("POST", "/env", bytes.NewReader(body))
	req.Header.Set("X-Draft-Token", srv.state.Token)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("get status %d", w.Code)
	}

	// SET
	body, _ = json.Marshal(map[string]string{"nodeId": "n1", "key": "FOO", "value": "bar"})
	req = httptest.NewRequest("POST", "/env/set", bytes.NewReader(body))
	req.Header.Set("X-Draft-Token", srv.state.Token)
	w = httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("set status %d", w.Code)
	}

	vars, err := st.ListEnvVars("n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vars) != 1 || vars[0].Key != "FOO" || vars[0].Value != "bar" || vars[0].Source != store.EnvSourceManual {
		t.Fatalf("vars = %+v", vars)
	}
	if _, err := os.Stat(filepath.Join(dir, ".env")); !os.IsNotExist(err) {
		t.Fatalf("SetEnvVar should not write .env, stat err = %v", err)
	}
}

func TestEnvImportRefreshExport(t *testing.T) {
	srv, st, _ := newTestServer(t)

	dir := t.TempDir()
	envPath := filepath.Join(dir, ".env")
	if err := os.WriteFile(envPath, []byte("FOO=file\nBAR=baz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	st.DB.Create(&store.Project{ID: 1, Name: "p", Path: dir})
	st.DB.Create(&store.CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	body, _ := json.Marshal(map[string]string{"nodeId": "n1", "path": envPath})
	req := httptest.NewRequest("POST", "/env/import", bytes.NewReader(body))
	req.Header.Set("X-Draft-Token", srv.state.Token)
	w := httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("import status %d body %s", w.Code, w.Body.String())
	}

	vars, err := st.ListEnvVars("n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(vars) != 2 {
		t.Fatalf("vars after import = %+v", vars)
	}

	if err := st.SetEnvVar("n1", "FOO", "manual"); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(envPath, []byte("FOO=file-updated\nBAR=baz\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	body, _ = json.Marshal(map[string]string{"nodeId": "n1"})
	req = httptest.NewRequest("POST", "/env/refresh", bytes.NewReader(body))
	req.Header.Set("X-Draft-Token", srv.state.Token)
	w = httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("refresh status %d body %s", w.Code, w.Body.String())
	}
	var result store.EnvFileSyncResult
	if err := json.NewDecoder(w.Body).Decode(&result); err != nil {
		t.Fatal(err)
	}
	if len(result.Conflicts) != 1 {
		t.Fatalf("refresh result = %+v, want conflict", result)
	}

	req = httptest.NewRequest("POST", "/env/export", bytes.NewReader(body))
	req.Header.Set("X-Draft-Token", srv.state.Token)
	w = httptest.NewRecorder()
	srv.routes().ServeHTTP(w, req)
	if w.Code != 200 {
		t.Fatalf("export status %d body %s", w.Code, w.Body.String())
	}
	data, err := os.ReadFile(envPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(data, []byte("FOO=manual")) || !bytes.Contains(data, []byte("BAR=baz")) {
		t.Fatalf("exported .env = %s", data)
	}
}
