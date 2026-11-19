package pdf

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
)

func imagesToolRequest(inputPaths []string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     ToolIDPDFImagesV1,
		Mode:       "single",
		InputPaths: inputPaths,
		Options:    options,
	}
}

// writeImagesToolImage writes a small PNG fixture, or a JPEG when the
// destination uses a .jpg/.jpeg extension.
func writeImagesToolImage(t *testing.T, path string) {
	t.Helper()

	const width, height = 64, 48
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.Set(x, y, color.RGBA{R: uint8(x * 3), G: uint8(y * 4), B: 100, A: 255})
		}
	}

	file, createErr := os.Create(path)
	if createErr != nil {
		t.Fatalf("create image fixture %s: %v", path, createErr)
	}

	var encodeErr error
	switch strings.ToLower(filepath.Ext(path)) {
	case ".jpg", ".jpeg":
		encodeErr = jpeg.Encode(file, canvas, &jpeg.Options{Quality: 90})
	default:
		encodeErr = png.Encode(file, canvas)
	}
	if encodeErr != nil {
		_ = file.Close()
		t.Fatalf("encode image fixture %s: %v", path, encodeErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		t.Fatalf("close image fixture %s: %v", path, closeErr)
	}
}

func imagesToolEffectiveSlots(t *testing.T, plan models.PlanJobResponseV1) []map[string]any {
	t.Helper()

	slots, ok := plan.EffectiveOptions["slots"].([]map[string]any)
	if !ok {
		t.Fatalf("expected effective slots as []map[string]any, got %#v", plan.EffectiveOptions["slots"])
	}

	return slots
}

func TestImagesToolManifest(t *testing.T) {
	tool := NewImagesToPDFTool()
	manifest := tool.Manifest()

	if manifest.ToolID != "tool.pdf.images" {
		t.Fatalf("expected the tool.pdf.images tool ID, got %q", manifest.ToolID)
	}
	if manifest.Name != "Images to PDF" {
		t.Fatalf("expected the Images to PDF name, got %q", manifest.Name)
	}
	if manifest.Description != "Place several images on a page grid and export one PDF." {
		t.Fatalf("unexpected description: %q", manifest.Description)
	}
	if manifest.Domain != "pdf" || manifest.Version != "v1" {
		t.Fatalf("unexpected domain/version: %q/%q", manifest.Domain, manifest.Version)
	}
	if !manifest.SupportsSingle || manifest.SupportsBatch {
		t.Fatalf("expected single-only support, got single=%v batch=%v", manifest.SupportsSingle, manifest.SupportsBatch)
	}
	if got := strings.Join(manifest.InputExtensions, ","); got != "jpg,jpeg,png,gif,webp,bmp,tiff,tif" {
		t.Fatalf("unexpected input extensions: %q", got)
	}
	if got := strings.Join(manifest.OutputExtensions, ","); got != "pdf" {
		t.Fatalf("unexpected output extensions: %q", got)
	}
	if got := strings.Join(manifest.RuntimeDeps, ","); got != "libvips,pdfcpu" {
		t.Fatalf("unexpected runtime dependencies: %q", got)
	}
	if got := strings.Join(manifest.Tags, ","); got != "pdf,images,layout,grid,album" {
		t.Fatalf("unexpected tags: %q", got)
	}
	if tool.Capability() != ToolIDPDFImagesV1 {
		t.Fatalf("expected the capability to be the tool ID, got %q", tool.Capability())
	}

	state := tool.RuntimeState(context.Background())
	if state.Status != "enabled" || !state.Healthy {
		t.Fatalf("expected an enabled and healthy runtime, got %+v", state)
	}
}

