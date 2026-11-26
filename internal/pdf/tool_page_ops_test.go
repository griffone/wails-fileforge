package pdf

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
	"fileforge-desktop/internal/verify"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

func writeGeometryPagePNG(t *testing.T, path string, width, height int) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 90, A: 255})
		}
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode png: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close png: %v", err)
	}
}

// writeGeometryPDFFixture builds a PDF whose page sizes identify each page.
func writeGeometryPDFFixture(t *testing.T, path string, sizes [][2]int) {
	t.Helper()

	pngPaths := make([]string, 0, len(sizes))
	for i, size := range sizes {
		pngPath := filepath.Join(t.TempDir(), fmt.Sprintf("page-%d.png", i+1))
		writeGeometryPagePNG(t, pngPath, size[0], size[1])
		pngPaths = append(pngPaths, pngPath)
	}

	if err := api.ImportImagesFile(context.Background(), pngPaths, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}

func writeBlankPDFFixture(t *testing.T, path string, pages int) {
	t.Helper()

	var builder strings.Builder
	builder.WriteString(`{"pages":{`)
	for page := 1; page <= pages; page++ {
		if page > 1 {
			builder.WriteString(",")
		}
		fmt.Fprintf(&builder, `"%d":{"content":{}}`, page)
	}
	builder.WriteString(`}}`)

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create blank pdf: %v", err)
	}
	if err := api.Create(context.Background(), nil, strings.NewReader(builder.String()), file, model.NewDefaultConfiguration()); err != nil {
		_ = file.Close()
		t.Fatalf("create blank pdf: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close blank pdf: %v", err)
	}
}

func pageOpDims(t *testing.T, path string) []string {
	t.Helper()

	dims, err := api.PageDimsFile(context.Background(), path)
	if err != nil {
		t.Fatalf("read page dims of %s: %v", filepath.Base(path), err)
	}

	formatted := make([]string, 0, len(dims))
	for _, dim := range dims {
		formatted = append(formatted, dim.String())
	}

	return formatted
}

func pageOpRotations(t *testing.T, path string) []int {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", filepath.Base(path), err)
	}
	defer func() { _ = file.Close() }()

	info, err := api.PDFInfo(context.Background(), file, path, nil, false, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatalf("read info of %s: %v", filepath.Base(path), err)
	}

	rotations := make([]int, 0, len(info.PageBoundaries))
	for _, boundaries := range info.PageBoundaries {
		rotations = append(rotations, boundaries.Rot)
	}

	return rotations
}

func assertPageCount(t *testing.T, path string, want int) {
	t.Helper()

	count, err := api.PageCountFile(context.Background(), path)
	if err != nil {
		t.Fatalf("read page count of %s: %v", filepath.Base(path), err)
	}
	if count != want {
		t.Fatalf("%s has %d pages, want %d", filepath.Base(path), count, want)
	}
}

// assertVerified proves the app's own verification passes for the produced
// outputs: PDFs get a page-count check, images get a dimensions check.
func assertVerified(t *testing.T, paths ...string) {
	t.Helper()

	report := verify.VerifyPaths(paths)
	if !report.Success {
		t.Fatalf("verification failed: %s", report.Message)
	}
	if len(report.Outputs) != len(paths) {
		t.Fatalf("verified outputs = %d, want %d", len(report.Outputs), len(paths))
	}
}

func pageOpTestRequest(toolID, mode string, inputs []string, options map[string]any) models.JobRequest {
	return models.JobRequest{
		ToolID:     toolID,
		Mode:       mode,
		InputPaths: inputs,
		Options:    options,
	}
}

