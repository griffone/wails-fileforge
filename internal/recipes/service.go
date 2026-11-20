// Package recipes implements the curated outcome recipes that chain the
// existing tools into one verified result. Phase 1 ships built-in recipes for
// Email PDF (linear: merge when needed, optimize to a target size), Web image
// set (fan-out: responsive WebP/JPEG variants plus a ZIP and srcset snippet),
// Social clip (fan-out: MP4 + WebM encodes plus a poster frame), Clean scan
// (local OCR into a searchable PDF plus a text sidecar) and Burned-in subtitles
// (local Whisper transcription burned into a new MP4 plus an SRT sidecar). All
// of them verify their outputs and record them for undo.
package recipes

import (
	"context"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/jobs"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/tools"
)

const (
	// RecipeIDEmailPDF is the curated outcome "Email PDF".
	RecipeIDEmailPDF = "recipe.email_pdf"

	// RecipeIDWebImageSet is the curated outcome "Web image set".
	RecipeIDWebImageSet = "recipe.web_image_set"

	// RecipeIDSocialClip is the curated outcome "Social clip".
	RecipeIDSocialClip = "recipe.social_clip"

	// RecipeIDCleanScan is the curated outcome "Clean scan".
	RecipeIDCleanScan = "recipe.clean_scan"

	// ProgressEventName is the Wails event emitted for every recipe stage.
	ProgressEventName = "recipes/progress/v1"

	// StateEventName carries a full RecipeResult snapshot when a run is
	// registered or reaches a terminal status.
	StateEventName = "recipes/state/v1"

	// retainedTerminalRuns is the number of finished recipe runs kept in the
	// in-memory registry. Active runs are always retained.
	retainedTerminalRuns = 25

	// defaultRunListLimit applies when ListRuns receives a non-positive limit.
	defaultRunListLimit = 100

	// emailPDFHistoryToolID, webImageSetHistoryToolID, socialClipHistoryToolID,
	// cleanScanHistoryToolID and burnedSubtitlesHistoryToolID are the
	// job-history tool ids used for recipe runs so the undo log can tell recipe
	// outputs apart from direct tool outputs.
	emailPDFHistoryToolID        = "recipe.email_pdf"
	webImageSetHistoryToolID     = "recipe.web_image_set"
	socialClipHistoryToolID      = "recipe.social_clip"
	cleanScanHistoryToolID       = "recipe.clean_scan"
	burnedSubtitlesHistoryToolID = "recipe.burned_subtitles"

	// recipeStepSettleTimeout bounds how long a cancelled step job may take to
	// reach its terminal state before the recipe cleans up partial outputs.
	recipeStepSettleTimeout = 10 * time.Second
)

// Service owns the curated recipe catalog and the in-memory run registry.
//
// MVP limitation: recipe jobs are not persisted. A restart loses in-flight and
// finished recipe snapshots, while the outputs themselves stay undoable through
// the shared job-history store.
type Service struct {
	registry *registry.Registry
	history  *jobhistory.Store
	emit     func(models.RecipeProgressEvent)
	onState  func(models.RecipeResult)
	engine   *jobs.Engine

	// tempDir creates the intermediate workspace for a run. It is a field so
	// tests can point it at a directory they can inspect.
	tempDir func() (string, error)

	mu   sync.RWMutex
	jobs map[string]*recipeJob
}

// NewService builds the recipe service. emit may be nil in tests or headless
// runs; history may be nil when undo is not configured.
func NewService(reg *registry.Registry, history *jobhistory.Store, emit func(models.RecipeProgressEvent)) *Service {
	if reg == nil {
		reg = registry.GetGlobalRegistry()
	}

	service := &Service{
		registry: reg,
		history:  history,
		emit:     emit,
		jobs:     make(map[string]*recipeJob),
	}
	service.tempDir = newRecipeTempDir

	return service
}

