package store

import "time"

// Project is a local project Draft manages: a named folder whose services and
// ports are orchestrated on the canvas.
type Project struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;not null" json:"name"`
	Path        string    `gorm:"not null" json:"path"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// Environment is a named, independently deployable copy of a project's
// services (e.g. "Main", "Staging"). Every project has exactly one default
// environment, created alongside the project. Slug is generated once and
// never changes — it's the segment baked into hostnames
// (service.project.environment.uid.draft.local, or
// service.project.sand.environment.uid.draft.local for sandboxes), Docker
// network names, and volume names, so renaming a project's display Name never
// disturbs already-running containers.
type Environment struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ProjectID uint      `gorm:"index;not null;uniqueIndex:idx_env_project_slug" json:"projectId"`
	Name      string    `gorm:"not null" json:"name"`
	Slug      string    `gorm:"not null;uniqueIndex:idx_env_project_slug" json:"slug"`
	IsDefault bool      `gorm:"not null;default:false" json:"isDefault"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// SandboxProjectSettings contains the project-wide lifecycle defaults used
// when a sandbox profile does not specify an explicit value. Values are in
// hours so they can be surfaced without a duration-format conversion layer.
type SandboxProjectSettings struct {
	ProjectID        uint      `gorm:"primaryKey" json:"projectId"`
	DefaultTTLHours  int       `gorm:"not null;default:168" json:"defaultTtlHours"`
	WarningHours     int       `gorm:"not null;default:24" json:"warningHours"`
	GraceHours       int       `gorm:"not null;default:72" json:"graceHours"`
	SuspendIdleHours int       `gorm:"not null;default:0" json:"suspendIdleHours"`
	CreatedAt        time.Time `json:"createdAt"`
	UpdatedAt        time.Time `json:"updatedAt"`
}

// SandboxProfile is a reusable, JSON-backed creation plan. SourceEnvironmentID
// is zero for a project-wide profile, or scopes the profile to a particular
// source environment (for example stricter production-like defaults).
type SandboxProfile struct {
	ID                  uint      `gorm:"primaryKey" json:"id"`
	ProjectID           uint      `gorm:"index;not null" json:"projectId"`
	SourceEnvironmentID uint      `gorm:"index;not null;default:0" json:"sourceEnvironmentId"`
	Name                string    `gorm:"not null" json:"name"`
	Description         string    `json:"description"`
	PlanJSON            string    `gorm:"not null;default:'{}'" json:"planJson"`
	IsDefault           bool      `gorm:"not null;default:false" json:"isDefault"`
	CreatedAt           time.Time `json:"createdAt"`
	UpdatedAt           time.Time `json:"updatedAt"`
}

