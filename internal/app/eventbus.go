package app

import "github.com/wailsapp/wails/v3/pkg/application"

// WailsBus publishes service events through the running Wails application.
// Headless runs and early startup have no application instance, so emission is
// a no-op instead of an error.
type WailsBus struct{}

// Emit forwards one event to the Wails event system.
func (WailsBus) Emit(name string, payload any) {
	app := application.Get()
	if app == nil {
		return
	}

	app.Event.Emit(name, payload)
}
