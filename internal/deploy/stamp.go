package deploy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"Draft/internal/store"
)

// CreateNodeFromTemplateRequest is the payload the create-service wizard sends
// to stamp a new node out of a template. It works uniformly for built-in,
// cloned, and user-authored templates — the template's Schema drives behavior.
type CreateNodeFromTemplateRequest struct {
	ID          string            `json:"id"`
	Label       string            `json:"label"`
	ProjectID   uint              `json:"projectId"`
	X           float64           `json:"x"`
	Y           float64           `json:"y"`
	TemplateID  uint              `json:"templateId"`
	ServiceRoot string            `json:"serviceRoot"` // absolute or project-relative; "" => none
	Overrides   map[string]string `json:"overrides"`   // wizard field overrides (settings + env)
}

// CreateNodeFromTemplateResult is the stamp result returned to the frontend.
// Warnings carry non-fatal notes (e.g. an existing Dockerfile was left alone)
// so the wizard can surface them without blocking creation.
type CreateNodeFromTemplateResult struct {
	Node     store.CanvasNode `json:"node"`
	Warnings []string         `json:"warnings"`
}

// templateEnvEntry is the JSON shape of a ServiceTemplate.EnvVars row.
type templateEnvEntry struct {
	Key   string `json:"key"`
	Value string `json:"value"`
	Scope string `json:"scope"`
}