// SetEngine routes every recipe step through the shared job engine. It is wired
// by the composition root after the tooling service owns the engine.
func (s *Service) SetEngine(engine *jobs.Engine) {
	s.mu.Lock()
	s.engine = engine
	s.mu.Unlock()
}

// SetStateEmitter registers the callback that receives the full run snapshot
// when a run is registered or reaches a terminal status. It may be nil.
func (s *Service) SetStateEmitter(onState func(models.RecipeResult)) {
	s.mu.Lock()
	s.onState = onState
	s.mu.Unlock()
}

// ListRuns returns every tracked run snapshot, newest first, capped to limit.
// A non-positive limit uses defaultRunListLimit. The returned slice is never
// nil.
func (s *Service) ListRuns(limit int) []models.RecipeResult {
	if limit <= 0 {
		limit = defaultRunListLimit
	}

	s.mu.RLock()
	runs := make([]models.RecipeResult, 0, len(s.jobs))
	for _, tracked := range s.jobs {
		runs = append(runs, tracked.snapshot())
	}
	s.mu.RUnlock()

	sortRecipeRuns(runs)
	if len(runs) > limit {
		runs = runs[:limit]
	}

	return runs
}

// List returns the built-in recipes. Email PDF is always first, followed by
// Web image set, Social clip, Clean scan and Burned-in subtitles.
func (s *Service) List() []models.Recipe {
	return []models.Recipe{emailPDFRecipe(), webImageSetRecipe(), socialClipRecipe(), cleanScanRecipe(), burnSubtitlesRecipe()}
}

// Plan validates a recipe request without executing any tool and resolves the
// final destination. Merge intermediates never appear in the response.
func (s *Service) Plan(req models.RecipeRequest) models.RecipePlanResponse {
	response, _ := s.plan(req)
	return response
}

// plan resolves and validates a request once, returning the public response and
// the internal resolution used by Run.
func (s *Service) plan(req models.RecipeRequest) (models.RecipePlanResponse, *resolvedRecipe) {
	resolved, jobErr := s.resolve(req)
	if jobErr != nil {
		return recipePlanFailure(jobErr), nil
	}

	warnings := append([]string{}, resolved.Warnings...)

	// Only Email PDF delegates part of its planning to the optimize tool; the
	// Web image set plan is fully recipe-owned.
	if resolved.Recipe.ID == RecipeIDEmailPDF {
		toolWarnings, toolErr := s.optimizePlanWarnings(resolved)
		if toolErr != nil {
			return recipePlanFailure(toolErr), nil
		}
		warnings = append(warnings, toolWarnings...)
	}

	outputs := resolved.PlannedOutputs
	if outputs == nil {
		outputs = []models.PlannedOutput{}
	}

	return models.RecipePlanResponse{
		Success:    true,
		Message:    resolved.PlanMessage,
		Recipe:     resolved.Recipe,
		InputPaths: resolved.Inputs,
		OutputDir:  resolved.OutputDir,
		Outputs:    outputs,
		Warnings:   warnings,
	}, resolved
}

// Run validates the request, registers an in-memory job and executes the chain
// in the background. A malformed request fails synchronously; execution errors
// are reported through Status/progress events.
func (s *Service) Run(ctx context.Context, req models.RecipeRequest) models.RunRecipeResponse {
	if ctx == nil {
		ctx = context.Background()
	}

	plan, resolved := s.plan(req)
	if !plan.Success || resolved == nil {
		message := plan.Message
		if message == "" {
			message = "recipe plan failed"
		}
		return models.RunRecipeResponse{
			Success: false,
			Message: message,
			Status:  models.JobStatusFailed,
			Error:   plan.Error,
		}
	}

	jobID := newRecipeJobID()
	jobCtx, cancel := context.WithCancel(ctx)
	tracked := &recipeJob{
		cancel: cancel,
		result: models.RecipeResult{
			JobID:           jobID,
			RecipeID:        resolved.Recipe.ID,
			Status:          models.JobStatusQueued,
			Message:         "recipe queued",
			TotalSteps:      len(resolved.Recipe.Steps),
			Steps:           pendingRecipeSteps(resolved.Recipe.Steps),
			Outputs:         []string{},
			TargetSizeBytes: resolved.TargetSizeBytes,
			StartedAt:       time.Now().UnixMilli(),
		},
	}

	s.mu.Lock()
	s.jobs[jobID] = tracked
	s.pruneTerminalRunsLocked()
	s.mu.Unlock()

	s.emitStateSnapshot(tracked)

	go s.runJob(jobCtx, tracked, resolved)

	return models.RunRecipeResponse{
		Success: true,
		Message: "recipe submitted",
		JobID:   jobID,
		Status:  models.JobStatusQueued,
	}
}

