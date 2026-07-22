package appupdate

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

type Job struct {
	AppPID    int    `json:"appPid"`
	DaemonPID int    `json:"daemonPid"`
	Artifact  string `json:"artifact"`
	AppPath   string `json:"appPath"`
}

// relaunch starts the updated app. Tests replace this to avoid launching a GUI.
var relaunch = relaunchApp

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
	logPath := filepath.Join(filepath.Dir(path), "apply.log")
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	logf := func(format string, args ...any) {
		_, _ = fmt.Fprintf(logFile, "%s "+format+"\n", append([]any{time.Now().Format(time.RFC3339)}, args...)...)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		logf("read job: %v", err)
		return err
	}
	var job Job
	if err := json.Unmarshal(data, &job); err != nil {
		logf("parse job: %v", err)
		return err
	}
	if job.Artifact == "" || job.AppPath == "" {
		err := fmt.Errorf("update job is incomplete")
		logf("%v", err)
		return err
	}
	logf("apply start appPid=%d daemonPid=%d artifact=%s appPath=%s", job.AppPID, job.DaemonPID, job.Artifact, job.AppPath)
	if err := apply(job, logf); err != nil {
		logf("apply failed: %v", err)
		return err
	}
	logf("apply succeeded")
	return nil
}
