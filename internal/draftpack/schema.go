package draftpack

import "time"

// FormatID is the stable magic string for Draft packs.
const FormatID = "draftpack"

// CurrentVersion is the schema version written by this build.
const CurrentVersion = 1

// Scope values.
const (
	ScopeService     = "service"
	ScopeEnvironment = "environment"
	ScopeProject     = "project"
)

// ExportOptions controls what is included when building a pack. Zero value
// uses DefaultExportOptions (safe cross-machine defaults).
type ExportOptions struct {
	// IncludeSecretValues exports plaintext secret env / project-var values.
	// Default false: keys and secret flags only.
	IncludeSecretValues bool `json:"includeSecretValues"`

	// IncludeAppSecrets embeds referenced app-secret values. Default false.
	IncludeAppSecrets bool `json:"includeAppSecrets"`

	// IncludeBindHostPaths keeps bind-mount host paths. Default false (paths
	// almost never match on another machine).
	IncludeBindHostPaths bool `json:"includeBindHostPaths"`

	// IncludeServiceRoots keeps project-relative service_root settings.
	// Default true. Absolute roots are never exported.
	IncludeServiceRoots bool `json:"includeServiceRoots"`

	// IncludeProjectEnvVars includes project-scoped {{project.KEY}} values.
	// Default true.
	IncludeProjectEnvVars bool `json:"includeProjectEnvVars"`

	// IncludeSandboxProfiles includes sandbox creation profiles on project export.
	// Default false (profiles are optional recipes).
	IncludeSandboxProfiles bool `json:"includeSandboxProfiles"`

	// IncludeCanvasLayout keeps node x/y positions. Default true.
	IncludeCanvasLayout bool `json:"includeCanvasLayout"`

	// IncludeGitSettings keeps git_branch, deploy_trigger, redeploy_on_pull, git_stream.
	// Default true.
	IncludeGitSettings bool `json:"includeGitSettings"`

	// IncludeSourceConfig keeps original cloud-config documents used for overlay export.
	// Default true.
	IncludeSourceConfig bool `json:"includeSourceConfig"`

	// EnvironmentIDs limits project-scope export to these environments.
	// Empty means all non-sandbox environments.
	EnvironmentIDs []uint `json:"environmentIds,omitempty"`
}

// DefaultExportOptions returns safe cross-machine defaults.
func DefaultExportOptions() ExportOptions {
	return ExportOptions{
		IncludeSecretValues:    false,
		IncludeAppSecrets:      false,
		IncludeBindHostPaths:   false,
		IncludeServiceRoots:    true,
		IncludeProjectEnvVars:  true,
		IncludeSandboxProfiles: false,
		IncludeCanvasLayout:    true,
		IncludeGitSettings:     true,
		IncludeSourceConfig:    true,
	}
}

// Pack is the top-level Draft pack document (JSON).
type Pack struct {
	Format     string    `json:"format"`
	Version    int       `json:"version"`
	ExportedAt time.Time `json:"exportedAt"`
	Scope      string    `json:"scope"`

	// ContentHash is a sha256 hex digest of the pack body (excluding this field).
	// Set on export; verified on import when present.
	ContentHash string `json:"contentHash,omitempty"`

	// Options records what the exporter included (for import UI defaults).
	Options ExportOptions `json:"options"`

	Project          *ProjectPayload          `json:"project,omitempty"`
	Environments     []EnvironmentPayload     `json:"environments,omitempty"`
	Services         []ServicePayload         `json:"services,omitempty"`
	ProjectEnvVars   []ProjectVarPayload      `json:"projectEnvVars,omitempty"`
	SandboxProfiles  []SandboxProfilePayload  `json:"sandboxProfiles,omitempty"`
	AppSecrets       []AppSecretPayload       `json:"appSecrets,omitempty"`
	Report           Report                   `json:"report"`
}

// ProjectPayload is portable project metadata (no absolute path).
type ProjectPayload struct {
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	// PathHint is the last path segment of the original project path (not a
	// full path) so import can suggest a folder name.
	PathHint string `json:"pathHint,omitempty"`
}

// EnvironmentPayload is one environment in the pack.
type EnvironmentPayload struct {
	// Key is a stable pack-local id (slug) used to group services.
	Key       string `json:"key"`
	Name      string `json:"name"`
	Slug      string `json:"slug"`
	IsDefault bool   `json:"isDefault"`
}

