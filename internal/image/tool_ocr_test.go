package image

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
)

func imageOCRTestRequest(mode string, inputPaths []string, options map[string]any) models.JobRequest {
	return models.JobRequest{
		ToolID:     ToolIDImageOCR,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

// imageOCRToolSuccessfulRunner answers Tesseract with deterministic outputs so
// tool-level planning, progress and result messages can be asserted.
func imageOCRToolSuccessfulRunner(t *testing.T, text string) *imageOCRStubRunner {
	t.Helper()

	return &imageOCRStubRunner{onRun: func(_ context.Context, name string, args []string) ([]byte, error) {
		if name != "tesseract" {
			return nil, fmt.Errorf("unexpected command %q", name)
		}

		return nil, imageOCRStubOutputs(args, text, "%PDF-1.4\nstub searchable pdf\n")
	}}
}

func TestImageOCRToolManifest(t *testing.T) {
	tool := NewImageOCRTool()
	manifest := tool.Manifest()

	if manifest.ToolID != "tool.image.ocr" {
		t.Fatalf("expected tool.image.ocr, got %q", manifest.ToolID)
	}
	if manifest.Name != "Image OCR" {
		t.Fatalf("expected Image OCR, got %q", manifest.Name)
	}
	if manifest.Description == "" {
		t.Fatal("expected a description")
	}
	if manifest.Domain != "image" {
		t.Fatalf("expected domain image, got %q", manifest.Domain)
	}
	if manifest.Capability != ToolIDImageOCR {
		t.Fatalf("expected capability %q, got %q", ToolIDImageOCR, manifest.Capability)
	}
	if manifest.Version != "1.0" {
		t.Fatalf("expected version 1.0, got %q", manifest.Version)
	}
	if !manifest.SupportsSingle || !manifest.SupportsBatch {
		t.Fatalf("expected single and batch support, got %+v", manifest)
	}
	if !slices.Equal(manifest.InputExtensions, imageInputExtensions) {
		t.Fatalf("expected the shared image inputs, got %v", manifest.InputExtensions)
	}
	if !slices.Equal(manifest.OutputExtensions, []string{"txt", "pdf"}) {
		t.Fatalf("expected txt/pdf outputs, got %v", manifest.OutputExtensions)
	}
	if !slices.Equal(manifest.RuntimeDeps, []string{"tesseract", "libvips"}) {
		t.Fatalf("expected tesseract and libvips runtime deps, got %v", manifest.RuntimeDeps)
	}
	for _, tag := range []string{"image", "ocr", "text"} {
		if !slices.Contains(manifest.Tags, tag) {
			t.Fatalf("expected tag %q in %v", tag, manifest.Tags)
		}
	}
	if tool.ID() != manifest.ToolID || tool.Capability() != manifest.Capability {
		t.Fatal("expected ID and Capability to match the manifest")
	}
}

func TestImageOCRToolPlanDefaults(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	runner := &imageOCRStubRunner{}
	detectionCalls := stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng", "spa"))
	tool := NewImageOCRToolWithRunner(runner)

	plan, planErr := tool.Plan(context.Background(), imageOCRTestRequest("single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if !plan.Success {
		t.Fatal("expected a success plan")
	}
	if plan.Message != "Ready to OCR 1 image." {
		t.Fatalf("unexpected plan message %q", plan.Message)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected one planned output, got %d", len(plan.Outputs))
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "photo.txt"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.InputPath != input {
		t.Fatalf("expected input %q, got %q", input, output.InputPath)
	}
	if output.OutputKind != imageOutputKindFile || output.OutputCount != 1 {
		t.Fatalf("unexpected output metadata: %+v", output)
	}
	if output.Exists || output.Adjusted {
		t.Fatalf("expected a fresh, unadjusted output, got exists=%v adjusted=%v", output.Exists, output.Adjusted)
	}
	if _, statErr := os.Stat(output.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatal("plan must not create the output file")
	}

	wantEffective := map[string]any{
		"language": "eng",
		"writePdf": false,
		"dpi":      300,
	}
	for key, want := range wantEffective {
		if got := plan.EffectiveOptions[key]; got != want {
			t.Fatalf("expected effective %s=%v (%T), got %v (%T)", key, want, want, got, got)
		}
	}

	if *detectionCalls != 1 {
		t.Fatalf("expected one capability detection, got %d", *detectionCalls)
	}
	if len(runner.snapshotCalls()) != 0 {
		t.Fatalf("plan must not run Tesseract, got %v", runner.snapshotCalls())
	}
}

func TestImageOCRToolPlanWritePDFAndCollisions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	runner := &imageOCRStubRunner{}
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(runner)

	plan, planErr := tool.Plan(context.Background(), imageOCRTestRequest("single", []string{input}, map[string]any{"writePdf": true}))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected the text and PDF outputs, got %d", len(plan.Outputs))
	}
	if want := filepath.Join(dir, "photo.txt"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(dir, "photo_ocr.pdf"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[1].OutputPath)
	}
	if plan.EffectiveOptions["writePdf"] != true {
		t.Fatalf("expected writePdf=true, got %v", plan.EffectiveOptions["writePdf"])
	}

	// Both auto names already exist: collision-safe planning must fall back to
	// the -2 suffix documented by outputplan.
	for _, path := range []string{filepath.Join(dir, "photo.txt"), filepath.Join(dir, "photo_ocr.pdf")} {
		if err := os.WriteFile(path, []byte("existing"), 0o644); err != nil {
			t.Fatalf("write collision fixture %s: %v", path, err)
		}
	}

	collisionPlan, collisionErr := tool.Plan(context.Background(), imageOCRTestRequest("single", []string{input}, map[string]any{"writePdf": true}))
	if collisionErr != nil {
		t.Fatalf("expected the collision plan to succeed, got %v", collisionErr)
	}
	if len(collisionPlan.Outputs) != 2 {
		t.Fatalf("expected the text and PDF outputs, got %d", len(collisionPlan.Outputs))
	}
	if want := filepath.Join(dir, "photo-2.txt"); collisionPlan.Outputs[0].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, collisionPlan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(dir, "photo_ocr-2.pdf"); collisionPlan.Outputs[1].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, collisionPlan.Outputs[1].OutputPath)
	}
	for _, output := range collisionPlan.Outputs {
		if !output.Adjusted {
			t.Fatalf("expected an adjusted output, got %+v", output)
		}
		if len(output.Warnings) == 0 {
			t.Fatalf("expected an adjustment warning, got %+v", output)
		}
	}
	if len(collisionPlan.Warnings) < 2 {
		t.Fatalf("expected both adjustment warnings in the plan, got %v", collisionPlan.Warnings)
	}
}

