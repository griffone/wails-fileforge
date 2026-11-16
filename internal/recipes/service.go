// Package recipes implements the curated outcome recipes that chain the
// existing tools into one verified result. Phase 1 ships a single built-in
// recipe (Email PDF) and a linear runner: merge when needed, optimize to a
// target size, verify the output and record it for undo.
package recipes

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"time"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/tools"
)

const (
	// RecipeIDEmailPDF is the curated outcome "Email PDF".
	RecipeIDEmailPDF = "recipe.email_pdf"

	// ProgressEventNameV1 is the Wails event emitted for every recipe stage.
	ProgressEventNameV1 = "recipes/progress/v1"

	// emailPDFHistoryToolID is the job-history tool id used for recipe runs so
	// the undo log can tell recipe outputs apart from direct tool outputs.
	emailPDFHistoryToolID = "recipe.email_pdf"

	maxConcurrentJobs = 2
)

// Service owns the curated recipe catalog and the in-memory run registry.
//
// MVP limitation: recipe jobs are not persisted. A restart loses in-flight and
// finished recipe snapshots, while the outputs themselves stay undoable through
// the shared job-history store.
type Service struct {
	registry *registry.Registry
	history  *jobhistory.Store
	emit     func(models.RecipeProgressEventV1)

	slots chan struct{}

	// tempDir creates the intermediate workspace for a run. It is a field so
	// tests can point it at a directory they can inspect.
	tempDir func() (string, error)

	mu   sync.RWMutex
	jobs map[string]*recipeJob
}

// NewService builds the recipe service. emit may be nil in tests or headless
// runs; history may be nil when undo is not configured.
func NewService(reg *registry.Registry, history *jobhistory.Store, emit func(models.RecipeProgressEventV1)) *Service {
	if reg == nil {
		reg = registry.GetGlobalRegistry()
	}

	service := &Service{
		registry: reg,
		history:  history,
		emit:     emit,
		slots:    make(chan struct{}, maxConcurrentJobs),
		jobs:     make(map[string]*recipeJob),
	}
	service.tempDir = newRecipeTempDir

	return service
}

// List returns the built-in recipes. Email PDF is the only curated recipe for
// now and is always first.
func (s *Service) List() []models.RecipeV1 {
	return []models.RecipeV1{emailPDFRecipe()}
}

// Plan validates a recipe request without executing any tool and resolves the
// final destination. Merge intermediates never appear in the response.
func (s *Service) Plan(req models.RecipeRequestV1) models.RecipePlanResponseV1 {
	response, _ := s.plan(req)
	return response
}

// plan resolves and validates a request once, returning the public response and
// the internal resolution used by Run.
func (s *Service) plan(req models.RecipeRequestV1) (models.RecipePlanResponseV1, *resolvedRecipe) {
	resolved, jobErr := s.resolve(req)
	if jobErr != nil {
		return recipePlanFailure(jobErr), nil
	}

	toolWarnings, toolErr := s.optimizePlanWarnings(resolved)
	if toolErr != nil {
		return recipePlanFailure(toolErr), nil
	}

	warnings := append([]string{}, resolved.Warnings...)
	warnings = append(warnings, toolWarnings...)

	return models.RecipePlanResponseV1{
		Success:    true,
		Message:    emailPDFPlanMessage(len(resolved.Inputs)),
		Recipe:     resolved.Recipe,
		InputPaths: resolved.Inputs,
		OutputDir:  resolved.OutputDir,
		Outputs: []models.PlannedOutputV1{{
			InputPath:   strings.Join(resolved.Inputs, ","),
			OutputPath:  resolved.FinalPath,
			OutputKind:  "file",
			OutputCount: 1,
		}},
		Warnings: warnings,
	}, resolved
}

