package jobs

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

func seedTrackedJob(orchestrator *Orchestrator, result models.JobResultV1) {
	orchestrator.jobs[result.JobID] = &trackedJob{result: result}
}

func listJobIDs(jobs []models.JobResultV1) []string {
	ids := make([]string, len(jobs))
	for index, job := range jobs {
		ids[index] = job.JobID
	}

	return ids
}

func assertJobOrder(t *testing.T, jobs []models.JobResultV1, want []string) {
	t.Helper()

	got := listJobIDs(jobs)
	if len(got) != len(want) {
		t.Fatalf("expected %v, got %v", want, got)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("expected %v, got %v", want, got)
		}
	}
}

func TestListJobsSortsNewestFirstAndAppliesLimit(t *testing.T) {
	orchestrator := NewOrchestrator(registry.NewRegistry(), 1)
	seedTrackedJob(orchestrator, models.JobResultV1{JobID: "job_old", StartedAt: 100, Status: StatusSuccess})
	seedTrackedJob(orchestrator, models.JobResultV1{JobID: "job_tie_a", StartedAt: 200, Status: StatusSuccess})
	seedTrackedJob(orchestrator, models.JobResultV1{JobID: "job_tie_b", StartedAt: 200, Status: StatusSuccess})
	seedTrackedJob(orchestrator, models.JobResultV1{JobID: "job_stage", StartedAt: 150, Status: "ocr"})
	seedTrackedJob(orchestrator, models.JobResultV1{JobID: "job_new", StartedAt: 300, Status: StatusRunning})

	t.Run("default limit returns every job newest first", func(t *testing.T) {
		jobs := orchestrator.ListJobs(0)
		assertJobOrder(t, jobs, []string{"job_new", "job_tie_b", "job_tie_a", "job_stage", "job_old"})
	})

	t.Run("positive limit caps the list", func(t *testing.T) {
		jobs := orchestrator.ListJobs(2)
		assertJobOrder(t, jobs, []string{"job_new", "job_tie_b"})
	})
}

func TestListJobsReturnsEmptyNonNilSlice(t *testing.T) {
	orchestrator := NewOrchestrator(registry.NewRegistry(), 1)

	jobs := orchestrator.ListJobs(0)
	if jobs == nil {
		t.Fatal("expected an empty non-nil slice")
	}
	if len(jobs) != 0 {
		t.Fatalf("expected no jobs, got %+v", jobs)
	}
}

func TestTerminalJobRetentionKeepsNewestTerminalJobs(t *testing.T) {
	orchestrator := NewOrchestrator(registry.NewRegistry(), 1)
	for index := 1; index <= retainedTerminalJobs+5; index++ {
		seedTrackedJob(orchestrator, models.JobResultV1{
			JobID:     jobIDForIndex(index),
			StartedAt: int64(index),
			Status:    StatusSuccess,
		})
	}
	seedTrackedJob(orchestrator, models.JobResultV1{JobID: "job_active_old", StartedAt: 1, Status: StatusRunning})
	seedTrackedJob(orchestrator, models.JobResultV1{JobID: "job_active_stage", StartedAt: 2, Status: "processing"})

	orchestrator.pruneTerminalJobs()

	if len(orchestrator.jobs) != retainedTerminalJobs+2 {
		t.Fatalf("expected %d retained jobs, got %d", retainedTerminalJobs+2, len(orchestrator.jobs))
	}
	for index := 1; index <= 5; index++ {
		if _, found := orchestrator.jobs[jobIDForIndex(index)]; found {
			t.Fatalf("expected the oldest terminal job %s to be pruned", jobIDForIndex(index))
		}
	}
	if _, found := orchestrator.jobs[jobIDForIndex(retainedTerminalJobs+5)]; !found {
		t.Fatalf("expected the newest terminal job %s to be retained", jobIDForIndex(retainedTerminalJobs+5))
	}
	for _, jobID := range []string{"job_active_old", "job_active_stage"} {
		if _, found := orchestrator.jobs[jobID]; !found {
			t.Fatalf("expected the non-terminal job %s to survive pruning", jobID)
		}
	}
}

