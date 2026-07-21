package deploy

import (
	"bytes"
	"context"
	"fmt"
	"net"

	"github.com/docker/docker/api/types/container"
	"github.com/docker/docker/pkg/stdcopy"
)

// ExecSession is an interactive exec attached to a running service container.
// Conn is the bidirectional TTY stream: bytes written to it become the
// process's stdin, bytes read from it are merged stdout/stderr. Close tears
// down the exec attach (the shared Docker client is left open).
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

const maxRunCommandOutputBytes = 2 << 20 // 2 MiB

type commandOutputWriter struct {
	buf       *bytes.Buffer
	on        func(string)
	maxBytes  int
	truncated bool
}

func (w *commandOutputWriter) Write(p []byte) (int, error) {
	if w.maxBytes > 0 && w.buf.Len() >= w.maxBytes {
		w.truncated = true
		return len(p), nil
	}
	chunk := p
	if w.maxBytes > 0 && w.buf.Len()+len(p) > w.maxBytes {
		chunk = p[:w.maxBytes-w.buf.Len()]
		w.truncated = true
	}
	n, err := w.buf.Write(chunk)
	if n > 0 && w.on != nil {
		w.on(string(chunk[:n]))
	}
	return len(p), err
}

// RunCommand executes a one-shot, non-interactive command in the active
// container for nodeID and returns the captured stdout/stderr plus exit code.
// Used by the per-service "Run" bar (e.g. `npm run migrate`, `rails db:seed`).
func (e *Engine) RunCommand(ctx context.Context, nodeID string, cmd []string, workDir string) (RunCommandResult, error) {
	return e.RunCommandStream(ctx, nodeID, cmd, workDir, nil)
}

// RunCommandStream is RunCommand with incremental decoded stdout/stderr.
// The callback is invoked from Docker's attach reader and must return quickly.
func (e *Engine) RunCommandStream(ctx context.Context, nodeID string, cmd []string, workDir string, onOutput func(string)) (RunCommandResult, error) {
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

	cli, err := e.dockerClient()
	if err != nil {
		return RunCommandResult{}, err
	}

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

	// With Tty=false Docker sends multiplexed stdout/stderr frames. Sending the
	// raw stream to the UI leaks its binary frame headers as replacement glyphs.
	var output bytes.Buffer
	writer := &commandOutputWriter{buf: &output, on: onOutput, maxBytes: maxRunCommandOutputBytes}
	_, err = stdcopy.StdCopy(writer, writer, hijack.Reader)
	if err != nil {
		return RunCommandResult{}, fmt.Errorf("exec read: %w", err)
	}
	out := output.String()
	if writer.truncated {
		out += "\n==> output truncated (size limit)\n"
	}

	inspect, err := cli.ContainerExecInspect(ctx, createResp.ID)
	if err != nil {
		return RunCommandResult{Output: out}, err
	}
	return RunCommandResult{ExitCode: inspect.ExitCode, Output: out}, nil
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

	cli, err := e.dockerClient()
	if err != nil {
		return nil, err
	}

	// bash/zsh need -i for reliable readline/$VAR Tab completion; ash/sh
	// (BusyBox) often reject unknown flags, so leave those as bare binaries.
	cmd := []string{shell}
	switch shell {
	case "bash", "zsh":
		cmd = []string{shell, "-i"}
	}

	createResp, err := cli.ContainerExecCreate(ctx, dep.ContainerID, container.ExecOptions{
		Cmd:          cmd,
		Env:          []string{"TERM=xterm-256color"},
		AttachStdin:  true,
		AttachStdout: true,
		AttachStderr: true,
		Tty:          true,
	})
	if err != nil {
		return nil, fmt.Errorf("exec create: %w", err)
	}

	hijack, err := cli.ContainerExecAttach(ctx, createResp.ID, container.ExecAttachOptions{Tty: true})
	if err != nil {
		return nil, fmt.Errorf("exec attach: %w", err)
	}

	return &ExecSession{
		Conn: hijack.Conn,
		resize: func(ctx context.Context, cols, rows uint) error {
			return cli.ContainerExecResize(ctx, createResp.ID, container.ResizeOptions{Width: cols, Height: rows})
		},
		close: func() error {
			hijack.Close()
			return nil
		},
	}, nil
}
