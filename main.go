package main

import (
	_ "embed"
	"log"

	myapp "fileforge-desktop/internal/app"
	"fileforge-desktop/internal/media"
	"fileforge-desktop/internal/version"

	"github.com/wailsapp/wails/v3/pkg/application"
)

// appIcon is the source image for the window and tray icon. The tray downsizes
// it before handing it to the platform.
//
//go:embed build/appicon.png
var appIcon []byte

func main() {
	log.Printf("fileforge desktop %s", version.String())

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
			application.NewService(bundle.Duplicates),
			application.NewService(bundle.Update),
		},
		Assets: application.AssetOptions{
			Handler:    application.AssetFileServerFS(assets),
			Middleware: media.Middleware,
		},
		// Quitting is explicit: the window close is intercepted so running work
		// survives, and every quit path (tray, app menu, idle close) runs the
		// normal Wails shutdown, which flushes the job and recipe stores.
		Mac: application.MacOptions{
			ApplicationShouldTerminateAfterLastWindowClosed: false,
		},
		Linux: application.LinuxOptions{
			DisableQuitOnLastWindowClosed: true,
		},
		Windows: application.WindowsOptions{
			DisableQuitOnLastWindowClosed: true,
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

	myapp.NewTray(bundle, app, win, appIcon).Install()

	if err := app.Run(); err != nil {
		log.Fatal(err)
	}
}
