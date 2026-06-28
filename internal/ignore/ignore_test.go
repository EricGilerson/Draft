package ignore

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseLineSkipsBlanksAndComments(t *testing.T) {
	for _, line := range []string{"", "  ", "# comment", "  # indented"} {
		if parseLine(line) != nil {
			t.Errorf("expected nil for %q", line)
		}
	}
}

func TestParseLineNegate(t *testing.T) {
	p := parseLine("!important.txt")
	if p == nil || !p.negate || p.pattern != "important.txt" {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseLineDirOnly(t *testing.T) {
	p := parseLine("build/")
	if p == nil || !p.dirOnly || p.pattern != "build" {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseLineAnchored(t *testing.T) {
	p := parseLine("foo/bar")
	if p == nil || !p.anchored || p.pattern != "foo/bar" {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestMatchBasic(t *testing.T) {
	m := New()
	m.rules = []rule{
		{base: "", pattern: Pattern{pattern: "*.log"}},
		{base: "", pattern: Pattern{pattern: "dist", dirOnly: true}},
		{base: "", pattern: Pattern{pattern: "node_modules"}},
	}

	tests := []struct {
		path  string
		isDir bool
		want  bool
	}{
		{"app.log", false, true},
		{"src/debug.log", false, true},
		{"app.txt", false, false},
		{"dist", true, true},
		{"dist", false, false}, // dirOnly
		{"node_modules", true, true},
		{"node_modules", false, true},
		{"src/node_modules", true, true},
	}
	for _, tt := range tests {
		got := m.Match(tt.path, tt.isDir)
		if got != tt.want {
			t.Errorf("Match(%q, isDir=%v) = %v, want %v", tt.path, tt.isDir, got, tt.want)
		}
	}
}

func TestMatchNegate(t *testing.T) {
	m := New()
	m.rules = []rule{
		{base: "", pattern: Pattern{pattern: "*.log"}},
		{base: "", pattern: Pattern{pattern: "important.log", negate: true}},
	}

	if !m.Match("debug.log", false) {
		t.Error("debug.log should match")
	}
	if m.Match("important.log", false) {
		t.Error("important.log should be negated")
	}
}

func TestMatchWithBase(t *testing.T) {
	m := New()
	m.rules = []rule{
		{base: "frontend", pattern: Pattern{pattern: "dist", dirOnly: true}},
	}

	if m.Match("dist", true) {
		t.Error("root dist should not match frontend-scoped rule")
	}
	if !m.Match("frontend/dist", true) {
		t.Error("frontend/dist should match")
	}
}

func TestMatchDoublestar(t *testing.T) {
	m := New()
	m.rules = []rule{
		{base: "", pattern: Pattern{pattern: "**/*.test.js", anchored: true}},
	}

	if !m.Match("src/foo.test.js", false) {
		t.Error("should match nested test file")
	}
	if !m.Match("foo.test.js", false) {
		t.Error("should match root test file")
	}
	if m.Match("foo.js", false) {
		t.Error("should not match non-test file")
	}
}

func TestScanDirFindsIgnoreFiles(t *testing.T) {
	// Create:
	//   repo/.gitignore        (contains "*.log")
	//   repo/backend/.gitignore (contains "tmp/")
	//   repo/backend/src/app.go
	root := t.TempDir()
	backend := filepath.Join(root, "backend")
	os.MkdirAll(filepath.Join(backend, "src"), 0755)
	os.WriteFile(filepath.Join(root, ".gitignore"), []byte("*.log\n"), 0644)
	os.WriteFile(filepath.Join(backend, ".gitignore"), []byte("tmp/\n"), 0644)
	os.WriteFile(filepath.Join(backend, "src", "app.go"), []byte("package main"), 0644)

	m := New()
	n, err := m.ScanDir(root, backend, ".gitignore")
	if err != nil {
		t.Fatal(err)
	}
	if n != 2 {
		t.Fatalf("expected 2 .gitignore files, found %d", n)
	}

	// *.log from repo root should apply (parent → base "")
	if !m.Match("error.log", false) {
		t.Error("*.log should match in tar root")
	}
	if !m.Match("src/debug.log", false) {
		t.Error("*.log should match nested")
	}

	// tmp/ from backend/.gitignore should apply (base "")
	if !m.Match("tmp", true) {
		t.Error("tmp/ should match as dir")
	}
	if m.Match("tmp", false) {
		t.Error("tmp should not match as file (dirOnly)")
	}

	// Non-ignored file
	if m.Match("src/app.go", false) {
		t.Error("app.go should not be ignored")
	}
}

func TestScanDirSubdirIgnore(t *testing.T) {
	// repo/backend/sub/.gitignore with "*.tmp"
	// tarRoot = repo/backend
	// Pattern should only apply under sub/
	root := t.TempDir()
	backend := filepath.Join(root, "backend")
	sub := filepath.Join(backend, "sub")
	os.MkdirAll(sub, 0755)
	os.WriteFile(filepath.Join(sub, ".gitignore"), []byte("*.tmp\n"), 0644)

	m := New()
	n, _ := m.ScanDir(root, backend, ".gitignore")
	if n != 1 {
		t.Fatalf("expected 1, got %d", n)
	}

	if m.Match("foo.tmp", false) {
		t.Error("root foo.tmp should NOT match (pattern scoped to sub/)")
	}
	if !m.Match("sub/foo.tmp", false) {
		t.Error("sub/foo.tmp should match")
	}
}
