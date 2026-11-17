package recipes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"

	"fileforge-desktop/internal/image"
	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/registry"
	"fileforge-desktop/internal/tools"
	"fileforge-desktop/internal/video"
)

const testLargeTargetBytes = int64(200 * 1024 * 1024)

func TestPlanEmailPDFSingleInput(t *testing.T) {
	service, _, _ := newTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 2)

	plan := service.Plan(models.RecipeRequestV1{
		RecipeID:   RecipeIDEmailPDF,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}
	if plan.Recipe == nil {
		t.Fatal("expected recipe metadata in the plan")
	}
	if plan.Recipe.ID != RecipeIDEmailPDF || plan.Recipe.Name != "Email PDF" {
		t.Fatalf("unexpected recipe metadata: %+v", plan.Recipe)
	}
	if plan.Recipe.DefaultTargetSizeBytes != 10*1024*1024 {
		t.Fatalf("expected the 10 MiB default target, got %d", plan.Recipe.DefaultTargetSizeBytes)
	}
	if len(plan.Recipe.Steps) != 2 {
		t.Fatalf("expected 2 recipe steps, got %+v", plan.Recipe.Steps)
	}
	if plan.Recipe.Steps[0].ID != stepIDMergePDFs || plan.Recipe.Steps[0].ToolID != pdf.ToolIDPDFMergeV1 {
		t.Fatalf("unexpected merge step: %+v", plan.Recipe.Steps[0])
	}
	if plan.Recipe.Steps[1].ID != stepIDOptimizeTarget || plan.Recipe.Steps[1].ToolID != pdf.ToolIDPDFOptimizeV1 {
		t.Fatalf("unexpected optimize step: %+v", plan.Recipe.Steps[1])
	}

	if plan.OutputDir != dir {
		t.Fatalf("expected the output folder to default to the input folder %q, got %q", dir, plan.OutputDir)
	}
	if len(plan.InputPaths) != 1 || plan.InputPaths[0] != input {
		t.Fatalf("unexpected planned inputs: %+v", plan.InputPaths)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected one planned output, got %+v", plan.Outputs)
	}

	output := plan.Outputs[0]
	wantPath := filepath.Join(dir, "report_email.pdf")
	if output.OutputPath != wantPath {
		t.Fatalf("expected final output %q, got %q", wantPath, output.OutputPath)
	}
	if output.OutputKind != "file" || output.OutputCount != 1 {
		t.Fatalf("unexpected output metadata: %+v", output)
	}
	if output.Exists {
		t.Fatal("expected the planned output to not exist yet")
	}
	if !warningsContain(plan.Warnings, "best effort") {
		t.Fatalf("expected a best-effort target warning, got %+v", plan.Warnings)
	}
}

func TestPlanEmailPDFMultipleInputs(t *testing.T) {
	service, _, _ := newTestService(t)

	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create output folder: %v", err)
	}
	inputs := []string{
		filepath.Join(dir, "first.pdf"),
		filepath.Join(dir, "second.pdf"),
		filepath.Join(dir, "third.pdf"),
	}
	for _, input := range inputs {
		writePDFFixture(t, input, 1)
	}

	plan := service.Plan(models.RecipeRequestV1{
		RecipeID:   RecipeIDEmailPDF,
		InputPaths: inputs,
		OutputDir:  outDir,
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}

	wantPath := filepath.Join(outDir, "first_email.pdf")
	if len(plan.Outputs) != 1 || plan.Outputs[0].OutputPath != wantPath {
		t.Fatalf("expected final output %q, got %+v", wantPath, plan.Outputs)
	}
	if !warningsContain(plan.Warnings, "best effort") {
		t.Fatalf("expected a best-effort target warning, got %+v", plan.Warnings)
	}

	// A second plan with the same name taken must resolve a collision-safe name.
	if err := os.WriteFile(wantPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}
	adjustedPlan := service.Plan(models.RecipeRequestV1{
		RecipeID:   RecipeIDEmailPDF,
		InputPaths: inputs,
		OutputDir:  outDir,
	})
	if !adjustedPlan.Success {
		t.Fatalf("expected the adjusted plan to succeed, got %+v", adjustedPlan.Error)
	}
	adjustedPath := filepath.Join(outDir, "first_email-2.pdf")
	if adjustedPlan.Outputs[0].OutputPath != adjustedPath {
		t.Fatalf("expected collision-safe output %q, got %q", adjustedPath, adjustedPlan.Outputs[0].OutputPath)
	}
	if !warningsContain(adjustedPlan.Warnings, "already exists") {
		t.Fatalf("expected an already-exists warning, got %+v", adjustedPlan.Warnings)
	}
}