// Status returns the current snapshot of a recipe run.
func (s *Service) Status(jobID string) models.RecipeStatusResponse {
	tracked, found := s.job(jobID)
	if !found {
		trimmed := strings.TrimSpace(jobID)
		return models.RecipeStatusResponse{
			Success: false,
			Found:   false,
			Message: fmt.Sprintf("recipe job '%s' not found", trimmed),
			Error:   newRecipeError("RECIPE_JOB_NOT_FOUND", fmt.Sprintf("recipe job '%s' not found", trimmed), nil),
		}
	}

	snapshot := tracked.snapshot()
	return models.RecipeStatusResponse{
		Success: true,
		Found:   true,
		Message: "recipe status retrieved",
		Result:  &snapshot,
	}
}

// Cancel requests cancellation. Tool executors stop at their own checkpoints;
// the job becomes cancelled once the running step returns.
func (s *Service) Cancel(jobID string) models.RecipeStatusResponse {
	tracked, found := s.job(jobID)
	if !found {
		trimmed := strings.TrimSpace(jobID)
		return models.RecipeStatusResponse{
			Success: false,
			Found:   false,
			Message: fmt.Sprintf("recipe job '%s' not found", trimmed),
			Error:   newRecipeError("RECIPE_JOB_NOT_FOUND", fmt.Sprintf("recipe job '%s' not found", trimmed), nil),
		}
	}

	if tracked.terminal() {
		snapshot := tracked.snapshot()
		return models.RecipeStatusResponse{
			Success: true,
			Found:   true,
			Message: "recipe job is already finished",
			Result:  &snapshot,
		}
	}

	tracked.requestCancel()
	snapshot := tracked.snapshot()

	return models.RecipeStatusResponse{
		Success: true,
		Found:   true,
		Message: "cancel requested",
		Result:  &snapshot,
	}
}

// Shutdown cancels every active run; it is called when the app quits.
func (s *Service) Shutdown() {
	s.mu.RLock()
	jobs := make([]*recipeJob, 0, len(s.jobs))
	for _, tracked := range s.jobs {
		jobs = append(jobs, tracked)
	}
	s.mu.RUnlock()

	for _, tracked := range jobs {
		tracked.requestCancel()
	}
}

func (s *Service) resolve(req models.RecipeRequest) (*resolvedRecipe, *models.JobError) {
	recipe := recipeByID(req.RecipeID)
	if recipe == nil {
		trimmed := strings.TrimSpace(req.RecipeID)
		return nil, newRecipeError("RECIPE_NOT_FOUND", fmt.Sprintf("recipe '%s' was not found", trimmed), nil)
	}

	switch recipe.ID {
	case RecipeIDEmailPDF:
		return resolveEmailPDF(recipe, req)
	case RecipeIDWebImageSet:
		return resolveWebImageSet(recipe, req)
	case RecipeIDSocialClip:
		return resolveSocialClip(recipe, req)
	case RecipeIDCleanScan:
		return s.resolveCleanScan(recipe, req)
	case RecipeIDBurnedSubtitles:
		return resolveBurnedSubtitles(recipe, req)
	default:
		return nil, newRecipeError("RECIPE_NOT_FOUND", fmt.Sprintf("recipe '%s' is not supported yet", recipe.ID), nil)
	}
}

