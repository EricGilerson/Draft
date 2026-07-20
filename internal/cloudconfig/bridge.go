package cloudconfig

import (
	"encoding/json"
	"fmt"
	"regexp"
	"sort"
	"strconv"
	"strings"

	"github.com/docker/go-units"
)

// draftRefRe matches Draft's @{Service.ATTR} env-reference syntax (see
// deploy/refs.go). On export these have no meaning on a real cloud platform, so
// they become ${SERVICE_ATTR} placeholders the user wires to a production
// endpoint.
var draftRefRe = regexp.MustCompile(`@\{([^.}]+)\.([A-Za-z0-9_]+)\}`)

// placeholderizeRefs replaces every @{Service.ATTR} with a ${SERVICE_ATTR}
// environment placeholder (non-alphanumeric characters in the service name
// become underscores, uppercased).
func placeholderizeRefs(value string) string {
	return draftRefRe.ReplaceAllStringFunc(value, func(m string) string {
		sub := draftRefRe.FindStringSubmatch(m)
		name := strings.ToUpper(nonAlnumRe.ReplaceAllString(sub[1], "_"))
		attr := strings.ToUpper(sub[2])
		return "${" + name + "_" + attr + "}"
	})
}

var nonAlnumRe = regexp.MustCompile(`[^A-Za-z0-9]+`)

// Bundle is the neutral Draft-side payload the bridge produces and consumes: a
// node_settings map plus the env var list. It is exactly what deploy/import.go
// writes to the store and what deploy/export.go reads from it — the bridge
// never touches the store itself, keeping this package dependency-free.
type Bundle struct {
	Settings map[string]string
	Env      []EnvVar
}

// volumeJSON is the on-disk shape of one volume_mounts entry. It matches
// deploy.VolumeSpec / store.TemplateVolume by JSON tag (target is
// "containerPath", not "target").
type volumeJSON struct {
	Type          string `json:"type,omitempty"`
	Source        string `json:"source,omitempty"`
	ContainerPath string `json:"containerPath"`
	ReadOnly      bool   `json:"readOnly,omitempty"`
}

