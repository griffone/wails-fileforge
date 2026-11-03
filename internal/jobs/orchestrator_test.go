package jobs

import (
	"context"
	"fmt"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
)

type fakeBatchTool struct {
	id      string
	calls   [][]string
	execute func(ctx context.Context, req models.JobRequestV1, call int, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1)
}

func (t *fakeBatchTool) ID() string {
	return t.id
}

func (t *fakeBatchTool) Capability() string {
	return t.id
}

func (t *fakeBatchTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: t.id}
}

func (t *fakeBatchTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *fakeBatchTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func (t *fakeBatchTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	t.calls = append(t.calls, append([]string(nil), req.InputPaths...))
	return t.execute(ctx, req, len(t.calls), onProgress)
}

func newBatchTestJob(ctx context.Context, toolID string) *trackedJob {
	return &trackedJob{
		ctx: ctx,
		result: models.JobResultV1{
			JobID:  "job_test",
			ToolID: toolID,
			Status: StatusRunning,
		},
	}
}

func TestRunBatchRetriesOnlyFailedItems(t *testing.T) {
	const toolID = "tool.image.crop"
	inputOne := "/tmp/fileforge/one.png"
	inputTwo := "/tmp/fileforge/two.png"
	inputThree := "/tmp/fileforge/three.png"

	tool := &fakeBatchTool{id: toolID}
	tool.execute = func(_ context.Context, req models.JobRequestV1, call int, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
		items := make([]models.JobResultItemV1, 0, len(req.InputPaths))
		var jobErr *models.JobErrorV1

		for index, inputPath := range req.InputPaths {
			if onProgress != nil {
				onProgress(models.JobProgressV1{Current: index + 1, Total: len(req.InputPaths), Stage: "running", Message: "processed"})
			}

			if call == 1 && inputPath == inputTwo {
				jobErr = models.NewCanonicalJobError("IMAGE_CROP_BATCH_ITEM", "crop failed", map[string]any{"inputPath": inputPath})
				items = append(items, models.JobResultItemV1{
					InputPath: inputPath,
					Success:   false,
					Message:   jobErr.Message,
					Error:     jobErr,
				})
				continue
			}

			items = append(items, models.JobResultItemV1{
				InputPath:  inputPath,
				Outputs:    []string{fmt.Sprintf("%s-out-%d.jpg", inputPath, call)},
				Success:    true,
				Message:    "crop successful",
				OutputPath: fmt.Sprintf("%s-out-%d.jpg", inputPath, call),
			})
		}

		return items, jobErr
	}

	ctx := context.Background()
	job := newBatchTestJob(ctx, toolID)

	var progressEvents []models.JobProgressEventV1
	job.onProgressEvent = func(event models.JobProgressEventV1) {
		progressEvents = append(progressEvents, event)
	}

	orchestrator := NewOrchestrator(registry.NewRegistry(), 1)
	req := models.JobRequestV1{
		ToolID:     toolID,
		Mode:       "batch",
		InputPaths: []string{inputOne, inputTwo, inputThree},
	}

	orchestrator.runBatch(job, tool, req)

	if len(tool.calls) != 2 {
		t.Fatalf("expected exactly 2 ExecuteBatch calls, got %d: %v", len(tool.calls), tool.calls)
	}
	if len(tool.calls[0]) != 3 {
		t.Fatalf("expected first attempt to receive all inputs, got %v", tool.calls[0])
	}
	if len(tool.calls[1]) != 1 || tool.calls[1][0] != inputTwo {
		t.Fatalf("expected retry to receive only %q, got %v", inputTwo, tool.calls[1])
	}

	result := job.snapshot()
	if result.Status != StatusSuccess {
		t.Fatalf("expected status %q, got %q (%s)", StatusSuccess, result.Status, result.Message)
	}
	if result.Error != nil {
		t.Fatalf("expected no job error, got %+v", result.Error)
	}
	if len(result.Items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(result.Items))
	}

	wantAttempts := []int{1, 2, 1}
	wantRetryCounts := []int{0, 1, 0}
	for index := range result.Items {
		item := result.Items[index]
		if !item.Success {
			t.Fatalf("expected item %d to succeed, got %+v", index, item)
		}
		if item.Attempts != wantAttempts[index] {
			t.Fatalf("item %d: expected attempts %d, got %d", index, wantAttempts[index], item.Attempts)
		}
		if item.RetryCount != wantRetryCounts[index] {
			t.Fatalf("item %d: expected retryCount %d, got %d", index, wantRetryCounts[index], item.RetryCount)
		}
	}

	if len(result.Items[0].Outputs) != 1 || result.Items[0].Outputs[0] != inputOne+"-out-1.jpg" {
		t.Fatalf("expected first item to keep its original output, got %v", result.Items[0].Outputs)
	}
	if len(result.Items[2].Outputs) != 1 || result.Items[2].Outputs[0] != inputThree+"-out-1.jpg" {
		t.Fatalf("expected third item to keep its original output, got %v", result.Items[2].Outputs)
	}
	if len(result.Items[1].Outputs) != 1 || result.Items[1].Outputs[0] != inputTwo+"-out-2.jpg" {
		t.Fatalf("expected retried item to expose the retry output, got %v", result.Items[1].Outputs)
	}

	retryingProgress := make([]models.JobProgressV1, 0)
	for _, event := range progressEvents {
		if event.Progress.Stage == "retrying" {
			retryingProgress = append(retryingProgress, event.Progress)
		}
	}
	if len(retryingProgress) == 0 {
		t.Fatal("expected at least one retrying progress event")
	}
	last := retryingProgress[len(retryingProgress)-1]
	if last.Current != 2 {
		t.Fatalf("expected retrying progress current to map to original index 2, got %d", last.Current)
	}
	if last.Total != 3 {
		t.Fatalf("expected retrying progress total 3, got %d", last.Total)
	}
}

