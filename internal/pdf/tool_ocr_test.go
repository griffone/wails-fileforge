package pdf

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
)

func ocrTestCapabilities() engine.OCRCapabilities {
	return engine.OCRCapabilities{
		PDFToPPM:            true,
		Tesseract:           true,
		PDFToText:           true,
		Languages:           []string{"afr", "eng", "spa"},
		RecommendedLanguage: "eng",
	}
}

func newFakeCapabilityOCRTool() *OCRTool {
	return NewOCRToolWithDeps(nil, func(context.Context) engine.OCRCapabilities {
		return ocrTestCapabilities()
	})
}

// realOCRTestLanguage returns the recommended language detected on this
// machine and skips the test when OCR cannot run.
func realOCRTestLanguage(t *testing.T) string {
	t.Helper()

	capabilities := engine.DetectCapabilities(context.Background(), nil)
	if !capabilities.Available() {
		t.Skipf("local OCR is unavailable: %s", OCRUnavailableReason(capabilities))
	}

	return capabilities.RecommendedLanguage
}

// writeOCRTextPDFFixture writes a text PDF large enough for OCR recognition.
func writeOCRTextPDFFixture(t *testing.T, path string, pages int) {
	t.Helper()

	document := gofpdf.New("P", "mm", "A4", "")
	document.SetFont("Helvetica", "", 28)
	for page := 0; page < pages; page++ {
		document.AddPage()
		document.SetXY(20, 40)
		document.MultiCell(0, 16, fmt.Sprintf("Page %d", page+1), "", "L", false)
		document.SetXY(20, 80)
		document.MultiCell(0, 16, "FileForge OCR tool test", "", "L", false)
	}
	if err := document.OutputFileAndClose(path); err != nil {
		t.Fatalf("write text pdf fixture %s: %v", path, err)
	}
}

