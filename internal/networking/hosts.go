package networking

import (
	"fmt"
	"os"
	"runtime"
	"strings"
)

const (
	markerStart = "# >>> Draft managed — do not edit <<<"
	markerEnd   = "# <<< Draft managed >>>"
)

// hostsFilePath returns the OS-appropriate hosts file location.
func hostsFilePath() string {
	if runtime.GOOS == "windows" {
		return `C:\Windows\System32\drivers\etc\hosts`
	}
	return "/etc/hosts"
}

// HostsEntry is a single line in the managed block: IP → hostname.
type HostsEntry struct {
	IP       string
	Hostname string
}

// SyncHostsFile reads the system hosts file, replaces the Draft-managed block
// with the given entries, and writes it back. Entries outside the markers are
// untouched.
//
// Requires elevated privileges (admin on Windows, root on macOS/Linux).
// Returns an error describing the permission issue if the write fails.
func SyncHostsFile(entries []HostsEntry) error {
	path := hostsFilePath()
	return syncHostsFileAt(path, entries)
}

// syncHostsFileAt is the testable core — operates on an arbitrary file path.
func syncHostsFileAt(path string, entries []HostsEntry) error {
	existing, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read hosts file: %w", err)
	}

	updated := replaceBlock(string(existing), entries)

	if err := os.WriteFile(path, []byte(updated), 0o644); err != nil {
		return fmt.Errorf("write hosts file (elevated privileges required): %w", err)
	}
	return nil
}

// replaceBlock replaces (or appends) the Draft-managed block in the hosts file
// content. If the block doesn't exist yet, it's appended.
func replaceBlock(content string, entries []HostsEntry) string {
	block := buildBlock(entries)

	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)

	if startIdx >= 0 && endIdx >= 0 && endIdx > startIdx {
		before := content[:startIdx]
		after := content[endIdx+len(markerEnd):]
		after = strings.TrimLeft(after, "\r\n")
		result := before + block
		if after != "" {
			result += "\n" + after
		}
		return result
	}

	content = strings.TrimRight(content, "\r\n")
	if content != "" {
		content += "\n\n"
	}
	return content + block + "\n"
}

// buildBlock formats the managed section.
func buildBlock(entries []HostsEntry) string {
	if len(entries) == 0 {
		return markerStart + "\n" + markerEnd
	}

	var b strings.Builder
	b.WriteString(markerStart)
	b.WriteByte('\n')
	for _, e := range entries {
		ip := e.IP
		if ip == "" {
			ip = "127.0.0.1"
		}
		fmt.Fprintf(&b, "%s  %s\n", ip, e.Hostname)
	}
	b.WriteString(markerEnd)
	return b.String()
}

// ReadManagedEntries reads the current Draft-managed entries from the hosts file.
func ReadManagedEntries() ([]HostsEntry, error) {
	return readManagedEntriesAt(hostsFilePath())
}

func readManagedEntriesAt(path string) ([]HostsEntry, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	return parseManagedBlock(string(data)), nil
}

// parseManagedBlock extracts entries from the managed block.
func parseManagedBlock(content string) []HostsEntry {
	startIdx := strings.Index(content, markerStart)
	endIdx := strings.Index(content, markerEnd)
	if startIdx < 0 || endIdx < 0 || endIdx <= startIdx {
		return nil
	}

	block := content[startIdx+len(markerStart) : endIdx]
	var entries []HostsEntry
	for _, line := range strings.Split(block, "\n") {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			entries = append(entries, HostsEntry{IP: fields[0], Hostname: fields[1]})
		}
	}
	return entries
}
