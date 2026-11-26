package jobs

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/fsx"
	"fileforge-desktop/internal/platform/jsonstore"
	"fileforge-desktop/internal/platform/progress"
	"fileforge-desktop/internal/tools"
)

const (
	defaultMaxQueue     = 256
	defaultMaxAttempts  = 3
	enginePanicDetail   = "PANIC"
	defaultEngineDetail = "EXECUTION_FAILED"

	// KindTool is the default job kind for direct tool runs submitted through
	// the public job API.
	KindTool = "tool"

	// KindRecipeStep marks a job submitted by the recipe service for one step.
	// These jobs are internal: the user-facing job list, events, and undo
	// history hide them, while the recipe run records their final outputs once.
	KindRecipeStep = "recipe-step"
)

// Emitter receives engine events. Implementations must be safe for concurrent
// use: running jobs report progress from their own goroutines.
type Emitter interface {
	JobProgress(models.JobProgressEvent)
	JobState(models.JobResult)
}

// ToolResolver looks up a tool by its stable ID.
type ToolResolver interface {
	GetTool(toolID string) (tools.Tool, error)
}

// RetryPolicy decides whether a failed attempt may be retried.
type RetryPolicy interface {
	Retryable(toolID string, err *models.JobError) bool
}

// Options configures an Engine. Zero values select the documented defaults.
type Options struct {
	Limits           Limits
	MaxQueue         int
	PersistencePath  string
	Emitter          Emitter
	Logger           *slog.Logger
	OnJobComplete    func(models.JobResult)
	Retry            RetryPolicy
	ProgressInterval time.Duration
	Debounce         time.Duration
	Retention        int
}

// SubmitOptions describes scheduling metadata for a submitted job.
type SubmitOptions struct {
	Kind        string
	Priority    Priority
	Class       Class
	Workers     int
	ParentID    string
	MaxAttempts int

	// OnProgress receives the same coalesced progress updates the engine
	// emits. It is never called with a terminal status and may be nil.
	OnProgress func(models.JobProgress)
}

// Engine is a priority-aware, class-limited job scheduler. One dispatcher
// goroutine owns scheduling; execution goroutines are bounded by Limits.
type Engine struct {
	resolver ToolResolver
	opts     Options
	logger   *slog.Logger
	retry    RetryPolicy
	store    *jsonstore.Store

	mu   sync.RWMutex
	jobs map[string]*jobState

	queueMu      sync.Mutex
	queue        []*jobState
	totalRunning int
	running      map[Class]int

	wake           chan struct{}
	stopCh         chan struct{}
	dispatcherDone chan struct{}
	stopOnce       sync.Once
	closed         atomic.Bool
	seq            atomic.Uint64
	wg             sync.WaitGroup

	persistMu sync.Mutex
	baseMu    sync.RWMutex
	baseCtx   context.Context
}

type defaultRetryPolicy struct{}

func (defaultRetryPolicy) Retryable(toolID string, jobErr *models.JobError) bool {
	return isRetryableError(toolID, jobErr)
}

// NewEngine creates an engine, normalizes defaults, and starts its dispatcher.
func NewEngine(resolver ToolResolver, opts Options) *Engine {
	opts = normalizeEngineOptions(opts)

	engine := &Engine{
		resolver:       resolver,
		opts:           opts,
		logger:         opts.Logger,
		retry:          opts.Retry,
		jobs:           make(map[string]*jobState),
		running:        make(map[Class]int),
		wake:           make(chan struct{}, 1),
		stopCh:         make(chan struct{}),
		dispatcherDone: make(chan struct{}),
	}

	if path := strings.TrimSpace(opts.PersistencePath); path != "" {
		engine.opts.PersistencePath = path
		engine.store = jsonstore.New(path, jsonstore.Options{
			Debounce: opts.Debounce,
			OnError: func(err error) {
				engine.logger.Warn("jobs persistence write failed", "path", path, "error", err)
			},
		})
	}

	go engine.dispatchLoop()

	return engine
}

