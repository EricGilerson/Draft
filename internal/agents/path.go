package agents

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
)

// AugmentedPATH returns PATH with common user bin dirs prepended so GUI-launched
// Draft (especially on macOS) can still resolve claude/codex/etc without admin.
func AugmentedPATH() string {
	pathEnv := "PATH"
	if runtime.GOOS == "windows" {
		pathEnv = "Path"
		if v := os.Getenv("Path"); v == "" {
			pathEnv = "PATH"
		}
	}
	current := os.Getenv(pathEnv)
	extras := candidateBinDirs()
	seen := map[string]bool{}
	var parts []string
	for _, p := range extras {
		if p == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(p))
		if seen[key] {
			continue
		}
		if st, err := os.Stat(p); err != nil || !st.IsDir() {
			continue
		}
		seen[key] = true
		parts = append(parts, p)
	}
	for _, p := range filepath.SplitList(current) {
		if p == "" {
			continue
		}
		key := strings.ToLower(filepath.Clean(p))
		if seen[key] {
			continue
		}
		seen[key] = true
		parts = append(parts, p)
	}
	return strings.Join(parts, string(os.PathListSeparator))
}

func candidateBinDirs() []string {
	home, _ := os.UserHomeDir()
	var dirs []string
	if home != "" {
		dirs = append(dirs,
			filepath.Join(home, ".local", "bin"),
			filepath.Join(home, "bin"),
			filepath.Join(home, ".cargo", "bin"),
			filepath.Join(home, "go", "bin"),
			filepath.Join(home, ".npm-global", "bin"),
			filepath.Join(home, "AppData", "Roaming", "npm"),
			filepath.Join(home, "AppData", "Local", "Programs"),
			filepath.Join(home, ".claude", "bin"),
			filepath.Join(home, ".codex", "bin"),
			filepath.Join(home, ".opencode", "bin"),
			filepath.Join(home, ".local", "share", "opencode", "bin"),
		)
		// nvm / fnm style (best-effort: only top-level current)
		dirs = append(dirs,
			filepath.Join(home, ".nvm", "current", "bin"),
			filepath.Join(home, ".fnm", "current", "bin"),
			filepath.Join(home, "Library", "Application Support", "fnm", "aliases", "default", "bin"),
		)
	}
	if runtime.GOOS == "darwin" {
		dirs = append(dirs,
			"/opt/homebrew/bin",
			"/usr/local/bin",
			"/opt/homebrew/sbin",
		)
	}
	if runtime.GOOS == "windows" {
		dirs = append(dirs,
			`C:\Program Files\nodejs`,
			`C:\Program Files\Git\cmd`,
		)
		if home != "" {
			dirs = append(dirs,
				filepath.Join(home, "AppData", "Local", "Microsoft", "WinGet", "Links"),
				filepath.Join(home, "scoop", "shims"),
			)
		}
	}
	return dirs
}

// LookPath finds binary on the augmented PATH (does not require admin).
func LookPath(name string) (string, error) {
	oldPath := os.Getenv("PATH")
	oldWinPath := os.Getenv("Path")
	aug := AugmentedPATH()
	_ = os.Setenv("PATH", aug)
	if runtime.GOOS == "windows" {
		_ = os.Setenv("Path", aug)
	}
	defer func() {
		_ = os.Setenv("PATH", oldPath)
		if runtime.GOOS == "windows" {
			_ = os.Setenv("Path", oldWinPath)
		}
	}()
	return exec.LookPath(name)
}

// childEnv returns os.Environ with PATH augmented for spawned agent processes.
func childEnv() []string {
	aug := AugmentedPATH()
	env := os.Environ()
	out := make([]string, 0, len(env)+1)
	replaced := false
	for _, e := range env {
		switch {
		case strings.HasPrefix(e, "PATH="), strings.HasPrefix(e, "Path="):
			if !replaced {
				out = append(out, "PATH="+aug)
				if runtime.GOOS == "windows" {
					out = append(out, "Path="+aug)
				}
				replaced = true
			}
		default:
			out = append(out, e)
		}
	}
	if !replaced {
		out = append(out, "PATH="+aug)
		if runtime.GOOS == "windows" {
			out = append(out, "Path="+aug)
		}
	}
	return out
}
