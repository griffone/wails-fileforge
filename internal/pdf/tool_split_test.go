package pdf

import (
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"

	"fileforge-desktop/internal/models"
)

func writePDFFixture(t *testing.T, path string) {
	t.Helper()

	pngPath := filepath.Join(t.TempDir(), "page.png")
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 90, A: 255})
		}
	}

	file, err := os.Create(pngPath)
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

	if err := api.ImportImagesFile([]string{pngPath}, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}

func writeBookmarkedPDFFixture(t *testing.T, path string, pages int, bookmarks []pdfcpu.Bookmark) {
	t.Helper()

	plain := filepath.Join(t.TempDir(), "plain.pdf")
	writeMultiPagePDFFixture(t, plain, pages)

	if err := api.AddBookmarksFile(plain, path, bookmarks, true, nil); err != nil {
		t.Fatalf("add bookmarks: %v", err)
	}
}

func splitTestRequest(mode string, inputPaths []string, options map[string]any) models.JobRequest {
	return models.JobRequest{
		ToolID:     ToolIDPDFSplit,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

func TestSplitToolPlanAutoDirectoryNextToInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewSplitTool()
	plan, planErr := tool.Plan(context.Background(), splitTestRequest("single", []string{input}, nil))
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
	want := filepath.Join(dir, "notes_pages")
	if output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.OutputKind != "directory" {
		t.Fatalf("expected directory output kind, got %q", output.OutputKind)
	}
	if output.OutputCount != 1 {
		t.Fatalf("expected output count 1, got %d", output.OutputCount)
	}
	if output.Adjusted {
		t.Fatal("expected a natural directory to be unadjusted")
	}
	if output.Exists {
		t.Fatal("expected the planned directory to not exist yet")
	}

	if _, statErr := os.Stat(want); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("plan must not create the output directory, got %v", statErr)
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read input dir: %v", readErr)
	}
	if len(entries) != 1 || entries[0].Name() != "notes.pdf" {
		t.Fatalf("plan must not write files, found %v", entryNames(entries))
	}
}

