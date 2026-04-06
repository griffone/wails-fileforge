package jobs

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/tools"
)

const (
	StatusQueued         = models.JobStatusQueued
	StatusRunning        = models.JobStatusRunning
	StatusSuccess        = models.JobStatusSuccess
	StatusFailed         = models.JobStatusFailed
	StatusPartialSuccess = models.JobStatusPartialSuccess
	StatusCancelled      = models.JobStatusCancelled
	StatusInterrupted    = models.JobStatusInterrupted
)

type Orchestrator struct {
	registry    *registry.Registry
	concurrency chan struct{}
	onProgress  func(models.JobProgressEventV1)
	storePath   string

	mu   sync.RWMutex
	jobs map[string]*trackedJob
}

func NewOrchestrator(reg *registry.Registry, maxConcurrent int) *Orchestrator {
	return NewOrchestratorWithProgressEmitter(reg, maxConcurrent, nil)
}

func NewOrchestratorWithProgressEmitter(reg *registry.Registry, maxConcurrent int, onProgress func(models.JobProgressEventV1)) *Orchestrator {
	if reg == nil {
		reg = registry.GetGlobalRegistry()
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}

	return &Orchestrator{
		registry:    reg,
		concurrency: make(chan struct{}, maxConcurrent),
		onProgress:  onProgress,
		jobs:        make(map[string]*trackedJob),
	}
}

func (o *Orchestrator) SetPersistencePath(path string) {
	o.mu.Lock()
	o.storePath = path
	o.mu.Unlock()
}

func (o *Orchestrator) RecoverInterruptedJobs() error {
	o.mu.RLock()
	storePath := o.storePath
	o.mu.RUnlock()

	if storePath == "" {
		return nil
	}

	persisted, err := o.loadPersistedJobs(storePath)
	if err != nil {
		return err
	}

	if len(persisted) == 0 {
		return nil
	}

	now := time.Now().UnixMilli()

	o.mu.Lock()
	for _, result := range persisted {
		if result.Status != StatusQueued && result.Status != StatusRunning {
			continue
		}

		result.Success = false
		result.Status = StatusInterrupted
		result.Message = "job interrupted after restart"
		result.EndedAt = now
		result.Progress.Stage = StatusInterrupted
		if result.Progress.Total > 0 && result.Progress.Current > result.Progress.Total {
			result.Progress.Current = result.Progress.Total
		}
		if result.Progress.Message == "" {
			result.Progress.Message = "interrupted"
		}

		o.jobs[result.JobID] = &trackedJob{result: result}
	}
	o.mu.Unlock()

	o.persistJobsSnapshot()
	return nil
}

func (o *Orchestrator) Submit(ctx context.Context, req models.JobRequestV1) (models.RunJobResponseV1, error) {
	tool, err := o.registry.GetToolV2(req.ToolID)
	if err != nil {
		return models.RunJobResponseV1{}, fmt.Errorf("tool lookup failed: %w", err)
	}

	if validationErr := tool.Validate(ctx, req); validationErr != nil {
		return models.RunJobResponseV1{
			Success: false,
			Message: "job validation failed",
			Status:  StatusFailed,
			Error:   normalizeJobError(validationErr),
		}, nil
	}

	jobID := fmt.Sprintf("job_%d", time.Now().UnixNano())
	startedAt := time.Now().UnixMilli()
	jobCtx, cancel := context.WithCancel(ctx)

	tracked := &trackedJob{
		ctx:             jobCtx,
		cancel:          cancel,
		onProgressEvent: o.onProgress,
		onStateChanged:  o.persistJobsSnapshot,
		result: models.JobResultV1{
			JobID:     jobID,
			Success:   false,
			Message:   "job queued",
			ToolID:    req.ToolID,
			Status:    StatusQueued,
			Progress:  models.JobProgressV1{Current: 0, Total: len(req.InputPaths), Stage: StatusQueued, Message: "queued"},
			StartedAt: startedAt,
		},
	}

	o.mu.Lock()
	o.jobs[jobID] = tracked
	o.mu.Unlock()
	o.persistJobsSnapshot()

	go o.run(tracked, tool, req)

	return models.RunJobResponseV1{
		Success: true,
		Message: "job submitted",
		JobID:   jobID,
		Status:  StatusQueued,
	}, nil
}

func (o *Orchestrator) run(job *trackedJob, tool tools.Tool, req models.JobRequestV1) {
	o.concurrency <- struct{}{}
	defer func() { <-o.concurrency }()

	job.updateProgress(models.JobProgressV1{
		Current: 0,
		Total:   len(req.InputPaths),
		Stage:   StatusRunning,
		Message: "running",
	})

	select {
	case <-job.ctx.Done():
		job.complete(StatusCancelled, "job cancelled", nil, models.NewJobError(models.ErrorCodeCancelledByUser, "JOB_CANCELLED", job.ctx.Err().Error(), nil), time.Now().UnixMilli())
		return
	default:
	}

	if req.Mode == "single" {
		o.runSingle(job, tool, req)
		return
	}

	o.runBatch(job, tool, req)
}

