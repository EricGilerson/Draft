package draftpack

import (
	"path/filepath"
	"strings"
)

// settings always stripped on export (machine-local caches / runtime).
var alwaysOmitSettings = map[string]bool{
	"git_repo_root": true,
}

// git-related settings controlled by IncludeGitSettings.
var gitSettings = map[string]bool{
	"git_branch":       true,
	"deploy_trigger":   true,
	"redeploy_on_pull": true,
	"git_stream":       true,
}

// source_config keys controlled by IncludeSourceConfig.
var sourceConfigSettings = map[string]bool{
	"source_config":        true,
	"source_config_format": true,
}

// IsProjectRelative reports whether p looks like a safe project-relative path
// (not absolute, not a Windows drive path, does not escape via "..").
func IsProjectRelative(p string) bool {
	p = strings.TrimSpace(p)
	if p == "" {
		return false
	}
	if filepath.IsAbs(p) {
		return false
	}
	// Windows drive or UNC even if filepath.IsAbs is false on the other OS.
	if len(p) >= 2 && p[1] == ':' {
		return false
	}
	if strings.HasPrefix(p, `\\`) || strings.HasPrefix(p, "//") {
		return false
	}
	// Reject parent traversal so Rel() results like ..\other are not treated as portable.
	clean := filepath.ToSlash(filepath.Clean(p))
	if clean == ".." || strings.HasPrefix(clean, "../") {
		return false
	}
	return true
}

// NormalizeRelative cleans a relative path for storage (slash-separated).
func NormalizeRelative(p string) string {
	p = strings.TrimSpace(p)
	p = strings.TrimPrefix(p, "./")
	p = strings.TrimPrefix(p, ".\\")
	if p == "." || p == "" {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(p))
}

// PathHint returns the last non-empty path segment of an absolute project path.
func PathHint(projectPath string) string {
	projectPath = strings.TrimSpace(projectPath)
	if projectPath == "" {
		return ""
	}
	base := filepath.Base(filepath.Clean(projectPath))
	if base == "." || base == string(filepath.Separator) {
		return ""
	}
	return base
}

// BindOverrideKey builds the map key for a bind path override.
func BindOverrideKey(serviceKey, containerPath string) string {
	return serviceKey + "|" + containerPath
}
