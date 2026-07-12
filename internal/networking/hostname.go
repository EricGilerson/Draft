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

const (
	Suffix       = "draft.local"
	PublicSuffix = "draft.resolv.sh"
	// LocalSuffix is the optional, machine-local public hostname suffix. It
	// deliberately remains separate from Suffix: Docker keeps using
	// *.draft.local internally, while host DNS may opt into *.draft.
	LocalSuffix = "draft"
)

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

func PublicHostname(hostname string) string {
	hostname = strings.TrimSuffix(hostname, ".")
	if strings.HasSuffix(hostname, "."+Suffix) {
		return strings.TrimSuffix(hostname, "."+Suffix) + "." + PublicSuffix
	}
	return hostname
}

// LocalHostname returns the optional local-DNS alias for an internal Draft
// hostname. Resolution is installed only when the user enables local domains.
func LocalHostname(hostname string) string {
	hostname = strings.TrimSuffix(hostname, ".")
	if strings.HasSuffix(hostname, "."+Suffix) {
		return strings.TrimSuffix(hostname, "."+Suffix) + "." + LocalSuffix
	}
	return hostname
}

func HostAliases(hostname string) []string {
	public := PublicHostname(hostname)
	local := LocalHostname(hostname)
	aliases := []string{hostname}
	if local != hostname {
		aliases = append(aliases, local)
	}
	if public != hostname && public != local {
		aliases = append(aliases, public)
	}
	return aliases
}

func InternalURL(hostname, port string) string {
	if hostname == "" || port == "" {
		return ""
	}
	return fmt.Sprintf("http://%s:%s", hostname, port)
}

func PublicURL(hostname string, proxyPort int) string {
	if hostname == "" || proxyPort <= 0 {
		return ""
	}
	if proxyPort == 80 {
		return fmt.Sprintf("http://%s", PublicHostname(hostname))
	}
	return fmt.Sprintf("http://%s:%d", PublicHostname(hostname), proxyPort)
}

// IsTCPProtocol reports whether the route protocol is TCP (stable host port,
// not the HTTP reverse proxy). Empty or unknown values are treated as HTTP.
func IsTCPProtocol(protocol string) bool {
	return strings.EqualFold(strings.TrimSpace(protocol), "tcp")
}

// PublicTCPEndpoint is the host-facing address for a TCP service: the public
// Draft hostname and the leased host port. No URL scheme — wire clients use
// their own (postgres://, redis://, …) with this host:port pair. DNS for
// *.draft.resolv.sh resolves to loopback.
func PublicTCPEndpoint(hostname string, hostPort int) string {
	if hostname == "" || hostPort <= 0 {
		return ""
	}
	return fmt.Sprintf("%s:%d", PublicHostname(hostname), hostPort)
}

// InternalTCPEndpoint is the container-network address for a TCP service
// (internal hostname + container port), without an HTTP scheme.
func InternalTCPEndpoint(hostname, port string) string {
	if hostname == "" || port == "" {
		return ""
	}
	return fmt.Sprintf("%s:%s", hostname, port)
}

// ServicePublicURL returns the best public access string for a service.
// HTTP: http://{publicHostname}:{proxyPort}
// TCP:  {publicHostname}:{hostPort}  (no scheme)
func ServicePublicURL(hostname string, proxyPort, hostPort int, protocol string) string {
	if IsTCPProtocol(protocol) {
		return PublicTCPEndpoint(hostname, hostPort)
	}
	return PublicURL(hostname, proxyPort)
}

// ServiceInternalURL returns the best internal access string for a service.
// HTTP: http://{hostname}:{port}
// TCP:  {hostname}:{port} with no scheme (wire clients / @{refs}; never http://)
func ServiceInternalURL(hostname, port, protocol string) string {
	if IsTCPProtocol(protocol) {
		return InternalTCPEndpoint(hostname, port)
	}
	return InternalURL(hostname, port)
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
