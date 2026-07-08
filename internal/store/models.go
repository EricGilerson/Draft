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
// (service.project.environment.uid.draft.local), Docker network names
// (draft-{project}-{environment}), and volume names, so renaming a project's
// display Name never disturbs already-running containers.
type Environment struct {
	ID        uint      `gorm:"primaryKey" json:"id"`
	ProjectID uint      `gorm:"index;not null;uniqueIndex:idx_env_project_slug" json:"projectId"`
	Name      string    `gorm:"not null" json:"name"`
	Slug      string    `gorm:"not null;uniqueIndex:idx_env_project_slug" json:"slug"`
	IsDefault bool      `gorm:"not null;default:false" json:"isDefault"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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
	// container name, scoped to the service rather than the app-wide deployment
	// primary key (ID), so two services with unrelated deploy histories don't
	// make each other's build numbers jump.
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
	Schema      string    `gorm:"type:text" json:"schema"`
	Builtin     bool      `gorm:"not null;default:false" json:"builtin"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
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
