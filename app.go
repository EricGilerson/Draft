package main

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"Draft/internal/deploy"
	"Draft/internal/dockerwatch"
	"Draft/internal/networking"
	"Draft/internal/store"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx    context.Context
	hub    *dockerwatch.Hub
	store  *store.Store
	router *networking.Router
	engine *deploy.Engine
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{hub: dockerwatch.New()}
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
	}

	if a.store != nil {
		a.router = networking.NewRouter(a.store, "127.0.0.1:0")
		if err := a.router.Start(); err != nil {
			fmt.Println("router: start:", err)
		}

		logDir := filepath.Join(os.TempDir(), "draft", "logs")
		if cfgDir, err := os.UserConfigDir(); err == nil {
			logDir = filepath.Join(cfgDir, "Draft", "logs")
		}
		a.engine = deploy.New(a.store, a.router, logDir, func(event string, data any) {
			wruntime.EventsEmit(a.ctx, event, data)
		})
	}

	// Bridge Docker daemon status changes to the frontend over a Wails event.
	// The hub broadcasts only on change, so this emits nothing in steady state.
	a.hub.Subscribe(func(ev dockerwatch.Event) {
		if ev.Kind == "daemon" && ev.Daemon != nil {
			wruntime.EventsEmit(a.ctx, "docker:status", ev.Daemon)
		}
		if ev.Raw != nil {
			wruntime.EventsEmit(a.ctx, "docker:activity", map[string]string{
				"type":   string(ev.Raw.Type),
				"action": string(ev.Raw.Action),
				"actor":  ev.Raw.Actor.ID,
				"name":   ev.Raw.Actor.Attributes["name"],
				"image":  ev.Raw.Actor.Attributes["image"],
			})
		}
	})
	go a.hub.Run(ctx)
}

// shutdown is called when the app closes; release the database connection.
func (a *App) shutdown(ctx context.Context) {
	if a.router != nil {
		_ = a.router.Stop()
	}
	if a.store != nil {
		_ = a.store.Close()
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

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
