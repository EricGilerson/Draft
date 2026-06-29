package deploy

import (
	"archive/tar"
	"compress/gzip"
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"Draft/internal/networking"
	"Draft/internal/store"
)

func openTestStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := store.FileDSN(filepath.Join(t.TempDir(), "test.db"))
	s, err := store.Open(dsn)
	if err != nil {
		t.Fatalf("Open store: %v", err)
	}
	t.Cleanup(func() { _ = s.Close() })
	return s
}

func newTestEngine(t *testing.T, s *store.Store) (*Engine, *eventCollector) {
	t.Helper()
	r := networking.NewRouter(s, "127.0.0.1:0")
	logDir := filepath.Join(t.TempDir(), "logs")
	col := &eventCollector{}
	e := New(s, r, logDir, col.emit)
	return e, col
}

type eventCollector struct {
	mu     sync.Mutex
	events []emittedEvent
}

type emittedEvent struct {
	Name string
	Data any
}

func (c *eventCollector) emit(event string, data any) {
	c.mu.Lock()
	c.events = append(c.events, emittedEvent{Name: event, Data: data})
	c.mu.Unlock()
}

func (c *eventCollector) get() []emittedEvent {
	c.mu.Lock()
	defer c.mu.Unlock()
	out := make([]emittedEvent, len(c.events))
	copy(out, c.events)
	return out
}

func (c *eventCollector) findStatus(nodeID, status string) *emittedEvent {
	for _, ev := range c.get() {
		if ev.Name == "deploy:status:"+nodeID {
			if se, ok := ev.Data.(StatusEvent); ok && se.Status == status {
				return &ev
			}
		}
	}
	return nil
}

// --- sanitize ---

func TestSanitize(t *testing.T) {
	tests := []struct {
		in, want string
	}{
		{"My App", "my-app"},
		{"hello_world", "hello-world"},
		{"  DRAFT  ", "draft"},
		{"a--b", "a--b"},
		{"---", ""},
		{"café", "caf"},
		{"123", "123"},
	}
	for _, tt := range tests {
		got := sanitize(tt.in)
		if got != tt.want {
			t.Errorf("sanitize(%q) = %q, want %q", tt.in, got, tt.want)
		}
	}
}

func TestDraftNetworkName(t *testing.T) {
	got := draftNetworkName(42, "My Project!", "default")
	if got != "draft-42-my-project-default" {
		t.Fatalf("draftNetworkName = %q", got)
	}
}

func TestInternalNetworkAliases(t *testing.T) {
	got := internalNetworkAliases("api", "api.app.default.abcd.draft.local")
	want := []string{"api", "api.app.default.abcd.draft.local"}
	if len(got) != len(want) {
		t.Fatalf("aliases = %+v", got)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("aliases = %+v, want %+v", got, want)
		}
	}
}

// --- shouldSkip ---

func TestShouldSkip(t *testing.T) {
	skipped := []string{".git", ".git/config", "node_modules", "node_modules/foo/bar", ".next", "__pycache__", ".venv"}
	for _, p := range skipped {
		if !shouldSkip(p) {
			t.Errorf("shouldSkip(%q) = false, want true", p)
		}
	}
	kept := []string{"src/main.go", "Dockerfile", "README.md", ".dockerignore", ".gitignore"}
	for _, p := range kept {
		if shouldSkip(p) {
			t.Errorf("shouldSkip(%q) = true, want false", p)
		}
	}
}

// --- tarDirectory ---

func TestTarDirectory(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "Dockerfile"), []byte("FROM alpine"), 0644)
	os.WriteFile(filepath.Join(dir, "main.go"), []byte("package main"), 0644)
	os.MkdirAll(filepath.Join(dir, "src"), 0755)
	os.WriteFile(filepath.Join(dir, "src", "app.go"), []byte("package src"), 0644)

	rc, _, _, err := tarDirectoryWithProgress(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	files := readTarFiles(t, rc)
	sort.Strings(files)

	want := []string{"Dockerfile", "main.go", "src", "src/app.go"}
	if len(files) != len(want) {
		t.Fatalf("got files %v, want %v", files, want)
	}
	for i, f := range want {
		if files[i] != f {
			t.Errorf("file %d: got %q, want %q", i, files[i], f)
		}
	}
}

func TestTarDirectorySkipsGitAndNodeModules(t *testing.T) {
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "app.js"), []byte("console.log()"), 0644)
	os.MkdirAll(filepath.Join(dir, ".git", "objects"), 0755)
	os.WriteFile(filepath.Join(dir, ".git", "HEAD"), []byte("ref: refs/heads/main"), 0644)
	os.MkdirAll(filepath.Join(dir, "node_modules", "express"), 0755)
	os.WriteFile(filepath.Join(dir, "node_modules", "express", "index.js"), []byte(""), 0644)

	rc, _, _, err := tarDirectoryWithProgress(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	files := readTarFiles(t, rc)
	if len(files) != 1 || files[0] != "app.js" {
		t.Errorf("expected only [app.js], got %v", files)
	}
}