func (s *Service) job(jobID string) (*recipeJob, bool) {
	s.mu.RLock()
	defer s.mu.RUnlock()

	tracked, found := s.jobs[strings.TrimSpace(jobID)]
	return tracked, found
}

func (s *Service) toolFor(toolID string) (tools.Tool, *models.JobError) {
	tool, err := s.registry.GetTool(toolID)
	if err != nil {
		return nil, newRecipeError(
			"RECIPE_TOOL_DEPENDENCY_MISSING",
			fmt.Sprintf("Tool '%s' is not available: %v", toolID, err),
			map[string]any{"toolId": toolID},
		)
	}

	return tool, nil
}

// singleExecutor resolves a tool, asserts the optional Planner capability the
// recipe chain depends on, and returns an executor that schedules the step on
// the shared job engine.
func (s *Service) singleExecutor(toolID string) (tools.SingleExecutor, tools.Planner, *models.JobError) {
	tool, jobErr := s.toolFor(toolID)
	if jobErr != nil {
		return nil, nil, jobErr
	}

	planner, plannerOK := tool.(tools.Planner)
	if !plannerOK {
		return nil, nil, newRecipeError(
			"RECIPE_TOOL_DEPENDENCY_MISSING",
			fmt.Sprintf("Tool '%s' cannot plan its output.", toolID),
			map[string]any{"toolId": toolID},
		)
	}

	if _, executorOK := tool.(tools.SingleExecutor); !executorOK {
		return nil, nil, newRecipeError(
			"RECIPE_TOOL_DEPENDENCY_MISSING",
			fmt.Sprintf("Tool '%s' does not support single-file execution.", toolID),
			map[string]any{"toolId": toolID},
		)
	}

	engine := s.sharedEngine()
	if engine == nil {
		return nil, nil, newRecipeError(
			"RECIPE_ENGINE_UNAVAILABLE",
			"The recipe job engine is not available.",
			map[string]any{"toolId": toolID},
		)
	}

	return &engineStepExecutor{service: s, engine: engine, tool: tool}, planner, nil
}

// sharedEngine returns the engine configured by SetEngine, if any.
func (s *Service) sharedEngine() *jobs.Engine {
	s.mu.RLock()
	defer s.mu.RUnlock()

	return s.engine
}

func (s *Service) runJob(ctx context.Context, tracked *recipeJob, resolved *resolvedRecipe) {
	s.execute(ctx, tracked, resolved)
}

// engineStepExecutor adapts a resolved tool to the recipe single-executor
// contract by submitting the step to the shared job engine and waiting for its
// result. The engine plans the request again at submission; the recipe keeps its
// own plan for output resolution.
type engineStepExecutor struct {
	service *Service
	engine  *jobs.Engine
	tool    tools.Tool
}

// ExecuteSingle delegates to ExecuteSingleWithProgress with no progress
// callback.
func (x *engineStepExecutor) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return x.ExecuteSingleWithProgress(ctx, req, nil)
}

