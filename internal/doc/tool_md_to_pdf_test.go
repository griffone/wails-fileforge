package doc

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

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

func mdPageSetupFromPlan(t *testing.T, effective map[string]any) map[string]any {
	t.Helper()

	if effective == nil {
		t.Fatal("expected effective options to include the page setup")
	}

	setup, ok := effective["pageSetup"].(map[string]any)
	if !ok {
		t.Fatalf("expected effectiveOptions.pageSetup object, got %#v", effective["pageSetup"])
	}

	return setup
}

func mdMarginsFromSetup(t *testing.T, setup map[string]any) map[string]any {
	t.Helper()

	margins, ok := setup["margins"].(map[string]any)
	if !ok {
		t.Fatalf("expected pageSetup.margins object, got %#v", setup["margins"])
	}

	return margins
}

func assertFloat(t *testing.T, label string, got, want float64) {
	t.Helper()

	if math.Abs(got-want) > 0.01 {
		t.Fatalf("expected %s %.2f, got %.2f", label, want, got)
	}
}

func TestMDPlanDefaultPageSetup(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	writeDocFixture(t, input, "# Notes\n")

	tool := NewMDToPDFTool()
	plan, planErr := tool.Plan(context.Background(), mdPlanRequest(input, "", nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	setup := mdPageSetupFromPlan(t, plan.EffectiveOptions)
	if setup["pageSize"] != "A4" {
		t.Fatalf("expected default page size A4, got %#v", setup["pageSize"])
	}
	if setup["orientation"] != "P" {
		t.Fatalf("expected default orientation P, got %#v", setup["orientation"])
	}

	margins := mdMarginsFromSetup(t, setup)
	assertFloat(t, "default top margin", margins["top"].(float64), 20)
	assertFloat(t, "default right margin", margins["right"].(float64), 15)
	assertFloat(t, "default bottom margin", margins["bottom"].(float64), 18)
	assertFloat(t, "default left margin", margins["left"].(float64), 15)
}

func TestMDPlanIncludesCustomPageSetup(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	writeDocFixture(t, input, "# Notes\n")

	options := map[string]any{
		"pageSize":    "letter",
		"orientation": "l",
		"margins": map[string]any{
			"top":  30,
			"left": 25.5,
		},
	}

	tool := NewMDToPDFTool()
	plan, planErr := tool.Plan(context.Background(), mdPlanRequest(input, "", options))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	setup := mdPageSetupFromPlan(t, plan.EffectiveOptions)
	if setup["pageSize"] != "Letter" {
		t.Fatalf("expected normalized page size Letter, got %#v", setup["pageSize"])
	}
	if setup["orientation"] != "L" {
		t.Fatalf("expected normalized orientation L, got %#v", setup["orientation"])
	}

	margins := mdMarginsFromSetup(t, setup)
	assertFloat(t, "custom top margin", margins["top"].(float64), 30)
	assertFloat(t, "custom left margin", margins["left"].(float64), 25.5)
	assertFloat(t, "default right margin", margins["right"].(float64), 15)
	assertFloat(t, "default bottom margin", margins["bottom"].(float64), 18)
}

func TestMDExecuteSingleDefaultPageIsA4Portrait(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	writeDocFixture(t, input, "# Notes\n\nHello from FileForge.\n")

	tool := NewMDToPDFTool()
	item, itemErr := tool.ExecuteSingle(context.Background(), mdPlanRequest(input, "", nil))
	if itemErr != nil {
		t.Fatalf("expected execution to succeed, got %v", itemErr)
	}

	dims, dimsErr := api.PageDimsFile(item.OutputPath)
	if dimsErr != nil {
		t.Fatalf("read rendered page dimensions: %v", dimsErr)
	}
	if len(dims) != 1 {
		t.Fatalf("expected 1 rendered page, got %d", len(dims))
	}
	if dims[0].Width >= dims[0].Height {
		t.Fatalf("expected portrait page, got %.2fx%.2f", dims[0].Width, dims[0].Height)
	}
	assertFloat(t, "A4 width", dims[0].Width, 595.28)
	assertFloat(t, "A4 height", dims[0].Height, 841.89)
}

func TestMDExecuteSingleLetterLandscapeProducesLandscapePage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	writeDocFixture(t, input, "# Notes\n\nHello from FileForge.\n")

	options := map[string]any{
		"pageSize":    "Letter",
		"orientation": "L",
		"margins": map[string]any{
			"top":    10,
			"right":  10,
			"bottom": 10,
			"left":   10,
		},
	}

	tool := NewMDToPDFTool()
	item, itemErr := tool.ExecuteSingle(context.Background(), mdPlanRequest(input, "", options))
	if itemErr != nil {
		t.Fatalf("expected execution to succeed, got %v", itemErr)
	}

	dims, dimsErr := api.PageDimsFile(item.OutputPath)
	if dimsErr != nil {
		t.Fatalf("read rendered page dimensions: %v", dimsErr)
	}
	if len(dims) != 1 {
		t.Fatalf("expected 1 rendered page, got %d", len(dims))
	}
	if dims[0].Width <= dims[0].Height {
		t.Fatalf("expected landscape page, got %.2fx%.2f", dims[0].Width, dims[0].Height)
	}
	assertFloat(t, "Letter landscape width", dims[0].Width, 792)
	assertFloat(t, "Letter landscape height", dims[0].Height, 612)
}

func TestMDPlanRejectsInvalidPageSetup(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	writeDocFixture(t, input, "# Notes\n")

	cases := []struct {
		name    string
		options map[string]any
	}{
		{name: "unknown page size", options: map[string]any{"pageSize": "A5"}},
		{name: "non string page size", options: map[string]any{"pageSize": 5}},
		{name: "unknown orientation", options: map[string]any{"orientation": "diagonal"}},
		{name: "non string orientation", options: map[string]any{"orientation": true}},
		{name: "margins not an object", options: map[string]any{"margins": "wide"}},
		{name: "non numeric margin", options: map[string]any{"margins": map[string]any{"right": "wide"}}},
		{name: "negative margin", options: map[string]any{"margins": map[string]any{"top": -1}}},
		{name: "margin above cap", options: map[string]any{"margins": map[string]any{"left": 101}}},
		{name: "non finite margin", options: map[string]any{"margins": map[string]any{"bottom": "NaN"}}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			tool := NewMDToPDFTool()
			plan, planErr := tool.Plan(context.Background(), mdPlanRequest(input, "", tc.options))
			if planErr == nil {
				t.Fatal("expected an invalid page setup error")
			}
			if planErr.DetailCode != "DOC_MD_TO_PDF_CONFIG_INVALID" {
				t.Fatalf("expected DOC_MD_TO_PDF_CONFIG_INVALID, got %q", planErr.DetailCode)
			}
			if planErr.Message == "" {
				t.Fatal("expected a clear validation message")
			}
			if plan.Success {
				t.Fatal("expected plan to report failure")
			}
		})
	}
}

