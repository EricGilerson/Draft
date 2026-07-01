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

// CanvasNode is a single node on a project's visual canvas.
type CanvasNode struct {
	ID        string    `gorm:"primaryKey" json:"id"`
	ProjectID uint      `gorm:"index;not null" json:"projectId"`
	Label     string    `gorm:"not null" json:"label"`
	X         float64   `json:"x"`
	Y         float64   `json:"y"`
	// UID is a short, stable identifier used to build the node's Draft hostname
	// (service.project.environment.uid.draft.local). It's generated once and
	// never changes, so the hostname stays valid across redeploys and other
	// services on the same Docker network can depend on it.
	UID       string    `gorm:"not null;default:''" json:"uid"`
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
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

// Deployment tracks a single build+run cycle for a service node.
type Deployment struct {
	ID                 uint       `gorm:"primaryKey" json:"id"`
	NodeID             string     `gorm:"index;not null" json:"nodeId"`
	ProjectID          uint       `gorm:"index;not null" json:"projectId"`
	ImageTag           string     `json:"imageTag"`
	ContainerID        string     `json:"containerId"`
	Status             string     `gorm:"not null;default:'pending'" json:"status"` // pending|building|built|starting|running|stopped|failed
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