// ServicePayload is one canvas service in the pack.
type ServicePayload struct {
	// Key is a pack-local stable id (not the DB node id).
	Key string `json:"key"`
	// EnvironmentKey matches EnvironmentPayload.Key.
	EnvironmentKey string `json:"environmentKey"`
	Label          string `json:"label"`
	X              float64 `json:"x,omitempty"`
	Y              float64 `json:"y,omitempty"`

	// Template identifies the source template without a local numeric id.
	Template *TemplateRef `json:"template,omitempty"`

	// Settings are effective node_settings after path/secret policy.
	Settings map[string]string `json:"settings,omitempty"`

	// Env is effective env vars after secret policy.
	Env []EnvVarPayload `json:"env,omitempty"`

	// ServiceLink is a portable link to another service by label + env key
	// (only when the target is also in the pack).
	ServiceLink *ServiceLinkPayload `json:"serviceLink,omitempty"`

	// NeedsServiceRoot is true when service_root was omitted or was absolute.
	NeedsServiceRoot bool `json:"needsServiceRoot,omitempty"`
	// ServiceRootHint is the original relative service_root when known.
	ServiceRootHint string `json:"serviceRootHint,omitempty"`

	// BindRemaps lists bind mounts that need a host path on import.
	BindRemaps []BindRemap `json:"bindRemaps,omitempty"`
}

// TemplateRef identifies a template by builtin name or user template snapshot.
type TemplateRef struct {
	// BuiltinName matches a built-in template Name when Builtin is true.
	BuiltinName string `json:"builtinName,omitempty"`
	// User is an embedded non-builtin template definition when the source was
	// a user template (so the pack is self-contained).
	User *UserTemplatePayload `json:"user,omitempty"`
}

// UserTemplatePayload is a portable user template definition.
type UserTemplatePayload struct {
	Name            string `json:"name"`
	Description     string `json:"description,omitempty"`
	Category        string `json:"category,omitempty"`
	Icon            string `json:"icon,omitempty"`
	Color           string `json:"color,omitempty"`
	Mode            string `json:"mode,omitempty"`
	Image           string `json:"image,omitempty"`
	ImageTags       string `json:"imageTags,omitempty"`
	Port            int    `json:"port,omitempty"`
	Dockerfile      string `json:"dockerfile,omitempty"`
	CmdOverride     string `json:"cmdOverride,omitempty"`
	Entrypoint      string `json:"entrypoint,omitempty"`
	WorkingDir      string `json:"workingDir,omitempty"`
	EnvVars         string `json:"envVars,omitempty"`
	Labels          string `json:"labels,omitempty"`
	Volumes         string `json:"volumes,omitempty"`
	Schema          string `json:"schema,omitempty"`
	DefaultSettings string `json:"defaultSettings,omitempty"`
}

// EnvVarPayload is one service env var in the pack.
type EnvVarPayload struct {
	Key    string `json:"key"`
	Value  string `json:"value,omitempty"`
	Scope  string `json:"scope,omitempty"`
	Secret bool   `json:"secret,omitempty"`
	Source string `json:"source,omitempty"`
	// ValueOmitted is true when a secret value was stripped on export.
	ValueOmitted bool `json:"valueOmitted,omitempty"`
}

// ProjectVarPayload is one project-scoped shared value.
type ProjectVarPayload struct {
	Key          string `json:"key"`
	Value        string `json:"value,omitempty"`
	Secret       bool   `json:"secret,omitempty"`
	ValueOmitted bool   `json:"valueOmitted,omitempty"`
}

// AppSecretPayload is an optional embedded app secret.
type AppSecretPayload struct {
	Key         string `json:"key"`
	Value       string `json:"value,omitempty"`
	Description string `json:"description,omitempty"`
	// ValueOmitted when only the key was listed (referenced but not embedded).
	ValueOmitted bool `json:"valueOmitted,omitempty"`
}

// SandboxProfilePayload is a portable sandbox profile (plan JSON frozen).
type SandboxProfilePayload struct {
	Name                string `json:"name"`
	Description         string `json:"description,omitempty"`
	PlanJSON            string `json:"planJson"`
	IsDefault           bool   `json:"isDefault,omitempty"`
	// SourceEnvironmentKey is empty for project-wide profiles.
	SourceEnvironmentKey string `json:"sourceEnvironmentKey,omitempty"`
}

// ServiceLinkPayload is a portable service_link (by label + env key).
type ServiceLinkPayload struct {
	RootEnvironmentKey string `json:"rootEnvironmentKey"`
	RootServiceLabel   string `json:"rootServiceLabel"`
}

