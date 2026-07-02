package dockerdesktop

import (
	"context"
	"errors"
	"os/exec"
	"reflect"
	"strings"
	"testing"
)

func TestStarterPrefersDockerDesktopCommand(t *testing.T) {
	var calls [][]string
	s := starter{
		goos: "darwin",
		run: func(ctx context.Context, name string, args ...string) error {
			calls = append(calls, append([]string{name}, args...))
			return nil
		},
	}

	if err := s.start(context.Background()); err != nil {
		t.Fatalf("start returned error: %v", err)
	}

	want := [][]string{{"docker", "desktop", "start"}}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestStarterFallsBackToMacOpen(t *testing.T) {
	var calls [][]string
	s := starter{
		goos: "darwin",
		run: func(ctx context.Context, name string, args ...string) error {
			calls = append(calls, append([]string{name}, args...))
			if len(calls) == 1 {
				return exec.ErrNotFound
			}
			return nil
		},
	}

	if err := s.start(context.Background()); err != nil {
		t.Fatalf("start returned error: %v", err)
	}

	want := [][]string{
		{"docker", "desktop", "start"},
		{"open", "-a", "Docker"},
	}
	if !reflect.DeepEqual(calls, want) {
		t.Fatalf("calls = %#v, want %#v", calls, want)
	}
}

func TestStarterReturnsCombinedErrors(t *testing.T) {
	s := starter{
		goos: "darwin",
		run: func(ctx context.Context, name string, args ...string) error {
			return errors.New("boom")
		},
	}

	err := s.start(context.Background())
	if err == nil {
		t.Fatal("expected error")
	}
	msg := err.Error()
	if msg == "" || !containsAll(msg,
		"start Docker:",
		"docker desktop start failed: boom",
		"open -a Docker failed: boom",
	) {
		t.Fatalf("unexpected error: %q", msg)
	}
}

func containsAll(s string, parts ...string) bool {
	for _, part := range parts {
		if !strings.Contains(s, part) {
			return false
		}
	}
	return true
}