func TestImagesToolPlanSuccess(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.jpeg")
	writeImagesToolImage(t, first)
	writeImagesToolImage(t, second)

	tool := NewImagesToPDFTool()
	plan, planErr := tool.Plan(context.Background(), imagesToolRequest([]string{first, second}, map[string]any{
		"pageSize":    "letter",
		"orientation": "landscape",
		"columns":     2,
		"rows":        1,
		"marginMm":    float64(6),
		"gapMm":       float64(2),
		"fit":         "cover",
	}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if !plan.Success {
		t.Fatal("expected a successful plan")
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected 1 planned output, got %d", len(plan.Outputs))
	}

	output := plan.Outputs[0]
	outputPath := filepath.Join(dir, "first_images.pdf")
	if output.OutputPath != outputPath {
		t.Fatalf("expected output %q, got %q", outputPath, output.OutputPath)
	}
	if output.InputPath != first+","+second {
		t.Fatalf("expected the joined input path, got %q", output.InputPath)
	}
	if output.OutputKind != "file" || output.OutputCount != 1 {
		t.Fatalf("unexpected output metadata: %+v", output)
	}
	if output.Adjusted {
		t.Fatal("expected a natural auto output to be unadjusted")
	}
	if output.Exists {
		t.Fatal("expected the planned output to not exist yet")
	}
	if plan.Message != "Ready to place 2 images on 1 page(s)." {
		t.Fatalf("unexpected plan message: %q", plan.Message)
	}

	wantOptions := map[string]any{
		"pageSize":    "letter",
		"orientation": "landscape",
		"columns":     2,
		"rows":        1,
		"marginMm":    float64(6),
		"gapMm":       float64(2),
		"fit":         "cover",
	}
	for key, want := range wantOptions {
		if got := plan.EffectiveOptions[key]; got != want {
			t.Fatalf("expected effective option %s=%v, got %v", key, want, got)
		}
	}

	wantSlots := []map[string]any{
		{"page": 0, "cell": 0, "inputIndex": 0},
		{"page": 0, "cell": 1, "inputIndex": 1},
	}
	if got := imagesToolEffectiveSlots(t, plan); !reflect.DeepEqual(got, wantSlots) {
		t.Fatalf("expected slots %v, got %v", wantSlots, got)
	}

	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("plan must not create the output, stat err = %v", statErr)
	}
}

func TestImagesToolPlanResolvesExplicitAndChosenOutput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writeImagesToolImage(t, first)
	writeImagesToolImage(t, second)

	tool := NewImagesToPDFTool()

	chosenDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(chosenDir, 0o755); err != nil {
		t.Fatalf("create chosen output dir: %v", err)
	}
	chosenRequest := imagesToolRequest([]string{first, second}, map[string]any{
		"columns": 2,
		"rows":    1,
	})
	chosenRequest.OutputDir = chosenDir

	chosenPlan, chosenErr := tool.Plan(context.Background(), chosenRequest)
	if chosenErr != nil {
		t.Fatalf("expected the chosen output dir plan to succeed, got %v", chosenErr)
	}
	if want := filepath.Join(chosenDir, "first_images.pdf"); chosenPlan.Outputs[0].OutputPath != want {
		t.Fatalf("expected output %q in the chosen folder, got %q", want, chosenPlan.Outputs[0].OutputPath)
	}

	explicitPath := filepath.Join(dir, "album.pdf")
	explicitPlan, explicitErr := tool.Plan(context.Background(), imagesToolRequest([]string{first, second}, map[string]any{
		"columns":    2,
		"rows":       1,
		"outputPath": explicitPath,
	}))
	if explicitErr != nil {
		t.Fatalf("expected the explicit output plan to succeed, got %v", explicitErr)
	}

	explicitOutput := explicitPlan.Outputs[0]
	if explicitOutput.OutputPath != explicitPath {
		t.Fatalf("expected the explicit output %q, got %q", explicitPath, explicitOutput.OutputPath)
	}
	if explicitOutput.Adjusted {
		t.Fatal("an explicit output must never be suffixed")
	}
	if len(explicitOutput.Warnings) != 0 {
		t.Fatalf("expected no warnings for an explicit output, got %v", explicitOutput.Warnings)
	}
}

