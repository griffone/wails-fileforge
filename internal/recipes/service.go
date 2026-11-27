// Package recipes implements the curated outcome recipes that chain the
// existing tools into one verified result. Phase 1 ships built-in recipes for
// Email PDF (linear: merge when needed, optimize to a target size), Web image
// set (fan-out: responsive WebP/JPEG variants plus a ZIP and srcset snippet),
// Social clip (fan-out: MP4 + WebM encodes plus a poster frame), Clean scan
// (local OCR into a searchable PDF plus a text sidecar), Burned-in subtitles
// (local Whisper transcription burned into a new MP4 plus an SRT sidecar) and
// Archive document (DOCX to PDF/A-2 plus its SHA-256). All of them verify their
// outputs and record them for undo.
package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/jobs"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/jsonstore"
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
	ProgressEventName = "recipes/progress"

	// StateEventName carries a full RecipeResult snapshot when a run is
	// registered or reaches a terminal status.
	StateEventName = "recipes/state"

	// retainedTerminalRuns is the number of finished recipe runs kept in the
	// in-memory registry and in the persisted snapshot, matching the job
	// engine's retainedTerminalJobs. Active runs are always retained.
	retainedTerminalRuns = 25

	// defaultRunListLimit applies when ListRuns receives a non-positive limit.
	defaultRunListLimit = 100

	// recipeRunsFileName is the run snapshot written next to the job engine and
	// job history stores.
	recipeRunsFileName = "recipe_runs.json"

	// recipeRunsVersion versions the persisted snapshot so a future shape change
	// can migrate instead of guessing.
	recipeRunsVersion = 1

	// recipeInterruptedMessage is used for a non-terminal run found after a
	// restart, matching the job engine's recovery wording.
	recipeInterruptedMessage = "recipe run interrupted after restart"

	// emailPDFHistoryToolID, webImageSetHistoryToolID, socialClipHistoryToolID,
	// cleanScanHistoryToolID and burnedSubtitlesHistoryToolID are the
	// job-history tool ids used for recipe runs so the undo log can tell recipe
	// outputs apart from direct tool outputs.
	emailPDFHistoryToolID        = "recipe.email_pdf"
	webImageSetHistoryToolID     = "recipe.web_image_set"
	socialClipHistoryToolID      = "recipe.social_clip"
	cleanScanHistoryToolID       = "recipe.clean_scan"
	burnedSubtitlesHistoryToolID = "recipe.burned_subtitles"
	archiveDocumentHistoryToolID = "recipe.archive_document"

	// recipeStepSettleTimeout bounds how long a cancelled step job may take to
	// reach its terminal state before the recipe cleans up partial outputs.
	recipeStepSettleTimeout = 10 * time.Second
)

// Service owns the curated recipe catalog and the run registry. When a
// persistence path is configured, run snapshots are written through the shared
// atomic JSON store; otherwise the registry is in-memory only. Terminal runs
// stay undoable through the shared job-history store regardless.
type Service struct {
	registry *registry.Registry
	history  *jobhistory.Store
	emit     func(models.RecipeProgressEvent)
	onState  func(models.RecipeResult)
	engine   *jobs.Engine

	// tempDir creates the intermediate workspace for a run. It is a field so
	// tests can point it at a directory they can inspect.
	tempDir func() (string, error)

	// path and store are empty/nil when persistence is disabled.
	path  string
	store *jsonstore.Store

	// persistMu serializes snapshot encoding and writes so concurrent terminal
	// updates cannot interleave or land out of order.
	persistMu sync.Mutex

	mu   sync.RWMutex
	jobs map[string]*recipeJob

	// userRecipes is the persisted duplicate-and-edit catalog. It is empty and
	// unsaved when no user store path is configured.
	userMu      sync.RWMutex
	userPath    string
	userStore   *jsonstore.Store
	userRecipes []userRecipe
}

// NewService builds the recipe service without persistence. emit may be nil in
// tests or headless runs; history may be nil when undo is not configured.
func NewService(reg *registry.Registry, history *jobhistory.Store, emit func(models.RecipeProgressEvent)) *Service {
	return newService(reg, history, emit, "")
}

// NewServiceWithPersistence builds the recipe service with a persisted run
// registry at path. An empty path disables persistence, so headless callers and
// tests keep the in-memory behavior.
func NewServiceWithPersistence(reg *registry.Registry, history *jobhistory.Store, emit func(models.RecipeProgressEvent), path string) *Service {
	return newService(reg, history, emit, path)
}

