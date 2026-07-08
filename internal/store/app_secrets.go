package store

import (
	"strings"

	"gorm.io/gorm"
)

// ListAppSecrets returns every app-wide secret ordered by key.
func (s *Store) ListAppSecrets() ([]AppSecret, error) {
	var secrets []AppSecret
	if err := s.DB.Order("key asc").Find(&secrets).Error; err != nil {
		return nil, err
	}
	return secrets, nil
}

// GetAppSecret returns a single app secret by key.
func (s *Store) GetAppSecret(key string) (AppSecret, error) {
	key = strings.TrimSpace(key)
	var secret AppSecret
	if err := s.DB.Where("key = ?", key).First(&secret).Error; err != nil {
		return AppSecret{}, err
	}
	return secret, nil
}

// SetAppSecret upserts an app-wide secret.
func (s *Store) SetAppSecret(key, value, description string) error {
	key = strings.TrimSpace(key)
	if err := validateEnvKey(key); err != nil {
		return err
	}
	secret := AppSecret{Key: key, Value: value, Description: strings.TrimSpace(description)}
	return s.DB.Save(&secret).Error
}

// DeleteAppSecret removes an app-wide secret by key.
func (s *Store) DeleteAppSecret(key string) error {
	key = strings.TrimSpace(key)
	if err := validateEnvKey(key); err != nil {
		return err
	}
	return s.DB.Delete(&AppSecret{}, "key = ?", key).Error
}

// AppSecretExists reports whether key is defined in app_secrets.
func (s *Store) AppSecretExists(key string) (bool, error) {
	key = strings.TrimSpace(key)
	var count int64
	if err := s.DB.Model(&AppSecret{}).Where("key = ?", key).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}

// ListAppSecretKeys returns sorted secret keys for reference validation.
func (s *Store) ListAppSecretKeys() ([]string, error) {
	secrets, err := s.ListAppSecrets()
	if err != nil {
		return nil, err
	}
	keys := make([]string, len(secrets))
	for i, s := range secrets {
		keys[i] = s.Key
	}
	return keys, nil
}

// ErrAppSecretNotFound is returned when a referenced app secret does not exist.
var ErrAppSecretNotFound = gorm.ErrRecordNotFound
