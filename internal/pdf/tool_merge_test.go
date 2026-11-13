package pdf

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"fileforge-desktop/internal/models"
)

func mergeTestRequest(inputPaths []string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     ToolIDPDFMergeV1,
		Mode:       "single",
		InputPaths: inputPaths,
		Options:    options,
	}
}

func TestMergeToolPlanAutoOutput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	tool := NewMergeTool()
	plan, planErr := tool.Plan(context.Background(), mergeTestRequest([]string{first, second}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if !plan.Success {
		t.Fatal("expected success plan")
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected 1 planned output, got %d", len(plan.Outputs))
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "first_merged.pdf"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.OutputKind != "file" {
		t.Fatalf("expected file output kind, got %q", output.OutputKind)
	}
	if output.Adjusted {
		t.Fatal("expected natural auto output to be unadjusted")
	}
	if output.Exists {
		t.Fatal("expected planned output to not exist yet")
	}
}

func TestMergeToolPlanUsesChosenOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	tool := NewMergeTool()
	req := mergeTestRequest([]string{first, second}, nil)
	req.OutputDir = outDir
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	want := filepath.Join(outDir, "first_merged.pdf")
	if plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
}

func TestMergeToolPlanAutoOutputSuffixesExistingFile(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)
	if err := os.WriteFile(filepath.Join(dir, "first_merged.pdf"), []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	tool := NewMergeTool()
	plan, planErr := tool.Plan(context.Background(), mergeTestRequest([]string{first, second}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "first_merged-2.pdf"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag when the natural name exists")
	}
	if len(output.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
}

func TestMergeToolPlanExplicitOutputConflict(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	outputPath := filepath.Join(dir, "merged.pdf")
	if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	tool := NewMergeTool()
	plan, planErr := tool.Plan(context.Background(), mergeTestRequest([]string{first, second}, map[string]any{"outputPath": outputPath}))
	if planErr == nil {
		t.Fatal("expected explicit output conflict")
	}
	if planErr.DetailCode != "PDF_MERGE_OUTPUT_ALREADY_EXISTS" {
		t.Fatalf("expected PDF_MERGE_OUTPUT_ALREADY_EXISTS, got %q", planErr.DetailCode)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}

	content, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("read existing output: %v", readErr)
	}
	if string(content) != "existing" {
		t.Fatal("plan must not overwrite the existing output")
	}
}

func TestMergeToolExecuteSingleAutoOutput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	tool := NewMergeTool()
	req := mergeTestRequest([]string{first, second}, nil)

	item, itemErr := tool.ExecuteSingle(context.Background(), req)
	if itemErr != nil {
		t.Fatalf("expected merge to succeed, got %v", itemErr)
	}
	if !item.Success {
		t.Fatalf("expected successful item, got %q", item.Message)
	}

	outputPath := filepath.Join(dir, "first_merged.pdf")
	if item.OutputPath != outputPath {
		t.Fatalf("expected output %q, got %q", outputPath, item.OutputPath)
	}
	if len(item.Outputs) != 1 || item.Outputs[0] != outputPath {
		t.Fatalf("expected outputs to contain %q, got %v", outputPath, item.Outputs)
	}
	if item.OutputCount != 1 {
		t.Fatalf("expected output count 1, got %d", item.OutputCount)
	}

	info, statErr := os.Stat(outputPath)
	if statErr != nil {
		t.Fatalf("expected merged output to exist: %v", statErr)
	}
	if info.Size() == 0 {
		t.Fatal("expected merged output to be non-empty")
	}

	// Running the same request again must not overwrite the first result.
	secondItem, secondErr := tool.ExecuteSingle(context.Background(), req)
	if secondErr != nil {
		t.Fatalf("expected second merge to succeed, got %v", secondErr)
	}
	if want := filepath.Join(dir, "first_merged-2.pdf"); secondItem.OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, secondItem.OutputPath)
	}
}

func writeMultiPagePDFFixture(t *testing.T, path string, pageCount int) {
	t.Helper()

	pngPaths := make([]string, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		pngPath := filepath.Join(t.TempDir(), fmt.Sprintf("page-%d.png", page+1))
		writePDFFixturePagePNG(t, pngPath)
		pngPaths = append(pngPaths, pngPath)
	}

	if err := api.ImportImagesFile(pngPaths, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}

func writePDFFixturePagePNG(t *testing.T, path string) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 90, A: 255})
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

