package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
)

var ErrInvalidNode = errors.New("node id and label are required")

// generateUID returns a random 4-character hex string. Mirrors
// networking.GenerateUID(), duplicated here to avoid store importing
// networking (which itself depends on store, e.g. for route persistence).
// Now that this value is persisted permanently on the node rather than
// regenerated per deploy, uniqueUIDForProject retries on collision to
// compensate for the small (65536) space. A var so tests can force
// collisions deterministically.
var generateUID = func() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// nodeUIDExists reports whether any node in the project already has the
// given UID.
func (s *Store) nodeUIDExists(projectID uint, uid string) (bool, error) {
	var count int64
	err := s.DB.Model(&CanvasNode{}).Where("project_id = ? AND uid = ?", projectID, uid).Count(&count).Error
	return count > 0, err
}

// uniqueUIDForProject generates a UID guaranteed not to collide with an
// existing node's UID in the same project.
func (s *Store) uniqueUIDForProject(projectID uint) (string, error) {
	for {
		uid := generateUID()
		exists, err := s.nodeUIDExists(projectID, uid)
		if err != nil {
			return "", err
		}
		if !exists {
			return uid, nil
		}
	}
}

func (s *Store) CreateNode(node *CanvasNode) (*CanvasNode, error) {
	node.ID = strings.TrimSpace(node.ID)
	node.Label = strings.TrimSpace(node.Label)
	if node.ID == "" || node.Label == "" {
		return nil, ErrInvalidNode
	}
	if node.UID == "" {
		uid, err := s.uniqueUIDForProject(node.ProjectID)
		if err != nil {
			return nil, err
		}
		node.UID = uid
	}
	if err := s.DB.Create(node).Error; err != nil {
		return nil, err
	}
	return node, nil
}

// EnsureNodeUID returns the node's stable UID, generating and persisting one
// if it doesn't have one yet (e.g. rows created before UID existed). This
// keeps the node's Draft hostname stable across redeploys.
func (s *Store) EnsureNodeUID(id string) (string, error) {
	var node CanvasNode
	if err := s.DB.First(&node, "id = ?", id).Error; err != nil {
		return "", err
	}
	if node.UID != "" {
		return node.UID, nil
	}
	uid, err := s.uniqueUIDForProject(node.ProjectID)
	if err != nil {
		return "", err
	}
	if err := s.DB.Model(&CanvasNode{}).Where("id = ?", id).Update("uid", uid).Error; err != nil {
		return "", err
	}
	return uid, nil
}

func (s *Store) UpdateNode(id string, x, y float64, label string) error {
	return s.DB.Model(&CanvasNode{}).Where("id = ?", id).Updates(map[string]any{
		"x":     x,
		"y":     y,
		"label": label,
	}).Error
}

func (s *Store) DeleteNode(id string) error {
	return s.DB.Delete(&CanvasNode{}, "id = ?", id).Error
}

func (s *Store) ListNodes(projectID uint) ([]CanvasNode, error) {
	var nodes []CanvasNode
	if err := s.DB.Where("project_id = ?", projectID).Order("created_at asc").Find(&nodes).Error; err != nil {
		return nil, err
	}
	return nodes, nil
}

func (s *Store) GetNode(id string) (*CanvasNode, error) {
	var n CanvasNode
	if err := s.DB.First(&n, "id = ?", id).Error; err != nil {
		return nil, err
	}
	return &n, nil
}
