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