func TestRunEmailPDFSingleInput(t *testing.T) {
	service, history, recorder := newTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 2)

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
	if result.Verification == nil {
		t.Fatal("expected a verification report")
	}
	if !result.Verification.Success {
		t.Fatalf("expected verification to pass, got %+v", result.Verification)
	}
	if result.TargetSizeBytes != testLargeTargetBytes {
		t.Fatalf("expected target %d, got %d", testLargeTargetBytes, result.TargetSizeBytes)
	}
	if result.StartedAt <= 0 || result.EndedAt < result.StartedAt {
		t.Fatalf("unexpected timestamps: started=%d ended=%d", result.StartedAt, result.EndedAt)
	}

	wantOutput := filepath.Join(dir, "report_email.pdf")
	if len(result.Outputs) != 1 || result.Outputs[0] != wantOutput {
		t.Fatalf("expected output %q, got %+v", wantOutput, result.Outputs)
	}
	info, err := os.Stat(wantOutput)
	if err != nil {
		t.Fatalf("expected the output to exist: %v", err)
	}
	if info.Size() == 0 {
		t.Fatal("expected a non-empty output")
	}

	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 step results, got %+v", result.Steps)
	}
	if result.Steps[0].Status != recipeStepStatusSkipped || result.Steps[0].Message != "Using the original PDF" {
		t.Fatalf("expected the merge step to be skipped, got %+v", result.Steps[0])
	}
	if result.Steps[1].Status != models.JobStatusSuccess {
		t.Fatalf("expected the optimize step to succeed, got %+v", result.Steps[1])
	}

	entries := history.List()
	if len(entries) != 1 {
		t.Fatalf("expected one history entry, got %+v", entries)
	}
	if entries[0].JobID != run.JobID || entries[0].ToolID != emailPDFHistoryToolID {
		t.Fatalf("unexpected history entry: %+v", entries[0])
	}
	if len(entries[0].Outputs) != 1 || entries[0].Outputs[0].Path != wantOutput {
		t.Fatalf("expected the output to be recorded for undo, got %+v", entries[0].Outputs)
	}

	terminalEvent := waitForRecipeEvent(t, recorder, func(event models.RecipeProgressEventV1) bool {
		return event.Status == models.JobStatusSuccess && event.Percent == 100
	})
	if terminalEvent.JobID != run.JobID || terminalEvent.RecipeID != RecipeIDEmailPDF {
		t.Fatalf("unexpected terminal event: %+v", terminalEvent)
	}
}

func TestRecipeOutputCanBeUndoneAndRestored(t *testing.T) {
	service, history, _ := newTestService(t)

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

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess || len(result.Outputs) != 1 {
		t.Fatalf("expected one successful output, got %+v", result)
	}
	output := result.Outputs[0]

	undo := history.Undo(run.JobID)
	if !undo.Success || len(undo.Moved) != 1 {
		t.Fatalf("expected the recipe output to be undone, got %+v", undo)
	}
	if _, err := os.Stat(output); !os.IsNotExist(err) {
		t.Fatalf("expected the output to leave its original path, stat err = %v", err)
	}

	restore := history.Restore(run.JobID)
	if !restore.Success || len(restore.Moved) != 1 {
		t.Fatalf("expected the recipe output to be restored, got %+v", restore)
	}
	info, err := os.Stat(output)
	if err != nil || info.Size() == 0 {
		t.Fatalf("expected the restored output to exist and be non-empty, info=%v err=%v", info, err)
	}
}

