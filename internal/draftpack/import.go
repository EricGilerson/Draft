package draftpack

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"Draft/internal/store"
)

// Importer applies packs to the store.
type Importer struct {
	Store *store.Store
}

// Preview builds an ImportPreview without writing anything.
func (e *Importer) Preview(pack *Pack) (*ImportPreview, error) {
	if pack == nil {
		return nil, fmt.Errorf("nil pack")
	}
	prev := &ImportPreview{
		PackScope:        pack.Scope,
		Report:           pack.Report, // include export notes
		ProjectEnvVars:   len(pack.ProjectEnvVars),
		SandboxProfiles:  len(pack.SandboxProfiles),
		AppSecrets:       len(pack.AppSecrets),
		NeedsProjectPath: true,
		Environments:     pack.Environments,
	}
	if pack.Project != nil {
		prev.ProjectName = pack.Project.Name
	}
	for _, svc := range pack.Services {
		mode := "build"
		image := ""
		if img := strings.TrimSpace(svc.Settings["image"]); img != "" && strings.TrimSpace(svc.Settings["dockerfile"]) == "" {
			mode = "image"
			image = img
		}
		sum := ServiceSummary{
			Key:              svc.Key,
			Label:            svc.Label,
			EnvironmentKey:   svc.EnvironmentKey,
			Mode:             mode,
			Image:            image,
			Port:             strings.TrimSpace(svc.Settings["service_port"]),
			NeedsServiceRoot: svc.NeedsServiceRoot || (mode == "build" && strings.TrimSpace(svc.Settings["service_root"]) == "" && strings.TrimSpace(svc.Settings["dockerfile"]) != ""),
			BindRemapCount:   len(svc.BindRemaps),
		}
		// Auto: if relative service_root present, not needed unless path missing (checked at import).
		if !svc.NeedsServiceRoot && strings.TrimSpace(svc.Settings["service_root"]) != "" {
			sum.NeedsServiceRoot = false
		}
		if svc.NeedsServiceRoot {
			prev.NeedsServiceRoots = append(prev.NeedsServiceRoots, ServiceRootNeed{
				ServiceKey: svc.Key,
				Label:      svc.Label,
				Hint:       svc.ServiceRootHint,
			})
		}
		for _, b := range svc.BindRemaps {
			prev.NeedsBinds = append(prev.NeedsBinds, BindNeed{
				ServiceKey:    svc.Key,
				Label:         svc.Label,
				ContainerPath: b.ContainerPath,
				OriginalHost:  b.OriginalHost,
			})
		}
		prev.Services = append(prev.Services, sum)
	}
	secretKeys := map[string]bool{}
	for _, svc := range pack.Services {
		for _, ev := range svc.Env {
			if ev.Secret && ev.ValueOmitted {
				secretKeys[ev.Key] = true
			}
		}
	}
	for _, pv := range pack.ProjectEnvVars {
		if pv.Secret && pv.ValueOmitted {
			secretKeys[pv.Key] = true
		}
	}
	for k := range secretKeys {
		prev.NeedsSecrets = append(prev.NeedsSecrets, k)
	}
	for _, s := range pack.AppSecrets {
		if s.ValueOmitted || s.Value == "" {
			prev.NeedsAppSecrets = append(prev.NeedsAppSecrets, s.Key)
		}
	}
	return prev, nil
}