func TestImageOCRToolPlanLanguageAndDPIOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng", "spa"))
	tool := NewImageOCRToolWithRunner(&imageOCRStubRunner{})

	cases := []struct {
		name       string
		options    map[string]any
		wantErr    bool
		wantDetail string
		wantCode   string
		wantOption string
		wantValue  any
	}{
		{
			name:       "installed language",
			options:    map[string]any{"language": "spa"},
			wantOption: "language",
			wantValue:  "spa",
		},
		{
			name:       "language expression",
			options:    map[string]any{"language": "eng+spa"},
			wantOption: "language",
			wantValue:  "eng+spa",
		},
		{
			name:       "missing language",
			options:    map[string]any{"language": "fra"},
			wantErr:    true,
			wantDetail: ImageOCRCodeLanguageUnavailable,
			wantCode:   models.ErrorCodeValidationInvalidInput,
		},
		{
			name:       "language is not a string",
			options:    map[string]any{"language": 42},
			wantErr:    true,
			wantDetail: "IMAGE_OCR_LANGUAGE_INVALID",
			wantCode:   models.ErrorCodeValidationInvalidInput,
		},
		{
			name:       "dpi below the range",
			options:    map[string]any{"dpi": 100},
			wantErr:    true,
			wantDetail: ImageOCRCodeDPIInvalid,
			wantCode:   models.ErrorCodeValidationInvalidInput,
		},
		{
			name:       "dpi above the range",
			options:    map[string]any{"dpi": 700},
			wantErr:    true,
			wantDetail: ImageOCRCodeDPIInvalid,
			wantCode:   models.ErrorCodeValidationInvalidInput,
		},
		{
			name:       "dpi is fractional",
			options:    map[string]any{"dpi": 450.5},
			wantErr:    true,
			wantDetail: ImageOCRCodeDPIInvalid,
			wantCode:   models.ErrorCodeValidationInvalidInput,
		},
		{
			name:       "dpi inside the range",
			options:    map[string]any{"dpi": 450},
			wantOption: "dpi",
			wantValue:  450,
		},
		{
			name:       "writePdf is not a boolean",
			options:    map[string]any{"writePdf": "yes"},
			wantErr:    true,
			wantDetail: "IMAGE_OCR_OPTION_INVALID",
			wantCode:   models.ErrorCodeValidationInvalidInput,
		},
		{
			name:       "writePdf enabled",
			options:    map[string]any{"writePdf": true},
			wantOption: "writePdf",
			wantValue:  true,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan, planErr := tool.Plan(context.Background(), imageOCRTestRequest("single", []string{input}, testCase.options))
			if testCase.wantErr {
				if planErr == nil {
					t.Fatalf("expected an error, got plan %+v", plan)
				}
				if planErr.DetailCode != testCase.wantDetail {
					t.Fatalf("expected detail %q, got %q", testCase.wantDetail, planErr.DetailCode)
				}
				if planErr.Code != testCase.wantCode {
					t.Fatalf("expected code %q, got %q", testCase.wantCode, planErr.Code)
				}

				return
			}

			if planErr != nil {
				t.Fatalf("expected the plan to succeed, got %v", planErr)
			}
			if got := plan.EffectiveOptions[testCase.wantOption]; got != testCase.wantValue {
				t.Fatalf("expected effective %s=%v (%T), got %v (%T)", testCase.wantOption, testCase.wantValue, testCase.wantValue, got, got)
			}
		})
	}
}

