package draftpack

import (
	"encoding/json"
	"fmt"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"time"

	"Draft/internal/store"
)

// secretTokenRe finds {{secret.KEY}} references in env values.
var secretTokenRe = regexp.MustCompile(`\{\{\s*secret\.([A-Za-z_][A-Za-z0-9_]*)\s*\}\}`)

// Exporter builds packs from the store.
type Exporter struct {
	Store *store.Store
}

// ExportService builds a pack for a single service node.
func (e *Exporter) ExportService(nodeID string, opts ExportOptions) (*Pack, error) {
	opts = fillExportDefaults(opts)
	node, err := e.Store.GetNode(nodeID)
	if err != nil {
		return nil, fmt.Errorf("node not found: %w", err)
	}
	project, err := e.Store.GetProject(node.ProjectID)
	if err != nil {
		return nil, err
	}
	env, err := e.Store.GetEnvironment(node.EnvironmentID)
	if err != nil {
		return nil, err
	}

	pack := newPack(ScopeService, opts)
	pack.Project = projectPayload(project)
	pack.Environments = []EnvironmentPayload{envPayload(*env)}

	svc, rep, err := e.buildService(*node, env.Slug, project, opts, map[string]envIndex{})
	if err != nil {
		return nil, err
	}
	pack.Report.Merge(rep)
	pack.Services = []ServicePayload{svc}

	if opts.IncludeProjectEnvVars {
		pack.ProjectEnvVars, rep = e.projectVars(project.ID, opts)
		pack.Report.Merge(rep)
	}
	e.attachAppSecrets(pack, opts)
	return pack, nil
}

// ExportEnvironment builds a pack for every service in an environment.
func (e *Exporter) ExportEnvironment(environmentID uint, opts ExportOptions) (*Pack, error) {
	opts = fillExportDefaults(opts)
	env, err := e.Store.GetEnvironment(environmentID)
	if err != nil {
		return nil, fmt.Errorf("environment not found: %w", err)
	}
	if e.isSandboxEnv(env.ID) {
		return nil, fmt.Errorf("sandbox environments cannot be exported as packs; export the durable source environment instead")
	}
	project, err := e.Store.GetProject(env.ProjectID)
	if err != nil {
		return nil, err
	}
	nodes, err := e.Store.ListNodesByEnvironment(env.ID)
	if err != nil {
		return nil, err
	}

	pack := newPack(ScopeEnvironment, opts)
	pack.Project = projectPayload(project)
	pack.Environments = []EnvironmentPayload{envPayload(*env)}

	// Index for service_link resolution within this env only.
	index := map[string]envIndex{env.Slug: {env: *env, nodes: nodes}}
	for _, n := range nodes {
		svc, rep, err := e.buildService(n, env.Slug, project, opts, index)
		if err != nil {
			pack.Report.Add(KindManual, "export_failed", n.Label, err.Error())
			continue
		}
		pack.Report.Merge(rep)
		pack.Services = append(pack.Services, svc)
	}
	sort.Slice(pack.Services, func(i, j int) bool {
		return pack.Services[i].Label < pack.Services[j].Label
	})

	if opts.IncludeProjectEnvVars {
		var rep Report
		pack.ProjectEnvVars, rep = e.projectVars(project.ID, opts)
		pack.Report.Merge(rep)
	}
	e.attachAppSecrets(pack, opts)
	return pack, nil
}

