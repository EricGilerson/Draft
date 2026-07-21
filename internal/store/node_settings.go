package store

import "strings"

// GetNodeSetting returns the value for a single key, or "" if not set.
func (s *Store) GetNodeSetting(nodeID, key string) (string, error) {
	var ns NodeSetting
	err := s.DB.Where("node_id = ? AND key = ?", nodeID, key).First(&ns).Error
	if err != nil {
		return "", err
	}
	return ns.Value, nil
}

// SetNodeSetting upserts a single key-value pair for a node.
func (s *Store) SetNodeSetting(nodeID, key, value string) error {
	nodeID = strings.TrimSpace(nodeID)
	key = strings.TrimSpace(key)
	if nodeID == "" || key == "" {
		return ErrInvalidNode
	}
	ns := NodeSetting{NodeID: nodeID, Key: key, Value: value}
	return s.DB.Save(&ns).Error
}

// GetNodeSettings returns all settings for a node as a key→value map.
func (s *Store) GetNodeSettings(nodeID string) (map[string]string, error) {
	var settings []NodeSetting
	if err := s.DB.Where("node_id = ?", nodeID).Find(&settings).Error; err != nil {
		return nil, err
	}
	m := make(map[string]string, len(settings))
	for _, ns := range settings {
		m[ns.Key] = ns.Value
	}
	return m, nil
}

// GetNodeSettingsByNodes returns settings for many nodes in one query.
// Missing nodes are present as empty maps.
func (s *Store) GetNodeSettingsByNodes(nodeIDs []string) (map[string]map[string]string, error) {
	out := make(map[string]map[string]string, len(nodeIDs))
	for _, id := range nodeIDs {
		out[id] = map[string]string{}
	}
	if len(nodeIDs) == 0 {
		return out, nil
	}
	var settings []NodeSetting
	if err := s.DB.Where("node_id IN ?", nodeIDs).Find(&settings).Error; err != nil {
		return nil, err
	}
	for _, ns := range settings {
		m := out[ns.NodeID]
		if m == nil {
			m = map[string]string{}
			out[ns.NodeID] = m
		}
		m[ns.Key] = ns.Value
	}
	return out, nil
}

// DeleteNodeSettings removes all settings for a node.
func (s *Store) DeleteNodeSettings(nodeID string) error {
	return s.DB.Delete(&NodeSetting{}, "node_id = ?", nodeID).Error
}
