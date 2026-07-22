//go:build windows

package appupdate

import (
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"syscall"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	seeMaskNoCloseProcess = 0x00000040
	swHide                = 0
)

var (
	shell32              = windows.NewLazySystemDLL("shell32.dll")
	procShellExecuteExW  = shell32.NewProc("ShellExecuteExW")
	errElevationRequired = syscall.Errno(windows.ERROR_ELEVATION_REQUIRED)
	errCancelled         = syscall.Errno(windows.ERROR_CANCELLED)
)

type shellExecuteInfo struct {
	CbSize     uint32
	FMask      uint32
	Wnd        uintptr
	Verb       *uint16
	File       *uint16
	Parameters *uint16
	Directory  *uint16
	Show       int32
	InstApp    uintptr
	IDList     uintptr
	Class      *uint16
	KeyClass   uintptr
	HotKey     uint32
	Icon       uintptr
	Process    windows.Handle
}

func waitForExit(pid int) error {
	if pid <= 0 {
		return nil
	}
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE, false, uint32(pid))
	if err != nil {
		return nil
	}
	defer syscall.CloseHandle(h)
	result, err := syscall.WaitForSingleObject(h, 120000)
	if err != nil || result == syscall.WAIT_TIMEOUT {
		return fmt.Errorf("timed out waiting for Draft to close")
	}
	return nil
}

func relaunchApp(appPath string) error {
	return exec.Command(appPath).Start()
}

func apply(job Job, logf func(string, ...any)) error {
	if err := waitForExit(job.AppPID); err != nil {
		return err
	}
	logf("app pid %d exited", job.AppPID)
	if err := waitForExit(job.DaemonPID); err != nil {
		return err
	}
	if job.DaemonPID > 0 {
		logf("daemon pid %d exited", job.DaemonPID)
	}
	installDir := filepath.Dir(job.AppPath)
	logf("running installer %s /S /D=%s", job.Artifact, installDir)
	if err := runInstaller(job.Artifact, installDir, logf); err != nil {
		return err
	}
	logf("relaunching %s", job.AppPath)
	return relaunch(job.AppPath)
}

// runInstaller tries a normal CreateProcess first (works for per-user /
// asInvoker payloads and tests). Draft's shipped NSIS installer requires
// admin, which fails with ERROR_ELEVATION_REQUIRED — then we ShellExecute
// with "runas" so UAC can elevate and we wait for the elevated process.
func runInstaller(artifact, installDir string, logf func(string, ...any)) error {
	// NSIS: /D= must be last and unquoted even when the path has spaces.
	args := "/S /D=" + installDir
	cmd := exec.Command(artifact, "/S", "/D="+installDir)
	out, err := cmd.CombinedOutput()
	if err == nil {
		if len(out) > 0 {
			logf("installer output: %s", out)
		}
		return nil
	}
	if !isElevationRequired(err) {
		return fmt.Errorf("installer failed: %w: %s", err, out)
	}
	logf("installer requires elevation; prompting via UAC")
	return runElevatedInstaller(artifact, args)
}

func isElevationRequired(err error) bool {
	for err != nil {
		if errors.Is(err, errElevationRequired) {
			return true
		}
		if errno, ok := err.(syscall.Errno); ok && errno == errElevationRequired {
			return true
		}
		err = errors.Unwrap(err)
	}
	return false
}

func runElevatedInstaller(artifact, parameters string) error {
	verb, err := syscall.UTF16PtrFromString("runas")
	if err != nil {
		return err
	}
	file, err := syscall.UTF16PtrFromString(artifact)
	if err != nil {
		return err
	}
	params, err := syscall.UTF16PtrFromString(parameters)
	if err != nil {
		return err
	}
	info := shellExecuteInfo{
		FMask:      seeMaskNoCloseProcess,
		Verb:       verb,
		File:       file,
		Parameters: params,
		Show:       swHide,
	}
	info.CbSize = uint32(unsafe.Sizeof(info))
	r1, _, callErr := procShellExecuteExW.Call(uintptr(unsafe.Pointer(&info)))
	if r1 == 0 {
		if errors.Is(callErr, errCancelled) {
			return fmt.Errorf("update cancelled: administrator permission was denied")
		}
		return fmt.Errorf("elevate installer: %w", callErr)
	}
	if info.Process == 0 {
		return fmt.Errorf("elevate installer: no process handle returned")
	}
	defer windows.CloseHandle(info.Process)
	if wait, err := windows.WaitForSingleObject(info.Process, windows.INFINITE); err != nil {
		return fmt.Errorf("wait for installer: %w", err)
	} else if wait != windows.WAIT_OBJECT_0 {
		return fmt.Errorf("wait for installer: unexpected result %d", wait)
	}
	var code uint32
	if err := windows.GetExitCodeProcess(info.Process, &code); err != nil {
		return fmt.Errorf("installer exit code: %w", err)
	}
	if code != 0 {
		return fmt.Errorf("installer exited with code %d", code)
	}
	return nil
}
