package image

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"
)

func writePlanPNG(t *testing.T, path string, width, height int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 96, A: 255})
		}
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png fixture %s: %v", path, err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode png fixture %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close png fixture %s: %v", path, err)
	}
}

func cropTestRequest(mode string, inputPaths []string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     ToolIDImageCropV1,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

func cropOptions(x, y, width, height int) map[string]any {
	return map[string]any{"x": x, "y": y, "width": width, "height": height}
}

func TestCropPlanSingleAutoOutputNextToInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, cropOptions(10, 10, 40, 30)))
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
	if want := filepath.Join(dir, "photo_cropped.png"); output.OutputPath != want {
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
	if got := plan.EffectiveOptions["ratioPreset"]; got != "free" {
		t.Fatalf("expected effective ratioPreset free, got %v", got)
	}
	if got := plan.EffectiveOptions["width"]; got != 40 {
		t.Fatalf("expected effective width 40, got %v (%T)", got, got)
	}
	if _, statErr := os.Stat(output.OutputPath); !os.IsNotExist(statErr) {
		t.Fatal("plan must not create the output file")
	}
}

func TestCropPlanSingleSuffixesExistingAutoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)
	writePlanPNG(t, filepath.Join(dir, "photo_cropped.png"), 10, 10)

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, cropOptions(0, 0, 20, 20)))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "photo_cropped-2.png"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected adjusted flag when the natural name exists")
	}
	if len(output.Warnings) == 0 || len(plan.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
}

func TestCropPlanSingleChosenOutputDir(t *testing.T) {
	root := t.TempDir()
	input := filepath.Join(root, "src", "photo.png")
	chosen := filepath.Join(root, "out")
	other := filepath.Join(root, "other")
	writePlanPNG(t, input, 120, 90)
	if err := os.MkdirAll(chosen, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}
	if err := os.MkdirAll(other, 0o755); err != nil {
		t.Fatalf("create secondary output dir: %v", err)
	}

	cases := []struct {
		name  string
		apply func(request *models.JobRequestV1)
		want  string
	}{
		{
			name:  "request output dir",
			apply: func(request *models.JobRequestV1) { request.OutputDir = chosen },
			want:  filepath.Join(chosen, "photo_cropped.png"),
		},
		{
			name:  "option output dir",
			apply: func(request *models.JobRequestV1) { request.Options["outputDir"] = chosen },
			want:  filepath.Join(chosen, "photo_cropped.png"),
		},
		{
			name: "request output dir wins over the option",
			apply: func(request *models.JobRequestV1) {
				request.OutputDir = chosen
				request.Options["outputDir"] = other
			},
			want: filepath.Join(chosen, "photo_cropped.png"),
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := cropTestRequest("single", []string{input}, cropOptions(0, 0, 20, 20))
			testCase.apply(&request)

			tool := NewCropTool()
			plan, planErr := tool.Plan(context.Background(), request)
			if planErr != nil {
				t.Fatalf("expected plan to succeed, got %v", planErr)
			}
			if plan.Outputs[0].OutputPath != testCase.want {
				t.Fatalf("expected output %q, got %q", testCase.want, plan.Outputs[0].OutputPath)
			}
		})
	}
}

func TestCropPlanSingleExplicitOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)
	custom := filepath.Join(dir, "custom.png")

	tool := NewCropTool()
	options := cropOptions(0, 0, 20, 20)
	options["outputPath"] = custom

	plan, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, options))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if plan.Outputs[0].OutputPath != custom {
		t.Fatalf("expected explicit output %q, got %q", custom, plan.Outputs[0].OutputPath)
	}
}

func TestCropPlanSingleExplicitOutputConflict(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)
	existing := filepath.Join(dir, "custom.png")
	writePlanPNG(t, existing, 10, 10)

	tool := NewCropTool()

	options := cropOptions(0, 0, 20, 20)
	options["outputPath"] = existing

	plan, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, options))
	if planErr == nil {
		t.Fatal("expected explicit output conflict")
	}
	if planErr.DetailCode != "IMAGE_CROP_OUTPUT_EXISTS" {
		t.Fatalf("expected detail code IMAGE_CROP_OUTPUT_EXISTS, got %q", planErr.DetailCode)
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

	sameAsInput := cropOptions(0, 0, 20, 20)
	sameAsInput["outputPath"] = input
	_, sameErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, sameAsInput))
	if sameErr == nil {
		t.Fatal("expected output path matching the input to fail")
	}
	if sameErr.DetailCode != "IMAGE_CROP_OUTPUT_COLLIDES_INPUT" {
		t.Fatalf("expected IMAGE_CROP_OUTPUT_COLLIDES_INPUT, got %q", sameErr.DetailCode)
	}
}

