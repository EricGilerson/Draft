package main

import (
	"context"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"Draft/internal/agents"
	"Draft/internal/appupdate"
	"Draft/internal/daemon"
	"Draft/internal/executil"
	"Draft/internal/githooks"
	"Draft/internal/store"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx          context.Context
	eventsDone   context.CancelFunc
	store        *store.Store
	daemon       *daemon.Client
	agentManager *agents.Manager
	updater      *appupdate.Manager
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx
	a.updater = appupdate.New(appVersion, updaterPublicKey)

	// Open the local database (dev-time AutoMigrate). Non-fatal if it fails so
	// the rest of the app still runs; surface the error in the log.
	if path, err := store.DefaultPath(); err != nil {
		fmt.Println("store: resolve path:", err)
	} else if s, err := store.Open(store.FileDSN(path)); err != nil {
		fmt.Println("store: open:", err)
	} else {
		a.store = s
		a.reconcileSavedRepoHooks()
	}

	if a.store != nil {
		if c, err := daemon.Ensure(ctx); err != nil {
			fmt.Println("daemon: start:", err)
		} else {
			a.daemon = c
			a.startDaemonEvents()
		}
	}

	a.agentManager = agents.NewManager(
		func(ev agents.OutputEvent) {
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "agent:session:output", ev)
			}
		},
		func(ev agents.ExitEvent) {
			if a.ctx != nil {
				wruntime.EventsEmit(a.ctx, "agent:session:exit", ev)
			}
		},
	)
	go a.checkForUpdate()
}

func (a *App) checkForUpdate() {
	if a.updater == nil {
		return
	}
	status := a.updater.Check(context.Background())
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "update:status", status)
	}
}

// GetAppVersion returns the running Draft build version (ldflags-stamped on
// release builds; defaults to the local-dev value in version.go).
func (a *App) GetAppVersion() string {
	return appVersion
}

// UpdateStatus returns the current staged-update state.
func (a *App) UpdateStatus() appupdate.Status {
	if a.updater == nil {
		return appupdate.Status{State: "checking"}
	}
	return a.updater.Status()
}

// CheckForUpdate checks the public GitHub Release in the background and
// returns the final status for the Settings "Check now" action.
func (a *App) CheckForUpdate() appupdate.Status {
	if a.updater == nil {
		return appupdate.Status{State: "disabled"}
	}
	status := a.updater.Check(context.Background())
	if a.ctx != nil {
		wruntime.EventsEmit(a.ctx, "update:status", status)
	}
	return status
}

type UpdateRestartResult struct {
	Ready       bool     `json:"ready"`
	ActiveNodes []string `json:"activeNodes,omitempty"`
	Message     string   `json:"message,omitempty"`
}

// RestartToUpdate drains the Draft daemon, then delegates replacement to a
// detached helper. On Windows the helper is a copy of this executable so the
// live .exe can be overwritten; on macOS the in-bundle binary is used (Unix
// can rename the .app while the process keeps running from the old inode).
func (a *App) RestartToUpdate(cancelActive bool) (*UpdateRestartResult, error) {
	if a.updater == nil {
		return nil, fmt.Errorf("updater is unavailable")
	}
	// A release may have appeared after the background download completed. The
	// final check guarantees we never install an already-obsolete payload.
	status := a.CheckForUpdate()
	if status.State != "ready" {
		return nil, fmt.Errorf("the latest update is not ready: %s", status.Message)
	}
	artifact, status, ok := a.updater.ReadyArtifact()
	if !ok {
		return nil, fmt.Errorf("no verified update is ready")
	}
	var daemonPID int
	if a.daemon != nil {
		ready, err := a.daemon.PrepareForUpdate(context.Background(), cancelActive)
		if err != nil {
			return nil, fmt.Errorf("prepare Draft for update: %w", err)
		}
		if !ready.Ready {
			return &UpdateRestartResult{ActiveNodes: ready.ActiveNodes, Message: "A build or deploy is still running."}, nil
		}
		daemonPID = a.daemon.State().PID
	}
	exe, err := os.Executable()
	if err != nil {
		return nil, err
	}
	helper := exe
	if runtime.GOOS == "windows" {
		helper = filepath.Join(a.updater.UpdateDir(), fmt.Sprintf("draft-update-helper-%d.exe", os.Getpid()))
		if err := copyExecutable(exe, helper); err != nil {
			return nil, err
		}
	}
	appPath := exe
	if runtime.GOOS == "darwin" {
		appPath = filepath.Clean(filepath.Join(exe, "..", "..", ".."))
	}
	updateDir := a.updater.UpdateDir()
	jobPath, err := appupdate.WriteJob(updateDir, appupdate.Job{AppPID: os.Getpid(), DaemonPID: daemonPID, Artifact: artifact, AppPath: appPath})
	if err != nil {
		return nil, err
	}
	logFile, err := os.OpenFile(filepath.Join(updateDir, "apply.log"), os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return nil, err
	}
	cmd := executil.Command(helper, "--apply-update", jobPath)
	cmd.Stdout = logFile
	cmd.Stderr = logFile
	if err := executil.StartDetached(cmd); err != nil {
		_ = logFile.Close()
		return nil, err
	}
	_ = logFile.Close()
	wruntime.Quit(a.ctx)
	return &UpdateRestartResult{Ready: true, Message: "Restarting to install Draft " + status.Version}, nil
}

