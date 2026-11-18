package image

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/gif"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"fileforge-desktop/internal/models"

	"github.com/h2non/bimg"
	"golang.org/x/image/bmp"
)

func compressTestRequest(mode string, inputPaths []string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     ToolIDImageCompressV1,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

func compressTargetOptions(targetSizeBytes int64) map[string]any {
	return map[string]any{"targetSizeBytes": targetSizeBytes}
}

func writeCompressTransparentGIF(t *testing.T, path string) {
	t.Helper()

	palette := color.Palette{color.NRGBA{R: 30, G: 160, B: 220, A: 255}, color.NRGBA{}}
	img := image.NewPaletted(image.Rect(0, 0, 40, 30), palette)
	for x := 0; x < 20; x++ {
		img.SetColorIndex(x, 0, 1)
	}

	var buffer bytes.Buffer
	if err := gif.Encode(&buffer, img, nil); err != nil {
		t.Fatalf("encode transparent gif: %v", err)
	}
	if err := writeCompressBytes(path, buffer.Bytes()); err != nil {
		t.Fatalf("write transparent gif %s: %v", path, err)
	}
}

func writeCompressBMP(t *testing.T, path string, width, height int) {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 96, A: 255})
		}
	}

	var buffer bytes.Buffer
	if err := bmp.Encode(&buffer, img); err != nil {
		t.Fatalf("encode bmp fixture: %v", err)
	}
	if err := writeCompressBytes(path, buffer.Bytes()); err != nil {
		t.Fatalf("write bmp fixture %s: %v", path, err)
	}
}

func TestCompressToolManifest(t *testing.T) {
	tool := NewCompressTool()
	manifest := tool.Manifest()

	if manifest.ToolID != "tool.image.compress" {
		t.Fatalf("expected tool.image.compress, got %q", manifest.ToolID)
	}
	if manifest.Name != "Image Compressor" {
		t.Fatalf("expected Image Compressor, got %q", manifest.Name)
	}
	if manifest.Description == "" {
		t.Fatal("expected a description")
	}
	if manifest.Domain != "image" {
		t.Fatalf("expected domain image, got %q", manifest.Domain)
	}
	if manifest.Capability != "image.compress" {
		t.Fatalf("expected capability image.compress, got %q", manifest.Capability)
	}
	if manifest.Version != "1.0.0" {
		t.Fatalf("expected version 1.0.0, got %q", manifest.Version)
	}
	if !manifest.SupportsSingle || !manifest.SupportsBatch {
		t.Fatalf("expected single and batch support, got %+v", manifest)
	}
	if !slices.Equal(manifest.InputExtensions, imageInputExtensions) {
		t.Fatalf("expected the shared image inputs, got %v", manifest.InputExtensions)
	}
	if !slices.Equal(manifest.OutputExtensions, []string{"jpeg", "png", "webp"}) {
		t.Fatalf("expected jpeg/png/webp outputs, got %v", manifest.OutputExtensions)
	}
	if !slices.Equal(manifest.RuntimeDeps, []string{"libvips"}) {
		t.Fatalf("expected libvips runtime dep, got %v", manifest.RuntimeDeps)
	}
	for _, tag := range []string{"image", "compress", "target-size"} {
		if !slices.Contains(manifest.Tags, tag) {
			t.Fatalf("expected tag %q in %v", tag, manifest.Tags)
		}
	}
	if tool.ID() != manifest.ToolID || tool.Capability() != manifest.Capability {
		t.Fatal("expected ID and Capability to match the manifest")
	}
}