// BundleFromSpec maps a ServiceSpec onto a Draft settings bundle (the import
// direction). It owns the env scope-split and secret reconciliations; the
// cross-service rewrite is applied separately by RewriteCrossServiceRefs once
// all sibling specs are known.
func BundleFromSpec(s ServiceSpec) (Bundle, Report) {
	var rep Report
	settings := map[string]string{}

	// Source: image-mode vs build-mode.
	if s.Build != nil {
		if s.Build.Dockerfile != "" {
			settings["dockerfile"] = s.Build.Dockerfile
		} else {
			settings["dockerfile"] = "Dockerfile"
		}
		if s.Build.Context != "" {
			settings["service_root"] = s.Build.Context
		}
		if s.Build.Target != "" {
			settings["build_target"] = s.Build.Target
		}
		if s.Build.Platform != "" {
			settings["build_platform"] = s.Build.Platform
		}
		if len(s.Build.PreSteps) > 0 {
			settings["pre_build_cmd"] = strings.Join(s.Build.PreSteps, " && ")
			rep.Add(KindTransformed, "build_pre_steps", "pre_build_cmd",
				"Non-build pipeline steps were mapped to a pre-build hook; review them.")
		}
		if len(s.Build.PostSteps) > 0 {
			settings["post_build_cmd"] = strings.Join(s.Build.PostSteps, " && ")
			rep.Add(KindTransformed, "build_post_steps", "post_build_cmd",
				"Non-build pipeline steps were mapped to a post-build hook; review them.")
		}
	} else if s.Image != "" {
		settings["image"] = s.Image
	}

	// Primary port → service_port; extras are recorded but not run.
	if len(s.Ports) > 0 {
		settings["service_port"] = strconv.Itoa(s.Ports[0].Container)
		if s.Ports[0].Published > 0 {
			settings["host_port"] = strconv.Itoa(s.Ports[0].Published)
		}
		for _, p := range s.Ports[1:] {
			rep.Add(KindIgnored, "extra_port", "service_port",
				fmt.Sprintf("Additional container port %d is not mapped; Draft runs one service port per node.", p.Container))
		}
	}

	// Command / entrypoint / working dir / user.
	if len(s.Command) > 0 {
		settings["cmd_override"] = shellJoin(s.Command)
	}
	if len(s.Entrypoint) > 0 {
		settings["entrypoint_override"] = shellJoin(s.Entrypoint)
	}
	if s.WorkingDir != "" {
		settings["working_dir"] = s.WorkingDir
	}
	if s.User != "" {
		settings["run_user"] = s.User
	}

	// Resources.
	if s.Resources.MilliCPU > 0 {
		settings["cpu_limit"] = strconv.FormatFloat(float64(s.Resources.MilliCPU)/1000, 'g', -1, 64)
	}
	if s.Resources.MemoryBytes > 0 {
		settings["memory_limit"] = ramToString(s.Resources.MemoryBytes)
	}
	if s.Resources.MemoryReservBytes > 0 {
		settings["memory_reservation"] = ramToString(s.Resources.MemoryReservBytes)
	}
	if s.Resources.PidsLimit > 0 {
		settings["pids_limit"] = strconv.FormatInt(s.Resources.PidsLimit, 10)
	}

	// Restart.
	if p := normalizeRestart(s.Restart.Policy); p != "" {
		settings["restart_policy"] = p
		if p == "on-failure" && s.Restart.MaxRetries > 0 {
			settings["restart_max_retries"] = strconv.Itoa(s.Restart.MaxRetries)
		}
	}

	// Health.
	writeHealth(settings, s.Health, &rep)

	// Volumes.
	if len(s.Volumes) > 0 {
		entries := make([]volumeJSON, 0, len(s.Volumes))
		for _, v := range s.Volumes {
			t := v.Type
			if t == "" {
				t = VolumeBind
			}
			entries = append(entries, volumeJSON{
				Type:          t,
				Source:        v.Source,
				ContainerPath: v.ContainerPath,
				ReadOnly:      v.ReadOnly,
			})
		}
		if raw, err := json.Marshal(entries); err == nil {
			settings["volume_mounts"] = string(raw)
		}
	}

	// Draft tracks a single env_file path; compose may list several — keep the first
	// as the refresh path and note when extras were folded in.
	if len(s.EnvFiles) > 0 {
		settings["env_file"] = s.EnvFiles[0]
		if len(s.EnvFiles) > 1 {
			rep.Add(KindTransformed, "env_file_multiple", "env_file",
				fmt.Sprintf("Multiple env_file entries were merged; Draft tracks %q for refresh.", s.EnvFiles[0]))
		}
	}

	// Security tail.
	if s.Security.Privileged {
		settings["privileged"] = "true"
	}
	if s.Security.ReadonlyRootfs {
		settings["readonly_rootfs"] = "true"
	}
	if s.Security.Init {
		settings["init_process"] = "true"
	}
	if len(s.Security.CapAdd) > 0 {
		settings["cap_add"] = strings.Join(s.Security.CapAdd, ",")
	}
	if len(s.Security.CapDrop) > 0 {
		settings["cap_drop"] = strings.Join(s.Security.CapDrop, ",")
	}
	if s.Security.StopGracePeriod > 0 {
		settings["stop_grace_period"] = strconv.Itoa(s.Security.StopGracePeriod)
	}
	if s.Security.StopSignal != "" {
		settings["stop_signal"] = s.Security.StopSignal
	}
	if len(s.Security.Labels) > 0 {
		if raw, err := json.Marshal(s.Security.Labels); err == nil {
			settings["custom_labels"] = string(raw)
		}
	}

	if s.Target != "" {
		settings["target_engine"] = s.Target
	}

	// Env: reconcile container env with build args by identity. Build args are
	// carried on Build.Args; a var present in both becomes scope=both.
	env := reconcileEnv(s, &rep)

	return Bundle{Settings: settings, Env: env}, rep
}

