package services

import (
	"context"
	"errors"
	"fmt"
	"log"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/jobs"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/eventbus"
	"fileforge-desktop/internal/platform/logx"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/verify"
)

const (
	JobProgressEventName = "jobs/progress/v1"

	// JobStateEventName carries a full JobResult snapshot whenever a job is
	// submitted or reaches a terminal status.
	JobStateEventName = "jobs/state/v1"
)

type ToolingService struct {
	ctx      context.Context
	registry *registry.Registry
	engine   *jobs.Engine
	history  *jobhistory.Store
	bus      eventbus.Bus
}

// NewToolingService builds the tooling service without an event bus. Events are
// dropped; use NewToolingServiceWithBus when the caller can deliver them.
func NewToolingService(reg *registry.Registry) *ToolingService {
	return newToolingService(reg, jobhistory.NewStore(jobhistory.DefaultPath()))
}

// NewToolingServiceWithBus builds the tooling service with an event bus so
// progress and state events reach the UI without importing the desktop runtime.
func NewToolingServiceWithBus(reg *registry.Registry, bus eventbus.Bus) *ToolingService {
	return newToolingServiceWithBus(reg, jobhistory.NewStore(jobhistory.DefaultPath()), bus)
}

func newToolingService(reg *registry.Registry, history *jobhistory.Store) *ToolingService {
	return newToolingServiceWithBus(reg, history, nil)
}

func newToolingServiceWithBus(reg *registry.Registry, history *jobhistory.Store, bus eventbus.Bus) *ToolingService {
	if reg == nil {
		reg = registry.GetGlobalRegistry()
	}
	if history == nil {
		history = jobhistory.NewStore(jobhistory.DefaultPath())
	}

	service := &ToolingService{
		registry: reg,
		history:  history,
		bus:      bus,
	}
	service.engine = jobs.NewEngine(reg, jobs.Options{
		Limits:          jobs.DefaultLimits(),
		PersistencePath: "", // configured by SetContext once the app context exists
		Emitter:         service,
		OnJobComplete:   service.recordTerminalJob,
		Logger:          logx.New(slog.LevelInfo),
	})

	return service
}

// SetContext binds the tooling service to the Wails application lifecycle.
// Persistence and interrupted-job recovery only become active here, so service
// tests that never call SetContext never touch the user config directory.
func (s *ToolingService) SetContext(ctx context.Context) {
	s.ctx = ctx
	s.engine.SetBaseContext(ctx)

	storePath := defaultJobsPersistencePath()
	if err := s.engine.SetPersistencePath(storePath); err != nil {
		log.Printf("tooling.persistence.failed path=%s err=%v", storePath, err)
	}
	if err := s.engine.Recover(ctx); err != nil {
		log.Printf("tooling.recovery.failed path=%s err=%v", storePath, err)
	} else {
		log.Printf("tooling.recovery.ok path=%s", storePath)
	}
}

// Shutdown stops the job engine, cancelling queued and running jobs and
// flushing the persisted snapshot.
func (s *ToolingService) Shutdown(ctx context.Context) error {
	return s.engine.Shutdown(ctx)
}

// Engine exposes the shared job engine so other services can schedule their
// work on the same scheduler. It is nil-safe for uninitialized services.
func (s *ToolingService) Engine() *jobs.Engine {
	if s == nil {
		return nil
	}

	return s.engine
}

// Registry exposes the tool registry so the app preview endpoints can resolve
// the concrete tools they render. It is nil-safe for uninitialized services.
func (s *ToolingService) Registry() *registry.Registry {
	if s == nil {
		return nil
	}

	return s.registry
}

// JobProgress emits a coalesced progress event. It implements jobs.Emitter.
// Recipe-step jobs are internal and never reach the user-facing event stream.
func (s *ToolingService) JobProgress(evt models.JobProgressEvent) {
	if evt.Kind == jobs.KindRecipeStep {
		return
	}

	s.emitEvent(JobProgressEventName, evt)
}