func TestImageOCRToolRuntimeStateIgnoresPDFRasterizer(t *testing.T) {
	cases := []struct {
		name         string
		capabilities engine.OCRCapabilities
		wantStatus   string
		wantHealthy  bool
		wantReason   string
	}{
		{
			name:         "tesseract and english enable the tool without pdftoppm",
			capabilities: engine.OCRCapabilities{Tesseract: true, Languages: []string{"eng"}},
			wantStatus:   "enabled",
			wantHealthy:  true,
		},
		{
			name:         "missing tesseract degrades",
			capabilities: engine.OCRCapabilities{Languages: []string{"eng"}},
			wantStatus:   "degraded",
			wantReason:   "install Tesseract",
		},
		{
			name:         "missing languages degrades",
			capabilities: engine.OCRCapabilities{Tesseract: true},
			wantStatus:   "degraded",
			wantReason:   "Tesseract language data is unavailable or could not be listed",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stubImageOCRCapabilities(t, testCase.capabilities)
			state := NewImageOCRTool().RuntimeState(context.Background())

			if state.Status != testCase.wantStatus || state.Healthy != testCase.wantHealthy {
				t.Fatalf("unexpected runtime state: %+v", state)
			}
			if testCase.wantReason != "" && !strings.Contains(state.Reason, testCase.wantReason) {
				t.Fatalf("expected reason to contain %q, got %q", testCase.wantReason, state.Reason)
			}
			if testCase.wantHealthy && state.Reason != "" {
				t.Fatalf("expected no reason for a healthy state, got %q", state.Reason)
			}
		})
	}
}

func TestImageOCRToolPlanRuntimeUnavailable(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	cases := []struct {
		name         string
		capabilities engine.OCRCapabilities
	}{
		{name: "missing tesseract", capabilities: engine.OCRCapabilities{Languages: []string{"eng"}}},
		{name: "missing languages", capabilities: engine.OCRCapabilities{Tesseract: true}},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			stubImageOCRCapabilities(t, testCase.capabilities)
			tool := NewImageOCRToolWithRunner(&imageOCRStubRunner{})

			plan, planErr := tool.Plan(context.Background(), imageOCRTestRequest("single", []string{input}, nil))
			if planErr == nil {
				t.Fatalf("expected a runtime error, got plan %+v", plan)
			}
			if planErr.Code != models.ErrorCodeRuntimeDepMissing || planErr.DetailCode != ImageOCRCodeRuntimeUnavailable {
				t.Fatalf("unexpected runtime error: %+v", planErr)
			}
			if plan.Success {
				t.Fatal("expected a failed plan")
			}
		})
	}
}