// CreateNodeFromTemplate stamps a new canvas node from a template: creates the
// node row, stamps node_settings (dockerfile/image, port, cmd, entrypoint,
// working dir, labels), seeds env vars with {{draft.*}} resolved against the
// new node's identity, and (for build templates with an embedded Dockerfile)
// best-effort writes the Dockerfile into the service root. Service root is
// never blocking — an empty ServiceRoot simply means "use the project root".
func (e *Engine) CreateNodeFromTemplate(req CreateNodeFromTemplateRequest) (*CreateNodeFromTemplateResult, error) {
	if e.store == nil {
		return nil, fmt.Errorf("store is not available")
	}
	if strings.TrimSpace(req.ID) == "" {
		return nil, store.ErrInvalidNode
	}

	tpl, err := e.store.GetTemplate(req.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}
	schema, err := store.ParseTemplateSchema(tpl.Schema)
	if err != nil {
		return nil, fmt.Errorf("template schema is invalid: %w", err)
	}
	schema = store.NormalizeSchema(schema, tpl.Mode)

	result := &CreateNodeFromTemplateResult{}

	// 1. Create the node row with the template link.
	node := &store.CanvasNode{
		ID:         req.ID,
		Label:      req.Label,
		ProjectID:  req.ProjectID,
		X:          req.X,
		Y:          req.Y,
		TemplateID: req.TemplateID,
	}
	node, err = e.store.CreateNode(node)
	if err != nil {
		return nil, err
	}

	// Rollback the node row if stamping fails so a half-stamped node never
	// lingers. Settings/env written along the way are also node-scoped, so
	// deleting the node's settings cleans them up.
	cleanup := func() {
		_ = e.store.DeleteNode(req.ID)
		_ = e.store.DeleteNodeSettings(req.ID)
	}

	// 2. Stamp service root (only when the schema allows it and a path was given).
	if schema.ServiceRoot != store.SchemaHidden && strings.TrimSpace(req.ServiceRoot) != "" {
		if err := e.store.SetServiceRoot(req.ID, req.ProjectID, req.ServiceRoot); err != nil {
			cleanup()
			return nil, fmt.Errorf("invalid service root: %w", err)
		}
	}

	// 3. Stamp the mode-specific settings. Port is resolved from the template
	// default or an override; cmd/entrypoint/workingdir/labels come from the
	// template body. {{draft.*}} in cmd/entrypoint is resolved at stamp time
	// (needs UID + service_port below).
	portStr := resolvePortOverride(tpl, req.Overrides)
	if portStr != "" {
		if err := e.store.SetNodeSetting(req.ID, "service_port", portStr); err != nil {
			cleanup()
			return nil, err
		}
	}

	if tpl.Mode == store.ModeImage {
		// An explicit "image" override (from the wizard's version picker) wins;
		// otherwise fall back to the template's default Image ref. Empty means
		// "no image stamped" — the user can set it later in Settings.
		imageRef := overrideOr(req.Overrides, "image", tpl.Image)
		if imageRef != "" {
			if err := e.store.SetNodeSetting(req.ID, "image", imageRef); err != nil {
				cleanup()
				return nil, err
			}
		}
	} else {
		// Build mode: default the dockerfile setting to "Dockerfile" so a
		// freshly-stamped node can deploy immediately once source is in place.
		dockerfileSetting := overrideOr(req.Overrides, "dockerfile", "Dockerfile")
		if dockerfileSetting != "" {
			if err := e.store.SetNodeSetting(req.ID, "dockerfile", dockerfileSetting); err != nil {
				cleanup()
				return nil, err
			}
		}
	}

	// 4. Ensure the UID is stable before resolving {{draft.*}} (password/uid
	// depend on it) and before computing the node address (needs service_port).
	if _, err := e.store.EnsureNodeUID(req.ID); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to assign node uid: %w", err)
	}

	// 5. Stamp cmd/entrypoint/workingdir, resolving {{draft.*}} now that UID +
	// service_port are set. These are best-effort; an expression resolution
	// failure on an empty override just skips the field.
	if err := e.stampResolvedSetting(req.ID, "cmd_override", tpl.CmdOverride, result); err != nil {
		cleanup()
		return nil, err
	}
	if err := e.stampResolvedSetting(req.ID, "entrypoint_override", tpl.Entrypoint, result); err != nil {
		cleanup()
		return nil, err
	}
	if err := e.stampResolvedSetting(req.ID, "working_dir", tpl.WorkingDir, result); err != nil {
		cleanup()
		return nil, err
	}

	// 6. Stamp custom labels (JSON object) from the template, if any.
	if strings.TrimSpace(tpl.Labels) != "" {
		if err := e.store.SetNodeSetting(req.ID, "custom_labels", tpl.Labels); err != nil {
			cleanup()
			return nil, err
		}
	}

	// 6.5. Stamp volume mounts. An explicit wizard override wins; otherwise the
	// template's Volumes default is used (this is what makes a freshly created
	// datastore persist — the Postgres/MySQL/Mongo/Redis built-ins ship a
	// Draft-managed named volume for their data directory). Empty means no
	// volumes stamped; the user can add them later in Settings.
	volumeMounts := strings.TrimSpace(req.Overrides["volume_mounts"])
	if volumeMounts == "" {
		volumeMounts = strings.TrimSpace(tpl.Volumes)
	}
	if volumeMounts != "" {
		if err := e.store.SetNodeSetting(req.ID, "volume_mounts", volumeMounts); err != nil {
			cleanup()
			return nil, fmt.Errorf("stamp volume_mounts: %w", err)
		}
	}

	// 7. Seed env vars from the template, resolving {{draft.*}} per value.
	entries, err := parseTemplateEnvVars(tpl.EnvVars)
	if err != nil {
		cleanup()
		return nil, fmt.Errorf("template env vars are invalid: %w", err)
	}
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			continue
		}
		resolved, err := e.ResolveNodeTemplateExprs(req.ID, entry.Value)
		if err != nil {
			cleanup()
			return nil, fmt.Errorf("resolve env %q: %w", key, err)
		}
		scope := entry.Scope
		if scope == "" {
			scope = store.EnvScopeRuntime
		}
		if err := e.store.UpsertEnvVar(store.EnvVar{
			NodeID: req.ID,
			Key:    key,
			Value:  resolved,
			Scope:  scope,
			Source: store.EnvSourceGenerated,
		}); err != nil {
			cleanup()
			return nil, fmt.Errorf("seed env %q: %w", key, err)
		}
	}

	// 8. Apply wizard overrides. A key declared in the schema with Type=="env"
	// is routed to env vars (Source=manual, wins over the generated default);
	// every other key is routed to node_settings.
	for key, value := range req.Overrides {
		if key == "dockerfile" || key == "service_port" || key == "image" || key == "volume_mounts" {
			continue // already handled above
		}
		if spec, ok := schema.Settings[key]; ok && spec.Type == "env" {
			if err := e.store.UpsertEnvVar(store.EnvVar{
				NodeID: req.ID,
				Key:    key,
				Value:  value,
				Scope:  store.EnvScopeRuntime,
				Source: store.EnvSourceManual,
			}); err != nil {
				cleanup()
				return nil, fmt.Errorf("override env %q: %w", key, err)
			}
			continue
		}
		if err := e.store.SetNodeSetting(req.ID, key, value); err != nil {
			cleanup()
			return nil, fmt.Errorf("override setting %q: %w", key, err)
		}
	}

	// 9. Best-effort Dockerfile write for build templates with an embedded body.
	// Never overwrites an existing file; a missing directory or write failure is
	// a warning, not a hard error (the user can author one manually).
	if tpl.Mode == store.ModeBuild && strings.TrimSpace(tpl.Dockerfile) != "" {
		if warning, err := e.writeTemplateDockerfile(req.ID, req.ProjectID, tpl.Dockerfile); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not write Dockerfile: %v", err))
		} else if warning != "" {
			result.Warnings = append(result.Warnings, warning)
		}
	}

	result.Node = *node
	return result, nil
}

