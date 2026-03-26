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
	StatusQueued    = "queued"
	StatusRunning   = "running"
	StatusCompleted = "completed"
	StatusFailed    = "failed"
	StatusCanceled  = "canceled"
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
			Error:   validationErr,
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
		job.complete(false, StatusCanceled, "job canceled", nil, &models.JobErrorV1{Code: "CANCELED", Message: job.ctx.Err().Error()}, time.Now().UnixMilli())
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
		if err != nil {
			job.complete(false, StatusFailed, "job failed", []models.JobResultItemV1{item}, err, time.Now().UnixMilli())
			return
		}

		job.complete(true, StatusCompleted, "job completed", []models.JobResultItemV1{item}, nil, time.Now().UnixMilli())
		return
	}

	exec, ok := tool.(tools.SingleExecutor)
	if !ok {
		job.complete(false, StatusFailed, "tool does not support single execution", nil, &models.JobErrorV1{Code: "UNSUPPORTED_MODE", Message: "single execution not supported"}, time.Now().UnixMilli())
		return
	}

	item, err := exec.ExecuteSingle(job.ctx, req)
	if err != nil {
		job.complete(false, StatusFailed, "job failed", []models.JobResultItemV1{item}, err, time.Now().UnixMilli())
		return
	}

	job.complete(true, StatusCompleted, "job completed", []models.JobResultItemV1{item}, nil, time.Now().UnixMilli())
}

func (o *Orchestrator) runBatch(job *trackedJob, tool tools.Tool, req models.JobRequestV1) {
	exec, ok := tool.(tools.BatchExecutor)
	if !ok {
		job.complete(false, StatusFailed, "tool does not support batch execution", nil, &models.JobErrorV1{Code: "UNSUPPORTED_MODE", Message: "batch execution not supported"}, time.Now().UnixMilli())
		return
	}

	items, err := exec.ExecuteBatch(job.ctx, req, func(progress models.JobProgressV1) {
		job.updateProgress(progress)
	})
	if err != nil {
		if job.ctx.Err() != nil {
			job.complete(false, StatusCanceled, "job canceled", items, &models.JobErrorV1{Code: "CANCELED", Message: job.ctx.Err().Error()}, time.Now().UnixMilli())
			return
		}
		job.complete(false, StatusFailed, "job failed", items, err, time.Now().UnixMilli())
		return
	}

	job.complete(true, StatusCompleted, "job completed", items, nil, time.Now().UnixMilli())
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
			Error:   &models.JobErrorV1{Code: "TOOL_NOT_FOUND", Message: err.Error()},
		}
	}

	if validationErr := tool.Validate(ctx, req); validationErr != nil {
		return models.ValidateJobResponseV1{
			Success: true,
			Message: "validation failed",
			Valid:   false,
			Error:   validationErr,
		}
	}

	return models.ValidateJobResponseV1{
		Success: true,
		Message: "job request is valid",
		Valid:   true,
	}
}
