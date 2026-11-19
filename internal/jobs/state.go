package jobs

import (
	"context"
	"fmt"
	"sync"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/progress"
	"fileforge-desktop/internal/tools"
)

// jobState owns one submitted job. Its mutex guards the result snapshot; the
// scheduler-only fields (queued) are guarded by Engine.queueMu instead.
type jobState struct {
	engine *Engine

	mu       sync.Mutex
	result   models.JobResultV1
	terminal bool
	cancel   context.CancelFunc
	done     chan struct{}
	reporter *progress.Reporter[models.JobProgressEventV1]

	// Immutable after submission.
	ctx         context.Context
	request     models.JobRequestV1
	tool        tools.Tool
	plan        *models.PlanJobResponseV1
	class       Class
	priority    Priority
	seq         uint64
	kind        string
	workers     int
	parentID    string
	maxAttempts int
	onProgress  func(models.JobProgressV1)

	// queued is guarded by Engine.queueMu.
	queued bool
}

func newJobState(engine *Engine, req models.JobRequestV1, tool tools.Tool, opts SubmitOptions, plan *models.PlanJobResponseV1) *jobState {
	seq := engine.seq.Add(1)
	startedAt := time.Now().UnixMilli()

	state := &jobState{
		engine:      engine,
		request:     req,
		tool:        tool,
		plan:        plan,
		class:       opts.Class,
		priority:    opts.Priority,
		seq:         seq,
		kind:        opts.Kind,
		workers:     opts.Workers,
		parentID:    opts.ParentID,
		maxAttempts: opts.MaxAttempts,
		onProgress:  opts.OnProgress,
		done:        make(chan struct{}),
		result: models.JobResultV1{
			JobID:     newJobID(seq),
			Success:   false,
			Message:   "job queued",
			ToolID:    req.ToolID,
			Status:    StatusQueued,
			Progress:  models.JobProgressV1{Current: 0, Total: len(req.InputPaths), Stage: StatusQueued, Message: "queued"},
			StartedAt: startedAt,
		},
	}

	state.reporter = progress.New(engine.opts.ProgressInterval, func(event models.JobProgressEventV1) {
		// Drop a coalesced stale update that lost the race against completion.
		if !isTerminalJobStatus(event.Status) && state.isTerminal() {
			return
		}
		engine.emitProgress(event)
		// The listener mirrors the coalesced progress stream but never sees
		// terminal progress; the terminal state event covers completion.
		if listener := state.onProgress; listener != nil && !isTerminalJobStatus(event.Status) {
			listener(event.Progress)
		}
	})

	return state
}

func newJobID(seq uint64) string {
	return fmt.Sprintf("job_%d_%d", time.Now().UnixNano(), seq)
}

func (j *jobState) isTerminal() bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	return j.terminal
}

// snapshot returns a copy that shares no slices or maps with the live state.
func (j *jobState) snapshot() models.JobResultV1 {
	j.mu.Lock()
	defer j.mu.Unlock()

	return cloneJobResult(j.result)
}

// updateProgress records scheduler progress and reports it through the
// coalescing reporter. Persistence is debounced so a fast tool cannot turn
// progress ticks into a write per tick.
func (j *jobState) updateProgress(value models.JobProgressV1) {
	j.mu.Lock()
	if j.terminal {
		j.mu.Unlock()
		return
	}

	value.ETASeconds = estimateETASeconds(j.result.StartedAt, value.Current, value.Total)
	j.result.Progress = value
	if value.Stage != "" {
		j.result.Status = value.Stage
	}
	event := models.JobProgressEventV1{
		JobID:    j.result.JobID,
		ToolID:   j.result.ToolID,
		Status:   j.result.Status,
		Progress: j.result.Progress,
	}
	reporter := j.reporter
	j.mu.Unlock()

	if reporter != nil {
		reporter.Report(event)
	}
	j.engine.persistDebounced()
}

// complete moves the job into a terminal state exactly once, then emits the
// terminal snapshot, persists it, and runs the completion hook. Later calls
// are no-ops.
func (j *jobState) complete(status, message string, items []models.JobResultItemV1, jobErr *models.JobErrorV1, endedAt int64) {
	j.mu.Lock()
	if j.terminal {
		j.mu.Unlock()
		return
	}
	j.terminal = true

	j.result.Success = status == models.JobStatusSuccess
	j.result.Status = status
	j.result.Message = message
	j.result.Items = cloneJobItems(items)
	j.result.Error = cloneJobError(jobErr)
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

	result := cloneJobResult(j.result)
	event := models.JobProgressEventV1{
		JobID:    j.result.JobID,
		ToolID:   j.result.ToolID,
		Status:   j.result.Status,
		Progress: j.result.Progress,
	}
	reporter := j.reporter
	done := j.done
	j.mu.Unlock()

	close(done)

	if reporter != nil {
		reporter.EmitNow(event)
		reporter.Flush()
	}
	j.engine.emitState(result)
	j.engine.persistNow()

	if onComplete := j.engine.opts.OnJobComplete; onComplete != nil {
		onComplete(cloneJobResult(result))
	}
	j.engine.pruneTerminalJobs()
}

// cancelContext requests cancellation for a running job. Terminal jobs and
// recovered jobs without a context are ignored.
func (j *jobState) cancelContext() {
	j.mu.Lock()
	cancel := j.cancel
	terminal := j.terminal
	j.mu.Unlock()

	if terminal || cancel == nil {
		return
	}
	cancel()
}

// cloneJobPlan returns a plan copy that shares no slices or maps with the
// tool-returned value, so the stored plan stays immutable after submission.
func cloneJobPlan(in *models.PlanJobResponseV1) *models.PlanJobResponseV1 {
	if in == nil {
		return nil
	}

	out := *in
	if in.Outputs != nil {
		out.Outputs = make([]models.PlannedOutputV1, len(in.Outputs))
		for index, output := range in.Outputs {
			out.Outputs[index] = clonePlannedOutput(output)
		}
	}
	if in.EffectiveOptions != nil {
		out.EffectiveOptions = make(map[string]any, len(in.EffectiveOptions))
		for key, value := range in.EffectiveOptions {
			out.EffectiveOptions[key] = value
		}
	}
	if in.Warnings != nil {
		out.Warnings = append([]string(nil), in.Warnings...)
	}
	out.Error = cloneJobError(in.Error)

	return &out
}

func clonePlannedOutput(in models.PlannedOutputV1) models.PlannedOutputV1 {
	out := in
	if in.Warnings != nil {
		out.Warnings = append([]string(nil), in.Warnings...)
	}

	return out
}

func cloneJobResult(in models.JobResultV1) models.JobResultV1 {
	out := in
	out.Items = cloneJobItems(in.Items)
	out.Error = cloneJobError(in.Error)

	return out
}

func cloneJobItems(items []models.JobResultItemV1) []models.JobResultItemV1 {
	if items == nil {
		return nil
	}

	out := make([]models.JobResultItemV1, len(items))
	for index, item := range items {
		out[index] = cloneJobItem(item)
	}

	return out
}

func cloneJobItem(in models.JobResultItemV1) models.JobResultItemV1 {
	out := in
	if in.Outputs != nil {
		out.Outputs = append([]string(nil), in.Outputs...)
	}
	out.Error = cloneJobError(in.Error)

	return out
}

func cloneJobError(in *models.JobErrorV1) *models.JobErrorV1 {
	if in == nil {
		return nil
	}

	out := *in
	if in.Details != nil {
		out.Details = make(map[string]any, len(in.Details))
		for key, value := range in.Details {
			out.Details[key] = value
		}
	}

	return &out
}
