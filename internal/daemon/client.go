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

	"Draft/internal/dockerwatch"
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

func (c *Client) CheckDocker(ctx context.Context) (dockerwatch.DaemonStatus, error) {
	var out dockerwatch.DaemonStatus
	err := c.get(ctx, "/docker", &out)
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