func TestMergeToolPlanIncludesPageRanges(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeMultiPagePDFFixture(t, first, 3)
	writeMultiPagePDFFixture(t, second, 2)

	tool := NewMergeTool()
	plan, planErr := tool.Plan(context.Background(), mergeTestRequest([]string{first, second}, map[string]any{
		"inputPageRanges": []any{"2", "1-2"},
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

	ranges, ok := plan.EffectiveOptions["inputPageRanges"].([]string)
	if !ok {
		t.Fatalf("expected effective inputPageRanges, got %#v", plan.EffectiveOptions)
	}
	if len(ranges) != 2 || ranges[0] != "2" || ranges[1] != "1-2" {
		t.Fatalf("expected ranges [2 1-2], got %v", ranges)
	}
}

func TestMergeToolPlanAcceptsStringSlicePageRanges(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeMultiPagePDFFixture(t, first, 2)
	writeMultiPagePDFFixture(t, second, 2)

	tool := NewMergeTool()
	plan, planErr := tool.Plan(context.Background(), mergeTestRequest([]string{first, second}, map[string]any{
		"inputPageRanges": []string{"1", "2"},
	}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if !plan.Success {
		t.Fatal("expected a successful plan")
	}
}

func TestMergeToolPlanRejectsMismatchedPageRangeCount(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeMultiPagePDFFixture(t, first, 2)
	writeMultiPagePDFFixture(t, second, 2)

	tool := NewMergeTool()
	plan, planErr := tool.Plan(context.Background(), mergeTestRequest([]string{first, second}, map[string]any{
		"inputPageRanges": []any{"1"},
	}))
	if planErr == nil {
		t.Fatal("expected a page range count mismatch")
	}
	if planErr.DetailCode != "PDF_MERGE_PAGE_RANGE_INVALID" {
		t.Fatalf("expected PDF_MERGE_PAGE_RANGE_INVALID, got %q", planErr.DetailCode)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
}

func TestMergeToolPlanRejectsOutOfBoundsPageRange(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeMultiPagePDFFixture(t, first, 2)
	writeMultiPagePDFFixture(t, second, 2)

	tool := NewMergeTool()
	_, planErr := tool.Plan(context.Background(), mergeTestRequest([]string{first, second}, map[string]any{
		"inputPageRanges": []any{"9", ""},
	}))
	if planErr == nil {
		t.Fatal("expected an out-of-bounds page range")
	}
	if planErr.DetailCode != "PDF_MERGE_PAGE_RANGE_INVALID" {
		t.Fatalf("expected PDF_MERGE_PAGE_RANGE_INVALID, got %q", planErr.DetailCode)
	}
}

func TestMergeToolExecuteSingleWithPageRanges(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeMultiPagePDFFixture(t, first, 3)
	writeMultiPagePDFFixture(t, second, 2)

	tool := NewMergeTool()
	req := mergeTestRequest([]string{first, second}, map[string]any{
		"inputPageRanges": []any{"2", "1-2"},
	})

	item, itemErr := tool.ExecuteSingle(context.Background(), req)
	if itemErr != nil {
		t.Fatalf("expected merge to succeed, got %v", itemErr)
	}
	if !item.Success {
		t.Fatalf("expected successful item, got %q", item.Message)
	}

	pageCount, err := api.PageCountFile(item.OutputPath)
	if err != nil {
		t.Fatalf("read merged page count: %v", err)
	}
	if pageCount != 3 {
		t.Fatalf("expected 3 merged pages (1 + 2), got %d", pageCount)
	}
}

func TestMergeToolExecuteSingleInvalidPageRangeFails(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeMultiPagePDFFixture(t, first, 2)
	writeMultiPagePDFFixture(t, second, 2)

	tool := NewMergeTool()
	req := mergeTestRequest([]string{first, second}, map[string]any{
		"inputPageRanges": []any{"1-9", ""},
	})

	item, itemErr := tool.ExecuteSingle(context.Background(), req)
	if itemErr == nil {
		t.Fatal("expected an out-of-bounds page range to fail")
	}
	if itemErr.DetailCode != "PDF_MERGE_PAGE_RANGE_INVALID" {
		t.Fatalf("expected PDF_MERGE_PAGE_RANGE_INVALID, got %q", itemErr.DetailCode)
	}
	if item.Success {
		t.Fatal("expected failed item")
	}
	if _, statErr := os.Stat(item.OutputPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no output file, got stat error %v", statErr)
	}
}
