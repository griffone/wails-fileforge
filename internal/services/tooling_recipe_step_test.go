package services

import (
	"context"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/jobs"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/recipes"
	"fileforge-desktop/internal/registry"

	"github.com/jung-kurt/gofpdf"
)

const recipeHistoryToolID = "recipe.email_pdf"

type serviceEvent struct {
	name    string
	payload any
}

// serviceEventRecorder captures the events the tooling service would hand to
// the Wails application so filtering can be asserted in headless tests.
type serviceEventRecorder struct {
	mu     sync.Mutex
	events []serviceEvent
}

func (r *serviceEventRecorder) record(name string, payload any) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, serviceEvent{name: name, payload: payload})
}

func (r *serviceEventRecorder) eventsForJob(jobID string) []serviceEvent {
	r.mu.Lock()
	defer r.mu.Unlock()

	matched := []serviceEvent{}
	for _, event := range r.events {
		switch payload := event.payload.(type) {
		case models.JobProgressEventV1:
			if payload.JobID == jobID {
				matched = append(matched, event)
			}
		case models.JobResultV1:
			if payload.JobID == jobID {
				matched = append(matched, event)
			}
		}
	}

	return matched
}

func TestToolingServiceHidesRecipeStepJobs(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out", "step.pdf")
	tool := &historyFakeTool{id: "tool.fake.recipe.step", outputPath: output}
	service, _ := newHistoryTestService(t, tool)
	recorder := &serviceEventRecorder{}
	service.eventSink = recorder.record

	submit, err := service.Engine().SubmitWithOptions(
		context.Background(),
		models.JobRequestV1{
			ToolID:     tool.id,
			Mode:       "single",
			InputPaths: []string{"/tmp/fileforge-recipe-step.pdf"},
		},
		jobs.SubmitOptions{Kind: jobs.KindRecipeStep, Priority: jobs.PriorityInteractive, Class: jobs.ClassCPU},
	)
	if err != nil {
		t.Fatalf("submit recipe step: %v", err)
	}
	if !submit.Success {
		t.Fatalf("recipe step rejected: %+v", submit)
	}

	result := waitForTerminalStatus(t, service, submit.JobID)
	if result.Kind != jobs.KindRecipeStep {
		t.Fatalf("job kind = %q, want %q", result.Kind, jobs.KindRecipeStep)
	}

	// Internal diagnostics still resolve step jobs by id.
	status := service.GetJobStatusV1(submit.JobID)
	if !status.Found || status.Result == nil {
		t.Fatalf("recipe step is missing from the engine lookup: %+v", status)
	}

	if listed := service.ListJobsV1(0); len(listed.Jobs) != 0 {
		t.Fatalf("recipe step leaked into the job list: %+v", listed.Jobs)
	}
	if events := recorder.eventsForJob(submit.JobID); len(events) != 0 {
		t.Fatalf("recipe step leaked into the event stream: %+v", events)
	}
	if entries := service.ListJobHistoryV1(); len(entries) != 0 {
		t.Fatalf("recipe step leaked into the undo history: %+v", entries)
	}
}

func TestToolingServiceExposesToolJobs(t *testing.T) {
	output := filepath.Join(t.TempDir(), "out", "converted.pdf")
	tool := &historyFakeTool{id: "tool.fake.visible", outputPath: output}
	service, _ := newHistoryTestService(t, tool)
	recorder := &serviceEventRecorder{}
	service.eventSink = recorder.record

	submit := service.RunJobV1(models.JobRequestV1{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-visible.pdf"},
	})
	if !submit.Success {
		t.Fatalf("submit tool job: %+v", submit)
	}

	result := waitForTerminalStatus(t, service, submit.JobID)
	if result.Kind != "" {
		t.Fatalf("tool job kind = %q, want the empty wire default", result.Kind)
	}

	listed := service.ListJobsV1(0)
	if len(listed.Jobs) != 1 || listed.Jobs[0].JobID != submit.JobID {
		t.Fatalf("tool job missing from the job list: %+v", listed.Jobs)
	}

	events := recorder.eventsForJob(submit.JobID)
	var sawProgress, sawState bool
	for _, event := range events {
		switch event.name {
		case JobProgressEventNameV1:
			sawProgress = true
		case JobStateEventNameV1:
			sawState = true
		}
	}
	if !sawProgress || !sawState {
		t.Fatalf("tool job events missing: progress=%t state=%t (%+v)", sawProgress, sawState, events)
	}

	if entries := service.ListJobHistoryV1(); len(entries) != 1 || entries[0].JobID != submit.JobID {
		t.Fatalf("tool job missing from the undo history: %+v", entries)
	}
}

