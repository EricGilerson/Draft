package daemon

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"Draft/internal/deploy"
	"Draft/internal/dockerwatch"
	"Draft/internal/networking"
	"Draft/internal/store"
)

type Client struct {
	state State
	http  *http.Client
}

func Ensure(ctx context.Context) (*Client, error) {
	if c, err := NewClientFromState(); err == nil && c.Ping(ctx) == nil {
		return c, nil
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
				return c, nil
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

func (c *Client) Deploy(ctx context.Context, nodeID string) error {
	return c.postNode(ctx, "/deploy", nodeID)
}

func (c *Client) Stop(ctx context.Context, nodeID string) error {
	return c.postNode(ctx, "/stop", nodeID)
}

func (c *Client) Restart(ctx context.Context, nodeID string) error {
	return c.postNode(ctx, "/restart", nodeID)
}

func (c *Client) StartLogStream(ctx context.Context, nodeID string) error {
	return c.postNode(ctx, "/logs/start", nodeID)
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

func (c *Client) CheckDocker(ctx context.Context) (dockerwatch.DaemonStatus, error) {
	var out dockerwatch.DaemonStatus
	return out, c.get(ctx, "/docker", &out)
}

func (c *Client) LocalDomainStatus(ctx context.Context) (networking.LocalDomainStatus, error) {
	var out networking.LocalDomainStatus
	return out, c.get(ctx, "/local-domain", &out)
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
	err := c.postJSON(ctx, "/env/export", nodeRequest{NodeID: nodeID}, &out)
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

func (c *Client) GetProjectConnections(ctx context.Context, projectID uint) ([]deploy.Connection, error) {
	var out []deploy.Connection
	err := c.get(ctx, fmt.Sprintf("/connections?projectId=%d", projectID), &out)
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

// DeleteManagedVolume removes a Draft-managed volume by name. force=true removes
// it even if a container still references it.
func (c *Client) DeleteManagedVolume(ctx context.Context, name string, force bool) error {
	return c.postJSON(ctx, "/volumes/delete", map[string]any{"name": name, "force": force}, nil)
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
	body, _ := json.Marshal(in)
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
	cmd := exec.Command(exe, "--daemon")
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
