package deploy

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"net/http"
	"strconv"
	"strings"
	"time"

	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

const (
	maxMetricPoints    = 120
	recentDeployWindow = 10
)

type MetricPoint struct {
	Timestamp        time.Time `json:"timestamp"`
	CPUPercent       float64   `json:"cpuPercent"`
	MemoryBytes      uint64    `json:"memoryBytes"`
	MemoryLimitBytes uint64    `json:"memoryLimitBytes"`
	NetworkRxBytes   uint64    `json:"networkRxBytes"`
	NetworkTxBytes   uint64    `json:"networkTxBytes"`
	NetworkRxRateBps float64   `json:"networkRxRateBps"`
	NetworkTxRateBps float64   `json:"networkTxRateBps"`
}

type ReachabilityCheck struct {
	Status     string     `json:"status"`
	TargetURL  string     `json:"targetUrl"`
	StatusCode int        `json:"statusCode"`
	LatencyMs  int64      `json:"latencyMs"`
	Error      string     `json:"error"`
	CheckedAt  *time.Time `json:"checkedAt"`
}

type DeploymentHistorySummary struct {
	TotalDeployments    int        `json:"totalDeployments"`
	RecentWindow        int        `json:"recentWindow"`
	SuccessCount        int        `json:"successCount"`
	FailureCount        int        `json:"failureCount"`
	InterruptedCount    int        `json:"interruptedCount"`
	LastDeployAt        *time.Time `json:"lastDeployAt"`
	LastFailureAt       *time.Time `json:"lastFailureAt"`
	LastFailureReason   string     `json:"lastFailureReason"`
	LastBuildDurationMs int64      `json:"lastBuildDurationMs"`
	LastBootDurationMs  int64      `json:"lastBootDurationMs"`
	LastRunDurationMs   int64      `json:"lastRunDurationMs"`
}

type DeploymentTimelineItem struct {
	DeploymentID    uint       `json:"deploymentId"`
	Status          string     `json:"status"`
	ImageTag        string     `json:"imageTag"`
	CreatedAt       time.Time  `json:"createdAt"`
	FinishedAt      *time.Time `json:"finishedAt"`
	BuildDurationMs int64      `json:"buildDurationMs"`
	BootDurationMs  int64      `json:"bootDurationMs"`
	RunDurationMs   int64      `json:"runDurationMs"`
	ExitCode        *int       `json:"exitCode"`
	Error           string     `json:"error"`
	OOMKilled       bool       `json:"oomKilled"`
}

type RuntimeEvent struct {
	Kind     string    `json:"kind"`
	Severity string    `json:"severity"`
	At       time.Time `json:"at"`
	Summary  string    `json:"summary"`
}

type ServiceMetrics struct {
	NodeID            string                   `json:"nodeId"`
	ServiceName       string                   `json:"serviceName"`
	ServiceType       string                   `json:"serviceType"`
	Status            string                   `json:"status"`
	DesiredPort       int                      `json:"desiredPort"`
	HostPort          int                      `json:"hostPort"`
	Hostname          string                   `json:"hostname"`
	InternalURL       string                   `json:"internalUrl"`
	PublicURL         string                   `json:"publicUrl"`
	CurrentDeployment *store.Deployment        `json:"currentDeployment"`
	UptimeMs          int64                    `json:"uptimeMs"`
	RestartCount      int                      `json:"restartCount"`
	OOMKilled         bool                     `json:"oomKilled"`
	ExitCode          *int                     `json:"exitCode"`
	DockerHealth      string                   `json:"dockerHealth"`
	ImageSizeBytes    int64                    `json:"imageSizeBytes"`
	WritableSizeBytes int64                    `json:"writableSizeBytes"`
	LiveMetricsError  string                   `json:"liveMetricsError"`
	LatestPoint       *MetricPoint             `json:"latestPoint"`
	LivePoints        []MetricPoint            `json:"livePoints"`
	Reachability      ReachabilityCheck        `json:"reachability"`
	DeploymentSummary DeploymentHistorySummary `json:"deploymentSummary"`
	RecentDeployments []DeploymentTimelineItem `json:"recentDeployments"`
	Events            []RuntimeEvent           `json:"events"`
}