func TestMDExecuteSingleKeepsHeaderFooterWithCustomPageSetup(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.md")
	writeDocFixture(t, input, "# Notes\n\nHello from FileForge.\n")

	options := map[string]any{
		"pageSize":    "Legal",
		"orientation": "P",
		"margins": map[string]any{
			"top":    25,
			"right":  20,
			"bottom": 25,
			"left":   20,
		},
		"header": map[string]any{
			"enabled":      true,
			"text":         "{fileName} - {page}/{totalPages}",
			"align":        "center",
			"font":         "helvetica",
			"marginTop":    10,
			"marginBottom": 4,
			"color":        "#333333",
		},
		"footer": map[string]any{
			"enabled":      true,
			"text":         "{date}",
			"align":        "right",
			"font":         "helvetica",
			"marginTop":    0,
			"marginBottom": 8,
			"color":        "#666666",
		},
	}

	tool := NewMDToPDFTool()
	item, itemErr := tool.ExecuteSingle(context.Background(), mdPlanRequest(input, "", options))
	if itemErr != nil {
		t.Fatalf("expected execution to succeed, got %v", itemErr)
	}

	dims, dimsErr := api.PageDimsFile(item.OutputPath)
	if dimsErr != nil {
		t.Fatalf("read rendered page dimensions: %v", dimsErr)
	}
	if len(dims) != 1 {
		t.Fatalf("expected 1 rendered page, got %d", len(dims))
	}
	if dims[0].Width >= dims[0].Height {
		t.Fatalf("expected Legal portrait page, got %.2fx%.2f", dims[0].Width, dims[0].Height)
	}
	assertFloat(t, "Legal width", dims[0].Width, 612)
	assertFloat(t, "Legal height", dims[0].Height, 1008)
}
