//go:build windows

package agents

import (
	"context"
	"fmt"
	"strings"
	"sync"

	"github.com/UserExistsError/conpty"
)

type winPTY struct {
	cpty *conpty.ConPty
	once sync.Once
}

func startPTY(plan *LaunchPlan, cols, rows uint16) (ptySession, error) {
	if !conpty.IsConPtyAvailable() {
		return nil, fmt.Errorf("ConPTY is not available on this Windows version")
	}
	cmdline := windowsCommandLine(plan.Path, plan.Args)
	opts := []conpty.ConPtyOption{
		conpty.ConPtyDimensions(int(cols), int(rows)),
		conpty.ConPtyEnv(plan.Env),
	}
	if plan.Cwd != "" {
		opts = append(opts, conpty.ConPtyWorkDir(plan.Cwd))
	}
	cpty, err := conpty.Start(cmdline, opts...)
	if err != nil {
		return nil, err
	}
	return &winPTY{cpty: cpty}, nil
}

func windowsCommandLine(exe string, args []string) string {
	parts := make([]string, 0, 1+len(args))
	parts = append(parts, windowsQuote(exe))
	for _, a := range args {
		parts = append(parts, windowsQuote(a))
	}
	return strings.Join(parts, " ")
}

// windowsQuote applies Windows argv quoting rules for CreateProcess.
func windowsQuote(s string) string {
	if s == "" {
		return `""`
	}
	if !strings.ContainsAny(s, " \t\"") {
		return s
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c == '"' {
			b.WriteString(`\"`)
			continue
		}
		b.WriteByte(c)
	}
	b.WriteByte('"')
	return b.String()
}

func (w *winPTY) Read(p []byte) (int, error)  { return w.cpty.Read(p) }
func (w *winPTY) Write(p []byte) (int, error) { return w.cpty.Write(p) }

func (w *winPTY) Resize(cols, rows uint16) error {
	return w.cpty.Resize(int(cols), int(rows))
}

func (w *winPTY) Wait() (int, error) {
	code, err := w.cpty.Wait(context.Background())
	return int(code), err
}

func (w *winPTY) Close() error {
	var err error
	w.once.Do(func() {
		err = w.cpty.Close()
	})
	return err
}