func TestImageOCRToolPlanAnimatedGIFWarning(t *testing.T) {
	dir := t.TempDir()
	animated := filepath.Join(dir, "animated.gif")
	writeCompressAnimatedGIF(t, animated)

	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(&imageOCRStubRunner{})

	plan, planErr := tool.Plan(context.Background(), imageOCRTestRequest("single", []string{animated}, nil))
	if planErr != nil {
		t.Fatalf("expected the animated plan to succeed, got %v", planErr)
	}

	wantWarning := "animated.gif: " + imageOCRAnimatedWarning
	if !slices.Contains(plan.Warnings, wantWarning) {
		t.Fatalf("expected the animated warning %q, got %v", wantWarning, plan.Warnings)
	}
}

func TestImageOCRToolPlanOutputDirectory(t *testing.T) {
	root := t.TempDir()
	dir := filepath.Join(root, "src")
	outputDir := filepath.Join(root, "out")
	for _, path := range []string{dir, outputDir} {
		if err := os.MkdirAll(path, 0o755); err != nil {
			t.Fatalf("create fixture dir %s: %v", path, err)
		}
	}

	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(&imageOCRStubRunner{})

	request := imageOCRTestRequest("single", []string{input}, nil)
	request.OutputDir = outputDir
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if want := filepath.Join(outputDir, "photo.txt"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[0].OutputPath)
	}
}

func TestImageOCRToolPlanRejectsReadOnlyDestination(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions are required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write to a 0555 directory")
	}

	t.Run("implicit input directory", func(t *testing.T) {
		root := t.TempDir()
		readOnly := filepath.Join(root, "readonly")
		if err := os.MkdirAll(readOnly, 0o755); err != nil {
			t.Fatalf("create fixture dir: %v", err)
		}

		input := filepath.Join(readOnly, "photo.png")
		writePlanPNG(t, input, 40, 30)
		if err := os.Chmod(readOnly, 0o555); err != nil {
			t.Fatalf("make fixture dir read-only: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(readOnly, 0o755)
		})

		stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
		tool := NewImageOCRToolWithRunner(&imageOCRStubRunner{})

		plan, planErr := tool.Plan(context.Background(), imageOCRTestRequest("single", []string{input}, nil))
		assertImageOCRPlanOutputDirInvalid(t, plan, planErr)
	})

	t.Run("explicit output directory", func(t *testing.T) {
		root := t.TempDir()
		inputDir := filepath.Join(root, "input")
		readOnly := filepath.Join(root, "readonly")
		for _, dir := range []string{inputDir, readOnly} {
			if err := os.MkdirAll(dir, 0o755); err != nil {
				t.Fatalf("create fixture dir %s: %v", dir, err)
			}
		}

		input := filepath.Join(inputDir, "photo.png")
		writePlanPNG(t, input, 40, 30)
		if err := os.Chmod(readOnly, 0o555); err != nil {
			t.Fatalf("make fixture dir read-only: %v", err)
		}
		t.Cleanup(func() {
			_ = os.Chmod(readOnly, 0o755)
		})

		stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
		tool := NewImageOCRToolWithRunner(&imageOCRStubRunner{})

		request := imageOCRTestRequest("single", []string{input}, nil)
		request.OutputDir = readOnly
		plan, planErr := tool.Plan(context.Background(), request)
		assertImageOCRPlanOutputDirInvalid(t, plan, planErr)
	})
}

func assertImageOCRPlanOutputDirInvalid(t *testing.T, plan models.PlanJobResponse, planErr *models.JobError) {
	t.Helper()

	if planErr == nil {
		t.Fatalf("expected the plan to reject the destination, got plan %+v", plan)
	}
	if planErr.DetailCode != ImageOCRCodeOutputDirInvalid || planErr.Code != models.ErrorCodeValidationInvalidInput {
		t.Fatalf("unexpected plan error: %+v", planErr)
	}
	if plan.Success {
		t.Fatal("expected a failed plan")
	}
	message := strings.ToLower(planErr.Message)
	if !strings.Contains(message, "output folder") {
		t.Fatalf("expected a clean destination message, got %q", planErr.Message)
	}
	if strings.Contains(message, "permission denied") {
		t.Fatalf("expected no raw OS error in the message, got %q", planErr.Message)
	}
}

func TestImageOCRToolPlanUnsupportedInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(input, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write input fixture: %v", err)
	}

	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(&imageOCRStubRunner{})

	_, planErr := tool.Plan(context.Background(), imageOCRTestRequest("single", []string{input}, nil))
	if planErr == nil {
		t.Fatal("expected an unsupported input error")
	}
	if planErr.Code != models.ErrorCodeUnsupportedFormat || planErr.DetailCode != ImageOCRCodeInputUnsupported {
		t.Fatalf("unexpected unsupported input error: %+v", planErr)
	}
}

func TestImageOCRToolExecuteSingleProgressAndMessage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	text := strings.Repeat("x", 1234)
	runner := imageOCRToolSuccessfulRunner(t, text)
	detectionCalls := stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(runner)

	var progress []models.JobProgress
	item, execErr := tool.ExecuteSingleWithProgress(context.Background(), imageOCRTestRequest("single", []string{input}, map[string]any{"writePdf": true}), func(event models.JobProgress) {
		progress = append(progress, event)
	})
	if execErr != nil {
		t.Fatalf("expected the single execution to succeed, got %v", execErr)
	}
	if !item.Success {
		t.Fatalf("expected a successful item, got %+v", item)
	}
	if item.OutputPath != filepath.Join(dir, "photo.txt") {
		t.Fatalf("unexpected primary output %q", item.OutputPath)
	}
	if !slices.Equal(item.Outputs, []string{filepath.Join(dir, "photo.txt"), filepath.Join(dir, "photo_ocr.pdf")}) {
		t.Fatalf("unexpected outputs %v", item.Outputs)
	}
	if item.OutputCount != 2 {
		t.Fatalf("expected 2 outputs, got %d", item.OutputCount)
	}
	if item.Message != "1,234 characters recognized" {
		t.Fatalf("unexpected result message %q", item.Message)
	}
	if *detectionCalls != 2 {
		// Plan detects once to validate the language and the executor detects
		// once for the whole job, never once per image.
		t.Fatalf("expected plan and execution detections, got %d", *detectionCalls)
	}

	if len(progress) != 2 {
		t.Fatalf("expected two progress events, got %+v", progress)
	}
	if progress[0].Message != "recognizing 1/1" || progress[1].Message != "processed 1/1" {
		t.Fatalf("unexpected progress messages %+v", progress)
	}
	for _, path := range item.Outputs {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected %s to exist, got %v", path, statErr)
		}
	}
}

func TestImageOCRToolExecuteSingleNoTextWarning(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "blank.png")
	writePlanPNG(t, input, 40, 30)

	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(imageOCRToolSuccessfulRunner(t, ""))

	item, execErr := tool.ExecuteSingle(context.Background(), imageOCRTestRequest("single", []string{input}, nil))
	if execErr != nil {
		t.Fatalf("a no-text result must not fail, got %v", execErr)
	}
	if !item.Success {
		t.Fatalf("expected a successful item with a warning, got %+v", item)
	}
	if item.Message != ImageOCRWarningNoText {
		t.Fatalf("expected the no-text warning message, got %q", item.Message)
	}
}

func TestImageOCRToolExecuteBatchProgress(t *testing.T) {
	dir := t.TempDir()
	inputs := make([]string, 0, 3)
	for _, name := range []string{"one.png", "two.png", "three.png"} {
		input := filepath.Join(dir, name)
		writePlanPNG(t, input, 40, 30)
		inputs = append(inputs, input)
	}

	runner := imageOCRToolSuccessfulRunner(t, "hello\n")
	detectionCalls := stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(runner)

	var progress []string
	items, batchErr := tool.ExecuteBatch(context.Background(), imageOCRTestRequest("batch", inputs, nil), func(event models.JobProgress) {
		progress = append(progress, event.Message)
	})
	if batchErr != nil {
		t.Fatalf("expected the batch to succeed, got %v", batchErr)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 result items, got %d", len(items))
	}
	for _, item := range items {
		if !item.Success || item.Message != "5 characters recognized" {
			t.Fatalf("unexpected batch item %+v", item)
		}
	}
	if !slices.Equal(progress, []string{"processed 1/3", "processed 2/3", "processed 3/3"}) {
		t.Fatalf("unexpected progress %v", progress)
	}
	if *detectionCalls != 2 {
		// Plan detects once and the whole batch detects once at execution; it
		// must not scale with the number of images.
		t.Fatalf("expected plan and execution detections, got %d", *detectionCalls)
	}
	if len(runner.snapshotCalls()) != 3 {
		t.Fatalf("expected one tesseract process per image, got %d", len(runner.snapshotCalls()))
	}
}

