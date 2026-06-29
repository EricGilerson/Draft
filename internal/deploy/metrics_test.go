package deploy

import (
	"testing"
	"time"

	"Draft/internal/store"
)

func TestSummarizeDeploymentHistoryUsesSeparatedTimings(t *testing.T) {
	buildStart := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	buildFinish := buildStart.Add(18 * time.Second)
	containerStart := buildFinish.Add(4 * time.Second)
	containerStop := containerStart.Add(2 * time.Minute)

	deployments := []store.Deployment{
		{
			ID:                 2,
			Status:             "stopped",
			CreatedAt:          buildStart,
			BuildStartedAt:     &buildStart,
			BuildFinishedAt:    &buildFinish,
			ContainerStartedAt: &containerStart,
			ContainerStoppedAt: &containerStop,
			FinishedAt:         &containerStop,
		},
		{
			ID:        1,
			Status:    "failed",
			CreatedAt: buildStart.Add(-10 * time.Minute),
			UpdatedAt: buildStart.Add(-9 * time.Minute),
			Error:     "container exited with code 1",
		},
	}

	summary := summarizeDeploymentHistory(deployments)
	if summary.TotalDeployments != 2 {
		t.Fatalf("TotalDeployments = %d, want 2", summary.TotalDeployments)
	}
	if summary.SuccessCount != 1 || summary.FailureCount != 1 {
		t.Fatalf("success/failure = %d/%d, want 1/1", summary.SuccessCount, summary.FailureCount)
	}
	if summary.LastBuildDurationMs != 18_000 {
		t.Fatalf("LastBuildDurationMs = %d, want 18000", summary.LastBuildDurationMs)
	}
	if summary.LastBootDurationMs != 4_000 {
		t.Fatalf("LastBootDurationMs = %d, want 4000", summary.LastBootDurationMs)
	}
	if summary.LastRunDurationMs != 120_000 {
		t.Fatalf("LastRunDurationMs = %d, want 120000", summary.LastRunDurationMs)
	}
	if summary.LastFailureReason != "container exited with code 1" {
		t.Fatalf("LastFailureReason = %q", summary.LastFailureReason)
	}
}

func TestBuildTimelineItemsIncludesExitMetadata(t *testing.T) {
	buildStart := time.Date(2026, 6, 29, 12, 0, 0, 0, time.UTC)
	buildFinish := buildStart.Add(10 * time.Second)
	containerStart := buildFinish.Add(3 * time.Second)
	containerStop := containerStart.Add(30 * time.Second)
	exitCode := 137

	items := buildTimelineItems([]store.Deployment{{
		ID:                 7,
		Status:             "failed",
		ImageTag:           "draft-web:7",
		CreatedAt:          buildStart,
		BuildStartedAt:     &buildStart,
		BuildFinishedAt:    &buildFinish,
		ContainerStartedAt: &containerStart,
		ContainerStoppedAt: &containerStop,
		FinishedAt:         &containerStop,
		ExitCode:           &exitCode,
		OOMKilled:          true,
		Error:              "killed",
	}})

	if len(items) != 1 {
		t.Fatalf("len(items) = %d, want 1", len(items))
	}
	if items[0].ExitCode == nil || *items[0].ExitCode != 137 {
		t.Fatalf("ExitCode = %#v, want 137", items[0].ExitCode)
	}
	if !items[0].OOMKilled {
		t.Fatal("OOMKilled = false, want true")
	}
	if items[0].BuildDurationMs != 10_000 || items[0].BootDurationMs != 3_000 || items[0].RunDurationMs != 30_000 {
		t.Fatalf("unexpected durations: %+v", items[0])
	}
}

func TestProbeReachabilitySkipsNonWebServices(t *testing.T) {
	result := probeReachability("database", 5432, "")
	if result.Status != "not_applicable" {
		t.Fatalf("Status = %q, want not_applicable", result.Status)
	}
}
