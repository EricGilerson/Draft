package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"time"

	"Draft/internal/deploy"
	"Draft/internal/dockerwatch"
	"Draft/internal/executil"
	"Draft/internal/networking"
	"Draft/internal/store"

	"github.com/docker/docker/api/types"
)

type Client struct {
	state State
	http  *http.Client
}

const daemonStartupLockName = "daemon-start.lock"

var (
	daemonStartupLockRetry      = 50 * time.Millisecond
	daemonStartupLockTimeout    = 10 * time.Second
	daemonStartupLockStaleAfter = 30 * time.Second
)

func Ensure(ctx context.Context) (*Client, error) {
	release, err := acquireDaemonStartupLock(ctx)
	if err != nil {
		return nil, err
	}
	defer release()

	if c, err := NewClientFromState(); err == nil && c.Ping(ctx) == nil {
		if c.IsCompatible() {
			return c, nil
		}
		if err := c.stopIncompatibleDaemon(ctx); err != nil {
			return nil, err
		}
	}
	if err := launchDaemon(); err != nil {
		return nil, err
	}
	deadline := time.Now().Add(10 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		c, err := NewClientFromState()
		if err == nil {
			if pingErr := c.Ping(ctx); pingErr == nil {
				if c.IsCompatible() {
					return c, nil
				}
				lastErr = fmt.Errorf("daemon protocol incompatible")
			} else {
				lastErr = pingErr
			}
		} else {
			lastErr = err
		}
		time.Sleep(200 * time.Millisecond)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("daemon did not become ready")
	}
	return nil, lastErr
}

// acquireDaemonStartupLock serializes daemon replacement across desktop-app,
// hook, and CLI processes. Without it, concurrent callers can each replace an
// incompatible daemon and leave separate proxies with divergent route maps.
func acquireDaemonStartupLock(ctx context.Context) (func(), error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, daemonStartupLockName)
	deadline := time.Now().Add(daemonStartupLockTimeout)

	for {
		file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
		if err == nil {
			_, _ = fmt.Fprintf(file, "%d\n", os.Getpid())
			_ = file.Close()
			return func() { _ = os.Remove(path) }, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create daemon startup lock: %w", err)
		}

		// A caller that crashed before its deferred cleanup must not prevent
		// Draft from starting forever. The lock covers only a short launch and
		// state-file handoff, so a 30-second-old file is abandoned safely.
		if info, statErr := os.Stat(path); statErr == nil && time.Since(info.ModTime()) > daemonStartupLockStaleAfter {
			_ = os.Remove(path)
			continue
		}
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		if time.Now().After(deadline) {
			return nil, fmt.Errorf("timed out waiting for daemon startup")
		}
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(daemonStartupLockRetry):
		}
	}
}

// IsCompatible reports whether this daemon implements the API expected by the
// current desktop binary. State files from earlier releases have Protocol=0.
func (c *Client) IsCompatible() bool {
	return c != nil && c.state.Protocol == daemonProtocolVersion
}

// stopIncompatibleDaemon stops only the process that both owns this state file
// and answers its authenticated health endpoint. The PID verification prevents
// an old state file from targeting an unrelated process after PID reuse.
func (c *Client) stopIncompatibleDaemon(ctx context.Context) error {
	var health struct {
		PID int `json:"pid"`
	}
	if err := c.get(ctx, "/health", &health); err != nil {
		return fmt.Errorf("check incompatible daemon: %w", err)
	}
	if c.state.PID <= 0 || health.PID != c.state.PID {
		return fmt.Errorf("incompatible daemon state does not match its running process")
	}
	proc, err := os.FindProcess(c.state.PID)
	if err != nil {
		return fmt.Errorf("find incompatible daemon: %w", err)
	}
	if err := proc.Kill(); err != nil {
		return fmt.Errorf("stop incompatible daemon: %w", err)
	}

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if c.Ping(ctx) != nil {
			return nil
		}
		time.Sleep(50 * time.Millisecond)
	}
	return fmt.Errorf("incompatible daemon did not stop")
}

