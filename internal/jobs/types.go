package jobs

import (
	"context"
	"sync"

	"fileforge-desktop/internal/models"
)

type trackedJob struct {
	mu     sync.RWMutex
	ctx    context.Context
	cancel context.CancelFunc
	result models.JobResultV1
}

func (j *trackedJob) snapshot() models.JobResultV1 {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.result
}

func (j *trackedJob) updateProgress(progress models.JobProgressV1) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.result.Progress = progress
	if progress.Stage != "" {
		j.result.Status = progress.Stage
	}
}

func (j *trackedJob) complete(success bool, status string, message string, items []models.JobResultItemV1, jobErr *models.JobErrorV1, endedAt int64) {
	j.mu.Lock()
	defer j.mu.Unlock()
	j.result.Success = success
	j.result.Status = status
	j.result.Message = message
	j.result.Items = items
	j.result.Error = jobErr
	j.result.EndedAt = endedAt
	if j.result.Progress.Total == 0 {
		j.result.Progress.Total = len(items)
	}
	if j.result.Progress.Total > 0 {
		j.result.Progress.Current = j.result.Progress.Total
	} else {
		j.result.Progress.Current = len(items)
	}
	j.result.Progress.Stage = status
	if message != "" {
		j.result.Progress.Message = message
	}
}