func TestImagesToolPlanAutoFillsSlots(t *testing.T) {
	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "first.png"),
		filepath.Join(dir, "second.png"),
		filepath.Join(dir, "third.png"),
		filepath.Join(dir, "fourth.png"),
		filepath.Join(dir, "fifth.png"),
	}
	for _, input := range inputs {
		writeImagesToolImage(t, input)
	}

	tool := NewImagesToPDFTool()
	plan, planErr := tool.Plan(context.Background(), imagesToolRequest(inputs, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if !plan.Success || len(plan.Outputs) != 1 {
		t.Fatalf("expected a successful plan with one output, got %+v", plan)
	}
	if plan.Outputs[0].OutputPath != filepath.Join(dir, "first_images.pdf") {
		t.Fatalf("unexpected auto output: %q", plan.Outputs[0].OutputPath)
	}
	if plan.Message != "Ready to place 5 images on 2 page(s)." {
		t.Fatalf("unexpected plan message: %q", plan.Message)
	}

	wantOptions := map[string]any{
		"pageSize":    "a4",
		"orientation": "portrait",
		"columns":     2,
		"rows":        2,
		"marginMm":    float64(10),
		"gapMm":       float64(4),
		"fit":         "contain",
	}
	for key, want := range wantOptions {
		if got := plan.EffectiveOptions[key]; got != want {
			t.Fatalf("expected default option %s=%v, got %v", key, want, got)
		}
	}

	wantSlots := []map[string]any{
		{"page": 0, "cell": 0, "inputIndex": 0},
		{"page": 0, "cell": 1, "inputIndex": 1},
		{"page": 0, "cell": 2, "inputIndex": 2},
		{"page": 0, "cell": 3, "inputIndex": 3},
		{"page": 1, "cell": 0, "inputIndex": 4},
	}
	if got := imagesToolEffectiveSlots(t, plan); !reflect.DeepEqual(got, wantSlots) {
		t.Fatalf("expected auto-filled slots %v, got %v", wantSlots, got)
	}
}

func TestImagesToolPlanUsesExplicitSlots(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writeImagesToolImage(t, first)
	writeImagesToolImage(t, second)

	cases := []struct {
		name  string
		slots any
	}{
		{
			name: "json slots",
			slots: []any{
				map[string]any{"page": 0, "cell": 1, "inputIndex": 0},
				map[string]any{"page": 0, "cell": 0, "inputIndex": 1},
			},
		},
		{
			name: "engine slots",
			slots: []engine.ImageSlot{
				{Page: 0, Cell: 1, InputIndex: 0},
				{Page: 0, Cell: 0, InputIndex: 1},
			},
		},
	}

	wantSlots := []map[string]any{
		{"page": 0, "cell": 1, "inputIndex": 0},
		{"page": 0, "cell": 0, "inputIndex": 1},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tool := NewImagesToPDFTool()
			plan, planErr := tool.Plan(context.Background(), imagesToolRequest([]string{first, second}, map[string]any{
				"columns": 2,
				"rows":    1,
				"slots":   testCase.slots,
			}))
			if planErr != nil {
				t.Fatalf("expected plan to succeed, got %v", planErr)
			}
			if plan.Message != "Ready to place 2 images on 1 page(s)." {
				t.Fatalf("unexpected plan message: %q", plan.Message)
			}
			if got := imagesToolEffectiveSlots(t, plan); !reflect.DeepEqual(got, wantSlots) {
				t.Fatalf("expected the explicit slot order %v, got %v", wantSlots, got)
			}
		})
	}
}