// JobState emits a full job snapshot so the frontend can merge it into the
// activity list without polling. It implements jobs.Emitter. Recipe-step jobs
// are internal and never reach the user-facing event stream.
func (s *ToolingService) JobState(job models.JobResult) {
	if job.Kind == jobs.KindRecipeStep {
		return
	}

	s.emitJobState(job)
}

// emitEvent forwards one typed event to the injected bus. A missing bus
// (headless tests, early startup) skips emission.
func (s *ToolingService) emitEvent(name string, payload any) {
	if s.bus == nil {
		return
	}

	s.bus.Emit(name, payload)
}

func defaultJobsPersistencePath() string {
	configDir, err := os.UserConfigDir()
	if err != nil || configDir == "" {
		return filepath.Join(".", ".fileforge", "jobs_state_v1.json")
	}

	return filepath.Join(configDir, "fileforge", "jobs_state_v1.json")
}

func (s *ToolingService) contextOrBackground() context.Context {
	if s.ctx == nil {
		return context.Background()
	}
	return s.ctx
}

func (s *ToolingService) ListTools() models.ListToolsResponse {
	ctx := s.contextOrBackground()
	tools := s.registry.ListTools(ctx)

	return models.ListToolsResponse{
		Success: true,
		Message: "tools listed successfully",
		Tools:   tools,
	}
}

func (s *ToolingService) ValidateJob(req models.JobRequest) models.ValidateJobResponse {
	res := s.engine.Validate(s.contextOrBackground(), req)
	if !res.Valid {
		code := ""
		if res.Error != nil {
			code = res.Error.Code
		}
		log.Printf("tooling.validate.invalid toolId=%s mode=%s errorCode=%s", req.ToolID, req.Mode, code)
	}
	return res
}

func (s *ToolingService) PlanJob(req models.JobRequest) models.PlanJobResponse {
	plan := s.engine.Plan(s.contextOrBackground(), req)
	if !plan.Success {
		code := ""
		if plan.Error != nil {
			code = plan.Error.Code
		}
		log.Printf("tooling.plan.invalid toolId=%s mode=%s errorCode=%s", req.ToolID, req.Mode, code)
	}
	return plan
}

func (s *ToolingService) RunJob(req models.JobRequest) models.RunJobResponse {
	res, err := s.engine.Submit(s.contextOrBackground(), req)
	if err != nil {
		log.Printf("tooling.run.submit_failed toolId=%s mode=%s err=%v", req.ToolID, req.Mode, err)
		message := "job submission failed"
		if errors.Is(err, jobs.ErrQueueFull) {
			message = "job queue is full; wait for running jobs to finish and retry"
		}
		return models.RunJobResponse{
			Success: false,
			Message: message,
			Status:  jobs.StatusFailed,
			Error:   models.NewCanonicalJobError("SUBMIT_ERROR", err.Error(), nil),
		}
	}

	if !res.Success {
		code := ""
		if res.Error != nil {
			code = res.Error.Code
		}
		log.Printf("tooling.run.rejected toolId=%s mode=%s errorCode=%s", req.ToolID, req.Mode, code)
	} else {
		log.Printf("tooling.run.submitted toolId=%s mode=%s jobId=%s", req.ToolID, req.Mode, res.JobID)
		s.emitJobSnapshot(res.JobID)
	}

	return res
}

// ListJobs returns every tracked job snapshot, newest first. The UI uses it
// to rebuild the activity list after navigation or reload. Internal recipe-step
// jobs are filtered out; the recipe run that owns them is the visible unit.
func (s *ToolingService) ListJobs(limit int) models.JobListResponse {
	engineJobs := s.engine.List(limit)
	visible := make([]models.JobResult, 0, len(engineJobs))
	for _, job := range engineJobs {
		if job.Kind == jobs.KindRecipeStep {
			continue
		}
		visible = append(visible, job)
	}

	return models.JobListResponse{
		Success: true,
		Message: "jobs listed",
		Jobs:    visible,
	}
}