func ocrTestRequest(mode string, inputPaths []string, options map[string]any) models.JobRequest {
	return models.JobRequest{
		ToolID:     ToolIDPDFOCR,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

func TestOCRToolManifest(t *testing.T) {
	manifest := NewOCRTool().Manifest()
	if manifest.ToolID != "tool.pdf.ocr" {
		t.Fatalf("expected tool.pdf.ocr, got %q", manifest.ToolID)
	}
	if manifest.Name != "PDF OCR" {
		t.Fatalf("expected the PDF OCR name, got %q", manifest.Name)
	}
	if !manifest.SupportsSingle || !manifest.SupportsBatch {
		t.Fatal("expected OCR to support single and batch")
	}
	if len(manifest.OutputExtensions) != 2 || manifest.OutputExtensions[0] != "pdf" || manifest.OutputExtensions[1] != "txt" {
		t.Fatalf("unexpected output extensions: %v", manifest.OutputExtensions)
	}
	if strings.Join(manifest.RuntimeDeps, ",") != "pdftoppm,tesseract" {
		t.Fatalf("unexpected runtime dependencies: %v", manifest.RuntimeDeps)
	}
}

func TestOCRToolPlanAutoOutputs(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writeOCRTextPDFFixture(t, input, 1)

	tool := newFakeCapabilityOCRTool()
	plan, planErr := tool.Plan(context.Background(), ocrTestRequest("single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if !plan.Success || len(plan.Outputs) != 2 {
		t.Fatalf("expected the PDF and TXT outputs, got %+v", plan)
	}

	pdfPath := filepath.Join(dir, "notes_ocr.pdf")
	textPath := filepath.Join(dir, "notes_ocr.txt")
	if plan.Outputs[0].OutputPath != pdfPath || plan.Outputs[0].OutputKind != "file" || plan.Outputs[0].OutputCount != 1 {
		t.Fatalf("unexpected PDF output: %+v", plan.Outputs[0])
	}
	if plan.Outputs[1].OutputPath != textPath || plan.Outputs[1].OutputKind != "file" || plan.Outputs[1].OutputCount != 1 {
		t.Fatalf("unexpected TXT output: %+v", plan.Outputs[1])
	}
	if plan.EffectiveOptions["language"] != "eng" {
		t.Fatalf("expected the recommended language, got %v", plan.EffectiveOptions["language"])
	}
	if plan.EffectiveOptions["textSidecar"] != true {
		t.Fatalf("expected the sidecar to default to true, got %v", plan.EffectiveOptions["textSidecar"])
	}
	if plan.EffectiveOptions["dpi"] != 300 {
		t.Fatalf("expected the default 300 DPI, got %v", plan.EffectiveOptions["dpi"])
	}
	for _, output := range plan.Outputs {
		if _, statErr := os.Stat(output.OutputPath); !os.IsNotExist(statErr) {
			t.Fatalf("plan must not create %s, stat err = %v", output.OutputPath, statErr)
		}
	}
	if !warningsContainOCRTool(plan.Warnings, "rasterized") {
		t.Fatalf("expected a rasterization warning, got %v", plan.Warnings)
	}
}

func TestOCRToolPlanExplicitOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writeOCRTextPDFFixture(t, input, 1)
	outputPath := filepath.Join(dir, "custom.pdf")

	tool := newFakeCapabilityOCRTool()
	plan, planErr := tool.Plan(context.Background(), ocrTestRequest("single", []string{input}, map[string]any{
		"outputPath": outputPath,
	}))
	if planErr != nil {
		t.Fatalf("expected explicit output plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 || plan.Outputs[0].OutputPath != outputPath {
		t.Fatalf("unexpected explicit outputs: %+v", plan.Outputs)
	}
	if plan.Outputs[1].OutputPath != filepath.Join(dir, "custom.txt") {
		t.Fatalf("expected the sidecar to derive from the PDF name, got %q", plan.Outputs[1].OutputPath)
	}

	if err := os.WriteFile(outputPath, []byte("taken"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}
	_, existsErr := tool.Plan(context.Background(), ocrTestRequest("single", []string{input}, map[string]any{
		"outputPath": outputPath,
	}))
	if existsErr == nil {
		t.Fatal("expected an existing explicit output to fail planning")
	}

	_, extensionErr := tool.Plan(context.Background(), ocrTestRequest("single", []string{input}, map[string]any{
		"outputPath": filepath.Join(dir, "custom.txt"),
	}))
	if extensionErr == nil {
		t.Fatal("expected a non-pdf explicit output to fail planning")
	}

	_, batchErr := tool.Plan(context.Background(), ocrTestRequest("batch", []string{input, input}, map[string]any{
		"outputPath": filepath.Join(dir, "custom.pdf"),
	}))
	if batchErr == nil {
		t.Fatal("expected options.outputPath to be rejected for batch")
	}
}

func TestOCRToolPlanWithoutSidecar(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writeOCRTextPDFFixture(t, input, 1)

	tool := newFakeCapabilityOCRTool()
	plan, planErr := tool.Plan(context.Background(), ocrTestRequest("single", []string{input}, map[string]any{
		"textSidecar": false,
	}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected one output without the sidecar, got %+v", plan.Outputs)
	}
	if plan.Outputs[0].OutputCount != 1 {
		t.Fatalf("expected one output count, got %d", plan.Outputs[0].OutputCount)
	}
	if plan.EffectiveOptions["textSidecar"] != false {
		t.Fatalf("expected the sidecar to be disabled, got %v", plan.EffectiveOptions["textSidecar"])
	}
}

func TestOCRToolPlanBatchOutputsAreCollisionSafe(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "notes_ocr.pdf"), []byte("taken"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	firstDir := filepath.Join(dir, "first")
	secondDir := filepath.Join(dir, "second")
	for _, inputDir := range []string{firstDir, secondDir} {
		if err := os.MkdirAll(inputDir, 0o755); err != nil {
			t.Fatalf("create input dir: %v", err)
		}
	}
	first := filepath.Join(firstDir, "notes.pdf")
	second := filepath.Join(secondDir, "notes.pdf")
	writeOCRTextPDFFixture(t, first, 1)
	writeOCRTextPDFFixture(t, second, 1)

	req := ocrTestRequest("batch", []string{first, second}, nil)
	req.OutputDir = outDir

	tool := newFakeCapabilityOCRTool()
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 4 {
		t.Fatalf("expected 2 PDFs and 2 TXT outputs, got %+v", plan.Outputs)
	}

	wantPaths := []string{
		filepath.Join(outDir, "notes_ocr-2.pdf"),
		filepath.Join(outDir, "notes_ocr-2.txt"),
		filepath.Join(outDir, "notes_ocr-3.pdf"),
		filepath.Join(outDir, "notes_ocr-3.txt"),
	}
	for index, output := range plan.Outputs {
		if output.OutputPath != wantPaths[index] {
			t.Fatalf("expected output %d %q, got %q", index, wantPaths[index], output.OutputPath)
		}
	}
}

func TestOCRToolPlanUnavailableRuntime(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writeOCRTextPDFFixture(t, input, 1)

	cases := []struct {
		name         string
		capabilities engine.OCRCapabilities
		messagePart  string
	}{
		{
			name:         "missing binaries",
			capabilities: engine.OCRCapabilities{Languages: []string{"eng"}, RecommendedLanguage: "eng"},
			messagePart:  "pdftoppm and tesseract",
		},
		{
			name:         "no languages",
			capabilities: engine.OCRCapabilities{PDFToPPM: true, Tesseract: true, PDFToText: true},
			messagePart:  "no Tesseract language data",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tool := NewOCRToolWithDeps(nil, func(context.Context) engine.OCRCapabilities {
				return testCase.capabilities
			})
			_, planErr := tool.Plan(context.Background(), ocrTestRequest("single", []string{input}, nil))
			if planErr == nil {
				t.Fatal("expected planning to fail")
			}
			if planErr.Code != models.ErrorCodeRuntimeDepMissing {
				t.Fatalf("expected RUNTIME_DEP_MISSING, got %q", planErr.Code)
			}
			if !strings.Contains(planErr.Message, testCase.messagePart) {
				t.Fatalf("expected %q in the message, got %q", testCase.messagePart, planErr.Message)
			}
		})
	}
}

func TestOCRToolPlanRejectsUnknownLanguage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writeOCRTextPDFFixture(t, input, 1)

	tool := newFakeCapabilityOCRTool()
	_, planErr := tool.Plan(context.Background(), ocrTestRequest("single", []string{input}, map[string]any{
		"language": "deu",
	}))
	if planErr == nil {
		t.Fatal("expected an uninstalled language to fail planning")
	}
	if planErr.Code != models.ErrorCodeValidationInvalidInput {
		t.Fatalf("expected VALIDATION_INVALID_INPUT, got %q", planErr.Code)
	}
	if planErr.DetailCode != engine.OCRCodeLanguageUnavailable {
		t.Fatalf("expected OCR_LANGUAGE_UNAVAILABLE, got %q", planErr.DetailCode)
	}
	for _, language := range []string{"afr", "eng", "spa"} {
		if !strings.Contains(planErr.Message, language) {
			t.Fatalf("expected the installed language %q in the message, got %q", language, planErr.Message)
		}
	}
}

func TestOCRToolPlanRejectsInvalidRequests(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writeOCRTextPDFFixture(t, input, 1)
	other := filepath.Join(dir, "second.pdf")
	writeOCRTextPDFFixture(t, other, 1)

	tool := newFakeCapabilityOCRTool()
	cases := []struct {
		name string
		req  models.JobRequest
	}{
		{name: "invalid mode", req: ocrTestRequest("nope", []string{input}, nil)},
		{name: "single with two inputs", req: ocrTestRequest("single", []string{input, other}, nil)},
		{name: "batch without inputs", req: ocrTestRequest("batch", nil, nil)},
		{name: "non pdf input", req: ocrTestRequest("single", []string{filepath.Join(dir, "notes.txt")}, nil)},
		{name: "dpi below range", req: ocrTestRequest("single", []string{input}, map[string]any{"dpi": 72})},
		{name: "dpi above range", req: ocrTestRequest("single", []string{input}, map[string]any{"dpi": 1200})},
		{name: "invalid sidecar option", req: ocrTestRequest("single", []string{input}, map[string]any{"textSidecar": "maybe"})},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if _, planErr := tool.Plan(context.Background(), testCase.req); planErr == nil {
				t.Fatal("expected planning to fail")
			}
		})
	}
}

func TestOCRToolExecuteSingleWithProgress(t *testing.T) {
	language := realOCRTestLanguage(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeOCRTextPDFFixture(t, input, 2)

	tool := NewOCRTool()
	req := ocrTestRequest("single", []string{input}, map[string]any{"language": language})

	progress := make([]models.JobProgress, 0, 2)
	item, jobErr := tool.ExecuteSingleWithProgress(context.Background(), req, func(update models.JobProgress) {
		progress = append(progress, update)
	})
	if jobErr != nil {
		t.Fatalf("expected OCR to succeed, got %v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected a successful item, got %+v", item)
	}

	pdfPath := filepath.Join(dir, "scan_ocr.pdf")
	textPath := filepath.Join(dir, "scan_ocr.txt")
	if item.OutputPath != pdfPath || item.OutputCount != 2 || len(item.Outputs) != 2 {
		t.Fatalf("unexpected item outputs: %+v", item)
	}
	if item.Outputs[0] != pdfPath || item.Outputs[1] != textPath {
		t.Fatalf("unexpected outputs: %+v", item.Outputs)
	}

	pages, pageErr := api.PageCountFile(context.Background(), pdfPath)
	if pageErr != nil || pages != 2 {
		t.Fatalf("expected 2 pages, got %d (err=%v)", pages, pageErr)
	}
	if text, readErr := os.ReadFile(textPath); readErr != nil || len(strings.TrimSpace(string(text))) == 0 {
		t.Fatalf("expected a non-empty sidecar, read err = %v", readErr)
	}
	if extracted, ok := engine.ExtractPDFText(context.Background(), nil, pdfPath); !ok || engine.CountTextCharacters(extracted) == 0 {
		t.Fatal("expected the output PDF to be searchable")
	}

	if !strings.Contains(item.Message, "2 pages") || !strings.Contains(item.Message, "characters recognized") {
		t.Fatalf("unexpected success message: %q", item.Message)
	}

	if len(progress) != 2 {
		t.Fatalf("expected one progress update per page, got %+v", progress)
	}
	last := progress[len(progress)-1]
	if last.Current != 2 || last.Total != 2 || last.Stage != "ocr" || !strings.Contains(last.Message, "2 of 2 pages") {
		t.Fatalf("unexpected final progress: %+v", last)
	}
}

func TestOCRToolExecuteSingleWithoutSidecar(t *testing.T) {
	language := realOCRTestLanguage(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeOCRTextPDFFixture(t, input, 1)

	tool := NewOCRTool()
	item, jobErr := tool.ExecuteSingle(context.Background(), ocrTestRequest("single", []string{input}, map[string]any{
		"language":    language,
		"textSidecar": false,
	}))
	if jobErr != nil {
		t.Fatalf("expected OCR to succeed, got %v", jobErr)
	}
	if item.OutputCount != 1 || len(item.Outputs) != 1 || item.Outputs[0] != item.OutputPath {
		t.Fatalf("expected only the PDF output, got %+v", item)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "scan_ocr.txt")); !os.IsNotExist(statErr) {
		t.Fatalf("expected no sidecar, stat err = %v", statErr)
	}
}

func TestOCRToolExecuteBatch(t *testing.T) {
	language := realOCRTestLanguage(t)

	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "first.pdf"),
		filepath.Join(dir, "second.pdf"),
	}
	for _, input := range inputs {
		writeOCRTextPDFFixture(t, input, 1)
	}

	tool := NewOCRTool()
	items, jobErr := tool.ExecuteBatch(context.Background(), ocrTestRequest("batch", inputs, map[string]any{
		"language": language,
	}), nil)
	if jobErr != nil {
		t.Fatalf("expected batch OCR to succeed, got %v", jobErr)
	}
	if len(items) != 4 {
		t.Fatalf("expected one result per planned output, got %+v", items)
	}

	// With the text sidecar enabled the plan has a PDF and a TXT output per
	// input; ExecutePlan reports one item per planned output in plan order.
	firstPDF := filepath.Join(dir, "first_ocr.pdf")
	firstText := filepath.Join(dir, "first_ocr.txt")
	secondPDF := filepath.Join(dir, "second_ocr.pdf")
	secondText := filepath.Join(dir, "second_ocr.txt")
	want := [][]string{
		{firstPDF, firstText},
		{firstText},
		{secondPDF, secondText},
		{secondText},
	}
	for index, outputs := range want {
		item := items[index]
		if !item.Success || item.OutputCount != len(outputs) || strings.Join(item.Outputs, ",") != strings.Join(outputs, ",") {
			t.Fatalf("unexpected item %d: %+v", index, item)
		}
		for _, output := range item.Outputs {
			info, statErr := os.Stat(output)
			if statErr != nil || info.Size() == 0 {
				t.Fatalf("expected %s to exist and be non-empty, stat err = %v", output, statErr)
			}
		}
	}
}

