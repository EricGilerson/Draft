package appupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
)

type Job struct {
	AppPID    int    `json:"appPid"`
	DaemonPID int    `json:"daemonPid"`
	Artifact  string `json:"artifact"`
	AppPath   string `json:"appPath"`
}

func WriteJob(dir string, job Job) (string, error) {
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return "", err
	}
	path := filepath.Join(dir, "apply-job.json")
	data, err := json.Marshal(job)
	if err != nil {
		return "", err
	}
	return path, os.WriteFile(path, data, 0o600)
}

func ApplyJobFile(path string) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return err
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		return err
	}
	if job.Artifact == "" || job.AppPath == "" {
		return fmt.Errorf("update job is incomplete")
	}
	return apply(job)
}
