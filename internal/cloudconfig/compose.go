package cloudconfig

import (
	"fmt"
	"sort"
	"strconv"
	"strings"

	"gopkg.in/yaml.v3"
)

func init() { Register(&composeAdapter{}) }

// composeAdapter maps docker-compose files. Compose is the closest format to
// Draft's model (container + port + env + volumes + healthcheck + restart), so
// the round-trip is nearly lossless. It is also multi-service: one file yields
// one ServiceSpec per compose service, which import turns into a whole project.
type composeAdapter struct{}

func (composeAdapter) Format() string { return "compose" }

func (composeAdapter) Detect(name string, data []byte) bool {
	base := strings.ToLower(name)
	if strings.Contains(base, "compose") && (strings.HasSuffix(base, ".yml") || strings.HasSuffix(base, ".yaml")) {
		return true
	}
	// Content sniff: a top-level "services:" mapping.
	var probe struct {
		Services map[string]yaml.Node `yaml:"services"`
	}
	if err := yaml.Unmarshal(data, &probe); err == nil && len(probe.Services) > 0 {
		return true
	}
	return false
}

func (composeAdapter) Import(data []byte) ([]ServiceSpec, Report, error) {
	var rep Report
	var file composeFile
	if err := yaml.Unmarshal(data, &file); err != nil {
		return nil, rep, fmt.Errorf("parse compose file: %w", err)
	}
	if len(file.Services) == 0 {
		return nil, rep, fmt.Errorf("no services found in compose file")
	}

	names := make([]string, 0, len(file.Services))
	for n := range file.Services {
		names = append(names, n)
	}
	sort.Strings(names)

	specs := make([]ServiceSpec, 0, len(names))
	for _, name := range names {
		svc := file.Services[name]
		spec := ServiceSpec{Name: name, Target: "compose", Extra: map[string]any{}}

		if svc.Build != nil {
			spec.Build = &BuildSpec{
				Context:    svc.Build.Context,
				Dockerfile: svc.Build.Dockerfile,
				Target:     svc.Build.Target,
			}
			for _, kv := range svc.Build.Args.items {
				spec.Build.Args = append(spec.Build.Args, EnvVar{Key: kv.k, Value: kv.v, Scope: ScopeBuild})
			}
		} else if svc.Image != "" {
			spec.Image = svc.Image
		} else {
			rep.Add(KindManual, "no_source", name,
				fmt.Sprintf("Service %q has neither image nor build; set one after import.", name))
		}

		spec.Command = svc.Command.items
		spec.Entrypoint = svc.Entrypoint.items
		spec.WorkingDir = svc.WorkingDir
		spec.User = svc.User

		for _, kv := range svc.Environment.items {
			spec.Env = append(spec.Env, EnvVar{Key: kv.k, Value: kv.v, Scope: ScopeRuntime})
		}
		if len(svc.EnvFile.items) > 0 {
			spec.EnvFiles = append(spec.EnvFiles, svc.EnvFile.items...)
		}

		for _, p := range svc.Ports {
			if ps, ok := parseComposePort(p); ok {
				spec.Ports = append(spec.Ports, ps)
			}
		}

		if svc.Deploy != nil && svc.Deploy.Resources != nil {
			applyComposeResources(&spec.Resources, svc.Deploy.Resources, &rep)
		}
		if svc.MemLimit != "" { // legacy top-level mem_limit
			if n, err := parseMem(svc.MemLimit); err == nil {
				spec.Resources.MemoryBytes = n
			}
		}

		for _, v := range svc.Volumes.items {
			spec.Volumes = append(spec.Volumes, v)
		}

		spec.Health = composeHealthToSpec(svc.Healthcheck)
		spec.Restart = RestartSpec{Policy: normalizeRestart(svc.Restart)}
		spec.Security = SecuritySpec{
			Privileged:     svc.Privileged,
			ReadonlyRootfs: svc.ReadOnly,
			Init:           svc.Init,
			CapAdd:         svc.CapAdd,
			CapDrop:        svc.CapDrop,
			StopSignal:     svc.StopSignal,
		}
		if svc.StopGrace != "" {
			if secs, err := parseDurationSeconds(svc.StopGrace); err == nil {
				spec.Security.StopGracePeriod = secs
			}
		}
		if len(svc.Labels.items) > 0 {
			spec.Security.Labels = map[string]string{}
			for _, kv := range svc.Labels.items {
				spec.Security.Labels[kv.k] = kv.v
			}
		}

		specs = append(specs, spec)
	}

	// Rewire plaintext sibling-service references to Draft's @{...} form.
	rep.Merge(RewriteCrossServiceRefs(specs))
	return specs, rep, nil
}