func normalizeEngineOptions(opts Options) Options {
	opts.Limits = normalizeLimits(opts.Limits)
	if opts.MaxQueue <= 0 {
		opts.MaxQueue = defaultMaxQueue
	}
	if opts.Retention <= 0 {
		opts.Retention = retainedTerminalJobs
	}
	if opts.ProgressInterval <= 0 {
		opts.ProgressInterval = progress.DefaultInterval
	}
	if opts.Debounce <= 0 {
		opts.Debounce = jsonstore.DefaultDebounce
	}
	if opts.Logger == nil {
		opts.Logger = slog.Default()
	}
	if opts.Retry == nil {
		opts.Retry = defaultRetryPolicy{}
	}

	return opts
}

// SetBaseContext sets the parent context for subsequently submitted jobs. The
// engine never cancels it; callers use it to bind jobs to app lifecycle.
func (e *Engine) SetBaseContext(ctx context.Context) {
	e.baseMu.Lock()
	e.baseCtx = ctx
	e.baseMu.Unlock()
}

// SetPersistencePath switches the persistence target. An empty path is a no-op.
// The previous store is closed before the new one is opened. When jobs already
// exist, the current snapshot is written once so a restart can recover them.
// Recovery itself stays the caller's responsibility; see Recover.
func (e *Engine) SetPersistencePath(path string) error {
	path = strings.TrimSpace(path)
	if path == "" {
		return nil
	}

	e.persistMu.Lock()
	var firstErr error
	if e.store != nil {
		if err := e.store.Close(context.Background()); err != nil {
			firstErr = err
		}
		e.store = nil
	}
	e.opts.PersistencePath = path
	e.store = jsonstore.New(path, jsonstore.Options{
		Debounce: e.opts.Debounce,
		OnError: func(err error) {
			e.logger.Warn("jobs persistence write failed", "path", path, "error", err)
		},
	})
	hasJobs := e.hasJobs()
	e.persistMu.Unlock()

	if hasJobs {
		if err := e.saveSnapshot(true); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (e *Engine) hasJobs() bool {
	e.mu.RLock()
	defer e.mu.RUnlock()

	return len(e.jobs) > 0
}

// normalizeJobError returns a canonical copy of a tool error: the detail code
// doubles as the canonical code and the details map is copied so callers never
// share mutable state with a tool.
func normalizeJobError(jobErr *models.JobError) *models.JobError {
	if jobErr == nil {
		return nil
	}

	details := jobErr.Details
	if len(details) > 0 {
		copied := make(map[string]any, len(details))
		for key, value := range details {
			copied[key] = value
		}
		details = copied
	}

	detailCode := jobErr.DetailCode
	if detailCode == "" {
		detailCode = jobErr.Code
	}

	return models.NewCanonicalJobError(detailCode, jobErr.Message, details)
}

func (e *Engine) jobParentContext(ctx context.Context) context.Context {
	e.baseMu.RLock()
	base := e.baseCtx
	e.baseMu.RUnlock()

	if base != nil {
		return base
	}

	return contextOrBackground(ctx)
}

func contextOrBackground(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	return ctx
}

func (e *Engine) resolveTool(toolID string) (tools.Tool, error) {
	if e.resolver == nil {
		return nil, fmt.Errorf("tool registry is not configured")
	}

	return e.resolver.GetTool(toolID)
}

// Recover loads the persisted snapshot, restores terminal jobs, marks every
// non-terminal job as interrupted, and persists the recovered state.
func (e *Engine) Recover(ctx context.Context) error {
	if e.store == nil {
		return nil
	}
	ctx = contextOrBackground(ctx)
	if err := ctx.Err(); err != nil {
		return err
	}

	path := e.opts.PersistencePath
	if info, err := os.Stat(path); err == nil && info.Size() == 0 {
		return nil
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("stat persisted jobs: %w", err)
	}

	var persisted []models.JobResult
	if err := jsonstore.Load(path, &persisted); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil
		}
		return err
	}

	now := time.Now().UnixMilli()

	e.mu.Lock()
	for _, result := range persisted {
		if strings.TrimSpace(result.JobID) == "" {
			continue
		}
		restored := cloneJobResult(result)
		if !isTerminalJobStatus(restored.Status) {
			restored.Success = false
			restored.Status = StatusInterrupted
			restored.Message = "job interrupted after restart"
			restored.EndedAt = now
			restored.Error = models.NewCanonicalJobError("JOB_INTERRUPTED", "job interrupted after restart", nil)
			restored.Progress.Stage = StatusInterrupted
			if restored.Progress.Total > 0 && restored.Progress.Current > restored.Progress.Total {
				restored.Progress.Current = restored.Progress.Total
			}
			if restored.Progress.Message == "" {
				restored.Progress.Message = "interrupted"
			}
		}
		e.jobs[restored.JobID] = newRecoveredJobState(e, restored)
	}
	e.pruneTerminalLocked()
	e.mu.Unlock()

	e.persistNow()

	return nil
}

