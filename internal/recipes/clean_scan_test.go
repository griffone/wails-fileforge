package recipes

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jung-kurt/gofpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"

	"fileforge-desktop/internal/jobhistory"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/pdf/engine"
	"fileforge-desktop/internal/registry"
)

// requireCleanScanRuntime skips the test when the machine has no usable OCR
// runtime. The test environment may only have `afr` installed.
func requireCleanScanRuntime(t *testing.T) engine.OCRCapabilities {
	t.Helper()

	capabilities := engine.DetectCapabilities(context.Background(), nil)
	if !capabilities.Available() {
		t.Skipf("local OCR is unavailable: %s", pdf.OCRUnavailableReason(capabilities))
	}

	return capabilities
}

func newCleanScanTestService(t *testing.T) (*Service, *jobhistory.Store, *eventRecorder) {
	t.Helper()

	return newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, pdf.NewMergeTool())
		mustRegisterTool(t, reg, pdf.NewOptimizeTool())
		mustRegisterTool(t, reg, pdf.NewOCRTool())
	})
}

// writeCleanScanTextPDF writes a text PDF whose glyphs are large enough for a
// reliable OCR pass.
func writeCleanScanTextPDF(t *testing.T, path string, pages int) {
	t.Helper()

	document := gofpdf.New("P", "mm", "A4", "")
	document.SetFont("Helvetica", "", 28)
	for page := 0; page < pages; page++ {
		document.AddPage()
		document.SetXY(20, 40)
		document.MultiCell(0, 16, cleanScanPageText(page), "", "L", false)
		document.SetXY(20, 80)
		document.MultiCell(0, 16, "FileForge Clean scan test", "", "L", false)
	}
	if err := document.OutputFileAndClose(path); err != nil {
		t.Fatalf("write clean scan fixture %s: %v", path, err)
	}
}

func cleanScanPageText(page int) string {
	return fmt.Sprintf("Page %d", page+1)
}

func writeCleanScanBlankPDF(t *testing.T, path string) {
	t.Helper()

	document := gofpdf.New("P", "mm", "A4", "")
	document.AddPage()
	if err := document.OutputFileAndClose(path); err != nil {
		t.Fatalf("write blank pdf fixture %s: %v", path, err)
	}
}

func TestListIncludesCleanScanLast(t *testing.T) {
	service, _, _ := newCleanScanTestService(t)

	recipes := service.List()
	if len(recipes) != 5 {
		t.Fatalf("expected 5 recipes, got %+v", recipes)
	}

	wantIDs := []string{RecipeIDEmailPDF, RecipeIDWebImageSet, RecipeIDSocialClip, RecipeIDCleanScan, RecipeIDBurnedSubtitles}
	for index, recipe := range recipes {
		if recipe.ID != wantIDs[index] {
			t.Fatalf("expected recipe %d to be %q, got %q", index, wantIDs[index], recipe.ID)
		}
	}
}

