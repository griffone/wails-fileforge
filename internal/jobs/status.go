package jobs

import (
	"math"
	"sort"
	"time"

	"fileforge-desktop/internal/models"
)

// Job status values exposed by the engine and the public service responses.
const (
	StatusQueued         = models.JobStatusQueued
	StatusRunning        = models.JobStatusRunning
	StatusSuccess        = models.JobStatusSuccess
	StatusFailed         = models.JobStatusFailed
	StatusPartialSuccess = models.JobStatusPartialSuccess
	StatusCancelled      = models.JobStatusCancelled
	StatusInterrupted    = models.JobStatusInterrupted

	// retainedTerminalJobs is the number of finished jobs kept in memory and in
	// the persisted snapshot. Active jobs are always retained.
	retainedTerminalJobs = 25

	// defaultJobListLimit applies when a caller asks for List with a
	// non-positive limit.
	defaultJobListLimit = 100
)

// isTerminalJobStatus reports whether a status is final. Running jobs expose a
// stage name (queued, running, processing, ocr, ...) as their status, so any
// unknown status is treated as active.
func isTerminalJobStatus(status string) bool {
	switch status {
	case StatusSuccess, StatusFailed, StatusPartialSuccess, StatusCancelled, StatusInterrupted:
		return true
	default:
		return false
	}
}

// sortJobResults orders job snapshots newest first, breaking ties by job id so
// list output stays deterministic.
func sortJobResults(jobs []models.JobResultV1) {
	sort.Slice(jobs, func(i, j int) bool {
		if jobs[i].StartedAt != jobs[j].StartedAt {
			return jobs[i].StartedAt > jobs[j].StartedAt
		}
		return jobs[i].JobID > jobs[j].JobID
	})
}

// estimateETASeconds extrapolates the remaining run time from the elapsed time
// and completed units. It returns zero when there is not enough information.
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
