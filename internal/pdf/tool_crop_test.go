package pdf

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
)

func cropTestRequest(mode string, inputPaths []string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     ToolIDPDFCropV1,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

func TestCropToolPlanAutoOutputNextToInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, map[string]any{
		"cropPreset":    "small",
		"pageSelection": "1",
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
	want := filepath.Join(dir, "notes_cropped.pdf")
	if output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.OutputKind != "file" {
		t.Fatalf("expected file output kind, got %q", output.OutputKind)
	}
	if output.OutputCount != 1 {
		t.Fatalf("expected output count 1, got %d", output.OutputCount)
	}
	if output.Adjusted {
		t.Fatal("expected a natural output name to be unadjusted")
	}
	if output.Exists {
		t.Fatal("expected the planned output to not exist yet")
	}
	if plan.EffectiveOptions["cropPreset"] != "small" {
		t.Fatalf("expected effective cropPreset small, got %v", plan.EffectiveOptions["cropPreset"])
	}
	if plan.EffectiveOptions["pageSelection"] != "1" {
		t.Fatalf("expected effective pageSelection 1, got %v", plan.EffectiveOptions["pageSelection"])
	}

	if _, statErr := os.Stat(want); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("plan must not create the output file, got %v", statErr)
	}
}

func TestCropToolPlanBatchPlansOutputPerInput(t *testing.T) {
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

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), cropTestRequest("batch", []string{first, second}, map[string]any{
		"cropPreset": "none",
	}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	wantOutputs := []string{
		filepath.Join(firstDir, "report_cropped.pdf"),
		filepath.Join(secondDir, "report_cropped.pdf"),
	}
	for i, output := range plan.Outputs {
		if output.OutputPath != wantOutputs[i] {
			t.Fatalf("expected output %d %q, got %q", i, wantOutputs[i], output.OutputPath)
		}
		if output.OutputKind != "file" {
			t.Fatalf("expected file output kind, got %q", output.OutputKind)
		}
		if output.Adjusted {
			t.Fatalf("expected output %d to be unadjusted", i)
		}
	}
}

func TestCropToolPlanUsesChosenOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	tool := NewCropTool()
	req := cropTestRequest("batch", []string{first, second}, map[string]any{
		"cropPreset": "small",
	})
	req.OutputDir = outDir

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	wantOutputs := []string{
		filepath.Join(outDir, "first_cropped.pdf"),
		filepath.Join(outDir, "second_cropped.pdf"),
	}
	for i, output := range plan.Outputs {
		if output.OutputPath != wantOutputs[i] {
			t.Fatalf("expected output %d %q, got %q", i, wantOutputs[i], output.OutputPath)
		}
	}
}

func TestCropToolPlanBatchSuffixesCollisionsInChosenOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

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

	tool := NewCropTool()
	req := cropTestRequest("batch", []string{first, second}, map[string]any{
		"cropPreset": "small",
	})
	req.OutputDir = outDir

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	wantOutputs := []string{
		filepath.Join(outDir, "report_cropped.pdf"),
		filepath.Join(outDir, "report_cropped-2.pdf"),
	}
	for i, output := range plan.Outputs {
		if output.OutputPath != wantOutputs[i] {
			t.Fatalf("expected output %d %q, got %q", i, wantOutputs[i], output.OutputPath)
		}
	}
	if plan.Outputs[0].Adjusted {
		t.Fatal("expected the first output to keep the natural name")
	}
	if !plan.Outputs[1].Adjusted {
		t.Fatal("expected the second output to be suffixed")
	}
}

func TestCropToolPlanUsesLegacyOptionsOutputDir(t *testing.T) {
	dir := t.TempDir()
	outDir := filepath.Join(dir, "exports")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, map[string]any{
		"cropPreset": "small",
		"outputDir":  outDir,
	}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	want := filepath.Join(outDir, "notes_cropped.pdf")
	if plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
}

func TestCropToolPlanSuffixesExistingAutoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	existing := filepath.Join(dir, "notes_cropped.pdf")
	if err := os.WriteFile(existing, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, map[string]any{
		"cropPreset": "small",
	}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "notes_cropped-2.pdf"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag when the natural name exists")
	}
	if len(output.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
	if len(plan.Warnings) == 0 {
		t.Fatal("expected the plan to surface the output warning")
	}

	content, readErr := os.ReadFile(existing)
	if readErr != nil {
		t.Fatalf("read existing output: %v", readErr)
	}
	if string(content) != "existing" {
		t.Fatal("plan must not overwrite the existing output")
	}
}