// ExecuteSingleWithProgress submits one recipe step as an interactive engine
// job, waits for the step, and maps the job result back to the single-item
// contract the recipe files use.
func (x *engineStepExecutor) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	if x == nil || x.service == nil || x.engine == nil || x.tool == nil {
		jobErr := newRecipeError("RECIPE_ENGINE_UNAVAILABLE", "The recipe job engine is not available.", nil)
		return failedEngineStepItem(req, jobErr), jobErr
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		jobErr := cancelledEngineStepError(ctx)
		return failedEngineStepItem(req, jobErr), jobErr
	}

	response, submitErr := x.engine.SubmitWithOptions(ctx, req, jobs.SubmitOptions{
		Kind:       jobs.KindRecipeStep,
		Priority:   jobs.PriorityInteractive,
		Class:      jobs.ClassForTool(req.ToolID),
		OnProgress: onProgress,
	})
	if submitErr != nil {
		jobErr := models.NewCanonicalJobError(
			"RECIPE_STEP_SUBMIT_FAILED",
			submitErr.Error(),
			map[string]any{"toolId": req.ToolID},
		)
		return failedEngineStepItem(req, jobErr), jobErr
	}
	if !response.Success {
		jobErr := response.Error
		if jobErr == nil {
			jobErr = models.NewCanonicalJobError(
				"RECIPE_STEP_REJECTED",
				"The recipe step was rejected.",
				map[string]any{"toolId": req.ToolID},
			)
		}
		return failedEngineStepItem(req, jobErr), jobErr
	}

	result, waitErr := x.engine.Wait(ctx, response.JobID)
	if waitErr != nil {
		cancelErr := cancelledEngineStepError(ctx)
		_ = x.engine.Cancel(response.JobID)
		// Let the step stop before the recipe removes its partial outputs.
		settleCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), recipeStepSettleTimeout)
		defer cancel()
		_, _ = x.engine.Wait(settleCtx, response.JobID)

		return failedEngineStepItem(req, cancelErr), cancelErr
	}

	if len(result.Items) == 0 {
		jobErr := result.Error
		if jobErr == nil {
			jobErr = models.NewCanonicalJobError(
				"RECIPE_STEP_NO_RESULT",
				"The recipe step returned no result.",
				map[string]any{"toolId": req.ToolID, "jobId": response.JobID},
			)
		}
		return failedEngineStepItem(req, jobErr), jobErr
	}

	return result.Items[0], result.Error
}

// failedEngineStepItem synthesizes the per-input failure item the recipe files
// expect when a step cannot run or returns no item.
func failedEngineStepItem(req models.JobRequest, jobErr *models.JobError) models.JobResultItem {
	message := "The recipe step failed."
	if jobErr != nil && strings.TrimSpace(jobErr.Message) != "" {
		message = jobErr.Message
	}

	inputPath := ""
	if len(req.InputPaths) > 0 {
		inputPath = req.InputPaths[0]
	}

	return models.JobResultItem{
		InputPath: inputPath,
		Success:   false,
		Message:   message,
		Error:     jobErr,
	}
}

// cancelledEngineStepError is the canonical cancellation error returned when
// the recipe context ends while the step job is running.
func cancelledEngineStepError(ctx context.Context) *models.JobError {
	message := "Recipe step cancelled."
	if ctx != nil && ctx.Err() != nil {
		message = ctx.Err().Error()
	}

	return models.NewCanonicalJobError("RECIPE_STEP_CANCELLED", message, nil)
}

func (s *Service) execute(ctx context.Context, tracked *recipeJob, resolved *resolvedRecipe) {
	switch resolved.Recipe.ID {
	case RecipeIDEmailPDF:
		s.executeEmailPDF(ctx, tracked, resolved)
	case RecipeIDWebImageSet:
		s.executeWebImageSet(ctx, tracked, resolved)
	case RecipeIDSocialClip:
		s.executeSocialClip(ctx, tracked, resolved)
	case RecipeIDCleanScan:
		s.executeCleanScan(ctx, tracked, resolved)
	case RecipeIDBurnedSubtitles:
		s.executeBurnedSubtitles(ctx, tracked, resolved)
	default:
		s.failJob(tracked, newRecipeError("RECIPE_NOT_FOUND", fmt.Sprintf("recipe '%s' is not supported yet", resolved.Recipe.ID), nil))
	}
}

func (s *Service) emitProgress(tracked *recipeJob, status, stepName, stage, message string, percent int) {
	tracked.mu.Lock()
	tracked.percent = percent
	event := models.RecipeProgressEvent{
		JobID:       tracked.result.JobID,
		RecipeID:    tracked.result.RecipeID,
		Status:      status,
		CurrentStep: tracked.result.CurrentStep,
		TotalSteps:  tracked.result.TotalSteps,
		StepName:    stepName,
		Stage:       stage,
		Message:     message,
		Percent:     percent,
	}
	tracked.mu.Unlock()

	if s.emit != nil {
		s.emit(event)
	}
	if isTerminalRecipeStatus(status) {
		s.emitStateSnapshot(tracked)
	}
}

