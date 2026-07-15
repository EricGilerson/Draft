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
	ImportAsNewProject   = "newProject"
	ImportIntoProject    = "intoProject"
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

	// ServiceLabelOverrides maps pack service key → final canvas label.
	// When omitted, colliding labels are auto-suffixed (api → api-2).
	ServiceLabelOverrides map[string]string `json:"serviceLabelOverrides,omitempty"`

	// EnvironmentNameOverrides maps pack environment key → display name (new project).
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

	// AppSecretValues maps app secret key → value to create on import.
	AppSecretValues map[string]string `json:"appSecretValues,omitempty"`

	// ImportAppSecrets writes pack.AppSecrets (when values present) into app_secrets.
	ImportAppSecrets bool `json:"importAppSecrets"`

	// StartAfter deploys every imported environment's services after materialize
	// (handled by the deploy engine wrapper; importer itself is store-only).
	StartAfter bool `json:"startAfter"`
}

// ImportPreview is a dry-run of pack application.
type ImportPreview struct {
	PackScope         string               `json:"packScope"`
	ProjectName       string               `json:"projectName"`
	SuggestedProjectName string            `json:"suggestedProjectName,omitempty"`
	Environments      []EnvironmentPayload `json:"environments"`
	Services          []ServiceSummary     `json:"services"`
	ProjectEnvVars    int                  `json:"projectEnvVarCount"`
	SandboxProfiles   int                  `json:"sandboxProfileCount"`
	AppSecrets        int                  `json:"appSecretCount"`
	NeedsProjectPath  bool                 `json:"needsProjectPath"`
	NeedsServiceRoots []ServiceRootNeed    `json:"needsServiceRoots,omitempty"`
	NeedsBinds        []BindNeed           `json:"needsBinds,omitempty"`
	NeedsSecrets      []string             `json:"needsSecrets,omitempty"`
	NeedsAppSecrets   []string             `json:"needsAppSecrets,omitempty"`
	// Collisions lists unique-field clashes with suggested renames.
	Collisions []Collision `json:"collisions,omitempty"`
	// HasBlockingCollision is true when import cannot auto-fix (e.g. path taken).
	HasBlockingCollision bool `json:"hasBlockingCollision"`
	Report               Report `json:"report"`
}

// ServiceSummary is a one-line service preview.
type ServiceSummary struct {
	Key            string `json:"key"`
	Label          string `json:"label"`
	EnvironmentKey string `json:"environmentKey"`
	Mode           string `json:"mode"` // image | build
	Image          string `json:"image,omitempty"`
	Port           string `json:"port,omitempty"`
	NeedsServiceRoot bool `json:"needsServiceRoot,omitempty"`
	BindRemapCount int    `json:"bindRemapCount,omitempty"`
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
