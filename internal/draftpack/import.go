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
// Pass PreviewOptions with mode/target so unique-field collisions are detected.
func (e *Importer) Preview(pack *Pack, opts PreviewOptions) (*ImportPreview, error) {
	if pack == nil {
		return nil, fmt.Errorf("nil pack")
	}
	services := selectedServices(pack, opts.ServiceKeys)
	prev := &ImportPreview{
		PackScope:        pack.Scope,
		Report:           pack.Report, // include export notes
		ProjectEnvVars:   len(pack.ProjectEnvVars),
		SandboxProfiles:  len(pack.SandboxProfiles),
		AppSecrets:       len(pack.AppSecrets),
		NeedsProjectPath: true,
		Environments:     pack.Environments,
		ContentHash:      strings.TrimSpace(pack.ContentHash),
		MultiEnv:         len(pack.Environments) > 1,
		CanRecreateEnvs:  len(pack.Environments) > 0,
	}
	if pack.Project != nil {
		prev.ProjectName = pack.Project.Name
	}
	if ok, has, err := VerifyContentHash(pack); err == nil && has {
		v := ok
		prev.ContentHashOK = &v
		if !ok {
			prev.Report.Add(KindManual, "integrity", "contentHash",
				"Pack content hash does not match; the file may be corrupted or edited.")
		}
	}
	// Suggested free project name when the pack name is taken.
	nameForSuggest := strings.TrimSpace(opts.ProjectName)
	if nameForSuggest == "" {
		nameForSuggest = prev.ProjectName
	}
	if nameForSuggest != "" {
		if taken, _ := e.projectNameTaken(nameForSuggest); taken {
			prev.SuggestedProjectName = e.uniqueProjectName(nameForSuggest)
		} else {
			prev.SuggestedProjectName = nameForSuggest
		}
	}

	for _, svc := range services {
		if svc.X != 0 || svc.Y != 0 {
			prev.HasLayout = true
		}
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
			X:                svc.X,
			Y:                svc.Y,
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
	for _, svc := range services {
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
	existingSet := map[string]bool{}
	if keys, err := e.Store.ListAppSecretKeys(); err == nil {
		for _, k := range keys {
			existingSet[k] = true
		}
	}
	for _, s := range pack.AppSecrets {
		if s.ValueOmitted || s.Value == "" {
			prev.NeedsAppSecrets = append(prev.NeedsAppSecrets, s.Key)
		}
		if existingSet[s.Key] {
			prev.ExistingAppSecrets = append(prev.ExistingAppSecrets, s.Key)
		}
	}
	// Also surface secret env keys that already exist as app secrets (vault link).
	for k := range secretKeys {
		if existingSet[k] {
			found := false
			for _, ek := range prev.ExistingAppSecrets {
				if ek == k {
					found = true
					break
				}
			}
			if !found {
				prev.ExistingAppSecrets = append(prev.ExistingAppSecrets, k)
			}
		}
	}

	// Collision detection uses selected services only via ServiceKeys on opts.
	prev.Collisions = e.detectCollisions(pack, opts)
	for _, c := range prev.Collisions {
		if c.Blocking {
			prev.HasBlockingCollision = true
		}
		kind := KindManual
		if !c.Blocking {
			kind = KindInfo
		}
		prev.Report.Add(kind, c.Kind, c.Field, c.Message)
	}

	// Canvas placement preview for the import mini-map.
	prev.Layout = e.buildLayoutPreview(pack, services, opts)
	if prev.Layout != nil && prev.Layout.OverlapCount > 0 {
		prev.Report.Add(KindInfo, "layout_overlap", "canvas",
			fmt.Sprintf("%d imported service(s) would sit on top of existing canvas nodes with the current placement mode.", prev.Layout.OverlapCount))
	} else if prev.Layout != nil && prev.Layout.WouldOverlapWithoutShift && prev.Layout.Shifted {
		prev.Report.Add(KindInfo, "layout_shifted", "canvas",
			"Pack coordinates overlapped existing services; auto placement moved the group clear.")
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

	if ok, has, err := VerifyContentHash(pack); err != nil {
		return nil, err
	} else if has && !ok {
		if opts.RequireIntegrity {
			return nil, fmt.Errorf("pack content hash mismatch (file may be corrupted or edited)")
		}
		rep.Add(KindManual, "integrity", "contentHash",
			"Pack content hash does not match; imported anyway (integrity not required).")
	}

	mode := opts.Mode
	if mode == "" {
		mode = ImportAsNewProject
	}
	opts.Mode = mode
	if strings.TrimSpace(opts.LayoutMode) == "" {
		opts.LayoutMode = LayoutAuto
	}
	if strings.TrimSpace(opts.EnvImportMode) == "" {
		opts.EnvImportMode = EnvImportFlatten
	}

	services := selectedServices(pack, opts.ServiceKeys)
	if len(services) == 0 {
		return nil, fmt.Errorf("no services selected for import")
	}

	// Auto-fix renamable collisions (labels, project name, host ports) when
	// the user left them blank; blocking path collisions still error below.
	e.applyCollisionDefaults(pack, &opts)

	// Final blocking check (e.g. project path already registered).
	for _, c := range e.detectCollisions(pack, PreviewOptions{
		Mode:                     opts.Mode,
		ProjectID:                opts.ProjectID,
		EnvironmentID:            opts.EnvironmentID,
		ProjectName:              opts.ProjectName,
		ProjectPath:              opts.ProjectPath,
		ServiceLabelOverrides:    opts.ServiceLabelOverrides,
		EnvironmentNameOverrides: opts.EnvironmentNameOverrides,
		HostPortOverrides:        opts.HostPortOverrides,
		ServiceKeys:              opts.ServiceKeys,
		EnvImportMode:            opts.EnvImportMode,
	}) {
		if c.Blocking {
			return nil, fmt.Errorf("%s", c.Message)
		}
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
		// If name still collides (race), bump once more.
		if taken, _ := e.projectNameTaken(name); taken {
			fixed := e.uniqueProjectName(name)
			rep.Add(KindInfo, "project_renamed", "project",
				fmt.Sprintf("Project name %q was taken; created as %q.", name, fixed))
			name = fixed
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
		if existing, err := e.Store.GetProjectByPath(abs); err == nil && existing != nil {
			return nil, fmt.Errorf("folder is already registered as project %q; choose a different project folder", existing.Name)
		}
		project, err = e.Store.CreateProject(name, abs, projectDesc(pack))
		if err != nil {
			// Unique name race: retry with suffix once.
			if taken, _ := e.projectNameTaken(name); taken {
				name = e.uniqueProjectName(name)
				project, err = e.Store.CreateProject(name, abs, projectDesc(pack))
			}
			if err != nil {
				return nil, fmt.Errorf("create project: %w", err)
			}
		}
		// Create environments from pack (or use default for service/env scope).
		envIDs, envIDByKey, err = e.ensureEnvironments(project.ID, pack, opts, &rep)
		if err != nil {
			return nil, err
		}

	case ImportIntoProject:
		project, err = e.Store.GetProject(opts.ProjectID)
		if err != nil {
			return nil, fmt.Errorf("project not found: %w", err)
		}
		if opts.EnvImportMode == EnvImportRecreate && len(pack.Environments) > 0 {
			envIDs, envIDByKey, err = e.recreateEnvironments(project.ID, pack, opts, &rep)
			if err != nil {
				return nil, err
			}
		} else {
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
			// Flatten: all pack services land in the selected environment.
			for _, pe := range pack.Environments {
				envIDByKey[pe.Key] = env.ID
			}
			if len(envIDByKey) == 0 {
				envIDByKey["default"] = env.ID
			}
			// Services without env keys still map to target.
			for _, svc := range services {
				if _, ok := envIDByKey[svc.EnvironmentKey]; !ok {
					envIDByKey[svc.EnvironmentKey] = env.ID
				}
			}
			envIDs = []uint{env.ID}
		}

	default:
		return nil, fmt.Errorf("unknown import mode %q", mode)
	}

	// Project env vars
	for _, pv := range pack.ProjectEnvVars {
		val := pv.Value
		if pv.ValueOmitted || (pv.Secret && val == "") {
			if link := secretLink(opts, pv.Key); link != "" {
				val = "{{secret." + link + "}}"
			} else if opts.SecretValues != nil {
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

	// App secrets: write new values and/or acknowledge existing vault keys.
	for _, s := range pack.AppSecrets {
		exists, _ := e.Store.AppSecretExists(s.Key)
		if exists && (opts.LinkExistingAppSecrets || !opts.ImportAppSecrets) {
			rep.Add(KindInfo, "app_secret_linked", s.Key,
				"App secret "+s.Key+" already exists on this install; left as-is.")
			continue
		}
		if !opts.ImportAppSecrets {
			continue
		}
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
	nodeIDs := make([]string, 0, len(services))
	// Map envKey+label → nodeID for service_link second pass
	labelIndex := map[string]string{} // envKey+"\x00"+label → nodeID

	coords := e.layoutPlan(pack, services, opts, envIDByKey)
	if opts.LayoutMode == LayoutAuto || opts.LayoutMode == LayoutGrid {
		rep.Add(KindInfo, "layout", "canvas",
			fmt.Sprintf("Canvas placement mode: %s.", opts.LayoutMode))
	}

	for _, svc := range services {
		envID, ok := envIDByKey[svc.EnvironmentKey]
		if !ok {
			// Fallback: first env
			if len(envIDs) == 0 {
				rep.Add(KindManual, "no_env", svc.Label, "No target environment for service.")
				continue
			}
			envID = envIDs[0]
		}
		// Prefer explicit override (user fix or applyCollisionDefaults).
		label := strings.TrimSpace(svc.Label)
		if opts.ServiceLabelOverrides != nil {
			if o := strings.TrimSpace(opts.ServiceLabelOverrides[svc.Key]); o != "" {
				label = o
			}
		}
		// Always ensure uniqueness in the target env (handles races / missed preview).
		finalLabel := e.uniqueLabel(envID, label)
		if finalLabel != svc.Label {
			rep.Add(KindInfo, "renamed", svc.Label,
				fmt.Sprintf("Service %q imported as %q to avoid a name collision.", svc.Label, finalLabel))
		} else if finalLabel != label {
			rep.Add(KindInfo, "renamed", label,
				fmt.Sprintf("Service renamed to %q to avoid a name collision.", finalLabel))
		}
		label = finalLabel
		xy := coords[svc.Key]
		node, err := e.Store.CreateNode(&store.CanvasNode{
			ID:            genNodeID(),
			Label:         label,
			ProjectID:     project.ID,
			EnvironmentID: envID,
			X:             xy[0],
			Y:             xy[1],
		})
		if err != nil {
			// Last-resort retry with a fresh unique label.
			retry := e.uniqueLabel(envID, label+"-import")
			node, err = e.Store.CreateNode(&store.CanvasNode{
				ID:            genNodeID(),
				Label:         retry,
				ProjectID:     project.ID,
				EnvironmentID: envID,
				X:             xy[0],
				Y:             xy[1],
			})
			if err != nil {
				rep.Add(KindManual, "create_failed", svc.Label, err.Error())
				continue
			}
			label = retry
			rep.Add(KindInfo, "renamed", svc.Label, fmt.Sprintf("Service imported as %q after create retry.", label))
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
		// Host port override (empty string clears a colliding fixed port)
		if opts.HostPortOverrides != nil {
			if v, ok := opts.HostPortOverrides[svc.Key]; ok {
				if strings.TrimSpace(v) == "" {
					delete(settings, "host_port")
					rep.Add(KindInfo, "host_port_cleared", label,
						"Fixed host port was cleared because it collided; Draft will assign a port on deploy.")
				} else {
					settings["host_port"] = strings.TrimSpace(v)
				}
			}
		}
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
				if link := secretLink(opts, ev.Key); link != "" {
					val = "{{secret." + link + "}}"
					rep.Add(KindInfo, "secret_linked", ev.Key,
						fmt.Sprintf("%s linked to app secret %s.", ev.Key, link))
				} else if opts.SecretValues != nil {
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

func secretLink(opts ImportOptions, key string) string {
	if opts.SecretAppLinks == nil {
		return ""
	}
	return strings.TrimSpace(opts.SecretAppLinks[key])
}

func envDisplayName(pe EnvironmentPayload, opts ImportOptions) string {
	if opts.EnvironmentNameOverrides != nil {
		if o := strings.TrimSpace(opts.EnvironmentNameOverrides[pe.Key]); o != "" {
			return o
		}
	}
	if pe.Name != "" {
		return pe.Name
	}
	return pe.Key
}

func (e *Importer) ensureEnvironments(projectID uint, pack *Pack, opts ImportOptions, rep *Report) ([]uint, map[string]uint, error) {
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
	defName := envDisplayName(*defaultPack, opts)
	if defName != "" && defName != def.Name {
		_ = e.Store.RenameEnvironment(def.ID, defName)
	}
	envIDByKey[defaultPack.Key] = def.ID
	envIDs = append(envIDs, def.ID)

	for i := range pack.Environments {
		pe := pack.Environments[i]
		if pe.Key == defaultPack.Key {
			continue
		}
		name := envDisplayName(pe, opts)
		// CreateEnvironment generates its own slug from name.
		env, err := e.Store.CreateEnvironment(projectID, name)
		if err != nil {
			rep.Add(KindManual, "env_create_failed", name, err.Error())
			// Fall back to default env for services
			envIDByKey[pe.Key] = def.ID
			continue
		}
		envIDByKey[pe.Key] = env.ID
		envIDs = append(envIDs, env.ID)
	}
	return envIDs, envIDByKey, nil
}

// recreateEnvironments maps pack environments onto an existing project:
// match by display name when possible, otherwise create.
func (e *Importer) recreateEnvironments(projectID uint, pack *Pack, opts ImportOptions, rep *Report) ([]uint, map[string]uint, error) {
	envIDByKey := map[string]uint{}
	var envIDs []uint
	existing, err := e.Store.ListEnvironments(projectID)
	if err != nil {
		return nil, nil, err
	}
	byName := map[string]store.Environment{}
	for _, env := range existing {
		byName[strings.ToLower(strings.TrimSpace(env.Name))] = env
	}
	seenIDs := map[uint]bool{}
	for _, pe := range pack.Environments {
		name := envDisplayName(pe, opts)
		if name == "" {
			name = pe.Key
		}
		nk := strings.ToLower(strings.TrimSpace(name))
		if env, ok := byName[nk]; ok {
			envIDByKey[pe.Key] = env.ID
			if !seenIDs[env.ID] {
				envIDs = append(envIDs, env.ID)
				seenIDs[env.ID] = true
			}
			rep.Add(KindInfo, "env_mapped", pe.Key,
				fmt.Sprintf("Pack environment %q mapped to existing %q.", pe.Name, env.Name))
			continue
		}
		created, err := e.Store.CreateEnvironment(projectID, name)
		if err != nil {
			// Unique-ish race: try a free name.
			free := e.uniqueEnvironmentName(projectID, name)
			created, err = e.Store.CreateEnvironment(projectID, free)
			if err != nil {
				rep.Add(KindManual, "env_create_failed", name, err.Error())
				continue
			}
			if free != name {
				rep.Add(KindInfo, "env_renamed", pe.Key,
					fmt.Sprintf("Environment %q created as %q.", name, free))
			}
		}
		envIDByKey[pe.Key] = created.ID
		envIDs = append(envIDs, created.ID)
		seenIDs[created.ID] = true
		byName[strings.ToLower(strings.TrimSpace(created.Name))] = *created
	}
	if len(envIDs) == 0 {
		def, err := e.Store.GetDefaultEnvironment(projectID)
		if err != nil {
			return nil, nil, err
		}
		return []uint{def.ID}, map[string]uint{"default": def.ID}, nil
	}
	return envIDs, envIDByKey, nil
}

func (e *Importer) uniqueEnvironmentName(projectID uint, base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "Environment"
	}
	existing, err := e.Store.ListEnvironments(projectID)
	if err != nil {
		return base
	}
	taken := map[string]bool{}
	for _, env := range existing {
		taken[strings.ToLower(strings.TrimSpace(env.Name))] = true
	}
	if !taken[strings.ToLower(base)] {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken[strings.ToLower(candidate)] {
			return candidate
		}
	}
	return base + "-import"
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
