package tools

import (
	"context"

	"fileforge-desktop/internal/models"
)

type Tool interface {
	ID() string
	Capability() string
	Manifest() models.ToolManifestV1
	RuntimeState(ctx context.Context) models.ToolRuntimeStateV1
	Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1
}

type SingleExecutor interface {
	ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1)
}

type SingleExecutorWithProgress interface {
	ExecuteSingleWithProgress(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1)
}

type BatchExecutor interface {
	ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1)
}

// Planner is an optional tool capability that resolves the exact output paths
// and effective options for a request without touching the filesystem outputs.
type Planner interface {
	Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1)
}

// PlanExecutor executes exactly the outputs in plan.Outputs. The engine may
// pass a subset of the original outputs when retrying only failed items, so
// implementations must iterate plan.Outputs instead of re-planning or iterating
// req.InputPaths. The request is provided for input paths; it must not be
// re-planned.
type PlanExecutor interface {
	ExecutePlan(ctx context.Context, req models.JobRequestV1, plan models.PlanJobResponseV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1)
}
