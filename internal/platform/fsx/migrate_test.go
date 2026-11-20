package fsx

import (
	"os"
	"path/filepath"
	"testing"
)

func TestMigrateFileCopiesLegacyFileAndKeepsIt(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "store_v1.json")
	newPath := filepath.Join(dir, "store.json")

	if err := os.WriteFile(oldPath, []byte("legacy"), 0o600); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}

	if err := MigrateFile(oldPath, newPath, 0o644); err != nil {
		t.Fatalf("migrate file: %v", err)
	}

	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read migrated file: %v", err)
	}
	if string(content) != "legacy" {
		t.Fatalf("migrated content = %q, want %q", content, "legacy")
	}
	if _, err := os.Stat(oldPath); err != nil {
		t.Fatalf("legacy file must be preserved: %v", err)
	}

	info, err := os.Stat(newPath)
	if err != nil {
		t.Fatalf("stat migrated file: %v", err)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("migrated mode = %o, want 644", info.Mode().Perm())
	}
}

func TestMigrateFileLeavesExistingDestinationUntouched(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "store_v1.json")
	newPath := filepath.Join(dir, "store.json")

	if err := os.WriteFile(oldPath, []byte("legacy"), 0o644); err != nil {
		t.Fatalf("write legacy file: %v", err)
	}
	if err := os.WriteFile(newPath, []byte("current"), 0o644); err != nil {
		t.Fatalf("write current file: %v", err)
	}

	if err := MigrateFile(oldPath, newPath, 0o644); err != nil {
		t.Fatalf("migrate file: %v", err)
	}

	content, err := os.ReadFile(newPath)
	if err != nil {
		t.Fatalf("read current file: %v", err)
	}
	if string(content) != "current" {
		t.Fatalf("destination content = %q, want %q", content, "current")
	}
}

func TestMigrateFileNoOpWhenNeitherExists(t *testing.T) {
	dir := t.TempDir()
	oldPath := filepath.Join(dir, "store_v1.json")
	newPath := filepath.Join(dir, "store.json")

	if err := MigrateFile(oldPath, newPath, 0o644); err != nil {
		t.Fatalf("migrate file: %v", err)
	}
	if _, err := os.Stat(newPath); !os.IsNotExist(err) {
		t.Fatalf("expected no destination file, got err=%v", err)
	}
}

func TestMigrateFileSamePathIsNoOp(t *testing.T) {
	path := filepath.Join(t.TempDir(), "store.json")
	if err := os.WriteFile(path, []byte("current"), 0o644); err != nil {
		t.Fatalf("write file: %v", err)
	}

	if err := MigrateFile(path, path, 0o644); err != nil {
		t.Fatalf("migrate file: %v", err)
	}

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read file: %v", err)
	}
	if string(content) != "current" {
		t.Fatalf("content = %q, want %q", content, "current")
	}
}

func TestMigrateFileRejectsEmptyPath(t *testing.T) {
	if err := MigrateFile("", filepath.Join(t.TempDir(), "store.json"), 0o644); err != nil {
		t.Fatalf("empty legacy path must be a no-op: %v", err)
	}
}