func (o *Orchestrator) runSingle(job *trackedJob, tool tools.Tool, req models.JobRequestV1) {
	if execWithProgress, ok := tool.(tools.SingleExecutorWithProgress); ok {
		item, err, attempts := o.executeSingleWithRetry(job, req.ToolID, func() (models.JobResultItemV1, *models.JobErrorV1) {
			return execWithProgress.ExecuteSingleWithProgress(job.ctx, req, func(progress models.JobProgressV1) {
				job.updateProgress(progress)
			})
		})
		item = normalizeItemError(item)
		item.Attempts = attempts
		item.RetryCount = max(0, attempts-1)
		if err != nil {
			job.complete(StatusFailed, "job failed", []models.JobResultItemV1{item}, normalizeJobError(err), time.Now().UnixMilli())
			return
		}

		job.complete(StatusSuccess, "job success", []models.JobResultItemV1{item}, nil, time.Now().UnixMilli())
		return
	}

	exec, ok := tool.(tools.SingleExecutor)
	if !ok {
		job.complete(StatusFailed, "tool does not support single execution", nil, models.NewCanonicalJobError("UNSUPPORTED_MODE", "single execution not supported", nil), time.Now().UnixMilli())
		return
	}

	item, err, attempts := o.executeSingleWithRetry(job, req.ToolID, func() (models.JobResultItemV1, *models.JobErrorV1) {
		return exec.ExecuteSingle(job.ctx, req)
	})
	item = normalizeItemError(item)
	item.Attempts = attempts
	item.RetryCount = max(0, attempts-1)
	if err != nil {
		job.complete(StatusFailed, "job failed", []models.JobResultItemV1{item}, normalizeJobError(err), time.Now().UnixMilli())
		return
	}

	job.complete(StatusSuccess, "job success", []models.JobResultItemV1{item}, nil, time.Now().UnixMilli())
}

func (o *Orchestrator) runBatch(job *trackedJob, tool tools.Tool, req models.JobRequestV1) {
	exec, ok := tool.(tools.BatchExecutor)
	if !ok {
		job.complete(StatusFailed, "tool does not support batch execution", nil, models.NewCanonicalJobError("UNSUPPORTED_MODE", "batch execution not supported", nil), time.Now().UnixMilli())
		return
	}

	attempts := 0
	var items []models.JobResultItemV1
	var err *models.JobErrorV1

	for attempts < maxRetryAttempts {
		attempts++
		items, err = exec.ExecuteBatch(job.ctx, req, func(progress models.JobProgressV1) {
			job.updateProgress(progress)
		})

		if err == nil || !isRetryableError(req.ToolID, err) || attempts >= maxRetryAttempts {
			break
		}

		if !waitRetryBackoff(job.ctx, attempts) {
			items = normalizeItemsErrors(items)
			items = o.decorateBatchRetryMetadata(items, attempts)
			job.complete(StatusCancelled, "job cancelled", items, models.NewCanonicalJobError("JOB_CANCELLED", job.ctx.Err().Error(), retryMetadata(attempts, err)), time.Now().UnixMilli())
			return
		}
	}

	items = normalizeItemsErrors(items)
	items = o.decorateBatchRetryMetadata(items, attempts)
	if err != nil {
		if job.ctx.Err() != nil {
			job.complete(StatusCancelled, "job cancelled", items, models.NewCanonicalJobError("JOB_CANCELLED", job.ctx.Err().Error(), retryMetadata(attempts, err)), time.Now().UnixMilli())
			return
		}
		status, message := deriveBatchFinalState(items)
		jobErr := normalizeJobError(err)
		if jobErr != nil {
			if jobErr.Details == nil {
				jobErr.Details = map[string]any{}
			}
			for k, v := range retryMetadata(attempts, err) {
				jobErr.Details[k] = v
			}
		}
		job.complete(status, message, items, jobErr, time.Now().UnixMilli())
		return
	}

	status, message := deriveBatchFinalState(items)
	job.complete(status, message, items, nil, time.Now().UnixMilli())
}

func (o *Orchestrator) executeSingleWithRetry(job *trackedJob, toolID string, execute func() (models.JobResultItemV1, *models.JobErrorV1)) (models.JobResultItemV1, *models.JobErrorV1, int) {
	attempts := 0
	var item models.JobResultItemV1
	var jobErr *models.JobErrorV1

	for attempts < maxRetryAttempts {
		attempts++
		item, jobErr = execute()

		if jobErr == nil {
			return item, nil, attempts
		}

		if !isRetryableError(toolID, jobErr) || attempts >= maxRetryAttempts {
			if item.Error == nil {
				item.Error = jobErr
			}
			if item.Error.Details == nil {
				item.Error.Details = map[string]any{}
			}
			for k, v := range retryMetadata(attempts, jobErr) {
				item.Error.Details[k] = v
			}
			return item, jobErr, attempts
		}

		if !waitRetryBackoff(job.ctx, attempts) {
			cancelErr := models.NewCanonicalJobError("JOB_CANCELLED", job.ctx.Err().Error(), retryMetadata(attempts, jobErr))
			if item.Error == nil {
				item.Error = cancelErr
			}
			return item, cancelErr, attempts
		}
	}

	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("EXECUTION_FAILED", "execution failed without explicit error", nil)
	}
	if item.Error == nil {
		item.Error = jobErr
	}
	return item, jobErr, attempts
}