// stampResolvedSetting writes a single template-derived setting, resolving
// {{draft.*}} expressions against the node. An empty template value is a no-op.
// A resolution error is returned (not swallowed) so a broken template doesn't
// stamp a literal {{draft.foo}} into the container command.
func (e *Engine) stampResolvedSetting(nodeID, key, raw string, _ *CreateNodeFromTemplateResult) error {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	resolved, err := e.ResolveNodeTemplateExprs(nodeID, raw)
	if err != nil {
		return fmt.Errorf("%s: %w", key, err)
	}
	return e.store.SetNodeSetting(nodeID, key, resolved)
}

// writeTemplateDockerfile writes the template's embedded Dockerfile body into
// the service root (or project root when no service root is set), but only when
// no Dockerfile already exists there. Returns a non-empty warning string when
// it intentionally did not write (so the caller can surface it), and an error
// only on directory resolution / write failures that should abort stamping.
func (e *Engine) writeTemplateDockerfile(nodeID string, projectID uint, body string) (string, error) {
	project, err := e.store.GetProject(projectID)
	if err != nil {
		return "", fmt.Errorf("project not found: %w", err)
	}
	dir := project.Path
	if abs, err := e.store.GetServiceRoot(nodeID, projectID); err == nil && abs != "" {
		dir = abs
	}
	if info, err := os.Stat(dir); err != nil || !info.IsDir() {
		// Service root doesn't exist on disk yet — skip writing rather than
		// failing; the user may create the directory later and re-deploy.
		return fmt.Sprintf("service root %q does not exist yet; Dockerfile not written", dir), nil
	}
	target := filepath.Join(dir, "Dockerfile")
	if _, err := os.Stat(target); err == nil {
		return fmt.Sprintf("Dockerfile already exists at %s; left unchanged", target), nil
	}
	if err := os.WriteFile(target, []byte(body), 0o644); err != nil {
		return "", err
	}
	return "", nil
}

// resolvePortOverride picks the service port string from an explicit override,
// falling back to the template's declared Port. Empty means "no port stamped"
// (the user can set it later — creation is not blocked).
func resolvePortOverride(tpl *store.ServiceTemplate, overrides map[string]string) string {
	if v, ok := overrides["service_port"]; ok {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	if tpl.Port > 0 {
		return strconv.Itoa(tpl.Port)
	}
	return ""
}

// overrideOr returns the override value for key when present and non-empty,
// otherwise the fallback.
func overrideOr(overrides map[string]string, key, fallback string) string {
	if v, ok := overrides[key]; ok {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return fallback
}

// parseTemplateEnvVars decodes a template's EnvVars JSON into entries. An empty
// string yields an empty list; malformed JSON is an error.
func parseTemplateEnvVars(raw string) ([]templateEnvEntry, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil, nil
	}
	var entries []templateEnvEntry
	if err := json.Unmarshal([]byte(raw), &entries); err != nil {
		return nil, err
	}
	return entries, nil
}