func TestRecipeUndoSkipsModifiedOutput(t *testing.T) {
	service, history, _ := newTestService(t)

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

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess || len(result.Outputs) != 1 {
		t.Fatalf("expected one successful output, got %+v", result)
	}
	output := result.Outputs[0]

	file, err := os.OpenFile(output, os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		t.Fatalf("open output for modification: %v", err)
	}
	if _, err := file.WriteString("modified after the recipe"); err != nil {
		file.Close()
		t.Fatalf("modify output: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close modified output: %v", err)
	}

	undo := history.Undo(run.JobID)
	if undo.Success {
		t.Fatalf("expected the modified output to be refused, got %+v", undo)
	}
	if len(undo.Skipped) != 1 || !strings.Contains(undo.Skipped[0].Reason, "modified") {
		t.Fatalf("expected a modified-file skip, got %+v", undo.Skipped)
	}
	if _, err := os.Stat(output); err != nil {
		t.Fatalf("expected the modified output to stay in place, stat err = %v", err)
	}
}

func TestRunEmailPDFMergesMultipleInputs(t *testing.T) {
	service, _, recorder := newTestService(t)

	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "first.pdf"),
		filepath.Join(dir, "second.pdf"),
		filepath.Join(dir, "third.pdf"),
	}
	pages := []int{2, 3, 1}
	for index, input := range inputs {
		writePDFFixture(t, input, pages[index])
	}

	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDEmailPDF,
		InputPaths: inputs,
		Options:    map[string]any{"targetSizeBytes": int64(testLargeTargetBytes)},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %q (%s)", result.Status, result.Message)
	}

	wantOutput := filepath.Join(dir, "first_email.pdf")
	if len(result.Outputs) != 1 || result.Outputs[0] != wantOutput {
		t.Fatalf("expected output %q, got %+v", wantOutput, result.Outputs)
	}

	pageCount, err := api.PageCountFile(wantOutput)
	if err != nil {
		t.Fatalf("read merged page count: %v", err)
	}
	if pageCount != 6 {
		t.Fatalf("expected 6 merged pages (2+3+1), got %d", pageCount)
	}

	if result.Steps[0].Status != models.JobStatusSuccess {
		t.Fatalf("expected the merge step to succeed, got %+v", result.Steps[0])
	}
	if len(result.Steps[0].Outputs) != 1 {
		t.Fatalf("expected the merge step to report its intermediate output, got %+v", result.Steps[0])
	}
	waitForPathGone(t, result.Steps[0].Outputs[0])

	waitForRecipeEvent(t, recorder, func(event models.RecipeProgressEventV1) bool {
		return event.StepName == "Merge PDFs" && event.Stage == "running"
	})
	verifyEvent := waitForRecipeEvent(t, recorder, func(event models.RecipeProgressEventV1) bool {
		return event.StepName == "Optimize to target" && event.Stage == "verifying"
	})
	if verifyEvent.CurrentStep != 2 || verifyEvent.TotalSteps != 2 {
		t.Fatalf("unexpected verify event step counters: %+v", verifyEvent)
	}
}

