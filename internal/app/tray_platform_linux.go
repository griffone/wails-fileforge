//go:build linux

package app

import "github.com/godbus/dbus/v5"

// traySupported reports whether the desktop can host a Wails status notifier
// item. Wails on Linux exports org.kde.StatusNotifierItem and relies on a
// StatusNotifierWatcher host (KDE, or GNOME with the AppIndicator extension).
// Without a watcher the icon is exported but never rendered, so the app must not
// hide its only window there.
func traySupported() (bool, string) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return false, "no D-Bus session bus: " + err.Error()
	}

	var owned bool
	if err := conn.Object("org.freedesktop.DBus", "/org/freedesktop/DBus").
		Call("org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").
		Store(&owned); err != nil {
		return false, "StatusNotifierWatcher probe failed: " + err.Error()
	}
	if !owned {
		return false, "org.kde.StatusNotifierWatcher is not running (install an AppIndicator host)"
	}

	return true, ""
}

// trayLabel is the name a Linux StatusNotifierItem host shows above the tooltip.
// Wails defaults it to "Wails"; other platforms show no label next to the icon.
func trayLabel() string {
	return baseWindowTitle
}
