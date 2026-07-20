package store

import (
	"errors"
	"strconv"
	"strings"
	"time"

	"gorm.io/gorm"
)

// Known app-setting keys. Values are free-form strings; helpers below normalize
// the documented ones.
const (
	AppSettingCompactSidebar        = "compact_sidebar"
	AppSettingLocalDomainPreference = "local_domain_preference"
	// AppSettingLocalDraftDomainEnabled records whether Draft's optional,
	// machine-local *.draft resolver has been installed and enabled.
	AppSettingLocalDraftDomainEnabled = "local_draft_domain_enabled"
	// Reverse-proxy listen preference: try port 80 for clean URLs, optionally a
	// user fallback, or a fixed custom primary (+ same fallback), then ephemeral.
	AppSettingProxyPortMode     = "proxy_port_mode"
	AppSettingProxyPort         = "proxy_port"
	AppSettingProxyFallbackPort = "proxy_fallback_port"
	// AppSettingProxyBoundPort is an internal runtime record of the last
	// successful proxy bind. It is deliberately not a user preference: it
	// keeps public URLs stable when the configured ports are unavailable.
	AppSettingProxyBoundPort = "proxy_bound_port"
)

// Local domain preference values for AppSettingLocalDomainPreference.
const (
	LocalDomainPrefAuto      = "auto"
	LocalDomainPrefPublic    = "public-hostname-port"
	LocalDomainPrefLocalhost = "localhost-port"
)

// Proxy port mode values for AppSettingProxyPortMode.
const (
	ProxyPortModePrefer80         = "prefer80"
	ProxyPortModePrefer80Fallback = "prefer80_fallback"
	ProxyPortModeCustom           = "custom"
)

// DefaultAppSettings returns the documented defaults applied when a key is
// missing from the database.
func DefaultAppSettings() map[string]string {
	return map[string]string{
		AppSettingCompactSidebar:          "false",
		AppSettingLocalDomainPreference:   LocalDomainPrefAuto,
		AppSettingLocalDraftDomainEnabled: "false",
		// Prefer 80 for clean URLs; if taken, use the fixed fallback (not an
		// ephemeral OS port) so public URLs stay stable across daemon restarts.
		AppSettingProxyPortMode:     ProxyPortModePrefer80Fallback,
		AppSettingProxyPort:         "38473",
		AppSettingProxyFallbackPort: "38473",
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
	proxySettingsChanged := false
	for _, key := range []string{AppSettingProxyPortMode, AppSettingProxyPort, AppSettingProxyFallbackPort} {
		value, ok := updates[key]
		if !ok {
			continue
		}
		current, err := s.GetAppSetting(key)
		if err != nil {
			return err
		}
		if normalizeAppSetting(key, value) != current {
			proxySettingsChanged = true
		}
	}
	for key, value := range updates {
		if err := s.SetAppSetting(key, value); err != nil {
			return err
		}
	}
	// An explicit port-preference change is the one time it is correct to
	// abandon the sticky runtime binding; the user expects the new port plan
	// to take effect on the next daemon restart.
	if proxySettingsChanged {
		if err := s.DB.Delete(&AppSetting{}, "key = ?", AppSettingProxyBoundPort).Error; err != nil {
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
	case AppSettingLocalDraftDomainEnabled:
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
	case AppSettingProxyPortMode:
		switch value {
		case ProxyPortModePrefer80, ProxyPortModePrefer80Fallback, ProxyPortModeCustom:
			return value
		default:
			return ProxyPortModePrefer80Fallback
		}
	case AppSettingProxyPort, AppSettingProxyFallbackPort:
		if value == "" {
			return DefaultAppSettings()[key]
		}
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 65535 {
			return DefaultAppSettings()[key]
		}
		return strconv.Itoa(n)
	case AppSettingProxyBoundPort:
		n, err := strconv.Atoi(value)
		if err != nil || n < 1 || n > 65535 {
			return ""
		}
		return strconv.Itoa(n)
	default:
		return value
	}
}
