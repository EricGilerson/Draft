package deploy

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"Draft/internal/cloudconfig"
	"Draft/internal/store"
)

// ServicePreview is a one-line summary of a service that would be created by an
// import, shown in the import dialog before the user commits.
type ServicePreview struct {
	Name  string `json:"name"`
	Mode  string `json:"mode"` // "image" | "build"
	Image string `json:"image,omitempty"`
	Port  string `json:"port,omitempty"`
}

// ImportPreview is the dry-run result of parsing a config file: what services
// would be created and the fidelity report, with nothing written.
type ImportPreview struct {
	Format   string             `json:"format"`
	Services []ServicePreview   `json:"services"`
	Report   cloudconfig.Report `json:"report"`
}

// ImportResult is returned after a config is actually imported.
type ImportResult struct {
	ProjectID uint               `json:"projectId"`
	Nodes     []store.CanvasNode `json:"nodes"`
	Report    cloudconfig.Report `json:"report"`
}

// ImportConfigPreview parses a config file and reports what would be imported
// without writing anything. It merges adapter notes (control-plane fields
// ignored, cross-service rewrites) with bridge notes (secrets to fill in).
func (e *Engine) ImportConfigPreview(path string) (*ImportPreview, error) {
	adapter, specs, report, err := parseConfigFile(path)
	if err != nil {
		return nil, err
	}
	for _, spec := range specs {
		_, r := cloudconfig.BundleFromSpec(spec)
		report.Merge(r)
	}

	previews := make([]ServicePreview, 0, len(specs))
	for _, s := range specs {
		previews = append(previews, servicePreview(s))
	}
	return &ImportPreview{Format: adapter.Format(), Services: previews, Report: report}, nil
}

// ImportConfigAsProject creates a new project from a config file (its directory
// becomes the project root) and stamps one node per service.
func (e *Engine) ImportConfigAsProject(path, projectName string) (*ImportResult, error) {
	adapter, specs, report, err := parseConfigFile(path)
	if err != nil {
		return nil, err
	}

	name := strings.TrimSpace(projectName)
	if name == "" {
		name = strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	}
	projectPath := filepath.Dir(path)
	project, err := e.store.CreateProject(name, projectPath, "Imported from "+adapter.Format())
	if err != nil {
		return nil, fmt.Errorf("create project: %w", err)
	}
	env, err := e.store.GetDefaultEnvironment(project.ID)
	if err != nil {
		return nil, fmt.Errorf("get default environment: %w", err)
	}

	report.Merge(rebaseImportedPaths(specs, path, projectPath))

	raw, _ := os.ReadFile(path)
	nodes, r := e.stampSpecsAsNodes(project.ID, env.ID, specs, adapter.Format(), string(raw), 0, 0)
	report.Merge(r)
	return &ImportResult{ProjectID: project.ID, Nodes: nodes, Report: report}, nil
}

