package dockerdesktop

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"runtime"
	"strings"
)

type commandRunner func(context.Context, string, ...string) error

type starter struct {
	goos string
	run  commandRunner
}

type startAttempt struct {
	name string
	args []string
}

// Start launches the local Docker runtime using the installed Docker CLI when
// possible, with a macOS app-launch fallback for setups that lack the desktop
// subcommand.
func Start(ctx context.Context) error {
	s := starter{
		goos: runtime.GOOS,
		run:  runCommand,
	}
	return s.start(ctx)
}

func (s starter) start(ctx context.Context) error {
	attempts := []startAttempt{
		{name: "docker", args: []string{"desktop", "start"}},
	}
	attempts = append(attempts, s.fallbacks()...)

	var errs []string
	for _, attempt := range attempts {
		if err := s.run(ctx, attempt.name, attempt.args...); err == nil {
			return nil
		} else {
			errs = append(errs, formatAttemptError(attempt, err))
		}
	}

	return fmt.Errorf("start Docker: %s", strings.Join(errs, "; "))
}

func (s starter) fallbacks() []startAttempt {
	switch s.goos {
	case "darwin":
		return []startAttempt{{name: "open", args: []string{"-a", "Docker"}}}
	default:
		return nil
	}
}

func runCommand(ctx context.Context, name string, args ...string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	return cmd.Run()
}

func formatAttemptError(attempt startAttempt, err error) string {
	label := strings.TrimSpace(strings.Join(append([]string{attempt.name}, attempt.args...), " "))
	var exitErr *exec.ExitError
	switch {
	case errors.Is(err, exec.ErrNotFound):
		return fmt.Sprintf("%s not available", label)
	case errors.As(err, &exitErr):
		return fmt.Sprintf("%s exited with %s", label, exitErr.ProcessState.String())
	default:
		return fmt.Sprintf("%s failed: %v", label, err)
	}
}