// Import applies a pack according to options.
func (e *Importer) Import(pack *Pack, opts ImportOptions) (*ImportResult, error) {
	if pack == nil {
		return nil, fmt.Errorf("nil pack")
	}
	var rep Report
	rep.Merge(pack.Report)

	mode := opts.Mode
	if mode == "" {
		mode = ImportAsNewProject
	}

	var project *store.Project
	var err error
	envIDByKey := map[string]uint{}
	var envIDs []uint

	switch mode {
	case ImportAsNewProject:
		name := strings.TrimSpace(opts.ProjectName)
		if name == "" && pack.Project != nil {
			name = pack.Project.Name
		}
		if name == "" {
			name = "imported"
		}
		path := strings.TrimSpace(opts.ProjectPath)
		if path == "" {
			return nil, fmt.Errorf("project folder is required")
		}
		abs, err := filepath.Abs(path)
		if err != nil {
			return nil, fmt.Errorf("project path: %w", err)
		}
		if st, err := os.Stat(abs); err != nil || !st.IsDir() {
			return nil, fmt.Errorf("project folder does not exist: %s", abs)
		}
		project, err = e.Store.CreateProject(name, abs, projectDesc(pack))
		if err != nil {
			return nil, fmt.Errorf("create project: %w", err)
		}
		// Create environments from pack (or use default for service/env scope).
		envIDs, envIDByKey, err = e.ensureEnvironments(project.ID, pack, &rep)
		if err != nil {
			return nil, err
		}

	case ImportIntoProject:
		project, err = e.Store.GetProject(opts.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("project not found: %w", err)
		}
		if opts.EnvironmentID == 0 {
			return nil, fmt.Errorf("target environment is required")
		}
		env, err := e.Store.GetEnvironment(opts.EnvironmentID)
		if err != nil {
			return nil, fmt.Errorf("environment not found: %w", err)
		}
		if env.ProjectID != project.ID {
			return nil, fmt.Errorf("environment does not belong to project")
		}
		// All pack services land in the selected environment.
		for _, pe := range pack.Environments {
			envIDByKey[pe.Key] = env.ID
		}
		if len(envIDByKey) == 0 {
			envIDByKey["default"] = env.ID
		}
		envIDs = []uint{env.ID}

	default:
		return nil, fmt.Errorf("unknown import mode %q", mode)
	}

	// Project env vars
	for _, pv := range pack.ProjectEnvVars {
		val := pv.Value
		if pv.ValueOmitted || (pv.Secret && val == "") {
			if opts.SecretValues != nil {
				if v, ok := opts.SecretValues[pv.Key]; ok {
					val = v
				}
			}
			if val == "" {
				rep.Add(KindManual, "project_var_empty", pv.Key,
					"Project value "+pv.Key+" has no value; set it in Project settings.")
			}
		}
		if err := e.Store.SetProjectEnvVar(project.ID, pv.Key, val, "", pv.Secret); err != nil {
			rep.Add(KindManual, "project_var_failed", pv.Key, err.Error())
		}
	}

	// App secrets
	if opts.ImportAppSecrets {
		for _, s := range pack.AppSecrets {
			val := s.Value
			if opts.AppSecretValues != nil {
				if v, ok := opts.AppSecretValues[s.Key]; ok && v != "" {
					val = v
				}
			}
			if val == "" {
				rep.Add(KindManual, "app_secret_empty", s.Key, "App secret "+s.Key+" not imported (no value).")
				continue
			}
			if err := e.Store.SetAppSecret(s.Key, val, s.Description); err != nil {
				rep.Add(KindManual, "app_secret_failed", s.Key, err.Error())
			} else {
				rep.Add(KindInfo, "app_secret_imported", s.Key, "App secret "+s.Key+" was written.")
			}
		}
	}

	// Sandbox profiles (new project only — need project id)
	if mode == ImportAsNewProject {
		for _, sp := range pack.SandboxProfiles {
			srcEnvID := uint(0)
			if sp.SourceEnvironmentKey != "" {
				srcEnvID = envIDByKey[sp.SourceEnvironmentKey]
			}
			_, err := e.Store.SaveSandboxProfile(store.SandboxProfile{
				ProjectID:           project.ID,
				SourceEnvironmentID: srcEnvID,
				Name:                sp.Name,
				Description:         sp.Description,
				PlanJSON:            sp.PlanJSON,
				IsDefault:           sp.IsDefault,
			})
			if err != nil {
				rep.Add(KindManual, "profile_failed", sp.Name, err.Error())
			}
		}
	}

	// First pass: create nodes + settings/env (no service_link yet)
	type created struct {
		packKey string
		nodeID  string
		label   string
		envKey  string
		link    *ServiceLinkPayload
	}
	var createdNodes []created
	nodeIDs := make([]string, 0, len(pack.Services))
	// Map envKey+label → nodeID for service_link second pass
	labelIndex := map[string]string{} // envKey+"\x00"+label → nodeID

	for _, svc := range pack.Services {
		envID, ok := envIDByKey[svc.EnvironmentKey]
		if !ok {
			// Fallback: first env
			if len(envIDs) == 0 {
				rep.Add(KindManual, "no_env", svc.Label, "No target environment for service.")
				continue
			}
			envID = envIDs[0]
		}
		label := e.uniqueLabel(envID, svc.Label)
		if label != svc.Label {
			rep.Add(KindInfo, "renamed", svc.Label, fmt.Sprintf("Service %q already existed; imported as %q.", svc.Label, label))
		}
		node, err := e.Store.CreateNode(&store.CanvasNode{
			ID:            genNodeID(),
			Label:         label,
			ProjectID:     project.ID,
			EnvironmentID: envID,
			X:             svc.X,
			Y:             svc.Y,
		})
		if err != nil {
			rep.Add(KindManual, "create_failed", svc.Label, err.Error())
			continue
		}
		if _, err := e.Store.EnsureNodeUID(node.ID); err != nil {
			rep.Add(KindManual, "uid_failed", svc.Label, err.Error())
		}

		// Template
		if svc.Template != nil {
			if tid := e.resolveTemplate(svc.Template, &rep); tid > 0 {
				_ = e.Store.DB.Model(node).Update("template_id", tid)
			}
		}

		// Settings
		settings := cloneSettings(svc.Settings)
		// Service root
		if root, ok := opts.ServiceRootOverrides[svc.Key]; ok && strings.TrimSpace(root) != "" {
			if err := e.applyServiceRoot(node.ID, project, root, &rep, label); err != nil {
				rep.Add(KindManual, "service_root", label, err.Error())
			}
			delete(settings, "service_root") // applyServiceRoot wrote it
		} else if rel := strings.TrimSpace(settings["service_root"]); rel != "" {
			// Verify relative path exists under project
			abs := filepath.Join(project.Path, filepath.FromSlash(NormalizeRelative(rel)))
			if st, err := os.Stat(abs); err != nil || !st.IsDir() {
				rep.Add(KindManual, "service_root_missing", label,
					"Service root "+rel+" not found under project folder; set it in Settings.")
				// Still write the relative path so user can fix
			}
		} else if svc.NeedsServiceRoot {
			rep.Add(KindManual, "service_root_needed", label,
				"No service root set; choose a folder before building.")
		}

		// Volumes with bind overrides
		if raw, ok := settings["volume_mounts"]; ok {
			settings["volume_mounts"] = applyBindOverrides(raw, svc.Key, opts.BindPathOverrides, &rep, label)
			if settings["volume_mounts"] == "" {
				delete(settings, "volume_mounts")
			}
		}

		for k, v := range settings {
			if k == "service_link" {
				continue
			}
			if err := e.Store.SetNodeSetting(node.ID, k, v); err != nil {
				rep.Add(KindManual, "setting_failed", k, err.Error())
			}
		}

		// Env vars
		for _, ev := range svc.Env {
			val := ev.Value
			if ev.ValueOmitted || (ev.Secret && val == "") {
				if opts.SecretValues != nil {
					if v, ok := opts.SecretValues[ev.Key]; ok {
						val = v
					}
				}
			}
			scope := ev.Scope
			if scope == "" {
				scope = store.EnvScopeRuntime
			}
			src := ev.Source
			if src == "" {
				src = store.EnvSourceImported
			}
			if err := e.Store.UpsertEnvVar(store.EnvVar{
				NodeID: node.ID,
				Key:    ev.Key,
				Value:  val,
				Scope:  scope,
				Secret: ev.Secret,
				Source: src,
			}); err != nil {
				rep.Add(KindManual, "env_failed", ev.Key, err.Error())
			}
		}

		createdNodes = append(createdNodes, created{
			packKey: svc.Key,
			nodeID:  node.ID,
			label:   label,
			envKey:  svc.EnvironmentKey,
			link:    svc.ServiceLink,
		})
		nodeIDs = append(nodeIDs, node.ID)
		labelIndex[svc.EnvironmentKey+"\x00"+svc.Label] = node.ID
		// Also index under final label
		labelIndex[svc.EnvironmentKey+"\x00"+label] = node.ID
	}

	// Second pass: service links
	for _, c := range createdNodes {
		if c.link == nil {
			continue
		}
		rootID := labelIndex[c.link.RootEnvironmentKey+"\x00"+c.link.RootServiceLabel]
		if rootID == "" {
			rep.Add(KindIgnored, "service_link_unresolved", c.label,
				"Could not resolve linked root "+c.link.RootServiceLabel+"; link not applied.")
			continue
		}
		payload, _ := json.Marshal(map[string]string{"rootNodeId": rootID})
		if err := e.Store.SetNodeSetting(c.nodeID, "service_link", string(payload)); err != nil {
			rep.Add(KindManual, "service_link_failed", c.label, err.Error())
		} else {
			rep.Add(KindInfo, "service_link_restored", c.label,
				"Linked to "+c.link.RootServiceLabel+" in "+c.link.RootEnvironmentKey+".")
		}
	}

	return &ImportResult{
		ProjectID:      project.ID,
		EnvironmentIDs: envIDs,
		NodeIDs:        nodeIDs,
		Report:         rep,
	}, nil
}

