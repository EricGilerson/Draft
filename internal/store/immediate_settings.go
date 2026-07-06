package store

import "strings"

// ImmediateSettingKeys are applied directly to node_settings (not staged) because
// they control git hooks and deploy automation that must work before redeploy.
var ImmediateSettingKeys = map[string]bool{
	"git_branch":       true,
	"deploy_trigger":   true,
	"redeploy_on_pull": true,
	"git_stream":       true,
	"service_root":     true,
	"env_file":         true,
}

func IsImmediateSetting(key string) bool {
	return ImmediateSettingKeys[strings.TrimSpace(key)]
}

func (s *Store) StripImmediateStagedSettings(nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ErrInvalidNode
	}
	var keys []string
	for key := range ImmediateSettingKeys {
		keys = append(keys, key)
	}
	if len(keys) == 0 {
		return nil
	}
	return s.DB.Delete(&NodeSettingStaged{}, "node_id = ? AND key IN ?", nodeID, keys).Error
}