func newRecoveredJobState(engine *Engine, result models.JobResult) *jobState {
	state := &jobState{
		engine:   engine,
		result:   result,
		terminal: true,
		done:     make(chan struct{}),
	}
	close(state.done)

	return state
}

// Submit enqueues a tool job with interactive priority and the tool's class.
func (e *Engine) Submit(ctx context.Context, req models.JobRequest) (models.RunJobResponse, error) {
	return e.SubmitWithOptions(ctx, req, SubmitOptions{
		Kind:     KindTool,
		Priority: PriorityInteractive,
	})
}

// SubmitWithOptions resolves the tool, plans the request once when the tool
// supports it, and enqueues the job, returning immediately. The engine
// dispatcher decides when it actually starts.
func (e *Engine) SubmitWithOptions(ctx context.Context, req models.JobRequest, opts SubmitOptions) (models.RunJobResponse, error) {
	ctx = contextOrBackground(ctx)

	tool, err := e.resolveTool(req.ToolID)
	if err != nil {
		return models.RunJobResponse{}, fmt.Errorf("tool lookup failed: %w", err)
	}

	opts = e.normalizeSubmitOptions(tool, req.ToolID, opts)

	plan, rejectErr := e.planRequest(ctx, tool, req)
	if rejectErr != nil {
		return models.RunJobResponse{
			Success: false,
			Message: "job validation failed",
			Status:  StatusFailed,
			Error:   normalizeJobError(rejectErr),
		}, nil
	}

	state := newJobState(e, req, tool, opts, plan)
	state.ctx, state.cancel = context.WithCancel(e.jobParentContext(ctx))

	if err := e.enqueueNewJob(state); err != nil {
		state.cancel()
		return models.RunJobResponse{}, err
	}

	return models.RunJobResponse{
		Success: true,
		Message: "job submitted",
		JobID:   state.result.JobID,
		Status:  StatusQueued,
	}, nil
}

// planRequest resolves a request exactly once for planner tools and stores the
// result on the job. Tools without a planner keep the legacy Validate contract.
func (e *Engine) planRequest(ctx context.Context, tool tools.Tool, req models.JobRequest) (*models.PlanJobResponse, *models.JobError) {
	planner, ok := tool.(tools.Planner)
	if !ok {
		return nil, tool.Validate(ctx, req)
	}

	plan, planErr := planner.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}
	if !plan.Success {
		message := strings.TrimSpace(plan.Message)
		if message == "" {
			message = "job request could not be planned"
		}

		return nil, models.NewCanonicalJobError("VALIDATION_ERROR", message, nil)
	}

	return cloneJobPlan(&plan), nil
}

func (e *Engine) normalizeSubmitOptions(tool tools.Tool, toolID string, opts SubmitOptions) SubmitOptions {
	opts.Kind = strings.TrimSpace(opts.Kind)
	if opts.Kind == "" {
		opts.Kind = KindTool
	}

	manifest := tool.Manifest()
	if opts.Class == "" {
		opts.Class = manifestResourceClass(manifest.ResourceClass)
	}
	if opts.Class == "" {
		opts.Class = classForTool(toolID)
	}
	switch opts.Class {
	case ClassCPU, ClassExternal, ClassIO:
	default:
		opts.Class = ClassCPU
	}
	if opts.Priority < PriorityInteractive {
		opts.Priority = PriorityInteractive
	}
	if opts.Priority > PriorityPreview {
		opts.Priority = PriorityPreview
	}
	if opts.MaxAttempts <= 0 {
		opts.MaxAttempts = defaultMaxAttempts
	}
	if opts.Workers <= 0 {
		if manifest.MaxWorkers > 0 {
			opts.Workers = manifest.MaxWorkers
		} else {
			opts.Workers = e.defaultWorkers(opts.Class)
		}
	}

	return opts
}