func (e *Engine) GetServiceMetrics(ctx context.Context, nodeID string) (ServiceMetrics, error) {
	var node store.CanvasNode
	if err := e.store.DB.First(&node, "id = ?", nodeID).Error; err != nil {
		return ServiceMetrics{}, err
	}
	settings, err := e.store.GetNodeSettings(nodeID)
	if err != nil {
		return ServiceMetrics{}, err
	}
	deployments, err := e.store.ListDeployments(nodeID)
	if err != nil {
		return ServiceMetrics{}, err
	}

	serviceType := metricsServiceType(node.Label, settings)
	desiredPort, _ := strconv.Atoi(strings.TrimSpace(settings["service_port"]))
	metrics := ServiceMetrics{
		NodeID:      nodeID,
		ServiceName: node.Label,
		ServiceType: serviceType,
		Status:      "stopped",
		DesiredPort: desiredPort,
		Reachability: ReachabilityCheck{
			Status: "not_running",
		},
		DeploymentSummary: summarizeDeploymentHistory(deployments),
		RecentDeployments: buildTimelineItems(deployments),
		Events:            buildRuntimeEvents(deployments),
		LivePoints:        e.metricSeries(nodeID),
	}

	if len(deployments) > 0 {
		latest := deployments[0]
		metrics.Status = latest.Status
	}

	var active *store.Deployment
	for i := range deployments {
		if deployments[i].Status != "stopped" && deployments[i].Status != "failed" {
			active = &deployments[i]
			break
		}
	}
	if active == nil && len(deployments) > 0 {
		active = &deployments[0]
	}
	if active != nil {
		metrics.CurrentDeployment = active
		metrics.Status = active.Status
		metrics.HostPort = active.HostPort
		metrics.Hostname = active.Hostname
		metrics.InternalURL = networking.InternalURL(active.Hostname, strings.TrimSpace(settings["service_port"]))
		if e.router != nil {
			metrics.PublicURL = networking.PublicURL(active.Hostname, e.router.LocalDomainStatus().ProxyPort)
		}
		metrics.UptimeMs = uptimeMillis(active)
		metrics.ExitCode = active.ExitCode
		metrics.OOMKilled = active.OOMKilled
	}

	if active == nil || active.ContainerID == "" || (active.Status != "running" && active.Status != "starting") {
		if serviceType != "web" {
			metrics.Reachability.Status = "not_applicable"
		}
		return metrics, nil
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		metrics.LiveMetricsError = "Could not connect to Docker."
		return metrics, nil
	}
	defer cli.Close()

	inspect, _, inspectErr := cli.ContainerInspectWithRaw(ctx, active.ContainerID, true)
	if inspectErr == nil {
		metrics.RestartCount = inspect.RestartCount
		if inspect.SizeRootFs != nil {
			metrics.ImageSizeBytes = *inspect.SizeRootFs
		}
		if inspect.SizeRw != nil {
			metrics.WritableSizeBytes = *inspect.SizeRw
		}
		if inspect.State != nil {
			metrics.OOMKilled = inspect.State.OOMKilled || metrics.OOMKilled
			if inspect.State.Health != nil {
				metrics.DockerHealth = inspect.State.Health.Status
			}
			if metrics.ExitCode == nil && inspect.State.ExitCode != 0 {
				exitCode := inspect.State.ExitCode
				metrics.ExitCode = &exitCode
			}
		}
	} else if active.Status == "running" {
		metrics.LiveMetricsError = "Container inspect failed."
	}

	point, pointErr := e.collectLiveMetricPoint(ctx, active)
	if pointErr == nil {
		metrics.LivePoints = e.recordMetricPoint(nodeID, point)
		metrics.LatestPoint = &point
	} else if active.Status == "running" {
		metrics.LiveMetricsError = "Live stats unavailable."
	}

	metrics.Reachability = probeReachability(serviceType, active.HostPort, metrics.PublicURL)
	return metrics, nil
}

func (e *Engine) collectLiveMetricPoint(ctx context.Context, dep *store.Deployment) (MetricPoint, error) {
	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return MetricPoint{}, err
	}
	defer cli.Close()

	resp, err := cli.ContainerStatsOneShot(ctx, dep.ContainerID)
	if err != nil {
		return MetricPoint{}, err
	}
	defer resp.Body.Close()

	var stats container.StatsResponse
	if err := json.NewDecoder(resp.Body).Decode(&stats); err != nil {
		return MetricPoint{}, err
	}

	rxBytes, txBytes := aggregateNetwork(stats.Networks)
	cpuPercent := cpuPercent(stats)
	point := MetricPoint{
		Timestamp:        time.Now(),
		CPUPercent:       cpuPercent,
		MemoryBytes:      stats.MemoryStats.Usage,
		MemoryLimitBytes: stats.MemoryStats.Limit,
		NetworkRxBytes:   rxBytes,
		NetworkTxBytes:   txBytes,
	}

	prev := e.lastMetricPoint(dep.NodeID)
	if prev != nil {
		seconds := point.Timestamp.Sub(prev.Timestamp).Seconds()
		if seconds > 0 {
			point.NetworkRxRateBps = math.Max(0, float64(point.NetworkRxBytes-prev.NetworkRxBytes)/seconds)
			point.NetworkTxRateBps = math.Max(0, float64(point.NetworkTxBytes-prev.NetworkTxBytes)/seconds)
		}
	}
	return point, nil
}

