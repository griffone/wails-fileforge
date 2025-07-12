package main

import (
	"embed"
	"log"

	myapp "fileforge-desktop/internal/app"

	"github.com/wailsapp/wails/v3/pkg/application"
)

//go:embed all:frontend/dist/frontend/browser
var assets embed.FS

func main() {
	app := application.New(application.Options{
		Name:        "FileForge Desktop",
		Description: "Cross-platform file conversion toolkit",
		Services: []application.Service{
			application.NewService(myapp.New()),
		},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	window := application.NewWindow(application.WebviewWindowOptions{
		Title:       "FileForge Desktop",
		Width:       1200,
		Height:      800,
		URL:         "/",
		MinWidth:    800,
		MinHeight:   600,
		X:           200,
		Y:           200,
		Hidden:      false,
		AlwaysOnTop: false,
		Frameless:   false,
	})

	// Forzar que la ventana sea visible
	window.Show()

	err := app.Run()
	if err != nil {
		log.Fatal(err)
	}
}