func TestCompressPlanSingleAutoOutput(t *testing.T) {
	const target int64 = 1024 * 1024

	cases := []struct {
		name       string
		extension  string
		write      func(*testing.T, string)
		wantFormat string
		wantExt    string
	}{
		{
			name:      "jpeg keeps jpeg",
			extension: ".jpg",
			write: func(t *testing.T, path string) {
				writeCompressNoiseJPEG(t, path, 400, 300, 90)
			},
			wantFormat: "jpeg",
			wantExt:    ".jpeg",
		},
		{
			name:      "png keeps png",
			extension: ".png",
			write: func(t *testing.T, path string) {
				writeCompressNoisePNG(t, path, 500, 400)
			},
			wantFormat: "png",
			wantExt:    ".png",
		},
		{
			name:       "gif with alpha becomes png",
			extension:  ".gif",
			write:      writeCompressTransparentGIF,
			wantFormat: "png",
			wantExt:    ".png",
		},
		{
			name:      "bmp falls back to jpeg",
			extension: ".bmp",
			write: func(t *testing.T, path string) {
				writeCompressBMP(t, path, 40, 30)
			},
			wantFormat: "jpeg",
			wantExt:    ".jpeg",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			caseDir := t.TempDir()
			input := filepath.Join(caseDir, "photo"+testCase.extension)
			testCase.write(t, input)

			tool := NewCompressTool()
			plan, planErr := tool.Plan(context.Background(), compressTestRequest("single", []string{input}, compressTargetOptions(target)))
			if planErr != nil {
				t.Fatalf("expected the plan to succeed, got %v", planErr)
			}
			if !plan.Success {
				t.Fatal("expected a success plan")
			}
			if len(plan.Outputs) != 1 {
				t.Fatalf("expected 1 planned output, got %d", len(plan.Outputs))
			}

			output := plan.Outputs[0]
			wantOutput := filepath.Join(caseDir, "photo_compressed"+testCase.wantExt)
			if output.OutputPath != wantOutput {
				t.Fatalf("expected output %q, got %q", wantOutput, output.OutputPath)
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
			if _, statErr := os.Stat(output.OutputPath); !os.IsNotExist(statErr) {
				t.Fatal("plan must not create the output file")
			}

			if got := plan.EffectiveOptions["format"]; got != testCase.wantFormat {
				t.Fatalf("expected effective format %q, got %v", testCase.wantFormat, got)
			}
			if got := plan.EffectiveOptions["targetSizeBytes"]; got != target {
				t.Fatalf("expected effective target %d, got %v (%T)", target, got, got)
			}
			if got := plan.EffectiveOptions["allowDownscale"]; got != true {
				t.Fatalf("expected allowDownscale default true, got %v", got)
			}
			if got := plan.EffectiveOptions["stripMetadata"]; got != true {
				t.Fatalf("expected stripMetadata default true, got %v", got)
			}
			if _, exists := plan.EffectiveOptions["maxDimension"]; exists {
				t.Fatalf("expected no effective maxDimension, got %v", plan.EffectiveOptions["maxDimension"])
			}
		})
	}
}

func TestCompressPlanSingleExplicitOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeCompressNoisePNG(t, input, 120, 90)
	custom := filepath.Join(dir, "custom.png")

	options := compressTargetOptions(1024 * 1024)
	options["outputPath"] = custom

	tool := NewCompressTool()
	plan, planErr := tool.Plan(context.Background(), compressTestRequest("single", []string{input}, options))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if plan.Outputs[0].OutputPath != custom {
		t.Fatalf("expected explicit output %q, got %q", custom, plan.Outputs[0].OutputPath)
	}

	existing := filepath.Join(dir, "existing.png")
	writePlanPNG(t, existing, 10, 10)
	conflictOptions := compressTargetOptions(1024 * 1024)
	conflictOptions["outputPath"] = existing

	conflictPlan, conflictErr := tool.Plan(context.Background(), compressTestRequest("single", []string{input}, conflictOptions))
	if conflictErr == nil {
		t.Fatal("expected an existing explicit output to fail")
	}
	if conflictErr.DetailCode != "IMAGE_COMPRESS_OUTPUT_EXISTS" {
		t.Fatalf("expected IMAGE_COMPRESS_OUTPUT_EXISTS, got %q", conflictErr.DetailCode)
	}
	if !strings.Contains(conflictErr.Message, "existing.png") {
		t.Fatalf("expected the message to name the file, got %q", conflictErr.Message)
	}
	if conflictPlan.Success {
		t.Fatal("expected the plan to report failure")
	}
	if len(conflictPlan.Outputs) != 1 || !conflictPlan.Outputs[0].Exists {
		t.Fatal("expected the conflicting output to be reported as existing")
	}

	sameOptions := compressTargetOptions(1024 * 1024)
	sameOptions["outputPath"] = input
	_, sameErr := tool.Plan(context.Background(), compressTestRequest("single", []string{input}, sameOptions))
	if sameErr == nil {
		t.Fatal("expected an output matching the input to fail")
	}
	if sameErr.DetailCode != "IMAGE_COMPRESS_OUTPUT_COLLIDES_INPUT" {
		t.Fatalf("expected IMAGE_COMPRESS_OUTPUT_COLLIDES_INPUT, got %q", sameErr.DetailCode)
	}
}

func TestCompressPlanSuffixesExistingAutoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writeCompressNoisePNG(t, input, 120, 90)
	writePlanPNG(t, filepath.Join(dir, "photo_compressed.png"), 10, 10)

	tool := NewCompressTool()
	plan, planErr := tool.Plan(context.Background(), compressTestRequest("single", []string{input}, compressTargetOptions(1024*1024)))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "photo_compressed-2.png"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected the adjusted flag")
	}
	if len(output.Warnings) == 0 || len(plan.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}
}

func TestCompressPlanBatchCollisionsInChosenOutputDir(t *testing.T) {
	root := t.TempDir()
	first := filepath.Join(root, "one", "photo.png")
	second := filepath.Join(root, "two", "photo.png")
	chosen := filepath.Join(root, "out")
	writeCompressNoisePNG(t, first, 120, 90)
	writeCompressNoisePNG(t, second, 120, 90)
	if err := os.MkdirAll(chosen, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	request := compressTestRequest("batch", []string{first, second}, compressTargetOptions(1024*1024))
	request.OutputDir = chosen

	tool := NewCompressTool()
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(plan.Outputs))
	}
	if want := filepath.Join(chosen, "photo_compressed.png"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected first output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(chosen, "photo_compressed-2.png"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected second output %q, got %q", want, plan.Outputs[1].OutputPath)
	}
	if !plan.Outputs[1].Adjusted {
		t.Fatal("expected the second collision to be adjusted")
	}
}