// BindRemap describes a bind mount that needs a host path on the destination.
type BindRemap struct {
	ContainerPath string `json:"containerPath"`
	// OriginalHost is the path from the source machine (hint only).
	OriginalHost string `json:"originalHost,omitempty"`
	ReadOnly     bool   `json:"readOnly,omitempty"`
}

// ImportMode values.
const (
	ImportAsNewProject = "newProject"
	ImportIntoProject  = "intoProject"
)

// EnvImportMode values (intoProject only).
const (
	// EnvImportFlatten puts every selected service into EnvironmentID.
	EnvImportFlatten = "flatten"
	// EnvImportRecreate recreates pack environments (or maps by name) on the project.
	EnvImportRecreate = "recreate"
)

// LayoutMode values control canvas placement on import.
const (
	// LayoutAuto offsets pack nodes away from existing canvas content when needed,
	// and lays out a grid when the pack has no coordinates.
	LayoutAuto = "auto"
	// LayoutPreserve keeps pack x/y as-is (may overlap existing nodes).
	LayoutPreserve = "preserve"
	// LayoutGrid ignores pack coordinates and places services in a free grid.
	LayoutGrid = "grid"
)

// PreviewOptions scopes collision detection for import preview.
// Zero value still produces a pack summary; pass mode/target to detect unique-field clashes.
type PreviewOptions struct {
	Mode          string `json:"mode,omitempty"` // newProject | intoProject
	ProjectID     uint   `json:"projectId,omitempty"`
	EnvironmentID uint   `json:"environmentId,omitempty"`
	// Proposed new-project identity (defaults to pack project name when empty).
	ProjectName string `json:"projectName,omitempty"`
	ProjectPath string `json:"projectPath,omitempty"`
	// Optional proposed renames when re-previewing after the user edits collisions.
	ServiceLabelOverrides    map[string]string `json:"serviceLabelOverrides,omitempty"`
	EnvironmentNameOverrides map[string]string `json:"environmentNameOverrides,omitempty"`
	// HostPortOverrides maps pack service key → proposed host port.
	// Empty string means "clear fixed port" (no collision). Used for live re-preview.
	HostPortOverrides map[string]string `json:"hostPortOverrides,omitempty"`
	// ServiceKeys limits which pack services are considered (empty = all).
	ServiceKeys []string `json:"serviceKeys,omitempty"`
	// EnvImportMode is flatten | recreate (intoProject). Empty = flatten.
	EnvImportMode string `json:"envImportMode,omitempty"`
	// LayoutMode is auto | preserve | grid for the placement preview. Empty = auto.
	LayoutMode string `json:"layoutMode,omitempty"`
}

// Collision kinds for unique / identity fields.
const (
	CollisionProjectName     = "project_name"
	CollisionProjectPath     = "project_path"
	CollisionServiceLabel    = "service_label"
	CollisionEnvironmentName = "environment_name"
	CollisionHostPort        = "host_port"
	CollisionSandboxProfile  = "sandbox_profile"
)

// Collision is one unique-field clash the user can fix before import.
type Collision struct {
	// Kind is one of the Collision* constants.
	Kind string `json:"kind"`
	// Field is a stable key for overrides (e.g. service pack key, env key, "project").
	Field string `json:"field"`
	// Label is a human-readable subject (service name, project, …).
	Label string `json:"label"`
	// Current is the value from the pack / proposal that collides.
	Current string `json:"current"`
	// Suggested is a free unique alternative (empty when the user must pick, e.g. path).
	Suggested string `json:"suggested,omitempty"`
	// Message explains the clash.
	Message string `json:"message"`
	// Blocking: import should not proceed until fixed (path) vs auto-renamable.
	Blocking bool `json:"blocking"`
}

