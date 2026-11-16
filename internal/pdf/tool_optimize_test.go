package pdf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"

	"fileforge-desktop/internal/models"
)

func optimizeTestRequest(mode string, inputPaths []string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     ToolIDPDFOptimizeV1,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

func readPDFInfo(t *testing.T, path string) *pdfcpu.PDFInfo {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open pdf: %v", err)
	}
	defer file.Close()

	info, err := api.PDFInfo(file, filepath.Base(path), nil, false, nil)
	if err != nil {
		t.Fatalf("read pdf info: %v", err)
	}

	return info
}

func TestOptimizeToolManifest(t *testing.T) {
	manifest := NewOptimizeTool().Manifest()
	if manifest.ToolID != "tool.pdf.optimize" {
		t.Fatalf("expected tool.pdf.optimize, got %q", manifest.ToolID)
	}
	if !manifest.SupportsSingle || !manifest.SupportsBatch {
		t.Fatal("expected optimize to support single and batch")
	}
}

func TestOptimizeToolPlanAutoOutputNextToInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewOptimizeTool()
	plan, planErr := tool.Plan(context.Background(), optimizeTestRequest("single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if !plan.Success || len(plan.Outputs) != 1 {
		t.Fatalf("expected one planned output, got %+v", plan)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "notes_optimized.pdf"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.OutputKind != "file" || output.OutputCount != 1 {
		t.Fatalf("expected one file output, got %+v", output)
	}
	if output.Adjusted {
		t.Fatal("expected a natural output name to be unadjusted")
	}
	if plan.EffectiveOptions["compress"] != true {
		t.Fatalf("expected compress to default to true, got %v", plan.EffectiveOptions["compress"])
	}
	if _, exists := plan.EffectiveOptions["metadata"]; exists {
		t.Fatalf("expected no effective metadata, got %v", plan.EffectiveOptions["metadata"])
	}
	if _, statErr := os.Stat(output.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("plan must not create the output file, got %v", statErr)
	}
}

func TestOptimizeToolPlanUsesChosenOutputDirAndAvoidsCollisions(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	if err := os.WriteFile(filepath.Join(outDir, "notes_optimized.pdf"), []byte("taken"), 0o644); err != nil {
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
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	req := optimizeTestRequest("batch", []string{first, second}, map[string]any{
		"metadata": map[string]any{"title": "Quarterly report"},
	})
	req.OutputDir = outDir

	tool := NewOptimizeTool()
	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	wantPaths := []string{
		filepath.Join(outDir, "notes_optimized-2.pdf"),
		filepath.Join(outDir, "notes_optimized-3.pdf"),
	}
	for i, output := range plan.Outputs {
		if output.OutputPath != wantPaths[i] {
			t.Fatalf("expected output %d %q, got %q", i, wantPaths[i], output.OutputPath)
		}
		if !output.Adjusted {
			t.Fatalf("expected output %d to be marked adjusted", i)
		}
	}

	metadata, ok := plan.EffectiveOptions["metadata"].(map[string]string)
	if !ok {
		t.Fatalf("expected effective metadata map, got %T", plan.EffectiveOptions["metadata"])
	}
	if metadata["title"] != "Quarterly report" {
		t.Fatalf("expected effective title, got %v", metadata)
	}
}

func TestOptimizeToolPlanExplicitOutputPath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewOptimizeTool()
	outputPath := filepath.Join(dir, "custom.pdf")
	plan, planErr := tool.Plan(context.Background(), optimizeTestRequest("single", []string{input}, map[string]any{
		"outputPath": outputPath,
	}))
	if planErr != nil {
		t.Fatalf("expected explicit output plan to succeed, got %v", planErr)
	}
	if plan.Outputs[0].OutputPath != outputPath {
		t.Fatalf("expected explicit output %q, got %q", outputPath, plan.Outputs[0].OutputPath)
	}

	if err := os.WriteFile(outputPath, []byte("taken"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}
	_, conflictErr := tool.Plan(context.Background(), optimizeTestRequest("single", []string{input}, map[string]any{
		"outputPath": outputPath,
	}))
	if conflictErr == nil {
		t.Fatal("expected an existing explicit output to fail planning")
	}
	if conflictErr.DetailCode != "PDF_OPTIMIZE_OUTPUT_ALREADY_EXISTS" {
		t.Fatalf("expected PDF_OPTIMIZE_OUTPUT_ALREADY_EXISTS, got %q", conflictErr.DetailCode)
	}

	_, extensionErr := tool.Plan(context.Background(), optimizeTestRequest("single", []string{input}, map[string]any{
		"outputPath": filepath.Join(dir, "custom.txt"),
	}))
	if extensionErr == nil {
		t.Fatal("expected a non-pdf explicit output to fail planning")
	}

	_, batchErr := tool.Plan(context.Background(), optimizeTestRequest("batch", []string{input, input}, map[string]any{
		"outputPath": filepath.Join(dir, "custom.pdf"),
	}))
	if batchErr == nil {
		t.Fatal("expected options.outputPath to be rejected for batch")
	}
}

func TestOptimizeToolPlanMetadataValidation(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewOptimizeTool()
	cases := []map[string]any{
		{"metadata": "not-an-object"},
		{"metadata": map[string]any{"title": 42}},
		{"metadata": map[string]any{"title": strings.Repeat("a", 513)}},
	}

	for _, options := range cases {
		_, planErr := tool.Plan(context.Background(), optimizeTestRequest("single", []string{input}, options))
		if planErr == nil {
			t.Fatalf("expected metadata %v to fail", options["metadata"])
		}
		if planErr.DetailCode != "PDF_OPTIMIZE_METADATA_INVALID" {
			t.Fatalf("expected PDF_OPTIMIZE_METADATA_INVALID, got %q", planErr.DetailCode)
		}
	}

	// Values are trimmed and empty values are ignored.
	plan, planErr := tool.Plan(context.Background(), optimizeTestRequest("single", []string{input}, map[string]any{
		"metadata": map[string]any{
			"title":  "  Trimmed title  ",
			"author": "   ",
		},
	}))
	if planErr != nil {
		t.Fatalf("expected metadata plan to succeed, got %v", planErr)
	}
	metadata, ok := plan.EffectiveOptions["metadata"].(map[string]string)
	if !ok {
		t.Fatalf("expected effective metadata map, got %T", plan.EffectiveOptions["metadata"])
	}
	if metadata["title"] != "Trimmed title" {
		t.Fatalf("expected trimmed title, got %q", metadata["title"])
	}
	if _, exists := metadata["author"]; exists {
		t.Fatalf("expected empty author to be omitted, got %v", metadata)
	}
}

func TestOptimizeToolExecuteSingleCompresses(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewOptimizeTool()
	item, itemErr := tool.ExecuteSingle(context.Background(), optimizeTestRequest("single", []string{input}, nil))
	if itemErr != nil {
		t.Fatalf("expected optimize to succeed, got %v", itemErr)
	}
	if !item.Success || item.OutputCount != 1 || len(item.Outputs) != 1 {
		t.Fatalf("expected one successful output, got %+v", item)
	}

	output := item.Outputs[0]
	if want := filepath.Join(dir, "notes_optimized.pdf"); output != want {
		t.Fatalf("expected output %q, got %q", want, output)
	}
	if err := api.ValidateFile(output, nil); err != nil {
		t.Fatalf("expected a valid output PDF: %v", err)
	}
	count, countErr := api.PageCountFile(output)
	if countErr != nil || count != 1 {
		t.Fatalf("expected 1 page, got count=%d err=%v", count, countErr)
	}
}

func TestOptimizeToolExecuteAppliesMetadata(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewOptimizeTool()
	item, itemErr := tool.ExecuteSingle(context.Background(), optimizeTestRequest("single", []string{input}, map[string]any{
		"compress": true,
		"metadata": map[string]any{
			"title":    "Field notes",
			"author":   "Ada Lovelace",
			"subject":  "Testing",
			"keywords": "pdf,optimize",
			"creator":  "FileForge",
		},
	}))
	if itemErr != nil {
		t.Fatalf("expected optimize to succeed, got %v", itemErr)
	}

	properties := readPDFInfo(t, item.Outputs[0])
	want := map[string]string{
		"Title":   "Field notes",
		"Author":  "Ada Lovelace",
		"Subject": "Testing",
		"Creator": "FileForge",
	}
	got := map[string]string{
		"Title":   properties.Title,
		"Author":  properties.Author,
		"Subject": properties.Subject,
		"Creator": properties.Creator,
	}
	for key, value := range want {
		if got[key] != value {
			t.Fatalf("expected property %s=%q, got %q (info: %+v)", key, value, got[key], properties)
		}
	}
	keywords := make(map[string]struct{}, len(properties.Keywords))
	for _, keyword := range properties.Keywords {
		keywords[keyword] = struct{}{}
	}
	if _, ok := keywords["pdf"]; !ok {
		t.Fatalf("expected keyword pdf, got %v", properties.Keywords)
	}
	if _, ok := keywords["optimize"]; !ok {
		t.Fatalf("expected keyword optimize, got %v", properties.Keywords)
	}
}

func TestOptimizeToolExecuteWithoutCompressionCopiesInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	inputBytes, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("read input: %v", err)
	}

	tool := NewOptimizeTool()
	item, itemErr := tool.ExecuteSingle(context.Background(), optimizeTestRequest("single", []string{input}, map[string]any{
		"compress": false,
	}))
	if itemErr != nil {
		t.Fatalf("expected copy to succeed, got %v", itemErr)
	}

	outputBytes, readErr := os.ReadFile(item.Outputs[0])
	if readErr != nil {
		t.Fatalf("read output: %v", readErr)
	}
	if string(outputBytes) != string(inputBytes) {
		t.Fatal("expected compress=false without metadata to copy the input byte-for-byte")
	}
}