func TestCompressPlanRejectsInvalidRequests(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "photo.png")
	writePlanPNG(t, valid, 120, 90)
	unsupported := filepath.Join(dir, "notes.txt")
	writePlanFixture(t, unsupported)
	missing := filepath.Join(dir, "missing.png")

	cases := []struct {
		name    string
		mode    string
		inputs  []string
		options map[string]any
		want    string
	}{
		{
			name:    "missing target",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{},
			want:    "IMAGE_COMPRESS_TARGET_REQUIRED",
		},
		{
			name:    "nil options",
			mode:    "single",
			inputs:  []string{valid},
			options: nil,
			want:    "IMAGE_COMPRESS_TARGET_REQUIRED",
		},
		{
			name:    "zero target",
			mode:    "single",
			inputs:  []string{valid},
			options: compressTargetOptions(0),
			want:    "IMAGE_COMPRESS_TARGET_INVALID",
		},
		{
			name:    "negative target",
			mode:    "single",
			inputs:  []string{valid},
			options: compressTargetOptions(-10),
			want:    "IMAGE_COMPRESS_TARGET_INVALID",
		},
		{
			name:    "fractional target",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1.5},
			want:    "IMAGE_COMPRESS_TARGET_INVALID",
		},
		{
			name:    "textual garbage target",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": "big"},
			want:    "IMAGE_COMPRESS_TARGET_INVALID",
		},
		{
			name:    "boolean target",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": true},
			want:    "IMAGE_COMPRESS_TARGET_INVALID",
		},
		{
			name:    "unsupported format",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1024, "format": "gif"},
			want:    "IMAGE_COMPRESS_FORMAT_INVALID",
		},
		{
			name:    "non-string format",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1024, "format": 3},
			want:    "IMAGE_COMPRESS_FORMAT_INVALID",
		},
		{
			name:    "quality below range",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1024, "quality": 20},
			want:    "IMAGE_COMPRESS_OPTION_INVALID",
		},
		{
			name:    "quality above range",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1024, "quality": 101},
			want:    "IMAGE_COMPRESS_OPTION_INVALID",
		},
		{
			name:    "fractional quality",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1024, "quality": 40.5},
			want:    "IMAGE_COMPRESS_OPTION_INVALID",
		},
		{
			name:    "zero max dimension",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1024, "maxDimension": 0},
			want:    "IMAGE_COMPRESS_OPTION_INVALID",
		},
		{
			name:    "negative max dimension",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1024, "maxDimension": -5},
			want:    "IMAGE_COMPRESS_OPTION_INVALID",
		},
		{
			name:    "textual max dimension",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1024, "maxDimension": "big"},
			want:    "IMAGE_COMPRESS_OPTION_INVALID",
		},
		{
			name:    "invalid allow downscale",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1024, "allowDownscale": "maybe"},
			want:    "IMAGE_COMPRESS_OPTION_INVALID",
		},
		{
			name:    "invalid strip metadata",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1024, "stripMetadata": 3},
			want:    "IMAGE_COMPRESS_OPTION_INVALID",
		},
		{
			name:    "invalid background",
			mode:    "single",
			inputs:  []string{valid},
			options: map[string]any{"targetSizeBytes": 1024, "background": "white"},
			want:    "IMAGE_COMPRESS_BACKGROUND_INVALID",
		},
		{
			name:    "invalid mode",
			mode:    "everything",
			inputs:  []string{valid},
			options: compressTargetOptions(1024),
			want:    "IMAGE_COMPRESS_MODE_INVALID",
		},
		{
			name:    "single with two inputs",
			mode:    "single",
			inputs:  []string{valid, valid},
			options: compressTargetOptions(1024),
			want:    "IMAGE_COMPRESS_SINGLE_INPUT_COUNT",
		},
		{
			name:    "no inputs",
			mode:    "batch",
			inputs:  []string{},
			options: compressTargetOptions(1024),
			want:    "IMAGE_COMPRESS_INPUT_REQUIRED",
		},
		{
			name:    "empty input path",
			mode:    "batch",
			inputs:  []string{"  "},
			options: compressTargetOptions(1024),
			want:    "IMAGE_COMPRESS_INPUT_REQUIRED",
		},
		{
			name:    "missing input file",
			mode:    "single",
			inputs:  []string{missing},
			options: compressTargetOptions(1024),
			want:    "IMAGE_COMPRESS_INPUT_NOT_FOUND",
		},
		{
			name:    "unsupported input extension",
			mode:    "single",
			inputs:  []string{unsupported},
			options: compressTargetOptions(1024),
			want:    "IMAGE_COMPRESS_INPUT_UNSUPPORTED",
		},
	}

	tool := NewCompressTool()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan, planErr := tool.Plan(context.Background(), compressTestRequest(testCase.mode, testCase.inputs, testCase.options))
			if planErr == nil {
				t.Fatalf("expected plan failure with %s", testCase.want)
			}
			if planErr.DetailCode != testCase.want {
				t.Fatalf("expected detail code %q, got %q (%s)", testCase.want, planErr.DetailCode, planErr.Message)
			}
			if !strings.HasPrefix(planErr.DetailCode, "IMAGE_COMPRESS_") {
				t.Fatalf("expected the IMAGE_COMPRESS_ prefix, got %q", planErr.DetailCode)
			}
			if plan.Success {
				t.Fatal("expected the plan to report failure")
			}
			if plan.Error == nil {
				t.Fatal("expected the plan error to be attached")
			}
		})
	}
}