// ExportProject builds a pack for a project (selected or all durable envs).
func (e *Exporter) ExportProject(projectID uint, opts ExportOptions) (*Pack, error) {
	opts = fillExportDefaults(opts)
	project, err := e.Store.GetProject(projectID)
	if err != nil {
		return nil, fmt.Errorf("project not found: %w", err)
	}
	envs, err := e.Store.ListEnvironments(projectID)
	if err != nil {
		return nil, err
	}

	// Filter sandboxes and optional EnvironmentIDs.
	want := map[uint]bool{}
	for _, id := range opts.EnvironmentIDs {
		want[id] = true
	}
	var selected []store.Environment
	for _, env := range envs {
		if e.isSandboxEnv(env.ID) {
			continue
		}
		if len(want) > 0 && !want[env.ID] {
			continue
		}
		selected = append(selected, env)
	}
	if len(selected) == 0 {
		return nil, fmt.Errorf("no durable environments to export")
	}

	pack := newPack(ScopeProject, opts)
	pack.Project = projectPayload(project)

	// Build index of all selected envs for cross-env service_link.
	index := map[string]envIndex{}
	for _, env := range selected {
		nodes, err := e.Store.ListNodesByEnvironment(env.ID)
		if err != nil {
			return nil, err
		}
		index[env.Slug] = envIndex{env: env, nodes: nodes}
		pack.Environments = append(pack.Environments, envPayload(env))
	}

	for _, env := range selected {
		for _, n := range index[env.Slug].nodes {
			svc, rep, err := e.buildService(n, env.Slug, project, opts, index)
			if err != nil {
				pack.Report.Add(KindManual, "export_failed", n.Label, err.Error())
				continue
			}
			pack.Report.Merge(rep)
			pack.Services = append(pack.Services, svc)
		}
	}
	sort.Slice(pack.Services, func(i, j int) bool {
		if pack.Services[i].EnvironmentKey != pack.Services[j].EnvironmentKey {
			return pack.Services[i].EnvironmentKey < pack.Services[j].EnvironmentKey
		}
		return pack.Services[i].Label < pack.Services[j].Label
	})

	if opts.IncludeProjectEnvVars {
		var rep Report
		pack.ProjectEnvVars, rep = e.projectVars(project.ID, opts)
		pack.Report.Merge(rep)
	}

	if opts.IncludeSandboxProfiles {
		profiles, err := e.Store.ListSandboxProfiles(projectID)
		if err == nil {
			for _, p := range profiles {
				pp := SandboxProfilePayload{
					Name:        p.Name,
					Description: p.Description,
					PlanJSON:    p.PlanJSON,
					IsDefault:   p.IsDefault,
				}
				if p.SourceEnvironmentID != 0 {
					if se, err := e.Store.GetEnvironment(p.SourceEnvironmentID); err == nil {
						// Only attach if that env is in the pack.
						if _, ok := index[se.Slug]; ok {
							pp.SourceEnvironmentKey = se.Slug
						} else {
							pack.Report.Add(KindInfo, "profile_unscoped", p.Name,
								"Sandbox profile source environment was not included; imported as project-wide.")
						}
					}
				}
				pack.SandboxProfiles = append(pack.SandboxProfiles, pp)
			}
		}
	}

	e.attachAppSecrets(pack, opts)
	return pack, nil
}

type envIndex struct {
	env   store.Environment
	nodes []store.CanvasNode
}

func newPack(scope string, opts ExportOptions) *Pack {
	return &Pack{
		Format:     FormatID,
		Version:    CurrentVersion,
		ExportedAt: time.Now().UTC(),
		Scope:      scope,
		Options:    opts,
	}
}

func fillExportDefaults(opts ExportOptions) ExportOptions {
	// Detect "unset" via a pointer-free approach: if the caller zeroed the
	// struct, apply defaults. Callers that want false for IncludeServiceRoots
	// must go through DefaultExportOptions and flip flags.
	//
	// Heuristic: if nothing looks intentional, use defaults. We treat the
	// presence of any true flag OR explicit EnvironmentIDs as intentional.
	// For API clarity, deploy layer always passes DefaultExportOptions merged
	// with user flags — so here we only fill individual false-by-default
	// fields that should be true.
	//
	// Actually the deploy layer will merge properly. Keep this as a safety net
	// for tests that pass zero struct wanting "all portable defaults".
	d := DefaultExportOptions()
	// Only replace when the zero value was used entirely (no true flags and no env filter).
	if !opts.IncludeSecretValues && !opts.IncludeAppSecrets && !opts.IncludeBindHostPaths &&
		!opts.IncludeServiceRoots && !opts.IncludeProjectEnvVars && !opts.IncludeSandboxProfiles &&
		!opts.IncludeCanvasLayout && !opts.IncludeGitSettings && !opts.IncludeSourceConfig &&
		len(opts.EnvironmentIDs) == 0 {
		return d
	}
	// Partial opts: fill "should default true" when the zero-value means "use default".
	// We cannot distinguish false intentional vs unset for bools. Deploy always
	// sends full DefaultExportOptions with overrides.
	return opts
}

func projectPayload(p *store.Project) *ProjectPayload {
	return &ProjectPayload{
		Name:        p.Name,
		Description: p.Description,
		PathHint:    PathHint(p.Path),
	}
}

