// Package cloudconfig converts between external cloud deployment formats
// (docker-compose, Cloud Run, ECS, Azure Container Apps) and Draft's internal
// service model. It is deliberately standalone: it depends on no other Draft
// internal package and operates on plain settings maps plus its own transient
// intermediate representation (ServiceSpec). node_settings remains the single
// source of truth; ServiceSpec exists only at the conversion boundary.
//
// The design rests on one observation: every field of every supported format
// sorts into one of four layers, and only two are Draft's job to run —
//
//   - run-contract  : how the container behaves (env, ports, resources, health,
//     restart) → maps bijectively to node_settings.
//   - build-recipe  : what the image contains (dockerfile, context, args,
//     target) → maps bijectively to Draft's build settings.
//   - control plane : orchestration around it (autoscaling, IAM, CI triggers,
//     push targets) → not run locally; preserved verbatim in Extra for
//     round-trip export.
//   - transport     : Draft-local dev-loop mechanics (git-stream, buildkit,
//     ignore toggles) → Draft-only; defaulted on import, omitted on export.
//
// Adapters own the first two mappings for their format; the bridge (bridge.go)
// owns the single settings<->spec mapping shared by all of them.
package cloudconfig

// Env scope values. These mirror store.EnvScope* by value; cloudconfig keeps
// its own copy so the package stays dependency-free.
const (
	ScopeRuntime = "runtime"
	ScopeBuild   = "build"
	ScopeBoth    = "both"
)

// Volume type values, mirroring deploy.VolumeType* / store TemplateVolume.
const (
	VolumeBind   = "bind"
	VolumeVolume = "volume"
)

// Health kinds.
const (
	HealthCmd  = "cmd"
	HealthHTTP = "http"
	HealthTCP  = "tcp"
)

// ServiceSpec is the transient, Draft-shaped intermediate representation every
// adapter maps to and from. It mirrors the container run-contract Draft stores
// in node_settings (see internal/deploy/settings.go) plus the build recipe.
// Exactly one of Image or Build is meaningfully set: a non-empty Image means
// image-mode (pull + run); a non-nil Build means build-mode.
type ServiceSpec struct {
	Name string

	Image string     // image-mode: prebuilt image ref
	Build *BuildSpec // build-mode: how to produce the image

	Command    []string
	Entrypoint []string
	WorkingDir string
	User       string

	Env       []EnvVar
	EnvFiles  []string // compose env_file paths (→ node_settings.env_file + imported values)
	Ports     []PortSpec
	Resources ResourceSpec
	Volumes   []VolumeMount
	Health    *HealthSpec
	Restart   RestartSpec
	Security  SecuritySpec

	// Target is the target engine this spec came from / is headed to
	// (cloudrun|ecs|containerapps|compose). It drives platform-conventional env
	// injection at deploy time (internal/deploy handles that, not this package).
	Target string

	// Extra carries control-plane / pipeline scaffolding the adapter parsed but
	// Draft does not run (autoscaling, IAM, test/push steps). It is preserved so
	// an in-process import→export keeps them; durable round-trip is handled by
	// deploy storing the original document text.
	Extra map[string]any
}

// BuildSpec is the extractable docker-build recipe: the single `docker build`
// invocation inside an otherwise-imperative build pipeline.
type BuildSpec struct {
	Context    string   // build context dir (→ service_root)
	Dockerfile string   // → dockerfile
	Target     string   // multistage target (→ build_target)
	Platform   string   // → build_platform
	Args       []EnvVar // build args (Scope treated as build)

	// PreSteps/PostSteps are non-docker-build shell steps from the pipeline,
	// mapped best-effort to Draft's pre/post-build lifecycle hooks.
	PreSteps  []string
	PostSteps []string
}

// EnvVar is one environment/build variable. Secret marks a value that came from
// (or should go to) a secret store rather than inline plaintext.
type EnvVar struct {
	Key    string
	Value  string
	Secret bool
	Scope  string // ScopeRuntime | ScopeBuild | ScopeBoth
}

// PortSpec is a single container port. Published is the host port when the
// format pins one (compose "8080:80"); 0 means Draft assigns it.
type PortSpec struct {
	Container int
	Published int
	Protocol  string // "tcp" (default) | "udp"
}

// ResourceSpec is canonical: CPU in millicores (1000 = 1 vCPU) and memory in
// bytes, so each adapter does its own unit math (ECS uses 1024 units per vCPU).
// A zero field means unset.
type ResourceSpec struct {
	MilliCPU          int
	MemoryBytes       int64
	MemoryReservBytes int64
	PidsLimit         int64
}

// VolumeMount mirrors the persisted volume_mounts JSON shape. Note the target
// field is ContainerPath (json "containerPath"), matching deploy.VolumeSpec —
// there is no "target" key.
type VolumeMount struct {
	Type          string // VolumeBind (default) | VolumeVolume
	Source        string // bind: host path; volume: name ("" = Draft auto-name)
	ContainerPath string
	ReadOnly      bool
}

// HealthSpec is format-neutral. Kind selects which fields are meaningful:
// cmd → Command; http → Path+Port; tcp → Port. Durations are Go duration
// strings ("10s"), matching how Draft stores and parses them.
type HealthSpec struct {
	Kind        string
	Command     []string
	Path        string
	Port        int
	Interval    string
	Timeout     string
	Retries     int
	StartPeriod string
	Disable     bool
}

// RestartSpec maps to Draft's restart_policy/restart_max_retries. Policy is one
// of "" | "no" | "always" | "on-failure" | "unless-stopped".
type RestartSpec struct {
	Policy     string
	MaxRetries int
}

// SecuritySpec collects the security/lifecycle tail. Most cloud run-specs carry
// little of this (managed platforms); compose and ECS carry the most.
type SecuritySpec struct {
	Privileged      bool
	ReadonlyRootfs  bool
	Init            bool
	CapAdd          []string
	CapDrop         []string
	StopGracePeriod int // seconds, 0 = unset
	StopSignal      string
	Labels          map[string]string
}

// IsImageMode reports whether the spec runs a prebuilt image rather than
// building one.
func (s ServiceSpec) IsImageMode() bool {
	return s.Image != "" && s.Build == nil
}
