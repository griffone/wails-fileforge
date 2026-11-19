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
	"fileforge-desktop/internal/platform/logx"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/verify"

	"github.com/wailsapp/wails/v3/pkg/application"
)

const (
	JobProgressEventNameV1 = "jobs/progress/v1"

	// JobStateEventNameV1 carries a full JobResultV1 snapshot whenever a job is
	// submitted or reaches a terminal status.
	JobStateEventNameV1 = "jobs/state/v1"
)

type ToolingService struct {
	ctx      context.Context
	registry *registry.Registry
	engine   *jobs.Engine
	history  *jobhistory.Store
}

func NewToolingService(reg *registry.Registry) *ToolingService {
	return newToolingService(reg, jobhistory.NewStore(jobhistory.DefaultPath()))
}

func newToolingService(reg *registry.Registry, history *jobhistory.Store) *ToolingService {
	if reg == nil {
		reg = registry.GetGlobalRegistry()
	}
	if history == nil {
		history = jobhistory.NewStore(jobhistory.DefaultPath())
	}

	service := &ToolingService{
		registry: reg,
		history:  history,
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

// JobProgress emits a coalesced progress event. It implements jobs.Emitter.
func (s *ToolingService) JobProgress(evt models.JobProgressEventV1) {
	app := application.Get()
	if app == nil {
		return
	}

	app.Event.Emit(JobProgressEventNameV1, evt)
}

// JobState emits a full job snapshot so the frontend can merge it into the
// activity list without polling. It implements jobs.Emitter.
func (s *ToolingService) JobState(job models.JobResultV1) {
	s.emitJobState(job)
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

func (s *ToolingService) ListToolsV1() models.ListToolsResponseV1 {
	ctx := s.contextOrBackground()
	tools := s.registry.ListToolsV2(ctx)

	return models.ListToolsResponseV1{
		Success: true,
		Message: "tools listed successfully",
		Tools:   tools,
	}
}

func (s *ToolingService) ValidateJobV1(req models.JobRequestV1) models.ValidateJobResponseV1 {
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

func (s *ToolingService) PlanJobV1(req models.JobRequestV1) models.PlanJobResponseV1 {
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

func (s *ToolingService) RunJobV1(req models.JobRequestV1) models.RunJobResponseV1 {
	res, err := s.engine.Submit(s.contextOrBackground(), req)
	if err != nil {
		log.Printf("tooling.run.submit_failed toolId=%s mode=%s err=%v", req.ToolID, req.Mode, err)
		message := "job submission failed"
		if errors.Is(err, jobs.ErrQueueFull) {
			message = "job queue is full; wait for running jobs to finish and retry"
		}
		return models.RunJobResponseV1{
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

// ListJobsV1 returns every tracked job snapshot, newest first. The UI uses it
// to rebuild the activity list after navigation or reload.
func (s *ToolingService) ListJobsV1(limit int) models.JobListResponseV1 {
	jobs := s.engine.List(limit)
	if jobs == nil {
		jobs = []models.JobResultV1{}
	}

	return models.JobListResponseV1{
		Success: true,
		Message: "jobs listed",
		Jobs:    jobs,
	}
}

// emitJobSnapshot emits the current full snapshot of one job. A missing job is
// silently skipped.
func (s *ToolingService) emitJobSnapshot(jobID string) {
	job, found := s.engine.Get(jobID)
	if !found {
		return
	}

	s.emitJobState(job)
}

// emitJobState publishes a full job snapshot so the frontend can merge it into
// the activity list without polling.
func (s *ToolingService) emitJobState(job models.JobResultV1) {
	app := application.Get()
	if app == nil {
		return
	}

	app.Event.Emit(JobStateEventNameV1, job)
}

func (s *ToolingService) CancelJobV1(jobID string) models.CancelJobResponseV1 {
	if err := s.engine.Cancel(jobID); err != nil {
		log.Printf("tooling.cancel.failed jobId=%s err=%v", jobID, err)
		return models.CancelJobResponseV1{
			Success: false,
			Message: "cancel failed",
			JobID:   jobID,
			Error:   models.NewCanonicalJobError("NOT_FOUND", err.Error(), nil),
		}
	}

	log.Printf("tooling.cancel.requested jobId=%s", jobID)
	return models.CancelJobResponseV1{
		Success: true,
		Message: "cancel requested",
		JobID:   jobID,
	}
}

func (s *ToolingService) GetJobStatusV1(jobID string) models.JobStatusResponseV1 {
	job, found := s.engine.Get(jobID)
	if !found {
		log.Printf("tooling.status.not_found jobId=%s", jobID)
		return models.JobStatusResponseV1{
			Success: false,
			Message: "job not found",
			Found:   false,
			Error:   models.NewCanonicalJobError("NOT_FOUND", fmt.Sprintf("job '%s' not found", jobID), nil),
		}
	}

	return models.JobStatusResponseV1{
		Success: true,
		Message: "job status retrieved",
		Found:   true,
		Result:  &job,
	}
}

// VerifyJobV1 verifies every output produced by a terminal job. Missing or
// unreadable outputs are reported per output instead of failing the whole
// report.
func (s *ToolingService) VerifyJobV1(jobID string) models.VerificationReportV1 {
	trimmedID := strings.TrimSpace(jobID)
	job, found := s.engine.Get(trimmedID)
	if !found {
		log.Printf("tooling.verify.not_found jobId=%s", trimmedID)
		return models.VerificationReportV1{
			Success: false,
			Message: fmt.Sprintf("job '%s' not found", trimmedID),
			Outputs: []models.VerifiedOutputV1{},
		}
	}

	paths := jobOutputPaths(job)
	if len(paths) == 0 {
		return models.VerificationReportV1{
			Success: false,
			Message: "job has no outputs to verify",
			Outputs: []models.VerifiedOutputV1{},
		}
	}

	report := verify.VerifyPaths(paths)
	if !report.Success {
		log.Printf("tooling.verify.failed jobId=%s outputs=%d message=%s", trimmedID, len(report.Outputs), report.Message)
	}
	return report
}

// jobOutputPaths collects declared outputs in order, falling back to the
// single outputPath field used by older tools. Duplicate paths are ignored.
func jobOutputPaths(job models.JobResultV1) []string {
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
// already emitted the terminal state before running this hook.
func (s *ToolingService) recordTerminalJob(job models.JobResultV1) {
	if s.history == nil {
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

// ListJobHistoryV1 returns recorded terminal jobs, newest first.
func (s *ToolingService) ListJobHistoryV1() []models.JobHistoryEntryV1 {
	entries := s.history.List()
	if entries == nil {
		entries = []models.JobHistoryEntryV1{}
	}

	return entries
}

// UndoJobV1 moves the untouched outputs of a recorded job to the app trash.
// Modified or missing outputs are skipped and reported.
func (s *ToolingService) UndoJobV1(jobID string) models.UndoResultV1 {
	result := s.history.Undo(jobID)
	if !result.Success {
		log.Printf("tooling.undo.rejected jobId=%s message=%s", strings.TrimSpace(jobID), result.Message)
		return result
	}

	log.Printf("tooling.undo.applied jobId=%s moved=%d skipped=%d", strings.TrimSpace(jobID), len(result.Moved), len(result.Skipped))
	return result
}

// RestoreJobV1 moves trashed outputs back to their original paths. Existing
// destinations are never overwritten.
func (s *ToolingService) RestoreJobV1(jobID string) models.UndoResultV1 {
	result := s.history.Restore(jobID)
	if !result.Success {
		log.Printf("tooling.restore.rejected jobId=%s message=%s", strings.TrimSpace(jobID), result.Message)
		return result
	}

	log.Printf("tooling.restore.applied jobId=%s restored=%d skipped=%d", strings.TrimSpace(jobID), len(result.Moved), len(result.Skipped))
	return result
}

// EmptyTrashV1 permanently removes the app-managed trash. History entries are
// kept and marked trashEmptied; user files outside the trash are never touched.
func (s *ToolingService) EmptyTrashV1() models.PathActionResponseV1 {
	if err := s.history.EmptyTrash(); err != nil {
		log.Printf("tooling.trash.empty_failed err=%v", err)
		return models.PathActionResponseV1{
			Success: false,
			Message: "The trash could not be emptied.",
		}
	}

	log.Print("tooling.trash.emptied")
	return models.PathActionResponseV1{
		Success: true,
		Message: "trash emptied",
	}
}
