package deploy

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"Draft/internal/cloudconfig"
	"Draft/internal/store"
)

// ExportedFile is one generated config file.
type ExportedFile struct {
	Name    string `json:"name"`
	Content string `json:"content"`
}

// ExportResult is the output of an export: the generated file(s) plus the
// fidelity report of anything that could not be represented in the target
// format.
type ExportResult struct {
	Format string             `json:"format"`
	Files  []ExportedFile     `json:"files"`
	Report cloudconfig.Report `json:"report"`
}

// ExportConfig serializes a single node to the requested cloud format. Env
// values keep their @{...} references so the adapter can turn them into
// ${...} placeholders (we do not resolve them to local hostnames, which would
// be meaningless off Draft).
func (e *Engine) ExportConfig(nodeID, format string) (*ExportResult, error) {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	project, err := e.store.GetProject(node.ProjectID)
	if err != nil {
		return nil, err
	}
	return e.exportConfig(nodeID, format, project.Path)
}

// ExportConfigToPath exports a node and writes the generated files into destDir.
// For compose, build/bind/env_file paths are rebased relative to destDir so the
// written file resolves the same way Compose would when run from that folder.
func (e *Engine) ExportConfigToPath(nodeID, format, destDir string) (*ExportResult, error) {
	res, err := e.exportConfig(nodeID, format, destDir)
	if err != nil {
		return nil, err
	}
	for _, f := range res.Files {
		target := filepath.Join(destDir, f.Name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, []byte(f.Content), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", f.Name, err)
		}
	}
	return res, nil
}

func (e *Engine) exportConfig(nodeID, format, composeBaseDir string) (*ExportResult, error) {
	adapter, err := cloudconfig.ByFormat(format)
	if err != nil {
		return nil, err
	}
	spec, rep, err := e.specForNode(nodeID)
	if err != nil {
		return nil, err
	}

	// Round-trip: if this node was imported from the same format, overlay the
	// current contract onto the original document so control-plane blocks Draft
	// does not model (autoscaling, IAM, ingress) survive the export.
	settings, _ := e.store.EffectiveNodeSettings(nodeID)
	if ov, ok := adapter.(cloudconfig.Overlayer); ok &&
		settings["source_config_format"] == format && settings["source_config"] != "" {
		out, r2, oerr := ov.Overlay([]byte(settings["source_config"]), spec)
		if oerr == nil {
			rep.Merge(r2)
			rep.Add(cloudconfig.KindInfo, "round_trip", "",
				"Exported by overlaying your changes onto the original config; unmapped platform settings were preserved.")
			return &ExportResult{Format: format, Files: []ExportedFile{{Name: primaryFileName(format), Content: string(out)}}, Report: rep}, nil
		}
	}

	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return nil, err
	}
	project, err := e.store.GetProject(node.ProjectID)
	if err != nil {
		return nil, err
	}

	specs := []cloudconfig.ServiceSpec{spec}
	if format == "compose" {
		rep.Merge(rebaseSpecsForComposeExport(specs, project.Path, composeBaseDir))
	}

	files, r2, err := adapter.Export(specs)
	if err != nil {
		return nil, err
	}
	rep.Merge(r2)
	return &ExportResult{Format: format, Files: toExportedFiles(files), Report: rep}, nil
}

// primaryFileName is the conventional single-service filename per format, used
// for the overlay round-trip path.
func primaryFileName(format string) string {
	switch format {
	case "cloudrun":
		return "service.yaml"
	case "ecs":
		return "taskdef.json"
	case "containerapps":
		return "containerapp.yaml"
	default:
		return "docker-compose.yml"
	}
}

// ExportProjectConfig serializes every node in a project's default (main)
// environment — the one deployable stack a docker-compose/cloud-config export
// is meant to represent, not a merged dump across every environment. Compose
// yields a single multi-service file; the single-service cloud formats yield
// one file per service (filenames are namespaced by service).
func (e *Engine) ExportProjectConfig(projectID uint, format string) (*ExportResult, error) {
	project, err := e.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	return e.exportProjectConfig(projectID, format, project.Path)
}

// ExportProjectConfigToPath writes a project stack export into destDir, rebasing
// compose paths relative to that directory.
func (e *Engine) ExportProjectConfigToPath(projectID uint, format, destDir string) (*ExportResult, error) {
	res, err := e.exportProjectConfig(projectID, format, destDir)
	if err != nil {
		return nil, err
	}
	for _, f := range res.Files {
		target := filepath.Join(destDir, f.Name)
		if err := os.MkdirAll(filepath.Dir(target), 0o755); err != nil {
			return nil, err
		}
		if err := os.WriteFile(target, []byte(f.Content), 0o644); err != nil {
			return nil, fmt.Errorf("write %s: %w", f.Name, err)
		}
	}
	return res, nil
}

