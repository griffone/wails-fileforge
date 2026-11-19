//go:build !production

package media

// loopbackEnabled reports whether the loopback media server may start. In
// development the webview is served by the frontend dev server, which cannot
// reach the embedded asset middleware, so the fallback listener is required.
func loopbackEnabled() bool { return true }