func (s *Service) emitTerminal(tracked *recipeJob, status, stepName, message string) {
	tracked.mu.RLock()
	event := models.RecipeProgressEvent{
		JobID:       tracked.result.JobID,
		RecipeID:    tracked.result.RecipeID,
		Status:      status,
		CurrentStep: tracked.result.CurrentStep,
		TotalSteps:  tracked.result.TotalSteps,
		StepName:    stepName,
		Stage:       status,
		Message:     message,
		Percent:     tracked.percent,
	}
	tracked.mu.RUnlock()

	if s.emit != nil {
		s.emit(event)
	}
	s.emitStateSnapshot(tracked)
}

// emitStateSnapshot publishes the full run snapshot for the activity list. The
// service lock only guards the emitter lookup, and the job lock is taken by
// snapshot(), so the callback never runs while a lock is held.
func (s *Service) emitStateSnapshot(tracked *recipeJob) {
	s.mu.RLock()
	onState := s.onState
	s.mu.RUnlock()

	if onState == nil {
		return
	}

	onState(tracked.snapshot())
}

// isTerminalRecipeStatus reports whether a recipe status is final. Running
// runs expose stage names, so any unknown status is treated as active.
func isTerminalRecipeStatus(status string) bool {
	switch status {
	case models.JobStatusSuccess,
		models.JobStatusPartialSuccess,
		models.JobStatusFailed,
		models.JobStatusCancelled,
		models.JobStatusInterrupted:
		return true
	default:
		return false
	}
}

// sortRecipeRuns orders run snapshots newest first, breaking ties by job id so
// list output stays deterministic.
func sortRecipeRuns(runs []models.RecipeResult) {
	sort.Slice(runs, func(i, j int) bool {
		if runs[i].StartedAt != runs[j].StartedAt {
			return runs[i].StartedAt > runs[j].StartedAt
		}
		return runs[i].JobID > runs[j].JobID
	})
}

// pruneTerminalRunsLocked drops the oldest terminal runs beyond
// retainedTerminalRuns. Non-terminal runs are never pruned.
func (s *Service) pruneTerminalRunsLocked() {
	type terminalRunRef struct {
		jobID     string
		startedAt int64
	}

	terminal := make([]terminalRunRef, 0, len(s.jobs))
	for jobID, tracked := range s.jobs {
		tracked.mu.RLock()
		status := tracked.result.Status
		startedAt := tracked.result.StartedAt
		tracked.mu.RUnlock()

		if !isTerminalRecipeStatus(status) {
			continue
		}
		terminal = append(terminal, terminalRunRef{jobID: jobID, startedAt: startedAt})
	}
	if len(terminal) <= retainedTerminalRuns {
		return
	}

	sort.Slice(terminal, func(i, j int) bool {
		if terminal[i].startedAt != terminal[j].startedAt {
			return terminal[i].startedAt > terminal[j].startedAt
		}
		return terminal[i].jobID > terminal[j].jobID
	})

	for _, ref := range terminal[retainedTerminalRuns:] {
		delete(s.jobs, ref.jobID)
	}
}

func (s *Service) cancelJob(tracked *recipeJob) {
	message := "Recipe cancelled."
	stepName := tracked.currentStepName()
	jobErr := models.NewJobError(models.ErrorCodeCancelledByUser, "RECIPE_CANCELLED", message, nil)

	tracked.finish(models.JobStatusCancelled, message, []string{}, nil, jobErr, false)
	s.emitTerminal(tracked, models.JobStatusCancelled, stepName, message)
}