func TestImagesToolPlanErrors(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writeImagesToolImage(t, first)
	writeImagesToolImage(t, second)

	textFile := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textFile, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write unsupported fixture: %v", err)
	}

	existingOutput := filepath.Join(dir, "taken.pdf")
	if err := os.WriteFile(existingOutput, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	batchRequest := imagesToolRequest([]string{first}, nil)
	batchRequest.Mode = "batch"

	cases := []struct {
		name           string
		req            models.JobRequestV1
		wantCode       string
		wantDetailCode string
	}{
		{
			name:     "batch mode",
			req:      batchRequest,
			wantCode: "VALIDATION_ERROR",
		},
		{
			name:           "no inputs",
			req:            imagesToolRequest(nil, nil),
			wantDetailCode: engine.ImagesErrorInvalidInput,
		},
		{
			name:           "duplicate input paths",
			req:            imagesToolRequest([]string{first, first}, nil),
			wantDetailCode: engine.ImagesErrorDuplicateInput,
		},
		{
			name:           "unsupported extension",
			req:            imagesToolRequest([]string{textFile}, nil),
			wantDetailCode: engine.ImagesErrorUnsupportedFormat,
		},
		{
			name:           "missing file",
			req:            imagesToolRequest([]string{filepath.Join(dir, "missing.png")}, nil),
			wantDetailCode: engine.ImagesErrorInvalidInput,
		},
		{
			name:           "columns below range",
			req:            imagesToolRequest([]string{first}, map[string]any{"columns": 0}),
			wantDetailCode: engine.ImagesErrorInvalidLayout,
		},
		{
			name:           "columns above range",
			req:            imagesToolRequest([]string{first}, map[string]any{"columns": 5}),
			wantDetailCode: engine.ImagesErrorInvalidLayout,
		},
		{
			name:           "rows below range",
			req:            imagesToolRequest([]string{first}, map[string]any{"rows": 0}),
			wantDetailCode: engine.ImagesErrorInvalidLayout,
		},
		{
			name:           "negative margin",
			req:            imagesToolRequest([]string{first}, map[string]any{"marginMm": -1}),
			wantDetailCode: engine.ImagesErrorInvalidLayout,
		},
		{
			name:           "gap above range",
			req:            imagesToolRequest([]string{first}, map[string]any{"gapMm": 50}),
			wantDetailCode: engine.ImagesErrorInvalidLayout,
		},
		{
			name:           "invalid fit",
			req:            imagesToolRequest([]string{first}, map[string]any{"fit": "stretch"}),
			wantDetailCode: engine.ImagesErrorInvalidLayout,
		},
		{
			name: "unplaced image",
			req: imagesToolRequest([]string{first, second}, map[string]any{
				"columns": 2,
				"rows":    1,
				"slots": []any{
					map[string]any{"page": 0, "cell": 0, "inputIndex": 0},
				},
			}),
			wantDetailCode: engine.ImagesErrorUnplacedImages,
		},
		{
			name:           "existing explicit output",
			req:            imagesToolRequest([]string{first}, map[string]any{"outputPath": existingOutput}),
			wantDetailCode: engine.ImagesErrorOutputExists,
		},
		{
			name:           "output collides with input",
			req:            imagesToolRequest([]string{first}, map[string]any{"outputPath": first}),
			wantDetailCode: engine.ImagesErrorOutputExists,
		},
		{
			name:           "malformed slot entry",
			req:            imagesToolRequest([]string{first}, map[string]any{"slots": []any{"not-a-slot"}}),
			wantDetailCode: engine.ImagesErrorInvalidLayout,
		},
		{
			name:           "slot missing field",
			req:            imagesToolRequest([]string{first}, map[string]any{"slots": []any{map[string]any{"page": 0, "cell": 0}}}),
			wantDetailCode: engine.ImagesErrorInvalidLayout,
		},
	}

	tool := NewImagesToPDFTool()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan, planErr := tool.Plan(context.Background(), testCase.req)
			if planErr == nil {
				t.Fatal("expected planning to fail")
			}
			if plan.Success {
				t.Fatal("expected the plan to report failure")
			}
			if planErr.Message == "" {
				t.Fatal("expected an error message")
			}
			if testCase.wantCode != "" && planErr.Code != testCase.wantCode {
				t.Fatalf("expected code %q, got %q", testCase.wantCode, planErr.Code)
			}
			if testCase.wantDetailCode != "" && planErr.DetailCode != testCase.wantDetailCode {
				t.Fatalf("expected detail code %q, got %q (%s)", testCase.wantDetailCode, planErr.DetailCode, planErr.Message)
			}

			validateErr := tool.Validate(context.Background(), testCase.req)
			if validateErr == nil {
				t.Fatal("expected Validate to report the same failure")
			}
			if validateErr.Code != planErr.Code || validateErr.DetailCode != planErr.DetailCode || validateErr.Message != planErr.Message {
				t.Fatalf("Validate returned %+v, plan returned %+v", validateErr, planErr)
			}
		})
	}
}

