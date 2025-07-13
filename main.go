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
		Services:    []application.Service{application.NewService(myapp.New())},
		Assets: application.AssetOptions{
			Handler: application.AssetFileServerFS(assets),
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	win := app.Window.New()
	win.SetTitle("FileForge Desktop").
		SetURL("/").
		SetSize(800, 600).
		Show()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