func envPayload(env store.Environment) EnvironmentPayload {
	return EnvironmentPayload{
		Key:       env.Slug,
		Name:      env.Name,
		Slug:      env.Slug,
		IsDefault: env.IsDefault,
	}
}

func (e *Exporter) isSandboxEnv(environmentID uint) bool {
	// Sandboxes table has unique EnvironmentID.
	var count int64
	e.Store.DB.Model(&store.Sandbox{}).Where("environment_id = ?", environmentID).Count(&count)
	return count > 0
}

func (e *Exporter) buildService(node store.CanvasNode, envKey string, project *store.Project, opts ExportOptions, index map[string]envIndex) (ServicePayload, Report, error) {
	var rep Report
	settings, err := e.Store.EffectiveNodeSettings(node.ID)
	if err != nil {
		return ServicePayload{}, rep, err
	}
	envVars, err := e.Store.EffectiveEnvVars(node.ID)
	if err != nil {
		return ServicePayload{}, rep, err
	}

	svc := ServicePayload{
		Key:            serviceKey(envKey, node.Label, node.ID),
		EnvironmentKey: envKey,
		Label:          node.Label,
	}
	if opts.IncludeCanvasLayout {
		svc.X = node.X
		svc.Y = node.Y
	}

	// Template ref
	if node.TemplateID > 0 {
		if tpl, err := e.Store.GetTemplate(node.TemplateID); err == nil {
			svc.Template = templateRef(tpl)
		}
	}

	// Settings policy
	outSettings := map[string]string{}
	for k, v := range settings {
		if alwaysOmitSettings[k] {
			rep.Add(KindIgnored, "machine_local", k, "Cached machine path omitted; it will be re-resolved on the destination.")
			continue
		}
		if gitSettings[k] && !opts.IncludeGitSettings {
			continue
		}
		if sourceConfigSettings[k] && !opts.IncludeSourceConfig {
			continue
		}
		if k == "service_link" {
			// Handled separately as portable ServiceLink.
			continue
		}
		if k == "service_root" {
			rel, need, hint, r := exportServiceRoot(v, project.Path, opts.IncludeServiceRoots)
			rep.Merge(r)
			svc.NeedsServiceRoot = need
			svc.ServiceRootHint = hint
			if rel != "" {
				outSettings[k] = rel
			}
			continue
		}
		if k == "env_file" {
			if IsProjectRelative(v) {
				outSettings[k] = NormalizeRelative(v)
			} else if strings.TrimSpace(v) != "" {
				rep.Add(KindManual, "env_file_path", node.Label,
					"env_file path was absolute and omitted; set it after import.")
			}
			continue
		}
		if k == "volume_mounts" {
			sanitized, remaps := sanitizeVolumes(v, opts.IncludeBindHostPaths, &rep, node.Label)
			svc.BindRemaps = remaps
			if sanitized != "" {
				outSettings[k] = sanitized
			}
			continue
		}
		if k == "host_port" && strings.TrimSpace(v) != "" {
			outSettings[k] = v
			rep.Add(KindInfo, "host_port", node.Label,
				"Fixed host port "+v+" was included; it may conflict on the destination and can be changed in Settings.")
			continue
		}
		outSettings[k] = v
	}
	svc.Settings = outSettings

	// Env vars
	for _, ev := range envVars {
		p := EnvVarPayload{
			Key:    ev.Key,
			Scope:  ev.Scope,
			Secret: ev.Secret,
			Source: ev.Source,
		}
		if ev.Secret && !opts.IncludeSecretValues {
			p.ValueOmitted = true
			rep.Add(KindManual, "secret_value_omitted", ev.Key,
				"Secret value for "+ev.Key+" was omitted; fill it on import or in Variables.")
		} else {
			p.Value = ev.Value
		}
		svc.Env = append(svc.Env, p)
	}

	// Portable service_link
	if raw := settings["service_link"]; strings.TrimSpace(raw) != "" {
		link, r := portableServiceLink(raw, index, e.Store)
		rep.Merge(r)
		if link != nil {
			svc.ServiceLink = link
		}
	}

	return svc, rep, nil
}