// Sandbox is the durable lifecycle record for one disposable environment.
// PlanJSON is an immutable resolved plan captured at creation time; changing a
// profile later never rewires a running sandbox.
//
// Purpose is denormalized from the plan ("preview" | "test") so list UIs can
// filter without parsing PlanJSON. The plan remains the source of truth for
// steps, service rules, and lifecycle knobs.
type Sandbox struct {
	ID                  uint       `gorm:"primaryKey" json:"id"`
	ProjectID           uint       `gorm:"index;not null" json:"projectId"`
	EnvironmentID       uint       `gorm:"uniqueIndex;not null" json:"environmentId"`
	SourceEnvironmentID uint       `gorm:"index;not null" json:"sourceEnvironmentId"`
	ProfileID           uint       `gorm:"index" json:"profileId"`
	Name                string     `gorm:"not null" json:"name"`
	Purpose             string     `gorm:"index;not null;default:'preview'" json:"purpose"`
	Status              string     `gorm:"not null;default:'active'" json:"status"`
	PlanJSON            string     `gorm:"not null" json:"planJson"`
	ExpiresAt           time.Time  `gorm:"index;not null" json:"expiresAt"`
	WarnAt              time.Time  `gorm:"index;not null" json:"warnAt"`
	GraceEndsAt         time.Time  `gorm:"index;not null" json:"graceEndsAt"`
	SuspendedAt         *time.Time `json:"suspendedAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

// SandboxTestRun records one execution of a testing sandbox's steps. The
// durable recipe lives on SandboxProfile (or the create request plan); each
// run is a short-lived instance outcome that can be inspected after the
// sandbox itself has been cleaned up.
type SandboxTestRun struct {
	ID                  uint       `gorm:"primaryKey" json:"id"`
	ProjectID           uint       `gorm:"index;not null" json:"projectId"`
	ProfileID           uint       `gorm:"index" json:"profileId"`
	SandboxID           uint       `gorm:"index" json:"sandboxId"`
	SourceEnvironmentID uint       `gorm:"index;not null" json:"sourceEnvironmentId"`
	Name                string     `gorm:"not null" json:"name"`
	Mode                string     `gorm:"not null;default:'fresh'" json:"mode"` // fresh | steps
	Status              string     `gorm:"index;not null;default:'running'" json:"status"` // running | passed | failed
	PlanJSON            string     `gorm:"not null;default:'{}'" json:"planJson"`
	StepsJSON           string     `gorm:"not null;default:'[]'" json:"stepsJson"`
	Error               string     `json:"error,omitempty"`
	StartedAt           time.Time  `gorm:"index;not null" json:"startedAt"`
	FinishedAt          *time.Time `json:"finishedAt,omitempty"`
	CreatedAt           time.Time  `json:"createdAt"`
	UpdatedAt           time.Time  `json:"updatedAt"`
}

// SandboxLink associates a sandbox with any number of external work items.
// Kind is intentionally open-ended (pr, ticket, incident, tag, URL) so Draft
// does not need provider-specific tables to preserve useful context.
type SandboxLink struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	SandboxID uint      `gorm:"uniqueIndex:idx_sandbox_link;not null" json:"sandboxId"`
	Kind      string    `gorm:"uniqueIndex:idx_sandbox_link;not null" json:"kind"`
	Value     string    `gorm:"uniqueIndex:idx_sandbox_link;not null" json:"value"`
	Label     string    `json:"label"`
	CreatedAt time.Time `json:"createdAt"`
}

// SandboxRepositorySource records the resolved, per-repository source used by
// a sandbox. This deliberately avoids a project-wide branch assumption.
type SandboxRepositorySource struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	SandboxID uint      `gorm:"uniqueIndex:idx_sandbox_repo;not null" json:"sandboxId"`
	RepoRoot  string    `gorm:"uniqueIndex:idx_sandbox_repo;not null" json:"repoRoot"`
	Ref       string    `json:"ref"`
	CommitSHA string    `json:"commitSha"`
	CreatedAt time.Time `json:"createdAt"`
}

// CanvasNode is a single node on a project's visual canvas.
type CanvasNode struct {
	ID            string  `gorm:"primaryKey" json:"id"`
	ProjectID     uint    `gorm:"index;not null" json:"projectId"`
	EnvironmentID uint    `gorm:"index;not null" json:"environmentId"`
	Label         string  `gorm:"not null" json:"label"`
	X             float64 `json:"x"`
	Y             float64 `json:"y"`
	// UID is a short, stable identifier used to build the node's Draft hostname
	// (service.project.environment.uid.draft.local). It's generated once and
	// never changes, so the hostname stays valid across redeploys and other
	// services on the same Docker network can depend on it.
	UID string `gorm:"not null;default:''" json:"uid"`
	// TemplateID is the ServiceTemplate this node was created from, or 0 for a
	// blank/legacy node. Stored so the canvas can render the template icon and
	// a future "re-apply template" action can find the source. 0 is treated as
	// "no template" everywhere; it is not a foreign key so deleting a template
	// never orphans nodes.
	TemplateID uint      `gorm:"index;default:0" json:"templateId"`
	CreatedAt  time.Time `json:"createdAt"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

// Route maps a Draft-managed hostname to a container target. HTTP services
// are proxied by the built-in reverse proxy; TCP services use the hostname
// as a friendly alias that resolves to 127.0.0.1 via the hosts file.
type Route struct {
	Hostname    string    `gorm:"primaryKey" json:"hostname"`
	ProjectID   uint      `gorm:"index;not null" json:"projectId"`
	NodeID      string    `gorm:"index;not null" json:"nodeId"`
	Environment string    `gorm:"not null;default:'default'" json:"environment"`
	Protocol    string    `gorm:"not null;default:'http'" json:"protocol"` // "http" or "tcp"
	TargetHost  string    `gorm:"not null" json:"targetHost"`              // container host (e.g. "localhost" or docker network name)
	TargetPort  int       `gorm:"not null" json:"targetPort"`              // container port
	HostPort    int       `json:"hostPort"`                                // mapped host port (TCP services only; 0 for proxied HTTP)
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// PortLease is a cross-project record of which host port is assigned to which
// service. Draft is the single authority on host port allocation.
type PortLease struct {
	Port      int       `gorm:"primaryKey" json:"port"`
	ProjectID uint      `gorm:"index;not null" json:"projectId"`
	NodeID    string    `gorm:"index;not null" json:"nodeId"`
	CreatedAt time.Time `json:"createdAt"`
}

// NodeSetting is a key-value pair scoped to a canvas node. Using a KV model
// lets settings grow over time without schema migrations — new features just
// introduce new keys.
type NodeSetting struct {
	NodeID string `gorm:"primaryKey;not null" json:"nodeId"`
	Key    string `gorm:"primaryKey;not null" json:"key"`
	Value  string `gorm:"not null" json:"value"`
}

// NodeSettingStaged holds pending node_settings overrides until the next
// successful deploy promotes them into NodeSetting.
type NodeSettingStaged struct {
	NodeID string `gorm:"primaryKey;not null" json:"nodeId"`
	Key    string `gorm:"primaryKey;not null" json:"key"`
	Value  string `gorm:"not null" json:"value"`
}

// EnvVarStaged holds pending env var upserts or deletions until deploy.
type EnvVarStaged struct {
	NodeID string `gorm:"primaryKey;not null" json:"nodeId"`
	Key    string `gorm:"primaryKey;not null" json:"key"`
	Value  string `gorm:"not null" json:"value"`
	Scope  string `gorm:"not null;default:'runtime'" json:"scope"`
	Delete bool   `gorm:"not null;default:false" json:"delete"`
}

// Deployment tracks a single build+run cycle for a service node.
type Deployment struct {
	ID          uint   `gorm:"primaryKey" json:"id"`
	NodeID      string `gorm:"index;not null" json:"nodeId"`
	ProjectID   uint   `gorm:"index;not null" json:"projectId"`
	ImageTag    string `json:"imageTag"`
	ContainerID string `json:"containerId"`
	// SourceSHA is the git commit this deployment was built from, when the node
	// is pinned to a git branch/ref. Empty for working-tree deploys. It is the
	// baseline the git-trigger reconciler diffs against to decide whether a
	// commit/push actually changed the tracked branch since the last deploy.
	SourceSHA          string     `json:"sourceSha"`
	Status             string     `gorm:"not null;default:'pending'" json:"status"` // pending|building|built|starting|running|stopped|failed
	// Sequence is the 1-based, per-node deploy counter (the 1st, 2nd, 3rd...
	// deploy of THIS service). It's the number shown in the image tag and
	// container name (draft-{project}-{environment}-{service}:{sequence}),
	// scoped to the service rather than the app-wide deployment primary key
	// (ID), so two services with unrelated deploy histories don't make each
	// other's build numbers jump. Environment keeps duplicated environments
	// with the same service label from sharing a tag at :1.
	Sequence           int        `gorm:"not null;default:0" json:"sequence"`
	Hostname           string     `json:"hostname"`
	HostPort           int        `json:"hostPort"`
	Error              string     `json:"error"`
	JobID              string     `gorm:"index" json:"jobId"`
	WorkerPID          int        `json:"workerPid"`
	StartedAt          *time.Time `json:"startedAt"`
	BuildStartedAt     *time.Time `json:"buildStartedAt"`
	BuildFinishedAt    *time.Time `json:"buildFinishedAt"`
	ContainerStartedAt *time.Time `json:"containerStartedAt"`
	ContainerStoppedAt *time.Time `json:"containerStoppedAt"`
	ExitCode           *int       `json:"exitCode"`
	OOMKilled          bool       `json:"oomKilled"`
	LastSeenAt         *time.Time `json:"lastSeenAt"`
	CreatedAt          time.Time  `json:"createdAt"`
	UpdatedAt          time.Time  `json:"updatedAt"`
	FinishedAt         *time.Time `json:"finishedAt"`
}

// EnvVar represents a single environment variable key/value pair.
type EnvVar struct {
	NodeID    string    `gorm:"primaryKey;not null" json:"nodeId"`
	Key       string    `gorm:"primaryKey;not null" json:"key"`
	Value     string    `gorm:"not null" json:"value"`
	Scope     string    `gorm:"not null;default:'runtime'" json:"scope"` // runtime|build|both
	Secret    bool      `json:"secret"`
	Source    string    `gorm:"not null;default:'manual'" json:"source"` // manual|imported|generated
	EnvFile   string    `json:"envFile"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ProjectEnvVar is a project-scoped shared value referenced explicitly from
// service env vars via {{project.KEY}} tokens.
type ProjectEnvVar struct {
	ProjectID uint      `gorm:"primaryKey;not null;index" json:"projectId"`
	Key       string    `gorm:"primaryKey;not null" json:"key"`
	Value     string    `gorm:"not null" json:"value"`
	Scope     string    `gorm:"not null;default:'runtime'" json:"scope"` // runtime|build|both
	Secret    bool      `json:"secret"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// AppSecret is an app-wide credential stored centrally and referenced from
// service/project env vars via {{secret.KEY}} tokens.
type AppSecret struct {
	Key         string    `gorm:"primaryKey;not null" json:"key"`
	Value       string    `gorm:"not null" json:"value"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}

// AppSetting is an app-wide key/value preference (not project-scoped).
// Known keys live in app_settings.go; unknown keys are allowed for forward
// compatibility but the Settings UI only edits the documented set.
type AppSetting struct {
	Key       string    `gorm:"primaryKey;not null" json:"key"`
	Value     string    `gorm:"not null" json:"value"`
	UpdatedAt time.Time `json:"updatedAt"`
}

// ServiceTemplate is a reusable blueprint for creating a service node. Built-in
// templates are seeded by the store and are locked (not editable/deletable);
// users clone them or create their own. The Dockerfile is embedded so a later
// "create service" flow can stamp a node from a template in one step — Draft
// writes the Dockerfile body into the service root at creation time.
type ServiceTemplate struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"not null" json:"name"`
	Description string    `json:"description"`
	Category    string    `json:"category"`                             // "web" | "datastore" | "language"
	Icon        string    `json:"icon"`                                 // simple-icons slug, e.g. "nextdotjs"
	Color       string    `json:"color"`                                // optional brand hex; empty = currentColor
	Mode        string    `gorm:"not null;default:'build'" json:"mode"` // "build" | "image"
	Image       string    `json:"image"`                                // image name when Mode=="image" (datastores)
	ImageTags   string    `gorm:"type:text" json:"imageTags"`           // JSON: ["16-alpine","16",...] curated tags for image-mode templates; base name comes from Image
	Port        int       `json:"port"`
	Dockerfile  string    `gorm:"type:text" json:"dockerfile"` // embedded Dockerfile content
	CmdOverride string    `json:"cmdOverride"`
	Entrypoint  string    `json:"entrypoint"`
	WorkingDir  string    `json:"workingDir"`
	EnvVars     string    `gorm:"type:text" json:"envVars"` // JSON: [{"key","value","scope"}]
	Labels      string    `gorm:"type:text" json:"labels"`  // JSON: {"key":"value"}
	// Volumes is a JSON array of TemplateVolume (see volumes.go): default volume
	// mounts seeded onto services created from this template. Datastore built-ins
	// carry a Draft-managed named volume for their data directory so a freshly
	// created Postgres/MySQL/Mongo/Redis persists across redeploys. Empty for
	// build-mode web/language templates. The create wizard lets the user edit
	// these defaults before stamping.
	Volumes     string    `gorm:"type:text" json:"volumes"` // JSON: [{type, source, target, readOnly, sizeHint, labels}]
	// Schema is a JSON-encoded TemplateSchema that drives the create-service
	// wizard (which steps/fields apply) and the Settings tab (which sections to
	// hide). Empty means "use the default for Mode"; see template_schema.go.
	Schema string `gorm:"type:text" json:"schema"`
	// DefaultSettings is a JSON object of node_settings stamped onto new nodes
	// (e.g. {"route_protocol":"tcp","host_port":"5432"} for pure wire datastores).
	// Wizard overrides and later Settings edits win over these defaults.
	DefaultSettings string    `gorm:"type:text" json:"defaultSettings"`
	Builtin         bool      `gorm:"not null;default:false" json:"builtin"`
	CreatedAt       time.Time `json:"createdAt"`
	UpdatedAt       time.Time `json:"updatedAt"`
}

type EnvVarConflict struct {
	Key           string `json:"key"`
	DatabaseValue string `json:"databaseValue"`
	FileValue     string `json:"fileValue"`
}

type EnvFileSyncResult struct {
	Path      string           `json:"path"`
	Imported  int              `json:"imported"`
	Updated   int              `json:"updated"`
	Unchanged int              `json:"unchanged"`
	Skipped   int              `json:"skipped"`
	Exported  int              `json:"exported"`
	Conflicts []EnvVarConflict `json:"conflicts"`
}