// emitJobSnapshot emits the current full snapshot of one job. A missing job or
// an internal recipe-step job is silently skipped.
func (s *ToolingService) emitJobSnapshot(jobID string) {
	job, found := s.engine.Get(jobID)
	if !found || job.Kind == jobs.KindRecipeStep {
		return
	}

	s.emitJobState(job)
}

// emitJobState publishes a full job snapshot so the frontend can merge it into
// the activity list without polling.
func (s *ToolingService) emitJobState(job models.JobResult) {
	if job.Kind == jobs.KindRecipeStep {
		return
	}

	s.emitEvent(JobStateEventName, job)
}

func (s *ToolingService) CancelJob(jobID string) models.CancelJobResponse {
	if err := s.engine.Cancel(jobID); err != nil {
		log.Printf("tooling.cancel.failed jobId=%s err=%v", jobID, err)
		return models.CancelJobResponse{
			Success: false,
			Message: "cancel failed",
			JobID:   jobID,
			Error:   models.NewCanonicalJobError("NOT_FOUND", err.Error(), nil),
		}
	}

	log.Printf("tooling.cancel.requested jobId=%s", jobID)
	return models.CancelJobResponse{
		Success: true,
		Message: "cancel requested",
		JobID:   jobID,
	}
}

func (s *ToolingService) GetJobStatus(jobID string) models.JobStatusResponse {
	job, found := s.engine.Get(jobID)
	if !found {
		log.Printf("tooling.status.not_found jobId=%s", jobID)
		return models.JobStatusResponse{
			Success: false,
			Message: "job not found",
			Found:   false,
			Error:   models.NewCanonicalJobError("NOT_FOUND", fmt.Sprintf("job '%s' not found", jobID), nil),
		}
	}

	return models.JobStatusResponse{
		Success: true,
		Message: "job status retrieved",
		Found:   true,
		Result:  &job,
	}
}

// VerifyJob verifies every output produced by a terminal job. Missing or
// unreadable outputs are reported per output instead of failing the whole
// report. Jobs that already left the engine are verified from recorded
// history, so a result stays verifiable after a restart.
func (s *ToolingService) VerifyJob(jobID string) models.VerificationReport {
	trimmedID := strings.TrimSpace(jobID)

	job, found := s.engine.Get(trimmedID)
	if found {
		return s.verifyJobOutputs(trimmedID, jobOutputPaths(job))
	}

	if s.history != nil {
		if entry, historyFound := s.history.Get(trimmedID); historyFound {
			return s.verifyJobOutputs(trimmedID, historyOutputPaths(entry))
		}
	}

	log.Printf("tooling.verify.not_found jobId=%s", trimmedID)
	return models.VerificationReport{
		Success: false,
		Message: fmt.Sprintf("job '%s' not found", trimmedID),
		Outputs: []models.VerifiedOutput{},
	}
}

func (s *ToolingService) verifyJobOutputs(jobID string, paths []string) models.VerificationReport {
	if len(paths) == 0 {
		return models.VerificationReport{
			Success: false,
			Message: "job has no outputs to verify",
			Outputs: []models.VerifiedOutput{},
		}
	}

	report := verify.VerifyPaths(paths)
	if !report.Success {
		log.Printf("tooling.verify.failed jobId=%s outputs=%d message=%s", jobID, len(report.Outputs), report.Message)
	}
	return report
}

// historyOutputPaths returns the recorded outputs that should still exist at
// their original path. Outputs moved to trash or pruned from it have no
// original file left to verify.
func historyOutputPaths(entry models.JobHistoryEntry) []string {
	paths := make([]string, 0, len(entry.Outputs))
	seen := make(map[string]struct{}, len(entry.Outputs))
	for _, output := range entry.Outputs {
		path := strings.TrimSpace(output.Path)
		if path == "" || strings.TrimSpace(output.TrashedPath) != "" {
			continue
		}
		key := filepath.Clean(path)
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		paths = append(paths, path)
	}

	return paths
}