func TestPlanCleanScan(t *testing.T) {
	capabilities := requireCleanScanRuntime(t)
	service, _, _ := newCleanScanTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeCleanScanTextPDF(t, input, 2)

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDCleanScan,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}
	if plan.Recipe == nil || plan.Recipe.ID != RecipeIDCleanScan || plan.Recipe.Name != "Clean scan" {
		t.Fatalf("unexpected recipe metadata: %+v", plan.Recipe)
	}
	if plan.Recipe.MaxInputs != 1 || plan.Recipe.InputKind != "pdf" || plan.Recipe.Category != "documents" {
		t.Fatalf("unexpected recipe constraints: %+v", plan.Recipe)
	}
	if len(plan.Recipe.Steps) != 2 {
		t.Fatalf("expected 2 steps, got %+v", plan.Recipe.Steps)
	}
	if plan.Recipe.Steps[0].ToolID != pdf.ToolIDPDFOCR || plan.Recipe.Steps[1].ToolID != "" {
		t.Fatalf("unexpected step tools: %+v", plan.Recipe.Steps)
	}
	if strings.Join(plan.Recipe.Highlights, ",") != "Local OCR,Searchable PDF,Text sidecar" {
		t.Fatalf("unexpected highlights: %v", plan.Recipe.Highlights)
	}

	if plan.OutputDir != dir {
		t.Fatalf("expected the output folder to default to the input folder, got %q", plan.OutputDir)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected the PDF and TXT outputs, got %+v", plan.Outputs)
	}

	pdfOutput := plan.Outputs[0]
	textOutput := plan.Outputs[1]
	if pdfOutput.OutputPath != filepath.Join(dir, "scan_clean.pdf") || pdfOutput.OutputCount != 1 {
		t.Fatalf("unexpected PDF output: %+v", pdfOutput)
	}
	if textOutput.OutputPath != filepath.Join(dir, "scan_clean.txt") || textOutput.OutputCount != 1 {
		t.Fatalf("unexpected TXT output: %+v", textOutput)
	}
	for _, output := range plan.Outputs {
		if output.OutputKind != "file" || output.Exists {
			t.Fatalf("unexpected output metadata: %+v", output)
		}
	}

	if plan.Message == "" {
		t.Fatal("expected a plan message")
	}
	if !warningsContain(plan.Warnings, "rasterized") {
		t.Fatalf("expected a rasterization warning, got %v", plan.Warnings)
	}
	if !warningsContain(plan.Warnings, "invisible text layer") {
		t.Fatalf("expected the invisible text layer note, got %v", plan.Warnings)
	}
	language := capabilities.RecommendedLanguage
	if language != "eng" && language != "spa" && !warningsContain(plan.Warnings, "install eng/spa language data") {
		t.Fatalf("expected the low-language warning for %q, got %v", language, plan.Warnings)
	}
}

func TestPlanCleanScanCollisionSafe(t *testing.T) {
	requireCleanScanRuntime(t)
	service, _, _ := newCleanScanTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeCleanScanTextPDF(t, input, 1)
	for _, taken := range []string{"scan_clean.pdf", "scan_clean.txt"} {
		if err := os.WriteFile(filepath.Join(dir, taken), []byte("taken"), 0o644); err != nil {
			t.Fatalf("write existing %s: %v", taken, err)
		}
	}

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDCleanScan,
		InputPaths: []string{input},
	})
	if !plan.Success {
		t.Fatalf("expected a successful plan, got %+v", plan.Error)
	}
	wantOutputs := [2]string{
		filepath.Join(dir, "scan_clean-2.pdf"),
		filepath.Join(dir, "scan_clean-2.txt"),
	}
	for index, want := range wantOutputs {
		if plan.Outputs[index].OutputPath != want {
			t.Fatalf("expected output %d %q, got %q", index, want, plan.Outputs[index].OutputPath)
		}
	}
	if !warningsContain(plan.Warnings, "already exists") {
		t.Fatalf("expected collision warnings, got %v", plan.Warnings)
	}
}

func TestPlanCleanScanRejectsInvalidInputs(t *testing.T) {
	requireCleanScanRuntime(t)
	service, _, _ := newCleanScanTestService(t)

	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeCleanScanTextPDF(t, first, 1)
	writeCleanScanTextPDF(t, second, 1)
	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write txt fixture: %v", err)
	}

	cases := []struct {
		name   string
		inputs []string
	}{
		{name: "no inputs"},
		{name: "multiple inputs", inputs: []string{first, second}},
		{name: "non pdf input", inputs: []string{textPath}},
		{name: "missing input", inputs: []string{filepath.Join(dir, "missing.pdf")}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan := service.Plan(models.RecipeRequest{
				RecipeID:   RecipeIDCleanScan,
				InputPaths: testCase.inputs,
			})
			if plan.Success {
				t.Fatalf("expected the plan to fail, got %+v", plan)
			}
			if plan.Error == nil || plan.Error.Message == "" {
				t.Fatalf("expected a plan error, got %+v", plan)
			}
		})
	}
}

