package jobs

import (
	"context"
	"math"
	"sync"
	"time"

	"fileforge-desktop/internal/models"
)

type trackedJob struct {
	mu              sync.RWMutex
	ctx             context.Context
	cancel          context.CancelFunc
	result          models.JobResultV1
	onProgressEvent func(models.JobProgressEventV1)
	onStateChanged  func()
}

func (j *trackedJob) snapshot() models.JobResultV1 {
	j.mu.RLock()
	defer j.mu.RUnlock()
	return j.result
}

func (j *trackedJob) updateProgress(progress models.JobProgressV1) {
	j.mu.Lock()
	progress.ETASeconds = estimateETASeconds(j.result.StartedAt, progress.Current, progress.Total)
	j.result.Progress = progress
	if progress.Stage != "" {
		j.result.Status = progress.Stage
	}
	evt := models.JobProgressEventV1{
		JobID:    j.result.JobID,
		ToolID:   j.result.ToolID,
		Status:   j.result.Status,
		Progress: j.result.Progress,
	}
	listener := j.onProgressEvent
	j.mu.Unlock()

	if listener != nil {
		listener(evt)
	}

	if j.onStateChanged != nil {
		j.onStateChanged()
	}
}

func estimateETASeconds(startedAtMillis int64, current, total int) int {
	if startedAtMillis <= 0 || total <= 0 || current <= 0 || current >= total {
		return 0
	}

	elapsedSeconds := time.Since(time.UnixMilli(startedAtMillis)).Seconds()
	if elapsedSeconds <= 0 {
		return 0
	}

	remainingWork := float64(total - current)
	ratePerUnit := elapsedSeconds / float64(current)
	eta := int(math.Ceil(remainingWork * ratePerUnit))
	if eta < 0 {
		return 0
	}

	return eta
}

func (j *trackedJob) complete(status string, message string, items []models.JobResultItemV1, jobErr *models.JobErrorV1, endedAt int64) {
	j.mu.Lock()
	j.result.Success = status == models.JobStatusSuccess
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
	j.result.Progress.ETASeconds = 0
	if message != "" {
		j.result.Progress.Message = message
	}
	evt := models.JobProgressEventV1{
		JobID:    j.result.JobID,
		ToolID:   j.result.ToolID,
		Status:   j.result.Status,
		Progress: j.result.Progress,
	}
	listener := j.onProgressEvent
	j.mu.Unlock()

	if listener != nil {
		listener(evt)
	}

	if j.onStateChanged != nil {
		j.onStateChanged()
	}
}
