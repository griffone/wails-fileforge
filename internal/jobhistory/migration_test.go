package jobhistory

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
)

func TestNewStoreMigratesLegacyHistoryFile(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, legacyHistoryFileName)
	currentPath := filepath.Join(dir, historyFileName)
	writeHistoryFile(t, legacyPath, "job_legacy")

	store := NewStore(currentPath)

	entries := store.List()
	if len(entries) != 1 || entries[0].JobID != "job_legacy" {
		t.Fatalf("expected the legacy entry to be loaded, got %+v", entries)
	}
	if _, err := os.Stat(currentPath); err != nil {
		t.Fatalf("expected the migrated file to exist: %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("expected the legacy file to be preserved: %v", err)
	}
}

func TestNewStoreKeepsExistingHistoryFile(t *testing.T) {
	dir := t.TempDir()
	legacyPath := filepath.Join(dir, legacyHistoryFileName)
	currentPath := filepath.Join(dir, historyFileName)
	writeHistoryFile(t, legacyPath, "job_legacy")
	original := writeHistoryFile(t, currentPath, "job_current")

	store := NewStore(currentPath)

	entries := store.List()
	if len(entries) != 1 || entries[0].JobID != "job_current" {
		t.Fatalf("expected the current entry to win, got %+v", entries)
	}
	content, err := os.ReadFile(currentPath)
	if err != nil {
		t.Fatalf("read current file: %v", err)
	}
	if !bytes.Equal(content, original) {
		t.Fatalf("expected the current file to stay untouched, got %q", content)
	}
}

func TestNewStoreWithoutAnyHistoryFileIsNoOp(t *testing.T) {
	dir := t.TempDir()
	currentPath := filepath.Join(dir, historyFileName)

	store := NewStore(currentPath)

	if entries := store.List(); len(entries) != 0 {
		t.Fatalf("expected an empty history, got %+v", entries)
	}
	if _, err := os.Stat(currentPath); !os.IsNotExist(err) {
		t.Fatalf("expected no history file to be created, got err=%v", err)
	}
}

func writeHistoryFile(t *testing.T, path, jobID string) []byte {
	t.Helper()

	entries := []models.JobHistoryEntry{{
		JobID:     jobID,
		ToolID:    "tool.image.convert",
		CreatedAt: 1700000000,
		Outputs:   []models.JobOutputRecord{},
	}}
	payload, err := json.Marshal(entries)
	if err != nil {
		t.Fatalf("encode history: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write history: %v", err)
	}

	return payload
}