func TestOCRToolCancellationLeavesNoOutputs(t *testing.T) {
	language := realOCRTestLanguage(t)

	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeOCRTextPDFFixture(t, input, 2)

	runner := &blockingCommandRunner{
		language: language,
		blockOn:  "pdftoppm",
	}
	tool := NewOCRToolWithDeps(runner, func(context.Context) engine.OCRCapabilities {
		return ocrTestCapabilities()
	})

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	item, jobErr := tool.ExecuteSingleWithProgress(ctx, ocrTestRequest("single", []string{input}, map[string]any{
		"language": language,
	}), nil)
	if jobErr == nil {
		t.Fatal("expected cancellation to fail the run")
	}
	if jobErr.Code != models.ErrorCodeCancelledByUser {
		t.Fatalf("expected CANCELLED_BY_USER, got %q", jobErr.Code)
	}
	if item.Success {
		t.Fatalf("expected a failed item, got %+v", item)
	}

	for _, path := range []string{filepath.Join(dir, "scan_ocr.pdf"), filepath.Join(dir, "scan_ocr.txt")} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("expected %s to be absent, stat err = %v", path, statErr)
		}
	}

	entries, readErr := os.ReadDir(tempRoot)
	if readErr != nil {
		t.Fatalf("read temp root: %v", readErr)
	}
	if len(entries) != 0 {
		t.Fatalf("expected the OCR workspace to be removed, found %d entries", len(entries))
	}
}