func projectDesc(pack *Pack) string {
	if pack.Project != nil && pack.Project.Description != "" {
		return pack.Project.Description
	}
	return "Imported from Draft pack"
}

func (e *Importer) ensureEnvironments(projectID uint, pack *Pack, rep *Report) ([]uint, map[string]uint, error) {
	envIDByKey := map[string]uint{}
	var envIDs []uint

	// Default env always exists after CreateProject.
	def, err := e.Store.GetDefaultEnvironment(projectID)
	if err != nil {
		return nil, nil, err
	}

	if len(pack.Environments) == 0 {
		envIDByKey["default"] = def.ID
		return []uint{def.ID}, envIDByKey, nil
	}

	// Prefer mapping the pack's default environment onto the project's default
	// so slug/name stay sensible when possible.
	var defaultPack *EnvironmentPayload
	for i := range pack.Environments {
		if pack.Environments[i].IsDefault {
			defaultPack = &pack.Environments[i]
			break
		}
	}
	if defaultPack == nil {
		defaultPack = &pack.Environments[0]
	}

	// Rename default env to match pack default name (slug stays as created).
	if defaultPack.Name != "" && defaultPack.Name != def.Name {
		_ = e.Store.RenameEnvironment(def.ID, defaultPack.Name)
	}
	envIDByKey[defaultPack.Key] = def.ID
	envIDs = append(envIDs, def.ID)

	for i := range pack.Environments {
		pe := pack.Environments[i]
		if pe.Key == defaultPack.Key {
			continue
		}
		// CreateEnvironment generates its own slug from name.
		env, err := e.Store.CreateEnvironment(projectID, pe.Name)
		if err != nil {
			rep.Add(KindManual, "env_create_failed", pe.Name, err.Error())
			// Fall back to default env for services
			envIDByKey[pe.Key] = def.ID
			continue
		}
		envIDByKey[pe.Key] = env.ID
		envIDs = append(envIDs, env.ID)
	}
	return envIDs, envIDByKey, nil
}