func TestImageOCRToolExecuteBatchPartialFailure(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "first.png")
	broken := filepath.Join(dir, "second.png")
	writePlanPNG(t, valid, 40, 30)
	if err := os.WriteFile(broken, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write broken fixture: %v", err)
	}

	// The broken input fails deterministically during normalization, so the
	// partial failure does not depend on which parallel worker runs first.
	runner := imageOCRToolSuccessfulRunner(t, "hello\n")
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(runner)

	items, batchErr := tool.ExecuteBatch(context.Background(), imageOCRTestRequest("batch", []string{valid, broken}, nil), nil)
	if batchErr == nil {
		t.Fatal("expected the failed item to report a batch error")
	}
	if batchErr.Code != models.ErrorCodeExecIOTransient || batchErr.DetailCode != ImageOCRCodeNormalizeFailed {
		t.Fatalf("unexpected batch error: %+v", batchErr)
	}
	if detail, ok := batchErr.Details["error"].(string); !ok || detail == "" {
		t.Fatalf("expected the raw cause in details, got %v", batchErr.Details)
	}
	if len(items) != 2 {
		t.Fatalf("expected both items to be reported, got %d", len(items))
	}
	if !items[0].Success {
		t.Fatalf("expected the first item to succeed, got %+v", items[0])
	}
	if items[1].Success || items[1].Message != "The image could not be prepared for OCR." {
		t.Fatalf("expected an honest failure item, got %+v", items[1])
	}
}

func TestImageOCRToolExecuteBatchCancellation(t *testing.T) {
	dir := t.TempDir()
	inputs := make([]string, 0, 2)
	for _, name := range []string{"first.png", "second.png"} {
		input := filepath.Join(dir, name)
		writePlanPNG(t, input, 40, 30)
		inputs = append(inputs, input)
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	var cancelOnce sync.Once
	runner := &imageOCRStubRunner{onRun: func(runCtx context.Context, _ string, _ []string) ([]byte, error) {
		cancelOnce.Do(cancel)
		return nil, runCtx.Err()
	}}
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(runner)

	items, batchErr := tool.ExecuteBatch(ctx, imageOCRTestRequest("batch", inputs, nil), nil)
	if batchErr == nil {
		t.Fatal("expected the cancelled batch to fail")
	}
	if batchErr.Code != models.ErrorCodeCancelledByUser || batchErr.DetailCode != ImageOCRCodeCancelled {
		t.Fatalf("expected a canonical cancellation, got %+v", batchErr)
	}

	// Workers run in parallel, so a cancelled batch reports the items that
	// actually completed. Each one must keep input order and be either a
	// success or a cancellation failure, never a retryable item failure.
	if len(items) == 0 || len(items) > len(inputs) {
		t.Fatalf("expected the completed items, got %+v", items)
	}
	previousIndex := -1
	for _, item := range items {
		index := slices.Index(inputs, item.InputPath)
		if index <= previousIndex {
			t.Fatalf("expected input order to be preserved, got %+v", items)
		}
		previousIndex = index

		if item.Success {
			continue
		}
		if item.Error == nil || item.Error.Code != models.ErrorCodeCancelledByUser {
			t.Fatalf("expected only cancellation failures, got %+v", item)
		}
	}
}

func TestImageOCRToolExecuteSingleCancellation(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	runner := &imageOCRStubRunner{onRun: func(runCtx context.Context, _ string, _ []string) ([]byte, error) {
		cancel()
		return nil, runCtx.Err()
	}}
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(runner)

	item, execErr := tool.ExecuteSingle(ctx, imageOCRTestRequest("single", []string{input}, nil))
	if execErr == nil {
		t.Fatal("expected the cancelled execution to fail")
	}
	if execErr.Code != models.ErrorCodeCancelledByUser || execErr.DetailCode != ImageOCRCodeCancelled {
		t.Fatalf("expected a canonical cancellation, got %+v", execErr)
	}
	if item.Success || item.Error == nil {
		t.Fatalf("expected a failed item, got %+v", item)
	}
}

func TestImageOCRToolExecutePlanHonorsResolvedPlan(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	runner := imageOCRToolSuccessfulRunner(t, "hello\n")
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(runner)

	request := imageOCRTestRequest("single", []string{input}, map[string]any{"writePdf": true})
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected the text and PDF outputs, got %d", len(plan.Outputs))
	}

	requestText := plan.Outputs[0].OutputPath
	requestPDF := plan.Outputs[1].OutputPath
	plannedText := filepath.Join(dir, "resolved-plan.txt")
	plannedPDF := filepath.Join(dir, "resolved-plan.pdf")
	plan.Outputs[0].OutputPath = plannedText
	plan.Outputs[1].OutputPath = plannedPDF

	items, execErr := tool.ExecutePlan(context.Background(), request, plan, nil)
	if execErr != nil {
		t.Fatalf("expected the planned OCR to succeed, got %v", execErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected one item per planned output, got %d", len(items))
	}
	if !items[0].Success || items[0].OutputPath != plannedText {
		t.Fatalf("expected the planned text output, got %+v", items[0])
	}
	if !items[1].Success || items[1].OutputPath != plannedPDF {
		t.Fatalf("expected the planned PDF sidecar, got %+v", items[1])
	}
	for _, path := range []string{plannedText, plannedPDF} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected %s on disk: %v", path, statErr)
		}
	}
	for _, path := range []string{requestText, requestPDF} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("expected the request-resolved output %q to be unused, got %v", path, statErr)
		}
	}
}

