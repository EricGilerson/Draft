package deploy

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"Draft/internal/store"
)

func TestCommandOutputWriterCapsBytes(t *testing.T) {
	var buf bytes.Buffer
	var streamed strings.Builder
	w := &commandOutputWriter{
		buf:      &buf,
		on:       func(s string) { streamed.WriteString(s) },
		maxBytes: 8,
	}
	if _, err := w.Write([]byte("abcdefghij")); err != nil {
		t.Fatal(err)
	}
	if buf.String() != "abcdefgh" {
		t.Fatalf("buf = %q", buf.String())
	}
	if !w.truncated {
		t.Fatal("expected truncated")
	}
	if _, err := w.Write([]byte("more")); err != nil {
		t.Fatal(err)
	}
	if buf.Len() != 8 {
		t.Fatalf("buf grew after cap: %d", buf.Len())
	}
	if streamed.String() != "abcdefgh" {
		t.Fatalf("streamed = %q", streamed.String())
	}
}

func TestEmitBuildLogStripsANSIOnlyWhenPresent(t *testing.T) {
	s := openTestStore(t)
	e, col := newTestEngine(t, s)
	e.emitBuildLog("n1", "plain line")
	e.emitBuildLog("n1", "with\x1b[31mred\x1b[0m")
	var lines []string
	for _, ev := range col.get() {
		if ev.Name != "build:log" {
			continue
		}
		data, _ := ev.Data.(map[string]any)
		line, _ := data["line"].(LogLine)
		lines = append(lines, line.Line)
	}
	if len(lines) < 2 {
		t.Fatalf("build log lines = %+v", lines)
	}
	if lines[0] != "plain line" {
		t.Fatalf("plain = %q", lines[0])
	}
	if strings.Contains(lines[1], "\x1b") || !strings.Contains(lines[1], "red") {
		t.Fatalf("ansi not stripped: %q", lines[1])
	}
}

func TestRemoveBuildLogsAndClearRuntimeState(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)
	dep, err := s.CreateDeployment(&store.Deployment{NodeID: "n1", ProjectID: 1, Status: "stopped"})
	if err != nil {
		t.Fatal(err)
	}
	path := e.logPath(dep.ID)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("log"), 0o644); err != nil {
		t.Fatal(err)
	}
	e.statsMu.Lock()
	e.stats["n1"] = []MetricPoint{{CPUPercent: 1}}
	e.statsMu.Unlock()
	e.activityMu.Lock()
	e.lastProxyTouch["n1"] = time.Now()
	e.activityMu.Unlock()

	e.removeBuildLogs([]store.Deployment{*dep})
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("log still present: %v", err)
	}
	e.clearNodeRuntimeState("n1")
	e.statsMu.Lock()
	_, okStats := e.stats["n1"]
	e.statsMu.Unlock()
	e.activityMu.Lock()
	_, okTouch := e.lastProxyTouch["n1"]
	e.activityMu.Unlock()
	if okStats || okTouch {
		t.Fatal("runtime maps should drop node")
	}
}

func TestMaxStackDeployConcurrencyBounded(t *testing.T) {
	if maxStackDeployConcurrency != 2 {
		t.Fatalf("maxStackDeployConcurrency = %d, want 2", maxStackDeployConcurrency)
	}
}

func TestReadBuildLogTailMissingAndEmpty(t *testing.T) {
	got, err := readBuildLogTail(filepath.Join(t.TempDir(), "missing.log"), 100)
	if err != nil || got != "" {
		t.Fatalf("missing: %q err=%v", got, err)
	}
	path := filepath.Join(t.TempDir(), "empty.log")
	if err := os.WriteFile(path, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	got, err = readBuildLogTail(path, 100)
	if err != nil || got != "" {
		t.Fatalf("empty: %q err=%v", got, err)
	}
}
