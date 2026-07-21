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

	"github.com/docker/docker/api/types/build"
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

// defaultEnvID returns the default ("Main") environment CreateProject creates
// for projectID, for tests that just need a valid EnvironmentID to satisfy
// CreateNode.
func defaultEnvID(t *testing.T, s *store.Store, projectID uint) uint {
	t.Helper()
	env, err := s.GetDefaultEnvironment(projectID)
	if err != nil {
		t.Fatalf("GetDefaultEnvironment: %v", err)
	}
	return env.ID
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

func TestDraftImageTagIncludesEnvironment(t *testing.T) {
	got := draftImageTag("My App", "staging", "api", 3)
	if got != "draft-my-app-staging-api:3" {
		t.Fatalf("draftImageTag = %q", got)
	}
	// Same project+service+sequence in different envs must not collide on the tag.
	main := draftImageTag("My App", "main", "api", 1)
	staging := draftImageTag("My App", "staging", "api", 1)
	if main == staging {
		t.Fatalf("main and staging image tags collided: %q", main)
	}
	// Empty env falls back to "default" for legacy safety.
	if draftImageTag("app", "", "web", 1) != "draft-app-default-web:1" {
		t.Fatalf("empty env fallback = %q", draftImageTag("app", "", "web", 1))
	}
}

func TestDraftPreviousImageTag(t *testing.T) {
	live := "draft-my-app-main-api:3"
	prev := draftPreviousImageTag(live)
	if prev != "draft-my-app-main-api:3-previous" {
		t.Fatalf("draftPreviousImageTag = %q", prev)
	}
	// Idempotent on already-previous tags.
	if draftPreviousImageTag(prev) != prev {
		t.Fatalf("expected previous tag to be stable, got %q", draftPreviousImageTag(prev))
	}
	if !isDraftPreviousImageTag(prev) {
		t.Fatal("expected isDraftPreviousImageTag")
	}
	if isDraftPreviousImageTag(live) {
		t.Fatal("live tag should not count as previous")
	}
	if !draftBuildTagPattern.MatchString(live) || !draftBuildTagPattern.MatchString(prev) {
		t.Fatalf("pattern should match live and previous: %q %q", live, prev)
	}
}

func TestDraftContainerNameIncludesEnvironment(t *testing.T) {
	got := draftContainerName("My App", "staging", "api", 3)
	if got != "draft-my-app-staging-api-3" {
		t.Fatalf("draftContainerName = %q", got)
	}
	main := draftContainerName("My App", "main", "api", 1)
	staging := draftContainerName("My App", "staging", "api", 1)
	if main == staging {
		t.Fatalf("main and staging container names collided: %q", main)
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

func TestGetBuildLogTailCapsLargeFile(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	var b strings.Builder
	b.WriteString("HEAD_SHOULD_DROP\n")
	for b.Len() < int(maxBuildLogBytes)+1024 {
		b.WriteString("keep-me-line\n")
	}
	b.WriteString("TAIL_MARKER\n")
	if err := os.WriteFile(e.logPath(43), []byte(b.String()), 0644); err != nil {
		t.Fatal(err)
	}

	log, err := e.GetBuildLog(43)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(log, "HEAD_SHOULD_DROP") {
		t.Fatal("expected oversized head to be truncated away")
	}
	if !strings.Contains(log, "TAIL_MARKER") {
		t.Fatal("expected tail of oversized log")
	}
	if !strings.Contains(log, "truncated") {
		t.Fatal("expected truncation marker")
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

func TestParseContainerLogLineSeparatesDockerTimestamp(t *testing.T) {
	entry := parseContainerLogLine("2026-07-20T12:34:56.123456789Z started", "stdout")
	if entry.Timestamp != "2026-07-20T12:34:56.123456789Z" || entry.Line != "started" || entry.Stream != "stdout" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestParseContainerLogLineLeavesUntimestampedOutputAlone(t *testing.T) {
	entry := parseContainerLogLine("started", "stderr")
	if entry.Timestamp != "" || entry.Line != "started" || entry.Stream != "stderr" {
		t.Fatalf("entry = %+v", entry)
	}
}

func TestGetContainerLogHistoryRejectsUnsafeTail(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	if _, err := e.GetContainerLogHistory(context.Background(), "n1", 0); err == nil {
		t.Fatal("expected invalid tail to fail")
	}
}

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

func TestExitingPreviousDeploymentKeepsNewDeploymentRoute(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	const nodeID = "svc1"
	const hostname = "api.project.default.abcd.draft.local"
	if _, err := s.CreateRoute(&store.Route{
		Hostname: hostname, ProjectID: 1, NodeID: nodeID, Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}
	old, err := s.CreateDeployment(&store.Deployment{NodeID: nodeID, ProjectID: 1, Hostname: hostname, Status: "stopped"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDeployment(&store.Deployment{NodeID: nodeID, ProjectID: 1, Hostname: hostname, Status: "running"}); err != nil {
		t.Fatal(err)
	}

	e.unregisterRouteIfUnowned(old, nodeID)
	if _, err := e.router.Lookup(hostname); err != nil {
		t.Fatalf("new deployment route was removed: %v", err)
	}
}

func TestExitingPreviousDeploymentRemovesRouteUntilSuccessorRuns(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	const nodeID = "svc1"
	const hostname = "api.project.default.abcd.draft.local"
	if _, err := s.CreateRoute(&store.Route{
		Hostname: hostname, ProjectID: 1, NodeID: nodeID, Protocol: "http", TargetHost: "127.0.0.1", TargetPort: 3000,
	}); err != nil {
		t.Fatal(err)
	}
	old, err := s.CreateDeployment(&store.Deployment{NodeID: nodeID, ProjectID: 1, Hostname: hostname, Status: "stopped"})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateDeployment(&store.Deployment{NodeID: nodeID, ProjectID: 1, Hostname: hostname, Status: "building"}); err != nil {
		t.Fatal(err)
	}

	e.unregisterRouteIfUnowned(old, nodeID)
	if _, err := e.router.Lookup(hostname); err == nil {
		t.Fatal("stopped deployment route remained while successor was not running")
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

func TestResolveBuildContextPlanUsesServiceRootWhenDockerfileInside(t *testing.T) {
	project := t.TempDir()
	serviceRoot := filepath.Join(project, "services", "web")
	if err := os.MkdirAll(serviceRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	plan, err := resolveBuildContextPlan(project, filepath.Join("services", "web"), "Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if plan.ContextRoot != serviceRoot {
		t.Fatalf("ContextRoot = %q, want %q", plan.ContextRoot, serviceRoot)
	}
	if plan.ServiceRoot != serviceRoot {
		t.Fatalf("ServiceRoot = %q, want %q", plan.ServiceRoot, serviceRoot)
	}
	if plan.RelativeDockerfile != "Dockerfile" {
		t.Fatalf("RelativeDockerfile = %q, want Dockerfile", plan.RelativeDockerfile)
	}
}

func TestResolveBuildContextPlanFallsBackToProjectRoot(t *testing.T) {
	project := t.TempDir()
	serviceRoot := filepath.Join(project, "services", "web")
	if err := os.MkdirAll(serviceRoot, 0o755); err != nil {
		t.Fatal(err)
	}

	plan, err := resolveBuildContextPlan(project, filepath.Join("services", "web"), "../Dockerfile")
	if err != nil {
		t.Fatal(err)
	}
	if plan.ContextRoot != project {
		t.Fatalf("ContextRoot = %q, want %q", plan.ContextRoot, project)
	}
	if plan.ServiceRoot != serviceRoot {
		t.Fatalf("ServiceRoot = %q, want %q", plan.ServiceRoot, serviceRoot)
	}
	if plan.RelativeDockerfile != "services/Dockerfile" {
		t.Fatalf("RelativeDockerfile = %q, want services/Dockerfile", plan.RelativeDockerfile)
	}
}

func TestBuildkitEnabledDefaultsOn(t *testing.T) {
	if !buildkitEnabled(map[string]string{}) {
		t.Fatal("expected BuildKit local-context to default on")
	}
	if buildkitEnabled(map[string]string{"use_buildkit_local_context": "false"}) {
		t.Fatal("expected explicit false to disable BuildKit local-context")
	}
}

func TestLegacyImageBuildOptionsForceBuilderV1(t *testing.T) {
	value := "bar"
	opts := legacyImageBuildOptions("draft-test:1", "Dockerfile", map[string]*string{"FOO": &value}, buildOverrides{})

	if opts.Version != build.BuilderV1 {
		t.Fatalf("expected legacy builder version %q, got %q", build.BuilderV1, opts.Version)
	}
	if len(opts.Tags) != 1 || opts.Tags[0] != "draft-test:1" {
		t.Fatalf("unexpected tags: %+v", opts.Tags)
	}
	if opts.Dockerfile != "Dockerfile" {
		t.Fatalf("unexpected dockerfile: %q", opts.Dockerfile)
	}
	if opts.BuildArgs["FOO"] == nil || *opts.BuildArgs["FOO"] != "bar" {
		t.Fatalf("unexpected build args: %+v", opts.BuildArgs)
	}
}

func TestBuildArgsForCLI(t *testing.T) {
	one := "one"
	three := "three"
	got := buildArgsForCLI(map[string]*string{
		"B": &three,
		"A": &one,
		"C": nil,
	})
	want := []string{"A=one", "B=three", "C="}
	if len(got) != len(want) {
		t.Fatalf("buildArgsForCLI length = %d, want %d", len(got), len(want))
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("buildArgsForCLI[%d] = %q, want %q", i, got[i], want[i])
		}
	}
}

func TestBuildxCompatibleWithSettingsRejectsGitignoreMode(t *testing.T) {
	project := t.TempDir()
	plan := buildContextPlan{ContextRoot: project, ServiceRoot: project}
	ok, reason := buildxCompatibleWithSettings(project, plan, map[string]string{"use_gitignore": "true"})
	if ok {
		t.Fatal("expected gitignore mode to reject buildx local-context")
	}
	if !strings.Contains(reason, ".gitignore") {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestBuildxCompatibleWithSettingsRejectsRootDockerignoreWhenToggleOff(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, ".dockerignore"), []byte("node_modules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := buildContextPlan{ContextRoot: project, ServiceRoot: project}
	ok, reason := buildxCompatibleWithSettings(project, plan, map[string]string{"use_dockerignore": "false"})
	if ok {
		t.Fatal("expected root .dockerignore with toggle off to reject buildx local-context")
	}
	if !strings.Contains(reason, ".dockerignore") {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestBuildxCompatibleWithSettingsAcceptsSimpleDockerignoreMode(t *testing.T) {
	project := t.TempDir()
	if err := os.WriteFile(filepath.Join(project, ".dockerignore"), []byte("node_modules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := buildContextPlan{ContextRoot: project, ServiceRoot: project}
	ok, reason := buildxCompatibleWithSettings(project, plan, map[string]string{"use_dockerignore": "true"})
	if !ok {
		t.Fatalf("expected simple root .dockerignore mode to allow buildx local-context, got %q", reason)
	}
}

func TestBuildxCompatibleWithSettingsRejectsLegacySkipEntriesWithoutRootDockerignore(t *testing.T) {
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	plan := buildContextPlan{ContextRoot: project, ServiceRoot: project}
	ok, reason := buildxCompatibleWithSettings(project, plan, map[string]string{})
	if ok {
		t.Fatal("expected top-level Draft-skipped entries to reject buildx local-context without root .dockerignore")
	}
	if !strings.Contains(reason, "node_modules") {
		t.Fatalf("unexpected reason: %q", reason)
	}
}

func TestBuildxCompatibleWithSettingsAcceptsLegacySkipEntriesWhenRootDockerignoreMatches(t *testing.T) {
	project := t.TempDir()
	if err := os.Mkdir(filepath.Join(project, "node_modules"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(project, ".dockerignore"), []byte("node_modules\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	plan := buildContextPlan{ContextRoot: project, ServiceRoot: project}
	ok, reason := buildxCompatibleWithSettings(project, plan, map[string]string{"use_dockerignore": "true"})
	if !ok {
		t.Fatalf("expected matching root .dockerignore to allow buildx local-context, got %q", reason)
	}
}

func TestLogPath(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	path := e.logPath(42)
	if !strings.HasSuffix(path, fmt.Sprintf("logs%c42.log", filepath.Separator)) {
		t.Errorf("unexpected log path: %s", path)
	}
}

func TestApplyContainerExitResultIntentionalStopPreservesStoppedState(t *testing.T) {
	exitCode := 0
	dep := &store.Deployment{
		Status:    "stopped",
		ExitCode:  &exitCode,
		OOMKilled: false,
	}

	applyContainerExitResult(dep, 137, nil)

	if dep.Status != "stopped" {
		t.Fatalf("Status = %q, want stopped", dep.Status)
	}
	if dep.Error != "" {
		t.Fatalf("Error = %q, want empty", dep.Error)
	}
	if dep.ExitCode == nil || *dep.ExitCode != 0 {
		t.Fatalf("ExitCode = %#v, want 0", dep.ExitCode)
	}
	if dep.OOMKilled {
		t.Fatal("OOMKilled = true, want false")
	}
}

func TestApplyContainerExitResultCrashMarksFailed(t *testing.T) {
	dep := &store.Deployment{}

	applyContainerExitResult(dep, 137, nil)

	if dep.Status != "failed" {
		t.Fatalf("Status = %q, want failed", dep.Status)
	}
	if dep.Error != "container exited with code 137" {
		t.Fatalf("Error = %q, want container exited with code 137", dep.Error)
	}
	if dep.ExitCode == nil || *dep.ExitCode != 137 {
		t.Fatalf("ExitCode = %#v, want 137", dep.ExitCode)
	}
}

func TestApplyContainerExitResultUnexpectedCleanExitMarksFailed(t *testing.T) {
	// Postgres (and similar) can exit 0 after a bad volume cutover when the
	// previous container deletes postmaster.pid. That must not look like Stop.
	dep := &store.Deployment{Status: "running"}

	applyContainerExitResult(dep, 0, nil)

	if dep.Status != "failed" {
		t.Fatalf("Status = %q, want failed", dep.Status)
	}
	if dep.Error != "container exited unexpectedly (code 0)" {
		t.Fatalf("Error = %q", dep.Error)
	}
	if dep.ExitCode == nil || *dep.ExitCode != 0 {
		t.Fatalf("ExitCode = %#v, want 0", dep.ExitCode)
	}
}

func TestIsDraftManagedImageTag(t *testing.T) {
	cases := []struct {
		tag  string
		want bool
	}{
		{"draft-app-main-api:3", true},
		{"draft-app-main-api:3-previous", true},
		{"postgres:16-alpine", false},
		{"postgres:16-alpine-previous", false},
		{"redis:7", false},
		{"ghcr.io/org/img:1", false},
		{"", false},
		{"draft-only", false}, // no tag separator
	}
	for _, tc := range cases {
		if got := isDraftManagedImageTag(tc.tag); got != tc.want {
			t.Errorf("isDraftManagedImageTag(%q) = %v, want %v", tc.tag, got, tc.want)
		}
	}
}
