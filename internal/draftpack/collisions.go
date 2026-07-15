package draftpack

import (
	"fmt"
	"path/filepath"
	"strings"

	"Draft/internal/store"
)

// detectCollisions finds unique-field clashes for the given import target.
// Suggestions are free of known taken values (store + other pack services).
func (e *Importer) detectCollisions(pack *Pack, opts PreviewOptions) []Collision {
	mode := opts.Mode
	if mode == "" {
		mode = ImportAsNewProject
	}

	var out []Collision

	proposedName := strings.TrimSpace(opts.ProjectName)
	if proposedName == "" && pack.Project != nil {
		proposedName = pack.Project.Name
	}

	if mode == ImportAsNewProject {
		if proposedName != "" {
			if taken, err := e.projectNameTaken(proposedName); err == nil && taken {
				out = append(out, Collision{
					Kind:      CollisionProjectName,
					Field:     "project",
					Label:     "Project",
					Current:   proposedName,
					Suggested: e.uniqueProjectName(proposedName),
					Message:   fmt.Sprintf("A project named %q already exists. Pick a different name.", proposedName),
					Blocking:  false,
				})
			}
		}
		path := strings.TrimSpace(opts.ProjectPath)
		if path != "" {
			if abs, err := filepath.Abs(path); err == nil {
				if existing, err := e.Store.GetProjectByPath(abs); err == nil && existing != nil {
					out = append(out, Collision{
						Kind:     CollisionProjectPath,
						Field:    "project_path",
						Label:    "Project folder",
						Current:  abs,
						Message:  fmt.Sprintf("This folder is already the project %q. Choose a different folder.", existing.Name),
						Blocking: true,
					})
				}
			}
		}
	}

	effectiveLabel := func(svc ServicePayload) string {
		if opts.ServiceLabelOverrides != nil {
			if o := strings.TrimSpace(opts.ServiceLabelOverrides[svc.Key]); o != "" {
				return o
			}
		}
		return strings.TrimSpace(svc.Label)
	}

	services := selectedServices(pack, opts.ServiceKeys)
	flatten := mode == ImportIntoProject && strings.TrimSpace(opts.EnvImportMode) != EnvImportRecreate

	// Build env grouping key for label uniqueness.
	envGroup := func(svc ServicePayload) string {
		if flatten {
			return "*"
		}
		if svc.EnvironmentKey == "" {
			return "default"
		}
		return svc.EnvironmentKey
	}

	// taken[envGroup][normalizedLabel] = first service key that claimed it
	type claim struct {
		serviceKey string
		label      string
	}
	claimed := map[string]map[string]claim{} // envGroup → normLabel → claim

	// Seed store labels for into-project target env (flatten mode).
	if flatten && opts.EnvironmentID != 0 {
		claimed["*"] = map[string]claim{}
		for nk := range e.labelsInEnvironment(opts.EnvironmentID) {
			claimed["*"][nk] = claim{serviceKey: "__store__", label: nk}
		}
	}
	// Seed store labels when recreating: match by environment name when possible.
	if mode == ImportIntoProject && !flatten && opts.ProjectID != 0 {
		existing, _ := e.Store.ListEnvironments(opts.ProjectID)
		byName := map[string]uint{}
		for _, env := range existing {
			byName[strings.ToLower(strings.TrimSpace(env.Name))] = env.ID
			claimed[fmt.Sprintf("id:%d", env.ID)] = map[string]claim{}
			for nk := range e.labelsInEnvironment(env.ID) {
				claimed[fmt.Sprintf("id:%d", env.ID)][nk] = claim{serviceKey: "__store__", label: nk}
			}
		}
		// Remap envGroup for pack env keys that match existing names.
		_ = byName
	}

	for _, svc := range services {
		label := effectiveLabel(svc)
		if label == "" {
			continue
		}
		eg := envGroup(svc)
		// When recreating, also check against an existing env of the same name.
		if mode == ImportIntoProject && !flatten && opts.ProjectID != 0 {
			name := svc.EnvironmentKey
			for _, pe := range pack.Environments {
				if pe.Key == svc.EnvironmentKey {
					if o := opts.EnvironmentNameOverrides[pe.Key]; strings.TrimSpace(o) != "" {
						name = o
					} else if pe.Name != "" {
						name = pe.Name
					}
					break
				}
			}
			if existing, err := e.Store.ListEnvironments(opts.ProjectID); err == nil {
				for _, env := range existing {
					if strings.EqualFold(env.Name, name) {
						eg = fmt.Sprintf("id:%d", env.ID)
						break
					}
				}
			}
		}
		if claimed[eg] == nil {
			claimed[eg] = map[string]claim{}
		}
		nk := normalizeLabelKey(label)
		if existing, ok := claimed[eg][nk]; ok {
			// Collision with store or earlier pack service.
			// Build taken set for suggestion.
			taken := map[string]bool{}
			for k := range claimed[eg] {
				taken[k] = true
			}
			// Also reserve other effective labels we'll assign
			for _, other := range services {
				if other.Key == svc.Key {
					continue
				}
				ol := effectiveLabel(other)
				if ol != "" && envGroup(other) == eg {
					taken[normalizeLabelKey(ol)] = true
				}
			}
			sug := nextFreeLabel(label, taken)
			msg := fmt.Sprintf("Service name %q is already used.", label)
			if existing.serviceKey == "__store__" {
				msg = fmt.Sprintf("Service name %q is already used in this environment.", label)
			} else {
				msg = fmt.Sprintf("This pack has more than one service named %q. Rename one of them.", label)
			}
			out = append(out, Collision{
				Kind:      CollisionServiceLabel,
				Field:     svc.Key,
				Label:     svc.Label,
				Current:   label,
				Suggested: sug,
				Message:   msg,
				Blocking:  false,
			})
			// Reserve suggestion so subsequent collisions get distinct names.
			claimed[eg][normalizeLabelKey(sug)] = claim{serviceKey: svc.Key, label: sug}
		} else {
			claimed[eg][nk] = claim{serviceKey: svc.Key, label: label}
		}
	}

	// Host ports: within destination project and within the pack itself.
	// Effective port honors HostPortOverrides (empty override = cleared, no collision).
	effectivePort := func(svc ServicePayload) (port string, cleared bool) {
		if opts.HostPortOverrides != nil {
			if v, ok := opts.HostPortOverrides[svc.Key]; ok {
				v = strings.TrimSpace(v)
				if v == "" {
					return "", true
				}
				return v, false
			}
		}
		return strings.TrimSpace(svc.Settings["host_port"]), false
	}

	// Seed ports already taken in the destination project (into-project only).
	takenPorts := map[string]string{} // port → owner description
	if mode == ImportIntoProject && opts.ProjectID != 0 {
		if nodes, err := e.Store.ListNodes(opts.ProjectID); err == nil {
			for _, n := range nodes {
				v, _ := e.Store.GetNodeSetting(n.ID, "host_port")
				v = strings.TrimSpace(v)
				if v != "" {
					takenPorts[v] = n.Label
				}
			}
		}
	}

	for _, svc := range services {
		port, cleared := effectivePort(svc)
		if cleared || port == "" {
			continue
		}
		if owner, ok := takenPorts[port]; ok {
			msg := fmt.Sprintf("Host port %s is already used", port)
			if owner != "" {
				msg = fmt.Sprintf("Host port %s is already used by %q", port, owner)
			}
			if strings.HasPrefix(owner, "pack:") {
				msg = fmt.Sprintf("Host port %s is used by more than one service in this pack", port)
			} else {
				msg += ". Change it or clear the field to let Draft assign a port."
			}
			out = append(out, Collision{
				Kind:     CollisionHostPort,
				Field:    svc.Key,
				Label:    effectiveLabel(svc),
				Current:  port,
				Message:  msg,
				Blocking: false,
			})
			// Keep first owner; do not re-claim under this key so later pack services also collide.
			continue
		}
		takenPorts[port] = "pack:" + svc.Key
	}

	return out
}