func (e *Importer) uniqueLabel(environmentID uint, name string) string {
	if _, err := e.Store.GetNodeByLabel(environmentID, name); err != nil {
		return name
	}
	for i := 2; i < 100; i++ {
		candidate := fmt.Sprintf("%s-%d", name, i)
		if _, err := e.Store.GetNodeByLabel(environmentID, candidate); err != nil {
			return candidate
		}
	}
	return name
}

func (e *Importer) resolveTemplate(ref *TemplateRef, rep *Report) uint {
	if ref == nil {
		return 0
	}
	if ref.BuiltinName != "" {
		templates, err := e.Store.ListTemplates()
		if err != nil {
			return 0
		}
		for _, t := range templates {
			if t.Builtin && t.Name == ref.BuiltinName {
				return t.ID
			}
		}
		rep.Add(KindInfo, "template_missing", ref.BuiltinName,
			"Built-in template "+ref.BuiltinName+" not found; service settings were still imported.")
		return 0
	}
	if ref.User != nil {
		// Prefer existing user template with same name.
		templates, err := e.Store.ListTemplates()
		if err == nil {
			for _, t := range templates {
				if !t.Builtin && t.Name == ref.User.Name {
					return t.ID
				}
			}
		}
		created, err := e.Store.CreateTemplate(&store.ServiceTemplate{
			Name:            ref.User.Name,
			Description:     ref.User.Description,
			Category:        ref.User.Category,
			Icon:            ref.User.Icon,
			Color:           ref.User.Color,
			Mode:            ref.User.Mode,
			Image:           ref.User.Image,
			ImageTags:       ref.User.ImageTags,
			Port:            ref.User.Port,
			Dockerfile:      ref.User.Dockerfile,
			CmdOverride:     ref.User.CmdOverride,
			Entrypoint:      ref.User.Entrypoint,
			WorkingDir:      ref.User.WorkingDir,
			EnvVars:         ref.User.EnvVars,
			Labels:          ref.User.Labels,
			Volumes:         ref.User.Volumes,
			Schema:          ref.User.Schema,
			DefaultSettings: ref.User.DefaultSettings,
		})
		if err != nil {
			rep.Add(KindManual, "template_import_failed", ref.User.Name, err.Error())
			return 0
		}
		rep.Add(KindInfo, "template_imported", ref.User.Name, "User template was added to the library.")
		return created.ID
	}
	return 0
}

func (e *Importer) applyServiceRoot(nodeID string, project *store.Project, root string, rep *Report, label string) error {
	root = strings.TrimSpace(root)
	if root == "" {
		return nil
	}
	// Absolute path must be inside project.
	if !IsProjectRelative(root) {
		return e.Store.SetServiceRoot(nodeID, project.ID, root)
	}
	// Relative: join and set via SetServiceRoot for validation.
	abs := filepath.Join(project.Path, filepath.FromSlash(NormalizeRelative(root)))
	return e.Store.SetServiceRoot(nodeID, project.ID, abs)
}

func cloneSettings(in map[string]string) map[string]string {
	if in == nil {
		return map[string]string{}
	}
	out := make(map[string]string, len(in))
	for k, v := range in {
		out[k] = v
	}
	return out
}
