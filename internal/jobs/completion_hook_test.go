package jobs

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

type fakeSingleFileTool struct {
	id         string
	outputPath string
	fail       bool
	waitFor    <-chan struct{}
}

func (t *fakeSingleFileTool) ID() string {
	return t.id
}

func (t *fakeSingleFileTool) Capability() string {
	return t.id
}

func (t *fakeSingleFileTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: t.id}
}

func (t *fakeSingleFileTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *fakeSingleFileTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func (t *fakeSingleFileTool) ExecuteSingle(_ context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	if t.waitFor != nil {
		<-t.waitFor
	}

	inputPath := ""
	if len(req.InputPaths) > 0 {
		inputPath = req.InputPaths[0]
	}
	if t.fail {
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: "fake failure"},
			models.NewCanonicalJobError("FAKE_EXECUTION_FAILED", "fake failure", nil)
	}

	if err := os.MkdirAll(filepath.Dir(t.outputPath), 0o755); err != nil {
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: err.Error()},
			models.NewCanonicalJobError("FAKE_WRITE_FAILED", err.Error(), nil)
	}
	if err := os.WriteFile(t.outputPath, []byte("converted"), 0o644); err != nil {
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: err.Error()},
			models.NewCanonicalJobError("FAKE_WRITE_FAILED", err.Error(), nil)
	}

	return models.JobResultItemV1{
		InputPath:  inputPath,
		OutputPath: t.outputPath,
		Outputs:    []string{t.outputPath},
		Success:    true,
		Message:    "fake conversion complete",
	}, nil
}

func newHookTestOrchestrator(t *testing.T, tool *fakeSingleFileTool) *Orchestrator {
	t.Helper()

	reg := registry.NewRegistry()
	if err := reg.RegisterToolV2(tool); err != nil {
		t.Fatalf("register tool: %v", err)
	}

	return NewOrchestrator(reg, 1)
}

func submitHookTestJob(t *testing.T, orchestrator *Orchestrator, toolID string) models.RunJobResponseV1 {
	t.Helper()

	response, err := orchestrator.Submit(context.Background(), models.JobRequestV1{
		ToolID:     toolID,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-hook-input.pdf"},
	})
	if err != nil {
		t.Fatalf("submit job: %v", err)
	}
	if !response.Success {
		t.Fatalf("expected the job to be submitted, got %+v", response)
	}

	return response
}

func TestOrchestratorCallsCompletionHookOnceForSuccess(t *testing.T) {
	tool := &fakeSingleFileTool{
		id:         "tool.fake.hook.success",
		outputPath: filepath.Join(t.TempDir(), "output.pdf"),
	}
	orchestrator := newHookTestOrchestrator(t, tool)

	completions := make(chan models.JobResultV1, 4)
	orchestrator.SetOnJobComplete(func(result models.JobResultV1) {
		completions <- result
	})

	response := submitHookTestJob(t, orchestrator, tool.id)

	var result models.JobResultV1
	select {
	case result = <-completions:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the completion hook")
	}

	if result.JobID != response.JobID {
		t.Fatalf("expected job id %s, got %s", response.JobID, result.JobID)
	}
	if result.ToolID != tool.id {
		t.Fatalf("expected tool id %s, got %s", tool.id, result.ToolID)
	}
	if result.Status != StatusSuccess {
		t.Fatalf("expected success status, got %s (%s)", result.Status, result.Message)
	}
	if len(result.Items) != 1 || !result.Items[0].Success || result.Items[0].OutputPath != tool.outputPath {
		t.Fatalf("expected the completed item with output %s, got %+v", tool.outputPath, result.Items)
	}

	select {
	case extra := <-completions:
		t.Fatalf("expected exactly one completion callback, got an extra %+v", extra)
	case <-time.After(100 * time.Millisecond):
	}
}

func TestOrchestratorCallsCompletionHookForFailedJob(t *testing.T) {
	tool := &fakeSingleFileTool{
		id:         "tool.fake.hook.failure",
		outputPath: filepath.Join(t.TempDir(), "output.pdf"),
		fail:       true,
	}
	orchestrator := newHookTestOrchestrator(t, tool)

	completions := make(chan models.JobResultV1, 4)
	orchestrator.SetOnJobComplete(func(result models.JobResultV1) {
		completions <- result
	})

	submitHookTestJob(t, orchestrator, tool.id)

	select {
	case result := <-completions:
		if result.Status != StatusFailed {
			t.Fatalf("expected failed status, got %s", result.Status)
		}
		if result.Error == nil {
			t.Fatal("expected the failure to be reported")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the completion hook")
	}
}

func TestSetOnJobCompleteAfterSubmitStillFires(t *testing.T) {
	release := make(chan struct{})
	tool := &fakeSingleFileTool{
		id:         "tool.fake.hook.late",
		outputPath: filepath.Join(t.TempDir(), "output.pdf"),
		waitFor:    release,
	}
	orchestrator := newHookTestOrchestrator(t, tool)

	submitHookTestJob(t, orchestrator, tool.id)

	completions := make(chan models.JobResultV1, 1)
	orchestrator.SetOnJobComplete(func(result models.JobResultV1) {
		completions <- result
	})
	close(release)

	select {
	case result := <-completions:
		if result.Status != StatusSuccess {
			t.Fatalf("expected success status, got %s", result.Status)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the late completion hook")
	}
}

func TestCompletionHookIgnoresRecoveredInterruptedJobs(t *testing.T) {
	dir := t.TempDir()
	storePath := filepath.Join(dir, "jobs_state_v1.json")
	persisted := []models.JobResultV1{{
		JobID:   "job_interrupted",
		ToolID:  "tool.fake.recovery",
		Status:  StatusRunning,
		Message: "running",
	}}
	payload, err := json.Marshal(persisted)
	if err != nil {
		t.Fatalf("encode persisted jobs: %v", err)
	}
	if err := os.WriteFile(storePath, payload, 0o644); err != nil {
		t.Fatalf("write persisted jobs: %v", err)
	}

	orchestrator := NewOrchestrator(registry.NewRegistry(), 1)
	orchestrator.SetPersistencePath(storePath)
	completions := make(chan models.JobResultV1, 1)
	orchestrator.SetOnJobComplete(func(result models.JobResultV1) {
		completions <- result
	})

	if err := orchestrator.RecoverInterruptedJobs(); err != nil {
		t.Fatalf("recover interrupted jobs: %v", err)
	}

	select {
	case result := <-completions:
		t.Fatalf("expected no completion hook for recovery, got %+v", result)
	case <-time.After(100 * time.Millisecond):
	}

	job, found := orchestrator.GetJob("job_interrupted")
	if !found {
		t.Fatal("expected the recovered job to be available")
	}
	if job.Status != StatusInterrupted {
		t.Fatalf("expected interrupted status, got %s", job.Status)
	}
}
