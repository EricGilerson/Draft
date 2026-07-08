package store

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"strings"
)

var ErrInvalidNode = errors.New("node id and label are required")
var ErrDuplicateNodeLabel = errors.New("a service with this name already exists in this project")
var ErrNodeNotFound = errors.New("no service with that name in this project")

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

// nodeUIDExists reports whether any node in the environment already has the
// given UID.
func (s *Store) nodeUIDExists(environmentID uint, uid string) (bool, error) {
	var count int64
	err := s.DB.Model(&CanvasNode{}).Where("environment_id = ? AND uid = ?", environmentID, uid).Count(&count).Error
	return count > 0, err
}

// sanitizeLabel normalizes a node label the same way engine.sanitize() does
// before it becomes a Docker service name, hostname, and network alias
// (lowercase, non [a-z0-9-] chars replaced with '-', trimmed). Two labels
// that normalize to the same value would collide on the Docker network, so
// uniqueness is checked against this normalized form rather than the raw
// string — otherwise "API" and "api" (or "my_svc" and "my-svc") could both
// be created and only collide later, at deploy time.
func sanitizeLabel(name string) string {
	return sanitizeSlugChars(name)
}

// sanitizeSlugChars lowercases name and replaces every character outside
// [a-z0-9-] with '-', trimming leading/trailing '-'. Shared by node label
// normalization and environment slug generation, since both feed the same
// Docker-name-safe identifier space (hostnames, network names, aliases).
func sanitizeSlugChars(name string) string {
	s := strings.ToLower(strings.TrimSpace(name))
	return strings.Trim(strings.Map(func(r rune) rune {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '-' {
			return r
		}
		return '-'
	}, s), "-")
}

// nodeLabelTaken reports whether another node in the environment already has
// a label that normalizes to the same value, excluding excludeID (used so
// updating a node's position, or re-saving its own unchanged label, never
// conflicts with itself).
func (s *Store) nodeLabelTaken(environmentID uint, label, excludeID string) (bool, error) {
	target := sanitizeLabel(label)
	if target == "" {
		return false, nil
	}
	var siblings []CanvasNode
	if err := s.DB.Where("environment_id = ? AND id <> ?", environmentID, excludeID).Find(&siblings).Error; err != nil {
		return false, err
	}
	for _, n := range siblings {
		if sanitizeLabel(n.Label) == target {
			return true, nil
		}
	}
	return false, nil
}

// uniqueUIDForEnvironment generates a UID guaranteed not to collide with an
// existing node's UID in the same environment.
func (s *Store) uniqueUIDForEnvironment(environmentID uint) (string, error) {
	for {
		uid := generateUID()
		exists, err := s.nodeUIDExists(environmentID, uid)
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
	if node.ID == "" || node.Label == "" || node.EnvironmentID == 0 {
		return nil, ErrInvalidNode
	}
	if taken, err := s.nodeLabelTaken(node.EnvironmentID, node.Label, node.ID); err != nil {
		return nil, err
	} else if taken {
		return nil, ErrDuplicateNodeLabel
	}
	if node.UID == "" {
		uid, err := s.uniqueUIDForEnvironment(node.EnvironmentID)
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
	uid, err := s.uniqueUIDForEnvironment(node.EnvironmentID)
	if err != nil {
		return "", err
	}
	if err := s.DB.Model(&CanvasNode{}).Where("id = ?", id).Update("uid", uid).Error; err != nil {
		return "", err
	}
	return uid, nil
}

func (s *Store) UpdateNode(id string, x, y float64, label string) error {
	label = strings.TrimSpace(label)
	if label == "" {
		return ErrInvalidNode
	}
	node, err := s.GetNode(id)
	if err != nil {
		return err
	}
	if taken, err := s.nodeLabelTaken(node.EnvironmentID, label, id); err != nil {
		return err
	} else if taken {
		return ErrDuplicateNodeLabel
	}
	return s.DB.Model(&CanvasNode{}).Where("id = ?", id).Updates(map[string]any{
		"x":     x,
		"y":     y,
		"label": label,
	}).Error
}

func (s *Store) DeleteNode(id string) error {
	return s.DB.Delete(&CanvasNode{}, "id = ?", id).Error
}

// ListNodes returns every node in a project across all of its environments.
// Used by project-wide concerns (git hook reconcile, secrets usage scan,
// project delete cascade) that intentionally aren't environment-scoped. For
// the nodes in a single environment, use ListNodesByEnvironment.
func (s *Store) ListNodes(projectID uint) ([]CanvasNode, error) {
	var nodes []CanvasNode
	if err := s.DB.Where("project_id = ?", projectID).Order("created_at asc").Find(&nodes).Error; err != nil {
		return nil, err
	}
	return nodes, nil
}

// ListNodesByEnvironment returns every node belonging to a single environment.
func (s *Store) ListNodesByEnvironment(environmentID uint) ([]CanvasNode, error) {
	var nodes []CanvasNode
	if err := s.DB.Where("environment_id = ?", environmentID).Order("created_at asc").Find(&nodes).Error; err != nil {
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

// GetNodeByLabel finds a node in an environment by its label, matching on the
// same normalized form used to enforce label uniqueness (case/format-
// insensitive).
func (s *Store) GetNodeByLabel(environmentID uint, label string) (*CanvasNode, error) {
	target := sanitizeLabel(label)
	var nodes []CanvasNode
	if err := s.DB.Where("environment_id = ?", environmentID).Find(&nodes).Error; err != nil {
		return nil, err
	}
	for i := range nodes {
		if sanitizeLabel(nodes[i].Label) == target {
			return &nodes[i], nil
		}
	}
	return nil, ErrNodeNotFound
}
