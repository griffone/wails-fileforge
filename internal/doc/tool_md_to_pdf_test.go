package doc

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
)

func writeDocFixture(t *testing.T, path, content string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func mdPlanRequest(inputPath, outputDir string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     ToolIDDocMDToPDFV1,
		Mode:       "single",
		InputPaths: []string{inputPath},
		OutputDir:  outputDir,
		Options:    options,
	}
}

func TestMDPlanAutoOutputNextToInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	writeDocFixture(t, input, "# Notes\n")

	tool := NewMDToPDFTool()
	plan, planErr := tool.Plan(context.Background(), mdPlanRequest(input, "", nil))
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
	want := filepath.Join(dir, "notes_md2pdf.pdf")
	if output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.InputPath != input {
		t.Fatalf("expected input %q, got %q", input, output.InputPath)
	}
	if output.OutputKind != "file" {
		t.Fatalf("expected file output kind, got %q", output.OutputKind)
	}
	if output.Exists {
		t.Fatal("expected planned output to not exist yet")
	}
	if output.Adjusted {
		t.Fatal("expected natural output name to be unadjusted")
	}
	if len(output.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", output.Warnings)
	}
	if _, statErr := os.Stat(output.OutputPath); !os.IsNotExist(statErr) {
		t.Fatal("plan must not create the output file")
	}
}

func TestMDPlanSuffixesExistingAutoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	writeDocFixture(t, input, "# Notes\n")
	existing := filepath.Join(dir, "notes_md2pdf.pdf")
	writeDocFixture(t, existing, "existing")

	tool := NewMDToPDFTool()
	plan, planErr := tool.Plan(context.Background(), mdPlanRequest(input, "", nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	want := filepath.Join(dir, "notes_md2pdf-2.pdf")
	if output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag when the natural name exists")
	}
	if len(output.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
	if !strings.Contains(output.Warnings[0], "notes_md2pdf.pdf") || !strings.Contains(output.Warnings[0], "notes_md2pdf-2.pdf") {
		t.Fatalf("expected warning to name both paths, got %q", output.Warnings[0])
	}
	if len(plan.Warnings) == 0 {
		t.Fatal("expected plan-level warnings")
	}
}

func TestMDPlanExplicitOutputSuffixesExistingFile(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	writeDocFixture(t, input, "# Notes\n")
	explicit := filepath.Join(dir, "custom.pdf")
	writeDocFixture(t, explicit, "existing")

	tool := NewMDToPDFTool()
	plan, planErr := tool.Plan(context.Background(), mdPlanRequest(input, "", map[string]any{"outputPath": explicit}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	want := filepath.Join(dir, "custom-2.pdf")
	if output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag for the explicit collision")
	}

	content, readErr := os.ReadFile(explicit)
	if readErr != nil {
		t.Fatalf("read existing output: %v", readErr)
	}
	if string(content) != "existing" {
		t.Fatal("plan must not overwrite the existing output")
	}
}

func TestMDPlanExplicitOutputKeepsFreePath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	writeDocFixture(t, input, "# Notes\n")
	explicit := filepath.Join(dir, "custom.pdf")

	tool := NewMDToPDFTool()
	plan, planErr := tool.Plan(context.Background(), mdPlanRequest(input, "", map[string]any{"outputPath": explicit}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if output.OutputPath != explicit {
		t.Fatalf("expected output %q, got %q", explicit, output.OutputPath)
	}
	if output.Adjusted {
		t.Fatal("expected a free explicit path to stay unadjusted")
	}
}

func TestMDExecuteSingleWritesPlannedOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	writeDocFixture(t, input, "# Notes\n\nHello from FileForge.\n")

	tool := NewMDToPDFTool()
	req := mdPlanRequest(input, "", nil)

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	item, itemErr := tool.ExecuteSingle(context.Background(), req)
	if itemErr != nil {
		t.Fatalf("expected execution to succeed, got %v", itemErr)
	}
	if !item.Success {
		t.Fatalf("expected successful item, got %q", item.Message)
	}
	if item.OutputPath != plan.Outputs[0].OutputPath {
		t.Fatalf("execution wrote %q but the plan promised %q", item.OutputPath, plan.Outputs[0].OutputPath)
	}
	if _, statErr := os.Stat(item.OutputPath); statErr != nil {
		t.Fatalf("expected output file to exist: %v", statErr)
	}
}

func TestMDPlanRejectsUnsupportedInputExtension(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.txt")
	writeDocFixture(t, input, "not markdown")

	tool := NewMDToPDFTool()
	plan, planErr := tool.Plan(context.Background(), mdPlanRequest(input, "", nil))
	if planErr == nil {
		t.Fatal("expected unsupported input error")
	}
	if planErr.DetailCode != "DOC_MD_TO_PDF_INPUT_UNSUPPORTED" {
		t.Fatalf("expected DOC_MD_TO_PDF_INPUT_UNSUPPORTED, got %q", planErr.DetailCode)
	}
	if !strings.Contains(planErr.Message, "notes.txt") {
		t.Fatalf("expected message to name the file, got %q", planErr.Message)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
	if plan.Error == nil {
		t.Fatal("expected the plan error to be attached")
	}
}