func TestRunEmailPDFTinyTargetReturnsPartialSuccess(t *testing.T) {
	service, history, _ := newTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 1)

	const tinyTarget = int64(1)
	run := service.Run(context.Background(), models.RecipeRequestV1{
		RecipeID:   RecipeIDEmailPDF,
		InputPaths: []string{input},
		Options:    map[string]any{"targetSizeBytes": tinyTarget},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusPartialSuccess {
		t.Fatalf("expected partial_success, got %q (%s)", result.Status, result.Message)
	}
	if !strings.Contains(strings.ToLower(result.Message), "best effort") {
		t.Fatalf("expected a best-effort message, got %q", result.Message)
	}
	if result.Verification == nil || result.Verification.Success {
		t.Fatalf("expected a failed verification report, got %+v", result.Verification)
	}

	wantOutput := filepath.Join(dir, "report_email.pdf")
	info, err := os.Stat(wantOutput)
	if err != nil {
		t.Fatalf("expected the best-effort output to exist: %v", err)
	}
	if info.Size() <= tinyTarget {
		t.Fatalf("expected the output to exceed the tiny target, got %d bytes", info.Size())
	}
	if len(result.Outputs) != 1 || result.Outputs[0] != wantOutput {
		t.Fatalf("expected the best-effort output in the result, got %+v", result.Outputs)
	}
	if entries := history.List(); len(entries) != 1 || len(entries[0].Outputs) != 1 {
		t.Fatalf("expected the best-effort output to be recorded for undo, got %+v", entries)
	}
}

func TestPlanEmailPDFRejectsInvalidRequests(t *testing.T) {
	service, _, _ := newTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "report.pdf")
	writePDFFixture(t, input, 1)
	textFile := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textFile, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write text fixture: %v", err)
	}
	notADirectory := filepath.Join(dir, "not-a-directory")
	if err := os.WriteFile(notADirectory, []byte("file"), 0o644); err != nil {
		t.Fatalf("write file fixture: %v", err)
	}

	cases := []struct {
		name       string
		req        models.RecipeRequestV1
		wantDetail string
	}{
		{
			name:       "unknown recipe",
			req:        models.RecipeRequestV1{RecipeID: "recipe.unknown", InputPaths: []string{input}},
			wantDetail: "RECIPE_NOT_FOUND",
		},
		{
			name:       "no inputs",
			req:        models.RecipeRequestV1{RecipeID: RecipeIDEmailPDF},
			wantDetail: "RECIPE_INVALID_INPUT",
		},
		{
			name:       "non pdf input",
			req:        models.RecipeRequestV1{RecipeID: RecipeIDEmailPDF, InputPaths: []string{textFile}},
			wantDetail: "RECIPE_INVALID_INPUT",
		},
		{
			name:       "missing pdf input",
			req:        models.RecipeRequestV1{RecipeID: RecipeIDEmailPDF, InputPaths: []string{filepath.Join(dir, "missing.pdf")}},
			wantDetail: "RECIPE_INPUT_MISSING",
		},
		{
			name: "duplicate input",
			req: models.RecipeRequestV1{
				RecipeID:   RecipeIDEmailPDF,
				InputPaths: []string{input, input},
			},
			wantDetail: "RECIPE_INVALID_INPUT",
		},
		{
			name: "zero target",
			req: models.RecipeRequestV1{
				RecipeID:   RecipeIDEmailPDF,
				InputPaths: []string{input},
				Options:    map[string]any{"targetSizeBytes": 0},
			},
			wantDetail: "RECIPE_INVALID_TARGET",
		},
		{
			name: "fractional target",
			req: models.RecipeRequestV1{
				RecipeID:   RecipeIDEmailPDF,
				InputPaths: []string{input},
				Options:    map[string]any{"targetSizeBytes": 10.5},
			},
			wantDetail: "RECIPE_INVALID_TARGET",
		},
		{
			name: "non numeric target",
			req: models.RecipeRequestV1{
				RecipeID:   RecipeIDEmailPDF,
				InputPaths: []string{input},
				Options:    map[string]any{"targetSizeBytes": "10MB"},
			},
			wantDetail: "RECIPE_INVALID_TARGET",
		},
		{
			name: "output dir is a file",
			req: models.RecipeRequestV1{
				RecipeID:   RecipeIDEmailPDF,
				InputPaths: []string{input},
				OutputDir:  notADirectory,
			},
			wantDetail: "RECIPE_INVALID_OUTPUT_DIR",
		},
		{
			name: "output dir is missing",
			req: models.RecipeRequestV1{
				RecipeID:   RecipeIDEmailPDF,
				InputPaths: []string{input},
				OutputDir:  filepath.Join(dir, "missing-folder"),
			},
			wantDetail: "RECIPE_INVALID_OUTPUT_DIR",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan := service.Plan(testCase.req)
			if plan.Success {
				t.Fatalf("expected the plan to fail, got %+v", plan)
			}
			if plan.Error == nil {
				t.Fatalf("expected a structured error, got %+v", plan)
			}
			if plan.Error.DetailCode != testCase.wantDetail {
				t.Fatalf("expected detail code %q, got %q (%s)", testCase.wantDetail, plan.Error.DetailCode, plan.Error.Message)
			}
			if plan.Message == "" {
				t.Fatal("expected a human readable plan message")
			}
		})
	}

	run := service.Run(context.Background(), models.RecipeRequestV1{RecipeID: RecipeIDEmailPDF})
	if run.Success || run.Error == nil {
		t.Fatalf("expected an invalid run to fail synchronously, got %+v", run)
	}
}