// blockingCommandRunner answers capability detection and the process commands
// that run before the blocked one, then waits for cancellation.
type blockingCommandRunner struct {
	language string
	blockOn  string
}

func (r *blockingCommandRunner) Output(ctx context.Context, name string, args []string) ([]byte, error) {
	if name == "tesseract" && len(args) > 0 && args[0] == "--list-langs" {
		return []byte("List of available languages (2):\n" + r.language + "\nosd\n"), nil
	}
	if name == r.blockOn {
		<-ctx.Done()
		return nil, ctx.Err()
	}

	return nil, fmt.Errorf("unexpected command: %s %v", name, args)
}

func warningsContainOCRTool(warnings []string, part string) bool {
	for _, warning := range warnings {
		if strings.Contains(strings.ToLower(warning), strings.ToLower(part)) {
			return true
		}
	}

	return false
}

func TestOCRToolRuntimeStateAndCapabilities(t *testing.T) {
	detected := engine.DetectCapabilities(context.Background(), nil)
	tool := NewOCRTool()

	state := tool.RuntimeState(context.Background())
	if detected.Available() != state.Healthy {
		t.Fatalf("expected healthy=%t to match detection %+v, got %+v", detected.Available(), detected, state)
	}
	if state.Healthy && (state.Status != "enabled" || state.Reason != "") {
		t.Fatalf("unexpected enabled state: %+v", state)
	}
	if !state.Healthy && (state.Status != "degraded" || state.Reason == "") {
		t.Fatalf("unexpected degraded state: %+v", state)
	}

	response := OCRCapabilities(context.Background())
	if !response.Success || response.Message == "" {
		t.Fatalf("unexpected capability response: %+v", response)
	}
	if response.PDFToPPM != detected.PDFToPPM || response.Tesseract != detected.Tesseract || response.PDFToText != detected.PDFToText {
		t.Fatalf("expected the binaries to match detection %+v, got %+v", detected, response)
	}
	if strings.Join(response.Languages, ",") != strings.Join(detected.Languages, ",") {
		t.Fatalf("expected languages %v, got %v", detected.Languages, response.Languages)
	}
	if response.RecommendedLanguage != detected.RecommendedLanguage {
		t.Fatalf("expected the recommended language %q, got %q", detected.RecommendedLanguage, response.RecommendedLanguage)
	}
	if response.Available != detected.Available() {
		t.Fatalf("expected available=%t, got %t", detected.Available(), response.Available)
	}
}