// ImportConfigIntoProject stamps the config's services as nodes into an
// existing project's environment, laid out around the given canvas anchor.
func (e *Engine) ImportConfigIntoProject(projectID, environmentID uint, path string, x, y float64) (*ImportResult, error) {
	adapter, specs, report, err := parseConfigFile(path)
	if err != nil {
		return nil, err
	}
	project, err := e.store.GetProject(projectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	report.Merge(rebaseImportedPaths(specs, path, project.Path))
	raw, _ := os.ReadFile(path)
	nodes, r := e.stampSpecsAsNodes(projectID, environmentID, specs, adapter.Format(), string(raw), x, y)
	report.Merge(r)
	return &ImportResult{ProjectID: projectID, Nodes: nodes, Report: report}, nil
}

// stampSpecsAsNodes creates a canvas node per spec, laying them out on a grid
// anchored at (ox, oy), and stamps each. Duplicate labels are de-duplicated
// with a numeric suffix (and a note). Best-effort: a per-service failure is
// reported and skipped rather than aborting the whole import.
func (e *Engine) stampSpecsAsNodes(projectID, environmentID uint, specs []cloudconfig.ServiceSpec, format, rawDoc string, ox, oy float64) ([]store.CanvasNode, cloudconfig.Report) {
	var report cloudconfig.Report
	var nodes []store.CanvasNode
	const cols = 3
	for i, spec := range specs {
		col := i % cols
		row := i / cols
		x := ox + 120 + float64(col)*260
		y := oy + 140 + float64(row)*180

		label := e.uniqueLabel(environmentID, spec.Name)
		if label != spec.Name {
			report.Add(cloudconfig.KindInfo, "renamed", spec.Name,
				fmt.Sprintf("Service %q already existed; imported as %q.", spec.Name, label))
		}
		node, err := e.store.CreateNode(&store.CanvasNode{
			ID:            genNodeID(),
			Label:         label,
			ProjectID:     projectID,
			EnvironmentID: environmentID,
			X:             x,
			Y:             y,
		})
		if err != nil {
			report.Add(cloudconfig.KindManual, "create_failed", spec.Name,
				fmt.Sprintf("Could not create service %q: %v", spec.Name, err))
			continue
		}
		if err := e.stampSpecOntoNode(node.ID, projectID, spec, format, rawDoc); err != nil {
			report.Add(cloudconfig.KindManual, "stamp_failed", spec.Name,
				fmt.Sprintf("Service %q created but partially configured: %v", spec.Name, err))
		}
		nodes = append(nodes, *node)
	}
	return nodes, report
}

// stampSpecOntoNode writes a ServiceSpec onto an existing node: the mapped
// node_settings, imported env vars, and the original document for round-trip
// export. It parallels stampTemplateOntoNode but is template-less. Image-mode
// services with enough config are deployed immediately.
func (e *Engine) stampSpecOntoNode(nodeID string, projectID uint, spec cloudconfig.ServiceSpec, format, rawDoc string) error {
	if _, err := e.store.EnsureNodeUID(nodeID); err != nil {
		return fmt.Errorf("assign node uid: %w", err)
	}

	bundle, _ := cloudconfig.BundleFromSpec(spec)

	for key, value := range bundle.Settings {
		if key == "service_root" {
			value = normalizeImportedPath(value)
			if value == "" {
				continue
			}
		}
		if err := e.store.SetNodeSetting(nodeID, key, value); err != nil {
			return fmt.Errorf("set %s: %w", key, err)
		}
	}

	for _, v := range bundle.Env {
		scope := v.Scope
		if scope == "" {
			scope = store.EnvScopeRuntime
		}
		if err := e.store.UpsertEnvVar(store.EnvVar{
			NodeID: nodeID,
			Key:    v.Key,
			Value:  v.Value,
			Scope:  scope,
			Secret: v.Secret,
			Source: store.EnvSourceImported,
		}); err != nil {
			return fmt.Errorf("import env %q: %w", v.Key, err)
		}
	}

	// Store the original document + format so a later export can overlay Draft's
	// current contract onto it and preserve control-plane blocks (phase: target
	// profile / round-trip).
	if rawDoc != "" {
		_ = e.store.SetNodeSetting(nodeID, "source_config", rawDoc)
		_ = e.store.SetNodeSetting(nodeID, "source_config_format", format)
	}

	settings, err := e.store.GetNodeSettings(nodeID)
	if err == nil && imageModeDeployable(settings) {
		_ = e.Deploy(context.Background(), nodeID)
	}
	return nil
}

// uniqueLabel returns spec name, or a numeric-suffixed variant if a node with
// that normalized label already exists in the environment.
func (e *Engine) uniqueLabel(environmentID uint, name string) string {
	if _, err := e.store.GetNodeByLabel(environmentID, name); err != nil {
		return name
	}
	for i := 2; i < 100; i++ {
		candidate := fmt.Sprintf("%s-%d", name, i)
		if _, err := e.store.GetNodeByLabel(environmentID, candidate); err != nil {
			return candidate
		}
	}
	return name
}

// parseConfigFile reads a file, detects its format, and imports it to specs.
func parseConfigFile(path string) (cloudconfig.Adapter, []cloudconfig.ServiceSpec, cloudconfig.Report, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, cloudconfig.Report{}, fmt.Errorf("read config: %w", err)
	}
	adapter, err := cloudconfig.Detect(filepath.Base(path), data)
	if err != nil {
		return nil, nil, cloudconfig.Report{}, err
	}
	specs, report, err := adapter.Import(data)
	if err != nil {
		return nil, nil, cloudconfig.Report{}, err
	}
	return adapter, specs, report, nil
}

func servicePreview(s cloudconfig.ServiceSpec) ServicePreview {
	p := ServicePreview{Name: s.Name, Mode: "build"}
	if s.IsImageMode() {
		p.Mode = "image"
		p.Image = s.Image
	}
	if len(s.Ports) > 0 {
		p.Port = fmt.Sprintf("%d", s.Ports[0].Container)
	}
	return p
}

