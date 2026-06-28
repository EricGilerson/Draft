package ignore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type Pattern struct {
	pattern  string
	negate   bool
	dirOnly  bool
	anchored bool // contains a slash → only matches from the base dir
}

type Matcher struct {
	rules []rule
}

type rule struct {
	base    string // directory containing the ignore file, relative to tar root ("" = root)
	pattern Pattern
}

func New() *Matcher {
	return &Matcher{}
}

func (m *Matcher) Empty() bool {
	return len(m.rules) == 0
}

// AddFile parses an ignore file (.gitignore or .dockerignore) and registers
// its patterns. base is the directory containing the file relative to the tar
// root; patterns are matched relative to it. Use "" for the tar root itself or
// for parent directories (their patterns apply everywhere).
func (m *Matcher) AddFile(path, base string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := scanner.Text()
		p := parseLine(line)
		if p == nil {
			continue
		}
		m.rules = append(m.rules, rule{base: filepath.ToSlash(base), pattern: *p})
	}
	return scanner.Err()
}

// Match returns true if the given path (forward-slash separated, relative to
// the tar root) should be ignored. isDir indicates whether the path is a
// directory.
func (m *Matcher) Match(rel string, isDir bool) bool {
	matched := false
	for _, r := range m.rules {
		if r.pattern.dirOnly && !isDir {
			continue
		}

		// The path to test against this rule is relative to the rule's base.
		testPath := rel
		if r.base != "" && r.base != "." {
			if !strings.HasPrefix(rel, r.base+"/") {
				continue
			}
			testPath = rel[len(r.base)+1:]
		}

		if matchPattern(r.pattern, testPath) {
			matched = !r.pattern.negate
		}
	}
	return matched
}

func parseLine(line string) *Pattern {
	line = strings.TrimRight(line, " \t\r")
	trimmed := strings.TrimLeft(line, " \t")
	if trimmed == "" || trimmed[0] == '#' {
		return nil
	}
	line = trimmed

	p := Pattern{}
	if line[0] == '!' {
		p.negate = true
		line = line[1:]
	}
	if strings.HasPrefix(line, `\#`) || strings.HasPrefix(line, `\!`) {
		line = line[1:]
	}
	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimRight(line, "/")
	}
	if strings.Contains(line, "/") {
		p.anchored = true
		line = strings.TrimPrefix(line, "/")
	}

	p.pattern = line
	return &p
}

func matchPattern(p Pattern, path string) bool {
	pattern := p.pattern

	if p.anchored {
		return matchGlob(pattern, path)
	}

	// Unanchored patterns match against the full path or the basename.
	if matchGlob(pattern, path) {
		return true
	}
	base := path
	if idx := strings.LastIndex(path, "/"); idx >= 0 {
		base = path[idx+1:]
	}
	return matchGlob(pattern, base)
}

func matchGlob(pattern, name string) bool {
	if strings.Contains(pattern, "**") {
		return matchDoublestar(pattern, name)
	}
	matched, _ := filepath.Match(pattern, name)
	return matched
}

func matchDoublestar(pattern, name string) bool {
	parts := strings.SplitN(pattern, "**", 2)
	prefix := parts[0]
	suffix := ""
	if len(parts) > 1 {
		suffix = strings.TrimPrefix(parts[1], "/")
	}

	if prefix != "" {
		prefix = strings.TrimSuffix(prefix, "/")
		if !strings.HasPrefix(name, prefix) {
			return false
		}
		if len(name) == len(prefix) {
			name = ""
		} else if name[len(prefix)] == '/' {
			name = name[len(prefix)+1:]
		} else {
			return false
		}
	}

	if suffix == "" {
		return true
	}

	for {
		if matchGlob(suffix, name) {
			return true
		}
		idx := strings.Index(name, "/")
		if idx < 0 {
			return false
		}
		name = name[idx+1:]
	}
}

// ScanDir walks scanRoot looking for files named target (e.g. ".gitignore" or
// ".dockerignore") and adds each one to m. tarRoot is the directory that will
// be tarred; pattern bases are computed relative to it.
//
// Ignore files found in parent directories of tarRoot (between scanRoot and
// tarRoot) get base "" so their patterns apply to the entire tar tree.
//
// scanRoot must be an ancestor of (or equal to) tarRoot.
func (m *Matcher) ScanDir(scanRoot, tarRoot, target string) (int, error) {
	scanRoot = filepath.Clean(scanRoot)
	tarRoot = filepath.Clean(tarRoot)

	count := 0
	err := filepath.Walk(scanRoot, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		name := info.Name()

		if info.IsDir() {
			switch name {
			case ".git", "node_modules", ".next", "__pycache__", ".venv":
				return filepath.SkipDir
			}
			return nil
		}

		if name != target {
			return nil
		}

		ignoreDir := filepath.Clean(filepath.Dir(path))
		var base string

		// Determine the base relative to tarRoot.
		rel, relErr := filepath.Rel(tarRoot, ignoreDir)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		if rel == "." {
			// Ignore file is in the tar root itself.
			base = ""
		} else if strings.HasPrefix(rel, "..") {
			// Ignore file is in a parent of tarRoot — patterns apply globally.
			base = ""
		} else {
			// Ignore file is inside tarRoot.
			base = rel
		}

		if err := m.AddFile(path, base); err != nil {
			return nil
		}
		count++
		return nil
	})
	return count, err
}