func TestOCRToolExecutePlanHonorsResolvedPlan(t *testing.T) {
	language := realOCRTestLanguage(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeOCRTextPDFFixture(t, input, 1)

	hookCalls := 0
	tool := NewOCRToolWithDeps(nil, func(ctx context.Context) engine.OCRCapabilities {
		hookCalls++
		return engine.DetectCapabilities(ctx, nil)
	})
	req := ocrTestRequest("single", []string{input}, map[string]any{"language": language})

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected the PDF and TXT outputs, got %d", len(plan.Outputs))
	}
	if _, ok := plan.EffectiveOptions["capabilities"].(engine.OCRCapabilities); !ok {
		t.Fatalf("expected typed capabilities in the plan, got %#v", plan.EffectiveOptions["capabilities"])
	}

	requestPDF := plan.Outputs[0].OutputPath
	requestText := plan.Outputs[1].OutputPath
	plannedPDF := filepath.Join(dir, "resolved-plan.pdf")
	plannedText := filepath.Join(dir, "resolved-plan.txt")
	plan.Outputs[0].OutputPath = plannedPDF
	plan.Outputs[1].OutputPath = plannedText

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("expected planned OCR to succeed, got %v", execErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected one item per planned output, got %d", len(items))
	}
	if !items[0].Success || items[0].OutputPath != plannedPDF {
		t.Fatalf("expected the planned PDF output, got %+v", items[0])
	}
	if !items[1].Success || items[1].OutputPath != plannedText {
		t.Fatalf("expected the planned TXT output, got %+v", items[1])
	}

	for _, path := range []string{plannedPDF, plannedText} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected %s on disk: %v", path, statErr)
		}
	}
	for _, path := range []string{requestPDF, requestText} {
		if _, statErr := os.Stat(path); !os.IsNotExist(statErr) {
			t.Fatalf("expected the request-resolved output %q to be unused, got %v", path, statErr)
		}
	}
	if hookCalls != 1 {
		t.Fatalf("expected capabilities to be detected once for the plan, got %d calls", hookCalls)
	}
}