func (e *Importer) projectNameTaken(name string) (bool, error) {
	name = strings.TrimSpace(name)
	if name == "" {
		return false, nil
	}
	var count int64
	err := e.Store.DB.Model(&store.Project{}).Where("name = ?", name).Count(&count).Error
	return count > 0, err
}

func (e *Importer) uniqueProjectName(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "imported"
	}
	if taken, _ := e.projectNameTaken(base); !taken {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if taken, _ := e.projectNameTaken(candidate); !taken {
			return candidate
		}
	}
	return base + "-copy"
}

func (e *Importer) labelsInEnvironment(environmentID uint) map[string]bool {
	out := map[string]bool{}
	nodes, err := e.Store.ListNodesByEnvironment(environmentID)
	if err != nil {
		return out
	}
	for _, n := range nodes {
		out[normalizeLabelKey(n.Label)] = true
	}
	return out
}

func (e *Importer) hostPortTaken(projectID uint, port, excludeNodeID string) bool {
	port = strings.TrimSpace(port)
	if port == "" {
		return false
	}
	nodes, err := e.Store.ListNodes(projectID)
	if err != nil {
		return false
	}
	for _, n := range nodes {
		if excludeNodeID != "" && n.ID == excludeNodeID {
			continue
		}
		v, _ := e.Store.GetNodeSetting(n.ID, "host_port")
		if strings.TrimSpace(v) == port {
			return true
		}
	}
	return false
}

func normalizeLabelKey(label string) string {
	return strings.ToLower(strings.TrimSpace(label))
}

func nextFreeLabel(base string, taken map[string]bool) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "service"
	}
	if !taken[normalizeLabelKey(base)] {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !taken[normalizeLabelKey(candidate)] {
			return candidate
		}
	}
	return base + "-copy"
}

// applyCollisionDefaults fills renames / clears when the user left a collision
// unedited, so import never hard-fails on auto-fixable unique fields.
func (e *Importer) applyCollisionDefaults(pack *Pack, opts *ImportOptions) {
	if opts.ServiceLabelOverrides == nil {
		opts.ServiceLabelOverrides = map[string]string{}
	}
	if opts.HostPortOverrides == nil {
		opts.HostPortOverrides = map[string]string{}
	}
	prev := PreviewOptions{
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
	}
	for _, c := range e.detectCollisions(pack, prev) {
		switch c.Kind {
		case CollisionProjectName:
			if c.Suggested != "" && (strings.TrimSpace(opts.ProjectName) == "" || strings.TrimSpace(opts.ProjectName) == c.Current) {
				opts.ProjectName = c.Suggested
			}
		case CollisionServiceLabel:
			if strings.TrimSpace(opts.ServiceLabelOverrides[c.Field]) == "" && c.Suggested != "" {
				opts.ServiceLabelOverrides[c.Field] = c.Suggested
			}
		case CollisionHostPort:
			if _, set := opts.HostPortOverrides[c.Field]; !set {
				opts.HostPortOverrides[c.Field] = "" // clear fixed port
			}
		}
	}
}
