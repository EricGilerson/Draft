package store

import (
	"fmt"
	"sort"
	"strings"

	"gorm.io/gorm"
)

// MergeNodeSettings overlays staged keys onto applied settings.
func MergeNodeSettings(applied, staged map[string]string) map[string]string {
	out := make(map[string]string, len(applied)+len(staged))
	for k, v := range applied {
		out[k] = v
	}
	for k, v := range staged {
		out[k] = v
	}
	return out
}

func (s *Store) GetStagedNodeSettings(nodeID string) (map[string]string, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, ErrInvalidNode
	}
	var rows []NodeSettingStaged
	if err := s.DB.Where("node_id = ?", nodeID).Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, row := range rows {
		m[row.Key] = row.Value
	}
	return m, nil
}

func (s *Store) StageNodeSettings(nodeID string, settings map[string]string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ErrInvalidNode
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		for key, value := range settings {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			row := NodeSettingStaged{NodeID: nodeID, Key: key, Value: value}
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) DiscardStagedNodeSettings(nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ErrInvalidNode
	}
	return s.DB.Delete(&NodeSettingStaged{}, "node_id = ?", nodeID).Error
}

func (s *Store) ListStagedEnvVarChanges(nodeID string) ([]EnvVarStaged, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return nil, ErrInvalidNode
	}
	var rows []EnvVarStaged
	if err := s.DB.Where("node_id = ?", nodeID).Order("key asc").Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}

type EnvVarStageUpsert struct {
	Key    string
	Value  string
	Scope  string
	Secret bool
	Source string
	EnvFile string
}

func (s *Store) StageEnvVarChanges(nodeID string, upserts []EnvVarStageUpsert, deleteKeys []string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ErrInvalidNode
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		for _, u := range upserts {
			key := strings.TrimSpace(u.Key)
			if key == "" {
				continue
			}
			if err := validateEnvKey(key); err != nil {
				return err
			}
			scope := strings.TrimSpace(u.Scope)
			if scope == "" {
				scope = EnvScopeRuntime
			}
			switch scope {
			case EnvScopeRuntime, EnvScopeBuild, EnvScopeBoth:
			default:
				return fmt.Errorf("invalid env scope %q", scope)
			}
			row := EnvVarStaged{
				NodeID: nodeID,
				Key:    key,
				Value:  u.Value,
				Scope:  scope,
				Delete: false,
			}
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		for _, key := range deleteKeys {
			key = strings.TrimSpace(key)
			if key == "" {
				continue
			}
			if err := validateEnvKey(key); err != nil {
				return err
			}
			row := EnvVarStaged{
				NodeID: nodeID,
				Key:    key,
				Delete: true,
			}
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

func (s *Store) DiscardStagedEnvVars(nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ErrInvalidNode
	}
	return s.DB.Delete(&EnvVarStaged{}, "node_id = ?", nodeID).Error
}

func (s *Store) DiscardAllStagedChanges(nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ErrInvalidNode
	}
	return s.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Delete(&NodeSettingStaged{}, "node_id = ?", nodeID).Error; err != nil {
			return err
		}
		return tx.Delete(&EnvVarStaged{}, "node_id = ?", nodeID).Error
	})
}

func (s *Store) HasStagedChanges(nodeID string) (bool, error) {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return false, ErrInvalidNode
	}
	var settingCount int64
	if err := s.DB.Model(&NodeSettingStaged{}).Where("node_id = ?", nodeID).Count(&settingCount).Error; err != nil {
		return false, err
	}
	if settingCount > 0 {
		return true, nil
	}
	var envCount int64
	if err := s.DB.Model(&EnvVarStaged{}).Where("node_id = ?", nodeID).Count(&envCount).Error; err != nil {
		return false, err
	}
	return envCount > 0, nil
}

func (s *Store) EffectiveNodeSettings(nodeID string) (map[string]string, error) {
	applied, err := s.GetNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}
	staged, err := s.GetStagedNodeSettings(nodeID)
	if err != nil {
		return nil, err
	}
	if len(applied) == 0 && len(staged) == 0 {
		return map[string]string{}, nil
	}
	if applied == nil {
		applied = map[string]string{}
	}
	effective := MergeNodeSettings(applied, staged)
	for key := range ImmediateSettingKeys {
		if v, ok := applied[key]; ok {
			effective[key] = v
		} else {
			delete(effective, key)
		}
	}
	return effective, nil
}

