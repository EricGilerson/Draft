package deploy

import (
	"context"
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
// so the wizard can surface them without blocking creation. DeployStarted is
// true when an image-mode stamp kicked off a background deploy (pull + run).
type CreateNodeFromTemplateResult struct {
	Node           store.CanvasNode `json:"node"`
	Warnings       []string         `json:"warnings"`
	DeployStarted  bool             `json:"deployStarted"`
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
	node, err := e.store.CreateNode(&store.CanvasNode{
		ID:         req.ID,
		Label:      req.Label,
		ProjectID:  req.ProjectID,
		X:          req.X,
		Y:          req.Y,
		TemplateID: req.TemplateID,
	})
	if err != nil {
		return nil, err
	}

	// Rollback the node row if stamping fails so a half-stamped node never
	// lingers. Settings/env written along the way are also node-scoped, so
	// deleting the node's settings cleans them up.
	cleanup := func() {
		_ = e.store.DeleteNode(req.ID)
		_ = e.store.DeleteNodeSettings(req.ID)
		_ = e.store.DeleteStagedChanges(req.ID)
	}

	// Ensure the UID is stable before resolving {{draft.*}} (password/uid
	// depend on it) and before computing the node address (needs service_port).
	if _, err := e.store.EnsureNodeUID(req.ID); err != nil {
		cleanup()
		return nil, fmt.Errorf("failed to assign node uid: %w", err)
	}

	if err := e.stampTemplateOntoNode(req.ID, req.ProjectID, req.ServiceRoot, tpl, schema, req.Overrides, result, false); err != nil {
		cleanup()
		return nil, err
	}

	result.Node = *node

	// Image-mode services (datastores and custom image templates) are fully
	// configured by stamping — no source tree or Dockerfile is required. Start
	// them immediately so connection URLs and env refs are live without a
	// manual Deploy click. Build-mode stamps stay manual: source may be missing
	// and the build can take a long time.
	if tpl.Mode == store.ModeImage {
		settings, err := e.store.GetNodeSettings(req.ID)
		if err == nil && imageModeDeployable(settings) {
			_ = e.Deploy(context.Background(), req.ID)
			result.DeployStarted = true
		}
	}

	return result, nil
}

// stampTemplateOntoNode writes a template's derived defaults onto an existing
// node: service root, mode-specific settings (port/image/dockerfile),
// cmd/entrypoint/workingdir, custom labels, volume mounts, env vars, and the
// best-effort Dockerfile. Shared by CreateNodeFromTemplate (preserveUserEnv=
// false — fresh node, nothing to preserve) and ReapplyTemplate (preserveUserEnv=
// true — env vars the user owns (Source manual/imported) are kept; generated
// defaults are re-resolved so template updates flow through).
//
// The node must already exist and have its UID assigned before this is called.
// serviceRoot is the dedicated wizard field ("" => leave unchanged); overrides
// are wizard field overrides (nil/empty for reapply).
func (e *Engine) stampTemplateOntoNode(
	nodeID string,
	projectID uint,
	serviceRoot string,
	tpl *store.ServiceTemplate,
	schema store.TemplateSchema,
	overrides map[string]string,
	result *CreateNodeFromTemplateResult,
	preserveUserEnv bool,
) error {
	// Service root (only when the schema allows it and a path was given).
	if schema.ServiceRoot != store.SchemaHidden && strings.TrimSpace(serviceRoot) != "" {
		if err := e.store.SetServiceRoot(nodeID, projectID, serviceRoot); err != nil {
			return fmt.Errorf("invalid service root: %w", err)
		}
	}

	// Mode-specific settings. Port is resolved from the template default or an
	// override; cmd/entrypoint/workingdir/labels come from the template body.
	// {{draft.*}} in cmd/entrypoint is resolved below (needs UID + service_port).
	portStr := resolvePortOverride(tpl, overrides)
	if portStr != "" {
		if err := e.store.SetNodeSetting(nodeID, "service_port", portStr); err != nil {
			return err
		}
	}

	if tpl.Mode == store.ModeImage {
		imageRef := overrideOr(overrides, "image", tpl.Image)
		if imageRef != "" {
			if err := e.store.SetNodeSetting(nodeID, "image", imageRef); err != nil {
				return err
			}
		}
	} else {
		dockerfileSetting := overrideOr(overrides, "dockerfile", "Dockerfile")
		if dockerfileSetting != "" {
			if err := e.store.SetNodeSetting(nodeID, "dockerfile", dockerfileSetting); err != nil {
				return err
			}
		}
	}

	// cmd/entrypoint/workingdir, resolving {{draft.*}} now that UID +
	// service_port are set. Best-effort; an empty template value is a no-op.
	if err := e.stampResolvedSetting(nodeID, "cmd_override", tpl.CmdOverride, result); err != nil {
		return err
	}
	if err := e.stampResolvedSetting(nodeID, "entrypoint_override", tpl.Entrypoint, result); err != nil {
		return err
	}
	if err := e.stampResolvedSetting(nodeID, "working_dir", tpl.WorkingDir, result); err != nil {
		return err
	}

	// Custom labels (JSON object) from the template, if any.
	if strings.TrimSpace(tpl.Labels) != "" {
		if err := e.store.SetNodeSetting(nodeID, "custom_labels", tpl.Labels); err != nil {
			return err
		}
	}

	// Volume mounts. An explicit wizard override wins; otherwise the template's
	// Volumes default. This is what makes a freshly created datastore persist
	// (Postgres/MySQL/Mongo/Redis built-ins ship a Draft-managed named volume).
	volumeMounts := strings.TrimSpace(overrides["volume_mounts"])
	if volumeMounts == "" {
		volumeMounts = strings.TrimSpace(tpl.Volumes)
	}
	if volumeMounts != "" {
		if err := e.store.SetNodeSetting(nodeID, "volume_mounts", volumeMounts); err != nil {
			return fmt.Errorf("stamp volume_mounts: %w", err)
		}
	}

	// Seed env vars from the template, resolving {{draft.*}} per value. When
	// re-applying, skip keys the user owns (manual/imported) so re-applying a
	// template doesn't clobber credentials they typed or imported.
	entries, err := parseTemplateEnvVars(tpl.EnvVars)
	if err != nil {
		return fmt.Errorf("template env vars are invalid: %w", err)
	}
	var ownedKeys map[string]bool
	if preserveUserEnv {
		existing, err := e.store.ListEnvVars(nodeID)
		if err != nil {
			return err
		}
		ownedKeys = make(map[string]bool, len(existing))
		for _, ev := range existing {
			if ev.Source == store.EnvSourceManual || ev.Source == store.EnvSourceImported {
				ownedKeys[ev.Key] = true
			}
		}
	}
	for _, entry := range entries {
		key := strings.TrimSpace(entry.Key)
		if key == "" {
			continue
		}
		if ownedKeys[key] {
			continue
		}
		resolved, err := e.ResolveNodeTemplateExprs(nodeID, entry.Value)
		if err != nil {
			return fmt.Errorf("resolve env %q: %w", key, err)
		}
		scope := entry.Scope
		if scope == "" {
			scope = store.EnvScopeRuntime
		}
		if err := e.store.UpsertEnvVar(store.EnvVar{
			NodeID: nodeID,
			Key:    key,
			Value:  resolved,
			Scope:  scope,
			Source: store.EnvSourceGenerated,
		}); err != nil {
			return fmt.Errorf("seed env %q: %w", key, err)
		}
	}

	// Wizard overrides. A key declared in the schema with Type=="env" is routed
	// to env vars (Source=manual, wins over the generated default); every other
	// key is routed to node_settings. Reapply passes nil overrides, so this is
	// a no-op there.
	for key, value := range overrides {
		if key == "dockerfile" || key == "service_port" || key == "image" || key == "volume_mounts" {
			continue // already handled above
		}
		if spec, ok := schema.Settings[key]; ok && spec.Type == "env" {
			if err := e.store.UpsertEnvVar(store.EnvVar{
				NodeID: nodeID,
				Key:    key,
				Value:  value,
				Scope:  store.EnvScopeRuntime,
				Source: store.EnvSourceManual,
			}); err != nil {
				return fmt.Errorf("override env %q: %w", key, err)
			}
			continue
		}
		if err := e.store.SetNodeSetting(nodeID, key, value); err != nil {
			return fmt.Errorf("override setting %q: %w", key, err)
		}
	}

	// Best-effort Dockerfile write for build templates with an embedded body.
	// Never overwrites an existing file; a missing directory or write failure is
	// a warning, not a hard error (the user can author one manually).
	if tpl.Mode == store.ModeBuild && strings.TrimSpace(tpl.Dockerfile) != "" {
		if warning, err := e.writeTemplateDockerfile(nodeID, projectID, tpl.Dockerfile); err != nil {
			result.Warnings = append(result.Warnings, fmt.Sprintf("could not write Dockerfile: %v", err))
		} else if warning != "" {
			result.Warnings = append(result.Warnings, warning)
		}
	}

	return nil
}

// ReapplyTemplate re-stamps a node from the template it was originally created
// from (CanvasNode.TemplateID). Template-derived settings and generated env
// vars are overwritten with the template's current defaults (so updating a
// template and re-applying flows the changes through), while env vars the user
// owns (Source manual/imported) are preserved. Returns the same result shape as
// CreateNodeFromTemplate so the UI can surface warnings. Returns an error if the
// node has no template link (TemplateID == 0).
func (e *Engine) ReapplyTemplate(nodeID string) (*CreateNodeFromTemplateResult, error) {
	if e.store == nil {
		return nil, fmt.Errorf("store is not available")
	}
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, store.ErrInvalidNode
	}
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	if node.TemplateID == 0 {
		return nil, fmt.Errorf("node has no template to re-apply")
	}
	tpl, err := e.store.GetTemplate(node.TemplateID)
	if err != nil {
		return nil, fmt.Errorf("template not found: %w", err)
	}
	schema, err := store.ParseTemplateSchema(tpl.Schema)
	if err != nil {
		return nil, fmt.Errorf("template schema is invalid: %w", err)
	}
	schema = store.NormalizeSchema(schema, tpl.Mode)

	if _, err := e.store.EnsureNodeUID(nodeID); err != nil {
		return nil, fmt.Errorf("failed to assign node uid: %w", err)
	}

	result := &CreateNodeFromTemplateResult{}
	if err := e.stampTemplateOntoNode(nodeID, node.ProjectID, "", tpl, schema, nil, result, true); err != nil {
		return nil, err
	}
	return result, nil
}

// imageModeDeployable reports whether stamped settings are enough for the
// image-pull deploy path (image + port, no dockerfile).
func imageModeDeployable(settings map[string]string) bool {
	return strings.TrimSpace(settings["image"]) != "" &&
		strings.TrimSpace(settings["service_port"]) != "" &&
		strings.TrimSpace(settings["dockerfile"]) == ""
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