func TestRotateToolPlanCollisionSafeName(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writeGeometryPDFFixture(t, input, [][2]int{{100, 200}, {200, 300}})

	existing := filepath.Join(dir, "notes_rotated.pdf")
	if err := os.WriteFile(existing, []byte("%PDF-1.4"), 0o644); err != nil {
		t.Fatalf("create colliding output: %v", err)
	}

	plan, planErr := NewRotateTool().Plan(context.Background(), pageOpTestRequest(ToolIDPDFRotate, "single", []string{input}, map[string]any{"angle": 90}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected 1 output, got %d", len(plan.Outputs))
	}

	output := plan.Outputs[0]
	want := filepath.Join(dir, "notes_rotated-2.pdf")
	if output.OutputPath != want {
		t.Fatalf("output = %q, want %q", output.OutputPath, want)
	}
	if !output.Adjusted || len(output.Warnings) == 0 {
		t.Fatalf("expected an adjusted output with a warning, got %+v", output)
	}
	if plan.EffectiveOptions["angle"] != 90 {
		t.Fatalf("effective angle = %v, want 90", plan.EffectiveOptions["angle"])
	}
}

func TestRotateToolExecutesAllPagesAndSelection(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	writeGeometryPDFFixture(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}})

	tool := NewRotateTool()

	allPlan, planErr := tool.Plan(context.Background(), pageOpTestRequest(ToolIDPDFRotate, "single", []string{input}, map[string]any{"angle": 90}))
	if planErr != nil {
		t.Fatalf("plan all pages: %v", planErr)
	}
	allItem, execErr := tool.ExecutePlan(context.Background(), pageOpTestRequest(ToolIDPDFRotate, "single", []string{input}, map[string]any{"angle": 90}), allPlan, nil)
	if execErr != nil {
		t.Fatalf("rotate all pages: %v", execErr)
	}
	allOutput := allItem[0].OutputPath
	if rotations := pageOpRotations(t, allOutput); fmt.Sprint(rotations) != "[90 90 90]" {
		t.Fatalf("all-page rotations = %v, want [90 90 90]", rotations)
	}
	assertPageCount(t, allOutput, 3)
	assertVerified(t, allOutput)

	selectionDir := filepath.Join(dir, "selection")
	if err := os.MkdirAll(selectionDir, 0o755); err != nil {
		t.Fatalf("create selection dir: %v", err)
	}
	selectionReq := pageOpTestRequest(ToolIDPDFRotate, "single", []string{input}, map[string]any{"angle": 180, "pageSelection": "2"})
	selectionReq.OutputDir = selectionDir
	selectionPlan, planErr := tool.Plan(context.Background(), selectionReq)
	if planErr != nil {
		t.Fatalf("plan selection: %v", planErr)
	}
	selectionItems, execErr := tool.ExecutePlan(context.Background(), selectionReq, selectionPlan, nil)
	if execErr != nil {
		t.Fatalf("rotate selection: %v", execErr)
	}
	selectionOutput := selectionItems[0].OutputPath
	if rotations := pageOpRotations(t, selectionOutput); fmt.Sprint(rotations) != "[0 180 0]" {
		t.Fatalf("selection rotations = %v, want [0 180 0]", rotations)
	}
	assertPageCount(t, selectionOutput, 3)
	assertVerified(t, selectionOutput)
}

func TestRemovePagesToolValidSelectionAndRejections(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	writeGeometryPDFFixture(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}})

	tool := NewRemovePagesTool()
	req := pageOpTestRequest(ToolIDPDFRemovePages, "single", []string{input}, map[string]any{"pageSelection": "1-2"})
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("plan remove pages: %v", planErr)
	}
	if want := filepath.Join(dir, "source_pages_removed.pdf"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("output = %q, want %q", plan.Outputs[0].OutputPath, want)
	}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("remove pages: %v", execErr)
	}
	output := items[0].OutputPath
	if dims := pageOpDims(t, output); fmt.Sprint(dims) != "[300.000000x400.000000]" {
		t.Fatalf("remaining pages = %v, want the third page", dims)
	}
	assertPageCount(t, output, 1)
	assertVerified(t, output)

	rejections := []struct {
		name      string
		selection string
		wantCode  string
	}{
		{name: "every page", selection: "1-3", wantCode: engine.ErrorCodeRemovePagesAllPages},
		{name: "out of range", selection: "2-4", wantCode: engine.ErrorCodeRemovePagesSelectionBounds},
		{name: "missing", selection: "", wantCode: engine.ErrorCodeRemovePagesSelectionRequired},
	}
	for _, tt := range rejections {
		t.Run(tt.name, func(t *testing.T) {
			_, planErr := tool.Plan(context.Background(), pageOpTestRequest(ToolIDPDFRemovePages, "single", []string{input}, map[string]any{"pageSelection": tt.selection}))
			if planErr == nil {
				t.Fatal("expected the plan to be rejected")
			}
			if planErr.DetailCode != tt.wantCode {
				t.Fatalf("detail code = %q, want %q (%s)", planErr.DetailCode, tt.wantCode, planErr.Message)
			}
		})
	}
}

