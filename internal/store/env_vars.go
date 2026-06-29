package store

import (
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"

	"gorm.io/gorm"
)

const (
	EnvScopeRuntime = "runtime"
	EnvScopeBuild   = "build"
	EnvScopeBoth    = "both"

	EnvSourceManual    = "manual"
	EnvSourceImported  = "imported"
	EnvSourceGenerated = "generated"
)

var envKeyPattern = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]*$`)

func (s *Store) ListEnvVars(nodeID string) ([]EnvVar, error) {
	var vars []EnvVar
	if err := s.DB.Where("node_id = ?", strings.TrimSpace(nodeID)).Order("key asc").Find(&vars).Error; err != nil {
		return nil, err
	}
	return vars, nil
}

func (s *Store) SetEnvVar(nodeID, key, value string) error {
	return s.UpsertEnvVar(EnvVar{
		NodeID: nodeID,
		Key:    key,
		Value:  value,
		Scope:  EnvScopeRuntime,
		Source: EnvSourceManual,
	})
}

func (s *Store) UpsertEnvVar(v EnvVar) error {
	normalized, err := normalizeEnvVar(v)
	if err != nil {
		return err
	}
	return s.DB.Save(&normalized).Error
}

func (s *Store) ImportEnvVars(nodeID, envFile string, values map[string]string) (EnvFileSyncResult, error) {
	nodeID = strings.TrimSpace(nodeID)
	envFile = strings.TrimSpace(envFile)
	result := EnvFileSyncResult{Path: envFile}
	if nodeID == "" {
		return result, ErrInvalidNode
	}

	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)

	for _, rawKey := range keys {
		key := strings.TrimSpace(rawKey)
		value := values[rawKey]
		if err := validateEnvKey(key); err != nil {
			result.Skipped++
			continue
		}

		var existing EnvVar
		err := s.DB.Where("node_id = ? AND key = ?", nodeID, key).First(&existing).Error
		if err != nil && !errors.Is(err, gorm.ErrRecordNotFound) {
			return result, err
		}

		if err == nil {
			if existing.Source == EnvSourceManual && existing.Value != value {
				result.Conflicts = append(result.Conflicts, EnvVarConflict{
					Key:           key,
					DatabaseValue: existing.Value,
					FileValue:     value,
				})
				result.Skipped++
				continue
			}

			if existing.Value == value && existing.Source == EnvSourceImported && existing.EnvFile == envFile {
				result.Unchanged++
				continue
			}

			existing.Value = value
			existing.Source = EnvSourceImported
			existing.EnvFile = envFile
			if existing.Scope == "" {
				existing.Scope = EnvScopeRuntime
			}
			if err := s.DB.Save(&existing).Error; err != nil {
				return result, err
			}
			result.Updated++
			continue
		}

		if err := s.UpsertEnvVar(EnvVar{
			NodeID:  nodeID,
			Key:     key,
			Value:   value,
			Scope:   EnvScopeRuntime,
			Source:  EnvSourceImported,
			EnvFile: envFile,
		}); err != nil {
			return result, err
		}
		result.Imported++
	}

	return result, nil
}

func normalizeEnvVar(v EnvVar) (EnvVar, error) {
	v.NodeID = strings.TrimSpace(v.NodeID)
	v.Key = strings.TrimSpace(v.Key)
	v.Scope = strings.TrimSpace(v.Scope)
	v.Source = strings.TrimSpace(v.Source)
	v.EnvFile = strings.TrimSpace(v.EnvFile)
	if v.NodeID == "" {
		return v, ErrInvalidNode
	}
	if err := validateEnvKey(v.Key); err != nil {
		return v, err
	}
	if v.Scope == "" {
		v.Scope = EnvScopeRuntime
	}
	if v.Source == "" {
		v.Source = EnvSourceManual
	}
	switch v.Scope {
	case EnvScopeRuntime, EnvScopeBuild, EnvScopeBoth:
	default:
		return v, fmt.Errorf("invalid env scope %q", v.Scope)
	}
	switch v.Source {
	case EnvSourceManual, EnvSourceImported, EnvSourceGenerated:
	default:
		return v, fmt.Errorf("invalid env source %q", v.Source)
	}
	return v, nil
}

func validateEnvKey(key string) error {
	if !envKeyPattern.MatchString(key) {
		return fmt.Errorf("invalid env key %q", key)
	}
	return nil
}