func newService(reg *registry.Registry, history *jobhistory.Store, emit func(models.RecipeProgressEvent), path string) *Service {
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
	service.restoreRuns(path)

	return service
}

// DefaultPath returns the persisted run location:
// os.UserConfigDir()/fileforge/recipe_runs.json, with a repo-local fallback
// when the platform config directory is unavailable. It mirrors the job engine
// and job history paths.
func DefaultPath() string {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		return filepath.Join(".", ".fileforge", recipeRunsFileName)
	}

	return filepath.Join(configDir, "fileforge", recipeRunsFileName)
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

// List returns the built-in recipes followed by the saved user recipes. Email
// PDF is always first. User recipes carry their base recipe id, their stored
// overrides and the duplicable marker; built-ins without a schema are not
// duplicable.
func (s *Service) List() []models.Recipe {
	builtins := []models.Recipe{emailPDFRecipe(), webImageSetRecipe(), socialClipRecipe(), cleanScanRecipe(), burnSubtitlesRecipe(), archiveDocumentRecipe()}
	users := s.userRecipeModels()

	recipes := make([]models.Recipe, 0, len(builtins)+len(users))
	recipes = append(recipes, builtins...)
	recipes = append(recipes, users...)

	return recipes
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
	if resolved.BaseRecipe != nil && resolved.BaseRecipe.ID == RecipeIDEmailPDF {
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

// Shutdown cancels every active run; it is called when the app quits. The
// current registry is written synchronously so a crash right after shutdown
// still recovers the latest state.
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

	s.persistNow()
}

func (s *Service) resolve(req models.RecipeRequest) (*resolvedRecipe, *models.JobError) {
	base, display, effective, jobErr := s.resolveRecipeRequest(req)
	if jobErr != nil {
		return nil, jobErr
	}

	request := req
	request.Options = effective

	var resolved *resolvedRecipe
	switch base.ID {
	case RecipeIDEmailPDF:
		resolved, jobErr = resolveEmailPDF(base, request)
	case RecipeIDWebImageSet:
		resolved, jobErr = resolveWebImageSet(base, request)
	case RecipeIDSocialClip:
		resolved, jobErr = resolveSocialClip(base, request)
	case RecipeIDCleanScan:
		resolved, jobErr = s.resolveCleanScan(base, request)
	case RecipeIDBurnedSubtitles:
		resolved, jobErr = resolveBurnedSubtitles(base, request)
	case RecipeIDArchiveDocument:
		resolved, jobErr = s.resolveArchiveDocument(base, request)
	default:
		return nil, newRecipeError("RECIPE_NOT_FOUND", fmt.Sprintf("recipe '%s' is not supported yet", base.ID), nil)
	}
	if jobErr != nil {
		return nil, jobErr
	}

	resolved.BaseRecipe = base
	resolved.Recipe = display

	return resolved, nil
}

// resolveRecipeRequest maps a request id to the built-in base recipe, the
// recipe shown to the caller and the effective option values. A built-in
// request keeps its caller options; a user recipe applies its stored overrides
// after validating them against the base schema.
func (s *Service) resolveRecipeRequest(req models.RecipeRequest) (*models.Recipe, *models.Recipe, map[string]any, *models.JobError) {
	if builtin := recipeByID(req.RecipeID); builtin != nil {
		return builtin, builtin, req.Options, nil
	}

	stored, found := s.userRecipeByID(req.RecipeID)
	if !found {
		trimmed := strings.TrimSpace(req.RecipeID)
		return nil, nil, nil, newRecipeError("RECIPE_NOT_FOUND", fmt.Sprintf("recipe '%s' was not found", trimmed), nil)
	}

	base := recipeByID(stored.BaseRecipeID)
	if base == nil {
		return nil, nil, nil, newRecipeError(
			"RECIPE_BASE_NOT_FOUND",
			fmt.Sprintf("User recipe '%s' references base recipe '%s', which is no longer available. Delete or recreate it.", stored.Name, stored.BaseRecipeID),
			map[string]any{"recipeId": stored.ID, "baseRecipeId": stored.BaseRecipeID},
		)
	}

	overrides, optionsErr := validateRecipeOptions(base, stored.Options)
	if optionsErr != nil {
		return nil, nil, nil, optionsErr
	}

	return base, userRecipeModel(base, stored), overrides, nil
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
	base := resolved.BaseRecipe
	if base == nil {
		base = resolved.Recipe
	}

	switch base.ID {
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
	case RecipeIDArchiveDocument:
		s.executeArchiveDocument(ctx, tracked, resolved)
	default:
		s.failJob(tracked, newRecipeError("RECIPE_NOT_FOUND", fmt.Sprintf("recipe '%s' is not supported yet", base.ID), nil))
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

// emitStateSnapshot publishes the full run snapshot for the activity list and
// persists the registry. The service lock only guards the emitter lookup, and
// the job lock is taken by snapshot(), so the callback never runs while a lock
// is held.
func (s *Service) emitStateSnapshot(tracked *recipeJob) {
	s.persistRuns()

	s.mu.RLock()
	onState := s.onState
	s.mu.RUnlock()

	if onState == nil {
		return
	}

	onState(tracked.snapshot())
}

// setWorkspace records the intermediate workspace for a run and persists the
// registry so a restart can clean a directory that outlived the process.
func (s *Service) setWorkspace(tracked *recipeJob, workspace string) {
	tracked.setWorkspace(workspace)
	s.persistRuns()
}

// persistedRecipeRun is one run as written to disk. Workspace is stored beside
// the public snapshot because only the recipe service needs it: it is cleaned
// best-effort after a restart and never exposed to the UI.
type persistedRecipeRun struct {
	Result    models.RecipeResult `json:"result"`
	Workspace string              `json:"workspace,omitempty"`
}

// persistedRecipeRuns versions the on-disk payload.
type persistedRecipeRuns struct {
	Version int                  `json:"version"`
	Runs    []persistedRecipeRun `json:"runs"`
}

// restoreRuns loads the persisted registry. A missing file is normal, while a
// corrupt or unreadable file is logged and ignored so startup never fails.
// Terminal runs are restored unchanged; every non-terminal run becomes
// interrupted and is never resumed.
func (s *Service) restoreRuns(path string) {
	path = strings.TrimSpace(path)
	if path == "" {
		return
	}

	s.path = path
	s.store = jsonstore.New(path, jsonstore.Options{
		Debounce: jsonstore.DefaultDebounce,
		OnError: func(err error) {
			log.Printf("recipes.persistence.write.failed path=%s err=%v", path, err)
		},
	})

	info, err := os.Stat(path)
	if err != nil {
		if !errors.Is(err, os.ErrNotExist) {
			log.Printf("recipes.persistence.load.failed path=%s err=%v", path, err)
		}
		return
	}
	if info.Size() == 0 {
		return
	}

	var persisted persistedRecipeRuns
	if err := jsonstore.Load(path, &persisted); err != nil {
		log.Printf("recipes.persistence.load.failed path=%s err=%v", path, err)
		return
	}

	now := time.Now().UnixMilli()
	restored := make(map[string]*recipeJob, len(persisted.Runs))
	for _, entry := range persisted.Runs {
		result := entry.Result
		jobID := strings.TrimSpace(result.JobID)
		if jobID == "" {
			continue
		}
		result.JobID = jobID

		if !isTerminalRecipeStatus(result.Status) {
			result.Status = models.JobStatusInterrupted
			result.Message = recipeInterruptedMessage
			result.EndedAt = now
			result.Error = newRecipeError("RECIPE_INTERRUPTED", recipeInterruptedMessage, nil)
		}

		restored[jobID] = &recipeJob{result: result, workspace: strings.TrimSpace(entry.Workspace)}
	}
	if len(restored) == 0 {
		return
	}

	s.mu.Lock()
	for jobID, tracked := range restored {
		s.jobs[jobID] = tracked
	}
	s.pruneTerminalRunsLocked()
	s.mu.Unlock()

	for _, tracked := range restored {
		s.cleanupRestoredWorkspace(tracked.workspacePath())
	}

	// Write the normalized registry back so interrupted runs stay interrupted
	// and pruned runs do not reappear on the next start.
	s.persistNow()
}

// workspaceTempPrefix mirrors newRecipeTempDir: only directories the recipe
// service created carry it.
const workspaceTempPrefix = "fileforge-recipe-"

// isRemovableWorkspace reports whether path is a recipe workspace this process
// may delete. A persisted path is untrusted input: a corrupt or hand-edited
// registry must never turn into an arbitrary RemoveAll, so cleanup stays inside
// the system temp root and only touches directories with the recipe prefix.
func isRemovableWorkspace(workspace string) bool {
	trimmed := strings.TrimSpace(workspace)
	if trimmed == "" {
		return false
	}

	clean := filepath.Clean(trimmed)
	if !strings.HasPrefix(filepath.Base(clean), workspaceTempPrefix) {
		return false
	}

	root := filepath.Clean(os.TempDir())
	rel, err := filepath.Rel(root, clean)
	if err != nil {
		return false
	}
	if rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return false
	}

	return true
}

// cleanupRestoredWorkspace removes an intermediate workspace left behind by a
// restart. It only ever deletes a directory: final outputs live outside the
// workspace, a missing path is a no-op, and an unsafe path is never touched.
func (s *Service) cleanupRestoredWorkspace(workspace string) {
	workspace = strings.TrimSpace(workspace)
	if workspace == "" {
		return
	}
	if !isRemovableWorkspace(workspace) {
		log.Printf("recipes.workspace.cleanup.skipped path=%s reason=unsafe", workspace)
		return
	}

	info, err := os.Stat(workspace)
	if err != nil || !info.IsDir() {
		return
	}
	if err := os.RemoveAll(workspace); err != nil {
		log.Printf("recipes.workspace.cleanup.failed path=%s err=%v", workspace, err)
	}
}

// persistRuns schedules a debounced registry write. It is a no-op without a
// configured path.
func (s *Service) persistRuns() {
	if err := s.saveSnapshot(false); err != nil && !errors.Is(err, jsonstore.ErrClosed) {
		log.Printf("recipes.persistence.enqueue.failed path=%s err=%v", s.path, err)
	}
}

// persistNow writes the registry immediately.
func (s *Service) persistNow() {
	if err := s.saveSnapshot(true); err != nil && !errors.Is(err, jsonstore.ErrClosed) {
		log.Printf("recipes.persistence.write.failed path=%s err=%v", s.path, err)
	}
}

// saveSnapshot encodes one consistent registry snapshot and hands it to the
// debounced store. Immediate writes are used after recovery and on shutdown.
// A debounced write passes the encoder itself: the store runs it only when the
// window elapses, so coalesced step-progress events never encode a snapshot.
func (s *Service) saveSnapshot(immediate bool) error {
	s.persistMu.Lock()
	defer s.persistMu.Unlock()

	if s.store == nil {
		return nil
	}

	if !immediate {
		return s.store.SaveFunc(s.encodeSnapshot)
	}

	payload, err := s.encodeSnapshot()
	if err != nil {
		return fmt.Errorf("encode recipe runs snapshot: %w", err)
	}

	return s.store.SaveNow(payload)
}

// encodeSnapshot deep-copies every tracked run newest first. Callers must not
// hold the service lock.
func (s *Service) encodeSnapshot() ([]byte, error) {
	s.mu.RLock()
	runs := make([]persistedRecipeRun, 0, len(s.jobs))
	for _, tracked := range s.jobs {
		runs = append(runs, persistedRecipeRun{
			Result:    tracked.snapshot(),
			Workspace: tracked.workspacePath(),
		})
	}
	s.mu.RUnlock()

	sort.Slice(runs, func(i, j int) bool {
		if runs[i].Result.StartedAt != runs[j].Result.StartedAt {
			return runs[i].Result.StartedAt > runs[j].Result.StartedAt
		}
		return runs[i].Result.JobID > runs[j].Result.JobID
	})

	return json.MarshalIndent(persistedRecipeRuns{Version: recipeRunsVersion, Runs: runs}, "", "  ")
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
	mu        sync.RWMutex
	result    models.RecipeResult
	cancel    context.CancelFunc
	percent   int
	workspace string
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

// setWorkspace records the intermediate workspace used by this run.
func (j *recipeJob) setWorkspace(workspace string) {
	j.mu.Lock()
	defer j.mu.Unlock()

	j.workspace = strings.TrimSpace(workspace)
}

// workspacePath returns the intermediate workspace, if one was created.
func (j *recipeJob) workspacePath() string {
	j.mu.RLock()
	defer j.mu.RUnlock()

	return j.workspace
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