func TestRemovePagesToolBatchReportsPartialSuccess(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	broken := filepath.Join(dir, "broken.pdf")
	writeGeometryPDFFixture(t, first, [][2]int{{100, 200}, {200, 300}, {300, 400}})
	if err := os.WriteFile(broken, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write broken pdf: %v", err)
	}

	tool := NewRemovePagesTool()
	req := pageOpTestRequest(ToolIDPDFRemovePages, "batch", []string{first, broken}, map[string]any{"pageSelection": "1"})
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected the batch plan to survive one unreadable input, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("planned outputs = %d, want 2", len(plan.Outputs))
	}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr == nil {
		t.Fatal("expected an aggregate failure for the broken input")
	}
	if execErr.DetailCode != engine.ErrorCodeRemovePagesFailed {
		t.Fatalf("aggregate code = %q, want %q", execErr.DetailCode, engine.ErrorCodeRemovePagesFailed)
	}
	if len(items) != 2 {
		t.Fatalf("items = %d, want 2 in plan order", len(items))
	}
	if !items[0].Success || items[1].Success {
		t.Fatalf("expected only the broken input to fail, got %+v", items)
	}
	if items[1].Error == nil || items[1].Error.DetailCode != engine.ErrorCodeInvalidInputPDF {
		t.Fatalf("broken input code = %+v, want %q", items[1].Error, engine.ErrorCodeInvalidInputPDF)
	}
	if _, statErr := os.Stat(items[0].OutputPath); statErr != nil {
		t.Fatalf("expected the healthy output on disk: %v", statErr)
	}
	if _, statErr := os.Stat(items[1].OutputPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no output for the broken input, got %v", statErr)
	}
	assertPageCount(t, items[0].OutputPath, 2)
	assertVerified(t, items[0].OutputPath)
}

func TestReorderToolProducesExactPageOrder(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	writeGeometryPDFFixture(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}, {400, 500}})

	tool := NewReorderTool()
	req := pageOpTestRequest(ToolIDPDFReorder, "single", []string{input}, map[string]any{"order": "3,1,2,4"})
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("plan reorder: %v", planErr)
	}
	if want := filepath.Join(dir, "source_reordered.pdf"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("output = %q, want %q", plan.Outputs[0].OutputPath, want)
	}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("reorder: %v", execErr)
	}
	output := items[0].OutputPath
	want := []string{
		"300.000000x400.000000",
		"100.000000x200.000000",
		"200.000000x300.000000",
		"400.000000x500.000000",
	}
	if dims := pageOpDims(t, output); strings.Join(dims, "|") != strings.Join(want, "|") {
		t.Fatalf("page order = %v, want %v", dims, want)
	}
	assertPageCount(t, output, 4)
	assertVerified(t, output)
}

func TestReorderToolRejectsBadSpecs(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	writeGeometryPDFFixture(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}})

	tool := NewReorderTool()
	tests := []struct {
		name     string
		order    string
		wantCode string
	}{
		{name: "missing", order: "1,3", wantCode: engine.ErrorCodeReorderPageMissing},
		{name: "duplicate", order: "1,1,2,3", wantCode: engine.ErrorCodeReorderPageDuplicate},
		{name: "invented", order: "1,2,3,9", wantCode: engine.ErrorCodeReorderPageOutOfBounds},
		{name: "invalid token", order: "1,two,3", wantCode: engine.ErrorCodeReorderOrderInvalid},
		{name: "empty", order: "", wantCode: engine.ErrorCodeReorderOrderRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, planErr := tool.Plan(context.Background(), pageOpTestRequest(ToolIDPDFReorder, "single", []string{input}, map[string]any{"order": tt.order}))
			if planErr == nil {
				t.Fatal("expected the plan to be rejected")
			}
			if planErr.DetailCode != tt.wantCode {
				t.Fatalf("detail code = %q, want %q (%s)", planErr.DetailCode, tt.wantCode, planErr.Message)
			}
		})
	}
}