// jobOutputPaths collects declared outputs in order, falling back to the
// single outputPath field used by older tools. Duplicate paths are ignored.
func jobOutputPaths(job models.JobResult) []string {
	paths := make([]string, 0, len(job.Items))
	seen := make(map[string]struct{}, len(job.Items))
	add := func(path string) {
		trimmed := strings.TrimSpace(path)
		if trimmed == "" {
			return
		}
		key := filepath.Clean(trimmed)
		if _, exists := seen[key]; exists {
			return
		}
		seen[key] = struct{}{}
		paths = append(paths, trimmed)
	}

	for _, item := range job.Items {
		if len(item.Outputs) > 0 {
			for _, path := range item.Outputs {
				add(path)
			}
			continue
		}
		add(item.OutputPath)
	}

	return paths
}

// recordTerminalJob records a terminal job in the undo history. The engine
// already emitted the terminal state before running this hook. Recipe-step jobs
// are internal: the owning recipe run records its final outputs once.
func (s *ToolingService) recordTerminalJob(job models.JobResult) {
	if s.history == nil {
		return
	}
	if job.Kind == jobs.KindRecipeStep {
		log.Printf("tooling.history.skipped_recipe_step jobId=%s toolId=%s", job.JobID, job.ToolID)
		return
	}

	s.history.RecordJob(job)
	log.Printf("tooling.history.completed jobId=%s toolId=%s status=%s", job.JobID, job.ToolID, job.Status)
}

// JobHistory exposes the shared undo store so other services (recipes) record
// their outputs into the same history the direct tools use.
func (s *ToolingService) JobHistory() *jobhistory.Store {
	return s.history
}

// ListJobHistory returns recorded terminal jobs, newest first.
func (s *ToolingService) ListJobHistory() []models.JobHistoryEntry {
	entries := s.history.List()
	if entries == nil {
		entries = []models.JobHistoryEntry{}
	}

	return entries
}

// UndoJob moves the untouched outputs of a recorded job to the app trash.
// Modified or missing outputs are skipped and reported.
func (s *ToolingService) UndoJob(jobID string) models.UndoResult {
	result := s.history.Undo(jobID)
	if !result.Success {
		log.Printf("tooling.undo.rejected jobId=%s message=%s", strings.TrimSpace(jobID), result.Message)
		return result
	}

	log.Printf("tooling.undo.applied jobId=%s moved=%d skipped=%d", strings.TrimSpace(jobID), len(result.Moved), len(result.Skipped))
	return result
}

// RestoreJob moves trashed outputs back to their original paths. Existing
// destinations are never overwritten.
func (s *ToolingService) RestoreJob(jobID string) models.UndoResult {
	result := s.history.Restore(jobID)
	if !result.Success {
		log.Printf("tooling.restore.rejected jobId=%s message=%s", strings.TrimSpace(jobID), result.Message)
		return result
	}

	log.Printf("tooling.restore.applied jobId=%s restored=%d skipped=%d", strings.TrimSpace(jobID), len(result.Moved), len(result.Skipped))
	return result
}

// EmptyTrash permanently removes the app-managed trash. History entries are
// kept and marked trashEmptied; user files outside the trash are never touched.
func (s *ToolingService) EmptyTrash() models.PathActionResponse {
	if err := s.history.EmptyTrash(); err != nil {
		log.Printf("tooling.trash.empty_failed err=%v", err)
		return models.PathActionResponse{
			Success: false,
			Message: "The trash could not be emptied.",
		}
	}

	log.Print("tooling.trash.emptied")
	return models.PathActionResponse{
		Success: true,
		Message: "trash emptied",
	}
}