func TestPlanCleanScanRejectsUnknownLanguage(t *testing.T) {
	requireCleanScanRuntime(t)
	service, _, _ := newCleanScanTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeCleanScanTextPDF(t, input, 1)

	plan := service.Plan(models.RecipeRequest{
		RecipeID:   RecipeIDCleanScan,
		InputPaths: []string{input},
		Options:    map[string]any{"language": "not-a-language"},
	})
	if plan.Success {
		t.Fatal("expected an uninstalled language to fail planning")
	}
	if !warningsContain([]string{plan.Message}, "Installed languages") {
		t.Fatalf("expected the installed languages in the message, got %q", plan.Message)
	}
}

func TestRunCleanScanProducesSearchableOutputs(t *testing.T) {
	requireCleanScanRuntime(t)
	service, history, recorder := newCleanScanTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeCleanScanTextPDF(t, input, 2)

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDCleanScan,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusSuccess {
		t.Fatalf("expected success, got %s: %s (%+v)", result.Status, result.Message, result.Error)
	}
	if result.Verification == nil || !result.Verification.Success {
		t.Fatalf("expected verification to pass, got %+v", result.Verification)
	}

	pdfPath := filepath.Join(dir, "scan_clean.pdf")
	textPath := filepath.Join(dir, "scan_clean.txt")
	if len(result.Outputs) != 2 || result.Outputs[0] != pdfPath || result.Outputs[1] != textPath {
		t.Fatalf("unexpected outputs: %+v", result.Outputs)
	}

	pages, pageErr := api.PageCountFile(pdfPath)
	if pageErr != nil || pages != 2 {
		t.Fatalf("expected 2 pages, got %d (err=%v)", pages, pageErr)
	}
	extracted, ok := engine.ExtractPDFText(context.Background(), nil, pdfPath)
	if !ok || engine.CountTextCharacters(extracted) == 0 {
		t.Fatal("expected the output PDF to be searchable")
	}
	if text, readErr := os.ReadFile(textPath); readErr != nil || engine.CountTextCharacters(string(text)) == 0 {
		t.Fatalf("expected a non-empty sidecar, read err = %v", readErr)
	}

	if len(result.Steps) != 2 {
		t.Fatalf("expected 2 step results, got %+v", result.Steps)
	}
	if result.Steps[0].Status != recipeStepStatusSuccess || result.Steps[0].ToolID != pdf.ToolIDPDFOCR {
		t.Fatalf("unexpected OCR step: %+v", result.Steps[0])
	}
	if result.Steps[1].Status != recipeStepStatusSuccess {
		t.Fatalf("unexpected verify step: %+v", result.Steps[1])
	}

	entries := history.List()
	if len(entries) != 1 || entries[0].JobID != run.JobID || entries[0].ToolID != cleanScanHistoryToolID {
		t.Fatalf("unexpected history entries: %+v", entries)
	}
	if len(entries[0].Outputs) != 2 {
		t.Fatalf("expected both outputs recorded for undo, got %+v", entries[0].Outputs)
	}

	undo := history.Undo(run.JobID)
	if !undo.Success || len(undo.Moved) != 2 {
		t.Fatalf("expected both outputs to be undoable, got %+v", undo)
	}
	restore := history.Restore(run.JobID)
	if !restore.Success || len(restore.Moved) != 2 {
		t.Fatalf("expected both outputs to be restorable, got %+v", restore)
	}

	terminalEvent := waitForRecipeEvent(t, recorder, func(event models.RecipeProgressEvent) bool {
		return event.Status == models.JobStatusSuccess && event.Percent == 100
	})
	if terminalEvent.RecipeID != RecipeIDCleanScan {
		t.Fatalf("unexpected terminal event: %+v", terminalEvent)
	}
}

func TestRunCleanScanReportsPartialSuccessWithoutText(t *testing.T) {
	requireCleanScanRuntime(t)
	service, _, _ := newCleanScanTestService(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "blank.pdf")
	writeCleanScanBlankPDF(t, input)

	run := service.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDCleanScan,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, service, run.JobID)
	if result.Status != models.JobStatusPartialSuccess {
		t.Fatalf("expected partial_success, got %s: %s (%+v)", result.Status, result.Message, result.Verification)
	}
	if result.Verification == nil || result.Verification.Success {
		t.Fatalf("expected verification to report the missing text, got %+v", result.Verification)
	}
	if !strings.Contains(strings.ToLower(result.Message), "no searchable text") {
		t.Fatalf("expected a clear no-text message, got %q", result.Message)
	}

	for _, output := range result.Outputs {
		if _, statErr := os.Stat(output); statErr != nil {
			t.Fatalf("expected the partial output %s to be kept, stat err = %v", output, statErr)
		}
	}
}