func (composeAdapter) Export(specs []ServiceSpec) (map[string][]byte, Report, error) {
	var rep Report
	out := composeFile{Services: map[string]composeService{}}
	namedVolumes := map[string]struct{}{}

	for _, spec := range specs {
		bundle, r := SpecFromBundle(bundleForExport(spec))
		rep.Merge(r)
		_ = bundle
		svc := composeService{}

		if spec.Build != nil {
			cb := &composeBuild{Context: orDot(spec.Build.Context), Dockerfile: spec.Build.Dockerfile, Target: spec.Build.Target}
			for _, a := range spec.Build.Args {
				cb.Args.items = append(cb.Args.items, kvPair{k: a.Key, v: a.Value})
			}
			svc.Build = cb
		} else if spec.Image != "" {
			svc.Image = spec.Image
		}

		svc.Command = flexStringList{items: spec.Command}
		svc.Entrypoint = flexStringList{items: spec.Entrypoint}
		svc.WorkingDir = spec.WorkingDir
		svc.User = spec.User

		for _, e := range spec.Env {
			val := e.Value
			if e.Secret && val == "" {
				val = "${" + e.Key + "}"
			}
			svc.Environment.items = append(svc.Environment.items, kvPair{k: e.Key, v: val})
		}
		if len(spec.EnvFiles) > 0 {
			svc.EnvFile = flexStringList{items: append([]string{}, spec.EnvFiles...)}
		}

		for _, p := range spec.Ports {
			svc.Ports = append(svc.Ports, formatComposePort(p))
		}

		if r := spec.Resources; r.MilliCPU > 0 || r.MemoryBytes > 0 || r.MemoryReservBytes > 0 {
			d := &composeDeploy{Resources: &composeResources{}}
			d.Resources.Limits = &composeResLimit{}
			if r.MilliCPU > 0 {
				d.Resources.Limits.CPUs = strconv.FormatFloat(float64(r.MilliCPU)/1000, 'g', -1, 64)
			}
			if r.MemoryBytes > 0 {
				d.Resources.Limits.Memory = ramToString(r.MemoryBytes)
			}
			if r.MemoryReservBytes > 0 {
				d.Resources.Reservations = &composeResLimit{Memory: ramToString(r.MemoryReservBytes)}
			}
			svc.Deploy = d
		}

		for _, v := range spec.Volumes {
			svc.Volumes.items = append(svc.Volumes.items, v)
			if v.Type == VolumeVolume && v.Source != "" {
				namedVolumes[v.Source] = struct{}{}
			}
		}

		svc.Healthcheck = specHealthToCompose(spec.Health)
		svc.Restart = spec.Restart.Policy
		svc.Privileged = spec.Security.Privileged
		svc.ReadOnly = spec.Security.ReadonlyRootfs
		svc.Init = spec.Security.Init
		svc.CapAdd = spec.Security.CapAdd
		svc.CapDrop = spec.Security.CapDrop
		svc.StopSignal = spec.Security.StopSignal
		if spec.Security.StopGracePeriod > 0 {
			svc.StopGrace = strconv.Itoa(spec.Security.StopGracePeriod) + "s"
		}
		for k, v := range spec.Security.Labels {
			svc.Labels.items = append(svc.Labels.items, kvPair{k: k, v: v})
		}

		out.Services[spec.Name] = svc
	}

	if len(namedVolumes) > 0 {
		out.Volumes = map[string]any{}
		for n := range namedVolumes {
			out.Volumes[n] = nil
		}
	}

	data, err := yaml.Marshal(out)
	if err != nil {
		return nil, rep, err
	}
	return map[string][]byte{"docker-compose.yml": data}, rep, nil
}

