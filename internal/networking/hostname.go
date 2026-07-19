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
	// SandboxKindLabel is the fixed DNS / Docker identity marker for sandbox
	// environments. Hostnames insert it as its own label
	// ({service}.{project}.sand.{slug}.{uid}.draft.local); Docker network,
	// image, container, and volume names use sand-{slug} as the env segment.
	// Environment slugs stay project-global unique (including sandboxes).
	SandboxKindLabel = "sand"
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

// Hostname builds a Draft-managed hostname for a normal (non-sandbox)
// environment.
// Pattern: {service}.{project}.{environment}.{uid}.draft.local
func Hostname(service, project, environment, uid string) string {
	return FormatHostname(service, project, environment, uid, false)
}

// FormatHostname builds a Draft-managed hostname. When sandbox is true the
// fixed sand label is inserted so sandbox DNS cannot be mistaken for a durable
// environment, even when the env slug string matches a real env name:
//
//	normal:  {service}.{project}.{environment}.{uid}.draft.local
//	sandbox: {service}.{project}.sand.{environment}.{uid}.draft.local
func FormatHostname(service, project, environment, uid string, sandbox bool) string {
	service = sanitize(service)
	project = sanitize(project)
	environment = sanitize(environment)
	if sandbox {
		return fmt.Sprintf("%s.%s.%s.%s.%s.%s",
			service, project, SandboxKindLabel, environment, uid, Suffix)
	}
	return fmt.Sprintf("%s.%s.%s.%s.%s",
		service, project, environment, uid, Suffix)
}

// DockerEnvironment is the single environment segment used in Docker network,
// image, container, and managed volume names. Sandboxes use sand-{slug} so
// operators can distinguish them in docker ps / network ls; slugs themselves
// remain unique per project so this is clarity, not a second uniqueness space.
func DockerEnvironment(slug string, sandbox bool) string {
	raw := strings.TrimSpace(slug)
	if raw == "" {
		if sandbox {
			return SandboxKindLabel + "-default"
		}
		return "default"
	}
	slug = sanitize(raw)
	if sandbox {
		return SandboxKindLabel + "-" + slug
	}
	return slug
}

func PublicHostname(hostname string) string {
	hostname = strings.TrimSuffix(hostname, ".")
	if strings.HasSuffix(hostname, "."+Suffix) {
		return strings.TrimSuffix(hostname, "."+Suffix) + "." + PublicSuffix
	}
	return hostname
}

// InternalHostnameFromAlias maps a public (*.draft.resolv.sh) or local
// (*.draft) alias back to the canonical *.draft.local hostname used in the
// routes table. Unknown hosts are returned unchanged.
func InternalHostnameFromAlias(hostname string) string {
	hostname = strings.ToLower(strings.TrimSuffix(strings.TrimSpace(hostname), "."))
	if strings.HasSuffix(hostname, "."+Suffix) {
		return hostname
	}
	if strings.HasSuffix(hostname, "."+PublicSuffix) {
		return strings.TrimSuffix(hostname, "."+PublicSuffix) + "." + Suffix
	}
	if strings.HasSuffix(hostname, "."+LocalSuffix) && !strings.HasSuffix(hostname, "."+Suffix) {
		return strings.TrimSuffix(hostname, "."+LocalSuffix) + "." + Suffix
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
	Sandbox     bool
}

// ParseHostname splits a Draft hostname into its parts. Returns nil if the
// hostname doesn't match the expected pattern. Accepts both normal 4-label
// hostnames and sandbox 5-label hostnames with a sand segment.
func ParseHostname(hostname string) *ParsedHostname {
	hostname = strings.TrimSuffix(hostname, ".")
	if !strings.HasSuffix(hostname, "."+Suffix) {
		return nil
	}
	prefix := strings.TrimSuffix(hostname, "."+Suffix)
	parts := strings.Split(prefix, ".")
	switch len(parts) {
	case 4:
		return &ParsedHostname{
			Service:     parts[0],
			Project:     parts[1],
			Environment: parts[2],
			UID:         parts[3],
		}
	case 5:
		if parts[2] != SandboxKindLabel {
			return nil
		}
		return &ParsedHostname{
			Service:     parts[0],
			Project:     parts[1],
			Environment: parts[3],
			UID:         parts[4],
			Sandbox:     true,
		}
	default:
		return nil
	}
}