func TestImagesToolPlanAdjustsExistingAutoOutput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writeImagesToolImage(t, first)
	writeImagesToolImage(t, second)

	naturalPath := filepath.Join(dir, "first_images.pdf")
	if err := os.WriteFile(naturalPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	tool := NewImagesToPDFTool()
	plan, planErr := tool.Plan(context.Background(), imagesToolRequest([]string{first, second}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	wantPath := filepath.Join(dir, "first_images-2.pdf")
	if output.OutputPath != wantPath {
		t.Fatalf("expected the suffixed output %q, got %q", wantPath, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected the adjusted flag when the natural name exists")
	}

	wantWarning := "first_images.pdf already exists. Saving as first_images-2.pdf."
	if len(output.Warnings) != 1 || output.Warnings[0] != wantWarning {
		t.Fatalf("expected warning %q, got %v", wantWarning, output.Warnings)
	}

	content, readErr := os.ReadFile(naturalPath)
	if readErr != nil {
		t.Fatalf("read existing output: %v", readErr)
	}
	if string(content) != "existing" {
		t.Fatal("plan must not overwrite the existing output")
	}
}

func TestImagesToolExecuteSingleSuccess(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writeImagesToolImage(t, first)
	writeImagesToolImage(t, second)

	tool := NewImagesToPDFTool()
	item, jobErr := tool.ExecuteSingle(context.Background(), imagesToolRequest([]string{first, second}, map[string]any{
		"columns": 2,
		"rows":    1,
	}))
	if jobErr != nil {
		t.Fatalf("expected execution to succeed, got %v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected a successful item, got %q", item.Message)
	}

	outputPath := filepath.Join(dir, "first_images.pdf")
	if item.OutputPath != outputPath {
		t.Fatalf("expected output %q, got %q", outputPath, item.OutputPath)
	}
	if item.InputPath != first+","+second {
		t.Fatalf("expected the joined input path, got %q", item.InputPath)
	}
	if item.OutputCount != 1 || len(item.Outputs) != 1 || item.Outputs[0] != outputPath {
		t.Fatalf("expected outputs [%q], got %+v", outputPath, item.Outputs)
	}
	if item.Message != "2 images on 1 page(s)." {
		t.Fatalf("unexpected success message: %q", item.Message)
	}

	pageCount, pageErr := api.PageCountFile(outputPath)
	if pageErr != nil {
		t.Fatalf("read generated page count: %v", pageErr)
	}
	if pageCount != 1 {
		t.Fatalf("expected 1 page, got %d", pageCount)
	}

	info, statErr := os.Stat(outputPath)
	if statErr != nil || info.Size() == 0 {
		t.Fatalf("expected a non-empty output, stat err = %v", statErr)
	}
}

func TestImagesToolExecuteSingleWithProgress(t *testing.T) {
	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "first.png"),
		filepath.Join(dir, "second.png"),
		filepath.Join(dir, "third.png"),
	}
	for _, input := range inputs {
		writeImagesToolImage(t, input)
	}

	tool := NewImagesToPDFTool()
	progress := make([]models.JobProgressV1, 0, len(inputs))
	item, jobErr := tool.ExecuteSingleWithProgress(context.Background(), imagesToolRequest(inputs, nil), func(update models.JobProgressV1) {
		progress = append(progress, update)
	})
	if jobErr != nil {
		t.Fatalf("expected execution to succeed, got %v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected a successful item, got %q", item.Message)
	}

	if len(progress) != len(inputs) {
		t.Fatalf("expected one progress update per image, got %+v", progress)
	}
	for index, update := range progress {
		if update.Current != index+1 || update.Total != len(inputs) {
			t.Fatalf("expected progress %d/%d, got %+v", index+1, len(inputs), update)
		}
		if update.Stage != "running" {
			t.Fatalf("expected the running stage, got %q", update.Stage)
		}
		if want := fmt.Sprintf("Placed %d of %d images", index+1, len(inputs)); update.Message != want {
			t.Fatalf("expected message %q, got %q", want, update.Message)
		}
	}

	last := progress[len(progress)-1]
	if last.Current != len(inputs) || last.Total != len(inputs) {
		t.Fatalf("expected the final progress to be %d/%d, got %+v", len(inputs), len(inputs), last)
	}
}

func TestImagesToolExecuteSingleCancellation(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "first.png")
	writeImagesToolImage(t, input)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tool := NewImagesToPDFTool()
	item, jobErr := tool.ExecuteSingleWithProgress(ctx, imagesToolRequest([]string{input}, nil), nil)
	if jobErr == nil {
		t.Fatal("expected cancellation to fail the run")
	}
	if jobErr.Code != models.ErrorCodeCancelledByUser {
		t.Fatalf("expected CANCELLED_BY_USER, got %q", jobErr.Code)
	}
	if jobErr.DetailCode != engine.ImagesErrorCancelled {
		t.Fatalf("expected %s, got %q", engine.ImagesErrorCancelled, jobErr.DetailCode)
	}
	if jobErr.Message != "Images to PDF was canceled." {
		t.Fatalf("unexpected cancellation message: %q", jobErr.Message)
	}
	if item.Success {
		t.Fatalf("expected a failed item, got %+v", item)
	}

	outputPath := filepath.Join(dir, "first_images.pdf")
	if item.OutputPath != outputPath {
		t.Fatalf("expected the planned output path %q, got %q", outputPath, item.OutputPath)
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no output after cancellation, stat err = %v", statErr)
	}
}

