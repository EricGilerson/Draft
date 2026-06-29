package store

import (
	"errors"
	"strings"
)

var ErrInvalidNode = errors.New("node id and label are required")

func (s *Store) CreateNode(node *CanvasNode) (*CanvasNode, error) {
	node.ID = strings.TrimSpace(node.ID)
	node.Label = strings.TrimSpace(node.Label)
	if node.ID == "" || node.Label == "" {
		return nil, ErrInvalidNode
	}
	if err := s.DB.Create(node).Error; err != nil {
		return nil, err
	}
	return node, nil
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
