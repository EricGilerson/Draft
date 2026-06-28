package store

import "time"

// Project is a local project Draft manages: a named folder whose services and
// ports are orchestrated on the canvas. This is the first real schema model;
// it will grow (manifest, environments, …) as features land.
type Project struct {
	ID          uint      `gorm:"primaryKey" json:"id"`
	Name        string    `gorm:"uniqueIndex;not null" json:"name"`
	Path        string    `gorm:"not null" json:"path"`
	Description string    `json:"description"`
	CreatedAt   time.Time `json:"createdAt"`
	UpdatedAt   time.Time `json:"updatedAt"`
}
