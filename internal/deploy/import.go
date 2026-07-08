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

	raw, _ := os.ReadFile(path)
	nodes, r := e.stampSpecsAsNodes(project.ID, specs, adapter.Format(), string(raw), 0, 0)
	report.Merge(r)
	return &ImportResult{ProjectID: project.ID, Nodes: nodes, Report: report}, nil
}

// ImportConfigIntoProject stamps the config's services as nodes into an
// existing project, laid out around the given canvas anchor.
func (e *Engine) ImportConfigIntoProject(projectID uint, path string, x, y float64) (*ImportResult, error) {
	adapter, specs, report, err := parseConfigFile(path)
	if err != nil {
		return nil, err
	}
	if _, err := e.store.GetProject(projectID); err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	raw, _ := os.ReadFile(path)
	nodes, r := e.stampSpecsAsNodes(projectID, specs, adapter.Format(), string(raw), x, y)
	report.Merge(r)
	return &ImportResult{ProjectID: projectID, Nodes: nodes, Report: report}, nil
}

// stampSpecsAsNodes creates a canvas node per spec, laying them out on a grid
// anchored at (ox, oy), and stamps each. Duplicate labels are de-duplicated
// with a numeric suffix (and a note). Best-effort: a per-service failure is
// reported and skipped rather than aborting the whole import.
func (e *Engine) stampSpecsAsNodes(projectID uint, specs []cloudconfig.ServiceSpec, format, rawDoc string, ox, oy float64) ([]store.CanvasNode, cloudconfig.Report) {
	var report cloudconfig.Report
	var nodes []store.CanvasNode
	const cols = 3
	for i, spec := range specs {
		col := i % cols
		row := i / cols
		x := ox + 120 + float64(col)*260
		y := oy + 140 + float64(row)*180

		label := e.uniqueLabel(projectID, spec.Name)
		if label != spec.Name {
			report.Add(cloudconfig.KindInfo, "renamed", spec.Name,
				fmt.Sprintf("Service %q already existed; imported as %q.", spec.Name, label))
		}
		node, err := e.store.CreateNode(&store.CanvasNode{
			ID:        genNodeID(),
			Label:     label,
			ProjectID: projectID,
			X:         x,
			Y:         y,
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
// that normalized label already exists in the project.
func (e *Engine) uniqueLabel(projectID uint, name string) string {
	if _, err := e.store.GetNodeByLabel(projectID, name); err != nil {
		return name
	}
	for i := 2; i < 100; i++ {
		candidate := fmt.Sprintf("%s-%d", name, i)
		if _, err := e.store.GetNodeByLabel(projectID, candidate); err != nil {
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

// normalizeImportedPath cleans an imported build-context path to the
// project-relative form Draft stores ("./web" → "web", "." → "").
func normalizeImportedPath(p string) string {
	p = strings.TrimSpace(p)
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
