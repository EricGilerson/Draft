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
func generateUID() string {
	b := make([]byte, 2)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

func (s *Store) CreateNode(node *CanvasNode) (*CanvasNode, error) {
	node.ID = strings.TrimSpace(node.ID)
	node.Label = strings.TrimSpace(node.Label)
	if node.ID == "" || node.Label == "" {
		return nil, ErrInvalidNode
	}
	if node.UID == "" {
		node.UID = generateUID()
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
	node.UID = generateUID()
	if err := s.DB.Model(&CanvasNode{}).Where("id = ?", id).Update("uid", node.UID).Error; err != nil {
		return "", err
	}
	return node.UID, nil
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
