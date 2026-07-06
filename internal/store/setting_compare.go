package store

import "strings"

// settingValuesEqual reports whether two stored setting values are equivalent
// for staging purposes. Boolean-like settings treat "", "false", and absent as off.
func settingValuesEqual(a, b string) bool {
	return normalizeSettingValue(a) == normalizeSettingValue(b)
}

func normalizeSettingValue(value string) string {
	v := strings.TrimSpace(value)
	switch strings.ToLower(v) {
	case "", "false", "0", "no", "off":
		return ""
	case "true", "1", "yes", "on":
		return "true"
	default:
		return v
	}
}

func meaningfulStagedSettings(applied, staged map[string]string) map[string]string {
	if len(staged) == 0 {
		return map[string]string{}
	}
	if applied == nil {
		applied = map[string]string{}
	}
	out := make(map[string]string, len(staged))
	for key, value := range staged {
		if IsImmediateSetting(key) {
			continue
		}
		appliedValue, ok := applied[key]
		if !ok {
			appliedValue = ""
		}
		if !settingValuesEqual(appliedValue, value) {
			out[key] = value
		}
	}
	return out
}

func MeaningfulStagedSettings(applied, staged map[string]string) map[string]string {
	return meaningfulStagedSettings(applied, staged)
}

func (s *Store) PruneNoOpStagedSettings(nodeID string) error {
	applied, err := s.GetNodeSettings(nodeID)
	if err != nil {
		return err
	}
	if applied == nil {
		applied = map[string]string{}
	}
	staged, err := s.GetStagedNodeSettings(nodeID)
	if err != nil {
		return err
	}
	for key, value := range staged {
		appliedValue, ok := applied[key]
		if !ok {
			appliedValue = ""
		}
		if settingValuesEqual(appliedValue, value) {
			if err := s.DB.Delete(&NodeSettingStaged{}, "node_id = ? AND key = ?", nodeID, key).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func envVarStageIsNoOp(applied []EnvVar, staged EnvVarStaged) bool {
	byKey := map[string]EnvVar{}
	for _, v := range applied {
		byKey[v.Key] = v
	}
	existing, has := byKey[staged.Key]
	if staged.Delete {
		return !has
	}
	if !has {
		return false
	}
	scope := staged.Scope
	if scope == "" {
		scope = EnvScopeRuntime
	}
	return existing.Value == staged.Value && existing.Scope == scope
}

func (s *Store) PruneNoOpStagedEnvVars(nodeID string) error {
	applied, err := s.ListEnvVars(nodeID)
	if err != nil {
		return err
	}
	staged, err := s.ListStagedEnvVarChanges(nodeID)
	if err != nil {
		return err
	}
	for _, row := range staged {
		if envVarStageIsNoOp(applied, row) {
			if err := s.DB.Delete(&EnvVarStaged{}, "node_id = ? AND key = ?", nodeID, row.Key).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

func MeaningfulStagedEnvChanges(applied []EnvVar, staged []EnvVarStaged) []EnvVarStaged {
	if len(staged) == 0 {
		return nil
	}
	out := make([]EnvVarStaged, 0, len(staged))
	for _, row := range staged {
		if !envVarStageIsNoOp(applied, row) {
			out = append(out, row)
		}
	}
	return out
}

func (s *Store) hasMeaningfulStagedChanges(nodeID string) (bool, error) {
	applied, err := s.GetNodeSettings(nodeID)
	if err != nil {
		return false, err
	}
	staged, err := s.GetStagedNodeSettings(nodeID)
	if err != nil {
		return false, err
	}
	if len(meaningfulStagedSettings(applied, staged)) > 0 {
		return true, nil
	}
	envApplied, err := s.ListEnvVars(nodeID)
	if err != nil {
		return false, err
	}
	envStaged, err := s.ListStagedEnvVarChanges(nodeID)
	if err != nil {
		return false, err
	}
	for _, row := range envStaged {
		if !envVarStageIsNoOp(envApplied, row) {
			return true, nil
		}
	}
	return false, nil
}
