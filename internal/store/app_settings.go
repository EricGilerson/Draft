package store

import (
	"errors"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Known app-setting keys. Values are free-form strings; helpers below normalize
// the documented ones.
const (
	AppSettingCompactSidebar        = "compact_sidebar"
	AppSettingLocalDomainPreference = "local_domain_preference"
)

// Local domain preference values for AppSettingLocalDomainPreference.
const (
	LocalDomainPrefAuto      = "auto"
	LocalDomainPrefPublic    = "public-hostname-port"
	LocalDomainPrefLocalhost = "localhost-port"
)

// DefaultAppSettings returns the documented defaults applied when a key is
// missing from the database.
func DefaultAppSettings() map[string]string {
	return map[string]string{
		AppSettingCompactSidebar:        "false",
		AppSettingLocalDomainPreference: LocalDomainPrefAuto,
	}
}

// GetAppSetting returns the stored value for key, or the default if unset.
func (s *Store) GetAppSetting(key string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", nil
	}
	var row AppSetting
	err := s.DB.First(&row, "key = ?", key).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		if def, ok := DefaultAppSettings()[key]; ok {
			return def, nil
		}
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return row.Value, nil
}

// ListAppSettings returns every known setting with defaults filled in for
// missing keys, plus any extra keys that happen to be stored.
func (s *Store) ListAppSettings() (map[string]string, error) {
	out := DefaultAppSettings()
	var rows []AppSetting
	if err := s.DB.Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		out[row.Key] = row.Value
	}
	return out, nil
}

// SetAppSetting upserts a single preference key.
func (s *Store) SetAppSetting(key, value string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return nil
	}
	value = normalizeAppSetting(key, value)
	row := AppSetting{Key: key, Value: value, UpdatedAt: time.Now()}
	return s.DB.Save(&row).Error
}

// SetAppSettings merges the provided map into stored preferences.
func (s *Store) SetAppSettings(updates map[string]string) error {
	for key, value := range updates {
		if err := s.SetAppSetting(key, value); err != nil {
			return err
		}
	}
	return nil
}

func normalizeAppSetting(key, value string) string {
	value = strings.TrimSpace(value)
	switch key {
	case AppSettingCompactSidebar:
		if value == "1" || strings.EqualFold(value, "true") || value == "yes" {
			return "true"
		}
		return "false"
	case AppSettingLocalDomainPreference:
		switch value {
		case LocalDomainPrefPublic, LocalDomainPrefLocalhost, LocalDomainPrefAuto:
			return value
		default:
			return LocalDomainPrefAuto
		}
	default:
		return value
	}
}
