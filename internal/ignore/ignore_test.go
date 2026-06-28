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
	if p == nil || !p.negate || p.raw != "important.txt" {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseLineDirOnly(t *testing.T) {
	p := parseLine("build/")
	if p == nil || !p.dirOnly || p.raw != "build" {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseLineAnchored(t *testing.T) {
	p := parseLine("foo/bar")
	if p == nil || !p.anchored || p.raw != "foo/bar" {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseLineEscapedHash(t *testing.T) {
	p := parseLine(`\#not-a-comment`)
	if p == nil || p.raw != "#not-a-comment" {
		t.Fatalf("unexpected: %+v", p)
	}
}

func TestParseLineTrailingSpace(t *testing.T) {
	p := parseLine(`foo\ `)
	if p == nil || p.raw != "foo " {
		t.Fatalf("expected raw='foo ', got %+v", p)
	}
}

func TestParseLineTrailingSpaceStripped(t *testing.T) {
	p := parseLine("foo   ")
	if p == nil || p.raw != "foo" {
		t.Fatalf("expected raw='foo', got %+v", p)
	}
}

func TestMatchBasic(t *testing.T) {
	m := New()
	m.rules = []rule{
		{base: "", pattern: pattern{raw: "*.log"}},
		{base: "", pattern: pattern{raw: "dist", dirOnly: true}},
		{base: "", pattern: pattern{raw: "node_modules"}},
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
		{base: "", pattern: pattern{raw: "*.log"}},
		{base: "", pattern: pattern{raw: "important.log", negate: true}},
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
		{base: "frontend", pattern: pattern{raw: "dist", dirOnly: true}},
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
		{base: "", pattern: pattern{raw: "**/*.test.js", anchored: true}},
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

func TestGlobStarDoesNotMatchSlash(t *testing.T) {
	if globMatch("*.log", "src/debug.log") {
		t.Error("single * should not match /")
	}
	if !globMatch("*.log", "debug.log") {
		t.Error("single * should match flat filename")
	}
}

func TestGlobQuestion(t *testing.T) {
	if !globMatch("?.txt", "a.txt") {
		t.Error("? should match single char")
	}
	if globMatch("?.txt", "ab.txt") {
		t.Error("? should not match two chars")
	}
	if globMatch("?", "/") {
		t.Error("? should not match /")
	}
}

func TestGlobCharClass(t *testing.T) {
	if !globMatch("[abc].txt", "a.txt") {
		t.Error("[abc] should match a")
	}
	if globMatch("[abc].txt", "d.txt") {
		t.Error("[abc] should not match d")
	}
	if !globMatch("[a-z].txt", "m.txt") {
		t.Error("[a-z] should match m")
	}
	if globMatch("[!a-z].txt", "m.txt") {
		t.Error("[!a-z] should not match m")
	}
	if !globMatch("[!a-z].txt", "1.txt") {
		t.Error("[!a-z] should match 1")
	}
}

func TestGlobBackslashEscape(t *testing.T) {
	if !globMatch(`\*.txt`, "*.txt") {
		t.Error(`\* should match literal *`)
	}
	if globMatch(`\*.txt`, "a.txt") {
		t.Error(`\* should not match a`)
	}
}

func TestGlobDoublestarMiddle(t *testing.T) {
	if !globMatch("a/**/b.txt", "a/b.txt") {
		t.Error("a/**/b.txt should match a/b.txt (zero dirs)")
	}
	if !globMatch("a/**/b.txt", "a/x/b.txt") {
		t.Error("a/**/b.txt should match a/x/b.txt")
	}
	if !globMatch("a/**/b.txt", "a/x/y/b.txt") {
		t.Error("a/**/b.txt should match a/x/y/b.txt")
	}
	if globMatch("a/**/b.txt", "c/x/b.txt") {
		t.Error("a/**/b.txt should not match c/x/b.txt")
	}
}

func TestGlobDoublestarTrailing(t *testing.T) {
	if !globMatch("src/**", "src/a.go") {
		t.Error("src/** should match src/a.go")
	}
	if !globMatch("src/**", "src/sub/a.go") {
		t.Error("src/** should match nested")
	}
}

func TestGlobDoublestarLeading(t *testing.T) {
	if !globMatch("**/test", "test") {
		t.Error("**/test should match root")
	}
	if !globMatch("**/test", "a/test") {
		t.Error("**/test should match nested")
	}
	if !globMatch("**/test", "a/b/test") {
		t.Error("**/test should match deeply nested")
	}
}

func TestUnanchoredMultiComponent(t *testing.T) {
	m := New()
	m.rules = []rule{
		{base: "", pattern: pattern{raw: "foo/bar"}},
	}

	if !m.Match("foo/bar", false) {
		t.Error("foo/bar should match at root")
	}
	if !m.Match("x/foo/bar", false) {
		t.Error("foo/bar unanchored should match x/foo/bar")
	}
	if m.Match("foo/baz", false) {
		t.Error("foo/baz should not match")
	}
}

func TestScanDirFindsIgnoreFiles(t *testing.T) {
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

	if !m.Match("error.log", false) {
		t.Error("*.log should match in tar root")
	}
	if !m.Match("src/debug.log", false) {
		t.Error("*.log should match nested")
	}
	if !m.Match("tmp", true) {
		t.Error("tmp/ should match as dir")
	}
	if m.Match("tmp", false) {
		t.Error("tmp should not match as file (dirOnly)")
	}
	if m.Match("src/app.go", false) {
		t.Error("app.go should not be ignored")
	}
}

func TestScanDirSubdirIgnore(t *testing.T) {
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