func (s *Service) failJob(tracked *recipeJob, jobErr *models.JobError) {
	message := "The recipe failed."
	if jobErr != nil && jobErr.Message != "" {
		message = jobErr.Message
	}
	stepName := tracked.currentStepName()

	tracked.finish(models.JobStatusFailed, message, []string{}, nil, jobErr, false)
	s.emitTerminal(tracked, models.JobStatusFailed, stepName, message)
}

// recipeJob is the mutable in-memory state of one recipe run.
type recipeJob struct {
	mu      sync.RWMutex
	result  models.RecipeResult
	cancel  context.CancelFunc
	percent int
}

func (j *recipeJob) snapshot() models.RecipeResult {
	j.mu.RLock()
	defer j.mu.RUnlock()

	snapshot := j.result
	snapshot.Steps = make([]models.RecipeStepResult, len(j.result.Steps))
	for index, step := range j.result.Steps {
		copied := step
		copied.Outputs = copyStrings(step.Outputs)
		snapshot.Steps[index] = copied
	}
	snapshot.Outputs = copyStrings(j.result.Outputs)
	if j.result.Verification != nil {
		verification := *j.result.Verification
		verification.Outputs = append([]models.VerifiedOutput{}, j.result.Verification.Outputs...)
		verification.Warnings = copyStrings(j.result.Verification.Warnings)
		snapshot.Verification = &verification
	}

	return snapshot
}

func (j *recipeJob) begin(stepIndex int) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.result.Status = models.JobStatusRunning
	j.result.Message = "recipe running"
	j.result.CurrentStep = stepIndex
}

func (j *recipeJob) setCurrentStep(stepIndex int) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.result.CurrentStep = stepIndex
}

func (j *recipeJob) setStep(index int, step models.RecipeStepResult) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if index < 0 || index >= len(j.result.Steps) {
		return
	}
	j.result.Steps[index] = step
}

func (j *recipeJob) finish(status, message string, outputs []string, verification *models.VerificationReport, jobErr *models.JobError, completed bool) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.result.Status = status
	j.result.Message = message
	if outputs != nil {
		j.result.Outputs = outputs
	}
	j.result.Verification = verification
	j.result.Error = jobErr
	j.result.EndedAt = time.Now().UnixMilli()
	if completed && j.result.TotalSteps > 0 {
		j.result.CurrentStep = j.result.TotalSteps
	}
}

func (j *recipeJob) terminal() bool {
	j.mu.RLock()
	defer j.mu.RUnlock()

	return isTerminalRecipeStatus(j.result.Status)
}

func (j *recipeJob) jobID() string {
	j.mu.RLock()
	defer j.mu.RUnlock()

	return j.result.JobID
}

func (j *recipeJob) requestCancel() {
	j.mu.RLock()
	cancel := j.cancel
	j.mu.RUnlock()

	if cancel != nil {
		cancel()
	}
}

func (j *recipeJob) currentStepName() string {
	j.mu.RLock()
	defer j.mu.RUnlock()

	index := j.result.CurrentStep - 1
	if index < 0 && len(j.result.Steps) > 0 {
		index = 0
	}
	if index >= 0 && index < len(j.result.Steps) {
		return j.result.Steps[index].Name
	}

	return ""
}

func newRecipeJobID() string {
	return fmt.Sprintf("recipe_%d", time.Now().UnixNano())
}

func pendingRecipeSteps(steps []models.RecipeStep) []models.RecipeStepResult {
	results := make([]models.RecipeStepResult, 0, len(steps))
	for _, step := range steps {
		results = append(results, models.RecipeStepResult{
			ID:     step.ID,
			Name:   step.Name,
			ToolID: step.ToolID,
			Status: recipeStepStatusPending,
		})
	}

	return results
}

func copyStrings(values []string) []string {
	return append([]string{}, values...)
}

func newRecipeError(detailCode, message string, details map[string]any) *models.JobError {
	return models.NewCanonicalJobError(detailCode, message, details)
}