// bundleForExport is a small shim so Export can reuse the settings<->spec
// reconciliations (secret placeholdering, ref placeholdering) without a store:
// it round-trips the spec through the bridge. In practice deploy/export.go
// passes a real Bundle; here we just want the note side effects.
func bundleForExport(spec ServiceSpec) Bundle {
	b, _ := BundleFromSpec(spec)
	return b
}

// --- compose YAML shapes ----------------------------------------------------

type composeFile struct {
	Services map[string]composeService `yaml:"services"`
	Volumes  map[string]any            `yaml:"volumes,omitempty"`
}

type composeService struct {
	Image       string          `yaml:"image,omitempty"`
	Build       *composeBuild   `yaml:"build,omitempty"`
	Command     flexStringList  `yaml:"command,omitempty"`
	Entrypoint  flexStringList  `yaml:"entrypoint,omitempty"`
	WorkingDir  string          `yaml:"working_dir,omitempty"`
	User        string          `yaml:"user,omitempty"`
	Environment flexKeyVal      `yaml:"environment,omitempty"`
	EnvFile     flexStringList  `yaml:"env_file,omitempty"`
	Ports       []string        `yaml:"ports,omitempty"`
	Deploy      *composeDeploy  `yaml:"deploy,omitempty"`
	Volumes     flexVolumes     `yaml:"volumes,omitempty"`
	Healthcheck *composeHealth  `yaml:"healthcheck,omitempty"`
	Restart     string          `yaml:"restart,omitempty"`
	CapAdd      []string        `yaml:"cap_add,omitempty"`
	CapDrop     []string        `yaml:"cap_drop,omitempty"`
	Privileged  bool            `yaml:"privileged,omitempty"`
	ReadOnly    bool            `yaml:"read_only,omitempty"`
	Init        bool            `yaml:"init,omitempty"`
	StopGrace   string          `yaml:"stop_grace_period,omitempty"`
	StopSignal  string          `yaml:"stop_signal,omitempty"`
	Labels      flexKeyVal      `yaml:"labels,omitempty"`
	DependsOn   flexStringList  `yaml:"depends_on,omitempty"`
	MemLimit    string          `yaml:"mem_limit,omitempty"`
}

type composeBuild struct {
	Context    string     `yaml:"context,omitempty"`
	Dockerfile string     `yaml:"dockerfile,omitempty"`
	Target     string     `yaml:"target,omitempty"`
	Args       flexKeyVal `yaml:"args,omitempty"`
}

// UnmarshalYAML accepts either a scalar context path or the long-form map.
func (b *composeBuild) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind == yaml.ScalarNode {
		b.Context = node.Value
		return nil
	}
	type raw composeBuild
	var r raw
	if err := node.Decode(&r); err != nil {
		return err
	}
	*b = composeBuild(r)
	return nil
}

type composeDeploy struct {
	Resources *composeResources `yaml:"resources,omitempty"`
}

type composeResources struct {
	Limits       *composeResLimit `yaml:"limits,omitempty"`
	Reservations *composeResLimit `yaml:"reservations,omitempty"`
}

type composeResLimit struct {
	CPUs   string `yaml:"cpus,omitempty"`
	Memory string `yaml:"memory,omitempty"`
}

type composeHealth struct {
	Test        flexStringList `yaml:"test,omitempty"`
	Interval    string         `yaml:"interval,omitempty"`
	Timeout     string         `yaml:"timeout,omitempty"`
	Retries     int            `yaml:"retries,omitempty"`
	StartPeriod string         `yaml:"start_period,omitempty"`
	Disable     bool           `yaml:"disable,omitempty"`
}

// --- flexible compose scalar/list/map types --------------------------------

// flexStringList decodes a scalar string or a sequence of strings into items.
type flexStringList struct{ items []string }

func (f *flexStringList) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.ScalarNode:
		if node.Value != "" {
			f.items = []string{node.Value}
		}
	case yaml.SequenceNode:
		for _, c := range node.Content {
			f.items = append(f.items, c.Value)
		}
	}
	return nil
}

