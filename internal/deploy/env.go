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
	vars, err := e.store.ListEnvVars(in.NodeID)
	if err != nil {
		return deploymentEnv{}, err
	}

	runtimeValues := make(map[string]string, len(vars)+len(generatedEnvKeys))
	buildArgs := map[string]*string{}
	for _, v := range vars {
		if _, reserved := generatedEnvKeys[v.Key]; reserved {
			return deploymentEnv{}, fmt.Errorf("%s is reserved for Draft-generated deployment values", v.Key)
		}
		runtimeValues[v.Key] = v.Value
		if v.Scope == store.EnvScopeBuild || v.Scope == store.EnvScopeBoth {
			value := v.Value
			buildArgs[v.Key] = &value
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