func TestCompletionHookRunsBeforeTerminalPruning(t *testing.T) {
	orchestrator := NewOrchestrator(registry.NewRegistry(), 1)
	for index := 1; index <= retainedTerminalJobs+5; index++ {
		seedTrackedJob(orchestrator, models.JobResultV1{
			JobID:     jobIDForIndex(index),
			StartedAt: int64(index),
			Status:    StatusSuccess,
		})
	}

	tracked := &trackedJob{
		result:         models.JobResultV1{JobID: "job_new", StartedAt: 1000, Status: StatusRunning},
		onStateChanged: orchestrator.persistJobsSnapshot,
		onComplete:     orchestrator.completionCallback(),
	}
	orchestrator.jobs[tracked.result.JobID] = tracked

	hookSawJob := false
	orchestrator.SetOnJobComplete(func(result models.JobResultV1) {
		if _, found := orchestrator.GetJob(result.JobID); !found {
			t.Errorf("expected the completed job %s to be visible to the completion hook", result.JobID)
			return
		}
		hookSawJob = true
	})

	tracked.complete(StatusSuccess, "job success", nil, nil, 1100)

	if !hookSawJob {
		t.Fatal("expected the completion hook to run")
	}
	if _, found := orchestrator.jobs["job_new"]; !found {
		t.Fatal("expected the newest terminal job to be retained")
	}
	if _, found := orchestrator.jobs[jobIDForIndex(1)]; found {
		t.Fatal("expected the oldest terminal job to be pruned after completion")
	}
}

func TestPersistJobsSnapshotKeepsActiveAndNewestTerminalJobs(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "state", "jobs_state_v1.json")

	orchestrator := NewOrchestrator(registry.NewRegistry(), 1)
	orchestrator.SetPersistencePath(storePath)
	for index := 1; index <= retainedTerminalJobs+5; index++ {
		seedTrackedJob(orchestrator, models.JobResultV1{
			JobID:     jobIDForIndex(index),
			StartedAt: int64(index),
			Status:    StatusSuccess,
		})
	}
	seedTrackedJob(orchestrator, models.JobResultV1{JobID: "job_queued", StartedAt: 500, Status: StatusQueued})
	seedTrackedJob(orchestrator, models.JobResultV1{JobID: "job_running", StartedAt: 501, Status: StatusRunning})

	orchestrator.persistJobsSnapshot()

	persisted := readPersistedJobs(t, storePath)
	if len(persisted) != retainedTerminalJobs+2 {
		t.Fatalf("expected %d persisted jobs, got %d", retainedTerminalJobs+2, len(persisted))
	}

	persistedIDs := make(map[string]struct{}, len(persisted))
	for _, job := range persisted {
		persistedIDs[job.JobID] = struct{}{}
	}
	for _, jobID := range []string{"job_queued", "job_running"} {
		if _, found := persistedIDs[jobID]; !found {
			t.Fatalf("expected the active job %s to be persisted", jobID)
		}
	}
	for index := 1; index <= 5; index++ {
		if _, found := persistedIDs[jobIDForIndex(index)]; found {
			t.Fatalf("expected the oldest terminal job %s to be excluded", jobIDForIndex(index))
		}
	}
	if _, found := persistedIDs[jobIDForIndex(retainedTerminalJobs+5)]; !found {
		t.Fatalf("expected the newest terminal job %s to be persisted", jobIDForIndex(retainedTerminalJobs+5))
	}

	info, err := os.Stat(storePath)
	if err != nil {
		t.Fatalf("stat persisted jobs: %v", err)
	}
	if permissions := info.Mode().Perm(); permissions != 0o644 {
		t.Fatalf("expected 0644 permissions, got %o", permissions)
	}

	entries, err := os.ReadDir(filepath.Dir(storePath))
	if err != nil {
		t.Fatalf("read persistence directory: %v", err)
	}
	for _, entry := range entries {
		if entry.Name() != filepath.Base(storePath) {
			t.Fatalf("expected no leftover temp files, found %s", entry.Name())
		}
	}
}

