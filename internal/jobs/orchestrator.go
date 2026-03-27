package jobs

import (
	"context"
	"fmt"
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

	mu   sync.RWMutex
	jobs map[string]*trackedJob
}

func NewOrchestrator(reg *registry.Registry, maxConcurrent int) *Orchestrator {
	if reg == nil {
		reg = registry.GetGlobalRegistry()
	}
	if maxConcurrent <= 0 {
		maxConcurrent = 2
	}

	return &Orchestrator{
		registry:    reg,
		concurrency: make(chan struct{}, maxConcurrent),
		jobs:        make(map[string]*trackedJob),
	}
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
		ctx:    jobCtx,
		cancel: cancel,
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
		item, err := execWithProgress.ExecuteSingleWithProgress(job.ctx, req, func(progress models.JobProgressV1) {
			job.updateProgress(progress)
		})
		item = normalizeItemError(item)
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

	item, err := exec.ExecuteSingle(job.ctx, req)
	item = normalizeItemError(item)
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

	items, err := exec.ExecuteBatch(job.ctx, req, func(progress models.JobProgressV1) {
		job.updateProgress(progress)
	})
	items = normalizeItemsErrors(items)
	if err != nil {
		if job.ctx.Err() != nil {
			job.complete(StatusCancelled, "job cancelled", items, models.NewCanonicalJobError("JOB_CANCELLED", job.ctx.Err().Error(), nil), time.Now().UnixMilli())
			return
		}
		status, message := deriveBatchFinalState(items)
		job.complete(status, message, items, normalizeJobError(err), time.Now().UnixMilli())
		return
	}

	status, message := deriveBatchFinalState(items)
	job.complete(status, message, items, nil, time.Now().UnixMilli())
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

	job.cancel()
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