func TestImageOCRToolExecutePlanBatchSubset(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writePlanPNG(t, first, 40, 30)
	writePlanPNG(t, second, 40, 30)

	runner := imageOCRToolSuccessfulRunner(t, "hello\n")
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(runner)

	request := imageOCRTestRequest("batch", []string{first, second}, nil)
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	subset := plan
	subset.Outputs = plan.Outputs[1:]

	items, execErr := tool.ExecutePlan(context.Background(), request, subset, nil)
	if execErr != nil {
		t.Fatalf("expected the subset to succeed, got %v", execErr)
	}
	if len(items) != 1 || items[0].InputPath != second || items[0].OutputPath != plan.Outputs[1].OutputPath {
		t.Fatalf("expected only the second planned output, got %+v", items)
	}
	if calls := runner.snapshotCalls(); len(calls) != 1 {
		t.Fatalf("expected one Tesseract run for the subset, got %d", len(calls))
	}
	if _, statErr := os.Stat(plan.Outputs[1].OutputPath); statErr != nil {
		t.Fatalf("expected the subset output to exist: %v", statErr)
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the excluded output %q to stay untouched, got %v", plan.Outputs[0].OutputPath, statErr)
	}
}

func TestImageOCRToolExecutePlanGroupsPDFSidecars(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writePlanPNG(t, first, 40, 30)
	writePlanPNG(t, second, 40, 30)

	runner := imageOCRToolSuccessfulRunner(t, "hello\n")
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(runner)

	request := imageOCRTestRequest("batch", []string{first, second}, map[string]any{"writePdf": true})
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 4 {
		t.Fatalf("expected text and PDF outputs for both inputs, got %d", len(plan.Outputs))
	}

	items, execErr := tool.ExecutePlan(context.Background(), request, plan, nil)
	if execErr != nil {
		t.Fatalf("expected the planned OCR to succeed, got %v", execErr)
	}
	if len(items) != 4 {
		t.Fatalf("expected one item per planned output, got %d", len(items))
	}
	if calls := runner.snapshotCalls(); len(calls) != 2 {
		t.Fatalf("expected one Tesseract run per input, got %d", len(calls))
	}

	for index, inputPath := range []string{first, second} {
		textItem := items[index*2]
		pdfItem := items[index*2+1]
		if textItem.InputPath != inputPath || pdfItem.InputPath != inputPath {
			t.Fatalf("expected plan order for %s, got %+v", inputPath, items)
		}
		if !textItem.Success || !pdfItem.Success {
			t.Fatalf("expected both outputs to succeed for %s, got %+v", inputPath, items)
		}
		if !slices.Equal(textItem.Outputs, []string{plan.Outputs[index*2].OutputPath, plan.Outputs[index*2+1].OutputPath}) {
			t.Fatalf("expected the text item to declare both outputs, got %+v", textItem)
		}
		if pdfItem.OutputPath != plan.Outputs[index*2+1].OutputPath {
			t.Fatalf("expected the PDF sidecar item, got %+v", pdfItem)
		}
	}
}

