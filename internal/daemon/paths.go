package daemon

import (
	"os"
	"path/filepath"
)

const stateFileName = "daemon.json"

var userConfigDir = os.UserConfigDir

func ConfigDir() (string, error) {
	dir, err := userConfigDir()
	if err != nil {
		return "", err
	}
	dir = filepath.Join(dir, "Draft")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	return dir, nil
}

func StatePath() (string, error) {
	dir, err := ConfigDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dir, stateFileName), nil
}