func (o *Orchestrator) decorateBatchRetryMetadata(items []models.JobResultItemV1, attempts int) []models.JobResultItemV1 {
	decorated := make([]models.JobResultItemV1, len(items))
	for i := range items {
		item := items[i]
		item.Attempts = attempts
		item.RetryCount = max(0, attempts-1)
		decorated[i] = item
	}
	return decorated
}

func (o *Orchestrator) GetJob(jobID string) (models.JobResultV1, bool) {
	o.mu.RLock()
	job, ok := o.jobs[jobID]
	o.mu.RUnlock()
	if !ok {
		return models.JobResultV1{}, false
	}

	return job.snapshot(), true
}

func (o *Orchestrator) Cancel(jobID string) error {
	o.mu.RLock()
	job, ok := o.jobs[jobID]
	o.mu.RUnlock()
	if !ok {
		return fmt.Errorf("job '%s' not found", jobID)
	}

	if job.cancel != nil {
		job.cancel()
	}
	return nil
}

func (o *Orchestrator) persistJobsSnapshot() {
	o.mu.RLock()
	storePath := o.storePath
	if storePath == "" {
		o.mu.RUnlock()
		return
	}

	persisted := make([]models.JobResultV1, 0)
	for _, job := range o.jobs {
		snapshot := job.snapshot()
		if snapshot.Status == StatusQueued || snapshot.Status == StatusRunning {
			persisted = append(persisted, snapshot)
		}
	}
	o.mu.RUnlock()

	_ = o.savePersistedJobs(storePath, persisted)
}

func (o *Orchestrator) loadPersistedJobs(path string) ([]models.JobResultV1, error) {
	content, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read persisted jobs: %w", err)
	}

	if len(content) == 0 {
		return nil, nil
	}

	jobs := make([]models.JobResultV1, 0)
	if err := json.Unmarshal(content, &jobs); err != nil {
		return nil, fmt.Errorf("decode persisted jobs: %w", err)
	}

	return jobs, nil
}

func (o *Orchestrator) savePersistedJobs(path string, persisted []models.JobResultV1) error {
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return fmt.Errorf("create persistence directory: %w", err)
	}

	payload, err := json.MarshalIndent(persisted, "", "  ")
	if err != nil {
		return fmt.Errorf("encode persisted jobs: %w", err)
	}

	if err := os.WriteFile(path, payload, 0o644); err != nil {
		return fmt.Errorf("write persisted jobs: %w", err)
	}

	return nil
}

func (o *Orchestrator) Validate(ctx context.Context, req models.JobRequestV1) models.ValidateJobResponseV1 {
	tool, err := o.registry.GetToolV2(req.ToolID)
	if err != nil {
		return models.ValidateJobResponseV1{
			Success: false,
			Message: "tool not found",
			Valid:   false,
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", err.Error(), nil),
		}
	}

	if validationErr := tool.Validate(ctx, req); validationErr != nil {
		return models.ValidateJobResponseV1{
			Success: false,
			Message: "validation failed",
			Valid:   false,
			Error:   normalizeJobError(validationErr),
		}
	}

	return models.ValidateJobResponseV1{
		Success: true,
		Message: "job request is valid",
		Valid:   true,
	}
}

func normalizeJobError(jobErr *models.JobErrorV1) *models.JobErrorV1 {
	if jobErr == nil {
		return nil
	}

	details := jobErr.Details
	if len(details) > 0 {
		copied := make(map[string]any, len(details))
		for k, v := range details {
			copied[k] = v
		}
		details = copied
	}

	detailCode := jobErr.DetailCode
	if detailCode == "" {
		detailCode = jobErr.Code
	}

	return models.NewCanonicalJobError(detailCode, jobErr.Message, details)
}

func normalizeItemError(item models.JobResultItemV1) models.JobResultItemV1 {
	if item.Error != nil {
		item.Error = normalizeJobError(item.Error)
	}
	return item
}

func normalizeItemsErrors(items []models.JobResultItemV1) []models.JobResultItemV1 {
	normalized := make([]models.JobResultItemV1, len(items))
	for i := range items {
		normalized[i] = normalizeItemError(items[i])
	}
	return normalized
}

func deriveBatchFinalState(items []models.JobResultItemV1) (string, string) {
	if len(items) == 0 {
		return StatusFailed, "job failed"
	}

	successCount := 0
	for _, item := range items {
		if item.Success {
			successCount++
		}
	}

	switch {
	case successCount == len(items):
		return StatusSuccess, "job success"
	case successCount == 0:
		return StatusFailed, "job failed"
	default:
		return StatusPartialSuccess, "job partial success"
	}
}