func TestOCRToolExecutePlanBatchSubset(t *testing.T) {
	language := realOCRTestLanguage(t)

	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeOCRTextPDFFixture(t, first, 1)
	writeOCRTextPDFFixture(t, second, 1)

	tool := NewOCRTool()
	req := ocrTestRequest("batch", []string{first, second}, map[string]any{"language": language})

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 4 {
		t.Fatalf("expected 4 planned outputs, got %d", len(plan.Outputs))
	}

	// A reduced retry plan can contain a single side of an output pair.
	subset := plan
	subset.Outputs = plan.Outputs[2:3]

	items, execErr := tool.ExecutePlan(context.Background(), req, subset, nil)
	if execErr != nil {
		t.Fatalf("expected the subset to succeed, got %v", execErr)
	}
	if len(items) != 1 || items[0].InputPath != second || items[0].OutputPath != plan.Outputs[2].OutputPath {
		t.Fatalf("expected only the second planned PDF output, got %+v", items)
	}
	if _, statErr := os.Stat(plan.Outputs[2].OutputPath); statErr != nil {
		t.Fatalf("expected the subset output on disk: %v", statErr)
	}
	for _, index := range []int{0, 1, 3} {
		if _, statErr := os.Stat(plan.Outputs[index].OutputPath); !os.IsNotExist(statErr) {
			t.Fatalf("expected the excluded output %q to stay untouched, got %v", plan.Outputs[index].OutputPath, statErr)
		}
	}
}

