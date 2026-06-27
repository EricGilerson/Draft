package main

import (
	"context"
	"fmt"

	"Draft/internal/dockerwatch"
	wruntime "github.com/wailsapp/wails/v2/pkg/runtime"
)

// App struct
type App struct {
	ctx context.Context
	hub *dockerwatch.Hub
}

// NewApp creates a new App application struct
func NewApp() *App {
	return &App{hub: dockerwatch.New()}
}

// startup is called when the app starts. The context is saved
// so we can call the runtime methods
func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Bridge Docker daemon status changes to the frontend over a Wails event.
	// The hub broadcasts only on change, so this emits nothing in steady state.
	a.hub.Subscribe(func(ev dockerwatch.Event) {
		if ev.Kind == "daemon" && ev.Daemon != nil {
			wruntime.EventsEmit(a.ctx, "docker:status", ev.Daemon)
		}
	})
	go a.hub.Run(ctx)
}

// Greet returns a greeting for the given name
func (a *App) Greet(name string) string {
	return fmt.Sprintf("Hello %s, It's show time!", name)
}
