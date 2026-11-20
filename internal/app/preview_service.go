package app

import (
	"context"

	"fileforge-desktop/internal/preview"
)

// PreviewService exposes the preview job queue and its cached results.
type PreviewService struct {
	service *preview.PreviewService
	runtime *Runtime
}

// StartPreview enqueues a preview job and returns a job id.
func (s *PreviewService) StartPreview(_ context.Context, req preview.PreviewRequest) preview.PreviewStartResponse {
	// Wails cancels the bound-method context as soon as the RPC returns, so it
	// cannot parent a background job. Use the app lifetime context instead; the
	// job is cancelled when the application shuts down.
	id, err := s.service.Enqueue(s.runtime.ContextOrBackground(), req)
	if err != nil {
		return preview.PreviewStartResponse{Success: false, Message: err.Error()}
	}
	return preview.PreviewStartResponse{Success: true, JobID: id, Message: "queued"}
}

// GetPreviewStatus returns the job status for a preview job.
func (s *PreviewService) GetPreviewStatus(ctx context.Context, jobID string) preview.JobStatus {
	status, ok := s.service.StatusContext(ctx, jobID)
	if !ok {
		return preview.JobStatus{Status: preview.JobStateFailed, Progress: 0, Message: "not found"}
	}
	return status
}

// GetPreview returns the preview bytes (if ready) for a job.
func (s *PreviewService) GetPreview(ctx context.Context, jobID string) preview.PreviewResult {
	res, err := s.service.Fetch(ctx, jobID)
	if err != nil {
		return preview.PreviewResult{Success: false, Message: err.Error()}
	}
	return res
}

// CancelPreview attempts to cancel a preview job.
func (s *PreviewService) CancelPreview(jobID string) preview.PreviewStartResponse {
	if err := s.service.Cancel(jobID); err != nil {
		return preview.PreviewStartResponse{Success: false, Message: err.Error()}
	}
	return preview.PreviewStartResponse{Success: true, JobID: jobID, Message: "canceled"}
}