func (e *Engine) recordMetricPoint(nodeID string, point MetricPoint) []MetricPoint {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	series := append(e.stats[nodeID], point)
	if len(series) > maxMetricPoints {
		series = series[len(series)-maxMetricPoints:]
	}
	e.stats[nodeID] = series
	return append([]MetricPoint(nil), series...)
}

func (e *Engine) metricSeries(nodeID string) []MetricPoint {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	return append([]MetricPoint(nil), e.stats[nodeID]...)
}

func (e *Engine) lastMetricPoint(nodeID string) *MetricPoint {
	e.statsMu.Lock()
	defer e.statsMu.Unlock()
	series := e.stats[nodeID]
	if len(series) == 0 {
		return nil
	}
	point := series[len(series)-1]
	return &point
}

func summarizeDeploymentHistory(deployments []store.Deployment) DeploymentHistorySummary {
	summary := DeploymentHistorySummary{
		TotalDeployments: len(deployments),
		RecentWindow:     recentDeployWindow,
	}
	window := deployments
	if len(window) > recentDeployWindow {
		window = window[:recentDeployWindow]
	}
	for i := range window {
		dep := window[i]
		if summary.LastDeployAt == nil {
			summary.LastDeployAt = &dep.CreatedAt
		}
		if isSuccessfulDeployment(dep) {
			summary.SuccessCount++
		}
		if dep.Status == "failed" {
			summary.FailureCount++
			if summary.LastFailureAt == nil {
				at := dep.ContainerStoppedAt
				if at == nil {
					at = dep.FinishedAt
				}
				if at == nil {
					at = &dep.UpdatedAt
				}
				summary.LastFailureAt = at
				summary.LastFailureReason = dep.Error
			}
		}
		if dep.Status == "interrupted" {
			summary.InterruptedCount++
		}
		if summary.LastBuildDurationMs == 0 {
			summary.LastBuildDurationMs = durationMillis(dep.BuildStartedAt, dep.BuildFinishedAt)
		}
		if summary.LastBootDurationMs == 0 {
			summary.LastBootDurationMs = durationMillis(dep.BuildFinishedAt, dep.ContainerStartedAt)
		}
		if summary.LastRunDurationMs == 0 {
			summary.LastRunDurationMs = runDurationMillis(dep)
		}
	}
	return summary
}

func buildTimelineItems(deployments []store.Deployment) []DeploymentTimelineItem {
	window := deployments
	if len(window) > recentDeployWindow {
		window = window[:recentDeployWindow]
	}
	items := make([]DeploymentTimelineItem, 0, len(window))
	for _, dep := range window {
		items = append(items, DeploymentTimelineItem{
			DeploymentID:    dep.ID,
			Status:          dep.Status,
			ImageTag:        dep.ImageTag,
			CreatedAt:       dep.CreatedAt,
			FinishedAt:      dep.FinishedAt,
			BuildDurationMs: durationMillis(dep.BuildStartedAt, dep.BuildFinishedAt),
			BootDurationMs:  durationMillis(dep.BuildFinishedAt, dep.ContainerStartedAt),
			RunDurationMs:   runDurationMillis(dep),
			ExitCode:        dep.ExitCode,
			Error:           dep.Error,
			OOMKilled:       dep.OOMKilled,
		})
	}
	return items
}

func buildRuntimeEvents(deployments []store.Deployment) []RuntimeEvent {
	events := make([]RuntimeEvent, 0, recentDeployWindow*2)
	window := deployments
	if len(window) > recentDeployWindow {
		window = window[:recentDeployWindow]
	}
	for _, dep := range window {
		events = append(events, RuntimeEvent{
			Kind:     "deploy",
			Severity: "info",
			At:       dep.CreatedAt,
			Summary:  "Deployment started",
		})
		if dep.ContainerStartedAt != nil {
			events = append(events, RuntimeEvent{
				Kind:     "running",
				Severity: "success",
				At:       *dep.ContainerStartedAt,
				Summary:  "Container reached running state",
			})
		}
		if dep.Status == "failed" {
			at := dep.UpdatedAt
			if dep.ContainerStoppedAt != nil {
				at = *dep.ContainerStoppedAt
			} else if dep.FinishedAt != nil {
				at = *dep.FinishedAt
			}
			summary := dep.Error
			if summary == "" {
				summary = "Deployment failed"
			}
			events = append(events, RuntimeEvent{
				Kind:     "failed",
				Severity: "error",
				At:       at,
				Summary:  summary,
			})
		} else if dep.Status == "interrupted" {
			at := dep.UpdatedAt
			if dep.FinishedAt != nil {
				at = *dep.FinishedAt
			}
			events = append(events, RuntimeEvent{
				Kind:     "interrupted",
				Severity: "warning",
				At:       at,
				Summary:  "Deployment interrupted by app restart",
			})
		} else if dep.Status == "stopped" && dep.ContainerStoppedAt != nil {
			events = append(events, RuntimeEvent{
				Kind:     "stopped",
				Severity: "warning",
				At:       *dep.ContainerStoppedAt,
				Summary:  "Container stopped",
			})
		}
	}
	if len(events) > recentDeployWindow*2 {
		events = events[:recentDeployWindow*2]
	}
	return events
}

