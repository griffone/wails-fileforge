package jobs

import (
	"testing"

	"fileforge-desktop/internal/models"
)

func newProgressTestState(current, total int) *jobState {
	return &jobState{
		engine: &Engine{},
		result: models.JobResult{
			Progress: models.JobProgress{Current: current, Total: total, Stage: StatusRunning},
		},
	}
}

func TestJobProgressPercentStyleNeverRegresses(t *testing.T) {
	state := newProgressTestState(60, percentProgressTotal)

	state.updateProgress(models.JobProgress{Current: 40, Total: percentProgressTotal, Stage: "retrying", Message: "retry"})
	if snapshot := state.snapshot(); snapshot.Progress.Current != 60 {
		t.Fatalf("progress regressed to %d, want the 60 floor", snapshot.Progress.Current)
	}

	state.updateProgress(models.JobProgress{Current: 70, Total: percentProgressTotal, Stage: "running"})
	if snapshot := state.snapshot(); snapshot.Progress.Current != 70 {
		t.Fatalf("progress did not advance, got %d", snapshot.Progress.Current)
	}
}

func TestJobProgressCountStyleMayRestart(t *testing.T) {
	state := newProgressTestState(3, 4)

	state.updateProgress(models.JobProgress{Current: 1, Total: 4, Stage: "running"})
	if snapshot := state.snapshot(); snapshot.Progress.Current != 1 {
		t.Fatalf("count progress must stay unchanged by the percent guard, got %d", snapshot.Progress.Current)
	}
}

func TestJobProgressRetryKeepsPerInputCountsMonotonic(t *testing.T) {
	state := newProgressTestState(4, 5)

	state.updateProgress(models.JobProgress{Current: 0, Total: 5, Stage: retryStage, Message: "retry"})
	snapshot := state.snapshot()
	if snapshot.Progress.Current != 4 || snapshot.Progress.Total != 5 {
		t.Fatalf("retry progress = %d/%d, want 4/5", snapshot.Progress.Current, snapshot.Progress.Total)
	}
	if snapshot.Progress.Stage != retryStage {
		t.Fatalf("stage = %q, want %q", snapshot.Progress.Stage, retryStage)
	}
}

func TestBatchProgressHandlerLeavesPerInputCountsAloneOnRetry(t *testing.T) {
	// A single planned output (pdf merge) reports its own input count, which
	// must not be folded into the plan-item position.
	state := newProgressTestState(4, 5)
	handler := (&Engine{}).batchProgressHandler(state, true, []int{0}, 1)

	handler(models.JobProgress{Current: 0, Total: 5})

	snapshot := state.snapshot()
	if snapshot.Progress.Current != 4 || snapshot.Progress.Total != 5 {
		t.Fatalf("retry progress = %d/%d, want 4/5", snapshot.Progress.Current, snapshot.Progress.Total)
	}
	if snapshot.Progress.Stage != retryStage {
		t.Fatalf("stage = %q, want %q", snapshot.Progress.Stage, retryStage)
	}
}

func TestBatchProgressHandlerKeepsPercentCoordinatesOnRetry(t *testing.T) {
	state := newProgressTestState(50, percentProgressTotal)
	handler := (&Engine{}).batchProgressHandler(state, true, []int{2}, 4)

	handler(models.JobProgress{Current: 10, Total: percentProgressTotal})

	snapshot := state.snapshot()
	if snapshot.Progress.Total != percentProgressTotal {
		t.Fatalf("total = %d, want %d", snapshot.Progress.Total, percentProgressTotal)
	}
	if snapshot.Progress.Current != 50 {
		t.Fatalf("current = %d, want the 50 floor", snapshot.Progress.Current)
	}
	if snapshot.Progress.Stage != "retrying" {
		t.Fatalf("stage = %q, want retrying", snapshot.Progress.Stage)
	}
}

func TestBatchProgressHandlerMapsCountsOnRetry(t *testing.T) {
	state := newProgressTestState(1, 4)
	handler := (&Engine{}).batchProgressHandler(state, true, []int{2}, 4)

	handler(models.JobProgress{Current: 1, Total: 1})

	snapshot := state.snapshot()
	if snapshot.Progress.Current != 3 || snapshot.Progress.Total != 4 {
		t.Fatalf("mapped progress = %d/%d, want 3/4", snapshot.Progress.Current, snapshot.Progress.Total)
	}
}