func TestImagesToolExecuteSingleDecodeFailure(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(input, []byte("this is not a png"), 0o644); err != nil {
		t.Fatalf("write corrupt fixture: %v", err)
	}

	tool := NewImagesToPDFTool()
	item, jobErr := tool.ExecuteSingle(context.Background(), imagesToolRequest([]string{input}, nil))
	if jobErr == nil {
		t.Fatal("expected the corrupt image to fail")
	}
	if jobErr.Code != engine.ImagesErrorDecodeFailed || jobErr.DetailCode != engine.ImagesErrorDecodeFailed {
		t.Fatalf("expected %s, got code %q detail %q", engine.ImagesErrorDecodeFailed, jobErr.Code, jobErr.DetailCode)
	}
	if item.Success {
		t.Fatalf("expected a failed item, got %+v", item)
	}

	outputPath := filepath.Join(dir, "broken_images.pdf")
	if item.OutputPath != outputPath {
		t.Fatalf("expected the planned output path %q, got %q", outputPath, item.OutputPath)
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no output after a decode failure, stat err = %v", statErr)
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read output directory: %v", readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "broken.png" {
		t.Fatalf("expected only the input to remain, got %v", entries)
	}
}

func TestImagesToolPlanThenExecuteUsesPlannedPath(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writeImagesToolImage(t, first)
	writeImagesToolImage(t, second)

	naturalPath := filepath.Join(dir, "first_images.pdf")
	if err := os.WriteFile(naturalPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	tool := NewImagesToPDFTool()
	req := imagesToolRequest([]string{first, second}, map[string]any{
		"columns": 2,
		"rows":    1,
	})

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	plannedPath := plan.Outputs[0].OutputPath
	if want := filepath.Join(dir, "first_images-2.pdf"); plannedPath != want {
		t.Fatalf("expected the planned output %q, got %q", want, plannedPath)
	}

	item, jobErr := tool.ExecuteSingle(context.Background(), req)
	if jobErr != nil {
		t.Fatalf("expected execution to succeed, got %v", jobErr)
	}
	if !item.Success || item.OutputPath != plannedPath {
		t.Fatalf("expected the planned path %q, got %+v", plannedPath, item)
	}

	pageCount, pageErr := api.PageCountFile(plannedPath)
	if pageErr != nil || pageCount != 1 {
		t.Fatalf("expected 1 page, got %d (err=%v)", pageCount, pageErr)
	}

	content, readErr := os.ReadFile(naturalPath)
	if readErr != nil {
		t.Fatalf("read existing output: %v", readErr)
	}
	if string(content) != "existing" {
		t.Fatal("execution must not overwrite the file that forced the adjustment")
	}
}

func TestImagesToolExecutePlanHonorsResolvedPlan(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writeImagesToolImage(t, first)
	writeImagesToolImage(t, second)

	tool := NewImagesToPDFTool()
	req := imagesToolRequest([]string{first, second}, map[string]any{
		"columns": 2,
		"rows":    1,
	})

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected 1 planned output, got %d", len(plan.Outputs))
	}

	requestOutput := plan.Outputs[0].OutputPath
	plannedOutput := filepath.Join(dir, "resolved-plan.pdf")
	plan.Outputs[0].OutputPath = plannedOutput

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("expected planned render to succeed, got %v", execErr)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 result item, got %d", len(items))
	}
	item := items[0]
	if !item.Success || item.OutputPath != plannedOutput {
		t.Fatalf("expected the planned output %q, got %+v", plannedOutput, item)
	}
	if item.OutputCount != 1 || len(item.Outputs) != 1 || item.Outputs[0] != plannedOutput {
		t.Fatalf("expected outputs [%q], got %+v", plannedOutput, item.Outputs)
	}
	if _, statErr := os.Stat(plannedOutput); statErr != nil {
		t.Fatalf("expected the planned output on disk: %v", statErr)
	}
	if _, statErr := os.Stat(requestOutput); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the request-resolved output %q to be unused, got %v", requestOutput, statErr)
	}
}
