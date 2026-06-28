// Package networking provides Draft's local service routing infrastructure:
// hostname generation, hosts file management, port allocation, and an HTTP
// reverse proxy. Nothing in this package touches Docker directly — it operates
// on hostnames, ports, and the local network stack.
package networking

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

const Suffix = "draft.local"

var unsafeChars = regexp.MustCompile(`[^a-z0-9-]`)

// sanitize lowercases a name, replaces unsafe characters with hyphens, collapses
// runs of hyphens, and trims leading/trailing hyphens. Returns "untitled" if the
// result is empty.
func sanitize(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	s = unsafeChars.ReplaceAllString(s, "-")
	s = regexp.MustCompile(`-{2,}`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "untitled"
	}
	return s
}

// GenerateUID returns a random 4-character hex string (2 bytes).
func GenerateUID() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Hostname builds a Draft-managed hostname from the component parts.
// Pattern: {service}.{project}.{environment}.{uid}.draft.local
func Hostname(service, project, environment, uid string) string {
	return fmt.Sprintf("%s.%s.%s.%s.%s",
		sanitize(service),
		sanitize(project),
		sanitize(environment),
		uid,
		Suffix,
	)
}

// ParsedHostname holds the decoded parts of a Draft hostname.
type ParsedHostname struct {
	Service     string
	Project     string
	Environment string
	UID         string
}

// ParseHostname splits a Draft hostname into its parts. Returns nil if the
// hostname doesn't match the expected pattern.
func ParseHostname(hostname string) *ParsedHostname {
	hostname = strings.TrimSuffix(hostname, ".")
	if !strings.HasSuffix(hostname, "."+Suffix) {
		return nil
	}
	prefix := strings.TrimSuffix(hostname, "."+Suffix)
	parts := strings.SplitN(prefix, ".", 4)
	if len(parts) != 4 {
		return nil
	}
	return &ParsedHostname{
		Service:     parts[0],
		Project:     parts[1],
		Environment: parts[2],
		UID:         parts[3],
	}
}