func TestCompressPlanBatchRejectsMissingOutputDir(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	request := compressTestRequest("batch", []string{input}, compressTargetOptions(1024))
	request.OutputDir = filepath.Join(dir, "missing-dir")

	tool := NewCompressTool()
	_, planErr := tool.Plan(context.Background(), request)
	if planErr == nil {
		t.Fatal("expected a missing output dir to fail")
	}
	if planErr.DetailCode != "IMAGE_COMPRESS_OUTPUT_DIR_INVALID" {
		t.Fatalf("expected IMAGE_COMPRESS_OUTPUT_DIR_INVALID, got %q", planErr.DetailCode)
	}
}

func TestCompressExecutionSingleWritesPlannedOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.jpg")
	writeCompressNoiseJPEG(t, input, 400, 300, 90)
	before, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("read input: %v", err)
	}

	const target int64 = 40 * 1024
	tool := NewCompressTool()
	request := compressTestRequest("single", []string{input}, compressTargetOptions(target))

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	plannedPath := plan.Outputs[0].OutputPath

	item, execErr := tool.ExecuteSingle(context.Background(), request)
	if execErr != nil {
		t.Fatalf("expected the compression to succeed, got %v", execErr)
	}
	if !item.Success {
		t.Fatalf("expected a success item, got %+v", item)
	}
	if item.OutputPath != plannedPath {
		t.Fatalf("expected execution to write %q, got %q", plannedPath, item.OutputPath)
	}
	if item.OutputCount != 1 || len(item.Outputs) != 1 || item.Outputs[0] != plannedPath {
		t.Fatalf("expected one declared output, got %+v", item)
	}
	if !strings.Contains(item.Message, "→") {
		t.Fatalf("expected a size arrow in the message, got %q", item.Message)
	}
	if !strings.Contains(item.Message, "target 40 KB") {
		t.Fatalf("expected the target in the message, got %q", item.Message)
	}
	if strings.Contains(item.Message, "target not reached") {
		t.Fatalf("expected a reached target, got %q", item.Message)
	}
	if strings.Contains(item.Message, "already under target") {
		t.Fatalf("expected a real encode, got %q", item.Message)
	}

	info, statErr := os.Stat(plannedPath)
	if statErr != nil {
		t.Fatalf("expected the output file: %v", statErr)
	}
	if info.Size() > target {
		t.Fatalf("expected output <= %d bytes, got %d", target, info.Size())
	}

	size, sizeErr := bimg.NewImage(readCompressFile(t, plannedPath)).Size()
	if sizeErr != nil {
		t.Fatalf("read output size: %v", sizeErr)
	}
	if size.Width != 400 || size.Height != 300 {
		t.Fatalf("expected 400x300 output, got %dx%d", size.Width, size.Height)
	}

	after, err := os.ReadFile(input)
	if err != nil {
		t.Fatalf("read input after execution: %v", err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("expected the input to stay untouched")
	}
}

func TestCompressExecutionSingleBestEffortMessage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.jpg")
	writeCompressNoiseJPEG(t, input, 200, 150, 90)

	tool := NewCompressTool()
	request := compressTestRequest("single", []string{input}, map[string]any{
		"targetSizeBytes": 1024,
		"allowDownscale":  false,
	})

	item, execErr := tool.ExecuteSingle(context.Background(), request)
	if execErr != nil {
		t.Fatalf("expected the best effort to succeed, got %v", execErr)
	}
	if !item.Success {
		t.Fatalf("expected a success item, got %+v", item)
	}
	if !strings.Contains(item.Message, "target not reached (best effort)") {
		t.Fatalf("expected an honest best-effort warning, got %q", item.Message)
	}
}

func TestCompressExecutionSingleAlreadyUnderTargetMessage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	tool := NewCompressTool()
	request := compressTestRequest("single", []string{input}, compressTargetOptions(1024*1024))

	item, execErr := tool.ExecuteSingle(context.Background(), request)
	if execErr != nil {
		t.Fatalf("expected the compression to succeed, got %v", execErr)
	}
	if !strings.Contains(item.Message, "already under target") {
		t.Fatalf("expected the already-under-target note, got %q", item.Message)
	}

	source := readCompressFile(t, input)
	output := readCompressFile(t, item.OutputPath)
	if !bytes.Equal(source, output) {
		t.Fatal("expected the original bytes to be copied unchanged")
	}
}

