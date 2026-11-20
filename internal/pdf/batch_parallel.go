package pdf

import (
	"fmt"
	"sync"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/utils"
)

// pdfBatchWorkers caps concurrent PDF jobs. pdfcpu and Ghostscript each use
// their own threads, so a small fan-out already saturates a desktop machine.
const pdfBatchWorkers = 4

func pdfBatchWorkerCount() int {
	workers := utils.ParallelWorkerCount()
	if workers > pdfBatchWorkers {
		workers = pdfBatchWorkers
	}
	if workers < 1 {
		workers = 1
	}

	return workers
}

// pdfBatchProgress serializes completion progress for a parallel batch so
// callers never observe out-of-order counters.
type pdfBatchProgress struct {
	total      int
	onProgress func(models.JobProgress)
	completed  int
	mu         sync.Mutex
}

func newPDFBatchProgress(total int, onProgress func(models.JobProgress)) *pdfBatchProgress {
	if onProgress == nil {
		return nil
	}

	return &pdfBatchProgress{total: total, onProgress: onProgress}
}

func (p *pdfBatchProgress) itemCompleted() {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	p.completed++
	p.onProgress(models.JobProgress{
		Current: p.completed,
		Total:   p.total,
		Stage:   "running",
		Message: fmt.Sprintf("processed %d/%d", p.completed, p.total),
	})
}

// completedPDFBatchItems returns the items written by index, in input order,
// dropping entries whose worker never ran after cancellation.
func completedPDFBatchItems(items []models.JobResultItem, completed []bool) []models.JobResultItem {
	collected := make([]models.JobResultItem, 0, len(items))
	for index, item := range items {
		if completed[index] {
			collected = append(collected, item)
		}
	}

	return collected
}