// ImportOptions controls how a pack is applied.
type ImportOptions struct {
	Mode string `json:"mode"` // newProject | intoProject

	// New project
	ProjectName string `json:"projectName,omitempty"`
	ProjectPath string `json:"projectPath,omitempty"`

	// Existing project / environment (intoProject)
	ProjectID     uint `json:"projectId,omitempty"`
	EnvironmentID uint `json:"environmentId,omitempty"`

	// EnvImportMode is flatten | recreate for intoProject. Empty = flatten.
	EnvImportMode string `json:"envImportMode,omitempty"`

	// LayoutMode is auto | preserve | grid. Empty = auto.
	LayoutMode string `json:"layoutMode,omitempty"`

	// ServiceKeys limits which pack services to import (empty = all).
	ServiceKeys []string `json:"serviceKeys,omitempty"`

	// ServiceLabelOverrides maps pack service key → final canvas label.
	// When omitted, colliding labels are auto-suffixed (api → api-2).
	ServiceLabelOverrides map[string]string `json:"serviceLabelOverrides,omitempty"`

	// EnvironmentNameOverrides maps pack environment key → display name.
	EnvironmentNameOverrides map[string]string `json:"environmentNameOverrides,omitempty"`

	// ServiceRootOverrides maps pack service key → absolute or project-relative path.
	ServiceRootOverrides map[string]string `json:"serviceRootOverrides,omitempty"`

	// BindPathOverrides maps "serviceKey|containerPath" → host path.
	BindPathOverrides map[string]string `json:"bindPathOverrides,omitempty"`

	// HostPortOverrides maps pack service key → host port string (empty clears fixed port).
	HostPortOverrides map[string]string `json:"hostPortOverrides,omitempty"`

	// SecretValues maps secret env key → value for services (and project vars).
	// Prefer explicit fills for omitted secrets.
	SecretValues map[string]string `json:"secretValues,omitempty"`

	// SecretAppLinks maps secret env key → existing local app-secret key.
	// Sets the env value to {{secret.KEY}} instead of a plaintext fill.
	SecretAppLinks map[string]string `json:"secretAppLinks,omitempty"`

	// AppSecretValues maps app secret key → value to create on import.
	AppSecretValues map[string]string `json:"appSecretValues,omitempty"`

	// ImportAppSecrets writes pack.AppSecrets (when values present) into app_secrets.
	ImportAppSecrets bool `json:"importAppSecrets"`

	// LinkExistingAppSecrets skips writing app secrets that already exist locally
	// (same key) and records them as linked. Default true when not creating values.
	LinkExistingAppSecrets bool `json:"linkExistingAppSecrets"`

	// RequireIntegrity fails import when the pack has a contentHash that does not match.
	// Packs without a hash still import (legacy / hand-edited).
	RequireIntegrity bool `json:"requireIntegrity"`

	// StartAfter deploys every imported environment's services after materialize
	// (handled by the deploy engine wrapper; importer itself is store-only).
	StartAfter bool `json:"startAfter"`
}

// ImportPreview is a dry-run of pack application.
type ImportPreview struct {
	PackScope            string               `json:"packScope"`
	ProjectName          string               `json:"projectName"`
	SuggestedProjectName string               `json:"suggestedProjectName,omitempty"`
	Environments         []EnvironmentPayload `json:"environments"`
	Services             []ServiceSummary     `json:"services"`
	ProjectEnvVars       int                  `json:"projectEnvVarCount"`
	SandboxProfiles      int                  `json:"sandboxProfileCount"`
	AppSecrets           int                  `json:"appSecretCount"`
	NeedsProjectPath     bool                 `json:"needsProjectPath"`
	NeedsServiceRoots    []ServiceRootNeed    `json:"needsServiceRoots,omitempty"`
	NeedsBinds           []BindNeed           `json:"needsBinds,omitempty"`
	NeedsSecrets         []string             `json:"needsSecrets,omitempty"`
	NeedsAppSecrets      []string             `json:"needsAppSecrets,omitempty"`
	// ExistingAppSecrets lists local app-secret keys that match pack needs (same name).
	ExistingAppSecrets []string `json:"existingAppSecrets,omitempty"`
	// ContentHash is the pack's declared integrity hash (if any).
	ContentHash string `json:"contentHash,omitempty"`
	// ContentHashOK is true/false when a hash is present and was checked; nil when absent.
	ContentHashOK *bool `json:"contentHashOk,omitempty"`
	// MultiEnv is true when the pack has more than one environment.
	MultiEnv bool `json:"multiEnv"`
	// CanRecreateEnvs is true when into-project can recreate pack environments.
	CanRecreateEnvs bool `json:"canRecreateEnvs"`
	// HasLayout is true when at least one selected service has non-zero canvas coords.
	HasLayout bool `json:"hasLayout"`
	// Layout is a dry-run of canvas placement (existing nodes + proposed pack positions).
	Layout *LayoutPreview `json:"layout,omitempty"`
	// Collisions lists unique-field clashes with suggested renames.
	Collisions []Collision `json:"collisions,omitempty"`
	// HasBlockingCollision is true when import cannot auto-fix (e.g. path taken).
	HasBlockingCollision bool   `json:"hasBlockingCollision"`
	Report               Report `json:"report"`
}

