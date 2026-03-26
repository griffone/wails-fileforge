//go:build !production

package main

import "os"

// In development/testing we avoid compile-time embedding of dist assets.
// This keeps `go test ./...` green even when frontend build artifacts are absent.
var assets = os.DirFS("frontend/public")