func serviceKey(envKey, label, nodeID string) string {
	// Stable within a pack; unique even if labels collide after rename.
	safe := regexp.MustCompile(`[^a-zA-Z0-9_-]+`).ReplaceAllString(label, "-")
	if safe == "" {
		safe = "service"
	}
	// Short suffix from node id
	suffix := nodeID
	if len(suffix) > 8 {
		suffix = suffix[len(suffix)-8:]
	}
	return envKey + "/" + safe + "-" + suffix
}

func exportServiceRoot(value, projectPath string, include bool) (rel string, need bool, hint string, rep Report) {
	value = strings.TrimSpace(value)
	if value == "" {
		return "", false, "", rep
	}
	if !include {
		rep.Add(KindIgnored, "service_root_omitted", "service_root", "Service root was omitted by export options.")
		return "", true, NormalizeRelative(value), rep
	}
	if IsProjectRelative(value) {
		rel = NormalizeRelative(value)
		return rel, false, rel, rep
	}
	// Absolute: try to make project-relative.
	if projectPath != "" {
		absProj, err1 := filepath.Abs(projectPath)
		absVal, err2 := filepath.Abs(value)
		if err1 == nil && err2 == nil {
			if r, err := filepath.Rel(absProj, absVal); err == nil && IsProjectRelative(r) {
				rel = NormalizeRelative(r)
				rep.Add(KindTransformed, "service_root_relativized", "service_root",
					"Absolute service root was rewritten as project-relative "+rel+".")
				return rel, false, rel, rep
			}
		}
	}
	rep.Add(KindManual, "service_root_absolute", "service_root",
		"Service root was outside the project and omitted; choose a path on import.")
	return "", true, "", rep
}

func portableServiceLink(raw string, index map[string]envIndex, st *store.Store) (*ServiceLinkPayload, Report) {
	var rep Report
	var link struct {
		RootNodeID string `json:"rootNodeId"`
	}
	if json.Unmarshal([]byte(raw), &link) != nil || strings.TrimSpace(link.RootNodeID) == "" {
		rep.Add(KindIgnored, "service_link_invalid", "service_link", "Invalid service_link was dropped.")
		return nil, rep
	}
	root, err := st.GetNode(link.RootNodeID)
	if err != nil {
		rep.Add(KindIgnored, "service_link_missing", "service_link", "Linked root service was not found; link dropped.")
		return nil, rep
	}
	rootEnv, err := st.GetEnvironment(root.EnvironmentID)
	if err != nil {
		rep.Add(KindIgnored, "service_link_env", "service_link", "Linked root environment missing; link dropped.")
		return nil, rep
	}
	if _, ok := index[rootEnv.Slug]; !ok {
		rep.Add(KindIgnored, "service_link_out_of_pack", root.Label,
			"Linked root is in an environment not included in this pack; link dropped.")
		return nil, rep
	}
	return &ServiceLinkPayload{
		RootEnvironmentKey: rootEnv.Slug,
		RootServiceLabel:   root.Label,
	}, rep
}

func templateRef(tpl *store.ServiceTemplate) *TemplateRef {
	if tpl.Builtin {
		return &TemplateRef{BuiltinName: tpl.Name}
	}
	return &TemplateRef{User: &UserTemplatePayload{
		Name:            tpl.Name,
		Description:     tpl.Description,
		Category:        tpl.Category,
		Icon:            tpl.Icon,
		Color:           tpl.Color,
		Mode:            tpl.Mode,
		Image:           tpl.Image,
		ImageTags:       tpl.ImageTags,
		Port:            tpl.Port,
		Dockerfile:      tpl.Dockerfile,
		CmdOverride:     tpl.CmdOverride,
		Entrypoint:      tpl.Entrypoint,
		WorkingDir:      tpl.WorkingDir,
		EnvVars:         tpl.EnvVars,
		Labels:          tpl.Labels,
		Volumes:         tpl.Volumes,
		Schema:          tpl.Schema,
		DefaultSettings: tpl.DefaultSettings,
	}}
}

func (e *Exporter) projectVars(projectID uint, opts ExportOptions) ([]ProjectVarPayload, Report) {
	var rep Report
	vars, err := e.Store.ListProjectEnvVars(projectID)
	if err != nil {
		return nil, rep
	}
	out := make([]ProjectVarPayload, 0, len(vars))
	for _, v := range vars {
		p := ProjectVarPayload{Key: v.Key, Secret: v.Secret}
		if v.Secret && !opts.IncludeSecretValues {
			p.ValueOmitted = true
			rep.Add(KindManual, "project_secret_omitted", v.Key,
				"Project secret "+v.Key+" value was omitted.")
		} else {
			p.Value = v.Value
		}
		out = append(out, p)
	}
	return out, rep
}