// SpecFromBundle maps a Draft settings bundle back onto a ServiceSpec (the
// export direction). It inverts the reconciliations: build-scoped env becomes
// Build.Args, secret vars become secret refs, @{...} references are left for
// the adapter to turn into ${VAR} placeholders (a note is added here).
func SpecFromBundle(b Bundle) (ServiceSpec, Report) {
	var rep Report
	s := ServiceSpec{Extra: map[string]any{}}
	get := func(k string) string { return strings.TrimSpace(b.Settings[k]) }

	s.Target = get("target_engine")

	// Source mode.
	if df := get("dockerfile"); df != "" {
		s.Build = &BuildSpec{
			Dockerfile: df,
			Context:    get("service_root"),
			Target:     get("build_target"),
			Platform:   get("build_platform"),
		}
		if v := get("pre_build_cmd"); v != "" {
			s.Build.PreSteps = []string{v}
		}
		if v := get("post_build_cmd"); v != "" {
			s.Build.PostSteps = []string{v}
		}
	} else if img := get("image"); img != "" {
		s.Image = img
	}

	if v := get("service_port"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			p := PortSpec{Container: n, Protocol: "tcp"}
			if hp := get("host_port"); hp != "" {
				if h, err := strconv.Atoi(hp); err == nil {
					p.Published = h
				}
			}
			s.Ports = []PortSpec{p}
		}
	}

	if v := get("cmd_override"); v != "" {
		s.Command = shellSplit(v)
	}
	if v := get("entrypoint_override"); v != "" {
		s.Entrypoint = shellSplit(v)
	}
	s.WorkingDir = get("working_dir")
	s.User = get("run_user")

	if v := get("cpu_limit"); v != "" {
		if f, err := strconv.ParseFloat(v, 64); err == nil {
			s.Resources.MilliCPU = int(f * 1000)
		}
	}
	if v := get("memory_limit"); v != "" {
		if n, err := units.RAMInBytes(v); err == nil {
			s.Resources.MemoryBytes = n
		}
	}
	if v := get("memory_reservation"); v != "" {
		if n, err := units.RAMInBytes(v); err == nil {
			s.Resources.MemoryReservBytes = n
		}
	}
	if v := get("pids_limit"); v != "" {
		if n, err := strconv.ParseInt(v, 10, 64); err == nil {
			s.Resources.PidsLimit = n
		}
	}

	if v := normalizeRestart(get("restart_policy")); v != "" {
		s.Restart.Policy = v
		if n, err := strconv.Atoi(get("restart_max_retries")); err == nil {
			s.Restart.MaxRetries = n
		}
	}

	s.Health = readHealth(b.Settings)

	if ef := get("env_file"); ef != "" {
		s.EnvFiles = []string{ef}
	}

	if raw := get("volume_mounts"); raw != "" {
		var entries []volumeJSON
		if json.Unmarshal([]byte(raw), &entries) == nil {
			for _, e := range entries {
				if e.ContainerPath == "" {
					continue
				}
				t := e.Type
				if t == "" {
					t = VolumeBind
				}
				s.Volumes = append(s.Volumes, VolumeMount{
					Type:          t,
					Source:        e.Source,
					ContainerPath: e.ContainerPath,
					ReadOnly:      e.ReadOnly,
				})
			}
		}
	}

	s.Security.Privileged = get("privileged") == "true"
	s.Security.ReadonlyRootfs = get("readonly_rootfs") == "true"
	s.Security.Init = get("init_process") == "true"
	s.Security.CapAdd = splitCSV(get("cap_add"))
	s.Security.CapDrop = splitCSV(get("cap_drop"))
	if v := get("stop_grace_period"); v != "" {
		if n, err := strconv.Atoi(v); err == nil {
			s.Security.StopGracePeriod = n
		}
	}
	s.Security.StopSignal = get("stop_signal")
	if raw := get("custom_labels"); raw != "" {
		labels := map[string]string{}
		if json.Unmarshal([]byte(raw), &labels) == nil {
			s.Security.Labels = labels
		}
	}

	// Env: split back into runtime env and build args, and flag @{...} refs.
	buildArgs := map[string]bool{}
	for _, v := range b.Env {
		if v.Scope == ScopeBuild || v.Scope == ScopeBoth {
			buildArgs[v.Key] = true
		}
	}
	for _, v := range b.Env {
		if strings.Contains(v.Value, "@{") {
			v.Value = placeholderizeRefs(v.Value)
			rep.Add(KindTransformed, "cross_service_ref", v.Key,
				fmt.Sprintf("%s referenced another Draft service; exported with a ${...} placeholder to wire to a real endpoint.", v.Key))
		}
		if v.Scope != ScopeBuild { // runtime or both → container env
			s.Env = append(s.Env, v)
		}
	}
	if s.Build != nil {
		for _, v := range b.Env {
			if v.Scope == ScopeBuild || v.Scope == ScopeBoth {
				arg := v
				arg.Scope = ScopeBuild
				s.Build.Args = append(s.Build.Args, arg)
			}
		}
	}

	return s, rep
}