func TestRunCleanScanCleansUpCreatedOutputsOnFailure(t *testing.T) {
	requireCleanScanRuntime(t)

	// The OCR tool is wrapped so it writes both planned outputs and then fails,
	// simulating a pipeline crash after the tool started writing.
	failingService, failingHistory, _ := newTestServiceWith(t, func(reg *registry.Registry) {
		mustRegisterTool(t, reg, pdf.NewMergeTool())
		mustRegisterTool(t, reg, pdf.NewOptimizeTool())
		mustRegisterTool(t, reg, &failingPartialOCRTool{OCRTool: pdf.NewOCRTool()})
	})

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeCleanScanTextPDF(t, input, 1)

	run := failingService.Run(context.Background(), models.RecipeRequest{
		RecipeID:   RecipeIDCleanScan,
		InputPaths: []string{input},
	})
	if !run.Success {
		t.Fatalf("expected the recipe to be submitted, got %+v", run)
	}

	result := waitForRecipeTerminal(t, failingService, run.JobID)
	if result.Status != models.JobStatusFailed {
		t.Fatalf("expected failure, got %s: %s", result.Status, result.Message)
	}
	if result.Message == "" {
		t.Fatal("expected a failure message")
	}

	for _, path := range []string{
		filepath.Join(dir, "scan_clean.pdf"),
		filepath.Join(dir, "scan_clean.txt"),
	} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("expected %s to be cleaned up, stat err = %v", path, statErr)
		}
	}
	if entries := failingHistory.List(); len(entries) != 0 {
		t.Fatalf("expected no history entry for a failed run, got %+v", entries)
	}
}

// failingPartialOCRTool writes partial outputs and then fails, simulating a
// pipeline crash after the tool started writing.
type failingPartialOCRTool struct {
	*pdf.OCRTool
}

func (t *failingPartialOCRTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

func (t *failingPartialOCRTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, _ func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	plan, planErr := t.OCRTool.Plan(ctx, req)
	if planErr != nil {
		return models.JobResultItem{Success: false, Message: planErr.Message, Error: planErr}, planErr
	}

	for _, output := range plan.Outputs {
		if writeErr := os.WriteFile(output.OutputPath, []byte("partial"), 0o644); writeErr != nil {
			jobErr := models.NewJobError(models.ErrorCodeExecIOTransient, engine.OCRCodePublishFailed, "could not write the partial output", nil)
			return models.JobResultItem{InputPath: req.InputPaths[0], Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
		}
	}

	jobErr := models.NewJobError(models.ErrorCodeExecIOTransient, engine.OCRCodeRecognizeFailed, "simulated OCR failure", nil)

	return models.JobResultItem{
		InputPath: req.InputPaths[0],
		Success:   false,
		Message:   jobErr.Message,
		Error:     jobErr,
	}, jobErr
}

// ExecutePlan mirrors the partial-output failure on the engine's plan-once path,
// which the engine prefers over ExecuteSingle.
func (t *failingPartialOCRTool) ExecutePlan(_ context.Context, req models.JobRequest, plan models.PlanJobResponse, _ func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	for _, output := range plan.Outputs {
		if writeErr := os.WriteFile(output.OutputPath, []byte("partial"), 0o644); writeErr != nil {
			jobErr := models.NewJobError(models.ErrorCodeExecIOTransient, engine.OCRCodePublishFailed, "could not write the partial output", nil)
			return failedPlanItems(plan, jobErr), jobErr
		}
	}

	jobErr := models.NewJobError(models.ErrorCodeExecIOTransient, engine.OCRCodeRecognizeFailed, "simulated OCR failure", nil)

	return failedPlanItems(plan, jobErr), jobErr
}
