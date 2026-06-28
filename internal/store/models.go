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