// manifestResourceClass maps a manifest resource class to a scheduler class.
// Empty or unknown values return "" so the caller falls back to ClassForTool.
func manifestResourceClass(resourceClass string) Class {
	class := Class(strings.ToLower(strings.TrimSpace(resourceClass)))
	switch class {
	case ClassCPU, ClassExternal, ClassIO:
		return class
	default:
		return ""
	}
}

func (e *Engine) defaultWorkers(class Class) int {
	workers := e.opts.Limits.capacity(class)
	if cpus := runtime.NumCPU(); cpus < workers {
		workers = cpus
	}
	if workers < 1 {
		workers = 1
	}

	return workers
}

func (e *Engine) enqueueNewJob(state *jobState) error {
	e.mu.Lock()
	e.jobs[state.result.JobID] = state
	e.mu.Unlock()

	if err := e.enqueue(state); err != nil {
		e.mu.Lock()
		delete(e.jobs, state.result.JobID)
		e.mu.Unlock()
		return err
	}

	// Persist the queued snapshot before the dispatcher can complete the job,
	// so a crash right after submission still leaves a recoverable record.
	e.persistNow()
	e.signal()

	return nil
}

// Cancel removes a queued job and completes it as cancelled. Running jobs get
// their context cancelled and finish through their normal cancellation path.
func (e *Engine) Cancel(jobID string) error {
	state := e.lookup(jobID)
	if state == nil {
		return fmt.Errorf("job '%s' not found", jobID)
	}

	if e.removeQueued(state) {
		state.complete(StatusCancelled, "job cancelled", nil, cancelledJobError(state.ctx, 0, nil), time.Now().UnixMilli())
		return nil
	}

	state.cancelContext()

	return nil
}

// Get returns one job snapshot.
func (e *Engine) Get(jobID string) (models.JobResult, bool) {
	state := e.lookup(jobID)
	if state == nil {
		return models.JobResult{}, false
	}

	return state.snapshot(), true
}

func (e *Engine) lookup(jobID string) *jobState {
	e.mu.RLock()
	state := e.jobs[jobID]
	e.mu.RUnlock()

	return state
}

// List returns job snapshots newest first, capped to limit. A non-positive
// limit uses 100.
func (e *Engine) List(limit int) []models.JobResult {
	if limit <= 0 {
		limit = defaultJobListLimit
	}

	e.mu.RLock()
	states := make([]*jobState, 0, len(e.jobs))
	for _, state := range e.jobs {
		states = append(states, state)
	}
	e.mu.RUnlock()

	jobs := make([]models.JobResult, 0, len(states))
	for _, state := range states {
		jobs = append(jobs, state.snapshot())
	}

	sortJobResults(jobs)
	if len(jobs) > limit {
		jobs = jobs[:limit]
	}

	return jobs
}

// Wait blocks until the job reaches a terminal state or ctx ends.
func (e *Engine) Wait(ctx context.Context, jobID string) (models.JobResult, error) {
	state := e.lookup(jobID)
	if state == nil {
		return models.JobResult{}, fmt.Errorf("job '%s' not found", jobID)
	}

	ctx = contextOrBackground(ctx)
	select {
	case <-state.done:
		return state.snapshot(), nil
	case <-ctx.Done():
		return state.snapshot(), ctx.Err()
	}
}