func TestImageOCRToolExecutePlanDetectsCapabilitiesOnce(t *testing.T) {
	dir := t.TempDir()
	inputs := make([]string, 0, 3)
	for _, name := range []string{"one.png", "two.png", "three.png"} {
		input := filepath.Join(dir, name)
		writePlanPNG(t, input, 40, 30)
		inputs = append(inputs, input)
	}

	runner := imageOCRToolSuccessfulRunner(t, "hello\n")
	detectionCalls := stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(runner)

	request := imageOCRTestRequest("batch", inputs, nil)
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}
	before := *detectionCalls

	if _, execErr := tool.ExecutePlan(context.Background(), request, plan, nil); execErr != nil {
		t.Fatalf("expected the planned OCR to succeed, got %v", execErr)
	}
	if got := *detectionCalls - before; got != 1 {
		t.Fatalf("expected one execution detection for the whole plan, got %d", got)
	}
	if calls := runner.snapshotCalls(); len(calls) != len(inputs) {
		t.Fatalf("expected one Tesseract run per input, got %d", len(calls))
	}
}

func TestImageOCRToolExecutePlanPreservesOrderAndItemFailures(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.png")
	broken := filepath.Join(dir, "broken.png")
	writePlanPNG(t, valid, 40, 30)
	if err := os.WriteFile(broken, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write broken fixture: %v", err)
	}

	runner := imageOCRToolSuccessfulRunner(t, "hello\n")
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(runner)

	request := imageOCRTestRequest("batch", []string{valid, broken}, nil)
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}

	var progress []int
	items, execErr := tool.ExecutePlan(context.Background(), request, plan, func(event models.JobProgress) {
		progress = append(progress, event.Current)
	})
	if execErr == nil {
		t.Fatal("expected the failed item to report a batch error")
	}
	if execErr.DetailCode != ImageOCRCodeNormalizeFailed {
		t.Fatalf("expected %s, got %q", ImageOCRCodeNormalizeFailed, execErr.DetailCode)
	}
	if len(items) != 2 {
		t.Fatalf("expected both planned items, got %d", len(items))
	}
	if !items[0].Success || items[0].InputPath != valid || items[0].OutputPath != plan.Outputs[0].OutputPath {
		t.Fatalf("expected the first planned item to succeed, got %+v", items[0])
	}
	if items[1].Success || items[1].Error == nil || items[1].InputPath != broken {
		t.Fatalf("expected the broken planned item to fail, got %+v", items[1])
	}
	if len(progress) != 2 || progress[0] != 1 || progress[1] != 2 {
		t.Fatalf("expected monotonic plan-order progress, got %v", progress)
	}
}

func TestImageOCRToolExecutePlanCancellation(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writePlanPNG(t, first, 40, 30)
	writePlanPNG(t, second, 40, 30)

	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))
	tool := NewImageOCRToolWithRunner(imageOCRToolSuccessfulRunner(t, "hello\n"))

	request := imageOCRTestRequest("batch", []string{first, second}, nil)
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	items, execErr := tool.ExecutePlan(ctx, request, plan, nil)
	if execErr == nil {
		t.Fatal("expected cancellation")
	}
	if execErr.Code != models.ErrorCodeCancelledByUser || execErr.DetailCode != ImageOCRCodeCancelled {
		t.Fatalf("expected a canonical cancellation, got %+v", execErr)
	}
	for _, item := range items {
		if item.Success {
			t.Fatalf("expected no successful item after cancel, got %+v", item)
		}
	}
	for _, planned := range plan.Outputs {
		if _, statErr := os.Stat(planned.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("expected no output after cancel, got %v for %q", statErr, planned.OutputPath)
		}
	}
}
