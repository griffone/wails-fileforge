package engine

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func writeGhostscriptStub(t *testing.T, dir, name string) {
	t.Helper()

	if err := os.WriteFile(filepath.Join(dir, name), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write ghostscript stub %s: %v", name, err)
	}
}

func TestResolveGhostscriptPathPrefersGS(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub binaries are Unix executables")
	}

	dir := t.TempDir()
	writeGhostscriptStub(t, dir, "gs")
	writeGhostscriptStub(t, dir, "gswin64c")
	t.Setenv("PATH", dir)

	path, found := ResolveGhostscriptPath()
	if !found {
		t.Fatal("expected a Ghostscript binary")
	}
	if filepath.Base(path) != "gs" {
		t.Fatalf("expected gs to win, got %q", path)
	}
	if !GhostscriptLookPath() {
		t.Fatal("GhostscriptLookPath should follow the resolver")
	}
}

func TestResolveGhostscriptPathFallsBackToWindowsNames(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the stub binaries are Unix executables")
	}

	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gswin32c"), []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write gswin32c stub: %v", err)
	}
	t.Setenv("PATH", dir)

	path, found := ResolveGhostscriptPath()
	if !found {
		t.Fatal("expected the Windows console name to be found")
	}
	if filepath.Base(path) != "gswin32c" {
		t.Fatalf("expected gswin32c, got %q", path)
	}
}

func TestResolveGhostscriptPathMissing(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("PATH isolation differs on Windows")
	}

	t.Setenv("PATH", t.TempDir())

	path, found := ResolveGhostscriptPath()
	if found {
		t.Fatalf("expected no Ghostscript binary, got %q", path)
	}
	if path != "" {
		t.Fatalf("expected an empty path, got %q", path)
	}
	if GhostscriptLookPath() {
		t.Fatal("GhostscriptLookPath should be false without a binary")
	}
}

func TestGhostscriptBinaryNamesOrder(t *testing.T) {
	want := []string{"gs", "gswin64c", "gswin32c"}
	if len(GhostscriptBinaryNames) != len(want) {
		t.Fatalf("unexpected names: %v", GhostscriptBinaryNames)
	}
	for index, name := range want {
		if GhostscriptBinaryNames[index] != name {
			t.Fatalf("name %d = %q, want %q", index, GhostscriptBinaryNames[index], name)
		}
	}
}