func TestCropToolPlanExplicitOutputConflict(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	outputPath := filepath.Join(dir, "cropped.pdf")
	if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, map[string]any{
		"cropPreset": "small",
		"outputPath": outputPath,
	}))
	if planErr == nil {
		t.Fatal("expected explicit output conflict")
	}
	if planErr.DetailCode != engine.ErrorCodeCropOutputExists {
		t.Fatalf("expected %s, got %q", engine.ErrorCodeCropOutputExists, planErr.DetailCode)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}

	content, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("read existing output: %v", readErr)
	}
	if string(content) != "existing" {
		t.Fatal("plan must not overwrite the existing explicit output")
	}
}

func TestCropToolPlanCustomPresetRequiresMargins(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewCropTool()
	_, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, map[string]any{
		"cropPreset": "custom",
	}))
	if planErr == nil {
		t.Fatal("expected custom preset without margins to fail")
	}
	if planErr.DetailCode != engine.ErrorCodeCropMarginsRequired {
		t.Fatalf("expected %s, got %q", engine.ErrorCodeCropMarginsRequired, planErr.DetailCode)
	}

	_, rangeErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, map[string]any{
		"cropPreset": "custom",
		"margins": map[string]any{
			"top":    10.0,
			"right":  10.0,
			"bottom": 10.0,
			"left":   721.0,
		},
	}))
	if rangeErr == nil {
		t.Fatal("expected out-of-range custom margins to fail")
	}
	if rangeErr.DetailCode != engine.ErrorCodeCropMarginsInvalid {
		t.Fatalf("expected %s, got %q", engine.ErrorCodeCropMarginsInvalid, rangeErr.DetailCode)
	}
}

func TestCropToolPlanInvalidPageSelection(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewCropTool()
	_, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, map[string]any{
		"cropPreset":    "small",
		"pageSelection": "3-1",
	}))
	if planErr == nil {
		t.Fatal("expected invalid page selection to fail")
	}
	if planErr.DetailCode != engine.ErrorCodeCropPageSelectionBad {
		t.Fatalf("expected %s, got %q", engine.ErrorCodeCropPageSelectionBad, planErr.DetailCode)
	}
}

func TestCropToolPlanMissingInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "missing.pdf")

	tool := NewCropTool()
	_, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, map[string]any{
		"cropPreset": "small",
	}))
	if planErr == nil {
		t.Fatal("expected a missing input to fail")
	}
	if planErr.DetailCode != engine.ErrorCodeInvalidInputPDF {
		t.Fatalf("expected %s, got %q", engine.ErrorCodeInvalidInputPDF, planErr.DetailCode)
	}
}

func TestCropToolExecuteSingleUsesPlannedAutoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewCropTool()
	req := cropTestRequest("single", []string{input}, map[string]any{"cropPreset": "none"})

	item, itemErr := tool.ExecuteSingle(context.Background(), req)
	if itemErr != nil {
		t.Fatalf("expected crop to succeed, got %v", itemErr)
	}
	if !item.Success {
		t.Fatalf("expected successful item, got %q", item.Message)
	}

	outputPath := filepath.Join(dir, "notes_cropped.pdf")
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
		t.Fatalf("expected cropped output to exist: %v", statErr)
	}
	if info.Size() == 0 {
		t.Fatal("expected cropped output to be non-empty")
	}

	// Running the same request again must use a new planned output.
	secondItem, secondErr := tool.ExecuteSingle(context.Background(), req)
	if secondErr != nil {
		t.Fatalf("expected second crop to succeed, got %v", secondErr)
	}
	if want := filepath.Join(dir, "notes_cropped-2.pdf"); secondItem.OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, secondItem.OutputPath)
	}
}