func TestCancelRecipeJobCleansTempWorkspace(t *testing.T) {
	blocking := &blockingOptimizeTool{started: make(chan struct{}, 1)}

	service, _, recorder := newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, pdf.NewMergeTool())
		mustRegisterTool(t, reg, blocking)
	})

	tempRoot := t.TempDir()
	service.tempDir = func() (string, error) {
		return os.MkdirTemp(tempRoot, "recipe-*")
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
	if result.Error == nil || result.Error.Code != models.ErrorCodeCancelledByUser {
		t.Fatalf("expected a cancellation error, got %+v", result.Error)
	}
	waitForRecipeEvent(t, recorder, func(event models.RecipeProgressEventV1) bool {
		return event.Status == models.JobStatusCancelled
	})
	waitForEmptyDir(t, tempRoot)

	if response := service.Status("recipe_missing"); response.Success || response.Found {
		t.Fatalf("expected an unknown recipe job, got %+v", response)
	}
	if response := service.Cancel("recipe_missing"); response.Success || response.Found {
		t.Fatalf("expected cancel of an unknown recipe job to fail, got %+v", response)
	}
}

func TestRunEmailPDFFailureCleansTempWorkspace(t *testing.T) {
	failing := &failingOptimizeTool{}

	service, history, _ := newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, pdf.NewMergeTool())
		mustRegisterTool(t, reg, failing)
	})

	tempRoot := t.TempDir()
	service.tempDir = func() (string, error) {
		return os.MkdirTemp(tempRoot, "recipe-*")
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

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusFailed {
		t.Fatalf("expected failed, got %q (%s)", result.Status, result.Message)
	}
	if result.Error == nil {
		t.Fatal("expected a step error in the result")
	}
	if len(result.Outputs) != 0 {
		t.Fatalf("expected no outputs for a failed run, got %+v", result.Outputs)
	}
	if result.Steps[optimizeStepIndex].Status != models.JobStatusFailed {
		t.Fatalf("expected the optimize step to be marked failed, got %+v", result.Steps[optimizeStepIndex])
	}
	if entries := history.List(); len(entries) != 0 {
		t.Fatalf("expected no history entry for a failed run, got %+v", entries)
	}
	waitForEmptyDir(t, tempRoot)
}