// rebaseImportedPaths rewrites Compose-style relative paths so Draft resolves
// them the same way Compose does: against the config file's directory, then
// stores build contexts as project-relative (or absolute when outside the
// project) and bind-mount sources as absolute host paths.
//
// Without this, importing compose from a subdirectory (or into an existing
// project whose root is not the compose dir) treats `context: ./api` as
// `<project>/api` instead of `<composeDir>/api`.
func rebaseImportedPaths(specs []cloudconfig.ServiceSpec, configPath, projectPath string) cloudconfig.Report {
	var rep cloudconfig.Report
	if len(specs) == 0 {
		return rep
	}
	absProject, err := filepath.Abs(projectPath)
	if err != nil {
		rep.Add(cloudconfig.KindManual, "path_rebase", "",
			fmt.Sprintf("Could not resolve project path %q; left imported paths unchanged: %v", projectPath, err))
		return rep
	}
	absProject = filepath.Clean(absProject)
	absConfigDir, err := filepath.Abs(filepath.Dir(configPath))
	if err != nil {
		rep.Add(cloudconfig.KindManual, "path_rebase", "",
			fmt.Sprintf("Could not resolve config directory for %q; left imported paths unchanged: %v", configPath, err))
		return rep
	}
	absConfigDir = filepath.Clean(absConfigDir)

	for i := range specs {
		spec := &specs[i]
		if spec.Build != nil {
			ctx := strings.TrimSpace(spec.Build.Context)
			if ctx == "" {
				ctx = "."
			}
			absCtx := resolveAgainstBase(absConfigDir, ctx)
			rel, ok := relInsideBase(absProject, absCtx)
			if ok {
				spec.Build.Context = rel
			} else {
				spec.Build.Context = absCtx
				rep.Add(cloudconfig.KindInfo, "path_outside_project", spec.Name,
					fmt.Sprintf("Build context %q resolves outside the project; stored as an absolute path.", ctx))
			}
			if before, after := ctx, spec.Build.Context; before != after && before != "./"+after && normalizeImportedPath(before) != after {
				rep.Add(cloudconfig.KindTransformed, "build_context_path", spec.Name,
					fmt.Sprintf("Build context %q → %q (relative to compose file, then project).", before, after))
			}
		}
		for j := range spec.Volumes {
			v := &spec.Volumes[j]
			t := v.Type
			if t == "" {
				t = cloudconfig.VolumeBind
			}
			if t != cloudconfig.VolumeBind {
				continue
			}
			src := strings.TrimSpace(v.Source)
			if src == "" || looksLikeNamedVolume(src) {
				continue
			}
			absSrc := resolveAgainstBase(absConfigDir, src)
			if absSrc != src {
				rep.Add(cloudconfig.KindTransformed, "bind_mount_path", spec.Name,
					fmt.Sprintf("Bind mount %q → %q (relative to compose file).", src, absSrc))
			}
			v.Source = absSrc
		}
	}
	return rep
}

// resolveAgainstBase joins a possibly-relative path to base when it is not
// absolute. Absolute inputs are cleaned and returned unchanged.
func resolveAgainstBase(base, p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return filepath.Clean(base)
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	return filepath.Clean(filepath.Join(base, p))
}

// relInsideBase returns the slash-form path of target relative to base when
// target is base or a descendant. "." means target == base.
func relInsideBase(base, target string) (string, bool) {
	rel, err := filepath.Rel(base, target)
	if err != nil {
		return "", false
	}
	rel = filepath.Clean(rel)
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", false
	}
	return filepath.ToSlash(rel), true
}

// looksLikeNamedVolume reports compose volume sources that are Docker volume
// names rather than host paths (no separators, not "."/"..", not absolute).
func looksLikeNamedVolume(src string) bool {
	if filepath.IsAbs(src) {
		return false
	}
	if src == "." || src == ".." {
		return false
	}
	if strings.ContainsAny(src, `/\`) {
		return false
	}
	return true
}

// normalizeImportedPath cleans an imported build-context path to the
// project-relative form Draft stores ("./web" → "web", "." → "").
// Absolute paths are kept absolute (cleaned) so contexts outside the project
// still resolve at deploy time.
func normalizeImportedPath(p string) string {
	p = strings.TrimSpace(p)
	if p == "" {
		return ""
	}
	if filepath.IsAbs(p) {
		return filepath.Clean(p)
	}
	p = strings.TrimPrefix(p, "./")
	if p == "." {
		return ""
	}
	return filepath.ToSlash(filepath.Clean(p))
}

func genNodeID() string {
	var b [8]byte
	_, _ = rand.Read(b[:])
	return "svc-" + hex.EncodeToString(b[:])
}
