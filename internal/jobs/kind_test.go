package jobs

import (
	"context"
	"testing"
	"time"

	"fileforge-desktop/internal/models"
)

// TestEngineKindTagsRecipeStepsAndHidesTheToolDefault proves the engine copies
// the submission kind onto the job result and every emitted event, while the
// default tool kind stays out of the wire payload.
func TestEngineKindTagsRecipeStepsAndHidesTheToolDefault(t *testing.T) {
	emitter := &recordingEngineEmitter{}
	tool := &engineFakeTool{
		id: "tool.fake.kind",
		single: func(_ context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
			path := firstEngineInput(req)
			if onProgress != nil {
				onProgress(models.JobProgress{Current: 1, Total: 1, Stage: "processing", Message: "working"})
			}

			return models.JobResultItem{InputPath: path, Success: true, Message: "done"}, nil
		},
	}
	engine := newTestEngine(t, newEngineResolver(t, tool), Options{
		Limits:           Limits{Total: 2, CPU: 2, External: 2, IO: 2},
		Emitter:          emitter,
		ProgressInterval: time.Hour,
	})

	toolSubmit := submitTestJob(t, engine, models.JobRequest{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-kind-tool.txt"},
	})
	stepSubmit := submitTestJobWith(t, engine, models.JobRequest{
		ToolID:     tool.id,
		Mode:       "single",
		InputPaths: []string{"/tmp/fileforge-kind-step.txt"},
	}, SubmitOptions{Kind: KindRecipeStep, Priority: PriorityInteractive, Class: ClassCPU})

	toolResult := awaitJobResult(t, engine, toolSubmit.JobID)
	stepResult := awaitJobResult(t, engine, stepSubmit.JobID)

	if toolResult.Kind != "" {
		t.Fatalf("tool job kind = %q, want the empty wire default", toolResult.Kind)
	}
	if stepResult.Kind != KindRecipeStep {
		t.Fatalf("recipe step kind = %q, want %q", stepResult.Kind, KindRecipeStep)
	}

	progressEvents, stateEvents := emitter.snapshots()
	if len(progressEvents) == 0 || len(stateEvents) == 0 {
		t.Fatalf("expected both progress and state events, got %d/%d", len(progressEvents), len(stateEvents))
	}
	for _, event := range progressEvents {
		want := ""
		if event.JobID == stepSubmit.JobID {
			want = KindRecipeStep
		}
		if event.Kind != want {
			t.Fatalf("progress event for %s has kind %q, want %q", event.JobID, event.Kind, want)
		}
	}
	for _, event := range stateEvents {
		want := ""
		if event.JobID == stepSubmit.JobID {
			want = KindRecipeStep
		}
		if event.Kind != want {
			t.Fatalf("state event for %s has kind %q, want %q", event.JobID, event.Kind, want)
		}
	}
}

func awaitJobResult(t *testing.T, engine *Engine, jobID string) models.JobResult {
	t.Helper()

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	result, err := engine.Wait(ctx, jobID)
	if err != nil {
		t.Fatalf("wait for job %s: %v", jobID, err)
	}

	return result
}