func TestListRecipesReturnsCuratedRecipesInOrder(t *testing.T) {
	service, _, _ := newTestService(t)

	recipes := service.List()
	if len(recipes) != 4 {
		t.Fatalf("expected four built-in recipes, got %+v", recipes)
	}

	email := recipes[0]
	if email.ID != RecipeIDEmailPDF || email.Name != "Email PDF" {
		t.Fatalf("expected Email PDF first, got %+v", email)
	}
	if email.InputKind != "pdf" || len(email.AcceptExtensions) != 1 || email.AcceptExtensions[0] != "pdf" {
		t.Fatalf("unexpected input metadata: %+v", email)
	}
	if email.MaxInputs != 0 {
		t.Fatalf("expected Email PDF to accept unlimited inputs, got %d", email.MaxInputs)
	}
	if len(email.Highlights) != 3 || email.Highlights[0] != "Merge when needed" {
		t.Fatalf("unexpected Email PDF highlights: %+v", email.Highlights)
	}
	if len(email.Steps) != 2 || email.Steps[0].Detail != "Only when there are two or more PDFs" {
		t.Fatalf("unexpected Email PDF step details: %+v", email.Steps)
	}

	web := recipes[1]
	if web.ID != RecipeIDWebImageSet || web.Name != "Web image set" {
		t.Fatalf("expected Web image set second, got %+v", web)
	}
	if web.Category != "image" || web.InputKind != "image" {
		t.Fatalf("unexpected Web image set input metadata: %+v", web)
	}
	if web.MaxInputs != 1 {
		t.Fatalf("expected Web image set to accept one input, got %d", web.MaxInputs)
	}
	if len(web.Highlights) != 3 || web.Highlights[0] != "No upscaling" {
		t.Fatalf("unexpected Web image set highlights: %+v", web.Highlights)
	}
	if len(web.AcceptExtensions) != 4 {
		t.Fatalf("expected four accepted extensions, got %+v", web.AcceptExtensions)
	}
	if len(web.Steps) != 3 {
		t.Fatalf("expected three Web image set steps, got %+v", web.Steps)
	}
	if web.Steps[0].ID != stepIDBuildVariants || web.Steps[0].ToolID != image.ToolIDImageConvertV1 {
		t.Fatalf("unexpected build step: %+v", web.Steps[0])
	}
	if web.Steps[0].Detail == "" || web.Steps[1].Detail == "" || web.Steps[2].Detail == "" {
		t.Fatalf("expected step details for every Web image set step, got %+v", web.Steps)
	}
	if web.Steps[1].ToolID != "" || web.Steps[2].ToolID != "" {
		t.Fatalf("expected packaging steps without a tool, got %+v", web.Steps)
	}

	social := recipes[2]
	if social.ID != RecipeIDSocialClip || social.Name != "Social clip" {
		t.Fatalf("expected Social clip third, got %+v", social)
	}
	if social.Category != "video" || social.InputKind != "video" {
		t.Fatalf("unexpected Social clip input metadata: %+v", social)
	}
	if social.MaxInputs != 1 {
		t.Fatalf("expected Social clip to accept one input, got %d", social.MaxInputs)
	}
	if len(social.AcceptExtensions) != 5 {
		t.Fatalf("expected five accepted extensions, got %+v", social.AcceptExtensions)
	}
	if len(social.Highlights) != 3 || social.Highlights[0] != "MP4 + WebM" {
		t.Fatalf("unexpected Social clip highlights: %+v", social.Highlights)
	}
	if len(social.Steps) != 3 {
		t.Fatalf("expected three Social clip steps, got %+v", social.Steps)
	}
	if social.Steps[0].ID != stepIDSocialMP4 || social.Steps[0].ToolID != video.ToolIDVideoConvertV1 {
		t.Fatalf("unexpected MP4 step: %+v", social.Steps[0])
	}
	if social.Steps[1].ID != stepIDSocialWebM || social.Steps[1].ToolID != video.ToolIDVideoConvertV1 {
		t.Fatalf("unexpected WebM step: %+v", social.Steps[1])
	}
	if social.Steps[2].ID != stepIDSocialPoster || social.Steps[2].ToolID != "" {
		t.Fatalf("unexpected poster step: %+v", social.Steps[2])
	}
	if social.Steps[0].Detail == "" || social.Steps[1].Detail == "" || social.Steps[2].Detail == "" {
		t.Fatalf("expected step details for every Social clip step, got %+v", social.Steps)
	}

	cleanScan := recipes[3]
	if cleanScan.ID != RecipeIDCleanScan || cleanScan.Name != "Clean scan" {
		t.Fatalf("expected Clean scan fourth, got %+v", cleanScan)
	}
	if cleanScan.Category != "documents" || cleanScan.InputKind != "pdf" {
		t.Fatalf("unexpected Clean scan input metadata: %+v", cleanScan)
	}
	if cleanScan.MaxInputs != 1 {
		t.Fatalf("expected Clean scan to accept one input, got %d", cleanScan.MaxInputs)
	}
	if len(cleanScan.AcceptExtensions) != 1 || cleanScan.AcceptExtensions[0] != "pdf" {
		t.Fatalf("unexpected Clean scan accepted extensions: %+v", cleanScan.AcceptExtensions)
	}
	if len(cleanScan.Highlights) != 3 || cleanScan.Highlights[0] != "Local OCR" {
		t.Fatalf("unexpected Clean scan highlights: %+v", cleanScan.Highlights)
	}
	if len(cleanScan.Steps) != 2 {
		t.Fatalf("expected two Clean scan steps, got %+v", cleanScan.Steps)
	}
	if cleanScan.Steps[0].ID != stepIDCleanScanOCR || cleanScan.Steps[0].ToolID != pdf.ToolIDPDFOCRV1 {
		t.Fatalf("unexpected Clean scan OCR step: %+v", cleanScan.Steps[0])
	}
	if cleanScan.Steps[1].ID != stepIDCleanScanVerify || cleanScan.Steps[1].ToolID != "" {
		t.Fatalf("unexpected Clean scan verify step: %+v", cleanScan.Steps[1])
	}
	if cleanScan.Steps[0].Detail == "" || cleanScan.Steps[1].Detail == "" {
		t.Fatalf("expected step details for every Clean scan step, got %+v", cleanScan.Steps)
	}
}

