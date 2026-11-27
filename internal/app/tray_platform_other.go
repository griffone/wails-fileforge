//go:build !linux

package app

// traySupported reports whether the desktop can host a system tray icon. Windows
// always exposes a notification area and macOS always exposes the menu bar, and
// Wails creates both without a separate host daemon.
func traySupported() (bool, string) {
	return true, ""
}

// trayLabel is the name a Linux StatusNotifierItem host shows above the tooltip.
// macOS keeps the status item icon-only and Windows has no label area, so only
// Linux uses it.
func trayLabel() string {
	return ""
}