// reconcileEnv builds the persisted env list from a spec's container env plus
// build args, merging by key: present in both → scope=both; build-arg only →
// build; container-env only → runtime. Secret refs import as empty-valued
// secret vars with a manual note.
func reconcileEnv(s ServiceSpec, rep *Report) []EnvVar {
	byKey := map[string]*EnvVar{}
	order := []string{}
	upsert := func(key string) *EnvVar {
		if e, ok := byKey[key]; ok {
			return e
		}
		e := &EnvVar{Key: key}
		byKey[key] = e
		order = append(order, key)
		return e
	}

	for _, v := range s.Env {
		e := upsert(v.Key)
		e.Value = v.Value
		e.Secret = v.Secret
		e.Scope = ScopeRuntime
		if v.Secret && v.Value == "" {
			rep.Add(KindManual, "secret_value", v.Key,
				fmt.Sprintf("%s is a secret with no local value; set it before deploying.", v.Key))
		}
	}
	if s.Build != nil {
		for _, a := range s.Build.Args {
			e := upsert(a.Key)
			if e.Scope == ScopeRuntime {
				e.Scope = ScopeBoth // was container env too
			} else {
				e.Scope = ScopeBuild
				e.Value = a.Value
				e.Secret = a.Secret
			}
		}
	}

	out := make([]EnvVar, 0, len(order))
	for _, k := range order {
		out = append(out, *byKey[k])
	}
	return out
}

// RewriteCrossServiceRefs scans every spec's env values for a plaintext
// reference to a sibling service (by name, as a host in a URL or bare host) and
// rewrites it to Draft's @{Name.DRAFT_INTERNAL_HOSTNAME} form so local service
// discovery works. Best-effort; only whole-token host matches are rewritten.
func RewriteCrossServiceRefs(specs []ServiceSpec) Report {
	var rep Report
	names := make([]string, 0, len(specs))
	for _, s := range specs {
		if s.Name != "" {
			names = append(names, s.Name)
		}
	}
	// Longest names first so "auth-db" wins over "auth".
	sort.Slice(names, func(i, j int) bool { return len(names[i]) > len(names[j]) })

	for si := range specs {
		self := specs[si].Name
		for ei := range specs[si].Env {
			val := specs[si].Env[ei].Value
			if val == "" || strings.Contains(val, "@{") {
				continue
			}
			for _, name := range names {
				if name == self {
					continue
				}
				if !hostToken(val, name) {
					continue
				}
				ref := "@{" + name + ".DRAFT_INTERNAL_HOSTNAME}"
				specs[si].Env[ei].Value = replaceHostToken(val, name, ref)
				rep.Add(KindTransformed, "cross_service_ref", specs[si].Env[ei].Key,
					fmt.Sprintf("Rewired %s host %q to %s for local service discovery.", specs[si].Env[ei].Key, name, ref))
				break
			}
		}
	}
	return rep
}

// hostToken reports whether name appears in val as a standalone host token
// (delimited by scheme separators, slashes, colons, @, or string ends).
func hostToken(val, name string) bool {
	idx := strings.Index(val, name)
	for idx >= 0 {
		before := byte('/')
		if idx > 0 {
			before = val[idx-1]
		}
		afterPos := idx + len(name)
		after := byte('/')
		if afterPos < len(val) {
			after = val[afterPos]
		}
		if isHostBoundary(before) && isHostBoundary(after) {
			return true
		}
		next := strings.Index(val[idx+1:], name)
		if next < 0 {
			break
		}
		idx = idx + 1 + next
	}
	return false
}

func replaceHostToken(val, name, ref string) string {
	// Replace only boundary-delimited occurrences.
	var b strings.Builder
	i := 0
	for i < len(val) {
		if strings.HasPrefix(val[i:], name) {
			before := byte('/')
			if i > 0 {
				before = val[i-1]
			}
			afterPos := i + len(name)
			after := byte('/')
			if afterPos < len(val) {
				after = val[afterPos]
			}
			if isHostBoundary(before) && isHostBoundary(after) {
				b.WriteString(ref)
				i = afterPos
				continue
			}
		}
		b.WriteByte(val[i])
		i++
	}
	return b.String()
}

func isHostBoundary(c byte) bool {
	switch c {
	case '/', ':', '@', '=', ' ', ',', '\t':
		return true
	}
	return false
}

// --- helpers ---------------------------------------------------------------

