package jobs

import (
	"context"
	"fmt"
	"strings"
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
	result   models.JobResult
	terminal bool
	cancel   context.CancelFunc
	done     chan struct{}
	reporter *progress.Reporter[models.JobProgressEvent]

	// Immutable after submission.
	ctx         context.Context
	request     models.JobRequest
	tool        tools.Tool
	plan        *models.PlanJobResponse
	class       Class
	priority    Priority
	seq         uint64
	kind        string
	workers     int
	parentID    string
	maxAttempts int
	onProgress  func(models.JobProgress)

	// queued is guarded by Engine.queueMu.
	queued bool
}

func newJobState(engine *Engine, req models.JobRequest, tool tools.Tool, opts SubmitOptions, plan *models.PlanJobResponse) *jobState {
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
		result: models.JobResult{
			JobID:     newJobID(seq),
			Kind:      resultKind(opts.Kind),
			Success:   false,
			Message:   "job queued",
			ToolID:    req.ToolID,
			Status:    StatusQueued,
			Progress:  models.JobProgress{Current: 0, Total: len(req.InputPaths), Stage: StatusQueued, Message: "queued"},
			StartedAt: startedAt,
		},
	}

	state.reporter = progress.New(engine.opts.ProgressInterval, func(event models.JobProgressEvent) {
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

// resultKind keeps the wire field empty for normal tool jobs while preserving
// explicit internal kinds such as recipe steps.
func resultKind(kind string) string {
	kind = strings.TrimSpace(kind)
	if kind == "" || kind == KindTool {
		return ""
	}

	return kind
}

func (j *jobState) isTerminal() bool {
	j.mu.Lock()
	defer j.mu.Unlock()

	return j.terminal
}

// snapshot returns a copy that shares no slices or maps with the live state.
func (j *jobState) snapshot() models.JobResult {
	j.mu.Lock()
	defer j.mu.Unlock()

	return cloneJobResult(j.result)
}

// updateProgress records scheduler progress and reports it through the
// coalescing reporter. Persistence is debounced so a fast tool cannot turn
// progress ticks into a write per tick.
func (j *jobState) updateProgress(value models.JobProgress) {
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
	event := models.JobProgressEvent{
		JobID:    j.result.JobID,
		Kind:     j.result.Kind,
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
func (j *jobState) complete(status, message string, items []models.JobResultItem, jobErr *models.JobError, endedAt int64) {
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
	event := models.JobProgressEvent{
		JobID:    j.result.JobID,
		Kind:     j.result.Kind,
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
func cloneJobPlan(in *models.PlanJobResponse) *models.PlanJobResponse {
	if in == nil {
		return nil
	}

	out := *in
	if in.Outputs != nil {
		out.Outputs = make([]models.PlannedOutput, len(in.Outputs))
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

func clonePlannedOutput(in models.PlannedOutput) models.PlannedOutput {
	out := in
	if in.Warnings != nil {
		out.Warnings = append([]string(nil), in.Warnings...)
	}

	return out
}

func cloneJobResult(in models.JobResult) models.JobResult {
	out := in
	out.Items = cloneJobItems(in.Items)
	out.Error = cloneJobError(in.Error)

	return out
}

func cloneJobItems(items []models.JobResultItem) []models.JobResultItem {
	if items == nil {
		return nil
	}

	out := make([]models.JobResultItem, len(items))
	for index, item := range items {
		out[index] = cloneJobItem(item)
	}

	return out
}

func cloneJobItem(in models.JobResultItem) models.JobResultItem {
	out := in
	if in.Outputs != nil {
		out.Outputs = append([]string(nil), in.Outputs...)
	}
	out.Error = cloneJobError(in.Error)

	return out
}

func cloneJobError(in *models.JobError) *models.JobError {
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