func TestSplitToolPlanBatchPlansDirectoryPerInput(t *testing.T) {
	dir := t.TempDir()
	firstDir := filepath.Join(dir, "first-dir")
	secondDir := filepath.Join(dir, "second-dir")
	for _, inputDir := range []string{firstDir, secondDir} {
		if err := os.MkdirAll(inputDir, 0o755); err != nil {
			t.Fatalf("create input dir: %v", err)
		}
	}

	first := filepath.Join(firstDir, "report.pdf")
	second := filepath.Join(secondDir, "report.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	tool := NewSplitTool()
	plan, planErr := tool.Plan(context.Background(), splitTestRequest("batch", []string{first, second}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	if want := filepath.Join(firstDir, "report_pages"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(secondDir, "report_pages"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
	for _, output := range plan.Outputs {
		if output.OutputKind != "directory" {
			t.Fatalf("expected directory output kind, got %q", output.OutputKind)
		}
		if output.OutputCount != 1 {
			t.Fatalf("expected output count 1, got %d", output.OutputCount)
		}
		if output.Adjusted {
			t.Fatal("expected natural directories to be unadjusted")
		}
	}
}

func TestSplitToolPlanUsesChosenOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewSplitTool()
	req := splitTestRequest("single", []string{input}, nil)
	req.OutputDir = outDir

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	want := filepath.Join(outDir, "notes_pages")
	if plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if _, statErr := os.Stat(want); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("plan must not create the output directory, got %v", statErr)
	}
}

func TestSplitToolPlanUsesLegacyOptionsOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewSplitTool()
	plan, planErr := tool.Plan(context.Background(), splitTestRequest("single", []string{input}, map[string]any{
		"outputDir": outDir,
	}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	want := filepath.Join(outDir, "notes_pages")
	if plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
}

func TestSplitToolPlanSuffixesExistingDirectory(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	existing := filepath.Join(dir, "notes_pages")
	if err := os.MkdirAll(existing, 0o755); err != nil {
		t.Fatalf("create existing output dir: %v", err)
	}

	tool := NewSplitTool()
	plan, planErr := tool.Plan(context.Background(), splitTestRequest("single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "notes_pages-2"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag when the natural directory exists")
	}
	if len(output.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
	if len(plan.Warnings) == 0 {
		t.Fatal("expected the plan to surface the output warning")
	}
}

func TestSplitToolPlanRangesStrategy(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewSplitTool()
	plan, planErr := tool.Plan(context.Background(), splitTestRequest("single", []string{input}, map[string]any{
		"strategy": "ranges",
		"ranges":   "1",
	}))
	if planErr != nil {
		t.Fatalf("expected ranges plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected 1 planned output, got %d", len(plan.Outputs))
	}
	if plan.Outputs[0].OutputCount != 1 {
		t.Fatalf("expected output count 1, got %d", plan.Outputs[0].OutputCount)
	}

	_, invalidErr := tool.Plan(context.Background(), splitTestRequest("single", []string{input}, map[string]any{
		"strategy": "ranges",
		"ranges":   "3-1",
	}))
	if invalidErr == nil {
		t.Fatal("expected invalid ranges to fail the plan")
	}
	if invalidErr.DetailCode != "PDF_SPLIT_RANGES_INVALID" {
		t.Fatalf("expected PDF_SPLIT_RANGES_INVALID, got %q", invalidErr.DetailCode)
	}
}

func TestSplitToolExecuteSingleUsesPlannedDirectory(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewSplitTool()
	req := splitTestRequest("single", []string{input}, nil)

	item, itemErr := tool.ExecuteSingle(context.Background(), req)
	if itemErr != nil {
		t.Fatalf("expected split to succeed, got %v", itemErr)
	}
	if !item.Success {
		t.Fatalf("expected successful item, got %q", item.Message)
	}

	plannedDir := filepath.Join(dir, "notes_pages")
	if item.OutputPath != plannedDir {
		t.Fatalf("expected output directory %q, got %q", plannedDir, item.OutputPath)
	}
	if item.OutputCount != 1 || len(item.Outputs) != 1 {
		t.Fatalf("expected 1 output file, got count=%d outputs=%v", item.OutputCount, item.Outputs)
	}
	if _, statErr := os.Stat(item.Outputs[0]); statErr != nil {
		t.Fatalf("expected split output on disk: %v", statErr)
	}

	// A second run must use a new planned directory instead of overwriting.
	secondItem, secondErr := tool.ExecuteSingle(context.Background(), req)
	if secondErr != nil {
		t.Fatalf("expected second split to succeed, got %v", secondErr)
	}
	if want := filepath.Join(dir, "notes_pages-2"); secondItem.OutputPath != want {
		t.Fatalf("expected second output directory %q, got %q", want, secondItem.OutputPath)
	}
}

func TestSplitToolExecuteBatchPlansNextToEachInput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	tool := NewSplitTool()
	items, jobErr := tool.ExecuteBatch(context.Background(), splitTestRequest("batch", []string{first, second}, nil), nil)
	if jobErr != nil {
		t.Fatalf("expected batch to succeed, got %v", jobErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	wantDirs := []string{
		filepath.Join(dir, "first_pages"),
		filepath.Join(dir, "second_pages"),
	}
	for i, item := range items {
		if !item.Success || item.Error != nil {
			t.Fatalf("expected item %d to succeed, got %+v", i, item)
		}
		if item.OutputPath != wantDirs[i] {
			t.Fatalf("expected item %d output %q, got %q", i, wantDirs[i], item.OutputPath)
		}
		if item.OutputCount != 1 || len(item.Outputs) != 1 {
			t.Fatalf("expected item %d to expose 1 output, got %+v", i, item)
		}
		if _, statErr := os.Stat(item.Outputs[0]); statErr != nil {
			t.Fatalf("expected item %d output on disk: %v", i, statErr)
		}
	}
}

func TestSplitToolExecuteBatchReportsPartialSuccess(t *testing.T) {
	dir := t.TempDir()
	inputDir := filepath.Join(dir, "in")
	outputDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatalf("create input dir: %v", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	first := filepath.Join(inputDir, "first.pdf")
	second := filepath.Join(inputDir, "second.pdf")
	broken := filepath.Join(inputDir, "broken.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)
	if err := os.WriteFile(broken, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write invalid pdf: %v", err)
	}

	req := splitTestRequest("batch", []string{first, broken, second}, map[string]any{
		"outputDir": outputDir,
		"strategy":  "every_page",
	})
	req.OutputDir = outputDir

	tool := NewSplitTool()
	items, jobErr := tool.ExecuteBatch(context.Background(), req, nil)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if jobErr == nil {
		t.Fatal("expected aggregate batch error")
	}
	if jobErr.Code != models.CanonicalErrorCode("PDF_SPLIT_FAILED") {
		t.Fatalf("expected canonical split failure code, got %q", jobErr.Code)
	}
	if jobErr.DetailCode != "PDF_SPLIT_FAILED" {
		t.Fatalf("expected detail code PDF_SPLIT_FAILED, got %q", jobErr.DetailCode)
	}

	fileErrors, ok := jobErr.Details["fileErrors"].([]map[string]any)
	if !ok {
		t.Fatalf("expected fileErrors slice in details, got %T", jobErr.Details["fileErrors"])
	}
	if len(fileErrors) != 1 {
		t.Fatalf("expected 1 file error, got %d", len(fileErrors))
	}
	if fileErrors[0]["path"] != broken {
		t.Fatalf("expected file error path %q, got %v", broken, fileErrors[0]["path"])
	}
	if fileErrors[0]["code"] != "PDF_INVALID_INPUT" {
		t.Fatalf("expected file error code PDF_INVALID_INPUT, got %v", fileErrors[0]["code"])
	}

	if !items[0].Success || items[0].Error != nil {
		t.Fatalf("expected first item to succeed, got %+v", items[0])
	}
	if want := filepath.Join(outputDir, "first_pages"); items[0].OutputPath != want {
		t.Fatalf("expected first item directory %q, got %q", want, items[0].OutputPath)
	}
	if len(items[0].Outputs) != 1 || items[0].OutputCount != 1 {
		t.Fatalf("expected first item to expose its output, got %+v", items[0])
	}
	if _, statErr := os.Stat(items[0].Outputs[0]); statErr != nil {
		t.Fatalf("expected first item output on disk: %v", statErr)
	}

	if items[1].Success || items[1].Error == nil {
		t.Fatalf("expected second item to fail, got %+v", items[1])
	}
	if want := filepath.Join(outputDir, "broken_pages"); items[1].OutputPath != want {
		t.Fatalf("expected failed item directory %q, got %q", want, items[1].OutputPath)
	}

	if !items[2].Success || items[2].Error != nil {
		t.Fatalf("expected third item to succeed, got %+v", items[2])
	}
	if want := filepath.Join(outputDir, "second_pages"); items[2].OutputPath != want {
		t.Fatalf("expected third item directory %q, got %q", want, items[2].OutputPath)
	}
	if len(items[2].Outputs) != 1 {
		t.Fatalf("expected third item to expose its output, got %+v", items[2])
	}
}

func TestSplitToolPlanPagesPerFile(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "manual.pdf")
	second := filepath.Join(dir, "notes.pdf")
	writeMultiPagePDFFixture(t, first, 11)
	writeMultiPagePDFFixture(t, second, 3)

	tool := NewSplitTool()
	req := splitTestRequest("batch", []string{first, second}, map[string]any{
		"strategy":     "pages_per_file",
		"pagesPerFile": 5,
	})

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}
	if want := filepath.Join(dir, "manual_pages"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if plan.Outputs[0].OutputCount != 3 {
		t.Fatalf("expected 3 chunks for 11 pages, got %d", plan.Outputs[0].OutputCount)
	}
	if plan.Outputs[1].OutputCount != 1 {
		t.Fatalf("expected 1 chunk for 3 pages, got %d", plan.Outputs[1].OutputCount)
	}
	if got := plan.EffectiveOptions["pagesPerFile"]; got != 5 {
		t.Fatalf("expected effective pagesPerFile 5, got %v", got)
	}

	single, singleErr := tool.Plan(context.Background(), splitTestRequest("single", []string{first}, map[string]any{
		"strategy":     "pages_per_file",
		"pagesPerFile": 5,
	}))
	if singleErr != nil {
		t.Fatalf("expected single plan to succeed, got %v", singleErr)
	}

	item, itemErr := tool.ExecuteSingle(context.Background(), splitTestRequest("single", []string{first}, map[string]any{
		"strategy":     "pages_per_file",
		"pagesPerFile": 5,
	}))
	if itemErr != nil {
		t.Fatalf("expected split to succeed, got %v", itemErr)
	}
	if item.OutputPath != single.Outputs[0].OutputPath {
		t.Fatalf("expected execution to use the planned directory %q, got %q", single.Outputs[0].OutputPath, item.OutputPath)
	}
	if item.OutputCount != 3 || len(item.Outputs) != 3 {
		t.Fatalf("expected 3 output files, got count=%d outputs=%v", item.OutputCount, item.Outputs)
	}

	wantNames := []string{
		"manual_part_001_p1-5.pdf",
		"manual_part_002_p6-10.pdf",
		"manual_part_003_p11.pdf",
	}
	for i, output := range item.Outputs {
		if filepath.Base(output) != wantNames[i] {
			t.Fatalf("expected output %d to be %q, got %q", i, wantNames[i], output)
		}
		if _, statErr := os.Stat(output); statErr != nil {
			t.Fatalf("expected output %d on disk: %v", i, statErr)
		}
	}
}

func TestSplitToolPagesPerFileRejectsInvalidValues(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "manual.pdf")
	writeMultiPagePDFFixture(t, input, 4)

	tool := NewSplitTool()
	for _, pagesPerFile := range []any{0, 1001, "five", 2.5} {
		_, planErr := tool.Plan(context.Background(), splitTestRequest("single", []string{input}, map[string]any{
			"strategy":     "pages_per_file",
			"pagesPerFile": pagesPerFile,
		}))
		if planErr == nil {
			t.Fatalf("expected pagesPerFile=%v to fail", pagesPerFile)
		}
		if planErr.DetailCode != "PDF_SPLIT_PAGES_PER_FILE_INVALID" {
			t.Fatalf("expected PDF_SPLIT_PAGES_PER_FILE_INVALID for %v, got %q", pagesPerFile, planErr.DetailCode)
		}
	}

	_, missingErr := tool.Plan(context.Background(), splitTestRequest("single", []string{input}, map[string]any{
		"strategy": "pages_per_file",
	}))
	if missingErr == nil || missingErr.DetailCode != "PDF_SPLIT_PAGES_PER_FILE_INVALID" {
		t.Fatalf("expected missing pagesPerFile to fail with PDF_SPLIT_PAGES_PER_FILE_INVALID, got %v", missingErr)
	}
}

func TestSplitToolBookmarksPlanAndExecute(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "guide.pdf")
	writeBookmarkedPDFFixture(t, input, 11, []pdfcpu.Bookmark{
		{Title: "Chapter One", PageFrom: 1},
		{Title: "Chapter Two", PageFrom: 6},
		{Title: "Appendix", PageFrom: 9},
	})

	tool := NewSplitTool()
	req := splitTestRequest("single", []string{input}, map[string]any{
		"strategy": "bookmarks",
	})

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected bookmarks plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected 1 planned directory, got %d", len(plan.Outputs))
	}
	if want := filepath.Join(dir, "guide_pages"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected output directory %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if plan.Outputs[0].OutputCount != 3 {
		t.Fatalf("expected 3 bookmark outputs, got %d", plan.Outputs[0].OutputCount)
	}

	item, itemErr := tool.ExecuteSingle(context.Background(), req)
	if itemErr != nil {
		t.Fatalf("expected bookmarks split to succeed, got %v", itemErr)
	}
	if item.OutputCount != 3 || len(item.Outputs) != 3 {
		t.Fatalf("expected 3 output files, got count=%d outputs=%v", item.OutputCount, item.Outputs)
	}

	wantNames := []string{
		"guide_bookmark_001_chapter-one.pdf",
		"guide_bookmark_002_chapter-two.pdf",
		"guide_bookmark_003_appendix.pdf",
	}
	for i, output := range item.Outputs {
		if filepath.Base(output) != wantNames[i] {
			t.Fatalf("expected output %d to be %q, got %q", i, wantNames[i], output)
		}
		if _, statErr := os.Stat(output); statErr != nil {
			t.Fatalf("expected output %d on disk: %v", i, statErr)
		}
	}
}

func TestSplitToolBookmarksWithoutOutline(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "plain.pdf")
	writeMultiPagePDFFixture(t, input, 3)

	tool := NewSplitTool()
	_, planErr := tool.Plan(context.Background(), splitTestRequest("single", []string{input}, map[string]any{
		"strategy": "bookmarks",
	}))
	if planErr == nil {
		t.Fatal("expected bookmarks plan without an outline to fail")
	}
	if planErr.DetailCode != "PDF_SPLIT_NO_BOOKMARKS" {
		t.Fatalf("expected PDF_SPLIT_NO_BOOKMARKS, got %q", planErr.DetailCode)
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}

func TestSplitToolExecutePlanHonorsResolvedPlan(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewSplitTool()
	req := splitTestRequest("single", []string{input}, nil)

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected 1 planned output, got %d", len(plan.Outputs))
	}

	// The request resolves to <stem>_pages. Mutate the resolved plan so the test
	// proves ExecutePlan ignores the request and uses the planned directory.
	requestOutput := plan.Outputs[0].OutputPath
	plannedOutput := filepath.Join(dir, "resolved-plan-pages")
	plan.Outputs[0].OutputPath = plannedOutput

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr != nil {
		t.Fatalf("expected planned split to succeed, got %v", execErr)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 result item, got %d", len(items))
	}

	item := items[0]
	if !item.Success || item.OutputPath != plannedOutput {
		t.Fatalf("expected the planned output %q, got %+v", plannedOutput, item)
	}
	if _, statErr := os.Stat(plannedOutput); statErr != nil {
		t.Fatalf("expected the planned directory on disk: %v", statErr)
	}
	if _, statErr := os.Stat(requestOutput); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the request-resolved directory %q to be unused, got %v", requestOutput, statErr)
	}
}

func TestSplitToolExecutePlanBatchSubset(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	tool := NewSplitTool()
	req := splitTestRequest("batch", []string{first, second}, nil)

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	subset := plan
	subset.Outputs = plan.Outputs[1:]

	items, execErr := tool.ExecutePlan(context.Background(), req, subset, nil)
	if execErr != nil {
		t.Fatalf("expected the subset to succeed, got %v", execErr)
	}
	if len(items) != 1 || items[0].InputPath != second || items[0].OutputPath != plan.Outputs[1].OutputPath {
		t.Fatalf("expected only the second planned output, got %+v", items)
	}
	if _, statErr := os.Stat(plan.Outputs[1].OutputPath); statErr != nil {
		t.Fatalf("expected the subset directory on disk: %v", statErr)
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the excluded directory %q to stay untouched, got %v", plan.Outputs[0].OutputPath, statErr)
	}
}

func TestSplitToolExecutePlanPreservesOrderAndItemFailures(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	broken := filepath.Join(dir, "broken.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)
	if err := os.WriteFile(broken, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write invalid pdf: %v", err)
	}

	tool := NewSplitTool()
	req := splitTestRequest("batch", []string{first, broken, second}, nil)

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr == nil {
		t.Fatal("expected an aggregate split failure")
	}
	if execErr.DetailCode != "PDF_SPLIT_FAILED" {
		t.Fatalf("expected PDF_SPLIT_FAILED, got %q", execErr.DetailCode)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items in plan order, got %d", len(items))
	}
	for index, wantInput := range []string{first, broken, second} {
		if items[index].InputPath != wantInput {
			t.Fatalf("expected item %d input %q, got %q", index, wantInput, items[index].InputPath)
		}
	}
	if !items[0].Success || items[1].Success || !items[2].Success {
		t.Fatalf("expected only the broken input to fail, got %+v", items)
	}
	if _, statErr := os.Stat(items[2].OutputPath); statErr != nil {
		t.Fatalf("expected the parallel batch to finish the last item: %v", statErr)
	}
}