func copyExecutable(source, destination string) error {
	in, err := os.Open(source)
	if err != nil {
		return err
	}
	defer in.Close()
	out, err := os.OpenFile(destination, os.O_CREATE|os.O_TRUNC|os.O_WRONLY, 0o700)
	if err != nil {
		return err
	}
	_, copyErr := io.Copy(out, in)
	closeErr := out.Close()
	if copyErr != nil {
		return copyErr
	}
	return closeErr
}

// shutdown is called when the app closes; release the database connection.
func (a *App) shutdown(ctx context.Context) {
	if a.eventsDone != nil {
		a.eventsDone()
	}
	if a.agentManager != nil {
		a.agentManager.CloseAll()
	}
	if a.store != nil {
		_ = a.store.Close()
	}
}

func (a *App) startDaemonEvents() {
	if a.eventsDone != nil {
		a.eventsDone()
	}
	ctx, cancel := context.WithCancel(a.ctx)
	a.eventsDone = cancel
	go func() {
		for ctx.Err() == nil {
			if a.daemon == nil {
				return
			}
			err := a.daemon.SubscribeEvents(ctx, func(event string, data any) {
				wruntime.EventsEmit(a.ctx, event, data)
			})
			if ctx.Err() != nil {
				return
			}
			if err != nil {
				fmt.Println("daemon events:", err)
			}
			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Second):
			}
		}
	}()
}

func (a *App) ensureDaemon() (*daemon.Client, error) {
	if a.daemon != nil {
		if err := a.daemon.Ping(a.ctx); err == nil && a.daemon.IsCompatible() {
			return a.daemon, nil
		}
	}
	c, err := daemon.Ensure(a.ctx)
	if err != nil {
		return nil, err
	}
	a.daemon = c
	a.startDaemonEvents()
	return c, nil
}

func (a *App) reconcileSavedRepoHooks() {
	if a.store == nil {
		return
	}
	if err := githooks.ReconcileAllHooks(a.ctx, a.store); err != nil {
		fmt.Println("githooks:", err)
	}
}

// SelectFolder opens a native directory-picker dialog and returns the chosen path.
func (a *App) SelectFolder() (string, error) {
	return a.selectFolder("")
}

func (a *App) selectFolder(defaultDir string) (string, error) {
	return wruntime.OpenDirectoryDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:            "Select folder",
		DefaultDirectory: defaultDir,
	})
}

// SelectFile opens a native file-picker dialog and returns the chosen path.
func (a *App) SelectFile(title string, defaultDir string) (string, error) {
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title:            title,
		DefaultDirectory: defaultDir,
	})
}

// SelectDraftPackFile opens a file picker filtered for .draftpack files.
func (a *App) SelectDraftPackFile() (string, error) {
	return wruntime.OpenFileDialog(a.ctx, wruntime.OpenDialogOptions{
		Title: "Select a Draft pack",
		Filters: []wruntime.FileFilter{
			{DisplayName: "Draft pack (*.draftpack)", Pattern: "*.draftpack"},
			{DisplayName: "JSON (*.json)", Pattern: "*.json"},
			{DisplayName: "All files", Pattern: "*.*"},
		},
	})
}

// SaveDraftPackFile opens a save dialog for a .draftpack file and returns the path.
func (a *App) SaveDraftPackFile(defaultFilename string) (string, error) {
	if strings.TrimSpace(defaultFilename) == "" {
		defaultFilename = "pack.draftpack"
	}
	return wruntime.SaveFileDialog(a.ctx, wruntime.SaveDialogOptions{
		Title:           "Save Draft pack",
		DefaultFilename: defaultFilename,
		Filters: []wruntime.FileFilter{
			{DisplayName: "Draft pack (*.draftpack)", Pattern: "*.draftpack"},
		},
	})
}