func (f flexStringList) MarshalYAML() (any, error) {
	if len(f.items) == 0 {
		return nil, nil
	}
	return f.items, nil
}

// IsZero lets yaml.v3 omitempty work: without it, a struct whose only field is
// unexported is always treated as zero and silently dropped.
func (f flexStringList) IsZero() bool { return len(f.items) == 0 }

type kvPair struct{ k, v string }

// flexKeyVal decodes a mapping {K: V} or a sequence ["K=V"] into ordered pairs.
type flexKeyVal struct{ items []kvPair }

func (f *flexKeyVal) UnmarshalYAML(node *yaml.Node) error {
	switch node.Kind {
	case yaml.MappingNode:
		for i := 0; i+1 < len(node.Content); i += 2 {
			f.items = append(f.items, kvPair{k: node.Content[i].Value, v: node.Content[i+1].Value})
		}
	case yaml.SequenceNode:
		for _, c := range node.Content {
			k, v, _ := strings.Cut(c.Value, "=")
			f.items = append(f.items, kvPair{k: k, v: v})
		}
	}
	return nil
}

func (f flexKeyVal) MarshalYAML() (any, error) {
	if len(f.items) == 0 {
		return nil, nil
	}
	m := yaml.Node{Kind: yaml.MappingNode}
	for _, kv := range f.items {
		m.Content = append(m.Content,
			&yaml.Node{Kind: yaml.ScalarNode, Value: kv.k},
			&yaml.Node{Kind: yaml.ScalarNode, Value: kv.v})
	}
	return m, nil
}

func (f flexKeyVal) IsZero() bool { return len(f.items) == 0 }

// flexVolumes decodes compose volume entries: short "src:dst[:ro]" strings or
// long-form maps {type, source, target, read_only}.
type flexVolumes struct{ items []VolumeMount }

func (f *flexVolumes) UnmarshalYAML(node *yaml.Node) error {
	if node.Kind != yaml.SequenceNode {
		return nil
	}
	for _, c := range node.Content {
		switch c.Kind {
		case yaml.ScalarNode:
			if m, ok := parseShortVolume(c.Value); ok {
				f.items = append(f.items, m)
			}
		case yaml.MappingNode:
			var lv struct {
				Type     string `yaml:"type"`
				Source   string `yaml:"source"`
				Target   string `yaml:"target"`
				ReadOnly bool   `yaml:"read_only"`
			}
			if err := c.Decode(&lv); err == nil && lv.Target != "" {
				t := lv.Type
				if t == "" {
					t = VolumeBind
				}
				f.items = append(f.items, VolumeMount{Type: t, Source: lv.Source, ContainerPath: lv.Target, ReadOnly: lv.ReadOnly})
			}
		}
	}
	return nil
}

func (f flexVolumes) MarshalYAML() (any, error) {
	if len(f.items) == 0 {
		return nil, nil
	}
	out := make([]string, 0, len(f.items))
	for _, m := range f.items {
		s := m.ContainerPath
		if m.Source != "" {
			s = m.Source + ":" + m.ContainerPath
		}
		if m.ReadOnly {
			s += ":ro"
		}
		out = append(out, s)
	}
	return out, nil
}

func (f flexVolumes) IsZero() bool { return len(f.items) == 0 }

// parseShortVolume parses "src:dst", "src:dst:ro", or "dst".
func parseShortVolume(s string) (VolumeMount, bool) {
	parts := strings.Split(s, ":")
	switch len(parts) {
	case 1:
		return VolumeMount{Type: VolumeVolume, ContainerPath: parts[0]}, parts[0] != ""
	case 2, 3:
		src, dst := parts[0], parts[1]
		ro := len(parts) == 3 && parts[2] == "ro"
		t := VolumeBind
		if !strings.Contains(src, "/") && !strings.Contains(src, "\\") && !strings.HasPrefix(src, ".") {
			t = VolumeVolume // a bare name, not a path → named volume
		}
		return VolumeMount{Type: t, Source: src, ContainerPath: dst, ReadOnly: ro}, dst != ""
	}
	return VolumeMount{}, false
}