func TestRecoverInterruptedJobsRestoresTerminalAndInterruptsActive(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs_state_v1.json")
	persisted := []models.JobResultV1{
		{
			JobID:     "job_terminal",
			ToolID:    "tool.fake.recovery",
			Success:   true,
			Message:   "job success",
			Status:    StatusSuccess,
			StartedAt: 100,
			EndedAt:   200,
		},
		{
			JobID:     "job_running",
			ToolID:    "tool.fake.recovery",
			Message:   "running",
			Status:    StatusRunning,
			Progress:  models.JobProgressV1{Current: 3, Total: 2, Stage: StatusRunning, Message: "running"},
			StartedAt: 300,
		},
		{
			JobID:     "job_queued",
			ToolID:    "tool.fake.recovery",
			Message:   "job queued",
			Status:    StatusQueued,
			StartedAt: 400,
		},
		{
			JobID:     "job_ocr",
			ToolID:    "tool.fake.recovery",
			Message:   "ocr in progress",
			Status:    "ocr",
			Progress:  models.JobProgressV1{Current: 1, Total: 4, Stage: "ocr"},
			StartedAt: 500,
		},
	}
	payload, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("encode persisted jobs: %v", err)
	}
	if err := os.WriteFile(storePath, payload, 0o644); err != nil {
		t.Fatalf("write persisted jobs: %v", err)
	}

	orchestrator := NewOrchestrator(registry.NewRegistry(), 1)
	orchestrator.SetPersistencePath(storePath)
	if err := orchestrator.RecoverInterruptedJobs(); err != nil {
		t.Fatalf("recover interrupted jobs: %v", err)
	}

	terminal, found := orchestrator.GetJob("job_terminal")
	if !found {
		t.Fatal("expected the terminal job to be restored")
	}
	if terminal.Status != StatusSuccess || !terminal.Success || terminal.Message != "job success" {
		t.Fatalf("expected the terminal job to be restored as-is, got %+v", terminal)
	}
	if terminal.StartedAt != 100 || terminal.EndedAt != 200 {
		t.Fatalf("expected the terminal timestamps to be preserved, got started=%d ended=%d", terminal.StartedAt, terminal.EndedAt)
	}

	for _, jobID := range []string{"job_running", "job_queued", "job_ocr"} {
		job, found := orchestrator.GetJob(jobID)
		if !found {
			t.Fatalf("expected the recovered job %s to be available", jobID)
		}
		if job.Status != StatusInterrupted || job.Success {
			t.Fatalf("expected %s to be interrupted, got %+v", jobID, job)
		}
		if job.Message != "job interrupted after restart" {
			t.Fatalf("expected the interruption message for %s, got %q", jobID, job.Message)
		}
		if job.EndedAt == 0 {
			t.Fatalf("expected an end timestamp for the interrupted job %s", jobID)
		}
		if job.Progress.Stage != StatusInterrupted {
			t.Fatalf("expected interrupted progress stage for %s, got %+v", jobID, job.Progress)
		}
		if job.Progress.Message == "" {
			t.Fatalf("expected a progress message for %s, got %+v", jobID, job.Progress)
		}
		if jobID == "job_ocr" && job.Progress.Message != "interrupted" {
			t.Fatalf("expected the empty ocr progress message to be replaced, got %q", job.Progress.Message)
		}
		if job.Progress.Total > 0 && job.Progress.Current > job.Progress.Total {
			t.Fatalf("expected progress to stay within the total for %s, got %+v", jobID, job.Progress)
		}
	}

	recovered := readPersistedJobs(t, storePath)
	if len(recovered) != len(persisted) {
		t.Fatalf("expected %d persisted jobs after recovery, got %d", len(persisted), len(recovered))
	}
	for _, job := range recovered {
		if !isTerminalJobStatus(job.Status) {
			t.Fatalf("expected only terminal jobs after recovery, got %s (%s)", job.Status, job.JobID)
		}
	}
}

func jobIDForIndex(index int) string {
	return fmt.Sprintf("job_terminal_%02d", index)
}

func readPersistedJobs(t *testing.T, path string) []models.JobResultV1 {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read persisted jobs: %v", err)
	}

	persisted := make([]models.JobResultV1, 0)
	if err := json.Unmarshal(content, &persisted); err != nil {
		t.Fatalf("decode persisted jobs: %v", err)
	}

	return persisted
}
