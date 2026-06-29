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

func TestEnvGetSet(t *testing.T) {
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

	// verify file
	b, err := os.ReadFile(filepath.Join(dir, ".env"))
	if err != nil {
		t.Fatalf("read .env: %v", err)
	}
	if !bytes.Contains(b, []byte("FOO=bar")) {
		t.Errorf("missing FOO=bar, got %s", b)
	}
}
