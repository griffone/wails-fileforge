package doc

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"

	"fileforge-desktop/internal/models"
)

// docItemProgress serializes completion progress for a parallel batch. The
// atomic counter numbers finished items; the mutex keeps the callback order
// monotonic too, so callers never observe "processed 2/3" before "processed
// 1/3" even when two workers finish at the same time.
type docItemProgress struct {
	total      int
	onProgress func(models.JobProgress)
	completed  atomic.Int64
	mu         sync.Mutex
}

func newDocItemProgress(total int, onProgress func(models.JobProgress)) *docItemProgress {
	if onProgress == nil {
		return nil
	}

	return &docItemProgress{total: total, onProgress: onProgress}
}

// itemCompleted counts one finished item and emits its progress event.
func (p *docItemProgress) itemCompleted() {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	current := int(p.completed.Add(1))
	p.onProgress(models.JobProgress{
		Current: current,
		Total:   p.total,
		Stage:   models.JobStatusRunning,
		Message: fmt.Sprintf("processed %d/%d", current, p.total),
	})
}

// collectCompletedDocItems returns the results written by index in plan order,
// dropping entries whose worker never ran (for example after cancellation).
// Each completed flag is written by the goroutine that owns its index, so the
// read after the workers join is race-free.
func collectCompletedDocItems(items []models.JobResultItem, completed []bool) []models.JobResultItem {
	collected := make([]models.JobResultItem, 0, len(items))
	for index, item := range items {
		if completed[index] {
			collected = append(collected, item)
		}
	}

	return collected
}

// firstFailedDocItemError returns the error of the first failed item in plan
// order, matching sequential loop semantics.
func firstFailedDocItemError(items []models.JobResultItem) *models.JobError {
	for _, item := range items {
		if !item.Success && item.Error != nil {
			return item.Error
		}
	}

	return nil
}

// docBatchCancellationCause returns the context error when the batch context
// was cancelled, otherwise the helper error.
func docBatchCancellationCause(ctx context.Context, runErr error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}

	return runErr
}

// docBatchCancelled reports whether a parallel batch must return its
// cancellation error after the workers join.
func docBatchCancelled(ctx context.Context, runErr error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}

	return errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded)
}