// Validate delegates to the tool's validation contract.
func (e *Engine) Validate(ctx context.Context, req models.JobRequest) models.ValidateJobResponse {
	ctx = contextOrBackground(ctx)

	tool, err := e.resolveTool(req.ToolID)
	if err != nil {
		return models.ValidateJobResponse{
			Success: false,
			Message: "tool not found",
			Valid:   false,
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", err.Error(), nil),
		}
	}

	if validationErr := tool.Validate(ctx, req); validationErr != nil {
		return models.ValidateJobResponse{
			Success: false,
			Message: "validation failed",
			Valid:   false,
			Error:   normalizeJobError(validationErr),
		}
	}

	return models.ValidateJobResponse{
		Success: true,
		Message: "job request is valid",
		Valid:   true,
	}
}

// Plan delegates to the tool's optional planner.
func (e *Engine) Plan(ctx context.Context, req models.JobRequest) models.PlanJobResponse {
	ctx = contextOrBackground(ctx)

	tool, err := e.resolveTool(req.ToolID)
	if err != nil {
		return models.PlanJobResponse{
			Success: false,
			Message: "tool not found",
			Outputs: []models.PlannedOutput{},
			Error:   models.NewCanonicalJobError("TOOL_NOT_FOUND", err.Error(), nil),
		}
	}

	planner, ok := tool.(tools.Planner)
	if !ok {
		message := "planning is not available for this tool"
		return models.PlanJobResponse{
			Success: false,
			Message: message,
			Outputs: []models.PlannedOutput{},
			Error:   models.NewCanonicalJobError("PLAN_UNSUPPORTED", message, nil),
		}
	}

	plan, planErr := planner.Plan(ctx, req)
	if plan.Outputs == nil {
		plan.Outputs = []models.PlannedOutput{}
	}
	if planErr == nil {
		return plan
	}

	plan.Success = false
	if plan.Message == "" {
		plan.Message = planErr.Message
	}
	plan.Error = normalizeJobError(planErr)

	return plan
}

// Shutdown stops the dispatcher, cancels queued and running jobs, waits for
// workers within ctx, and closes the store. Repeated calls are safe.
func (e *Engine) Shutdown(ctx context.Context) error {
	ctx = contextOrBackground(ctx)

	e.stopOnce.Do(func() {
		e.closed.Store(true)
		close(e.stopCh)
		e.signal()
	})

	for _, state := range e.drainQueue() {
		state.complete(StatusCancelled, "job cancelled", nil, cancelledJobError(state.ctx, 0, nil), time.Now().UnixMilli())
	}

	var firstErr error
	select {
	case <-e.dispatcherDone:
	case <-ctx.Done():
		firstErr = ctx.Err()
	}

	e.mu.RLock()
	states := make([]*jobState, 0, len(e.jobs))
	for _, state := range e.jobs {
		states = append(states, state)
	}
	e.mu.RUnlock()

	for _, state := range states {
		state.cancelContext()
	}

	done := make(chan struct{})
	go func() {
		e.wg.Wait()
		close(done)
	}()

	select {
	case <-done:
	case <-ctx.Done():
		if firstErr == nil {
			firstErr = ctx.Err()
		}
	}

	if e.store != nil {
		closeCtx := ctx
		if closeCtx.Err() != nil {
			// A expired deadline must not leave the store unusable: flushing the
			// pending snapshot is still safe and keeps recovery consistent.
			closeCtx = context.WithoutCancel(ctx)
		}
		if err := e.store.Close(closeCtx); err != nil && firstErr == nil {
			firstErr = err
		}
	}

	return firstErr
}

func (e *Engine) emitProgress(event models.JobProgressEvent) {
	if e.opts.Emitter == nil {
		return
	}
	e.opts.Emitter.JobProgress(event)
}

func (e *Engine) emitState(result models.JobResult) {
	if e.opts.Emitter == nil {
		return
	}
	e.opts.Emitter.JobState(result)
}

// persistDebounced schedules a full snapshot with the store's debounce window.
func (e *Engine) persistDebounced() {
	if err := e.saveSnapshot(false); err != nil && !errors.Is(err, jsonstore.ErrClosed) {
		e.logger.Warn("jobs persistence enqueue failed", "error", err)
	}
}

// persistNow writes a full snapshot immediately.
func (e *Engine) persistNow() {
	if err := e.saveSnapshot(true); err != nil && !errors.Is(err, jsonstore.ErrClosed) {
		e.logger.Warn("jobs persistence write failed", "error", err)
	}
}