// blockingOptimizeTool blocks in ExecuteSingle until its context is cancelled,
// so the cancel test is deterministic.
type blockingOptimizeTool struct {
	started chan struct{}
}

func (t *blockingOptimizeTool) ID() string { return pdf.ToolIDPDFOptimizeV1 }

func (t *blockingOptimizeTool) Capability() string { return t.ID() }

func (t *blockingOptimizeTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: t.ID(), Name: "Blocking Optimize"}
}

func (t *blockingOptimizeTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *blockingOptimizeTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func (t *blockingOptimizeTool) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	outputPath, _ := req.Options["outputPath"].(string)

	return models.PlanJobResponseV1{
		Success: true,
		Message: "ready to block",
		Outputs: []models.PlannedOutputV1{{
			InputPath:   firstRequestInput(req),
			OutputPath:  outputPath,
			OutputKind:  "file",
			OutputCount: 1,
		}},
	}, nil
}

func (t *blockingOptimizeTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	select {
	case t.started <- struct{}{}:
	default:
	}

	<-ctx.Done()

	return models.JobResultItemV1{
		InputPath: firstRequestInput(req),
		Success:   false,
		Message:   "optimize canceled",
	}, &models.JobErrorV1{Code: "CANCELED", DetailCode: "PDF_OPTIMIZE_FAILED", Message: "optimize canceled"}
}

// failingOptimizeTool always fails so failure cleanup can be asserted.
type failingOptimizeTool struct{}

func (t *failingOptimizeTool) ID() string { return pdf.ToolIDPDFOptimizeV1 }

func (t *failingOptimizeTool) Capability() string { return t.ID() }

func (t *failingOptimizeTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{ToolID: t.ID(), Name: "Failing Optimize"}
}

func (t *failingOptimizeTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *failingOptimizeTool) Validate(_ context.Context, _ models.JobRequestV1) *models.JobErrorV1 {
	return nil
}

func (t *failingOptimizeTool) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	outputPath, _ := req.Options["outputPath"].(string)

	return models.PlanJobResponseV1{
		Success: true,
		Message: "ready to fail",
		Outputs: []models.PlannedOutputV1{{
			InputPath:   firstRequestInput(req),
			OutputPath:  outputPath,
			OutputKind:  "file",
			OutputCount: 1,
		}},
	}, nil
}

