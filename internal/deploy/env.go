package deploy

import (
	"fmt"
	"sort"

	"Draft/internal/store"
)

var generatedEnvKeys = map[string]struct{}{
	"DRAFT_PORT":         {},
	"DRAFT_HOSTNAME":     {},
	"DRAFT_SERVICE_NAME": {},
	"DRAFT_PROJECT_NAME": {},
	"DRAFT_ENVIRONMENT":  {},
}

type deploymentEnv struct {
	RuntimeEnv []string
	BuildArgs  map[string]*string
}

type deploymentEnvInput struct {
	NodeID      string
	ServiceName string
	ProjectName string
	Environment string
	Port        string
	Hostname    string
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
	runtimeValues["DRAFT_PORT"] = in.Port
	runtimeValues["DRAFT_HOSTNAME"] = in.Hostname
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
