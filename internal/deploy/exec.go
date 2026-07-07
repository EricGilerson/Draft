package deploy

import (
	"context"
	"fmt"
	"io"
	"net"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/client"
)

// ExecSession is an interactive exec attached to a running service container.
// Conn is the bidirectional TTY stream: bytes written to it become the
// process's stdin, bytes read from it are merged stdout/stderr. Close tears
// down the exec attach and the Docker client.
type ExecSession struct {
	Conn   net.Conn
	resize func(ctx context.Context, cols, rows uint) error
	close  func() error
}

func (s *ExecSession) Close() error {
	if s.close != nil {
		return s.close()
	}
	return s.Conn.Close()
}

// Resize adjusts the exec's pty dimensions to match the client terminal.
func (s *ExecSession) Resize(ctx context.Context, cols, rows uint) error {
	if s.resize == nil {
		return nil
	}
	return s.resize(ctx, cols, rows)
}

// RunCommandResult is the one-shot exec response: captured combined output and
// the process exit code. Error is set only when the exec itself could not be
// created/attached, not for a non-zero process exit.
type RunCommandResult struct {
	ExitCode int    `json:"exitCode"`
	Output   string `json:"output"`
	Error    string `json:"error,omitempty"`
}

// RunCommand executes a one-shot, non-interactive command in the active
// container for nodeID and returns the captured stdout/stderr plus exit code.
// Used by the per-service "Run" bar (e.g. `npm run migrate`, `rails db:seed`).
func (e *Engine) RunCommand(ctx context.Context, nodeID string, cmd []string, workDir string) (RunCommandResult, error) {
	dep, err := e.store.ActiveDeployment(nodeID)
	if err != nil {
		return RunCommandResult{}, err
	}
	if dep == nil || dep.ContainerID == "" {
		return RunCommandResult{}, fmt.Errorf("service is not running")
	}
	if len(cmd) == 0 {
		return RunCommandResult{}, fmt.Errorf("cmd is empty")
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return RunCommandResult{}, err
	}
	defer cli.Close()

	opts := container.ExecOptions{
		Cmd:          cmd,
		AttachStdout: true,
		AttachStderr: true,
		AttachStdin:  false,
		Tty:          false,
	}
	if workDir != "" {
		opts.WorkingDir = workDir
	}
	createResp, err := cli.ContainerExecCreate(ctx, dep.ContainerID, opts)
	if err != nil {
		return RunCommandResult{}, fmt.Errorf("exec create: %w", err)
	}

	hijack, err := cli.ContainerExecAttach(ctx, createResp.ID, container.ExecAttachOptions{})
	if err != nil {
		return RunCommandResult{}, fmt.Errorf("exec attach: %w", err)
	}
	defer hijack.Close()

	// With Tty=false the hijacked stream is multiplexed (stdout/stderr frames).
	// stdcopy demultiplexes into a single combined buffer for display.
	out, err := io.ReadAll(hijack.Reader)
	if err != nil {
		return RunCommandResult{}, fmt.Errorf("exec read: %w", err)
	}

	inspect, err := cli.ContainerExecInspect(ctx, createResp.ID)
	if err != nil {
		return RunCommandResult{Output: string(out)}, err
	}
	return RunCommandResult{ExitCode: inspect.ExitCode, Output: string(out)}, nil
}

// ExecAttach creates an interactive exec instance running `shell` inside the
// active container for nodeID and attaches a TTY. The caller pumps bytes
// between ExecSession.Conn and its own transport (a WebSocket in the daemon).
// Closing the session ends the exec; the shell exits when its stdin closes.
func (e *Engine) ExecAttach(ctx context.Context, nodeID, shell string) (*ExecSession, error) {
	if shell == "" {
		shell = "sh"
	}
	dep, err := e.store.ActiveDeployment(nodeID)
	if err != nil {
		return nil, err
	}
	if dep == nil || dep.ContainerID == "" {
		return nil, fmt.Errorf("service is not running")
	}

	cli, err := client.NewClientWithOpts(client.FromEnv, client.WithAPIVersionNegotiation())
	if err != nil {
		return nil, err
	}

	createResp, err := cli.ContainerExecCreate(ctx, dep.ContainerID, container.ExecOptions{
		Cmd:          []string{shell},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	})
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("exec create: %w", err)
	}

	hijack, err := cli.ContainerExecAttach(ctx, createResp.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		cli.Close()
		return nil, fmt.Errorf("exec attach: %w", err)
	}

	return &ExecSession{
		Conn: hijack.Conn,
		resize: func(ctx context.Context, cols, rows uint) error {
			return cli.ContainerExecResize(ctx, createResp.ID, container.ResizeOptions{Width: cols, Height: rows})
		},
		close: func() error {
			hijack.Close()
			cli.Close()
			return nil
		},
	}, nil
}