// Run validates the request, registers an in-memory job and executes the chain
// in the background. A malformed request fails synchronously; execution errors
// are reported through Status/progress events.
func (s *Service) Run(ctx context.Context, req models.RecipeRequestV1) models.RunRecipeResponseV1 {
	if ctx == nil {
		ctx = context.Background()
	}

	plan, resolved := s.plan(req)
	if !plan.Success || resolved == nil {
		message := plan.Message
		if message == "" {
			message = "recipe plan failed"
		}
		return models.RunRecipeResponseV1{
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
		result: models.RecipeResultV1{
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
	s.mu.Unlock()

	go s.runJob(jobCtx, tracked, resolved)

	return models.RunRecipeResponseV1{
		Success: true,
		Message: "recipe submitted",
		JobID:   jobID,
		Status:  models.JobStatusQueued,
	}
}

// Status returns the current snapshot of a recipe run.
func (s *Service) Status(jobID string) models.RecipeStatusResponseV1 {
	tracked, found := s.job(jobID)
	if !found {
		trimmed := strings.TrimSpace(jobID)
		return models.RecipeStatusResponseV1{
			Success: false,
			Found:   false,
			Message: fmt.Sprintf("recipe job '%s' not found", trimmed),
			Error:   newRecipeError("RECIPE_JOB_NOT_FOUND", fmt.Sprintf("recipe job '%s' not found", trimmed), nil),
		}
	}

	snapshot := tracked.snapshot()
	return models.RecipeStatusResponseV1{
		Success: true,
		Found:   true,
		Message: "recipe status retrieved",
		Result:  &snapshot,
	}
}

// Cancel requests cancellation. Tool executors stop at their own checkpoints;
// the job becomes cancelled once the running step returns.
func (s *Service) Cancel(jobID string) models.RecipeStatusResponseV1 {
	tracked, found := s.job(jobID)
	if !found {
		trimmed := strings.TrimSpace(jobID)
		return models.RecipeStatusResponseV1{
			Success: false,
			Found:   false,
			Message: fmt.Sprintf("recipe job '%s' not found", trimmed),
			Error:   newRecipeError("RECIPE_JOB_NOT_FOUND", fmt.Sprintf("recipe job '%s' not found", trimmed), nil),
		}
	}

	if tracked.terminal() {
		snapshot := tracked.snapshot()
		return models.RecipeStatusResponseV1{
			Success: true,
			Found:   true,
			Message: "recipe job is already finished",
			Result:  &snapshot,
		}
	}

	tracked.requestCancel()
	snapshot := tracked.snapshot()

	return models.RecipeStatusResponseV1{
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

func (s *Service) resolve(req models.RecipeRequestV1) (*resolvedRecipe, *models.JobErrorV1) {
	recipe := recipeByID(req.RecipeID)
	if recipe == nil {
		trimmed := strings.TrimSpace(req.RecipeID)
		return nil, newRecipeError("RECIPE_NOT_FOUND", fmt.Sprintf("recipe '%s' was not found", trimmed), nil)
	}

	switch recipe.ID {
	case RecipeIDEmailPDF:
		return resolveEmailPDF(recipe, req)
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

func (s *Service) toolFor(toolID string) (tools.Tool, *models.JobErrorV1) {
	tool, err := s.registry.GetToolV2(toolID)
	if err != nil {
		return nil, newRecipeError(
			"RECIPE_TOOL_DEPENDENCY_MISSING",
			fmt.Sprintf("Tool '%s' is not available: %v", toolID, err),
			map[string]any{"toolId": toolID},
		)
	}

	return tool, nil
}

// singleExecutor resolves a tool and asserts the optional Planner and
// SingleExecutor capabilities the recipe chain depends on.
func (s *Service) singleExecutor(toolID string) (tools.SingleExecutor, tools.Planner, *models.JobErrorV1) {
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

	executor, executorOK := tool.(tools.SingleExecutor)
	if !executorOK {
		return nil, nil, newRecipeError(
			"RECIPE_TOOL_DEPENDENCY_MISSING",
			fmt.Sprintf("Tool '%s' does not support single-file execution.", toolID),
			map[string]any{"toolId": toolID},
		)
	}

	return executor, planner, nil
}

func (s *Service) runJob(ctx context.Context, tracked *recipeJob, resolved *resolvedRecipe) {
	select {
	case s.slots <- struct{}{}:
		defer func() { <-s.slots }()
	case <-ctx.Done():
		s.cancelJob(tracked)
		return
	}

	s.execute(ctx, tracked, resolved)
}

func (s *Service) execute(ctx context.Context, tracked *recipeJob, resolved *resolvedRecipe) {
	switch resolved.Recipe.ID {
	case RecipeIDEmailPDF:
		s.executeEmailPDF(ctx, tracked, resolved)
	default:
		s.failJob(tracked, newRecipeError("RECIPE_NOT_FOUND", fmt.Sprintf("recipe '%s' is not supported yet", resolved.Recipe.ID), nil))
	}
}

func (s *Service) emitProgress(tracked *recipeJob, status, stepName, stage, message string, percent int) {
	if s.emit == nil {
		return
	}

	tracked.mu.Lock()
	tracked.percent = percent
	event := models.RecipeProgressEventV1{
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

	s.emit(event)
}

func (s *Service) emitTerminal(tracked *recipeJob, status, stepName, message string) {
	if s.emit == nil {
		return
	}

	tracked.mu.RLock()
	event := models.RecipeProgressEventV1{
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

	s.emit(event)
}

func (s *Service) cancelJob(tracked *recipeJob) {
	message := "Recipe cancelled."
	stepName := tracked.currentStepName()
	jobErr := models.NewJobError(models.ErrorCodeCancelledByUser, "RECIPE_CANCELLED", message, nil)

	tracked.finish(models.JobStatusCancelled, message, []string{}, nil, jobErr, false)
	s.emitTerminal(tracked, models.JobStatusCancelled, stepName, message)
}

func (s *Service) failJob(tracked *recipeJob, jobErr *models.JobErrorV1) {
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
	result  models.RecipeResultV1
	cancel  context.CancelFunc
	percent int
}

func (j *recipeJob) snapshot() models.RecipeResultV1 {
	j.mu.RLock()
	defer j.mu.RUnlock()

	snapshot := j.result
	snapshot.Steps = make([]models.RecipeStepResultV1, len(j.result.Steps))
	for index, step := range j.result.Steps {
		copied := step
		copied.Outputs = copyStrings(step.Outputs)
		snapshot.Steps[index] = copied
	}
	snapshot.Outputs = copyStrings(j.result.Outputs)
	if j.result.Verification != nil {
		verification := *j.result.Verification
		verification.Outputs = append([]models.VerifiedOutputV1{}, j.result.Verification.Outputs...)
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

func (j *recipeJob) setStep(index int, step models.RecipeStepResultV1) {
	j.mu.Lock()
	defer j.mu.Unlock()

	if index < 0 || index >= len(j.result.Steps) {
		return
	}
	j.result.Steps[index] = step
}

func (j *recipeJob) finish(status, message string, outputs []string, verification *models.VerificationReportV1, jobErr *models.JobErrorV1, completed bool) {
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

	switch j.result.Status {
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

func pendingRecipeSteps(steps []models.RecipeStepV1) []models.RecipeStepResultV1 {
	results := make([]models.RecipeStepResultV1, 0, len(steps))
	for _, step := range steps {
		results = append(results, models.RecipeStepResultV1{
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

func newRecipeError(detailCode, message string, details map[string]any) *models.JobErrorV1 {
	return models.NewCanonicalJobError(detailCode, message, details)
}
