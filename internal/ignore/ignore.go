package ignore

import (
	"bufio"
	"os"
	"path/filepath"
	"strings"
)

type pattern struct {
	raw      string
	negate   bool
	dirOnly  bool
	anchored bool
}

type rule struct {
	base    string // directory containing the ignore file, relative to tar root
	pattern pattern
}

// Matcher evaluates .gitignore / .dockerignore patterns against paths.
type Matcher struct {
	rules []rule
}

// New creates an empty Matcher.
func New() *Matcher {
	return &Matcher{}
}

// Empty reports whether the matcher has any rules loaded.
func (m *Matcher) Empty() bool {
	return len(m.rules) == 0
}

// AddFile parses an ignore file and registers its patterns. base is the
// directory containing the file relative to the tar root; use "" for the tar
// root itself or for parent directories (patterns apply everywhere).
func (m *Matcher) AddFile(path, base string) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()

	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		p := parseLine(scanner.Text())
		if p == nil {
			continue
		}
		m.rules = append(m.rules, rule{base: filepath.ToSlash(base), pattern: *p})
	}
	return scanner.Err()
}

// Match returns true if rel (forward-slash-separated, relative to the tar
// root) should be excluded. isDir indicates whether the entry is a directory.
func (m *Matcher) Match(rel string, isDir bool) bool {
	matched := false
	for _, r := range m.rules {
		if r.pattern.dirOnly && !isDir {
			continue
		}

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

// ---- parsing ----

func parseLine(line string) *pattern {
	// Trailing whitespace is stripped unless escaped with \.
	line = trimTrailingUnescapedSpaces(line)

	line = strings.TrimLeft(line, " \t")
	if line == "" || line[0] == '#' {
		return nil
	}

	p := &pattern{}

	if line[0] == '!' {
		p.negate = true
		line = line[1:]
	}

	// Leading \# or \! escapes.
	if len(line) >= 2 && line[0] == '\\' && (line[1] == '#' || line[1] == '!') {
		line = line[1:]
	}

	if strings.HasSuffix(line, "/") {
		p.dirOnly = true
		line = strings.TrimRight(line, "/")
	}

	// A pattern containing a slash (other than a trailing one, already
	// stripped above) is anchored to the base directory.
	if strings.Contains(line, "/") {
		p.anchored = true
		line = strings.TrimPrefix(line, "/")
	}

	p.raw = line
	return p
}

func trimTrailingUnescapedSpaces(s string) string {
	end := len(s)
	for end > 0 && s[end-1] == ' ' {
		if end >= 2 && s[end-2] == '\\' {
			// escaped space — replace the backslash-space with just a space
			s = s[:end-2] + " "
			end--
			break
		}
		end--
	}
	return s[:end]
}

// ---- matching ----

func matchPattern(p pattern, path string) bool {
	pat := p.raw

	if p.anchored {
		return globMatch(pat, path)
	}

	// Unanchored: try matching against every suffix of the path.
	// "foo" matches "foo", "a/foo", "a/b/foo".
	// "*.txt" matches "a.txt", "dir/b.txt".
	if globMatch(pat, path) {
		return true
	}
	for i := 0; i < len(path); i++ {
		if path[i] == '/' {
			if globMatch(pat, path[i+1:]) {
				return true
			}
		}
	}
	return false
}

// globMatch matches a gitignore glob pattern against a forward-slash path.
// Unlike filepath.Match, * never matches /, and ** is supported.
func globMatch(pat, name string) bool {
	// Fast-path for trivial patterns.
	if pat == "" {
		return name == ""
	}

	for len(pat) > 0 {
		switch pat[0] {
		case '*':
			if len(pat) >= 2 && pat[1] == '*' {
				return matchDoublestar(pat, name)
			}
			// Single * — match any characters except /.
			pat = pat[1:]
			// If nothing left in pattern, * must match the rest (which must
			// not contain /).
			if pat == "" {
				return !strings.Contains(name, "/")
			}
			// Try every position in name (up to the next /) as the end of
			// the * match.
			for i := 0; i <= len(name); i++ {
				if i > 0 && name[i-1] == '/' {
					return false
				}
				if globMatch(pat, name[i:]) {
					return true
				}
			}
			return false

		case '?':
			if len(name) == 0 || name[0] == '/' {
				return false
			}
			pat = pat[1:]
			name = name[1:]

		case '[':
			if len(name) == 0 || name[0] == '/' {
				return false
			}
			ok, width := matchCharClass(pat, name[0])
			if width == 0 {
				return false // malformed class
			}
			if !ok {
				return false
			}
			pat = pat[width:]
			name = name[1:]

		case '\\':
			// Escape next character (treat as literal).
			pat = pat[1:]
			if len(pat) == 0 {
				return false
			}
			if len(name) == 0 || name[0] != pat[0] {
				return false
			}
			pat = pat[1:]
			name = name[1:]

		default:
			if len(name) == 0 || name[0] != pat[0] {
				return false
			}
			pat = pat[1:]
			name = name[1:]
		}
	}
	return name == ""
}

// matchCharClass parses a [...] bracket expression starting at pat[0]=='['
// and checks whether ch is in the class. Returns (matched, patternWidth).
// patternWidth is 0 on malformed input.
func matchCharClass(pat string, ch byte) (bool, int) {
	if len(pat) < 2 || pat[0] != '[' {
		return false, 0
	}
	i := 1
	negate := false
	if i < len(pat) && (pat[i] == '!' || pat[i] == '^') {
		negate = true
		i++
	}
	matched := false
	first := true
	for i < len(pat) {
		if pat[i] == ']' && !first {
			i++
			return matched != negate, i
		}
		first = false

		lo := pat[i]
		i++
		if i+1 < len(pat) && pat[i] == '-' && pat[i+1] != ']' {
			hi := pat[i+1]
			i += 2
			if lo <= ch && ch <= hi {
				matched = true
			}
		} else {
			if ch == lo {
				matched = true
			}
		}
	}
	return false, 0 // no closing ]
}

// matchDoublestar handles ** which matches zero or more complete path segments.
func matchDoublestar(pat, name string) bool {
	// Consume the **.
	pat = pat[2:]

	// ** at end of pattern matches everything.
	if pat == "" {
		return true
	}

	// Strip the / after ** (e.g. **/ or a/**/b).
	pat = strings.TrimPrefix(pat, "/")

	// Try matching the remainder against every suffix of name.
	if globMatch(pat, name) {
		return true
	}
	for i := 0; i < len(name); i++ {
		if name[i] == '/' {
			if globMatch(pat, name[i+1:]) {
				return true
			}
		}
	}
	return false
}

// ---- scanning ----

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

		if info.IsDir() {
			switch info.Name() {
			case ".git", "node_modules", ".next", "__pycache__", ".venv":
				return filepath.SkipDir
			}
			return nil
		}

		if info.Name() != target {
			return nil
		}

		ignoreDir := filepath.Clean(filepath.Dir(path))
		rel, relErr := filepath.Rel(tarRoot, ignoreDir)
		if relErr != nil {
			return nil
		}
		rel = filepath.ToSlash(rel)

		var base string
		if rel == "." || strings.HasPrefix(rel, "..") {
			base = ""
		} else {
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