func writeHealth(settings map[string]string, h *HealthSpec, rep *Report) {
	if h == nil {
		return
	}
	if h.Disable {
		settings["healthcheck_disable"] = "true"
		return
	}
	switch h.Kind {
	case HealthCmd:
		if len(h.Command) > 0 {
			settings["healthcheck_cmd"] = shellJoin(h.Command)
		}
	case HealthHTTP:
		path := h.Path
		if path == "" {
			path = "/"
		}
		settings["healthcheck_cmd"] = fmt.Sprintf("wget -qO- --spider http://127.0.0.1:%d%s || exit 1", h.Port, path)
		rep.Add(KindTransformed, "http_probe", "healthcheck_cmd",
			"HTTP probe converted to a shell healthcheck (wget); adjust if the image lacks wget.")
	case HealthTCP:
		settings["healthcheck_cmd"] = fmt.Sprintf("nc -z 127.0.0.1 %d || exit 1", h.Port)
		rep.Add(KindTransformed, "tcp_probe", "healthcheck_cmd",
			"TCP probe converted to a shell healthcheck (nc); adjust if the image lacks nc.")
	default:
		return
	}
	if h.Interval != "" {
		settings["healthcheck_interval"] = h.Interval
	}
	if h.Timeout != "" {
		settings["healthcheck_timeout"] = h.Timeout
	}
	if h.StartPeriod != "" {
		settings["healthcheck_start_period"] = h.StartPeriod
	}
	if h.Retries > 0 {
		settings["healthcheck_retries"] = strconv.Itoa(h.Retries)
	}
}

func readHealth(settings map[string]string) *HealthSpec {
	if strings.TrimSpace(settings["healthcheck_disable"]) == "true" {
		return &HealthSpec{Disable: true}
	}
	cmd := strings.TrimSpace(settings["healthcheck_cmd"])
	if cmd == "" {
		return nil
	}
	h := &HealthSpec{
		Kind:        HealthCmd,
		Command:     shellSplit(cmd),
		Interval:    strings.TrimSpace(settings["healthcheck_interval"]),
		Timeout:     strings.TrimSpace(settings["healthcheck_timeout"]),
		StartPeriod: strings.TrimSpace(settings["healthcheck_start_period"]),
	}
	if n, err := strconv.Atoi(strings.TrimSpace(settings["healthcheck_retries"])); err == nil {
		h.Retries = n
	}
	return h
}

func normalizeRestart(p string) string {
	switch strings.TrimSpace(p) {
	case "always":
		return "always"
	case "on-failure":
		return "on-failure"
	case "unless-stopped":
		return "unless-stopped"
	default:
		return ""
	}
}

// ramToString renders bytes as the most compact binary form Draft's
// units.RAMInBytes parses back exactly ("512m", "2g", …), falling back to raw
// bytes when no unit divides evenly.
func ramToString(b int64) string {
	const (
		k = 1 << 10
		m = 1 << 20
		g = 1 << 30
	)
	switch {
	case b%g == 0:
		return strconv.FormatInt(b/g, 10) + "g"
	case b%m == 0:
		return strconv.FormatInt(b/m, 10) + "m"
	case b%k == 0:
		return strconv.FormatInt(b/k, 10) + "k"
	default:
		return strconv.FormatInt(b, 10)
	}
}

func splitCSV(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := make([]string, 0, len(parts))
	for _, p := range parts {
		if t := strings.TrimSpace(p); t != "" {
			out = append(out, t)
		}
	}
	return out
}

// shellJoin renders argv as a shell string, quoting args that contain spaces or
// quotes. It is the inverse of shellSplit for the common cases.
func shellJoin(argv []string) string {
	parts := make([]string, 0, len(argv))
	for _, a := range argv {
		if a == "" {
			parts = append(parts, `""`)
			continue
		}
		if strings.ContainsAny(a, " \t\"'\\") {
			parts = append(parts, `"`+strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(a)+`"`)
			continue
		}
		parts = append(parts, a)
	}
	return strings.Join(parts, " ")
}

// shellSplit mirrors deploy.shellSplit: whitespace-delimited with single/double
// quotes and backslash escapes.
func shellSplit(s string) []string {
	var parts []string
	var cur strings.Builder
	inSingle, inDouble, escaped := false, false, false
	for _, r := range s {
		if escaped {
			cur.WriteRune(r)
			escaped = false
			continue
		}
		if r == '\\' && !inSingle {
			escaped = true
			continue
		}
		if r == '\'' && !inDouble {
			inSingle = !inSingle
			continue
		}
		if r == '"' && !inSingle {
			inDouble = !inDouble
			continue
		}
		if r == ' ' && !inSingle && !inDouble {
			if cur.Len() > 0 {
				parts = append(parts, cur.String())
				cur.Reset()
			}
			continue
		}
		cur.WriteRune(r)
	}
	if cur.Len() > 0 {
		parts = append(parts, cur.String())
	}
	return parts
}