func probeReachability(serviceType string, hostPort int, publicURL string) ReachabilityCheck {
	if serviceType != "web" {
		return ReachabilityCheck{Status: "not_applicable"}
	}
	if hostPort <= 0 {
		return ReachabilityCheck{Status: "not_running"}
	}

	targetURL := fmt.Sprintf("http://127.0.0.1:%d", hostPort)
	if publicURL != "" {
		targetURL = publicURL
	}
	now := time.Now()
	result := ReachabilityCheck{
		Status:    "unreachable",
		TargetURL: targetURL,
		CheckedAt: &now,
	}
	client := &http.Client{Timeout: 1500 * time.Millisecond}
	start := time.Now()
	resp, err := client.Get(fmt.Sprintf("http://127.0.0.1:%d", hostPort))
	result.LatencyMs = time.Since(start).Milliseconds()
	if err != nil {
		result.Error = err.Error()
		return result
	}
	defer resp.Body.Close()
	result.StatusCode = resp.StatusCode
	if resp.StatusCode >= 200 && resp.StatusCode < 400 {
		result.Status = "healthy"
		return result
	}
	result.Status = "degraded"
	return result
}

func metricsServiceType(label string, settings map[string]string) string {
	value := strings.ToLower(label + " " + settings["dockerfile"] + " " + settings["service_root"])
	switch {
	case strings.Contains(value, "postgres"), strings.Contains(value, "mysql"), strings.Contains(value, "database"), strings.Contains(value, " db"):
		return "database"
	case strings.Contains(value, "redis"), strings.Contains(value, "cache"):
		return "cache"
	case strings.Contains(value, "worker"), strings.Contains(value, "queue"), strings.Contains(value, "job"):
		return "worker"
	default:
		return "web"
	}
}

func cpuPercent(stats container.StatsResponse) float64 {
	cpuDelta := float64(stats.CPUStats.CPUUsage.TotalUsage) - float64(stats.PreCPUStats.CPUUsage.TotalUsage)
	systemDelta := float64(stats.CPUStats.SystemUsage) - float64(stats.PreCPUStats.SystemUsage)
	if cpuDelta <= 0 || systemDelta <= 0 {
		return 0
	}
	onlineCPUs := float64(stats.CPUStats.OnlineCPUs)
	if onlineCPUs == 0 {
		onlineCPUs = float64(len(stats.CPUStats.CPUUsage.PercpuUsage))
	}
	if onlineCPUs == 0 {
		onlineCPUs = 1
	}
	return (cpuDelta / systemDelta) * onlineCPUs * 100
}

func aggregateNetwork(networks map[string]container.NetworkStats) (uint64, uint64) {
	var rx, tx uint64
	for _, stats := range networks {
		rx += stats.RxBytes
		tx += stats.TxBytes
	}
	return rx, tx
}

func durationMillis(start, end *time.Time) int64 {
	if start == nil || end == nil {
		return 0
	}
	if end.Before(*start) {
		return 0
	}
	return end.Sub(*start).Milliseconds()
}

func runDurationMillis(dep store.Deployment) int64 {
	if dep.ContainerStartedAt == nil {
		return 0
	}
	end := dep.ContainerStoppedAt
	if end == nil {
		now := time.Now()
		end = &now
	}
	return durationMillis(dep.ContainerStartedAt, end)
}

func uptimeMillis(dep *store.Deployment) int64 {
	if dep == nil {
		return 0
	}
	start := dep.ContainerStartedAt
	if start == nil {
		start = dep.StartedAt
	}
	if start == nil {
		return 0
	}
	return time.Since(*start).Milliseconds()
}

func isSuccessfulDeployment(dep store.Deployment) bool {
	return dep.ContainerStartedAt != nil || dep.Status == "running" || dep.Status == "stopped"
}