func NewClientFromState() (*Client, error) {
	path, err := StatePath()
	if err != nil {
		return nil, err
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	var state State
	if err := json.Unmarshal(data, &state); err != nil {
		return nil, err
	}
	if state.Addr == "" || state.Token == "" {
		return nil, fmt.Errorf("daemon state is incomplete")
	}
	return &Client{
		state: state,
		http:  &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (c *Client) Ping(ctx context.Context) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/health"), nil)
	if err != nil {
		return err
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return fmt.Errorf("daemon health returned %s", resp.Status)
	}
	return nil
}

// State returns the daemon's address and auth token. The frontend needs these
// to open a direct WebSocket to the daemon (e.g. for the interactive shell),
// since browsers can't attach the X-Draft-Token header to a WS handshake.
func (c *Client) State() State { return c.state }

func (c *Client) Deploy(ctx context.Context, nodeID string) error {
	return c.postNode(ctx, "/deploy", nodeID)
}

func (c *Client) Stop(ctx context.Context, nodeID string) error {
	return c.postNode(ctx, "/stop", nodeID)
}

func (c *Client) Restart(ctx context.Context, nodeID string) error {
	return c.postNode(ctx, "/restart", nodeID)
}

// PrepareForUpdate asks the daemon to either report active deployments or to
// gracefully drain and exit. It intentionally does not stop user containers.
func (c *Client) PrepareForUpdate(ctx context.Context, cancelActive bool) (*UpdateReadiness, error) {
	var out UpdateReadiness
	err := c.postJSONWithTimeout(ctx, "/update/prepare", prepareUpdateRequest{CancelActive: cancelActive}, &out, 35*time.Second)
	return &out, err
}

func (c *Client) StartLogStream(ctx context.Context, nodeID string) error {
	return c.postNode(ctx, "/logs/start", nodeID)
}

func (c *Client) GetContainerLogHistory(ctx context.Context, nodeID string, tail int) (*deploy.LogHistory, error) {
	var out deploy.LogHistory
	err := c.get(ctx, fmt.Sprintf("/logs/history?nodeId=%s&tail=%d", url.QueryEscape(nodeID), tail), &out)
	return &out, err
}

// CreateNodeFromTemplate stamps a new canvas node out of a template via the
// daemon (which owns the deploy engine needed to resolve {{draft.*}} at stamp
// time). Returns the created node plus any non-fatal warnings.
func (c *Client) CreateNodeFromTemplate(ctx context.Context, req deploy.CreateNodeFromTemplateRequest) (*deploy.CreateNodeFromTemplateResult, error) {
	var out deploy.CreateNodeFromTemplateResult
	if err := c.postJSON(ctx, "/node/create-from-template", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteService(ctx context.Context, nodeID string) error {
	return c.postNode(ctx, "/node/delete", nodeID)
}

func (c *Client) DeleteEnvironment(ctx context.Context, environmentID uint) error {
	return c.postJSON(ctx, "/environment/delete", map[string]uint{"environmentId": environmentID}, nil)
}

func (c *Client) PreviewSandbox(ctx context.Context, req deploy.SandboxCreateRequest) (*deploy.SandboxPreview, error) {
	var out deploy.SandboxPreview
	if err := c.postJSON(ctx, "/sandbox/preview", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CreateSandbox(ctx context.Context, req deploy.SandboxCreateRequest) (*deploy.SandboxCreateResult, error) {
	var out deploy.SandboxCreateResult
	if err := c.postJSON(ctx, "/sandbox/create", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListSandboxSourceRepos(ctx context.Context, sourceEnvironmentID uint) (*deploy.SandboxSourceRepos, error) {
	var out deploy.SandboxSourceRepos
	if err := c.postJSON(ctx, "/sandbox/source-repos", map[string]uint{"sourceEnvironmentId": sourceEnvironmentID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ResolveSandboxRef(ctx context.Context, repoRoot, ref, commitSHA string) (*store.SandboxRepositorySource, error) {
	var out store.SandboxRepositorySource
	if err := c.postJSON(ctx, "/sandbox/resolve-ref", map[string]string{
		"repoRoot":  repoRoot,
		"ref":       ref,
		"commitSha": commitSHA,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RefreshSandbox(ctx context.Context, req deploy.SandboxRefreshRequest) (*deploy.SandboxRefreshResult, error) {
	var out deploy.SandboxRefreshResult
	if err := c.postJSON(ctx, "/sandbox/refresh", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ExtendSandbox(ctx context.Context, sandboxID uint, ttlHours int) (*store.Sandbox, error) {
	var out store.Sandbox
	if err := c.postJSON(ctx, "/sandbox/extend", map[string]any{"sandboxId": sandboxID, "ttlHours": ttlHours}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ExtendSandboxUntil(ctx context.Context, sandboxID uint, expiresAt time.Time) (*store.Sandbox, error) {
	var out store.Sandbox
	if err := c.postJSON(ctx, "/sandbox/extend", map[string]any{"sandboxId": sandboxID, "expiresAt": expiresAt.UTC()}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DeleteSandbox(ctx context.Context, sandboxID uint) error {
	return c.postJSON(ctx, "/sandbox/delete", map[string]uint{"sandboxId": sandboxID}, nil)
}

func (c *Client) PreviewSandboxPurge(ctx context.Context, sandboxID uint) (*deploy.SandboxPurgeInventory, error) {
	var out deploy.SandboxPurgeInventory
	if err := c.postJSON(ctx, "/sandbox/purge-preview", map[string]uint{"sandboxId": sandboxID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetSandboxDetail(ctx context.Context, sandboxID uint) (*deploy.SandboxDetail, error) {
	var out deploy.SandboxDetail
	if err := c.postJSON(ctx, "/sandbox/detail", map[string]uint{"sandboxId": sandboxID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) SuspendSandbox(ctx context.Context, sandboxID uint) (*store.Sandbox, error) {
	var out store.Sandbox
	if err := c.postJSON(ctx, "/sandbox/suspend", map[string]uint{"sandboxId": sandboxID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ResumeSandbox(ctx context.Context, sandboxID uint) (*store.Sandbox, error) {
	var out store.Sandbox
	if err := c.postJSON(ctx, "/sandbox/resume", map[string]uint{"sandboxId": sandboxID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RunTestingSandbox(ctx context.Context, req deploy.SandboxTestRunRequest) (*deploy.SandboxTestRunResult, error) {
	var out deploy.SandboxTestRunResult
	if err := c.postJSON(ctx, "/sandbox/test/run", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StartTestingSandbox(ctx context.Context, req deploy.SandboxTestRunRequest) (*deploy.SandboxTestRunResult, error) {
	var out deploy.SandboxTestRunResult
	if err := c.postJSON(ctx, "/sandbox/test/start", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ListSandboxTestRuns(ctx context.Context, projectID uint, limit int) ([]store.SandboxTestRun, error) {
	var out []store.SandboxTestRun
	if err := c.postJSON(ctx, "/sandbox/test/runs", map[string]any{"projectId": projectID, "limit": limit}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetSandboxTestRun(ctx context.Context, runID uint) (*deploy.SandboxTestRunResult, error) {
	var out deploy.SandboxTestRunResult
	if err := c.postJSON(ctx, "/sandbox/test/run/get", map[string]uint{"runId": runID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RunEnvironmentStack(ctx context.Context, environmentID uint, action string) (*deploy.EnvironmentStackResult, error) {
	var out deploy.EnvironmentStackResult
	body := map[string]any{
		"environmentId": environmentID,
		"action":        action,
	}
	if err := c.postJSON(ctx, "/environment/stack", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) PreviewSync(ctx context.Context, req deploy.SyncRequest) (*deploy.SyncPreview, error) {
	var out deploy.SyncPreview
	if err := c.postJSON(ctx, "/sync/preview", req, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ApplySync(ctx context.Context, req deploy.SyncRequest, mode string) (*deploy.SyncApplyResult, error) {
	var out deploy.SyncApplyResult
	body := map[string]any{
		"scope":               req.Scope,
		"sourceEnvironmentId": req.SourceEnvironmentID,
		"targetEnvironmentId": req.TargetEnvironmentID,
		"sourceNodeId":        req.SourceNodeID,
		"targetNodeId":        req.TargetNodeID,
		"includeSettings":     req.IncludeSettings,
		"includeEnv":          req.IncludeEnv,
		"mode":                mode,
	}
	if err := c.postJSON(ctx, "/sync/apply", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) DuplicateEnvironment(ctx context.Context, sourceEnvironmentID uint, newName string, choices []deploy.ServiceDataChoice, startAfter bool, repositories []deploy.SandboxRepositoryRef) (*deploy.DuplicateEnvironmentResult, error) {
	var out deploy.DuplicateEnvironmentResult
	body := map[string]any{
		"sourceEnvironmentId": sourceEnvironmentID,
		"newName":             newName,
		"choices":             choices,
		"startAfter":          startAfter,
		"repositories":        repositories,
	}
	// Duplicate with clone-data choices can copy multiple volumes; keep the UI waiting.
	// Start-after also kicks off deploys inline (kickoff only), so keep the long timeout.
	if err := c.postJSONWithTimeout(ctx, "/environment/duplicate", body, &out, 10*time.Minute); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) PreviewEnvironmentDuplicate(ctx context.Context, sourceEnvironmentID uint) ([]deploy.StatefulServiceSummary, error) {
	var out []deploy.StatefulServiceSummary
	body := map[string]any{"sourceEnvironmentId": sourceEnvironmentID}
	if err := c.postJSON(ctx, "/environment/duplicate-preview", body, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) GetLinkedServiceInfo(ctx context.Context, nodeID string) (*deploy.LinkedServiceInfo, error) {
	var out deploy.LinkedServiceInfo
	if err := c.postJSON(ctx, "/service/link-info", map[string]string{"nodeId": nodeID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) PromoteLinkedService(ctx context.Context, nodeID, seed string, consistency deploy.CloneConsistency) error {
	// Promote+clone may stop the root, tar-copy volumes, and restart — far beyond the default 30s client timeout.
	return c.postJSONWithTimeout(ctx, "/service/promote", map[string]any{
		"nodeId": nodeID, "seed": seed, "consistency": consistency,
	}, nil, 10*time.Minute)
}

func (c *Client) UnlinkService(ctx context.Context, nodeID, become string) error {
	return c.postJSON(ctx, "/service/unlink", map[string]any{
		"nodeId": nodeID, "become": become,
	}, nil)
}

func (c *Client) PreviewLinkToSharedRoot(ctx context.Context, nodeID, rootNodeID string) (*deploy.LinkToSharedRootPreview, error) {
	var out deploy.LinkToSharedRootPreview
	if err := c.postJSON(ctx, "/service/link-preview", map[string]any{
		"nodeId": nodeID, "rootNodeId": rootNodeID,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) LinkToSharedRoot(ctx context.Context, nodeID, rootNodeID string, volumes deploy.VolumeDisposition) error {
	return c.postJSONWithTimeout(ctx, "/service/link", map[string]any{
		"nodeId": nodeID, "rootNodeId": rootNodeID, "volumes": volumes,
	}, nil, 2*time.Minute)
}

func (c *Client) ListShareableRoots(ctx context.Context, projectID, excludeEnvironmentID uint) ([]deploy.RootServiceSummary, error) {
	var out []deploy.RootServiceSummary
	if err := c.postJSON(ctx, "/service/shareable-roots", map[string]any{
		"projectId": projectID, "excludeEnvironmentId": excludeEnvironmentID,
	}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) ListShareTargets(ctx context.Context, nodeID string) ([]deploy.ShareTargetEnvironment, error) {
	var out []deploy.ShareTargetEnvironment
	if err := c.postJSON(ctx, "/service/share-targets", map[string]any{
		"nodeId": nodeID,
	}, &out); err != nil {
		return nil, err
	}
	return out, nil
}

func (c *Client) PreviewCloneVolume(ctx context.Context, targetNodeID, sourceNodeID, containerPath string) (*deploy.CloneVolumePreview, error) {
	var out deploy.CloneVolumePreview
	if err := c.postJSON(ctx, "/volume/clone-preview", map[string]any{
		"targetNodeId": targetNodeID, "sourceNodeId": sourceNodeID, "containerPath": containerPath,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) CloneVolumeData(ctx context.Context, targetNodeID, sourceNodeID, containerPath string, consistency deploy.CloneConsistency) (*deploy.CloneVolumeResult, error) {
	var out deploy.CloneVolumeResult
	// Volume tar-copy (and consistent stop/restart) routinely exceeds the default 30s client timeout.
	if err := c.postJSONWithTimeout(ctx, "/volume/clone", map[string]any{
		"targetNodeId": targetNodeID, "sourceNodeId": sourceNodeID,
		"containerPath": containerPath, "consistency": consistency,
	}, &out, 10*time.Minute); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) PreviewDeleteService(ctx context.Context, nodeID string) (*deploy.DeleteServicePreview, error) {
	var out deploy.DeleteServicePreview
	if err := c.postJSON(ctx, "/node/delete-preview", nodeRequest{NodeID: nodeID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetNodeConfigStatus(ctx context.Context, nodeID string) (*deploy.NodeConfigStatus, error) {
	var out deploy.NodeConfigStatus
	if err := c.postJSON(ctx, "/node/config-status", nodeRequest{NodeID: nodeID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) GetServiceStaleness(ctx context.Context, nodeID string) (*deploy.ServiceStaleness, error) {
	var out deploy.ServiceStaleness
	if err := c.postJSON(ctx, "/node/staleness", nodeRequest{NodeID: nodeID}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StageNodeSettings(ctx context.Context, nodeID string, projectID uint, settings map[string]string) error {
	return c.postJSON(ctx, "/node/stage-settings", map[string]any{
		"nodeId": nodeID, "projectId": projectID, "settings": settings,
	}, nil)
}

func (c *Client) StageEnvVarChanges(ctx context.Context, nodeID string, upserts []store.EnvVarStageUpsert, deleteKeys []string) error {
	return c.postJSON(ctx, "/node/stage-env", map[string]any{
		"nodeId": nodeID, "upserts": upserts, "deleteKeys": deleteKeys,
	}, nil)
}

func (c *Client) DiscardStagedChanges(ctx context.Context, nodeID string) error {
	return c.postNode(ctx, "/node/discard-staged", nodeID)
}

func (c *Client) PreviewStagedChanges(ctx context.Context, nodeID string, proposedSettings map[string]string) (*deploy.StagedChangePreview, error) {
	var out deploy.StagedChangePreview
	if err := c.postJSON(ctx, "/node/preview-staged", map[string]any{
		"nodeId": nodeID, "proposedSettings": proposedSettings,
	}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) StopLogStream(ctx context.Context, nodeID string) error {
	return c.postNode(ctx, "/logs/stop", nodeID)
}

func (c *Client) GetDeployments(ctx context.Context, nodeID string) ([]store.Deployment, error) {
	var out []store.Deployment
	err := c.get(ctx, "/deployments?nodeId="+url.QueryEscape(nodeID), &out)
	return out, err
}

func (c *Client) GetActiveDeployment(ctx context.Context, nodeID string) (*store.Deployment, error) {
	var out *store.Deployment
	err := c.get(ctx, "/active-deployment?nodeId="+url.QueryEscape(nodeID), &out)
	return out, err
}

func (c *Client) GetBuildLog(ctx context.Context, deploymentID uint) (string, error) {
	var out struct {
		Log string `json:"log"`
	}
	err := c.get(ctx, fmt.Sprintf("/build-log?id=%d", deploymentID), &out)
	return out.Log, err
}

func (c *Client) GetServiceMetrics(ctx context.Context, nodeID string) (deploy.ServiceMetrics, error) {
	var out deploy.ServiceMetrics
	err := c.get(ctx, "/metrics?nodeId="+url.QueryEscape(nodeID), &out)
	return out, err
}

func (c *Client) GetNodeHealth(ctx context.Context, nodeID string) (deploy.NodeHealth, error) {
	var out deploy.NodeHealth
	err := c.get(ctx, "/node/health?nodeId="+url.QueryEscape(nodeID), &out)
	return out, err
}

func (c *Client) ReapplyTemplate(ctx context.Context, nodeID string) (deploy.CreateNodeFromTemplateResult, error) {
	var out deploy.CreateNodeFromTemplateResult
	err := c.postJSON(ctx, "/node/reapply-template", nodeRequest{NodeID: nodeID}, &out)
	return out, err
}

func (c *Client) ImportConfigPreview(ctx context.Context, path string) (*deploy.ImportPreview, error) {
	var out deploy.ImportPreview
	if err := c.postJSON(ctx, "/config/import-preview", map[string]string{"path": path}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ImportConfigAsProject(ctx context.Context, path, projectName string) (*deploy.ImportResult, error) {
	var out deploy.ImportResult
	if err := c.postJSON(ctx, "/config/import-as-project", map[string]string{"path": path, "projectName": projectName}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ImportConfigIntoProject(ctx context.Context, projectID, environmentID uint, path string, x, y float64) (*deploy.ImportResult, error) {
	var out deploy.ImportResult
	body := map[string]any{"projectId": projectID, "environmentId": environmentID, "path": path, "x": x, "y": y}
	if err := c.postJSON(ctx, "/config/import-into-project", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ExportConfig(ctx context.Context, nodeID, format string) (*deploy.ExportResult, error) {
	var out deploy.ExportResult
	if err := c.postJSON(ctx, "/config/export", map[string]string{"nodeId": nodeID, "format": format}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ExportProjectConfig(ctx context.Context, projectID uint, format string) (*deploy.ExportResult, error) {
	var out deploy.ExportResult
	body := map[string]any{"projectId": projectID, "format": format}
	if err := c.postJSON(ctx, "/config/export-project", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ExportConfigToPath(ctx context.Context, nodeID, format, destDir string) (*deploy.ExportResult, error) {
	var out deploy.ExportResult
	body := map[string]string{"nodeId": nodeID, "format": format, "destDir": destDir}
	if err := c.postJSON(ctx, "/config/export-to-path", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ExportDraftPackService(ctx context.Context, nodeID string, opts deploy.DraftPackExportOptions) (*deploy.DraftPackExportResult, error) {
	var out deploy.DraftPackExportResult
	if err := c.postJSON(ctx, "/draftpack/export-service", map[string]any{"nodeId": nodeID, "options": opts}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ExportDraftPackEnvironment(ctx context.Context, environmentID uint, opts deploy.DraftPackExportOptions) (*deploy.DraftPackExportResult, error) {
	var out deploy.DraftPackExportResult
	if err := c.postJSON(ctx, "/draftpack/export-environment", map[string]any{"environmentId": environmentID, "options": opts}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ExportDraftPackProject(ctx context.Context, projectID uint, opts deploy.DraftPackExportOptions) (*deploy.DraftPackExportResult, error) {
	var out deploy.DraftPackExportResult
	if err := c.postJSON(ctx, "/draftpack/export-project", map[string]any{"projectId": projectID, "options": opts}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ExportDraftPackToPath(ctx context.Context, scope, nodeID string, projectID, environmentID uint, opts deploy.DraftPackExportOptions, destPath string) (*deploy.DraftPackExportResult, error) {
	var out deploy.DraftPackExportResult
	body := map[string]any{
		"scope": scope, "nodeId": nodeID, "projectId": projectID, "environmentId": environmentID,
		"options": opts, "destPath": destPath,
	}
	if err := c.postJSON(ctx, "/draftpack/export-to-path", body, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) PreviewDraftPackImport(ctx context.Context, path string, opts deploy.DraftPackPreviewOptions) (*deploy.DraftPackImportPreview, error) {
	var out deploy.DraftPackImportPreview
	if err := c.postJSON(ctx, "/draftpack/import-preview", map[string]any{"path": path, "options": opts}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ImportDraftPack(ctx context.Context, path string, opts deploy.DraftPackImportOptions) (*deploy.DraftPackImportResult, error) {
	var out deploy.DraftPackImportResult
	if err := c.postJSON(ctx, "/draftpack/import", map[string]any{"path": path, "options": opts}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) PreviewDraftPackJSON(ctx context.Context, jsonText string, opts deploy.DraftPackPreviewOptions) (*deploy.DraftPackImportPreview, error) {
	var out deploy.DraftPackImportPreview
	if err := c.postJSON(ctx, "/draftpack/import-preview-json", map[string]any{"json": jsonText, "options": opts}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) ImportDraftPackJSON(ctx context.Context, jsonText string, opts deploy.DraftPackImportOptions) (*deploy.DraftPackImportResult, error) {
	var out deploy.DraftPackImportResult
	if err := c.postJSON(ctx, "/draftpack/import-json", map[string]any{"json": jsonText, "options": opts}, &out); err != nil {
		return nil, err
	}
	return &out, nil
}

func (c *Client) RollbackDeployment(ctx context.Context, deploymentID uint) error {
	return c.postJSON(ctx, "/rollback", map[string]any{"deploymentId": deploymentID}, nil)
}

func (c *Client) RollbackEligibility(ctx context.Context, nodeID string) ([]deploy.RollbackEligibility, error) {
	var out []deploy.RollbackEligibility
	err := c.postJSON(ctx, "/deployments/rollback-eligible", nodeRequest{NodeID: nodeID}, &out)
	return out, err
}

// RunCommand executes a one-shot command in the node's active container.
func (c *Client) RunCommand(ctx context.Context, nodeID string, cmd []string, workDir string) (deploy.RunCommandResult, error) {
	var out deploy.RunCommandResult
	err := c.postJSON(ctx, "/exec/run", map[string]any{"nodeId": nodeID, "cmd": cmd, "workDir": workDir}, &out)
	return out, err
}

// MintShellTicket mints a short-lived single-use ticket for WebSocket /exec/attach.
func (c *Client) MintShellTicket(ctx context.Context, nodeID, shell string) (*execTicketResponse, error) {
	var out execTicketResponse
	err := c.postJSON(ctx, "/exec/ticket", map[string]any{"nodeId": nodeID, "shell": shell}, &out)
	if err != nil {
		return nil, err
	}
	return &out, nil
}

// ListProjects returns all projects.
func (c *Client) ListProjects(ctx context.Context) ([]store.Project, error) {
	var out []store.Project
	err := c.postJSON(ctx, "/projects/list", map[string]any{}, &out)
	return out, err
}

// CreateProject creates a project and its default environment.
func (c *Client) CreateProject(ctx context.Context, name, path, description string) (*store.Project, error) {
	var out store.Project
	err := c.postJSON(ctx, "/project/create", map[string]any{
		"name": name, "path": path, "description": description,
	}, &out)
	return &out, err
}

// ListEnvironments returns environments for a project.
func (c *Client) ListEnvironments(ctx context.Context, projectID uint) ([]store.Environment, error) {
	var out []store.Environment
	err := c.postJSON(ctx, "/environments/list", map[string]any{"projectId": projectID}, &out)
	return out, err
}

// CreateEnvironment creates a named environment in a project.
func (c *Client) CreateEnvironment(ctx context.Context, projectID uint, name string) (*store.Environment, error) {
	var out store.Environment
	err := c.postJSON(ctx, "/environment/create", map[string]any{"projectId": projectID, "name": name}, &out)
	return &out, err
}

// RenameEnvironment updates an environment's display name (slug is immutable).
func (c *Client) RenameEnvironment(ctx context.Context, id uint, name string) error {
	return c.postJSON(ctx, "/environment/rename", map[string]any{"id": id, "name": name}, nil)
}

// SetDefaultEnvironment marks an environment as the project's default.
func (c *Client) SetDefaultEnvironment(ctx context.Context, environmentID uint) error {
	return c.postJSON(ctx, "/environment/set-default", map[string]any{"environmentId": environmentID}, nil)
}

// ListNodes returns canvas nodes for an environment.
func (c *Client) ListNodes(ctx context.Context, environmentID uint) ([]store.CanvasNode, error) {
	var out []store.CanvasNode
	err := c.postJSON(ctx, "/nodes/list", map[string]any{"environmentId": environmentID}, &out)
	return out, err
}

// CreateNode creates a blank canvas service node (no template).
func (c *Client) CreateNode(ctx context.Context, id, label string, projectID, environmentID uint, x, y float64) (*store.CanvasNode, error) {
	var out store.CanvasNode
	err := c.postJSON(ctx, "/node/create", map[string]any{
		"id": id, "label": label, "projectId": projectID, "environmentId": environmentID, "x": x, "y": y,
	}, &out)
	return &out, err
}

// GetNode returns a single canvas node.
func (c *Client) GetNode(ctx context.Context, id string) (*store.CanvasNode, error) {
	var out store.CanvasNode
	err := c.postJSON(ctx, "/node/get", map[string]any{"id": id}, &out)
	return &out, err
}

// GetNodeSettings returns applied node settings as a key/value map.
func (c *Client) GetNodeSettings(ctx context.Context, nodeID string) (map[string]string, error) {
	var out map[string]string
	err := c.postJSON(ctx, "/node/settings", map[string]any{"nodeId": nodeID}, &out)
	return out, err
}

// ListTemplates returns the service template library.
func (c *Client) ListTemplates(ctx context.Context) ([]store.ServiceTemplate, error) {
	var out []store.ServiceTemplate
	err := c.postJSON(ctx, "/templates/list", map[string]any{}, &out)
	return out, err
}

// GetTemplate returns one service template by ID.
func (c *Client) GetTemplate(ctx context.Context, id uint) (*store.ServiceTemplate, error) {
	var out store.ServiceTemplate
	err := c.postJSON(ctx, "/templates/get", map[string]any{"id": id}, &out)
	return &out, err
}

// ListSandboxes returns sandboxes for a project.
func (c *Client) ListSandboxes(ctx context.Context, projectID uint) ([]store.Sandbox, error) {
	var out []store.Sandbox
	err := c.postJSON(ctx, "/sandboxes/list", map[string]any{"projectId": projectID}, &out)
	return out, err
}

// ListSandboxProfiles returns sandbox profiles for a project.
func (c *Client) ListSandboxProfiles(ctx context.Context, projectID uint) ([]store.SandboxProfile, error) {
	var out []store.SandboxProfile
	err := c.postJSON(ctx, "/sandbox/profiles/list", map[string]any{"projectId": projectID}, &out)
	return out, err
}

// SaveSandboxProfile creates or updates a sandbox profile.
func (c *Client) SaveSandboxProfile(ctx context.Context, profile store.SandboxProfile) (*store.SandboxProfile, error) {
	var out store.SandboxProfile
	err := c.postJSON(ctx, "/sandbox/profiles/save", profile, &out)
	return &out, err
}

// DeleteSandboxProfile removes a sandbox profile by ID.
func (c *Client) DeleteSandboxProfile(ctx context.Context, id uint) error {
	return c.postJSON(ctx, "/sandbox/profiles/delete", map[string]any{"id": id}, nil)
}

// GetSandboxProjectSettings returns project-wide sandbox TTL defaults.
func (c *Client) GetSandboxProjectSettings(ctx context.Context, projectID uint) (*store.SandboxProjectSettings, error) {
	var out store.SandboxProjectSettings
	err := c.postJSON(ctx, "/sandbox/project-settings/get", map[string]any{"projectId": projectID}, &out)
	return &out, err
}

// SaveSandboxProjectSettings upserts project-wide sandbox defaults.
func (c *Client) SaveSandboxProjectSettings(ctx context.Context, settings store.SandboxProjectSettings) (*store.SandboxProjectSettings, error) {
	var out store.SandboxProjectSettings
	err := c.postJSON(ctx, "/sandbox/project-settings/save", settings, &out)
	return &out, err
}

// ListProjectServicesSummary returns multi-env service rollup for a project.
func (c *Client) ListProjectServicesSummary(ctx context.Context, projectID uint) (*deploy.ProjectServicesSummary, error) {
	var out deploy.ProjectServicesSummary
	err := c.postJSON(ctx, "/projects/services-summary", map[string]any{"projectId": projectID}, &out)
	return &out, err
}

// GetAppSettings returns persisted app preferences with defaults filled in.
func (c *Client) GetAppSettings(ctx context.Context) (*AppSettings, error) {
	var out AppSettings
	err := c.postJSON(ctx, "/app-settings/get", map[string]any{}, &out)
	return &out, err
}

// SetAppSettings merges app preferences and returns the stored result.
func (c *Client) SetAppSettings(ctx context.Context, settings AppSettings) (*AppSettings, error) {
	var out AppSettings
	err := c.postJSON(ctx, "/app-settings/set", settings, &out)
	return &out, err
}

// UpdateProject edits a project's name/description.
func (c *Client) UpdateProject(ctx context.Context, id uint, name, description string) error {
	return c.postJSON(ctx, "/project/update", map[string]any{"id": id, "name": name, "description": description}, nil)
}

// DeleteProject removes a project and every service in it.
func (c *Client) DeleteProject(ctx context.Context, id uint) error {
	return c.postJSON(ctx, "/project/delete", map[string]any{"id": id}, nil)
}

// ListProjectEnvVars returns project-level env vars (defaults injected into
// every service in the project at deploy time).
func (c *Client) ListProjectEnvVars(ctx context.Context, projectID uint) ([]store.ProjectEnvVar, error) {
	var out []store.ProjectEnvVar
	err := c.postJSON(ctx, "/project/env", map[string]any{"projectId": projectID}, &out)
	return out, err
}

// SetProjectEnvVar upserts a project-level env var.
func (c *Client) SetProjectEnvVar(ctx context.Context, projectID uint, key, value, scope string, secret bool) error {
	return c.postJSON(ctx, "/project/env/set", map[string]any{
		"projectId": projectID, "key": key, "value": value, "scope": scope, "secret": secret,
	}, nil)
}

// DeleteProjectEnvVar removes a project-level env var.
func (c *Client) DeleteProjectEnvVar(ctx context.Context, projectID uint, key string) error {
	return c.postJSON(ctx, "/project/env/delete", map[string]any{"projectId": projectID, "key": key}, nil)
}

// ListProjectEnvVarUsages returns services referencing {{project.key}}.
func (c *Client) ListProjectEnvVarUsages(ctx context.Context, projectID uint, key string) ([]deploy.SecretUsage, error) {
	var out []deploy.SecretUsage
	err := c.postJSON(ctx, "/project/env/usages", map[string]any{
		"projectId": projectID, "key": key,
	}, &out)
	return out, err
}

// ListRoutes returns routes, optionally filtered by project. Each row is
// enriched with the owning project name and service label.
func (c *Client) ListRoutes(ctx context.Context, projectID *uint) ([]RouteRow, error) {
	path := "/routes"
	if projectID != nil {
		path = fmt.Sprintf("/routes?projectId=%d", *projectID)
	}
	var out []RouteRow
	err := c.get(ctx, path, &out)
	return out, err
}

func (c *Client) CheckDocker(ctx context.Context) (dockerwatch.DaemonStatus, error) {
	var out dockerwatch.DaemonStatus
	return out, c.get(ctx, "/docker", &out)
}

func (c *Client) LocalDomainStatus(ctx context.Context) (networking.LocalDomainStatus, error) {
	var out networking.LocalDomainStatus
	return out, c.get(ctx, "/local-domain", &out)
}

func (c *Client) RefreshLocalDomainStatus(ctx context.Context) (networking.LocalDomainStatus, error) {
	var out networking.LocalDomainStatus
	return out, c.get(ctx, "/local-domain?refresh=1", &out)
}

func (c *Client) EnableLocalDraftDomain(ctx context.Context) (networking.LocalDomainStatus, error) {
	var out networking.LocalDomainStatus
	return out, c.postJSONWithTimeout(ctx, "/local-domain/enable", map[string]any{}, &out, 2*time.Minute)
}

func (c *Client) DisableLocalDraftDomain(ctx context.Context) (networking.LocalDomainStatus, error) {
	var out networking.LocalDomainStatus
	return out, c.postJSONWithTimeout(ctx, "/local-domain/disable", map[string]any{}, &out, 2*time.Minute)
}

func (c *Client) EnableLocalHTTPS(ctx context.Context) (networking.LocalDomainStatus, error) {
	var out networking.LocalDomainStatus
	err := c.postJSONWithTimeout(ctx, "/local-https/enable", map[string]any{}, &out, 2*time.Minute)
	return out, err
}

func (c *Client) DisableLocalHTTPS(ctx context.Context) (networking.LocalDomainStatus, error) {
	var out networking.LocalDomainStatus
	err := c.postJSONWithTimeout(ctx, "/local-https/disable", map[string]any{}, &out, 2*time.Minute)
	return out, err
}

func (c *Client) SuggestEnvFile(ctx context.Context, nodeID string, projectID uint) (string, error) {
	var out struct{ Path string }
	body, _ := json.Marshal(map[string]any{"nodeId": nodeID, "projectId": projectID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/env/suggest"), bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(tokenHeader, c.state.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return "", fmt.Errorf("%s", strings.TrimSpace(string(msg)))
	}
	if err := json.NewDecoder(resp.Body).Decode(&out); err != nil {
		return "", err
	}
	return out.Path, nil
}

func (c *Client) GetEnvVars(ctx context.Context, nodeID string) ([]store.EnvVar, error) {
	var out []store.EnvVar
	body, _ := json.Marshal(map[string]string{"nodeId": nodeID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/env"), bytes.NewReader(body))
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(tokenHeader, c.state.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("%s", strings.TrimSpace(string(msg)))
	}
	return out, json.NewDecoder(resp.Body).Decode(&out)
}

func (c *Client) SetEnvVar(ctx context.Context, nodeID, key, value string) error {
	body, _ := json.Marshal(map[string]string{"nodeId": nodeID, "key": key, "value": value})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url("/env/set"), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(tokenHeader, c.state.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(msg)))
	}
	return nil
}

func (c *Client) DeleteEnvVar(ctx context.Context, nodeID, key string) error {
	return c.postJSON(ctx, "/env/delete", map[string]string{"nodeId": nodeID, "key": key}, nil)
}

func (c *Client) SetEnvVarScope(ctx context.Context, nodeID, key, scope string) error {
	return c.postJSON(ctx, "/env/scope", map[string]string{"nodeId": nodeID, "key": key, "scope": scope}, nil)
}

func (c *Client) ImportEnvFile(ctx context.Context, nodeID, path string) (store.EnvFileSyncResult, error) {
	var out store.EnvFileSyncResult
	err := c.postJSON(ctx, "/env/import", map[string]string{"nodeId": nodeID, "path": path}, &out)
	return out, err
}

func (c *Client) RefreshEnvFile(ctx context.Context, nodeID string) (store.EnvFileSyncResult, error) {
	var out store.EnvFileSyncResult
	err := c.postJSON(ctx, "/env/refresh", nodeRequest{NodeID: nodeID}, &out)
	return out, err
}

func (c *Client) ExportEnvFile(ctx context.Context, nodeID string) (store.EnvFileSyncResult, error) {
	var out store.EnvFileSyncResult
	err := c.postJSON(ctx, "/env/export", map[string]any{"nodeId": nodeID}, &out)
	return out, err
}

func (c *Client) PreviewEnvVars(ctx context.Context, nodeID string) (map[string]deploy.EnvPreview, error) {
	var out map[string]deploy.EnvPreview
	err := c.postJSON(ctx, "/env/preview", nodeRequest{NodeID: nodeID}, &out)
	return out, err
}

func (c *Client) ListReferenceTargets(ctx context.Context, nodeID string) ([]deploy.ReferenceTarget, error) {
	var out []deploy.ReferenceTarget
	err := c.postJSON(ctx, "/env/reference-targets", nodeRequest{NodeID: nodeID}, &out)
	return out, err
}

func (c *Client) ListReferenceIssues(ctx context.Context, nodeID string) ([]deploy.ReferenceIssue, error) {
	var out []deploy.ReferenceIssue
	err := c.postJSON(ctx, "/env/reference-issues", nodeRequest{NodeID: nodeID}, &out)
	return out, err
}

func (c *Client) GetEnvironmentConnections(ctx context.Context, environmentID uint) ([]deploy.Connection, error) {
	var out []deploy.Connection
	err := c.get(ctx, fmt.Sprintf("/connections?environmentId=%d", environmentID), &out)
	return out, err
}

// ListManagedVolumes returns Draft-managed Docker volumes, optionally filtered
// by node and/or project. An empty nodeID lists all Draft-managed volumes in
// the (optional) project scope.
func (c *Client) ListManagedVolumes(ctx context.Context, projectID *uint, nodeID string) ([]deploy.ManagedVolume, error) {
	var sb strings.Builder
	sb.WriteString("/volumes?")
	if projectID != nil {
		sb.WriteString(fmt.Sprintf("projectId=%d&", *projectID))
	}
	if nodeID != "" {
		sb.WriteString("nodeId=" + url.QueryEscape(nodeID) + "&")
	}
	var out []deploy.ManagedVolume
	err := c.get(ctx, sb.String(), &out)
	return out, err
}

// ListVolumesOverview returns every Draft-managed volume across all projects,
// enriched with its owning node's label and an orphaned flag. Backs the global
// Volumes tab.
func (c *Client) ListVolumesOverview(ctx context.Context) ([]deploy.VolumeOverview, error) {
	var out []deploy.VolumeOverview
	err := c.get(ctx, "/volumes/overview", &out)
	return out, err
}

// DeleteManagedVolume removes a Draft-managed volume by name. force=true removes
// it even if a container still references it.
func (c *Client) DeleteManagedVolume(ctx context.Context, name string, force bool) error {
	return c.postJSON(ctx, "/volumes/delete", map[string]any{"name": name, "force": force}, nil)
}

// SystemDF returns the daemon-wide disk usage breakdown (images, containers,
// volumes, build cache). Backs the Docker tab's summary bar.
func (c *Client) SystemDF(ctx context.Context) (types.DiskUsage, error) {
	var out types.DiskUsage
	err := c.get(ctx, "/docker/df", &out)
	return out, err
}

// ListContainers returns every container on the daemon, Draft-managed or not.
func (c *Client) ListContainers(ctx context.Context) ([]deploy.ContainerSummary, error) {
	var out []deploy.ContainerSummary
	err := c.get(ctx, "/docker/containers", &out)
	return out, err
}

func (c *Client) StartContainer(ctx context.Context, id string) error {
	return c.postJSON(ctx, "/docker/containers/start", map[string]any{"id": id}, nil)
}

func (c *Client) StopContainer(ctx context.Context, id string) error {
	return c.postJSON(ctx, "/docker/containers/stop", map[string]any{"id": id}, nil)
}

func (c *Client) RestartContainer(ctx context.Context, id string) error {
	return c.postJSON(ctx, "/docker/containers/restart", map[string]any{"id": id}, nil)
}

func (c *Client) RemoveContainer(ctx context.Context, id string, force bool) error {
	return c.postJSON(ctx, "/docker/containers/remove", map[string]any{"id": id, "force": force}, nil)
}

// ListImages returns every image on the daemon.
func (c *Client) ListImages(ctx context.Context) ([]deploy.ImageSummary, error) {
	var out []deploy.ImageSummary
	err := c.get(ctx, "/docker/images", &out)
	return out, err
}

func (c *Client) RemoveImage(ctx context.Context, id string, force bool) error {
	return c.postJSON(ctx, "/docker/images/remove", map[string]any{"id": id, "force": force}, nil)
}

// ListNetworks returns every network on the daemon.
func (c *Client) ListNetworks(ctx context.Context) ([]deploy.NetworkSummary, error) {
	var out []deploy.NetworkSummary
	err := c.get(ctx, "/docker/networks", &out)
	return out, err
}

func (c *Client) RemoveNetwork(ctx context.Context, id string) error {
	return c.postJSON(ctx, "/docker/networks/remove", map[string]any{"id": id}, nil)
}

// ListAllVolumes returns every volume on the daemon, Draft-managed or not —
// the unrestricted counterpart to ListVolumesOverview.
func (c *Client) ListAllVolumes(ctx context.Context) ([]deploy.VolumeOverview, error) {
	var out []deploy.VolumeOverview
	err := c.get(ctx, "/docker/volumes/all", &out)
	return out, err
}

// RemoveVolume removes any Docker volume by name, unlike DeleteManagedVolume
// which only allows removal of Draft-managed volumes.
func (c *Client) RemoveVolume(ctx context.Context, name string, force bool) error {
	return c.postJSON(ctx, "/docker/volumes/remove", map[string]any{"name": name, "force": force}, nil)
}

// PruneDocker runs a scoped or unscoped prune for one resource type
// ("containers" | "images" | "networks" | "volumes" | "buildcache").
func (c *Client) PruneDocker(ctx context.Context, resource string, draftOnly bool) (deploy.PruneReport, error) {
	var out deploy.PruneReport
	err := c.postJSON(ctx, "/docker/prune", map[string]any{"resource": resource, "draftOnly": draftOnly}, &out)
	return out, err
}

func (c *Client) SubscribeEvents(ctx context.Context, fn func(string, any)) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url("/events"), nil)
	if err != nil {
		return err
	}
	req.Header.Set(tokenHeader, c.state.Token)
	resp, err := (&http.Client{}).Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("event stream returned %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	scanner := bufio.NewScanner(resp.Body)
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var ev struct {
			Name string          `json:"name"`
			Data json.RawMessage `json:"data"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &ev); err != nil {
			continue
		}
		var data any
		if len(ev.Data) > 0 {
			_ = json.Unmarshal(ev.Data, &data)
		}
		fn(ev.Name, data)
	}
	return scanner.Err()
}

func (c *Client) postNode(ctx context.Context, path, nodeID string) error {
	body, _ := json.Marshal(nodeRequest{NodeID: nodeID})
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(tokenHeader, c.state.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(msg)))
	}
	return nil
}

func (c *Client) postJSON(ctx context.Context, path string, in any, out any) error {
	return c.postJSONWithClient(ctx, path, in, out, c.http)
}

// postJSONWithTimeout is for user-mediated system operations, such as a
// macOS administrator sheet. Most daemon calls intentionally retain the
// normal 30-second client timeout.
func (c *Client) postJSONWithTimeout(ctx context.Context, path string, in any, out any, timeout time.Duration) error {
	client := *c.http
	client.Timeout = timeout
	return c.postJSONWithClient(ctx, path, in, out, &client)
}

func (c *Client) postJSONWithClient(ctx context.Context, path string, in any, out any, client *http.Client) error {
	body, _ := json.Marshal(in)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url(path), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set(tokenHeader, c.state.Token)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(msg)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, c.url(path), nil)
	if err != nil {
		return err
	}
	req.Header.Set(tokenHeader, c.state.Token)
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		msg, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s", strings.TrimSpace(string(msg)))
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

func (c *Client) url(path string) string {
	return "http://" + c.state.Addr + path
}

func (c *Client) ListAppSecrets(ctx context.Context) ([]store.AppSecret, error) {
	var out []store.AppSecret
	err := c.postJSON(ctx, "/secrets", map[string]any{}, &out)
	return out, err
}

func (c *Client) SetAppSecret(ctx context.Context, key, value, description string) error {
	return c.postJSON(ctx, "/secrets/set", map[string]any{
		"key": key, "value": value, "description": description,
	}, nil)
}

func (c *Client) DeleteAppSecret(ctx context.Context, key string) error {
	return c.postJSON(ctx, "/secrets/delete", map[string]string{"key": key}, nil)
}

func (c *Client) ListAppSecretUsages(ctx context.Context, key string) ([]deploy.SecretUsage, error) {
	var out []deploy.SecretUsage
	err := c.postJSON(ctx, "/secrets/usages", map[string]string{"key": key}, &out)
	return out, err
}

func launchDaemon() error {
	exe, err := os.Executable()
	if err != nil {
		return err
	}
	cfgDir, err := ConfigDir()
	if err != nil {
		return err
	}
	logFile, err := os.OpenFile(filepath.Join(cfgDir, "daemon.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	cmd := executil.Command(exe, "--daemon")
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := cmd.Start(); err != nil {
		_ = logFile.Close()
		return err
	}
	if err := cmd.Process.Release(); err != nil {
		_ = logFile.Close()
		return err
	}
	return logFile.Close()
}
