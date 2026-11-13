package main

import (
	"log"

	myapp "fileforge-desktop/internal/app"

	"github.com/wailsapp/wails/v3/pkg/application"
)

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

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:           "main",
		Title:          "FileForge Desktop",
		URL:            "/",
		MinWidth:       1080,
		MinHeight:      720,
		EnableFileDrop: true,
	})
	win.Show()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