func (s *Store) EffectiveEnvVars(nodeID string) ([]EnvVar, error) {
	applied, err := s.ListEnvVars(nodeID)
	if err != nil {
		return nil, err
	}
	staged, err := s.ListStagedEnvVarChanges(nodeID)
	if err != nil {
		return nil, err
	}
	if len(staged) == 0 {
		return applied, nil
	}

	byKey := make(map[string]EnvVar, len(applied))
	for _, v := range applied {
		byKey[v.Key] = v
	}
	for _, ch := range staged {
		if ch.Delete {
			delete(byKey, ch.Key)
			continue
		}
		existing := byKey[ch.Key]
		existing.NodeID = nodeID
		existing.Key = ch.Key
		existing.Value = ch.Value
		if ch.Scope != "" {
			existing.Scope = ch.Scope
		}
		if existing.Scope == "" {
			existing.Scope = EnvScopeRuntime
		}
		if existing.Source == "" {
			existing.Source = EnvSourceManual
		}
		byKey[ch.Key] = existing
	}

	out := make([]EnvVar, 0, len(byKey))
	for _, v := range byKey {
		out = append(out, v)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out, nil
}

// PromoteStagedToApplied merges staged overrides into applied settings/env vars
// and clears staging rows. No-op when nothing is staged.
func (s *Store) PromoteStagedToApplied(nodeID string) error {
	nodeID = strings.TrimSpace(nodeID)
	if nodeID == "" {
		return ErrInvalidNode
	}
	has, err := s.HasStagedChanges(nodeID)
	if err != nil {
		return err
	}
	if !has {
		return nil
	}

	return s.DB.Transaction(func(tx *gorm.DB) error {
		stagedSettings, err := s.getStagedNodeSettingsTx(tx, nodeID)
		if err != nil {
			return err
		}
		for key, value := range stagedSettings {
			row := NodeSetting{NodeID: nodeID, Key: key, Value: value}
			if err := tx.Save(&row).Error; err != nil {
				return err
			}
		}

		stagedEnv, err := s.listStagedEnvVarChangesTx(tx, nodeID)
		if err != nil {
			return err
		}
		for _, ch := range stagedEnv {
			if ch.Delete {
				if err := tx.Delete(&EnvVar{}, "node_id = ? AND key = ?", nodeID, ch.Key).Error; err != nil {
					return err
				}
				continue
			}
			var existing EnvVar
			findErr := tx.Where("node_id = ? AND key = ?", nodeID, ch.Key).First(&existing).Error
			if findErr != nil && findErr != gorm.ErrRecordNotFound {
				return findErr
			}
			scope := ch.Scope
			if scope == "" {
				scope = EnvScopeRuntime
			}
			if findErr == gorm.ErrRecordNotFound {
				row := EnvVar{
					NodeID: nodeID,
					Key:    ch.Key,
					Value:  ch.Value,
					Scope:  scope,
					Source: EnvSourceManual,
				}
				if err := tx.Create(&row).Error; err != nil {
					return err
				}
				continue
			}
			existing.Value = ch.Value
			existing.Scope = scope
			if err := tx.Save(&existing).Error; err != nil {
				return err
			}
		}

		if err := tx.Delete(&NodeSettingStaged{}, "node_id = ?", nodeID).Error; err != nil {
			return err
		}
		return tx.Delete(&EnvVarStaged{}, "node_id = ?", nodeID).Error
	})
}

func (s *Store) DeleteStagedChanges(nodeID string) error {
	return s.DiscardAllStagedChanges(nodeID)
}

func (s *Store) getStagedNodeSettingsTx(tx *gorm.DB, nodeID string) (map[string]string, error) {
	var rows []NodeSettingStaged
	if err := tx.Where("node_id = ?", nodeID).Find(&rows).Error; err != nil {
		return nil, err
	}
	m := make(map[string]string, len(rows))
	for _, row := range rows {
		m[row.Key] = row.Value
	}
	return m, nil
}

func (s *Store) listStagedEnvVarChangesTx(tx *gorm.DB, nodeID string) ([]EnvVarStaged, error) {
	var rows []EnvVarStaged
	if err := tx.Where("node_id = ?", nodeID).Find(&rows).Error; err != nil {
		return nil, err
	}
	return rows, nil
}
