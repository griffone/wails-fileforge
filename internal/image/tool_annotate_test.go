package image

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
)

func annotateTestRequest(mode string, inputPaths []string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     ToolIDImageAnnotateV1,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

func rectAnnotation(x, y, width, height int) map[string]any {
	return map[string]any{
		"type":        "rect",
		"x":           x,
		"y":           y,
		"width":       width,
		"height":      height,
		"color":       "#ff0000",
		"strokeWidth": 2,
	}
}

func annotateOptions(operations ...map[string]any) map[string]any {
	list := make([]any, 0, len(operations))
	for _, operation := range operations {
		list = append(list, operation)
	}

	return map[string]any{"operations": list}
}

func TestAnnotatePlanSingleAutoOutputNextToInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	tool := NewAnnotateTool()
	plan, planErr := tool.Plan(context.Background(), annotateTestRequest("single", []string{input}, annotateOptions(rectAnnotation(10, 10, 40, 30))))
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
	if want := filepath.Join(dir, "photo_annotated.png"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.InputPath != input {
		t.Fatalf("expected input %q, got %q", input, output.InputPath)
	}
	if output.OutputKind != "file" {
		t.Fatalf("expected file output kind, got %q", output.OutputKind)
	}
	if output.Exists || output.Adjusted {
		t.Fatalf("expected a fresh, unadjusted output, got exists=%v adjusted=%v", output.Exists, output.Adjusted)
	}
	if got := plan.EffectiveOptions["format"]; got != "png" {
		t.Fatalf("expected effective format png, got %v", got)
	}
	if got := plan.EffectiveOptions["operationCount"]; got != 1 {
		t.Fatalf("expected effective operationCount 1, got %v (%T)", got, got)
	}
	if _, statErr := os.Stat(output.OutputPath); !os.IsNotExist(statErr) {
		t.Fatal("plan must not create the output file")
	}
}

func TestAnnotatePlanSingleSuffixesExistingAutoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)
	writePlanPNG(t, filepath.Join(dir, "photo_annotated.png"), 10, 10)

	tool := NewAnnotateTool()
	plan, planErr := tool.Plan(context.Background(), annotateTestRequest("single", []string{input}, annotateOptions(rectAnnotation(0, 0, 20, 20))))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "photo_annotated-2.png"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag when the natural name exists")
	}
	if len(output.Warnings) == 0 || len(plan.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
}

func TestAnnotatePlanSingleChosenOutputDir(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "src", "photo.png")
	chosen := filepath.Join(root, "out")
	writePlanPNG(t, input, 120, 90)
	if err := os.MkdirAll(chosen, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	cases := []struct {
		name  string
		apply func(request *models.JobRequestV1)
	}{
		{
			name:  "request output dir",
			apply: func(request *models.JobRequestV1) { request.OutputDir = chosen },
		},
		{
			name:  "option output dir",
			apply: func(request *models.JobRequestV1) { request.Options["outputDir"] = chosen },
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := annotateTestRequest("single", []string{input}, annotateOptions(rectAnnotation(0, 0, 20, 20)))
			testCase.apply(&request)

			tool := NewAnnotateTool()
			plan, planErr := tool.Plan(context.Background(), request)
			if planErr != nil {
				t.Fatalf("expected plan to succeed, got %v", planErr)
			}

			if want := filepath.Join(chosen, "photo_annotated.png"); plan.Outputs[0].OutputPath != want {
				t.Fatalf("expected output %q, got %q", want, plan.Outputs[0].OutputPath)
			}
		})
	}
}

func TestAnnotatePlanSingleExplicitOutputConflict(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	existing := filepath.Join(dir, "custom.png")
	writePlanPNG(t, input, 120, 90)
	writePlanPNG(t, existing, 10, 10)

	tool := NewAnnotateTool()

	options := annotateOptions(rectAnnotation(0, 0, 20, 20))
	options["outputPath"] = existing

	plan, planErr := tool.Plan(context.Background(), annotateTestRequest("single", []string{input}, options))
	if planErr == nil {
		t.Fatal("expected explicit output conflict")
	}
	if planErr.DetailCode != "IMAGE_ANNOTATE_OUTPUT_EXISTS" {
		t.Fatalf("expected detail code IMAGE_ANNOTATE_OUTPUT_EXISTS, got %q", planErr.DetailCode)
	}
	if !strings.Contains(planErr.Message, "custom.png") {
		t.Fatalf("expected message to name the conflicting file, got %q", planErr.Message)
	}
	if plan.Success {
		t.Fatal("expected plan to report failure")
	}
	if len(plan.Outputs) != 1 || !plan.Outputs[0].Exists {
		t.Fatal("expected the conflicting output to be reported as existing")
	}

	sameAsInput := annotateOptions(rectAnnotation(0, 0, 20, 20))
	sameAsInput["outputPath"] = input
	_, sameErr := tool.Plan(context.Background(), annotateTestRequest("single", []string{input}, sameAsInput))
	if sameErr == nil {
		t.Fatal("expected output path matching the input to fail")
	}
	if sameErr.DetailCode != "IMAGE_ANNOTATE_OUTPUT_COLLIDES_INPUT" {
		t.Fatalf("expected IMAGE_ANNOTATE_OUTPUT_COLLIDES_INPUT, got %q", sameErr.DetailCode)
	}
}