// saveSnapshot serializes one full snapshot and hands it to the store.
func (e *Engine) saveSnapshot(immediate bool) error {
	e.persistMu.Lock()
	defer e.persistMu.Unlock()

	return e.saveSnapshotLocked(immediate)
}

// saveSnapshotLocked writes the current snapshot. The caller holds persistMu.
// A debounced snapshot hands the store a producer instead of encoded bytes: the
// engine marshals only when the debounce window actually elapses, so progress
// ticks that the store coalesces cost no clone and no JSON encode.
func (e *Engine) saveSnapshotLocked(immediate bool) error {
	if e.store == nil {
		return nil
	}

	if !immediate {
		return e.store.SaveFunc(e.marshalSnapshot)
	}

	payload, err := e.marshalSnapshot()
	if err != nil {
		return fmt.Errorf("encode jobs snapshot: %w", err)
	}

	return e.store.SaveNow(payload)
}

func (e *Engine) marshalSnapshot() ([]byte, error) {
	return encodeJobsSnapshot(e.jobSnapshot())
}

// encodeJobsSnapshot renders a job list in the persisted shape.
func encodeJobsSnapshot(jobs []models.JobResult) ([]byte, error) {
	return json.MarshalIndent(jobs, "", "  ")
}

// saveJobsSnapshot writes one full job snapshot atomically. The engine writes
// the same payload through its debounced store; this synchronous helper backs
// the persistence tests and benchmark.
func saveJobsSnapshot(path string, jobs []models.JobResult) error {
	payload, err := encodeJobsSnapshot(jobs)
	if err != nil {
		return fmt.Errorf("encode jobs snapshot: %w", err)
	}
	if err := fsx.WriteAtomic(path, payload, 0o644); err != nil {
		return fmt.Errorf("persist jobs: %w", err)
	}

	return nil
}

// jobSnapshot mirrors the old engine shape: active jobs plus the newest
// Retention terminal jobs, all sorted newest first.
func (e *Engine) jobSnapshot() []models.JobResult {
	e.mu.RLock()
	states := make([]*jobState, 0, len(e.jobs))
	for _, state := range e.jobs {
		states = append(states, state)
	}
	e.mu.RUnlock()

	active := make([]models.JobResult, 0, len(states))
	terminal := make([]models.JobResult, 0, len(states))
	for _, state := range states {
		snapshot := state.snapshot()
		if isTerminalJobStatus(snapshot.Status) {
			terminal = append(terminal, snapshot)
			continue
		}
		active = append(active, snapshot)
	}

	sortJobResults(terminal)
	if len(terminal) > e.opts.Retention {
		terminal = terminal[:e.opts.Retention]
	}

	jobs := make([]models.JobResult, 0, len(active)+len(terminal))
	jobs = append(jobs, active...)
	jobs = append(jobs, terminal...)
	sortJobResults(jobs)

	return jobs
}

// pruneTerminalJobs drops the oldest in-memory terminal jobs beyond Retention.
func (e *Engine) pruneTerminalJobs() {
	e.mu.Lock()
	defer e.mu.Unlock()

	e.pruneTerminalLocked()
}

// pruneTerminalLocked requires Engine.mu to be held for writing.
func (e *Engine) pruneTerminalLocked() {
	type terminalRef struct {
		jobID     string
		startedAt int64
	}

	terminal := make([]terminalRef, 0, len(e.jobs))
	for jobID, state := range e.jobs {
		snapshot := state.snapshot()
		if !isTerminalJobStatus(snapshot.Status) {
			continue
		}
		terminal = append(terminal, terminalRef{jobID: jobID, startedAt: snapshot.StartedAt})
	}

	if len(terminal) <= e.opts.Retention {
		return
	}

	sort.Slice(terminal, func(i, j int) bool {
		if terminal[i].startedAt != terminal[j].startedAt {
			return terminal[i].startedAt > terminal[j].startedAt
		}
		return terminal[i].jobID > terminal[j].jobID
	})
	for _, ref := range terminal[e.opts.Retention:] {
		delete(e.jobs, ref.jobID)
	}
}