func (e *Exporter) attachAppSecrets(pack *Pack, opts ExportOptions) {
	keys := map[string]bool{}
	collectSecretKeys := func(val string) {
		for _, m := range secretTokenRe.FindAllStringSubmatch(val, -1) {
			if len(m) > 1 {
				keys[m[1]] = true
			}
		}
	}
	for _, svc := range pack.Services {
		for _, ev := range svc.Env {
			collectSecretKeys(ev.Value)
			if ev.Secret {
				// Secret-flagged env is not necessarily {{secret.X}}, skip.
			}
		}
		for _, v := range svc.Settings {
			collectSecretKeys(v)
		}
	}
	for _, pv := range pack.ProjectEnvVars {
		collectSecretKeys(pv.Value)
	}
	if len(keys) == 0 && !opts.IncludeAppSecrets {
		return
	}
	// Always list referenced keys; embed values only when IncludeAppSecrets.
	var names []string
	for k := range keys {
		names = append(names, k)
	}
	sort.Strings(names)
	for _, k := range names {
		sec, err := e.Store.GetAppSecret(k)
		if err != nil {
			pack.AppSecrets = append(pack.AppSecrets, AppSecretPayload{Key: k, ValueOmitted: true})
			pack.Report.Add(KindManual, "app_secret_missing", k,
				"Referenced app secret "+k+" was not found on this machine.")
			continue
		}
		p := AppSecretPayload{Key: k, Description: sec.Description}
		if opts.IncludeAppSecrets {
			p.Value = sec.Value
			pack.Report.Add(KindInfo, "app_secret_included", k,
				"App secret "+k+" value was embedded in the pack. Handle the file carefully.")
		} else {
			p.ValueOmitted = true
			pack.Report.Add(KindManual, "app_secret_omitted", k,
				"App secret "+k+" is referenced; create or link it on the destination.")
		}
		pack.AppSecrets = append(pack.AppSecrets, p)
	}
}

// MarshalPack encodes a pack as pretty JSON.
func MarshalPack(pack *Pack) ([]byte, error) {
	if pack == nil {
		return nil, fmt.Errorf("nil pack")
	}
	return json.MarshalIndent(pack, "", "  ")
}

// UnmarshalPack decodes and validates a pack document.
func UnmarshalPack(data []byte) (*Pack, error) {
	var pack Pack
	if err := json.Unmarshal(data, &pack); err != nil {
		return nil, fmt.Errorf("parse draft pack: %w", err)
	}
	if pack.Format != FormatID {
		return nil, fmt.Errorf("not a Draft pack (format %q)", pack.Format)
	}
	if pack.Version < 1 || pack.Version > CurrentVersion {
		return nil, fmt.Errorf("unsupported draft pack version %d", pack.Version)
	}
	if pack.Scope != ScopeService && pack.Scope != ScopeEnvironment && pack.Scope != ScopeProject {
		return nil, fmt.Errorf("unknown pack scope %q", pack.Scope)
	}
	return &pack, nil
}

// SuggestedFileName returns a friendly .draftpack filename.
func SuggestedFileName(pack *Pack) string {
	name := "draft"
	if pack.Project != nil && pack.Project.Name != "" {
		name = sanitizeFilePart(pack.Project.Name)
	}
	switch pack.Scope {
	case ScopeService:
		if len(pack.Services) > 0 {
			return name + "-" + sanitizeFilePart(pack.Services[0].Label) + ".draftpack"
		}
	case ScopeEnvironment:
		if len(pack.Environments) > 0 {
			return name + "-" + sanitizeFilePart(pack.Environments[0].Slug) + ".draftpack"
		}
	case ScopeProject:
		return name + ".draftpack"
	}
	return name + ".draftpack"
}

func sanitizeFilePart(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	s = regexp.MustCompile(`[^a-z0-9._-]+`).ReplaceAllString(s, "-")
	s = strings.Trim(s, "-")
	if s == "" {
		return "pack"
	}
	return s
}
