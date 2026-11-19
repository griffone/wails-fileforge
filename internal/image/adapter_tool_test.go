package image

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/utils"
)

func writePlanFixture(t *testing.T, path string) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

func planImageRequest(mode string, inputPaths []string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     ToolIDImageConvertV1,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

func TestPlanSingleAutoOutputNextToInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanFixture(t, input)

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, nil))
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
	if want := filepath.Join(dir, "photo.webp"); output.OutputPath != want {
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
	if got := plan.EffectiveOptions["format"]; got != "webp" {
		t.Fatalf("expected effective format webp, got %v", got)
	}
	if _, statErr := os.Stat(output.OutputPath); !os.IsNotExist(statErr) {
		t.Fatal("plan must not create the output file")
	}
}

func TestPlanSameFormatAvoidsOverwritingInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.webp")
	writePlanFixture(t, input)

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{"format": "webp"}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "photo-converted.webp"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag for the same-format rename")
	}
	if len(output.Warnings) == 0 {
		t.Fatal("expected a warning explaining the rename")
	}
}

func TestPlanSuffixesExistingAutoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanFixture(t, input)
	existing := filepath.Join(dir, "photo.webp")
	writePlanFixture(t, existing)

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, nil))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "photo-2.webp"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag when the natural name exists")
	}
	if len(output.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
}

func TestPlanExplicitOutputConflictWhenFileExists(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanFixture(t, input)
	existing := filepath.Join(dir, "custom.webp")
	writePlanFixture(t, existing)

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{"outputPath": existing}))
	if planErr == nil {
		t.Fatal("expected explicit output conflict")
	}
	if planErr.DetailCode != "IMAGE_OUTPUT_EXISTS" {
		t.Fatalf("expected detail code IMAGE_OUTPUT_EXISTS, got %q", planErr.DetailCode)
	}
	if !strings.Contains(planErr.Message, "custom.webp") {
		t.Fatalf("expected message to name the conflicting file, got %q", planErr.Message)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
	if len(plan.Outputs) != 1 || !plan.Outputs[0].Exists {
		t.Fatal("expected the conflicting output to be reported as existing")
	}
	if plan.Error == nil {
		t.Fatal("expected the plan error to be attached")
	}

	content, readErr := os.ReadFile(existing)
	if readErr != nil {
		t.Fatalf("read existing output: %v", readErr)
	}
	if string(content) != "fixture" {
		t.Fatal("plan must not overwrite the existing output")
	}
}

func TestPlanBatchWithoutOutputDirUsesEachInputDirectory(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "first.png")
	second := filepath.Join(root, "two", "second.jpg")
	writePlanFixture(t, first)
	writePlanFixture(t, second)

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("batch", []string{first, second}, map[string]any{"quality": float64(80)}))
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	if want := filepath.Join(root, "one", "first.webp"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(root, "two", "second.webp"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
	if got := plan.EffectiveOptions["quality"]; got != 80 {
		t.Fatalf("expected normalized quality 80, got %v (%T)", got, got)
	}
}

func TestPlanBatchAvoidsDuplicateOutputsAcrossInputs(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "photo.png")
	second := filepath.Join(dir, "photo.jpg")
	writePlanFixture(t, first)
	writePlanFixture(t, second)

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("batch", []string{first, second}, nil))
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	if want := filepath.Join(dir, "photo.webp"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(dir, "photo-2.webp"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
}

func TestPlanUnsupportedInputExtension(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "notes.txt")
	writePlanFixture(t, input)

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, nil))
	if planErr == nil {
		t.Fatal("expected unsupported input error")
	}
	if planErr.DetailCode != "IMAGE_INPUT_UNSUPPORTED" {
		t.Fatalf("expected IMAGE_INPUT_UNSUPPORTED, got %q", planErr.DetailCode)
	}
	if !strings.Contains(planErr.Message, "notes.txt") {
		t.Fatalf("expected message to name the file, got %q", planErr.Message)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
}

func TestPlanMissingInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "missing.png")

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, nil))
	if planErr == nil {
		t.Fatal("expected missing input error")
	}
	if planErr.DetailCode != "IMAGE_INPUT_NOT_FOUND" {
		t.Fatalf("expected IMAGE_INPUT_NOT_FOUND, got %q", planErr.DetailCode)
	}
	if !strings.Contains(planErr.Message, "missing.png") {
		t.Fatalf("expected message to name the file, got %q", planErr.Message)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
}

func TestValidateUsesPlanForBatchWithoutOutputDir(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "first.png")
	second := filepath.Join(root, "second.png")
	writePlanFixture(t, first)
	writePlanFixture(t, second)

	adapter := NewImageToolAdapter(nil)
	validationErr := adapter.Validate(context.Background(), planImageRequest("batch", []string{first, second}, nil))
	if validationErr != nil {
		t.Fatalf("expected batch validation without outputDir to pass, got %v", validationErr)
	}
}

// TestExecuteBatchKeepsInputOrderWhenItemsFinishOutOfOrder blocks the first
// input until every other item finished, so completion order is guaranteed to
// differ from input order. The batch must still return results by input index.
func TestExecuteBatchKeepsInputOrderWhenItemsFinishOutOfOrder(t *testing.T) {
	if utils.ParallelWorkerCount() < 2 {
		t.Skip("batch order test needs at least two parallel workers")
	}

	dir := t.TempDir()
	inputPaths := []string{
		filepath.Join(dir, "first.png"),
		filepath.Join(dir, "second.png"),
		filepath.Join(dir, "third.png"),
	}
	for _, inputPath := range inputPaths {
		writePlanFixture(t, inputPath)
	}

	adapter := NewImageToolAdapter(nil)

	var (
		mu       sync.Mutex
		finished []string
		others   sync.WaitGroup
	)
	others.Add(len(inputPaths) - 1)
	adapter.convertSingle = func(_ context.Context, inputPath, outputPath, _ string, _ map[string]any) error {
		if inputPath == inputPaths[0] {
			others.Wait()
		} else {
			others.Done()
		}

		mu.Lock()
		finished = append(finished, filepath.Base(inputPath))
		mu.Unlock()

		return os.WriteFile(outputPath, []byte("converted"), 0o644)
	}

	var progress []int
	items, execErr := adapter.ExecuteBatch(context.Background(), planImageRequest("batch", inputPaths, nil), func(event models.JobProgressV1) {
		progress = append(progress, event.Current)
	})
	if execErr != nil {
		t.Fatalf("expected the batch to succeed, got %v", execErr)
	}
	if len(items) != len(inputPaths) {
		t.Fatalf("expected %d items, got %d", len(inputPaths), len(items))
	}

	for index, item := range items {
		if item.InputPath != inputPaths[index] {
			t.Fatalf("expected item %d to keep input order, got %+v", index, item)
		}
		if !item.Success {
			t.Fatalf("expected item %d to succeed, got %+v", index, item)
		}
		if want := filepath.Join(dir, strings.TrimSuffix(filepath.Base(inputPaths[index]), ".png")+".webp"); item.OutputPath != want {
			t.Fatalf("expected item %d at %q, got %q", index, want, item.OutputPath)
		}
	}

	mu.Lock()
	completionOrder := append([]string(nil), finished...)
	mu.Unlock()
	if len(completionOrder) != len(inputPaths) || completionOrder[0] == "first.png" {
		t.Fatalf("expected the first input to finish last, got %v", completionOrder)
	}

	if len(progress) != len(inputPaths) {
		t.Fatalf("expected one progress event per item, got %v", progress)
	}
	for index, current := range progress {
		if current != index+1 {
			t.Fatalf("expected monotonic progress counts, got %v", progress)
		}
	}
}
