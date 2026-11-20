package main

import (
	"log"

	myapp "fileforge-desktop/internal/app"
	"fileforge-desktop/internal/media"

	"github.com/wailsapp/wails/v3/pkg/application"
)

func main() {
	notifier := myapp.NewNotifier()
	bundle := myapp.New(notifier)

	app := application.New(application.Options{
		Name:        "FileForge Desktop",
		Description: "Cross-platform file conversion toolkit",
		Services: []application.Service{
			application.NewService(bundle.Lifecycle),
			application.NewService(bundle.Tools),
			application.NewService(bundle.Recipes),
			application.NewService(bundle.Automation),
			application.NewService(bundle.Toolchain),
			application.NewService(bundle.Preview),
			application.NewService(bundle.System),
			application.NewService(bundle.Notifier),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: media.Middleware,
		},
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: true,
		},
	})

	win := app.Window.NewWithOptions(application.WebviewWindowOptions{
		Name:           "main",
		Title:          "FileForge Desktop",
		MinWidth:       1080,
		MinHeight:      720,
		EnableFileDrop: true,
	})
	win.Show()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