func TestRunBatchDoesNotRetryNonRetryableErrors(t *testing.T) {
	const toolID = "tool.image.crop"
	inputOne := "/tmp/fileforge/one.png"
	inputTwo := "/tmp/fileforge/two.png"

	tool := &fakeBatchTool{id: toolID}
	tool.execute = func(_ context.Context, req models.JobRequestV1, _ int, _ func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
		items := make([]models.JobResultItemV1, 0, len(req.InputPaths))
		var jobErr *models.JobErrorV1

		for _, inputPath := range req.InputPaths {
			if inputPath == inputTwo {
				jobErr = models.NewCanonicalJobError("IMAGE_CROP_DIMENSIONS_INVALID", "invalid dimensions", nil)
				items = append(items, models.JobResultItemV1{
					InputPath: inputPath,
					Success:   false,
					Message:   jobErr.Message,
					Error:     jobErr,
				})
				continue
			}

			items = append(items, models.JobResultItemV1{InputPath: inputPath, Success: true, Message: "crop successful"})
		}

		return items, jobErr
	}

	ctx := context.Background()
	job := newBatchTestJob(ctx, toolID)
	orchestrator := NewOrchestrator(registry.NewRegistry(), 1)
	req := models.JobRequestV1{
		ToolID:     toolID,
		Mode:       "batch",
		InputPaths: []string{inputOne, inputTwo},
	}

	orchestrator.runBatch(job, tool, req)

	if len(tool.calls) != 1 {
		t.Fatalf("expected exactly 1 ExecuteBatch call, got %d: %v", len(tool.calls), tool.calls)
	}

	result := job.snapshot()
	if result.Status != StatusPartialSuccess {
		t.Fatalf("expected status %q, got %q", StatusPartialSuccess, result.Status)
	}
	if len(result.Items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(result.Items))
	}
	if result.Items[0].Attempts != 1 || result.Items[1].Attempts != 1 {
		t.Fatalf("expected all items to record a single attempt, got %+v", result.Items)
	}
}

func TestRunBatchStopsRetryingWhenNoItemsReturned(t *testing.T) {
	const toolID = "tool.image.crop"

	tool := &fakeBatchTool{id: toolID}
	tool.execute = func(_ context.Context, _ models.JobRequestV1, _ int, _ func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
		return nil, models.NewCanonicalJobError("IMAGE_CROP_EXECUTION", "crop crashed", nil)
	}

	ctx := context.Background()
	job := newBatchTestJob(ctx, toolID)
	orchestrator := NewOrchestrator(registry.NewRegistry(), 1)
	req := models.JobRequestV1{
		ToolID:     toolID,
		Mode:       "batch",
		InputPaths: []string{"/tmp/fileforge/one.png"},
	}

	orchestrator.runBatch(job, tool, req)

	if len(tool.calls) != 1 {
		t.Fatalf("expected retrying to stop after an empty result, got %d calls", len(tool.calls))
	}

	result := job.snapshot()
	if result.Status != StatusFailed {
		t.Fatalf("expected status %q, got %q", StatusFailed, result.Status)
	}
	if result.Error == nil {
		t.Fatal("expected the execution error to be reported")
	}
}