// LayoutPreview describes where selected services will land relative to existing canvas nodes.
type LayoutPreview struct {
	// Mode is the layout mode used for this preview (auto|preserve|grid).
	Mode string `json:"mode"`
	// NodeWidth / NodeHeight are the footprint used for overlap checks (canvas units).
	NodeWidth  float64 `json:"nodeWidth"`
	NodeHeight float64 `json:"nodeHeight"`
	// Existing nodes already on the destination canvas (into-project flatten target, or empty for new project).
	Existing []LayoutNode `json:"existing,omitempty"`
	// Incoming proposed positions for selected pack services after layoutPlan.
	Incoming []LayoutNode `json:"incoming,omitempty"`
	// OverlapCount is how many incoming nodes overlap an existing node (or another incoming).
	OverlapCount int `json:"overlapCount"`
	// WouldOverlapWithoutShift is true when preserve/raw pack coords would stack on existing nodes.
	WouldOverlapWithoutShift bool `json:"wouldOverlapWithoutShift"`
	// Shifted is true when auto mode moved the pack group away from existing content.
	Shifted bool `json:"shifted"`
	// Bounds of the mini-map content (union of existing + incoming).
	MinX float64 `json:"minX"`
	MinY float64 `json:"minY"`
	MaxX float64 `json:"maxX"`
	MaxY float64 `json:"maxY"`
}

// LayoutNode is one rectangle on the layout mini-map.
type LayoutNode struct {
	// Key is pack service key for incoming, or store node id for existing.
	Key   string  `json:"key"`
	Label string  `json:"label"`
	X     float64 `json:"x"`
	Y     float64 `json:"y"`
	// PackX / PackY are original pack coordinates (incoming only).
	PackX float64 `json:"packX,omitempty"`
	PackY float64 `json:"packY,omitempty"`
	// Overlaps is true when this incoming node intersects an existing (or peer) node.
	Overlaps bool `json:"overlaps,omitempty"`
	// OverlapsLabels names what this node sits on (existing service labels).
	OverlapsLabels []string `json:"overlapsLabels,omitempty"`
	// Kind is "existing" | "incoming".
	Kind string `json:"kind"`
	// EnvironmentKey is the pack env key (incoming) or empty for existing.
	EnvironmentKey string `json:"environmentKey,omitempty"`
}

// ServiceSummary is a one-line service preview.
type ServiceSummary struct {
	Key              string  `json:"key"`
	Label            string  `json:"label"`
	EnvironmentKey   string  `json:"environmentKey"`
	Mode             string  `json:"mode"` // image | build
	Image            string  `json:"image,omitempty"`
	Port             string  `json:"port,omitempty"`
	NeedsServiceRoot bool    `json:"needsServiceRoot,omitempty"`
	BindRemapCount   int     `json:"bindRemapCount,omitempty"`
	// X / Y are pack coordinates (pre-layout); final placement is in Layout.Incoming.
	X float64 `json:"x,omitempty"`
	Y float64 `json:"y,omitempty"`
}

// ServiceRootNeed is a service that needs a service_root on import.
type ServiceRootNeed struct {
	ServiceKey string `json:"serviceKey"`
	Label      string `json:"label"`
	Hint       string `json:"hint,omitempty"`
}

// BindNeed is a bind mount that needs a host path.
type BindNeed struct {
	ServiceKey    string `json:"serviceKey"`
	Label         string `json:"label"`
	ContainerPath string `json:"containerPath"`
	OriginalHost  string `json:"originalHost,omitempty"`
}

// ImportResult is returned after a successful import.
type ImportResult struct {
	ProjectID      uint     `json:"projectId"`
	EnvironmentIDs []uint   `json:"environmentIds,omitempty"`
	NodeIDs        []string `json:"nodeIds,omitempty"`
	Report         Report   `json:"report"`
	// Started is true when StartAfter ran and every environment stack kickoff
	// succeeded (filled by the deploy engine, not the pack importer itself).
	Started bool `json:"started,omitempty"`
	// StartError is set when import succeeded but optional StartAfter failed
	// for one or more environments.
	StartError string `json:"startError,omitempty"`
}

// ExportResult is returned after building a pack (in-memory or written to path).
type ExportResult struct {
	Pack     *Pack  `json:"pack"`
	JSON     string `json:"json"`
	Path     string `json:"path,omitempty"`
	FileName string `json:"fileName,omitempty"`
	Report   Report `json:"report"`
}
