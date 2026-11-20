package services

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/jobs"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

func TestMigrateJobsPersistenceCopiesLegacySnapshot(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, jobsFileName)
	legacyPath := filepath.Join(dir, legacyJobsFileName)
	writeJobsSnapshot(t, legacyPath, "job_legacy")

	if err := migrateJobsPersistence(storePath); err != nil {
		t.Fatalf("migrate jobs persistence: %v", err)
	}
	if _, err := os.Stat(storePath); err != nil {
		t.Fatalf("expected the migrated snapshot to exist: %v", err)
	}
	if _, err := os.Stat(legacyPath); err != nil {
		t.Fatalf("expected the legacy snapshot to be preserved: %v", err)
	}

	service := newToolingService(registry.NewRegistry(), jobhistory.NewStore(filepath.Join(dir, "job_history.json")))
	t.Cleanup(func() { _ = service.Shutdown(context.Background()) })

	if err := service.Engine().SetPersistencePath(storePath); err != nil {
		t.Fatalf("set persistence path: %v", err)
	}
	if err := service.Engine().Recover(context.Background()); err != nil {
		t.Fatalf("recover jobs: %v", err)
	}

	job, found := service.Engine().Get("job_legacy")
	if !found {
		t.Fatal("expected the migrated job to be recovered")
	}
	if job.Status != jobs.StatusSuccess || !job.Success {
		t.Fatalf("expected the migrated job to keep its terminal state, got %+v", job)
	}
}

func TestMigrateJobsPersistenceKeepsExistingSnapshot(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, jobsFileName)
	legacyPath := filepath.Join(dir, legacyJobsFileName)
	writeJobsSnapshot(t, legacyPath, "job_legacy")
	original := writeJobsSnapshot(t, storePath, "job_current")

	if err := migrateJobsPersistence(storePath); err != nil {
		t.Fatalf("migrate jobs persistence: %v", err)
	}

	content, err := os.ReadFile(storePath)
	if err != nil {
		t.Fatalf("read current snapshot: %v", err)
	}
	if !bytes.Equal(content, original) {
		t.Fatalf("expected the current snapshot to stay untouched, got %q", content)
	}
}

func TestMigrateJobsPersistenceWithoutFilesIsNoOp(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, jobsFileName)

	if err := migrateJobsPersistence(storePath); err != nil {
		t.Fatalf("migrate jobs persistence: %v", err)
	}
	if _, err := os.Stat(storePath); !os.IsNotExist(err) {
		t.Fatalf("expected no snapshot to be created, got err=%v", err)
	}
}

func writeJobsSnapshot(t *testing.T, path, jobID string) []byte {
	t.Helper()

	persisted := []models.JobResult{{
		JobID:     jobID,
		ToolID:    "tool.fake.migration",
		Success:   true,
		Message:   "job success",
		Status:    jobs.StatusSuccess,
		StartedAt: 100,
		EndedAt:   200,
	}}
	payload, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("encode jobs snapshot: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o644); err != nil {
		t.Fatalf("write jobs snapshot: %v", err)
	}

	return payload
}
