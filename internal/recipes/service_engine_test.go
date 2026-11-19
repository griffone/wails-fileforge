package recipes

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"fileforge-desktop/internal/jobs"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/registry"
)

// TestRecipeStepRunsOnSharedEngine proves recipe steps are scheduled as engine
// jobs instead of executing inline in the recipe goroutine.
func TestRecipeStepRunsOnSharedEngine(t *testing.T) {
	service, _, _ := newTestService(t)
	engine := service.sharedEngine()
	if engine == nil {
		t.Fatal("recipe service has no shared engine")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 1)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDEmailPDF,
		InputPaths: []string{input},
		Options:    map[string]any{"targetSizeBytes": float64(testLargeTargetBytes)},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s)", result.Status, result.Message)
	}

	engineJobs := engine.List(0)
	if len(engineJobs) != 1 {
		t.Fatalf("engine tracked %d jobs, want one optimize step: %+v", len(engineJobs), engineJobs)
	}

	step := engineJobs[0]
	if step.ToolID != pdf.ToolIDPDFOptimizeV1 {
		t.Fatalf("engine step tool = %q, want %q", step.ToolID, pdf.ToolIDPDFOptimizeV1)
	}
	if step.Status != jobs.StatusSuccess {
		t.Fatalf("engine step status = %q, want %q", step.Status, jobs.StatusSuccess)
	}
	if len(step.Items) == 0 || step.Items[0].OutputPath == "" {
		t.Fatalf("expected the engine step to report its output, got %+v", step.Items)
	}
	if len(result.Outputs) != 1 || result.Outputs[0] != step.Items[0].OutputPath {
		t.Fatalf("recipe output %+v does not match the engine step output %q", result.Outputs, step.Items[0].OutputPath)
	}
}

// TestRecipeCancellationCancelsEngineStep proves cancelling a recipe run
// propagates to the submitted step job and leaves no published output.
func TestRecipeCancellationCancelsEngineStep(t *testing.T) {
	blocking := &blockingOptimizeTool{started: make(chan struct{}, 1)}

	service, _, _ := newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, pdf.NewMergeTool())
		mustRegisterTool(t, reg, blocking)
	})
	engine := service.sharedEngine()
	if engine == nil {
		t.Fatal("recipe service has no shared engine")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 1)

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDEmailPDF,
		InputPaths: []string{input},
		Options:    map[string]any{"targetSizeBytes": testLargeTargetBytes},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	select {
	case <-blocking.started:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for the optimize step to start")
	}

	cancelled := service.Cancel(run.JobID)
	if !cancelled.Success || !cancelled.Found {
		t.Fatalf("expected cancel to be accepted, got %+v", cancelled)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusCancelled {
		t.Fatalf("expected cancelled, got %q (%s)", result.Status, result.Message)
	}
	if len(result.Outputs) != 0 {
		t.Fatalf("expected no published outputs, got %+v", result.Outputs)
	}

	engineJobs := engine.List(0)
	if len(engineJobs) != 1 {
		t.Fatalf("engine tracked %d jobs, want one optimize step: %+v", len(engineJobs), engineJobs)
	}
	if engineJobs[0].Status != jobs.StatusCancelled {
		t.Fatalf("engine step status = %q, want %q", engineJobs[0].Status, jobs.StatusCancelled)
	}
	if len(engineJobs[0].Items) > 0 && engineJobs[0].Items[0].Success {
		t.Fatalf("expected the cancelled step to report no successful item, got %+v", engineJobs[0].Items)
	}

	assertDirContainsOnly(t, dir, "report.pdf")
}
