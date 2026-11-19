//go:build production

package media

// loopbackEnabled reports whether the loopback media server may start.
// Production builds serve media through the embedded asset server, so no
// loopback listener is opened.
func loopbackEnabled() bool { return false }
