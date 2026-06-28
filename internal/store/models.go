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