func (t *failingOptimizeTool) ExecuteSingle(_ context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	jobErr := &models.JobErrorV1{Code: "PDF_OPTIMIZE_FAILED", DetailCode: "PDF_OPTIMIZE_FAILED", Message: "optimize failed for test"}

	return models.JobResultItemV1{
		InputPath: firstRequestInput(req),
		Success:   false,
		Message:   jobErr.Message,
		Error:     jobErr,
	}, jobErr
}

func firstRequestInput(req models.JobRequestV1) string {
	if len(req.InputPaths) > 0 {
		return req.InputPaths[0]
	}

	return ""
}

type eventRecorder struct {
	mu     sync.Mutex
	events []models.RecipeProgressEventV1
}

func (r *eventRecorder) record(event models.RecipeProgressEventV1) {
	r.mu.Lock()
	defer r.mu.Unlock()

	r.events = append(r.events, event)
}

func (r *eventRecorder) snapshot() []models.RecipeProgressEventV1 {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]models.RecipeProgressEventV1{}, r.events...)
}

func newTestService(t *testing.T) (*Service, *jobhistory.Store, *eventRecorder) {
	t.Helper()

	return newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, pdf.NewMergeTool())
		mustRegisterTool(t, reg, pdf.NewOptimizeTool())
	})
}

func newTestServiceWith(t *testing.T, setup func(*registry.Registry)) (*Service, *jobhistory.Store, *eventRecorder) {
	t.Helper()

	reg := registry.NewRegistry()
	setup(reg)

	history := jobhistory.NewStore(filepath.Join(t.TempDir(), "history", "job_history_v1.json"))
	recorder := &eventRecorder{}
	service := NewService(reg, history, recorder.record)

	return service, history, recorder
}

func mustRegisterTool(t *testing.T, reg *registry.Registry, tool tools.Tool) {
	t.Helper()

	if err := reg.RegisterToolV2(tool); err != nil {
		t.Fatalf("register tool %s: %v", tool.ID(), err)
	}
}

func writePDFFixture(t *testing.T, path string, pages int) {
	t.Helper()

	document := gofpdf.New("P", "mm", "A4", "")
	document.SetFont("Helvetica", "", 12)
	for page := 0; page < pages; page++ {
		document.AddPage()
		document.Cell(40, 10, fmt.Sprintf("Page %d", page+1))
	}
	if err := document.OutputFileAndClose(path); err != nil {
		t.Fatalf("write pdf fixture %s: %v", path, err)
	}
}

func waitForRecipeTerminal(t *testing.T, service *Service, jobID string) models.RecipeResultV1 {
	t.Helper()

	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		response := service.Status(jobID)
		if response.Found && response.Result != nil {
			switch response.Result.Status {
			case models.JobStatusSuccess,
				models.JobStatusPartialSuccess,
				models.JobStatusFailed,
				models.JobStatusCancelled,
				models.JobStatusInterrupted:
				return *response.Result
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for recipe job %s", jobID)
	return models.RecipeResultV1{}
}

func waitForRecipeEvent(t *testing.T, recorder *eventRecorder, match func(models.RecipeProgressEventV1) bool) models.RecipeProgressEventV1 {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		for _, event := range recorder.snapshot() {
			if match(event) {
				return event
			}
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("timed out waiting for a recipe event; recorded: %+v", recorder.snapshot())
	return models.RecipeProgressEventV1{}
}

func waitForPathGone(t *testing.T, path string) {
	t.Helper()

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := os.Stat(path); os.IsNotExist(err) {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	t.Fatalf("expected %s to be cleaned up", path)
}

func waitForEmptyDir(t *testing.T, dir string) {
	t.Helper()

	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("read temp root %s: %v", dir, err)
		}
		if len(entries) == 0 {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}

	entries, _ := os.ReadDir(dir)
	t.Fatalf("expected %s to be cleaned; still present: %+v", dir, entries)
}

func warningsContain(warnings []string, needle string) bool {
	for _, warning := range warnings {
		if strings.Contains(strings.ToLower(warning), strings.ToLower(needle)) {
			return true
		}
	}

	return false
}