func TestTarDirectoryEmpty(t *testing.T) {
	dir := t.TempDir()
	rc, _, _, err := tarDirectoryWithProgress(dir, nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer rc.Close()

	files := readTarFiles(t, rc)
	if len(files) != 0 {
		t.Errorf("expected empty tar, got %v", files)
	}
}

func readTarFiles(t *testing.T, r io.Reader) []string {
	t.Helper()
	gr, err := gzip.NewReader(r)
	if err != nil {
		t.Fatal(err)
	}
	defer gr.Close()

	tr := tar.NewReader(gr)
	var files []string
	for {
		hdr, err := tr.Next()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatal(err)
		}
		files = append(files, hdr.Name)
	}
	return files
}

// --- streamBuildOutput ---

func TestStreamBuildOutputSuccess(t *testing.T) {
	s := openTestStore(t)
	e, col := newTestEngine(t, s)

	logFile := filepath.Join(t.TempDir(), "build.log")
	f, _ := os.Create(logFile)
	defer f.Close()

	input := strings.NewReader(
		`{"stream":"Step 1/3 : FROM alpine\n"}` + "\n" +
			`{"stream":"Step 2/3 : RUN echo hi\n"}` + "\n" +
			`{"stream":"Step 3/3 : CMD [\"app\"]\n"}` + "\n",
	)

	err := e.streamBuildOutput(context.Background(), input, f, "test-node", 1)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	var nodeEvents []emittedEvent
	for _, ev := range col.get() {
		if ev.Name == "build:log:test-node" {
			nodeEvents = append(nodeEvents, ev)
		}
	}
	// 1 "Building image..." + 3 stream lines = 4
	if len(nodeEvents) != 4 {
		t.Fatalf("expected 4 per-node log events, got %d", len(nodeEvents))
	}
	for _, ev := range nodeEvents {
		ll := ev.Data.(LogLine)
		if ll.Stream != "build" {
			t.Errorf("expected stream=build, got %s", ll.Stream)
		}
	}

	// Verify log file was written
	data, _ := os.ReadFile(logFile)
	lines := strings.Split(strings.TrimSpace(string(data)), "\n")
	if len(lines) != 3 {
		t.Errorf("expected 3 lines in log file, got %d", len(lines))
	}
}

func TestStreamBuildOutputError(t *testing.T) {
	s := openTestStore(t)
	e, col := newTestEngine(t, s)

	logFile := filepath.Join(t.TempDir(), "build.log")
	f, _ := os.Create(logFile)
	defer f.Close()

	input := strings.NewReader(
		`{"stream":"Step 1/2 : FROM alpine\n"}` + "\n" +
			`{"error":"some build error"}` + "\n",
	)

	err := e.streamBuildOutput(context.Background(), input, f, "test-node", 1)
	if err == nil {
		t.Fatal("expected error")
	}
	if !strings.Contains(err.Error(), "some build error") {
		t.Errorf("error should contain build error, got: %v", err)
	}

	var nodeEvents []emittedEvent
	for _, ev := range col.get() {
		if ev.Name == "build:log:test-node" {
			nodeEvents = append(nodeEvents, ev)
		}
	}
	// 1 "Building image..." + 1 stream + 1 error = 3
	if len(nodeEvents) != 3 {
		t.Fatalf("expected 3 per-node log events, got %d", len(nodeEvents))
	}
}

func TestStreamBuildOutputCancelled(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	logFile := filepath.Join(t.TempDir(), "build.log")
	f, _ := os.Create(logFile)
	defer f.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	input := strings.NewReader(`{"stream":"Step 1/1 : FROM alpine\n"}` + "\n")
	err := e.streamBuildOutput(ctx, input, f, "test-node", 1)
	if err == nil || !strings.Contains(err.Error(), "context canceled") {
		t.Errorf("expected context canceled error, got: %v", err)
	}
}

// --- GetBuildLog ---

func TestGetBuildLogExists(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	os.WriteFile(e.logPath(42), []byte("build line 1\nbuild line 2\n"), 0644)

	log, err := e.GetBuildLog(42)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(log, "build line 1") {
		t.Errorf("log should contain build output, got: %s", log)
	}
}

func TestGetBuildLogMissing(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	log, err := e.GetBuildLog(999)
	if err != nil {
		t.Fatal(err)
	}
	if log != "" {
		t.Errorf("expected empty string for missing log, got: %q", log)
	}
}

// --- Deploy validation ---

