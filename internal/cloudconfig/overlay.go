package cloudconfig

import (
	"encoding/json"

	"gopkg.in/yaml.v3"
)

// Overlay support: when a service was imported from a cloud config, Draft stores
// the original document. On export to the same format, rather than regenerating
// from scratch (which would drop control-plane blocks Draft does not model —
// autoscaling, IAM, ingress rules), the adapter overlays Draft's current
// run-contract onto the original document, patching only the fields the user is
// likely to have changed locally (image and environment) and leaving everything
// else untouched.
//
// Overlay patches image + env only; if the base cannot be navigated it returns
// ok=false so the caller falls back to a full Export.

func (a cloudRunAdapter) Overlay(base []byte, spec ServiceSpec) ([]byte, Report, error) {
	var rep Report
	var doc map[string]any
	if yaml.Unmarshal(base, &doc) != nil {
		return nil, rep, errFallback
	}
	c := navMap(doc, "spec", "template", "spec")
	containers := navSlice(c, "containers")
	if len(containers) == 0 {
		return nil, rep, errFallback
	}
	cont, ok := containers[0].(map[string]any)
	if !ok {
		return nil, rep, errFallback
	}
	if spec.Image != "" {
		cont["image"] = spec.Image
	}
	env := make([]any, 0, len(spec.Env))
	for _, e := range spec.Env {
		if e.Secret {
			env = append(env, map[string]any{"name": e.Key, "valueFrom": map[string]any{"secretKeyRef": map[string]any{"name": secretName(e.Key), "key": "latest"}}})
			continue
		}
		env = append(env, map[string]any{"name": e.Key, "value": e.Value})
	}
	cont["env"] = env
	out, err := yaml.Marshal(doc)
	return out, rep, err
}

func (a containerAppsAdapter) Overlay(base []byte, spec ServiceSpec) ([]byte, Report, error) {
	var rep Report
	var doc map[string]any
	if yaml.Unmarshal(base, &doc) != nil {
		return nil, rep, errFallback
	}
	tmpl := navMap(doc, "properties", "template")
	containers := navSlice(tmpl, "containers")
	if len(containers) == 0 {
		return nil, rep, errFallback
	}
	cont, ok := containers[0].(map[string]any)
	if !ok {
		return nil, rep, errFallback
	}
	if spec.Image != "" {
		cont["image"] = spec.Image
	}
	env := make([]any, 0, len(spec.Env))
	for _, e := range spec.Env {
		if e.Secret {
			env = append(env, map[string]any{"name": e.Key, "secretRef": secretName(e.Key)})
			continue
		}
		env = append(env, map[string]any{"name": e.Key, "value": e.Value})
	}
	cont["env"] = env
	out, err := yaml.Marshal(doc)
	return out, rep, err
}

func (a ecsAdapter) Overlay(base []byte, spec ServiceSpec) ([]byte, Report, error) {
	var rep Report
	var doc map[string]any
	if json.Unmarshal(base, &doc) != nil {
		return nil, rep, errFallback
	}
	containers := navSlice(doc, "containerDefinitions")
	if len(containers) == 0 {
		return nil, rep, errFallback
	}
	cont, ok := containers[0].(map[string]any)
	if !ok {
		return nil, rep, errFallback
	}
	if spec.Image != "" {
		cont["image"] = spec.Image
	}
	var envList []any
	var secrets []any
	for _, e := range spec.Env {
		if e.Secret {
			secrets = append(secrets, map[string]any{"name": e.Key, "valueFrom": "arn:aws:ssm:REGION:ACCOUNT:parameter/" + e.Key})
			continue
		}
		envList = append(envList, map[string]any{"name": e.Key, "value": e.Value})
	}
	cont["environment"] = envList
	if len(secrets) > 0 {
		cont["secrets"] = secrets
	}
	out, err := json.MarshalIndent(doc, "", "  ")
	return out, rep, err
}

// errFallback signals the caller to regenerate from scratch instead of
// overlaying.
var errFallback = &fallbackError{}

type fallbackError struct{}

func (*fallbackError) Error() string { return "overlay not applicable; export from scratch" }

// IsFallback reports whether an Overlay error means "fall back to full export".
func IsFallback(err error) bool {
	_, ok := err.(*fallbackError)
	return ok
}

func navMap(m map[string]any, keys ...string) map[string]any {
	cur := m
	for _, k := range keys {
		if cur == nil {
			return nil
		}
		next, ok := cur[k].(map[string]any)
		if !ok {
			return nil
		}
		cur = next
	}
	return cur
}

func navSlice(m map[string]any, key string) []any {
	if m == nil {
		return nil
	}
	s, _ := m[key].([]any)
	return s
}

func secretName(key string) string {
	out := make([]rune, 0, len(key))
	for _, r := range key {
		if r == '_' {
			out = append(out, '-')
			continue
		}
		if r >= 'A' && r <= 'Z' {
			r = r - 'A' + 'a'
		}
		out = append(out, r)
	}
	return string(out)
}
