package main

import (
	"context"
	"embed"
	"os"

	"Draft/internal/daemon"
	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/mac"
)

//go:embed all:frontend/dist
var assets embed.FS

func main() {
	if len(os.Args) > 1 && os.Args[1] == "--daemon" {
		if err := daemon.RunProcess(context.Background()); err != nil {
			println("Daemon error:", err.Error())
			os.Exit(1)
		}
		return
	}

	// --git-hook is invoked by an installed git hook (post-commit / pre-push).
	// It's a transient, sub-second call: it wakes-or-reaches the daemon and
	// rings its doorbell, then exits. It must never fail the user's git command,
	// so any error is swallowed with exit 0.
	if len(os.Args) > 1 && os.Args[1] == "--git-hook" {
		repo, event := parseGitHookArgs(os.Args[2:])
		if repo != "" && event != "" {
			_ = daemon.FireHook(context.Background(), repo, event)
		}
		return
	}

	// Create an instance of the app structure
	app := NewApp()

	// Create application with options
	err := wails.Run(&options.App{
		Title:            "Draft",
		Width:            1024,
		Height:           768,
		MinWidth:         900,
		MinHeight:        640,
		DisableResize:    false,
		Frameless:        false,
		Fullscreen:       false,
		WindowStartState: options.Normal,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: &options.RGBA{R: 27, G: 38, B: 54, A: 1},
		Mac: &mac.Options{
			DisableZoom: false,
			TitleBar: &mac.TitleBar{
				HideTitleBar:    false,
				HideTitle:       false,
				FullSizeContent: false,
			},
		},
		OnStartup:  app.startup,
		OnShutdown: app.shutdown,
		Bind: []interface{}{
			app,
		},
	})

	if err != nil {
		println("Error:", err.Error())
	}
}

// parseGitHookArgs extracts --repo and --event from the --git-hook arg list.
func parseGitHookArgs(args []string) (repo, event string) {
	for i := 0; i < len(args); i++ {
		switch args[i] {
		case "--repo":
			if i+1 < len(args) {
				repo = args[i+1]
				i++
			}
		case "--event":
			if i+1 < len(args) {
				event = args[i+1]
				i++
			}
		}
	}
	return repo, event
}