// --- port / resource / health helpers --------------------------------------

// parseComposePort parses "8080:80", "80", "127.0.0.1:8080:80", any with an
// optional "/proto" suffix.
func parseComposePort(s string) (PortSpec, bool) {
	proto := "tcp"
	if i := strings.Index(s, "/"); i >= 0 {
		proto = s[i+1:]
		s = s[:i]
	}
	parts := strings.Split(s, ":")
	atoi := func(v string) int { n, _ := strconv.Atoi(strings.TrimSpace(v)); return n }
	switch len(parts) {
	case 1:
		c := atoi(parts[0])
		return PortSpec{Container: c, Protocol: proto}, c > 0
	case 2:
		return PortSpec{Published: atoi(parts[0]), Container: atoi(parts[1]), Protocol: proto}, atoi(parts[1]) > 0
	case 3: // ip:published:container
		return PortSpec{Published: atoi(parts[1]), Container: atoi(parts[2]), Protocol: proto}, atoi(parts[2]) > 0
	}
	return PortSpec{}, false
}

func formatComposePort(p PortSpec) string {
	s := strconv.Itoa(p.Container)
	if p.Published > 0 {
		s = strconv.Itoa(p.Published) + ":" + strconv.Itoa(p.Container)
	}
	if p.Protocol != "" && p.Protocol != "tcp" {
		s += "/" + p.Protocol
	}
	return s
}

func applyComposeResources(r *ResourceSpec, cr *composeResources, rep *Report) {
	if cr.Limits != nil {
		if cr.Limits.CPUs != "" {
			if f, err := strconv.ParseFloat(cr.Limits.CPUs, 64); err == nil {
				r.MilliCPU = int(f * 1000)
			}
		}
		if cr.Limits.Memory != "" {
			if n, err := parseMem(cr.Limits.Memory); err == nil {
				r.MemoryBytes = n
			}
		}
	}
	if cr.Reservations != nil && cr.Reservations.Memory != "" {
		if n, err := parseMem(cr.Reservations.Memory); err == nil {
			r.MemoryReservBytes = n
		}
	}
}

func composeHealthToSpec(h *composeHealth) *HealthSpec {
	if h == nil {
		return nil
	}
	if h.Disable {
		return &HealthSpec{Disable: true}
	}
	test := h.Test.items
	if len(test) == 0 {
		return nil
	}
	// test forms: ["CMD", "curl", ...], ["CMD-SHELL", "..."], or a bare string.
	var cmd []string
	switch test[0] {
	case "CMD":
		cmd = test[1:]
	case "CMD-SHELL":
		cmd = shellSplit(strings.Join(test[1:], " "))
	case "NONE":
		return &HealthSpec{Disable: true}
	default:
		cmd = shellSplit(strings.Join(test, " "))
	}
	return &HealthSpec{
		Kind: HealthCmd, Command: cmd,
		Interval: h.Interval, Timeout: h.Timeout, StartPeriod: h.StartPeriod, Retries: h.Retries,
	}
}

func specHealthToCompose(h *HealthSpec) *composeHealth {
	if h == nil {
		return nil
	}
	if h.Disable {
		return &composeHealth{Disable: true}
	}
	ch := &composeHealth{Interval: h.Interval, Timeout: h.Timeout, StartPeriod: h.StartPeriod, Retries: h.Retries}
	if len(h.Command) > 0 {
		ch.Test = flexStringList{items: append([]string{"CMD"}, h.Command...)}
	} else if h.Kind == HealthHTTP {
		ch.Test = flexStringList{items: []string{"CMD-SHELL", fmt.Sprintf("wget -qO- --spider http://127.0.0.1:%d%s || exit 1", h.Port, h.Path)}}
	}
	return ch
}

func orDot(s string) string {
	if s == "" {
		return "."
	}
	return s
}

// parseMem parses a compose memory string ("512M", "1g", "256mb") to bytes.
func parseMem(s string) (int64, error) { return unitsRAMInBytes(s) }