func TestExtractImagesToolWritesUniqueFiles(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "album.pdf")
	writeGeometryPDFFixture(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}})

	// Make the natural folder name busy so the plan has to suffix it.
	if err := os.MkdirAll(filepath.Join(dir, "album_images"), 0o755); err != nil {
		t.Fatalf("create colliding folder: %v", err)
	}

	tool := NewExtractImagesTool()
	req := pageOpTestRequest(ToolIDPDFExtractImages, "single", []string{input}, nil)
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("plan extract images: %v", planErr)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("planned outputs = %d, want 1", len(plan.Outputs))
	}
	outputDir := plan.Outputs[0].OutputPath
	if want := filepath.Join(dir, "album_images-2"); outputDir != want {
		t.Fatalf("output dir = %q, want %q", outputDir, want)
	}
	if plan.Outputs[0].OutputKind != "directory" {
		t.Fatalf("output kind = %q, want directory", plan.Outputs[0].OutputKind)
	}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("extract images: %v", execErr)
	}
	item := items[0]
	if !item.Success {
		t.Fatalf("expected success, got %+v", item)
	}
	if item.OutputCount != 3 {
		t.Fatalf("output count = %d, want 3", item.OutputCount)
	}
	if len(item.Outputs) != 3 {
		t.Fatalf("reported files = %d, want 3", len(item.Outputs))
	}
	if item.Details["imageCount"] != 3 {
		t.Fatalf("details imageCount = %v, want 3", item.Details["imageCount"])
	}

	seen := make(map[string]struct{}, len(item.Outputs))
	for _, file := range item.Outputs {
		if filepath.Dir(file) != outputDir {
			t.Fatalf("extracted file outside the planned folder: %s", file)
		}
		if _, duplicate := seen[file]; duplicate {
			t.Fatalf("duplicate extracted file: %s", filepath.Base(file))
		}
		seen[file] = struct{}{}
	}
	assertVerified(t, item.Outputs...)
}

func TestExtractImagesToolReportsDocumentWithoutImages(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "blank.pdf")
	writeBlankPDFFixture(t, input, 2)

	tool := NewExtractImagesTool()
	req := pageOpTestRequest(ToolIDPDFExtractImages, "single", []string{input}, nil)
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("plan extract images: %v", planErr)
	}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr == nil {
		t.Fatal("expected image extraction to fail on a document without images")
	}
	if execErr.DetailCode != engine.ErrorCodeExtractImagesFailed {
		t.Fatalf("aggregate code = %q, want %q", execErr.DetailCode, engine.ErrorCodeExtractImagesFailed)
	}
	if len(items) != 1 || items[0].Success {
		t.Fatalf("expected one failed item, got %+v", items)
	}
	if items[0].Error == nil || items[0].Error.DetailCode != engine.ErrorCodeExtractImagesNone {
		t.Fatalf("item code = %+v, want %q", items[0].Error, engine.ErrorCodeExtractImagesNone)
	}
	if !strings.Contains(items[0].Message, "no embedded images") {
		t.Fatalf("message = %q, want a clear no-images message", items[0].Message)
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); !os.IsNotExist(statErr) {
		t.Fatalf("no-images extraction must not leave a folder, got %v", statErr)
	}
}

func TestPageOpToolsCancellationLeavesNoPartialOutput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeGeometryPDFFixture(t, first, [][2]int{{100, 200}, {200, 300}, {300, 400}})
	writeGeometryPDFFixture(t, second, [][2]int{{100, 200}, {200, 300}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	tests := []struct {
		name    string
		plan    func() models.PlanJobResponse
		execute func(models.PlanJobResponse) ([]models.JobResultItem, *models.JobError)
	}{
		{
			name: "rotate",
			plan: func() models.PlanJobResponse {
				plan, _ := NewRotateTool().Plan(context.Background(), pageOpTestRequest(ToolIDPDFRotate, "batch", []string{first, second}, map[string]any{"angle": 90}))
				return plan
			},
			execute: func(plan models.PlanJobResponse) ([]models.JobResultItem, *models.JobError) {
				return NewRotateTool().ExecutePlan(ctx, pageOpTestRequest(ToolIDPDFRotate, "batch", []string{first, second}, map[string]any{"angle": 90}), plan, nil)
			},
		},
		{
			name: "extract images",
			plan: func() models.PlanJobResponse {
				plan, _ := NewExtractImagesTool().Plan(context.Background(), pageOpTestRequest(ToolIDPDFExtractImages, "batch", []string{first, second}, nil))
				return plan
			},
			execute: func(plan models.PlanJobResponse) ([]models.JobResultItem, *models.JobError) {
				return NewExtractImagesTool().ExecutePlan(ctx, pageOpTestRequest(ToolIDPDFExtractImages, "batch", []string{first, second}, nil), plan, nil)
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			plan := tt.plan()
			if len(plan.Outputs) != 2 {
				t.Fatalf("planned outputs = %d, want 2", len(plan.Outputs))
			}

			items, execErr := tt.execute(plan)
			if execErr == nil {
				t.Fatal("expected a cancellation error")
			}
			if len(items) != 0 {
				t.Fatalf("canceled batch completed items: %+v", items)
			}

			for _, output := range plan.Outputs {
				if _, statErr := os.Stat(output.OutputPath); !os.IsNotExist(statErr) {
					t.Fatalf("canceled batch left a partial output %q: %v", output.OutputPath, statErr)
				}
			}
		})
	}
}
