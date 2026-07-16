//go:build unix

package agents

import (
	"os"
	"os/exec"
	"sync"

	"github.com/creack/pty"
)

type unixPTY struct {
	cmd  *exec.Cmd
	file *os.File
	once sync.Once
	wait chan waitResult
}

type waitResult struct {
	code int
	err  error
}

func startPTY(plan *LaunchPlan, cols, rows uint16) (ptySession, error) {
	cmd := exec.Command(plan.Path, plan.Args...)
	cmd.Dir = plan.Cwd
	cmd.Env = plan.Env
	f, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: cols, Rows: rows})
	if err != nil {
		return nil, err
	}
	u := &unixPTY{cmd: cmd, file: f, wait: make(chan waitResult, 1)}
	go func() {
		err := cmd.Wait()
		code := 0
		if err != nil {
			if ee, ok := err.(*exec.ExitError); ok {
				code = ee.ExitCode()
			} else {
				code = 1
			}
		}
		u.wait <- waitResult{code: code, err: err}
	}()
	return u, nil
}

func (u *unixPTY) Read(p []byte) (int, error)  { return u.file.Read(p) }
func (u *unixPTY) Write(p []byte) (int, error) { return u.file.Write(p) }

func (u *unixPTY) Resize(cols, rows uint16) error {
	return pty.Setsize(u.file, &pty.Winsize{Cols: cols, Rows: rows})
}

func (u *unixPTY) Wait() (int, error) {
	r := <-u.wait
	if r.err != nil && r.code == 0 {
		return r.code, r.err
	}
	// Normalize successful exit.
	if r.err != nil {
		if _, ok := r.err.(*exec.ExitError); ok {
			return r.code, nil
		}
		return r.code, r.err
	}
	return r.code, nil
}

func (u *unixPTY) Close() error {
	var err error
	u.once.Do(func() {
		if u.cmd.Process != nil {
			_ = u.cmd.Process.Kill()
		}
		err = u.file.Close()
	})
	return err
}