func TestAnnotatePlanBatchWithoutOutputDirUsesEachInputDirectory(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "first.png")
	second := filepath.Join(root, "two", "second.png")
	writePlanPNG(t, first, 120, 90)
	writePlanPNG(t, second, 60, 40)

	operations := annotateOptions(rectAnnotation(0, 0, 20, 20))

	tool := NewAnnotateTool()
	plan, planErr := tool.Plan(context.Background(), annotateTestRequest("batch", []string{first, second}, operations))
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	if want := filepath.Join(root, "one", "first_annotated.png"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(root, "two", "second_annotated.png"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
	if got := plan.EffectiveOptions["format"]; got != "png" {
		t.Fatalf("expected effective format png, got %v", got)
	}
}

func TestAnnotatePlanBatchChosenOutputDirSuffixesDuplicateNames(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "photo.png")
	second := filepath.Join(root, "two", "photo.png")
	chosen := filepath.Join(root, "out")
	writePlanPNG(t, first, 120, 90)
	writePlanPNG(t, second, 60, 40)
	if err := os.MkdirAll(chosen, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	request := annotateTestRequest("batch", []string{first, second}, annotateOptions(rectAnnotation(0, 0, 20, 20)))
	request.OutputDir = chosen

	tool := NewAnnotateTool()
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	if want := filepath.Join(chosen, "photo_annotated.png"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(chosen, "photo_annotated-2.png"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
	if !plan.Outputs[1].Adjusted {
		t.Fatal("expected adjusted flag for the second colliding output")
	}
}

func TestAnnotatePlanRejectsInvalidOperations(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	cases := []struct {
		name    string
		options map[string]any
		want    string
	}{
		{
			name:    "missing operations",
			options: map[string]any{},
			want:    "IMAGE_ANNOTATE_OPTION_REQUIRED",
		},
		{
			name:    "empty operations",
			options: map[string]any{"operations": []any{}},
			want:    "IMAGE_ANNOTATE_OPERATIONS_EMPTY",
		},
		{
			name:    "unsupported operation type",
			options: annotateOptions(map[string]any{"type": "star", "x": 10, "y": 10}),
			want:    "IMAGE_ANNOTATE_OPERATION_UNSUPPORTED",
		},
		{
			name:    "text without content",
			options: annotateOptions(map[string]any{"type": "text", "x": 10, "y": 10}),
			want:    "IMAGE_ANNOTATE_TEXT_REQUIRED",
		},
		{
			name:    "rectangle outside the image",
			options: annotateOptions(rectAnnotation(100, 80, 40, 30)),
			want:    "IMAGE_ANNOTATE_OUT_OF_BOUNDS",
		},
		{
			name:    "arrow without two distinct points",
			options: annotateOptions(map[string]any{"type": "arrow", "x": 10, "y": 10, "x2": 10, "y2": 10}),
			want:    "IMAGE_ANNOTATE_ARROW_INVALID",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tool := NewAnnotateTool()
			plan, planErr := tool.Plan(context.Background(), annotateTestRequest("single", []string{input}, testCase.options))
			if planErr == nil {
				t.Fatalf("expected plan to fail with %s", testCase.want)
			}
			if planErr.DetailCode != testCase.want {
				t.Fatalf("expected detail code %q, got %q (%s)", testCase.want, planErr.DetailCode, planErr.Message)
			}
			if plan.Success {
				t.Fatal("expected plan to report failure")
			}
			if plan.Error == nil {
				t.Fatal("expected the plan error to be attached")
			}
		})
	}
}

func TestAnnotateExecutionUsesPlannedSinglePath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	tool := NewAnnotateTool()
	request := annotateTestRequest("single", []string{input}, annotateOptions(rectAnnotation(10, 10, 40, 30)))
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	plannedPath := plan.Outputs[0].OutputPath

	item, execErr := tool.ExecuteSingle(context.Background(), request)
	if execErr != nil {
		t.Fatalf("expected annotate to succeed, got %v", execErr)
	}
	if !item.Success {
		t.Fatalf("expected success item, got %+v", item)
	}
	if item.OutputPath != plannedPath {
		t.Fatalf("expected execution to write %q, got %q", plannedPath, item.OutputPath)
	}

	file, openErr := os.Open(plannedPath)
	if openErr != nil {
		t.Fatalf("open annotated output: %v", openErr)
	}
	defer file.Close()

	config, decodeErr := png.DecodeConfig(file)
	if decodeErr != nil {
		t.Fatalf("decode annotated output: %v", decodeErr)
	}
	if config.Width != 120 || config.Height != 90 {
		t.Fatalf("expected the annotated image to keep its size, got %dx%d", config.Width, config.Height)
	}
	if _, statErr := os.Stat(input); statErr != nil {
		t.Fatalf("expected the input to stay untouched: %v", statErr)
	}
}

func TestAnnotateExecutionUsesPlannedBatchPaths(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "first.png")
	second := filepath.Join(root, "two", "second.png")
	writePlanPNG(t, first, 120, 90)
	writePlanPNG(t, second, 60, 40)

	tool := NewAnnotateTool()
	request := annotateTestRequest("batch", []string{first, second}, annotateOptions(rectAnnotation(0, 0, 20, 20)))
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	var progress []models.JobProgressV1
	items, execErr := tool.ExecuteBatch(context.Background(), request, func(event models.JobProgressV1) {
		progress = append(progress, event)
	})
	if execErr != nil {
		t.Fatalf("expected batch annotate to succeed, got %v", execErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 result items, got %d", len(items))
	}

	for index, item := range items {
		if !item.Success {
			t.Fatalf("expected item %d to succeed, got %+v", index, item)
		}
		if item.OutputPath != plan.Outputs[index].OutputPath {
			t.Fatalf("expected item %d to write %q, got %q", index, plan.Outputs[index].OutputPath, item.OutputPath)
		}
		if _, statErr := os.Stat(item.OutputPath); statErr != nil {
			t.Fatalf("expected item %d output on disk: %v", index, statErr)
		}
	}

	if len(progress) != 2 || progress[1].Current != 2 || progress[1].Total != 2 {
		t.Fatalf("expected progress for both items, got %+v", progress)
	}
}