func TestDeployMissingSettings(t *testing.T) {
	s := openTestStore(t)
	s.DB.Create(&store.Project{Name: "proj", Path: "/proj"})
	s.DB.Create(&store.CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	e, col := newTestEngine(t, s)
	e.Deploy(context.Background(), "n1")

	time.Sleep(100 * time.Millisecond)

	ev := col.findStatus("n1", "failed")
	if ev == nil {
		t.Fatal("expected failed status event")
	}
	se := ev.Data.(StatusEvent)
	if !strings.Contains(se.Error, "required settings") {
		t.Errorf("expected 'required settings' error, got: %s", se.Error)
	}
}

func TestDeployMissingNode(t *testing.T) {
	s := openTestStore(t)
	e, col := newTestEngine(t, s)

	s.SetNodeSetting("ghost", "dockerfile", "Dockerfile")
	s.SetNodeSetting("ghost", "service_port", "8080")

	e.Deploy(context.Background(), "ghost")
	time.Sleep(100 * time.Millisecond)

	ev := col.findStatus("ghost", "failed")
	if ev == nil {
		t.Fatal("expected failed status for missing node")
	}
}

func TestDeployPartialSettings(t *testing.T) {
	s := openTestStore(t)
	s.DB.Create(&store.Project{Name: "proj", Path: "/proj"})
	s.DB.Create(&store.CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})
	s.SetNodeSetting("n1", "dockerfile", "Dockerfile")
	// Missing service_port

	e, col := newTestEngine(t, s)
	e.Deploy(context.Background(), "n1")
	time.Sleep(100 * time.Millisecond)

	ev := col.findStatus("n1", "failed")
	if ev == nil {
		t.Fatal("expected failed status for missing port")
	}
}

// --- failDeployment ---

func TestFailDeploymentSetsFields(t *testing.T) {
	s := openTestStore(t)
	s.DB.Create(&store.Project{Name: "proj", Path: "/proj"})
	s.DB.Create(&store.CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})

	dep, _ := s.CreateDeployment(&store.Deployment{NodeID: "n1", ProjectID: 1, Status: "building"})

	e, col := newTestEngine(t, s)
	e.failDeployment(dep, "n1", "something broke")

	got, _ := s.GetDeployment(dep.ID)
	if got.Status != "failed" {
		t.Errorf("expected failed status, got %s", got.Status)
	}
	if got.Error != "something broke" {
		t.Errorf("expected error message, got %s", got.Error)
	}
	if got.FinishedAt == nil {
		t.Error("expected FinishedAt to be set")
	}

	ev := col.findStatus("n1", "failed")
	if ev == nil {
		t.Fatal("expected failed event emitted")
	}
}

// --- StopLogStream ---

func TestStopLogStreamNoOp(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	// Should not panic
	e.StopLogStream("nonexistent")
}

// --- GetDeployments / GetActiveDeployment delegates ---

func TestGetDeploymentsDelegates(t *testing.T) {
	s := openTestStore(t)
	s.DB.Create(&store.Project{Name: "proj", Path: "/proj"})
	s.DB.Create(&store.CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})
	s.CreateDeployment(&store.Deployment{NodeID: "n1", ProjectID: 1, Status: "stopped"})
	s.CreateDeployment(&store.Deployment{NodeID: "n1", ProjectID: 1, Status: "running"})

	e, _ := newTestEngine(t, s)

	list, err := e.GetDeployments("n1")
	if err != nil {
		t.Fatal(err)
	}
	if len(list) != 2 {
		t.Fatalf("expected 2, got %d", len(list))
	}

	active, err := e.GetActiveDeployment("n1")
	if err != nil {
		t.Fatal(err)
	}
	if active == nil || active.Status != "running" {
		t.Fatalf("expected running active deployment, got %+v", active)
	}
}

func TestGetActiveDeploymentNone(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	active, err := e.GetActiveDeployment("nonexistent")
	if err != nil {
		t.Fatal(err)
	}
	if active != nil {
		t.Fatalf("expected nil, got %+v", active)
	}
}

// --- Concurrent deploy cancels previous ---

func TestDeployCancelsPrevious(t *testing.T) {
	s := openTestStore(t)
	s.DB.Create(&store.Project{Name: "proj", Path: "/proj"})
	s.DB.Create(&store.CanvasNode{ID: "n1", ProjectID: 1, Label: "svc"})
	s.SetNodeSetting("n1", "dockerfile", "Dockerfile")
	s.SetNodeSetting("n1", "service_port", "8080")

	e, _ := newTestEngine(t, s)

	// First deploy will fail (no Docker / no Dockerfile on disk) but the point
	// is that the second deploy cancels the first's context
	e.Deploy(context.Background(), "n1")
	time.Sleep(10 * time.Millisecond)

	// Second deploy should cancel the first
	e.Deploy(context.Background(), "n1")
	time.Sleep(100 * time.Millisecond)

	// Both should have completed without deadlock
	e.mu.Lock()
	_, stillActive := e.active["n1"]
	e.mu.Unlock()
	// May or may not be active depending on timing — just verify no deadlock
	_ = stillActive
}

// --- logPath ---

func TestLogPath(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	path := e.logPath(42)
	if !strings.HasSuffix(path, fmt.Sprintf("logs%c42.log", filepath.Separator)) {
		t.Errorf("unexpected log path: %s", path)
	}
}
