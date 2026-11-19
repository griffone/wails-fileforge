// Package testutil provides small, dependency-free helpers shared by the Go
// test suites: temporary directories, file fixtures, polling and JSON loading.
package testutil

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TempDir returns a test-scoped temporary directory that is removed when the
// test finishes.
func TempDir(t *testing.T) string {
	t.Helper()

	return t.TempDir()
}

// WriteFile writes data to path, creating the parent directories first.
func WriteFile(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("testutil: create parent directory for %s: %v", path, err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("testutil: write %s: %v", path, err)
	}
}

// Eventually polls fn until it reports true or timeout elapses. It fails the
// test with a clear message when the condition is never met.
func Eventually(t *testing.T, timeout, interval time.Duration, fn func() bool) {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for {
		if fn() {
			return
		}
		if time.Now().After(deadline) {
			t.Fatalf("testutil: condition not met within %s", timeout)
		}
		time.Sleep(interval)
	}
}

// ReadJSON reads path and decodes it into target.
func ReadJSON(t *testing.T, path string, target any) {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("testutil: read %s: %v", path, err)
	}
	if err := json.Unmarshal(data, target); err != nil {
		t.Fatalf("testutil: decode %s: %v", path, err)
	}
}
