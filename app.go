package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"Draft/internal/daemon"
	"Draft/internal/githooks"
	"Draft/internal/store"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx        context.Context
	eventsDone context.CancelFunc
	store      *store.Store
	daemon     *daemon.Client
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

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
}

// shutdown is called when the app closes; release the database connection.
func (a *App) shutdown(ctx context.Context) {
	if a.eventsDone != nil {
		a.eventsDone()
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
		if err := a.daemon.Ping(a.ctx); err == nil {
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