// TestRecipeRunRecordsFinalOutputsOnce proves that sharing the tooling engine
// with the recipe service no longer records one undo entry per recipe step: the
// recipe run records its final outputs exactly once.
func TestRecipeRunRecordsFinalOutputsOnce(t *testing.T) {
	reg := registry.NewRegistry()
	if err := reg.RegisterToolV2(pdf.NewOptimizeTool()); err != nil {
		t.Fatalf("register optimize tool: %v", err)
	}

	history := jobhistory.NewStore(filepath.Join(t.TempDir(), "history", "job_history_v1.json"))
	service := newToolingService(reg, history)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = service.Shutdown(ctx)
	})

	recipeService := recipes.NewService(reg, history, nil)
	recipeService.SetEngine(service.Engine())

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writeServicePDFFixture(t, input, 1)

	run := recipeService.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   recipes.RecipeIDEmailPDF,
		InputPaths: []string{input},
		Options:    map[string]any{"targetSizeBytes": float64(200 * 1024 * 1024)},
	})
	if !run.Success {
		t.Fatalf("run recipe: %+v", run)
	}

	result := waitForRecipeResult(t, recipeService, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("recipe status = %q (%s), want success", result.Status, result.Message)
	}

	engineJobs := service.Engine().List(0)
	if len(engineJobs) != 1 {
		t.Fatalf("engine tracked %d step jobs, want one: %+v", len(engineJobs), engineJobs)
	}
	if engineJobs[0].Kind != jobs.KindRecipeStep {
		t.Fatalf("step job kind = %q, want %q", engineJobs[0].Kind, jobs.KindRecipeStep)
	}
	if listed := service.ListJobsV1(0); len(listed.Jobs) != 0 {
		t.Fatalf("recipe step leaked into the job list: %+v", listed.Jobs)
	}

	entries := service.ListJobHistoryV1()
	if len(entries) != 1 {
		t.Fatalf("history entries = %d, want exactly one recipe entry: %+v", len(entries), entries)
	}
	entry := entries[0]
	if entry.JobID != run.JobID || entry.ToolID != recipeHistoryToolID {
		t.Fatalf("unexpected recipe history entry: %+v", entry)
	}
	if len(entry.Outputs) != 1 || entry.Outputs[0].Path != result.Outputs[0] {
		t.Fatalf("recipe outputs were not recorded once: %+v", entry.Outputs)
	}
}

func writeServicePDFFixture(t *testing.T, path string, pages int) {
	t.Helper()

	document := gofpdf.New("P", "mm", "A4", "")
	document.SetFont("Helvetica", "", 12)
	for page := 0; page < pages; page++ {
		document.AddPage()
		document.Cell(40, 10, "Page")
	}
	if err := document.OutputFileAndClose(path); err != nil {
		t.Fatalf("write pdf fixture %s: %v", path, err)
	}
}

func waitForRecipeResult(t *testing.T, service *recipes.Service, jobID string) models.RecipeResultV1 {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		status := service.Status(jobID)
		if status.Found && status.Result != nil && terminalRecipeStatus(status.Result.Status) {
			return *status.Result
		}
		time.Sleep(20 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for recipe %s to finish", jobID)
	return models.RecipeResultV1{}
}

func terminalRecipeStatus(status string) bool {
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
