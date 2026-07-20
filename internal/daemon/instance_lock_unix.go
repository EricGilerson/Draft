//go:build unix

package daemon

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
)

const instanceLockName = "daemon.lock"

// acquireInstanceLock takes an exclusive, process-lifetime lock so only one
// Draft daemon can boot at a time. The lock is released automatically when the
// process exits (including Kill), which closes the underlying file descriptor.
// Non-blocking: if another daemon already holds the lock, returns an error.
func acquireInstanceLock() (func(), error) {
	dir, err := ConfigDir()
	if err != nil {
		return nil, err
	}
	path := filepath.Join(dir, instanceLockName)
	f, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open daemon lock: %w", err)
	}
	if err := syscall.Flock(int(f.Fd()), syscall.LOCK_EX|syscall.LOCK_NB); err != nil {
		_ = f.Close()
		return nil, fmt.Errorf("another draft daemon is already running")
	}
	// Best-effort identity for operators inspecting the lock file.
	_, _ = f.Seek(0, 0)
	_ = f.Truncate(0)
	_, _ = fmt.Fprintf(f, "%d\n", os.Getpid())
	_ = f.Sync()

	return func() {
		_ = syscall.Flock(int(f.Fd()), syscall.LOCK_UN)
		_ = f.Close()
	}, nil
}