func TestCropPlanBatchWithoutOutputDirUsesEachInputDirectory(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "first.png")
	second := filepath.Join(root, "two", "second.jpg")
	writePlanPNG(t, first, 60, 40)
	writePlanFixture(t, second)

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), cropTestRequest("batch", []string{first, second}, cropOptions(0, 0, 20, 20)))
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	if want := filepath.Join(root, "one", "first_cropped.png"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(root, "two", "second_cropped.jpeg"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
	if _, exists := plan.EffectiveOptions["format"]; exists {
		t.Fatalf("expected mixed batch formats to leave the effective format unset, got %v", plan.EffectiveOptions["format"])
	}
}

func TestCropPlanBatchChosenOutputDirSuffixesDuplicateNames(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "photo.png")
	second := filepath.Join(root, "two", "photo.png")
	chosen := filepath.Join(root, "out")
	writePlanPNG(t, first, 60, 40)
	writePlanPNG(t, second, 60, 40)
	if err := os.MkdirAll(chosen, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	request := cropTestRequest("batch", []string{first, second}, cropOptions(0, 0, 20, 20))
	request.OutputDir = chosen

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	if want := filepath.Join(chosen, "photo_cropped.png"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(chosen, "photo_cropped-2.png"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
	if !plan.Outputs[1].Adjusted {
		t.Fatal("expected adjusted flag for the second colliding output")
	}
}

func TestCropPlanRejectsInvalidRequests(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	cases := []struct {
		name       string
		mode       string
		inputPaths []string
		options    map[string]any
		wantCode   string
	}{
		{
			name:       "missing width",
			mode:       "single",
			inputPaths: []string{input},
			options:    map[string]any{"x": 0, "y": 0, "height": 10},
			wantCode:   "IMAGE_CROP_OPTION_REQUIRED",
		},
		{
			name:       "ratio preset does not match",
			mode:       "single",
			inputPaths: []string{input},
			options:    map[string]any{"x": 0, "y": 0, "width": 40, "height": 30, "ratioPreset": "1:1"},
			wantCode:   "IMAGE_CROP_RATIO_INVALID",
		},
		{
			name:       "unsupported ratio preset",
			mode:       "single",
			inputPaths: []string{input},
			options:    map[string]any{"x": 0, "y": 0, "width": 40, "height": 30, "ratioPreset": "3:2"},
			wantCode:   "IMAGE_CROP_RATIO_INVALID",
		},
		{
			name:       "crop outside image",
			mode:       "single",
			inputPaths: []string{input},
			options:    cropOptions(100, 80, 40, 30),
			wantCode:   "IMAGE_CROP_OUT_OF_BOUNDS",
		},
		{
			name:       "unsupported output format",
			mode:       "single",
			inputPaths: []string{input},
			options:    map[string]any{"x": 0, "y": 0, "width": 10, "height": 10, "format": "bmp"},
			wantCode:   "IMAGE_CROP_FORMAT_UNSUPPORTED",
		},
		{
			name:       "missing input file",
			mode:       "single",
			inputPaths: []string{filepath.Join(dir, "missing.png")},
			options:    cropOptions(0, 0, 10, 10),
			wantCode:   "IMAGE_CROP_INPUT_NOT_FOUND",
		},
		{
			name:       "unsupported input extension",
			mode:       "single",
			inputPaths: []string{filepath.Join(dir, "notes.txt")},
			options:    cropOptions(0, 0, 10, 10),
			wantCode:   "IMAGE_CROP_INPUT_UNSUPPORTED",
		},
		{
			name:       "invalid mode",
			mode:       "everything",
			inputPaths: []string{input},
			options:    cropOptions(0, 0, 10, 10),
			wantCode:   "IMAGE_CROP_MODE_INVALID",
		},
		{
			name:       "single mode with two inputs",
			mode:       "single",
			inputPaths: []string{input, input},
			options:    cropOptions(0, 0, 10, 10),
			wantCode:   "IMAGE_CROP_SINGLE_INPUT_COUNT",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if testCase.wantCode == "IMAGE_CROP_INPUT_UNSUPPORTED" {
				writePlanFixture(t, filepath.Join(dir, "notes.txt"))
			}

			tool := NewCropTool()
			plan, planErr := tool.Plan(context.Background(), cropTestRequest(testCase.mode, testCase.inputPaths, testCase.options))
			if planErr == nil {
				t.Fatalf("expected plan to fail with %s", testCase.wantCode)
			}
			if planErr.DetailCode != testCase.wantCode {
				t.Fatalf("expected detail code %q, got %q (%s)", testCase.wantCode, planErr.DetailCode, planErr.Message)
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

func TestCropExecutionUsesPlannedSinglePath(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	tool := NewCropTool()
	request := cropTestRequest("single", []string{input}, cropOptions(10, 10, 40, 30))
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	plannedPath := plan.Outputs[0].OutputPath

	item, execErr := tool.ExecuteSingle(context.Background(), request)
	if execErr != nil {
		t.Fatalf("expected crop to succeed, got %v", execErr)
	}
	if !item.Success {
		t.Fatalf("expected success item, got %+v", item)
	}
	if item.OutputPath != plannedPath {
		t.Fatalf("expected execution to write %q, got %q", plannedPath, item.OutputPath)
	}

	file, openErr := os.Open(plannedPath)
	if openErr != nil {
		t.Fatalf("open cropped output: %v", openErr)
	}
	defer file.Close()

	config, decodeErr := png.DecodeConfig(file)
	if decodeErr != nil {
		t.Fatalf("decode cropped output: %v", decodeErr)
	}
	if config.Width != 40 || config.Height != 30 {
		t.Fatalf("expected 40x30 output, got %dx%d", config.Width, config.Height)
	}
	if _, statErr := os.Stat(input); statErr != nil {
		t.Fatalf("expected the input to stay untouched: %v", statErr)
	}
}

func TestCropExecutionReportsBatchItemFailures(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.png")
	broken := filepath.Join(dir, "broken.png")
	writePlanPNG(t, valid, 120, 90)
	if err := os.WriteFile(broken, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write broken fixture: %v", err)
	}

	tool := NewCropTool()
	request := cropTestRequest("batch", []string{valid, broken}, cropOptions(0, 0, 20, 20))
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	var progress []models.JobProgressV1
	items, execErr := tool.ExecuteBatch(context.Background(), request, func(event models.JobProgressV1) {
		progress = append(progress, event)
	})
	if execErr == nil {
		t.Fatal("expected a first item error for the broken image")
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 result items, got %d", len(items))
	}
	if !items[0].Success || items[0].OutputPath != plan.Outputs[0].OutputPath {
		t.Fatalf("expected the first item to succeed at the planned path, got %+v", items[0])
	}
	if items[1].Success {
		t.Fatalf("expected the broken image to fail, got %+v", items[1])
	}
	if len(progress) != 2 || progress[1].Current != 2 || progress[1].Total != 2 {
		t.Fatalf("expected progress for both items, got %+v", progress)
	}
}

func TestCropPlanBatchValidatesEveryInputBounds(t *testing.T) {
	dir := t.TempDir()
	large := filepath.Join(dir, "large.png")
	small := filepath.Join(dir, "small.png")
	writePlanPNG(t, large, 120, 90)
	writePlanPNG(t, small, 40, 40)

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), cropTestRequest("batch", []string{large, small}, cropOptions(10, 10, 60, 50)))
	if planErr == nil {
		t.Fatal("expected batch plan to fail when a crop does not fit every image")
	}
	if plan.Success {
		t.Fatal("expected failure plan")
	}
	if !strings.Contains(planErr.Message, "small.png") {
		t.Fatalf("expected error to name the offending file, got %q", planErr.Message)
	}
}

func TestCropExecutePlanHonorsResolvedPlan(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	tool := NewCropTool()
	request := cropTestRequest("single", []string{input}, cropOptions(10, 10, 40, 30))

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	requestOutput := plan.Outputs[0].OutputPath

	plannedOutput := filepath.Join(dir, "resolved-plan.png")
	plan.Outputs[0].OutputPath = plannedOutput
	plan.EffectiveOptions["x"] = 0
	plan.EffectiveOptions["y"] = 0
	plan.EffectiveOptions["width"] = 20
	plan.EffectiveOptions["height"] = 10

	items, execErr := tool.ExecutePlan(context.Background(), request, plan, nil)
	if execErr != nil {
		t.Fatalf("expected planned crop to succeed, got %v", execErr)
	}
	if len(items) != 1 {
		t.Fatalf("expected 1 result item, got %d", len(items))
	}

	item := items[0]
	if !item.Success {
		t.Fatalf("expected a successful item, got %+v", item)
	}
	if item.OutputPath != plannedOutput {
		t.Fatalf("expected the planned output %q, got %q", plannedOutput, item.OutputPath)
	}
	if _, statErr := os.Stat(requestOutput); !os.IsNotExist(statErr) {
		t.Fatalf("expected the request-resolved output %q to be unused, got %v", requestOutput, statErr)
	}

	file, openErr := os.Open(plannedOutput)
	if openErr != nil {
		t.Fatalf("open planned output: %v", openErr)
	}
	defer file.Close()

	config, decodeErr := png.DecodeConfig(file)
	if decodeErr != nil {
		t.Fatalf("decode planned output: %v", decodeErr)
	}
	if config.Width != 20 || config.Height != 10 {
		t.Fatalf("expected the planned effective crop 20x10, got %dx%d", config.Width, config.Height)
	}
}

func TestCropExecutePlanBatchSubset(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writePlanPNG(t, first, 120, 90)
	writePlanPNG(t, second, 120, 90)

	tool := NewCropTool()
	request := cropTestRequest("batch", []string{first, second}, cropOptions(0, 0, 20, 20))

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 planned outputs, got %d", len(plan.Outputs))
	}

	subset := plan
	subset.Outputs = plan.Outputs[1:]

	items, execErr := tool.ExecutePlan(context.Background(), request, subset, nil)
	if execErr != nil {
		t.Fatalf("expected the subset to succeed, got %v", execErr)
	}
	if len(items) != 1 {
		t.Fatalf("expected only the planned subset item, got %+v", items)
	}
	if items[0].InputPath != second || items[0].OutputPath != plan.Outputs[1].OutputPath {
		t.Fatalf("expected the second planned output, got %+v", items[0])
	}
	if _, statErr := os.Stat(items[0].OutputPath); statErr != nil {
		t.Fatalf("expected the subset output to exist: %v", statErr)
	}
	if _, statErr := os.Stat(plan.Outputs[0].OutputPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected the excluded output %q to stay untouched, got %v", plan.Outputs[0].OutputPath, statErr)
	}
}

func TestCropExecutePlanPreservesOrderAndItemFailures(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.png")
	broken := filepath.Join(dir, "broken.png")
	writePlanPNG(t, valid, 120, 90)
	if err := os.WriteFile(broken, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write broken fixture: %v", err)
	}

	tool := NewCropTool()
	request := cropTestRequest("batch", []string{valid, broken}, cropOptions(0, 0, 20, 20))

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	var progress []int
	items, execErr := tool.ExecutePlan(context.Background(), request, plan, func(event models.JobProgressV1) {
		progress = append(progress, event.Current)
	})
	if execErr == nil {
		t.Fatal("expected the first item error")
	}
	if execErr.DetailCode != "IMAGE_CROP_BATCH_ITEM" {
		t.Fatalf("expected IMAGE_CROP_BATCH_ITEM, got %q", execErr.DetailCode)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 result items, got %d", len(items))
	}
	if !items[0].Success || items[0].InputPath != valid || items[0].OutputPath != plan.Outputs[0].OutputPath {
		t.Fatalf("expected the first planned item to succeed, got %+v", items[0])
	}
	if items[1].Success || items[1].Error == nil || items[1].InputPath != broken {
		t.Fatalf("expected the broken planned item to fail, got %+v", items[1])
	}
	if len(progress) != 2 || progress[0] != 1 || progress[1] != 2 {
		t.Fatalf("expected monotonic plan-order progress, got %v", progress)
	}
}
