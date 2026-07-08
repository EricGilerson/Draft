package deploy

import (
	"fmt"
	"sort"

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
	projectVars, err := e.store.ListProjectEnvVars(in.ProjectID)
	if err != nil {
		return deploymentEnv{}, err
	}

	runtimeValues := make(map[string]string, len(nodeVars)+len(projectVars)+len(generatedEnvKeys))
	buildArgs := map[string]*string{}

	// Project-level vars are injected first, as defaults. A node-level var with
	// the same key (added below) overrides them. This lets a project share
	// DATABASE_URL / LOG_LEVEL / API keys across services while still allowing
	// one service to override.
	addVar := func(v scopeVar) error {
		if _, reserved := generatedEnvKeys[v.Key]; reserved {
			return fmt.Errorf("%s is reserved for Draft-generated deployment values", v.Key)
		}
		value, err := e.resolveValue(in.NodeID, in.ProjectID, v.Value, map[string]bool{in.NodeID: true})
		if err != nil {
			return fmt.Errorf("%s: %w", v.Key, err)
		}
		switch v.Scope {
		case store.EnvScopeRuntime, store.EnvScopeBoth:
			runtimeValues[v.Key] = value
		case store.EnvScopeBuild:
			// Build-only vars must not leak into the running container. A later
			// node-level override can also demote a project "both" var to
			// build-only, so clear any runtime value seeded earlier.
			delete(runtimeValues, v.Key)
		default:
			runtimeValues[v.Key] = value
		}
		if v.Scope == store.EnvScopeBuild || v.Scope == store.EnvScopeBoth {
			buildArgs[v.Key] = &value
		} else if _, ok := buildArgs[v.Key]; ok {
			// A node var that overrides a project build-arg must also clear the
			// build arg unless the node var itself is build-scoped.
			delete(buildArgs, v.Key)
		}
		return nil
	}

	for _, pv := range projectVars {
		if err := addVar(scopeVar{Key: pv.Key, Value: pv.Value, Scope: pv.Scope}); err != nil {
			return deploymentEnv{}, err
		}
	}
	for _, v := range nodeVars {
		if err := addVar(scopeVar{Key: v.Key, Value: v.Value, Scope: v.Scope}); err != nil {
			return deploymentEnv{}, err
		}
	}

	environment := in.Environment
	if environment == "" {
		environment = "default"
	}
	runtimeValues["DRAFT_SERVICE_PORT"] = in.ServicePort
	runtimeValues["DRAFT_INTERNAL_HOSTNAME"] = in.InternalHostname
	runtimeValues["DRAFT_INTERNAL_URL"] = in.InternalURL
	runtimeValues["DRAFT_PUBLIC_HOSTNAME"] = in.PublicHostname
	runtimeValues["DRAFT_PUBLIC_URL"] = in.PublicURL
	runtimeValues["DRAFT_SERVICE_NAME"] = in.ServiceName
	runtimeValues["DRAFT_PROJECT_NAME"] = in.ProjectName
	runtimeValues["DRAFT_ENVIRONMENT"] = environment

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

// scopeVar is the common shape of a node- or project-level env var for the
// resolution loop above.
type scopeVar struct {
	Key   string
	Value string
	Scope string
}