func TestOptimizeToolExecuteLeavesNoOutputOnFailure(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "broken.pdf")
	if err := os.WriteFile(input, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write invalid pdf: %v", err)
	}

	tool := NewOptimizeTool()
	item, itemErr := tool.ExecuteSingle(context.Background(), optimizeTestRequest("single", []string{input}, nil))
	if itemErr == nil {
		t.Fatal("expected an invalid PDF to fail execution")
	}
	if itemErr.DetailCode != "PDF_OPTIMIZE_FAILED" {
		t.Fatalf("expected PDF_OPTIMIZE_FAILED, got %q", itemErr.DetailCode)
	}
	if item.OutputPath == "" {
		t.Fatal("expected the failed item to report the planned output")
	}
	if _, statErr := os.Stat(item.OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no output after failure, got %v", statErr)
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read dir: %v", readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fileforge-optimize-") {
			t.Fatalf("expected temporary files to be cleaned up, found %s", entry.Name())
		}
	}
}

func TestOptimizeToolExecuteBatchReportsPartialSuccess(t *testing.T) {
	dir := t.TempDir()
	inputDir := filepath.Join(dir, "in")
	outputDir := filepath.Join(dir, "out")
	for _, target := range []string{inputDir, outputDir} {
		if err := os.MkdirAll(target, 0o755); err != nil {
			t.Fatalf("create dir: %v", err)
		}
	}

	first := filepath.Join(inputDir, "first.pdf")
	second := filepath.Join(inputDir, "second.pdf")
	broken := filepath.Join(inputDir, "broken.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)
	if err := os.WriteFile(broken, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write invalid pdf: %v", err)
	}

	req := optimizeTestRequest("batch", []string{first, broken, second}, nil)
	req.OutputDir = outputDir

	tool := NewOptimizeTool()
	items, jobErr := tool.ExecuteBatch(context.Background(), req, nil)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if jobErr == nil {
		t.Fatal("expected an aggregate batch error")
	}
	if jobErr.Code != models.CanonicalErrorCode("PDF_OPTIMIZE_FAILED") {
		t.Fatalf("expected canonical PDF_OPTIMIZE_FAILED, got %q", jobErr.Code)
	}
	if jobErr.DetailCode != "PDF_OPTIMIZE_FAILED" {
		t.Fatalf("expected detail code PDF_OPTIMIZE_FAILED, got %q", jobErr.DetailCode)
	}

	fileErrors, ok := jobErr.Details["fileErrors"].([]map[string]any)
	if !ok {
		t.Fatalf("expected fileErrors in details, got %T", jobErr.Details["fileErrors"])
	}
	if len(fileErrors) != 1 || fileErrors[0]["path"] != broken {
		t.Fatalf("expected one file error for %q, got %v", broken, fileErrors)
	}

	if !items[0].Success || items[0].OutputPath != filepath.Join(outputDir, "first_optimized.pdf") {
		t.Fatalf("expected first item to succeed, got %+v", items[0])
	}
	if items[1].Success || items[1].Error == nil {
		t.Fatalf("expected broken item to fail, got %+v", items[1])
	}
	if !items[2].Success || items[2].OutputPath != filepath.Join(outputDir, "second_optimized.pdf") {
		t.Fatalf("expected third item to succeed, got %+v", items[2])
	}
}

func TestOptimizeEngineRefusesToOverwriteExistingOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	output := filepath.Join(dir, "notes_optimized.pdf")
	writePDFFixture(t, input)
	if err := os.WriteFile(output, []byte("taken"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	tool := NewOptimizeTool()
	if _, planErr := tool.Plan(context.Background(), optimizeTestRequest("single", []string{input}, map[string]any{
		"outputPath": output,
	})); planErr == nil {
		t.Fatal("expected planning to reject the existing output")
	}

	if _, itemErr := tool.ExecuteSingle(context.Background(), optimizeTestRequest("single", []string{input}, map[string]any{
		"outputPath": output,
	})); itemErr == nil {
		t.Fatal("expected execution to reject the existing output")
	}

	content, err := os.ReadFile(output)
	if err != nil {
		t.Fatalf("read existing output: %v", err)
	}
	if string(content) != "taken" {
		t.Fatal("existing output must not be overwritten")
	}
}
