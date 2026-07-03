package deploy

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"strings"
)

// Template default values (and command/entrypoint overrides) may carry
// {{draft.X}} placeholder expressions. Draft expands them to values derived
// from the same identity tuple that produces the service hostname
// (service.project.environment.uid), so two Postgres nodes in different
// projects or environments never collide on db name / user / password, and a
// node's own address is authorable without knowing its UID up front.
//
// Expansion runs in two places: at stamp time when a node is created from a
// template (concrete values get written into env_vars), and in the live env
// resolver so a manually-typed {{draft.password}} previews and exports
// correctly today. Both call the same resolveTemplateExprs.
//
// The derived password/uuid are local-dev defaults, NOT production secrets —
// the salt is a fixed app constant so values are reproducible across machines,
// which is the point for a local-first tool.

var exprPattern = regexp.MustCompile(`\{\{draft\.([A-Za-z_]+)\}\}`)

// appSalt is the fixed HMAC key for {{draft.password}}. Local-dev only; a
// per-installation random salt is a later hardening step.
var appSalt = []byte("draft-local-dev-v1")

// templateExprInput is the identity + address a single expansion runs against.
// The address fields mirror the DRAFT_* vars Draft injects at deploy time, so
// {{draft.internal_hostname}} reads exactly the same as DRAFT_INTERNAL_HOSTNAME.
type templateExprInput struct {
	ServiceName      string // sanitized service label
	ProjectName      string // sanitized project name
	Environment      string // "default" when unset
	UID              string // node's permanent UID
	ServicePort      string // configured container port
	InternalHostname string
	InternalURL      string
	PublicHostname   string
	PublicURL        string

	// UUIDFunc overrides the default crypto/rand uuid generator; tests use it
	// for determinism. nil => generateUUID.
	UUIDFunc func() string
}

// exprInputFromAddress builds an input from a computed NodeAddress plus uid.
func exprInputFromAddress(addr NodeAddress, uid string) templateExprInput {
	return templateExprInput{
		ServiceName:      addr.ServiceName,
		ProjectName:      addr.ProjectName,
		Environment:      addr.Environment,
		UID:              uid,
		ServicePort:      addr.ServicePort,
		InternalHostname: addr.InternalHostname,
		InternalURL:      addr.InternalURL,
		PublicHostname:   addr.PublicHostname,
		PublicURL:        addr.PublicURL,
	}
}

// resolveTemplateExprs substitutes every {{draft.X}} token in raw. An unknown
// token name is an error rather than silently passing through, so a typo in a
// template doesn't ship a literal {{draft.foo}} into a container env var.
func resolveTemplateExprs(in templateExprInput, raw string) (string, error) {
	if !strings.Contains(raw, "{{draft.") {
		return raw, nil
	}
	var out strings.Builder
	last := 0
	for _, m := range exprPattern.FindAllStringSubmatchIndex(raw, -1) {
		out.WriteString(raw[last:m[0]])
		name := raw[m[2]:m[3]]
		val, err := resolveExprToken(in, name)
		if err != nil {
			return "", err
		}
		out.WriteString(val)
		last = m[1]
	}
	out.WriteString(raw[last:])
	return out.String(), nil
}

func resolveExprToken(in templateExprInput, name string) (string, error) {
	switch name {
	case "service":
		return in.ServiceName, nil
	case "project":
		return in.ProjectName, nil
	case "environment":
		if in.Environment == "" {
			return "default", nil
		}
		return in.Environment, nil
	case "uid":
		return in.UID, nil
	case "db_name":
		return deriveDbName(in.ProjectName, in.Environment, in.ServiceName), nil
	case "db_user":
		return deriveDbUser(in.ProjectName, in.Environment, in.ServiceName), nil
	case "password":
		return derivePassword(in.ProjectName, in.Environment, in.ServiceName, in.UID), nil
	case "uuid":
		if in.UUIDFunc != nil {
			return in.UUIDFunc(), nil
		}
		return generateUUID(), nil
	case "internal_hostname":
		return in.InternalHostname, nil
	case "internal_url":
		return in.InternalURL, nil
	case "public_hostname":
		return in.PublicHostname, nil
	case "public_url":
		return in.PublicURL, nil
	case "service_port":
		return in.ServicePort, nil
	default:
		return "", fmt.Errorf("unknown draft expression %q", name)
	}
}

// derivePassword returns a deterministic 24-char hex string keyed on the node's
// full identity. Same project/env/service/uid => same password across deploys
// and machines; any one of those differing => a different password, so sibling
// DBs never share credentials.
func derivePassword(project, env, service, uid string) string {
	h := hmac.New(sha256.New, appSalt)
	fmt.Fprintf(h, "%s|%s|%s|%s|password", project, env, service, uid)
	return hex.EncodeToString(h.Sum(nil))[:24]
}

// deriveIdentifier joins parts with underscores, keeps [a-z0-9_], collapses
// runs of underscores, and guarantees a leading letter (Postgres/MySQL
// identifiers shouldn't start with a digit). Empty result falls back to
// "draft" so a template never produces a blank db name / user.
func deriveIdentifier(parts ...string) string {
	s := strings.ToLower(strings.Join(parts, "_"))
	s = strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '_' {
			return r
		}
		return '_'
	}, s)
	s = regexp.MustCompile(`_{2,}`).ReplaceAllString(s, "_")
	s = strings.Trim(s, "_")
	if s == "" {
		return "draft"
	}
	if s[0] >= '0' && s[0] <= '9' {
		s = "n" + s
	}
	return s
}

func deriveDbName(project, env, service string) string {
	return truncate(deriveIdentifier(project, env, service), 63)
}

// deriveDbUser is capped at 32 chars to satisfy MySQL's username length limit
// (Postgres allows 63). Same derivation as db_name so user/db pair naturally.
func deriveDbUser(project, env, service string) string {
	return truncate(deriveIdentifier(project, env, service), 32)
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}

func generateUUID() string {
	b := make([]byte, 8)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// ResolveNodeTemplateExprs is the public entrypoint the create-service-from-
// template flow calls to stamp a concrete value out of a template expression,
// given an existing node (the node must already have its UID and service_port
// setting so the address is computable). It is also safe to call on any node's
// already-stored values to re-resolve expressions.
func (e *Engine) ResolveNodeTemplateExprs(nodeID, raw string) (string, error) {
	in, err := e.nodeExprInput(nodeID)
	if err != nil {
		return "", err
	}
	return resolveTemplateExprs(in, raw)
}

// nodeExprInput builds a templateExprInput for nodeID by reusing
// computeNodeAddress (single source for hostnames/ports) plus the node's UID.
func (e *Engine) nodeExprInput(nodeID string) (templateExprInput, error) {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return templateExprInput{}, err
	}
	addr, err := e.computeNodeAddress(node)
	if err != nil {
		return templateExprInput{}, err
	}
	uid, err := e.store.EnsureNodeUID(nodeID)
	if err != nil {
		return templateExprInput{}, err
	}
	return exprInputFromAddress(addr, uid), nil
}
