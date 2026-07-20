package deploy

import (
	"fmt"
	"sort"
	"strings"

	"Draft/internal/store"
)

var generatedEnvKeys = map[string]struct{}{
	"DRAFT_SERVICE_PORT":      {},
	"DRAFT_INTERNAL_HOSTNAME": {},
	"DRAFT_INTERNAL_URL":      {},
	"DRAFT_PUBLIC_HOSTNAME":   {},
	"DRAFT_PUBLIC_URL":        {},
	"DRAFT_SERVICE_NAME":      {},
	"DRAFT_PROJECT_NAME":      {},
	"DRAFT_ENVIRONMENT":       {},

	// Deprecated generated names remain reserved so user-managed variables
	// cannot silently reintroduce the old ambiguous contract.
	"DRAFT_PORT":     {},
	"DRAFT_HOSTNAME": {},
}

type deploymentEnv struct {
	RuntimeEnv []string
	BuildArgs  map[string]*string
}

type deploymentEnvInput struct {
	NodeID           string
	ProjectID        uint
	EnvironmentID    uint
	ServiceName      string
	ProjectName      string
	Environment      string
	ServicePort      string
	InternalHostname string
	InternalURL      string
	PublicHostname   string
	PublicURL        string
}

func (e *Engine) resolveDeploymentEnv(in deploymentEnvInput) (deploymentEnv, error) {
	nodeVars, err := e.loadEffectiveEnvVars(in.NodeID)
	if err != nil {
		return deploymentEnv{}, err
	}

	runtimeValues := make(map[string]string, len(nodeVars)+len(generatedEnvKeys))
	buildArgs := map[string]*string{}

	addVar := func(v scopeVar, source string) error {
		if _, reserved := generatedEnvKeys[v.Key]; reserved {
			return fmt.Errorf("%s is reserved for Draft-generated deployment values", v.Key)
		}
		raw := v.Value
		// Template defaults are stamped as concrete strings for portability, but
		// address-derived generated values must follow the identity and port used
		// by this deployment. Rehydrate them from the template before resolving
		// references so a port/hostname change is correct on the first deploy.
		if source == store.EnvSourceGenerated {
			if templateRaw, found := e.templateEnvRaw(in.NodeID, v.Key); found {
				uid, err := e.store.EnsureNodeUID(in.NodeID)
				if err != nil {
					return err
				}
				expanded, err := resolveTemplateExprs(templateExprInput{
					ServiceName: in.ServiceName, ProjectName: in.ProjectName, Environment: in.Environment,
					UID: uid, ServicePort: in.ServicePort, InternalHostname: in.InternalHostname,
					InternalURL: in.InternalURL, PublicHostname: in.PublicHostname, PublicURL: in.PublicURL,
				}, templateRaw)
				if err != nil {
					return err
				}
				raw = expanded
			}
		}
		value, err := e.resolveValue(in.NodeID, in.ProjectID, in.EnvironmentID, raw, map[string]bool{in.NodeID: true})
		if err != nil {
			return fmt.Errorf("%s: %w", v.Key, err)
		}
		switch v.Scope {
		case store.EnvScopeRuntime, store.EnvScopeBoth:
			runtimeValues[v.Key] = value
		case store.EnvScopeBuild:
			delete(runtimeValues, v.Key)
		default:
			runtimeValues[v.Key] = value
		}
		if v.Scope == store.EnvScopeBuild || v.Scope == store.EnvScopeBoth {
			buildArgs[v.Key] = &value
		} else if _, ok := buildArgs[v.Key]; ok {
			delete(buildArgs, v.Key)
		}
		return nil
	}

	for _, v := range nodeVars {
		if err := addVar(scopeVar{Key: v.Key, Value: v.Value, Scope: v.Scope}, v.Source); err != nil {
			return deploymentEnv{}, err
		}
	}

	environment := in.Environment
	if environment == "" {
		environment = "default"
	}
	runtimeValues["DRAFT_SERVICE_PORT"] = in.ServicePort
	runtimeValues["DRAFT_INTERNAL_HOSTNAME"] = in.InternalHostname
	runtimeValues["DRAFT_PUBLIC_HOSTNAME"] = in.PublicHostname
	runtimeValues["DRAFT_SERVICE_NAME"] = in.ServiceName
	runtimeValues["DRAFT_PROJECT_NAME"] = in.ProjectName
	runtimeValues["DRAFT_ENVIRONMENT"] = environment
	// HTTP URL inject is only meaningful for HTTP-routed services. TCP wire
	// services (Postgres, Redis, …) use protocol-specific DSNs; injecting
	// http://… would be actively misleading for consumers and @{refs}.
	if in.InternalURL != "" {
		runtimeValues["DRAFT_INTERNAL_URL"] = in.InternalURL
	}
	if in.PublicURL != "" {
		runtimeValues["DRAFT_PUBLIC_URL"] = in.PublicURL
	}

	// Target-engine profile: inject the platform-conventional runtime vars the
	// target cloud would provide (e.g. PORT/K_SERVICE for Cloud Run) so a service
	// imported from or headed to that platform behaves the same locally. An
	// explicit user value for the same key wins (we only fill gaps).
	if settings, err := e.loadEffectiveSettings(in.NodeID); err == nil {
		for k, v := range targetProfileEnv(settings["target_engine"], in) {
			if _, exists := runtimeValues[k]; !exists {
				runtimeValues[k] = v
			}
		}
	}

	keys := make([]string, 0, len(runtimeValues))
	for key := range runtimeValues {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	runtimeEnv := make([]string, 0, len(keys))
	for _, key := range keys {
		runtimeEnv = append(runtimeEnv, key+"="+runtimeValues[key])
	}

	return deploymentEnv{
		RuntimeEnv: runtimeEnv,
		BuildArgs:  buildArgs,
	}, nil
}

// withTCPPublicURL rewrites a resolved deployment env so public TCP endpoints
// use the leased host port. Env is resolved before the lease exists, so
// DRAFT_PUBLIC_URL and stamped PUBLIC_* DSNs may still carry the preferred
// port; replace that host:port pair once the real lease is known.
func withTCPPublicURL(env deploymentEnv, oldPublicURL, newPublicURL string) deploymentEnv {
	if newPublicURL == "" || oldPublicURL == newPublicURL {
		if newPublicURL != "" {
			env.RuntimeEnv = setRuntimeEnvValue(env.RuntimeEnv, "DRAFT_PUBLIC_URL", newPublicURL)
		}
		return env
	}
	out := make([]string, len(env.RuntimeEnv))
	copy(out, env.RuntimeEnv)
	if oldPublicURL != "" {
		for i, item := range out {
			key, val, ok := splitEnv(item)
			if !ok {
				continue
			}
			if key == "DRAFT_PUBLIC_URL" {
				out[i] = key + "=" + newPublicURL
				continue
			}
			if strings.Contains(val, oldPublicURL) {
				out[i] = key + "=" + strings.ReplaceAll(val, oldPublicURL, newPublicURL)
			}
		}
	}
	out = setRuntimeEnvValue(out, "DRAFT_PUBLIC_URL", newPublicURL)
	env.RuntimeEnv = out

	if env.BuildArgs != nil && oldPublicURL != "" {
		for key, ptr := range env.BuildArgs {
			if ptr == nil || !strings.Contains(*ptr, oldPublicURL) {
				continue
			}
			updated := strings.ReplaceAll(*ptr, oldPublicURL, newPublicURL)
			env.BuildArgs[key] = &updated
		}
	}
	return env
}

func setRuntimeEnvValue(runtimeEnv []string, key, value string) []string {
	prefix := key + "="
	for i, item := range runtimeEnv {
		if strings.HasPrefix(item, prefix) {
			runtimeEnv[i] = prefix + value
			return runtimeEnv
		}
	}
	return append(runtimeEnv, prefix+value)
}

func splitEnv(item string) (string, string, bool) {
	for i, r := range item {
		if r == '=' {
			return item[:i], item[i+1:], true
		}
	}
	return "", "", false
}

// scopeVar is the common shape of a node-level env var for the resolution loop above.
type scopeVar struct {
	Key   string
	Value string
	Scope string
}

// templateEnvRaw returns the authored default for a generated key. It keeps
// generated identity values refreshable without overwriting user-owned vars.
func (e *Engine) templateEnvRaw(nodeID, key string) (string, bool) {
	node, err := e.store.GetNode(nodeID)
	if err != nil || node.TemplateID == 0 {
		return "", false
	}
	tpl, err := e.store.GetTemplate(node.TemplateID)
	if err != nil {
		return "", false
	}
	entries, err := parseTemplateEnvVars(tpl.EnvVars)
	if err != nil {
		return "", false
	}
	for _, entry := range entries {
		if entry.Key == key {
			return entry.Value, true
		}
	}
	return "", false
}
