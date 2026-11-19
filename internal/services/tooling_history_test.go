package services

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

type historyFakeTool struct {
	id         string
	outputPath string
	fail       bool
	started    chan<- struct{}
	release    <-chan struct{}
}

func (t *historyFakeTool) ID() string {
	return t.id
}

func (t *historyFakeTool) Capability() string {
	return t.id
}

func (t *historyFakeTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: t.id}
}

func (t *historyFakeTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *historyFakeTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func (t *historyFakeTool) ExecuteSingle(_ context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	if t.started != nil {
		t.started <- struct{}{}
	}
	if t.release != nil {
		<-t.release
	}

	inputPath := ""
	if len(req.InputPaths) > 0 {
		inputPath = req.InputPaths[0]
	}
	if t.fail {
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: "fake failure"},
			models.NewCanonicalJobError("FAKE_SERVICE_FAILED", "fake failure", nil)
	}

	if err := os.MkdirAll(filepath.Dir(t.outputPath), 0o755); err != nil {
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: err.Error()},
			models.NewCanonicalJobError("FAKE_SERVICE_WRITE", err.Error(), nil)
	}
	if err := os.WriteFile(t.outputPath, []byte("converted"), 0o644); err != nil {
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: err.Error()},
			models.NewCanonicalJobError("FAKE_SERVICE_WRITE", err.Error(), nil)
	}

	return models.JobResultItemV1{
		InputPath:  inputPath,
		OutputPath: t.outputPath,
		Outputs:    []string{t.outputPath},
		Success:    true,
		Message:    "fake conversion complete",
	}, nil
}

func newHistoryTestService(t *testing.T, tool *historyFakeTool) (*ToolingService, string) {
	t.Helper()

	dir := t.TempDir()
	reg := registry.NewRegistry()
	if err := reg.RegisterToolV2(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	history := jobhistory.NewStore(filepath.Join(dir, "history", "job_history_v1.json"))
	// No SetContext call: the orchestrator keeps an empty persistence path, so
	// the test never touches the real user config directory.
	return newToolingService(reg, history), dir
}

func waitForHistoryEntries(t *testing.T, service *ToolingService, want int) []models.JobHistoryEntryV1 {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		entries := service.ListJobHistoryV1()
		if len(entries) >= want {
			return entries
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for %d history entries, got %+v", want, service.ListJobHistoryV1())
	return nil
}

func TestToolingServiceRecordsTerminalJobsAndSupportsUndoRestore(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out", "converted.pdf")
	tool := &historyFakeTool{id: "tool.fake.history", outputPath: output}
	service, _ := newHistoryTestService(t, tool)

	if entries := service.ListJobHistoryV1(); entries == nil || len(entries) != 0 {
		t.Fatalf("expected an empty non-nil history, got %+v", entries)
	}

	submit := service.RunJobV1(models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-service-input.pdf"},
	})
	if !submit.Success {
		t.Fatalf("expected the job to be submitted, got %+v", submit)
	}

	entries := waitForHistoryEntries(t, service, 1)
	entry := entries[0]
	if entry.JobID != submit.JobID || entry.ToolID != tool.id {
		t.Fatalf("unexpected history entry: %+v", entry)
	}
	if len(entry.Outputs) != 1 || entry.Outputs[0].Path != output {
		t.Fatalf("expected the job output to be recorded, got %+v", entry.Outputs)
	}

	undo := service.UndoJobV1(entry.JobID)
	if !undo.Success {
		t.Fatalf("expected undo success, got %+v", undo)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("expected the output to leave its original path, got %v", err)
	}
	if entries := service.ListJobHistoryV1(); !entries[0].Undone {
		t.Fatalf("expected the job to be marked undone, got %+v", entries[0])
	}

	restore := service.RestoreJobV1(entry.JobID)
	if !restore.Success {
		t.Fatalf("expected restore success, got %+v", restore)
	}
	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read restored output: %v", err)
	}
	if string(content) != "converted" {
		t.Fatalf("expected restored bytes, got %q", content)
	}

	empty := service.EmptyTrashV1()
	if !empty.Success {
		t.Fatalf("expected empty trash success, got %+v", empty)
	}
}

func TestToolingServiceSkipsHistoryForJobsWithoutOutputs(t *testing.T) {
	tool := &historyFakeTool{
		id:         "tool.fake.history.failure",
		outputPath: filepath.Join(t.TempDir(), "never-written.pdf"),
		fail:       true,
	}
	service, _ := newHistoryTestService(t, tool)

	submit := service.RunJobV1(models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-service-input.pdf"},
	})
	if !submit.Success {
		t.Fatalf("expected the job to be submitted, got %+v", submit)
	}

	status := waitForTerminalStatus(t, service, submit.JobID)
	if status.Status != "failed" {
		t.Fatalf("expected a failed job, got %+v", status)
	}

	if entries := service.ListJobHistoryV1(); len(entries) != 0 {
		t.Fatalf("expected failed jobs without outputs to be ignored, got %+v", entries)
	}
}

func waitForTerminalStatus(t *testing.T, service *ToolingService, jobID string) models.JobResultV1 {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		response := service.GetJobStatusV1(jobID)
		if response.Found && response.Result != nil {
			switch response.Result.Status {
			case models.JobStatusSuccess,
				models.JobStatusPartialSuccess,
				models.JobStatusFailed,
				models.JobStatusCancelled,
				models.JobStatusInterrupted:
				return *response.Result
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for job %s to reach a terminal status", jobID)
	return models.JobResultV1{}
}

func TestToolingServiceEmptyTrashWithoutHistory(t *testing.T) {
	service, _ := newHistoryTestService(t, &historyFakeTool{id: "tool.fake.history.idle"})

	response := service.EmptyTrashV1()
	if !response.Success {
		t.Fatalf("expected empty trash to succeed, got %+v", response)
	}
}
