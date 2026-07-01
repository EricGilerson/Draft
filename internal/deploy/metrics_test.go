package deploy

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
	"time"

	"Draft/internal/store"
)

// TestMetricSeriesNonNilWhenEmpty guards the frontend crash: a nil slice
// marshals to JSON null, and the metrics UI calls .map()/.length on livePoints.
// An empty series must serialize as [] so the frontend never sees null.
func TestMetricSeriesNonNilWhenEmpty(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	series := e.metricSeries("node-with-no-metrics")
	if series == nil {
		t.Fatalf("metricSeries returned nil; frontend would receive null and crash")
	}
	if len(series) != 0 {
		t.Fatalf("expected empty series, got %d points", len(series))
	}

	out, err := json.Marshal(series)
	if err != nil {
		t.Fatalf("marshal: %v", err)
	}
	if string(out) != "[]" {
		t.Fatalf("expected empty series to marshal to [], got %q", out)
	}
}

// TestGetServiceMetricsEmptyStateSerializesArrays ensures the full metrics
// payload for a fresh, never-deployed, not-running service carries non-null
// arrays for the fields the frontend iterates.
func TestGetServiceMetricsEmptyStateSerializesArrays(t *testing.T) {
	s := openTestStore(t)
	e, _ := newTestEngine(t, s)

	project, err := s.CreateProject("Empty", t.TempDir(), "")
	if err != nil {
		t.Fatalf("create project: %v", err)
	}
	if _, err := s.CreateNode(&store.CanvasNode{ID: "node-1", ProjectID: project.ID, Label: "web"}); err != nil {
		t.Fatalf("create node: %v", err)
	}

	metrics, err := e.GetServiceMetrics(context.Background(), "node-1")
	if err != nil {
		t.Fatalf("GetServiceMetrics: %v", err)
	}

	out, err := json.Marshal(metrics)
	if err != nil {
		t.Fatalf("marshal metrics: %v", err)
	}
	payload := string(out)
	for _, field := range []string{`"livePoints":null`, `"events":null`, `"recentDeployments":null`} {
		if strings.Contains(payload, field) {
			t.Fatalf("metrics payload contains %s — frontend would crash on it", field)
		}
	}
}

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