func TestCropToolExecuteBatchPlansNextToEachInput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	tool := NewCropTool()
	items, jobErr := tool.ExecuteBatch(context.Background(), cropTestRequest("batch", []string{first, second}, map[string]any{
		"cropPreset": "none",
	}), nil)
	if jobErr != nil {
		t.Fatalf("expected batch to succeed, got %v", jobErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}

	wantOutputs := []string{
		filepath.Join(dir, "first_cropped.pdf"),
		filepath.Join(dir, "second_cropped.pdf"),
	}
	for i, item := range items {
		if !item.Success || item.Error != nil {
			t.Fatalf("expected item %d to succeed, got %+v", i, item)
		}
		if item.OutputPath != wantOutputs[i] {
			t.Fatalf("expected item %d output %q, got %q", i, wantOutputs[i], item.OutputPath)
		}
		if len(item.Outputs) != 1 || item.OutputCount != 1 {
			t.Fatalf("expected item %d to expose one output, got %+v", i, item)
		}
		if _, statErr := os.Stat(item.Outputs[0]); statErr != nil {
			t.Fatalf("expected item %d output on disk: %v", i, statErr)
		}
	}
}

func TestCropToolExecuteBatchReportsPartialSuccess(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	broken := filepath.Join(dir, "broken.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)
	if err := os.WriteFile(broken, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write invalid pdf: %v", err)
	}

	tool := NewCropTool()
	items, jobErr := tool.ExecuteBatch(context.Background(), cropTestRequest("batch", []string{first, broken, second}, map[string]any{
		"cropPreset": "none",
	}), nil)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if jobErr == nil {
		t.Fatal("expected aggregate batch error")
	}
	if jobErr.Code != models.CanonicalErrorCode(engine.ErrorCodeCropFailed) {
		t.Fatalf("expected canonical crop failure code, got %q", jobErr.Code)
	}
	if jobErr.DetailCode != engine.ErrorCodeCropFailed {
		t.Fatalf("expected detail code %s, got %q", engine.ErrorCodeCropFailed, jobErr.DetailCode)
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

	if !items[0].Success || items[0].Error != nil {
		t.Fatalf("expected first item to succeed, got %+v", items[0])
	}
	if items[1].Success || items[1].Error == nil {
		t.Fatalf("expected second item to fail, got %+v", items[1])
	}
	if !items[2].Success || items[2].Error != nil {
		t.Fatalf("expected third item to succeed, got %+v", items[2])
	}
}

func TestCropToolExecutePlanHonorsResolvedPlan(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.pdf")
	writePDFFixture(t, input)

	tool := NewCropTool()
	req := cropTestRequest("single", []string{input}, map[string]any{"cropPreset": "none"})

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
		t.Fatalf("expected planned crop to succeed, got %v", execErr)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 result item, got %d", len(items))
	}
	if !items[0].Success || items[0].OutputPath != plannedOutput {
		t.Fatalf("expected the planned output %q, got %+v", plannedOutput, items[0])
	}
	if _, statErr := os.Stat(plannedOutput); statErr != nil {
		t.Fatalf("expected the planned output on disk: %v", statErr)
	}
	if _, statErr := os.Stat(requestOutput); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the request-resolved output %q to be unused, got %v", requestOutput, statErr)
	}
}

func TestCropToolExecutePlanBatchSubset(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	tool := NewCropTool()
	req := cropTestRequest("batch", []string{first, second}, map[string]any{"cropPreset": "none"})

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
		t.Fatalf("expected the subset output on disk: %v", statErr)
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the excluded output %q to stay untouched, got %v", plan.Outputs[0].OutputPath, statErr)
	}
}

func TestCropToolExecutePlanPreservesOrderAndItemFailures(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	broken := filepath.Join(dir, "broken.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)
	if err := os.WriteFile(broken, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write invalid pdf: %v", err)
	}

	tool := NewCropTool()
	req := cropTestRequest("batch", []string{first, broken, second}, map[string]any{"cropPreset": "none"})

	plan, planErr := tool.Plan(context.Background(), req)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	items, execErr := tool.ExecutePlan(context.Background(), req, plan, nil)
	if execErr == nil {
		t.Fatal("expected an aggregate crop failure")
	}
	if execErr.DetailCode != engine.ErrorCodeCropFailed {
		t.Fatalf("expected %s, got %q", engine.ErrorCodeCropFailed, execErr.DetailCode)
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
