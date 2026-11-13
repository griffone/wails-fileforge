package pdf

import (
	"context"
	"os"
	"path/filepath"
	"testing"

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
