package envfile

import (
	"bufio"
	"bytes"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"Draft/internal/store"
)

func Read(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()

	values := map[string]string{}
	scanner := bufio.NewScanner(f)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if idx := strings.Index(line, "="); idx != -1 {
			key := strings.TrimSpace(line[:idx])
			val := strings.TrimSpace(line[idx+1:])
			if key != "" {
				if strings.HasPrefix(val, "{") && braceDelta(val) > 0 {
					var b strings.Builder
					b.WriteString(val)
					balance := braceDelta(val)
					for balance > 0 && scanner.Scan() {
						next := scanner.Text()
						b.WriteByte('\n')
						b.WriteString(next)
						balance += braceDelta(next)
					}
					if err := scanner.Err(); err != nil {
						return nil, err
					}
					val = b.String()
				}
				values[key] = val
			}
		}
	}
	return values, scanner.Err()
}

func braceDelta(line string) int {
	delta := 0
	inString := false
	escaped := false
	for _, r := range line {
		if escaped {
			escaped = false
			continue
		}
		if r == '\\' {
			escaped = true
			continue
		}
		if r == '"' {
			inString = !inString
			continue
		}
		if inString {
			continue
		}
		switch r {
		case '{':
			delta++
		case '}':
			delta--
		}
	}
	return delta
}

// Write serializes vars to a .env file at path. Stored values are written
// as-is (reference tokens like {{secret.KEY}} and {{project.*}} are preserved literally).
func Write(path string, vars []store.EnvVar) (int, error) {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return 0, err
	}

	keys := make([]string, 0, len(vars))
	values := make(map[string]string, len(vars))
	for _, v := range vars {
		if strings.TrimSpace(v.Key) == "" {
			continue
		}
		keys = append(keys, v.Key)
		values[v.Key] = v.Value
	}
	sort.Strings(keys)

	var b bytes.Buffer
	for _, key := range keys {
		fmt.Fprintf(&b, "%s=%s\n", key, values[key])
	}
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		return 0, err
	}
	return len(keys), nil
}