func (e *Engine) exportProjectConfig(projectID uint, format, composeBaseDir string) (*ExportResult, error) {
	adapter, err := cloudconfig.ByFormat(format)
	if err != nil {
		return nil, err
	}
	project, err := e.store.GetProject(projectID)
	if err != nil {
		return nil, err
	}
	env, err := e.store.GetDefaultEnvironment(projectID)
	if err != nil {
		return nil, err
	}
	nodes, err := e.store.ListNodesByEnvironment(env.ID)
	if err != nil {
		return nil, err
	}
	var rep cloudconfig.Report
	specs := make([]cloudconfig.ServiceSpec, 0, len(nodes))
	for _, n := range nodes {
		spec, r, err := e.specForNode(n.ID)
		if err != nil {
			rep.Add(cloudconfig.KindManual, "export_failed", n.Label,
				fmt.Sprintf("Could not export %q: %v", n.Label, err))
			continue
		}
		rep.Merge(r)
		specs = append(specs, spec)
	}
	if format == "compose" {
		rep.Merge(rebaseSpecsForComposeExport(specs, project.Path, composeBaseDir))
	}
	files, r2, err := adapter.Export(specs)
	if err != nil {
		return nil, err
	}
	rep.Merge(r2)
	return &ExportResult{Format: format, Files: toExportedFiles(files), Report: rep}, nil
}

// rebaseSpecsForComposeExport rewrites Draft project-rooted paths so they are
// relative to composeBaseDir (the directory the compose file will live in).
func rebaseSpecsForComposeExport(specs []cloudconfig.ServiceSpec, projectPath, composeBaseDir string) cloudconfig.Report {
	var rep cloudconfig.Report
	absProject, err := filepath.Abs(projectPath)
	if err != nil {
		return rep
	}
	absProject = filepath.Clean(absProject)
	absBase, err := filepath.Abs(composeBaseDir)
	if err != nil {
		return rep
	}
	absBase = filepath.Clean(absBase)

	for i := range specs {
		spec := &specs[i]
		if spec.Build != nil {
			ctx := strings.TrimSpace(spec.Build.Context)
			if ctx == "" {
				ctx = "."
			}
			absCtx := store.ResolveUnderProject(absProject, ctx)
			if rel, err := filepath.Rel(absBase, absCtx); err == nil {
				spec.Build.Context = filepath.ToSlash(filepath.Clean(rel))
			} else {
				spec.Build.Context = absCtx
				rep.Add(cloudconfig.KindInfo, "export_path_absolute", spec.Name,
					fmt.Sprintf("Build context kept absolute (%s); could not relativize to export dir.", absCtx))
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
			absSrc := store.ResolveUnderProject(absProject, src)
			if rel, err := filepath.Rel(absBase, absSrc); err == nil {
				v.Source = filepath.ToSlash(filepath.Clean(rel))
			} else {
				v.Source = absSrc
			}
		}
		for j := range spec.EnvFiles {
			src := strings.TrimSpace(spec.EnvFiles[j])
			if src == "" {
				continue
			}
			absSrc := store.ResolveUnderProject(absProject, src)
			if rel, err := filepath.Rel(absBase, absSrc); err == nil {
				spec.EnvFiles[j] = filepath.ToSlash(filepath.Clean(rel))
			} else {
				spec.EnvFiles[j] = absSrc
			}
		}
	}
	return rep
}

// specForNode builds a cloudconfig.ServiceSpec from a node's effective settings
// and (raw, unresolved) env vars.
func (e *Engine) specForNode(nodeID string) (cloudconfig.ServiceSpec, cloudconfig.Report, error) {
	node, err := e.store.GetNode(nodeID)
	if err != nil {
		return cloudconfig.ServiceSpec{}, cloudconfig.Report{}, err
	}
	settings, err := e.store.EffectiveNodeSettings(nodeID)
	if err != nil {
		return cloudconfig.ServiceSpec{}, cloudconfig.Report{}, err
	}
	envVars, err := e.store.EffectiveEnvVars(nodeID)
	if err != nil {
		return cloudconfig.ServiceSpec{}, cloudconfig.Report{}, err
	}

	bundle := cloudconfig.Bundle{Settings: settings}
	for _, v := range envVars {
		bundle.Env = append(bundle.Env, cloudconfig.EnvVar{
			Key:    v.Key,
			Value:  v.Value,
			Secret: v.Secret,
			Scope:  v.Scope,
		})
	}

	spec, rep := cloudconfig.SpecFromBundle(bundle)
	spec.Name = sanitize(node.Label)
	if spec.Name == "" {
		spec.Name = "service"
	}
	return spec, rep, nil
}

func toExportedFiles(files map[string][]byte) []ExportedFile {
	out := make([]ExportedFile, 0, len(files))
	names := make([]string, 0, len(files))
	for n := range files {
		names = append(names, n)
	}
	// Stable order for deterministic UI.
	for i := 0; i < len(names); i++ {
		for j := i + 1; j < len(names); j++ {
			if names[j] < names[i] {
				names[i], names[j] = names[j], names[i]
			}
		}
	}
	for _, n := range names {
		out = append(out, ExportedFile{Name: n, Content: string(files[n])})
	}
	return out
}