func TestOCRToolExecutePlanUsesPlannedCapabilitiesOnce(t *testing.T) {
	language := realOCRTestLanguage(t)

	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeOCRTextPDFFixture(t, first, 1)
	writeOCRTextPDFFixture(t, second, 1)

	hookCalls := 0
	tool := NewOCRToolWithDeps(nil, func(ctx context.Context) engine.OCRCapabilities {
		hookCalls++
		return engine.DetectCapabilities(ctx, nil)
	})
	req := ocrTestRequest("batch", []string{first, second}, map[string]any{
		"language":    language,
		"textSidecar": false,
	})

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if hookCalls != 1 {
		t.Fatalf("expected capabilities to be detected once for the plan, got %d calls", hookCalls)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	// Replace the plan runtime with one that disables pdftotext. ExecutePlan
	// must use this snapshot, not probe again, and the warning is observable.
	plan.EffectiveOptions["capabilities"] = engine.OCRCapabilities{
		PDFToPPM:  true,
		Tesseract: true,
		PDFToText: false,
		Languages: []string{language},
	}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("expected planned batch OCR to succeed, got %v", execErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected one item per planned output, got %d", len(items))
	}
	for index, wantInput := range []string{first, second} {
		if items[index].InputPath != wantInput || !items[index].Success {
			t.Fatalf("expected item %d for %q, got %+v", index, wantInput, items[index])
		}
		if !strings.Contains(items[index].Message, engine.OCRWarningPDFToTextUnavailable) {
			t.Fatalf("expected the plan capability warning on item %d, got %q", index, items[index].Message)
		}
	}
	if hookCalls != 1 {
		t.Fatalf("expected no capability detection during execution, got %d calls", hookCalls)
	}
}

func TestOCRToolExecutePlanPreservesOrderAndItemFailures(t *testing.T) {
	language := realOCRTestLanguage(t)

	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.pdf")
	broken := filepath.Join(dir, "broken.pdf")
	writeOCRTextPDFFixture(t, valid, 1)
	if err := os.WriteFile(broken, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write invalid pdf: %v", err)
	}

	tool := NewOCRTool()
	req := ocrTestRequest("batch", []string{valid, broken}, map[string]any{
		"language":    language,
		"textSidecar": false,
	})

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr == nil {
		t.Fatal("expected an aggregate OCR failure")
	}
	if execErr.DetailCode != engine.OCRCodeRecognizeFailed {
		t.Fatalf("expected %s, got %q", engine.OCRCodeRecognizeFailed, execErr.DetailCode)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items in plan order, got %d", len(items))
	}
	if items[0].InputPath != valid || !items[0].Success {
		t.Fatalf("expected the valid input to succeed first, got %+v", items[0])
	}
	if items[1].InputPath != broken || items[1].Success || items[1].Error == nil {
		t.Fatalf("expected the broken input to fail second, got %+v", items[1])
	}

	fileErrors, ok := execErr.Details["fileErrors"].([]map[string]any)
	if !ok || len(fileErrors) != 1 || fileErrors[0]["path"] != broken {
		t.Fatalf("expected one file error for the broken input, got %#v", execErr.Details["fileErrors"])
	}
}