func TestCompressExecutionSingleWithProgress(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	tool := NewCompressTool()
	request := compressTestRequest("single", []string{input}, compressTargetOptions(1024*1024))

	var events []models.JobProgressV1
	item, execErr := tool.ExecuteSingleWithProgress(context.Background(), request, func(event models.JobProgressV1) {
		events = append(events, event)
	})
	if execErr != nil {
		t.Fatalf("expected the compression to succeed, got %v", execErr)
	}
	if !item.Success {
		t.Fatalf("expected a success item, got %+v", item)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 progress events, got %+v", events)
	}
	if events[0].Current != 0 || events[0].Total != 1 || events[0].Message != "compressing 1/1" {
		t.Fatalf("unexpected start event: %+v", events[0])
	}
	if events[1].Current != 1 || events[1].Total != 1 || events[1].Message != "processed 1/1" {
		t.Fatalf("unexpected end event: %+v", events[1])
	}
	if events[0].Stage != models.JobStatusRunning {
		t.Fatalf("expected the running stage, got %q", events[0].Stage)
	}
}

func TestCompressExecutionBatchMixedExtensions(t *testing.T) {
	dir := t.TempDir()
	jpegPath := filepath.Join(dir, "photo.jpg")
	pngPath := filepath.Join(dir, "scan.png")
	writeCompressNoiseJPEG(t, jpegPath, 400, 300, 90)
	writeCompressNoisePNG(t, pngPath, 500, 400)

	jpegBefore := readCompressFile(t, jpegPath)

	const target int64 = 40 * 1024
	tool := NewCompressTool()
	request := compressTestRequest("batch", []string{jpegPath, pngPath}, compressTargetOptions(target))

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}
	if want := filepath.Join(dir, "photo_compressed.jpeg"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if want := filepath.Join(dir, "scan_compressed.png"); plan.Outputs[1].OutputPath != want {
		t.Fatalf("expected %q, got %q", want, plan.Outputs[1].OutputPath)
	}

	var events []models.JobProgressV1
	items, execErr := tool.ExecuteBatch(context.Background(), request, func(event models.JobProgressV1) {
		events = append(events, event)
	})
	if execErr != nil {
		t.Fatalf("expected the batch to succeed, got %v", execErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	for index, item := range items {
		if !item.Success {
			t.Fatalf("expected item %d to succeed, got %+v", index, item)
		}
		if item.OutputPath != plan.Outputs[index].OutputPath {
			t.Fatalf("expected item %d at %q, got %q", index, plan.Outputs[index].OutputPath, item.OutputPath)
		}
		if _, statErr := os.Stat(item.OutputPath); statErr != nil {
			t.Fatalf("expected item %d output to exist: %v", index, statErr)
		}
	}

	if !strings.Contains(items[0].Message, "target 40 KB") || strings.Contains(items[0].Message, "target not reached") {
		t.Fatalf("expected the jpeg target to be reached, got %q", items[0].Message)
	}
	if !strings.Contains(items[1].Message, "target not reached (best effort)") {
		t.Fatalf("expected the png best-effort warning, got %q", items[1].Message)
	}

	if len(events) != 2 {
		t.Fatalf("expected 2 progress events, got %+v", events)
	}
	if events[0].Message != "processed 1/2" || events[0].Current != 1 || events[0].Total != 2 {
		t.Fatalf("unexpected first progress event: %+v", events[0])
	}
	if events[1].Message != "processed 2/2" || events[1].Current != 2 || events[1].Total != 2 {
		t.Fatalf("unexpected second progress event: %+v", events[1])
	}

	if after := readCompressFile(t, jpegPath); !bytes.Equal(jpegBefore, after) {
		t.Fatal("expected the jpeg input to stay untouched")
	}
}

func TestCompressExecutionBatchReportsItemFailures(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.png")
	broken := filepath.Join(dir, "broken.png")
	writePlanPNG(t, valid, 40, 30)
	if err := os.WriteFile(broken, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write broken fixture: %v", err)
	}

	tool := NewCompressTool()
	request := compressTestRequest("batch", []string{valid, broken}, compressTargetOptions(1024*1024))

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to tolerate a broken image, got %v", planErr)
	}

	var events []models.JobProgressV1
	items, execErr := tool.ExecuteBatch(context.Background(), request, func(event models.JobProgressV1) {
		events = append(events, event)
	})
	if execErr == nil {
		t.Fatal("expected a first item error for the broken image")
	}
	if execErr.DetailCode != "IMAGE_COMPRESS_BATCH_ITEM" {
		t.Fatalf("expected IMAGE_COMPRESS_BATCH_ITEM, got %q", execErr.DetailCode)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 result items, got %d", len(items))
	}
	if !items[0].Success || items[0].OutputPath != plan.Outputs[0].OutputPath {
		t.Fatalf("expected the first item to succeed, got %+v", items[0])
	}
	if items[1].Success {
		t.Fatalf("expected the broken image to fail, got %+v", items[1])
	}
	if items[1].Error == nil || !strings.HasPrefix(items[1].Error.DetailCode, "IMAGE_COMPRESS_") {
		t.Fatalf("expected a prefixed item error, got %+v", items[1].Error)
	}
	if len(events) != 2 {
		t.Fatalf("expected progress for both items, got %+v", events)
	}
}

func TestCompressExecutionBatchCancellation(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writePlanPNG(t, first, 40, 30)
	writePlanPNG(t, second, 40, 30)

	tool := NewCompressTool()
	request := compressTestRequest("batch", []string{first, second}, compressTargetOptions(1024*1024))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	items, execErr := tool.ExecuteBatch(ctx, request, func(event models.JobProgressV1) {
		if event.Current == 1 {
			cancel()
		}
	})
	if execErr == nil {
		t.Fatal("expected the batch to report cancellation")
	}
	if execErr.DetailCode != "IMAGE_COMPRESS_CANCELLED" {
		t.Fatalf("expected IMAGE_COMPRESS_CANCELLED, got %q", execErr.DetailCode)
	}
	if !strings.Contains(execErr.Message, "operation cancelled") {
		t.Fatalf("expected the cancellation message, got %q", execErr.Message)
	}
	if len(items) != 1 || !items[0].Success {
		t.Fatalf("expected one successful item before cancellation, got %+v", items)
	}
}

func TestCompressExecutionSingleCancelledContext(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	tool := NewCompressTool()
	request := compressTestRequest("single", []string{input}, compressTargetOptions(1024*1024))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	item, execErr := tool.ExecuteSingle(ctx, request)
	if execErr == nil {
		t.Fatal("expected cancellation")
	}
	if execErr.DetailCode != "IMAGE_COMPRESS_CANCELLED" {
		t.Fatalf("expected IMAGE_COMPRESS_CANCELLED, got %q", execErr.DetailCode)
	}
	if !strings.Contains(execErr.Message, "operation cancelled") {
		t.Fatalf("expected the cancellation message, got %q", execErr.Message)
	}
	if item.Success {
		t.Fatalf("expected a failure item, got %+v", item)
	}
	if _, statErr := os.Stat(item.OutputPath); !os.IsNotExist(statErr) {
		t.Fatal("expected no output to be written")
	}
}

func TestCompressExecutionBatchRequiresBatchMode(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 30)

	tool := NewCompressTool()
	request := compressTestRequest("single", []string{input}, compressTargetOptions(1024))

	_, execErr := tool.ExecuteBatch(context.Background(), request, nil)
	if execErr == nil {
		t.Fatal("expected single mode to be rejected")
	}
	if execErr.DetailCode != "IMAGE_COMPRESS_MODE_INVALID" {
		t.Fatalf("expected IMAGE_COMPRESS_MODE_INVALID, got %q", execErr.DetailCode)
	}
}

func readCompressFile(t *testing.T, path string) []byte {
	t.Helper()

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}

	return data
}
