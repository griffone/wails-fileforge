package image

import (
	"context"
	"fmt"
	"image/color"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"fileforge-desktop/internal/models"

	"github.com/h2non/bimg"
)

func watermarkTestRequest(mode string, inputPaths []string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     ToolIDImageWatermarkV1,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

func watermarkRequestOptions() map[string]any {
	return map[string]any{"text": "Sample"}
}

func writeWatermarkWebP(t *testing.T, path string, width, height int) {
	t.Helper()

	source := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 80, G: 140, B: 200, A: 255})
	encoded, err := bimg.NewImage(source).Process(bimg.Options{Type: bimg.WEBP, Quality: 90})
	if err != nil {
		t.Fatalf("encode webp fixture: %v", err)
	}
	if err := os.WriteFile(path, encoded, 0o644); err != nil {
		t.Fatalf("write webp fixture %s: %v", path, err)
	}
}

func writeWatermarkLogoFile(t *testing.T, path string, width, height int) {
	t.Helper()

	if err := os.WriteFile(path, encodeWatermarkLogo(t, width, height, false), 0o644); err != nil {
		t.Fatalf("write logo fixture %s: %v", path, err)
	}
}

func TestWatermarkToolManifest(t *testing.T) {
	tool := NewWatermarkTool()
	manifest := tool.Manifest()

	if manifest.ToolID != "tool.image.watermark" {
		t.Fatalf("expected tool.image.watermark, got %q", manifest.ToolID)
	}
	if manifest.Name != "Image Watermark" {
		t.Fatalf("expected Image Watermark, got %q", manifest.Name)
	}
	if manifest.Description == "" {
		t.Fatal("expected a description")
	}
	if manifest.Domain != "image" {
		t.Fatalf("expected domain image, got %q", manifest.Domain)
	}
	if manifest.Capability != ToolIDImageWatermarkV1 {
		t.Fatalf("expected capability %q, got %q", ToolIDImageWatermarkV1, manifest.Capability)
	}
	if manifest.Version != "v1" {
		t.Fatalf("expected version v1, got %q", manifest.Version)
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
	for _, tag := range []string{"image", "watermark"} {
		if !slices.Contains(manifest.Tags, tag) {
			t.Fatalf("expected tag %q in %v", tag, manifest.Tags)
		}
	}
	if tool.ID() != manifest.ToolID || tool.Capability() != manifest.Capability {
		t.Fatal("expected ID and Capability to match the manifest")
	}
}

func TestWatermarkPlanDefaults(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 300, 240)

	tool := NewWatermarkTool()
	request := watermarkTestRequest("single", []string{input}, watermarkRequestOptions())

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if !plan.Success {
		t.Fatal("expected a success plan")
	}
	if plan.Message != "Ready to watermark 1 image." {
		t.Fatalf("unexpected plan message %q", plan.Message)
	}
	if len(plan.Outputs) != 1 {
		t.Fatalf("expected 1 planned output, got %d", len(plan.Outputs))
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "photo_watermarked.png"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.InputPath != input {
		t.Fatalf("expected input %q, got %q", input, output.InputPath)
	}
	if output.OutputKind != imageOutputKindFile {
		t.Fatalf("expected file output kind, got %q", output.OutputKind)
	}
	if output.Exists || output.Adjusted {
		t.Fatalf("expected a fresh, unadjusted output, got exists=%v adjusted=%v", output.Exists, output.Adjusted)
	}
	if _, statErr := os.Stat(output.OutputPath); !os.IsNotExist(statErr) {
		t.Fatal("plan must not create the output file")
	}

	effective := plan.EffectiveOptions
	expected := map[string]any{
		"kind":          "text",
		"text":          "Sample",
		"anchor":        "bottom-right",
		"marginPercent": 3.0,
		"scalePercent":  5.0,
		"opacity":       0.6,
		"color":         "#FFFFFF",
		"bold":          true,
		"tile":          false,
		"quality":       92,
		"format":        "png",
	}
	for key, want := range expected {
		if got := effective[key]; got != want {
			t.Fatalf("expected effective %s=%v (%T), got %v (%T)", key, want, want, got, got)
		}
	}
	if _, exists := effective["rotationDegrees"]; exists {
		t.Fatalf("expected no rotationDegrees for a non-tiled watermark, got %v", effective["rotationDegrees"])
	}
	if _, exists := effective["logoPath"]; exists {
		t.Fatalf("expected no logoPath for a text watermark, got %v", effective["logoPath"])
	}

	if validateErr := tool.Validate(context.Background(), request); validateErr != nil {
		t.Fatalf("expected the request to validate, got %v", validateErr)
	}
}

func TestWatermarkPlanLogoDefaults(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 300, 240)
	logo := filepath.Join(dir, "logo.png")
	writeWatermarkLogoFile(t, logo, 60, 30)

	tool := NewWatermarkTool()
	options := map[string]any{"kind": "logo", "logoPath": logo}
	plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, options))
	if planErr != nil {
		t.Fatalf("expected the logo plan to succeed, got %v", planErr)
	}

	effective := plan.EffectiveOptions
	if effective["kind"] != "logo" {
		t.Fatalf("expected logo kind, got %v", effective["kind"])
	}
	if effective["scalePercent"] != 20.0 {
		t.Fatalf("expected the logo scale default 20, got %v", effective["scalePercent"])
	}
	if effective["logoPath"] != logo {
		t.Fatalf("expected the logo path in the effective options, got %v", effective["logoPath"])
	}
	if _, exists := effective["text"]; exists {
		t.Fatalf("expected no text for a logo watermark, got %v", effective["text"])
	}
	for _, key := range []string{"color", "bold", "rotationDegrees"} {
		if value, exists := effective[key]; exists {
			t.Fatalf("expected no %s for a logo watermark, got %v", key, value)
		}
	}

	if want := filepath.Join(dir, "photo_watermarked.png"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
}

func TestWatermarkPlanTiledEffectiveOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 300, 240)

	tool := NewWatermarkTool()
	options := watermarkRequestOptions()
	options["tile"] = true
	options["rotationDegrees"] = -45

	plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, options))
	if planErr != nil {
		t.Fatalf("expected the tiled plan to succeed, got %v", planErr)
	}
	if got := plan.EffectiveOptions["rotationDegrees"]; got != -45.0 {
		t.Fatalf("expected the tile rotation -45, got %v", got)
	}
	if got := plan.EffectiveOptions["tile"]; got != true {
		t.Fatalf("expected tile true, got %v", got)
	}
	if got := plan.EffectiveOptions["color"]; got != "#FFFFFF" {
		t.Fatalf("expected the text color, got %v", got)
	}
}

func TestWatermarkPlanAutoFormats(t *testing.T) {
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
				writeCompressNoiseJPEG(t, path, 200, 150, 90)
			},
			wantFormat: "jpeg",
			wantExt:    ".jpeg",
		},
		{
			name:      "png keeps png",
			extension: ".png",
			write: func(t *testing.T, path string) {
				writePlanPNG(t, path, 200, 150)
			},
			wantFormat: "png",
			wantExt:    ".png",
		},
		{
			name:      "webp keeps webp",
			extension: ".webp",
			write: func(t *testing.T, path string) {
				writeWatermarkWebP(t, path, 200, 150)
			},
			wantFormat: "webp",
			wantExt:    ".webp",
		},
		{
			name:       "transparent gif becomes png",
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
		{
			name:       "gray tiff becomes jpeg",
			extension:  ".tif",
			write:      writeCompressGrayTIFF,
			wantFormat: "jpeg",
			wantExt:    ".jpeg",
		},
		{
			name:       "alpha tiff becomes png",
			extension:  ".tiff",
			write:      writeCompressAlphaTIFF,
			wantFormat: "png",
			wantExt:    ".png",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			caseDir := t.TempDir()
			input := filepath.Join(caseDir, "photo"+testCase.extension)
			testCase.write(t, input)

			tool := NewWatermarkTool()
			plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, map[string]any{"text": "S"}))
			if planErr != nil {
				t.Fatalf("expected the plan to succeed, got %v", planErr)
			}

			wantOutput := filepath.Join(caseDir, "photo_watermarked"+testCase.wantExt)
			if plan.Outputs[0].OutputPath != wantOutput {
				t.Fatalf("expected output %q, got %q", wantOutput, plan.Outputs[0].OutputPath)
			}
			if got := plan.EffectiveOptions["format"]; got != testCase.wantFormat {
				t.Fatalf("expected effective format %q, got %v", testCase.wantFormat, got)
			}
		})
	}
}

func TestWatermarkPlanRejectsInvalidRequests(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "photo.png")
	writePlanPNG(t, valid, 120, 90)
	second := filepath.Join(dir, "second.png")
	writePlanPNG(t, second, 120, 90)
	unsupported := filepath.Join(dir, "notes.txt")
	writePlanFixture(t, unsupported)
	missing := filepath.Join(dir, "missing.png")
	outputFile := filepath.Join(dir, "out.txt")
	writePlanFixture(t, outputFile)
	missingDir := filepath.Join(dir, "missing-dir")

	cases := []struct {
		name      string
		mode      string
		inputs    []string
		outputDir string
		options   map[string]any
		want      string
	}{
		{
			name:    "invalid mode",
			mode:    "many",
			inputs:  []string{valid},
			options: watermarkRequestOptions(),
			want:    "IMAGE_WATERMARK_MODE_INVALID",
		},
		{
			name:    "no inputs",
			mode:    "single",
			inputs:  nil,
			options: watermarkRequestOptions(),
			want:    "IMAGE_WATERMARK_INPUT_REQUIRED",
		},
		{
			name:    "single with two inputs",
			mode:    "single",
			inputs:  []string{valid, second},
			options: watermarkRequestOptions(),
			want:    "IMAGE_WATERMARK_SINGLE_INPUT_COUNT",
		},
		{
			name:    "missing input",
			mode:    "single",
			inputs:  []string{missing},
			options: watermarkRequestOptions(),
			want:    "IMAGE_WATERMARK_INPUT_NOT_FOUND",
		},
		{
			name:    "input folder",
			mode:    "single",
			inputs:  []string{dir},
			options: watermarkRequestOptions(),
			want:    "IMAGE_WATERMARK_INPUT_UNSUPPORTED",
		},
		{
			name:    "unsupported input extension",
			mode:    "single",
			inputs:  []string{unsupported},
			options: watermarkRequestOptions(),
			want:    "IMAGE_WATERMARK_INPUT_UNSUPPORTED",
		},
		{
			name:      "batch missing output dir",
			mode:      "batch",
			inputs:    []string{valid},
			outputDir: missingDir,
			options:   watermarkRequestOptions(),
			want:      "IMAGE_WATERMARK_OUTPUT_DIR_INVALID",
		},
		{
			name:      "batch output dir is a file",
			mode:      "batch",
			inputs:    []string{valid},
			outputDir: outputFile,
			options:   watermarkRequestOptions(),
			want:      "IMAGE_WATERMARK_OUTPUT_DIR_INVALID",
		},
		{
			name:      "single missing output dir",
			mode:      "single",
			inputs:    []string{valid},
			outputDir: missingDir,
			options:   watermarkRequestOptions(),
			want:      "IMAGE_WATERMARK_OUTPUT_DIR_INVALID",
		},
		{
			name:      "single output dir is a file",
			mode:      "single",
			inputs:    []string{valid},
			outputDir: outputFile,
			options:   watermarkRequestOptions(),
			want:      "IMAGE_WATERMARK_OUTPUT_DIR_INVALID",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			request := watermarkTestRequest(testCase.mode, testCase.inputs, testCase.options)
			request.OutputDir = testCase.outputDir

			tool := NewWatermarkTool()
			plan, planErr := tool.Plan(context.Background(), request)
			if planErr == nil {
				t.Fatalf("expected %s", testCase.want)
			}
			if planErr.DetailCode != testCase.want {
				t.Fatalf("expected %s, got %s (%s)", testCase.want, planErr.DetailCode, planErr.Message)
			}
			if plan.Success {
				t.Fatal("expected the plan to report failure")
			}
		})
	}
}

func TestWatermarkPlanRejectsInvalidOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 200, 150)
	logo := filepath.Join(dir, "logo.png")
	writeWatermarkLogoFile(t, logo, 40, 40)
	textFile := filepath.Join(dir, "notes.txt")
	writePlanFixture(t, textFile)
	corruptLogo := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(corruptLogo, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write broken logo: %v", err)
	}

	cases := []struct {
		name    string
		options map[string]any
		want    string
	}{
		{
			name:    "invalid kind",
			options: map[string]any{"kind": "sticker", "text": "x"},
			want:    "IMAGE_WATERMARK_KIND_INVALID",
		},
		{
			name:    "text required",
			options: map[string]any{"kind": "text"},
			want:    "IMAGE_WATERMARK_TEXT_REQUIRED",
		},
		{
			name:    "blank text",
			options: map[string]any{"text": "   "},
			want:    "IMAGE_WATERMARK_TEXT_REQUIRED",
		},
		{
			name:    "text too long",
			options: map[string]any{"text": strings.Repeat("a", 201)},
			want:    "IMAGE_WATERMARK_TEXT_TOO_LONG",
		},
		{
			name:    "text too many lines",
			options: map[string]any{"text": strings.Repeat("a\n", 10) + "z"},
			want:    "IMAGE_WATERMARK_TEXT_TOO_MANY_LINES",
		},
		{
			name:    "text control chars",
			options: map[string]any{"text": "a\x01b"},
			want:    "IMAGE_WATERMARK_TEXT_CONTROL_CHARS",
		},
		{
			name:    "text wrong type",
			options: map[string]any{"text": 42},
			want:    "IMAGE_WATERMARK_OPTION_INVALID",
		},
		{
			name:    "logo path required",
			options: map[string]any{"kind": "logo"},
			want:    "IMAGE_WATERMARK_LOGO_REQUIRED",
		},
		{
			name:    "logo missing",
			options: map[string]any{"kind": "logo", "logoPath": filepath.Join(dir, "missing.png")},
			want:    "IMAGE_WATERMARK_LOGO_NOT_FOUND",
		},
		{
			name:    "logo is a folder",
			options: map[string]any{"kind": "logo", "logoPath": dir},
			want:    "IMAGE_WATERMARK_LOGO_UNSUPPORTED",
		},
		{
			name:    "logo wrong extension",
			options: map[string]any{"kind": "logo", "logoPath": textFile},
			want:    "IMAGE_WATERMARK_LOGO_UNSUPPORTED",
		},
		{
			name:    "logo unreadable image",
			options: map[string]any{"kind": "logo", "logoPath": corruptLogo},
			want:    "IMAGE_WATERMARK_LOGO_INVALID",
		},
		{
			name:    "tile with logo",
			options: map[string]any{"kind": "logo", "logoPath": logo, "tile": true},
			want:    "IMAGE_WATERMARK_TILE_UNSUPPORTED",
		},
		{
			name:    "invalid anchor",
			options: map[string]any{"text": "x", "anchor": "middle"},
			want:    "IMAGE_WATERMARK_ANCHOR_INVALID",
		},
		{
			name:    "margin below range",
			options: map[string]any{"text": "x", "marginPercent": 1.5},
			want:    "IMAGE_WATERMARK_MARGIN_INVALID",
		},
		{
			name:    "margin above range",
			options: map[string]any{"text": "x", "marginPercent": 5.5},
			want:    "IMAGE_WATERMARK_MARGIN_INVALID",
		},
		{
			name:    "margin not numeric",
			options: map[string]any{"text": "x", "marginPercent": "3"},
			want:    "IMAGE_WATERMARK_MARGIN_INVALID",
		},
		{
			name:    "text scale below range",
			options: map[string]any{"text": "x", "scalePercent": 2},
			want:    "IMAGE_WATERMARK_SCALE_INVALID",
		},
		{
			name:    "text scale above range",
			options: map[string]any{"text": "x", "scalePercent": 9},
			want:    "IMAGE_WATERMARK_SCALE_INVALID",
		},
		{
			name:    "logo scale below range",
			options: map[string]any{"kind": "logo", "logoPath": logo, "scalePercent": 4},
			want:    "IMAGE_WATERMARK_SCALE_INVALID",
		},
		{
			name:    "logo scale above range",
			options: map[string]any{"kind": "logo", "logoPath": logo, "scalePercent": 61},
			want:    "IMAGE_WATERMARK_SCALE_INVALID",
		},
		{
			name:    "opacity below range",
			options: map[string]any{"text": "x", "opacity": 0.01},
			want:    "IMAGE_WATERMARK_OPACITY_INVALID",
		},
		{
			name:    "opacity above range",
			options: map[string]any{"text": "x", "opacity": 1.5},
			want:    "IMAGE_WATERMARK_OPACITY_INVALID",
		},
		{
			name:    "rotation below range",
			options: map[string]any{"text": "x", "rotationDegrees": -91},
			want:    "IMAGE_WATERMARK_ROTATION_INVALID",
		},
		{
			name:    "rotation above range",
			options: map[string]any{"text": "x", "rotationDegrees": 91},
			want:    "IMAGE_WATERMARK_ROTATION_INVALID",
		},
		{
			name:    "quality below range",
			options: map[string]any{"text": "x", "quality": 30},
			want:    "IMAGE_WATERMARK_QUALITY_INVALID",
		},
		{
			name:    "quality above range",
			options: map[string]any{"text": "x", "quality": 101},
			want:    "IMAGE_WATERMARK_QUALITY_INVALID",
		},
		{
			name:    "unsupported format",
			options: map[string]any{"text": "x", "format": "gif"},
			want:    "IMAGE_WATERMARK_FORMAT_INVALID",
		},
		{
			name:    "invalid color",
			options: map[string]any{"text": "x", "color": "red"},
			want:    "IMAGE_WATERMARK_COLOR_INVALID",
		},
		{
			name:    "invalid color length",
			options: map[string]any{"text": "x", "color": "#12345"},
			want:    "IMAGE_WATERMARK_COLOR_INVALID",
		},
		{
			name:    "bold not boolean",
			options: map[string]any{"text": "x", "bold": 1.5},
			want:    "IMAGE_WATERMARK_OPTION_INVALID",
		},
		{
			name:    "tile not boolean",
			options: map[string]any{"text": "x", "tile": "yes"},
			want:    "IMAGE_WATERMARK_OPTION_INVALID",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tool := NewWatermarkTool()
			plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, testCase.options))
			if planErr == nil {
				t.Fatalf("expected %s", testCase.want)
			}
			if planErr.DetailCode != testCase.want {
				t.Fatalf("expected %s, got %s (%s)", testCase.want, planErr.DetailCode, planErr.Message)
			}
			if planErr.Message == "" {
				t.Fatal("expected a user message")
			}
			if plan.Success {
				t.Fatal("expected the plan to report failure")
			}
		})
	}
}

func TestWatermarkPlanSuffixesExistingAutoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 300, 240)

	existing := filepath.Join(dir, "photo_watermarked.png")
	sentinel := []byte("do not overwrite")
	if err := os.WriteFile(existing, sentinel, 0o644); err != nil {
		t.Fatalf("write existing output: %v", err)
	}

	tool := NewWatermarkTool()
	request := watermarkTestRequest("single", []string{input}, watermarkRequestOptions())

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "photo_watermarked-2.png"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected the adjusted flag")
	}
	if len(output.Warnings) == 0 || len(plan.Warnings) == 0 {
		t.Fatal("expected a warning explaining the suffix")
	}

	if got := readFixtureBytes(t, existing); !strings.Contains(string(got), "do not overwrite") {
		t.Fatal("the plan must not touch the existing file")
	}

	item, execErr := tool.ExecuteSingle(context.Background(), request)
	if execErr != nil {
		t.Fatalf("expected the execution to succeed, got %v", execErr)
	}
	if item.OutputPath != output.OutputPath {
		t.Fatalf("expected execution to write %q, got %q", output.OutputPath, item.OutputPath)
	}
	if got := readFixtureBytes(t, existing); !strings.Contains(string(got), "do not overwrite") {
		t.Fatal("execution must never overwrite the collision target")
	}
	if _, statErr := os.Stat(item.OutputPath); statErr != nil {
		t.Fatalf("expected the suffixed output: %v", statErr)
	}
}

func TestWatermarkPlanExplicitOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 200, 150)

	tool := NewWatermarkTool()

	options := watermarkRequestOptions()
	custom := filepath.Join(dir, "custom.png")
	options["outputPath"] = custom
	plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, options))
	if planErr != nil {
		t.Fatalf("expected the explicit plan to succeed, got %v", planErr)
	}
	if plan.Outputs[0].OutputPath != custom {
		t.Fatalf("expected explicit output %q, got %q", custom, plan.Outputs[0].OutputPath)
	}

	mismatchOptions := watermarkRequestOptions()
	mismatchOptions["outputPath"] = filepath.Join(dir, "custom.jpg")
	_, mismatchErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, mismatchOptions))
	if mismatchErr == nil {
		t.Fatal("expected an extension mismatch to fail")
	}
	if mismatchErr.DetailCode != "IMAGE_WATERMARK_OUTPUT_EXTENSION_MISMATCH" {
		t.Fatalf("expected IMAGE_WATERMARK_OUTPUT_EXTENSION_MISMATCH, got %q", mismatchErr.DetailCode)
	}

	sameOptions := watermarkRequestOptions()
	sameOptions["outputPath"] = input
	_, sameErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, sameOptions))
	if sameErr == nil {
		t.Fatal("expected an output matching the input to fail")
	}
	if sameErr.DetailCode != "IMAGE_WATERMARK_OUTPUT_COLLIDES_INPUT" {
		t.Fatalf("expected IMAGE_WATERMARK_OUTPUT_COLLIDES_INPUT, got %q", sameErr.DetailCode)
	}

	existing := filepath.Join(dir, "existing.png")
	writePlanPNG(t, existing, 20, 20)
	conflictOptions := watermarkRequestOptions()
	conflictOptions["outputPath"] = existing
	conflictPlan, conflictErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, conflictOptions))
	if conflictErr == nil {
		t.Fatal("expected an existing explicit output to fail")
	}
	if conflictErr.DetailCode != "IMAGE_WATERMARK_OUTPUT_EXISTS" {
		t.Fatalf("expected IMAGE_WATERMARK_OUTPUT_EXISTS, got %q", conflictErr.DetailCode)
	}
	if len(conflictPlan.Outputs) != 1 || !conflictPlan.Outputs[0].Exists {
		t.Fatal("expected the conflicting output to be reported as existing")
	}
}

func TestWatermarkExecutionSingle(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 300, 240)
	before := readFixtureBytes(t, input)

	tool := NewWatermarkTool()
	request := watermarkTestRequest("single", []string{input}, watermarkRequestOptions())

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	plannedPath := plan.Outputs[0].OutputPath

	item, execErr := tool.ExecuteSingle(context.Background(), request)
	if execErr != nil {
		t.Fatalf("expected the watermark to succeed, got %v", execErr)
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
	if !strings.Contains(item.Message, "text watermark at bottom-right") {
		t.Fatalf("expected an anchor in the message, got %q", item.Message)
	}
	if !strings.Contains(item.Message, "300×240") {
		t.Fatalf("expected the encoded dimensions in the message, got %q", item.Message)
	}

	output := readFixtureBytes(t, plannedPath)
	if string(output) == string(before) {
		t.Fatal("expected the output to differ from the source")
	}
	if got := readFixtureBytes(t, input); string(got) != string(before) {
		t.Fatal("expected the input to stay untouched")
	}

	beforeImage := watermarkDecodedImage(t, before)
	afterImage := watermarkDecodedImage(t, output)
	changed := watermarkChangedBounds(t, beforeImage, afterImage)
	watermarkAssertThird(t, changed, 300, 240, "right", "bottom")
}

func TestWatermarkExecutionSingleWithProgress(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	tool := NewWatermarkTool()
	request := watermarkTestRequest("single", []string{input}, watermarkRequestOptions())

	var events []models.JobProgressV1
	item, execErr := tool.ExecuteSingleWithProgress(context.Background(), request, func(event models.JobProgressV1) {
		events = append(events, event)
	})
	if execErr != nil {
		t.Fatalf("expected the watermark to succeed, got %v", execErr)
	}
	if !item.Success {
		t.Fatalf("expected a success item, got %+v", item)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 progress events, got %+v", events)
	}
	if events[0].Current != 0 || events[0].Total != 1 || events[0].Message != "watermarking 1/1" {
		t.Fatalf("unexpected start event: %+v", events[0])
	}
	if events[1].Current != 1 || events[1].Total != 1 || events[1].Message != "processed 1/1" {
		t.Fatalf("unexpected end event: %+v", events[1])
	}
	if events[0].Stage != models.JobStatusRunning {
		t.Fatalf("expected the running stage, got %q", events[0].Stage)
	}
}

func TestWatermarkExecutionSingleCancelledContext(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	tool := NewWatermarkTool()
	request := watermarkTestRequest("single", []string{input}, watermarkRequestOptions())

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	item, execErr := tool.ExecuteSingle(ctx, request)
	if execErr == nil {
		t.Fatal("expected cancellation")
	}
	if execErr.DetailCode != "IMAGE_WATERMARK_CANCELLED" {
		t.Fatalf("expected IMAGE_WATERMARK_CANCELLED, got %q", execErr.DetailCode)
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

func TestWatermarkExecutionBatchMixedExtensions(t *testing.T) {
	dir := t.TempDir()
	jpegPath := filepath.Join(dir, "photo.jpg")
	pngPath := filepath.Join(dir, "scan.png")
	webpPath := filepath.Join(dir, "graphic.webp")
	writeCompressNoiseJPEG(t, jpegPath, 320, 240, 90)
	writePlanPNG(t, pngPath, 200, 150)
	writeWatermarkWebP(t, webpPath, 200, 150)
	jpegBefore := readFixtureBytes(t, jpegPath)

	tool := NewWatermarkTool()
	request := watermarkTestRequest("batch", []string{jpegPath, pngPath, webpPath}, watermarkRequestOptions())

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}
	expectedPaths := []string{
		filepath.Join(dir, "photo_watermarked.jpeg"),
		filepath.Join(dir, "scan_watermarked.png"),
		filepath.Join(dir, "graphic_watermarked.webp"),
	}
	for index, want := range expectedPaths {
		if plan.Outputs[index].OutputPath != want {
			t.Fatalf("expected output %d at %q, got %q", index, want, plan.Outputs[index].OutputPath)
		}
	}

	var events []models.JobProgressV1
	items, execErr := tool.ExecuteBatch(context.Background(), request, func(event models.JobProgressV1) {
		events = append(events, event)
	})
	if execErr != nil {
		t.Fatalf("expected the batch to succeed, got %v", execErr)
	}
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}

	wantFormats := []string{"jpeg", "png", "webp"}
	for index, item := range items {
		if !item.Success {
			t.Fatalf("expected item %d to succeed, got %+v", index, item)
		}
		if item.OutputPath != expectedPaths[index] {
			t.Fatalf("expected item %d at %q, got %q", index, expectedPaths[index], item.OutputPath)
		}
		if got := bimg.DetermineImageTypeName(readFixtureBytes(t, item.OutputPath)); got != wantFormats[index] {
			t.Fatalf("expected item %d to be %s, got %s", index, wantFormats[index], got)
		}
	}

	if len(events) != 3 {
		t.Fatalf("expected 3 progress events, got %+v", events)
	}
	for index, event := range events {
		if event.Current != index+1 || event.Total != 3 {
			t.Fatalf("unexpected progress event %d: %+v", index, event)
		}
		want := fmt.Sprintf("processed %d/3", index+1)
		if event.Message != want {
			t.Fatalf("expected progress message %q, got %q", want, event.Message)
		}
	}

	if got := readFixtureBytes(t, jpegPath); string(got) != string(jpegBefore) {
		t.Fatal("expected the jpeg input to stay untouched")
	}
}

func TestWatermarkExecutionBatchCancellation(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writePlanPNG(t, first, 200, 150)
	writePlanPNG(t, second, 200, 150)

	tool := NewWatermarkTool()
	request := watermarkTestRequest("batch", []string{first, second}, watermarkRequestOptions())

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
	if execErr.DetailCode != "IMAGE_WATERMARK_CANCELLED" {
		t.Fatalf("expected IMAGE_WATERMARK_CANCELLED, got %q", execErr.DetailCode)
	}
	if !strings.Contains(execErr.Message, "operation cancelled") {
		t.Fatalf("expected the cancellation message, got %q", execErr.Message)
	}
	if len(items) != 1 || !items[0].Success {
		t.Fatalf("expected one successful item before cancellation, got %+v", items)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "second_watermarked.png")); !os.IsNotExist(statErr) {
		t.Fatal("expected the last item not to run")
	}
}

func TestWatermarkExecutionBatchReportsItemFailures(t *testing.T) {
	dir := t.TempDir()
	valid := filepath.Join(dir, "valid.png")
	broken := filepath.Join(dir, "broken.png")
	writePlanPNG(t, valid, 120, 90)
	if err := os.WriteFile(broken, []byte("not an image"), 0o644); err != nil {
		t.Fatalf("write broken fixture: %v", err)
	}

	tool := NewWatermarkTool()
	request := watermarkTestRequest("batch", []string{valid, broken}, watermarkRequestOptions())

	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected the batch plan to tolerate a broken image, got %v", planErr)
	}

	items, execErr := tool.ExecuteBatch(context.Background(), request, nil)
	if execErr == nil {
		t.Fatal("expected a first item error for the broken image")
	}
	if execErr.DetailCode != "IMAGE_WATERMARK_BATCH_ITEM" {
		t.Fatalf("expected IMAGE_WATERMARK_BATCH_ITEM, got %q", execErr.DetailCode)
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
	if items[1].Error == nil || !strings.HasPrefix(items[1].Error.DetailCode, "IMAGE_WATERMARK_") {
		t.Fatalf("expected a prefixed item error, got %+v", items[1].Error)
	}
	if items[1].Error.Message != "Could not watermark the image." {
		t.Fatalf("expected a clean item message, got %q", items[1].Error.Message)
	}
	if items[1].Error.Details["error"] == nil || items[1].Error.Details["error"] == "" {
		t.Fatalf("expected the raw cause in details, got %v", items[1].Error.Details)
	}
	if strings.Contains(items[1].Message, "decode image") {
		t.Fatalf("expected no raw libvips text in the item message, got %q", items[1].Message)
	}
}

func TestWatermarkExecutionBatchRequiresBatchMode(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	tool := NewWatermarkTool()
	request := watermarkTestRequest("single", []string{input}, watermarkRequestOptions())

	_, execErr := tool.ExecuteBatch(context.Background(), request, nil)
	if execErr == nil {
		t.Fatal("expected single mode to be rejected")
	}
	if execErr.DetailCode != "IMAGE_WATERMARK_MODE_INVALID" {
		t.Fatalf("expected IMAGE_WATERMARK_MODE_INVALID, got %q", execErr.DetailCode)
	}
}

func TestWatermarkPlanAnimatedInputWarning(t *testing.T) {
	dir := t.TempDir()
	animated := filepath.Join(dir, "animated.gif")
	static := filepath.Join(dir, "static.gif")
	writeCompressAnimatedGIF(t, animated)
	writeCompressTransparentGIF(t, static)

	tool := NewWatermarkTool()

	plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{animated}, map[string]any{"text": "S"}))
	if planErr != nil {
		t.Fatalf("expected the animated plan to succeed, got %v", planErr)
	}
	wantWarning := "animated.gif: animated GIF input: only the first frame is watermarked"
	if !slices.Contains(plan.Warnings, wantWarning) {
		t.Fatalf("expected the animated warning %q, got %v", wantWarning, plan.Warnings)
	}

	staticPlan, staticErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{static}, map[string]any{"text": "S"}))
	if staticErr != nil {
		t.Fatalf("expected the static plan to succeed, got %v", staticErr)
	}
	for _, warning := range staticPlan.Warnings {
		if strings.Contains(warning, watermarkAnimatedWarning) {
			t.Fatalf("expected no animated warning for a single-frame gif, got %q", warning)
		}
	}
}

func TestWatermarkPlanBatchIgnoresExplicitOutputPath(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "one.png")
	second := filepath.Join(dir, "two.png")
	writePlanPNG(t, first, 120, 90)
	writePlanPNG(t, second, 120, 90)

	tool := NewWatermarkTool()
	options := watermarkRequestOptions()
	options["outputPath"] = filepath.Join(dir, "ignored.png")

	plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("batch", []string{first, second}, options))
	if planErr != nil {
		t.Fatalf("expected the batch plan to succeed, got %v", planErr)
	}
	if len(plan.Outputs) != 2 {
		t.Fatalf("expected 2 outputs, got %d", len(plan.Outputs))
	}
	for index, want := range []string{filepath.Join(dir, "one_watermarked.png"), filepath.Join(dir, "two_watermarked.png")} {
		if plan.Outputs[index].OutputPath != want {
			t.Fatalf("expected output %d at %q, got %q", index, want, plan.Outputs[index].OutputPath)
		}
	}
}

func TestWatermarkLogoIsReadOnceDuringPlanning(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 300, 240)
	logo := filepath.Join(dir, "logo.png")
	writeWatermarkLogoFile(t, logo, 50, 25)

	request := watermarkTestRequest("batch", []string{input}, map[string]any{"kind": "logo", "logoPath": logo})

	parsed, parseErr := parseWatermarkRequest(request)
	if parseErr != nil {
		t.Fatalf("expected the request to parse, got %v", parseErr)
	}
	if len(parsed.logoBytes) == 0 {
		t.Fatal("expected the logo bytes to be loaded during parsing")
	}

	// Removing the file proves planning and execution use the in-memory bytes
	// instead of reading the logo per item.
	if err := os.Remove(logo); err != nil {
		t.Fatalf("remove logo fixture: %v", err)
	}

	plan, prepared, planErr := planParsedWatermarkRequest(parsed)
	if planErr != nil {
		t.Fatalf("expected planning to use the loaded logo bytes, got %v", planErr)
	}
	if !plan.Success || len(prepared) != 1 {
		t.Fatalf("expected one prepared item, got %+v", plan)
	}
	if len(prepared[0].options.Logo) == 0 {
		t.Fatal("expected the prepared item to carry the logo bytes")
	}

	if _, execErr := executeWatermarkToPath(context.Background(), prepared[0]); execErr != nil {
		t.Fatalf("expected execution to use the planned logo bytes, got %v", execErr)
	}
	if _, statErr := os.Stat(prepared[0].outputPath); statErr != nil {
		t.Fatalf("expected the logo output to exist: %v", statErr)
	}
}

func TestWatermarkExecutionLogoBatch(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "one.png")
	second := filepath.Join(dir, "two.png")
	writePlanPNG(t, first, 300, 240)
	writePlanPNG(t, second, 300, 240)
	logo := filepath.Join(dir, "logo.png")
	writeWatermarkLogoFile(t, logo, 60, 30)

	tool := NewWatermarkTool()
	request := watermarkTestRequest("batch", []string{first, second}, map[string]any{"kind": "logo", "logoPath": logo})

	items, execErr := tool.ExecuteBatch(context.Background(), request, nil)
	if execErr != nil {
		t.Fatalf("expected the logo batch to succeed, got %v", execErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	for index, item := range items {
		if !item.Success {
			t.Fatalf("expected item %d to succeed, got %+v", index, item)
		}
		if !strings.Contains(item.Message, "logo watermark at bottom-right") {
			t.Fatalf("expected a logo message, got %q", item.Message)
		}
		if _, statErr := os.Stat(item.OutputPath); statErr != nil {
			t.Fatalf("expected item %d output: %v", index, statErr)
		}
	}
}

func TestWatermarkExecutionJPEGFlattensAlphaOnWhite(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "transparent.png")
	writeWatermarkSolidPNG(t, input, 200, 150, color.NRGBA{})

	tool := NewWatermarkTool()
	options := watermarkRequestOptions()
	options["format"] = "jpeg"

	plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, options))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if got := plan.EffectiveOptions["format"]; got != "jpeg" {
		t.Fatalf("expected jpeg format, got %v", got)
	}

	item, execErr := tool.ExecuteSingle(context.Background(), watermarkTestRequest("single", []string{input}, options))
	if execErr != nil {
		t.Fatalf("expected the jpeg watermark to succeed, got %v", execErr)
	}
	if got := bimg.DetermineImageTypeName(readFixtureBytes(t, item.OutputPath)); got != "jpeg" {
		t.Fatalf("expected jpeg output, got %s", got)
	}

	decoded := watermarkDecodedImage(t, readFixtureBytes(t, item.OutputPath))
	pixel := color.NRGBAModel.Convert(decoded.At(20, 20)).(color.NRGBA)
	if pixel.R < 240 || pixel.G < 240 || pixel.B < 240 {
		t.Fatalf("expected the transparent source to flatten on white, got %v", pixel)
	}
}

func TestWatermarkPlanRejectsUnsupportedScripts(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 300, 240)

	cases := []struct {
		name string
		text string
	}{
		{name: "cjk", text: "漢字"},
		{name: "arabic", text: "مرحبا"},
		{name: "devanagari", text: "नमस्ते"},
		{name: "emoji", text: "😀"},
	}

	tool := NewWatermarkTool()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			options := map[string]any{"text": testCase.text}
			plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, options))
			if planErr == nil {
				t.Fatalf("expected %q to be rejected", testCase.text)
			}
			if planErr.DetailCode != "IMAGE_WATERMARK_TEXT_UNSUPPORTED" {
				t.Fatalf("expected IMAGE_WATERMARK_TEXT_UNSUPPORTED, got %q (%s)", planErr.DetailCode, planErr.Message)
			}
			if !strings.Contains(planErr.Message, "bundled font does not include") {
				t.Fatalf("expected the missing-glyph explanation, got %q", planErr.Message)
			}
			if plan.Success {
				t.Fatal("expected the plan to report failure")
			}
		})
	}

	accepted := []struct {
		name string
		text string
	}{
		{name: "latin", text: "Watermark"},
		{name: "greek", text: "Γειά σου"},
		{name: "cyrillic", text: "Привет мир"},
	}
	for _, testCase := range accepted {
		t.Run(testCase.name, func(t *testing.T) {
			options := map[string]any{"text": testCase.text}
			if _, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, options)); planErr != nil {
				t.Fatalf("expected %q to be accepted, got %v", testCase.text, planErr)
			}
		})
	}
}

func TestWatermarkPlanWindowsLineEndings(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 300, 240)

	tool := NewWatermarkTool()
	options := map[string]any{"text": "First line\r\nSecond line\rThird line"}
	plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, options))
	if planErr != nil {
		t.Fatalf("expected pasted Windows text to be accepted, got %v", planErr)
	}
	if got := plan.EffectiveOptions["text"]; got != "First line\nSecond line\nThird line" {
		t.Fatalf("expected normalized line endings, got %q", got)
	}
}

func TestWatermarkPlanPNGQualityWarning(t *testing.T) {
	dir := t.TempDir()
	pngInput := filepath.Join(dir, "photo.png")
	jpegInput := filepath.Join(dir, "photo.jpg")
	writePlanPNG(t, pngInput, 300, 240)
	writeCompressNoiseJPEG(t, jpegInput, 300, 240, 90)

	tool := NewWatermarkTool()

	withQuality := watermarkRequestOptions()
	withQuality["quality"] = 80

	pngPlan, pngErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{pngInput}, withQuality))
	if pngErr != nil {
		t.Fatalf("expected the png plan to succeed, got %v", pngErr)
	}
	if !slices.Contains(pngPlan.Warnings, watermarkPNGQualityWarning) {
		t.Fatalf("expected the PNG quality warning, got %v", pngPlan.Warnings)
	}

	jpegPlan, jpegErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{jpegInput}, withQuality))
	if jpegErr != nil {
		t.Fatalf("expected the jpeg plan to succeed, got %v", jpegErr)
	}
	if slices.Contains(jpegPlan.Warnings, watermarkPNGQualityWarning) {
		t.Fatalf("expected no PNG quality warning for jpeg-only output, got %v", jpegPlan.Warnings)
	}

	defaultPlan, defaultErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{pngInput}, watermarkRequestOptions()))
	if defaultErr != nil {
		t.Fatalf("expected the default-quality plan to succeed, got %v", defaultErr)
	}
	if slices.Contains(defaultPlan.Warnings, watermarkPNGQualityWarning) {
		t.Fatalf("expected no warning when quality was not supplied, got %v", defaultPlan.Warnings)
	}

	mixedPlan, mixedErr := tool.Plan(context.Background(), watermarkTestRequest("batch", []string{pngInput, jpegInput}, withQuality))
	if mixedErr != nil {
		t.Fatalf("expected the mixed plan to succeed, got %v", mixedErr)
	}
	if !slices.Contains(mixedPlan.Warnings, watermarkPNGQualityWarning) {
		t.Fatalf("expected the PNG quality warning in a mixed batch, got %v", mixedPlan.Warnings)
	}
}

func TestWatermarkPlanExplicitJPGAlias(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.jpg")
	writeCompressNoiseJPEG(t, input, 200, 150, 90)

	tool := NewWatermarkTool()
	for _, name := range []string{"custom.jpg", "custom.JPG"} {
		t.Run(name, func(t *testing.T) {
			options := watermarkRequestOptions()
			want := filepath.Join(dir, name)
			options["outputPath"] = want

			plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, options))
			if planErr != nil {
				t.Fatalf("expected %s to be accepted, got %v", name, planErr)
			}
			if plan.Outputs[0].OutputPath != want {
				t.Fatalf("expected output %q, got %q", want, plan.Outputs[0].OutputPath)
			}
		})
	}
}

func TestWatermarkExecutionReportsAutoFitFontSize(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 300, 240)

	lines := make([]string, watermarkMaxTextLines)
	for index := range lines {
		lines[index] = fmt.Sprintf("Line %d", index+1)
	}

	options := watermarkRequestOptions()
	options["text"] = strings.Join(lines, "\n")
	options["scalePercent"] = 8

	tool := NewWatermarkTool()
	item, execErr := tool.ExecuteSingle(context.Background(), watermarkTestRequest("single", []string{input}, options))
	if execErr != nil {
		t.Fatalf("expected the auto-fit execution to succeed, got %v", execErr)
	}

	const marker = "(font "
	index := strings.Index(item.Message, marker)
	if index < 0 {
		t.Fatalf("expected the font size in the message, got %q", item.Message)
	}
	var fontSize int
	if _, scanErr := fmt.Sscanf(item.Message[index+len(marker):], "%d", &fontSize); scanErr != nil {
		t.Fatalf("expected a numeric font size in %q: %v", item.Message, scanErr)
	}
	if fontSize >= 19 || fontSize < watermarkMinFontSize {
		t.Fatalf("expected the auto-fit size in [12, 19), got %d (%q)", fontSize, item.Message)
	}
}

// watermarkLateCancelContext closes its Done channel on the Nth check so a test
// can deterministically cancel the last batch item mid-execution, after the
// engine encoded but before any output is written.
type watermarkLateCancelContext struct {
	done        chan struct{}
	checks      int
	cancelAfter int
}

func (c *watermarkLateCancelContext) Deadline() (time.Time, bool) { return time.Time{}, false }

func (c *watermarkLateCancelContext) Done() <-chan struct{} {
	c.checks++
	if c.checks >= c.cancelAfter {
		select {
		case <-c.done:
		default:
			close(c.done)
		}
	}

	return c.done
}

func (c *watermarkLateCancelContext) Err() error {
	select {
	case <-c.done:
		return context.Canceled
	default:
		return nil
	}
}

func (c *watermarkLateCancelContext) Value(any) any { return nil }

func TestWatermarkBatchLastItemMidExecutionCancellation(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.png")
	writePlanPNG(t, first, 300, 240)
	writePlanPNG(t, second, 300, 240)

	tool := NewWatermarkTool()
	request := watermarkTestRequest("batch", []string{first, second}, watermarkRequestOptions())

	// Each item performs four context checks: one loop pre-check plus three
	// inside executeWatermarkToPath. The eighth check is the last item's
	// post-encode check, so cancellation happens mid-execution.
	ctx := &watermarkLateCancelContext{done: make(chan struct{}), cancelAfter: 8}

	items, execErr := tool.ExecuteBatch(ctx, request, nil)
	if execErr == nil {
		t.Fatal("expected the last item to be reclassified as cancelled")
	}
	if execErr.DetailCode != "IMAGE_WATERMARK_CANCELLED" {
		t.Fatalf("expected IMAGE_WATERMARK_CANCELLED, got %q", execErr.DetailCode)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 items, got %d", len(items))
	}
	if !items[0].Success {
		t.Fatalf("expected the first item to complete, got %+v", items[0])
	}
	if items[1].Success {
		t.Fatalf("expected the last item to fail, got %+v", items[1])
	}
	if items[1].Error == nil || items[1].Error.DetailCode != "IMAGE_WATERMARK_CANCELLED" {
		t.Fatalf("expected the cancelled item error, got %+v", items[1].Error)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "second_watermarked.png")); !os.IsNotExist(statErr) {
		t.Fatal("expected the cancelled last item not to write an output")
	}
}

func TestWatermarkPlanNormalizesDecomposedAccents(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 300, 240)

	tool := NewWatermarkTool()
	options := map[string]any{"text": "e\u0301"}

	plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{input}, options))
	if planErr != nil {
		t.Fatalf("expected decomposed text to plan through NFC, got %v", planErr)
	}
	if got := plan.EffectiveOptions["text"]; got != "é" {
		t.Fatalf("expected the NFC-composed text %q, got %q", "é", got)
	}

	item, execErr := tool.ExecuteSingle(context.Background(), watermarkTestRequest("single", []string{input}, options))
	if execErr != nil {
		t.Fatalf("expected decomposed text to draw, got %v", execErr)
	}
	if !item.Success {
		t.Fatalf("expected a success item, got %+v", item)
	}

	before := watermarkDecodedImage(t, readFixtureBytes(t, input))
	after := watermarkDecodedImage(t, readFixtureBytes(t, item.OutputPath))
	if changed := watermarkChangedBounds(t, before, after); changed.Empty() {
		t.Fatal("expected the decomposed accent to change pixels")
	}
}

func TestWatermarkPlanRejectsUnfittableText(t *testing.T) {
	dir := t.TempDir()
	small := filepath.Join(dir, "small.png")
	wide := filepath.Join(dir, "wide.png")
	tileInput := filepath.Join(dir, "tile.png")
	writePlanPNG(t, small, 100, 100)
	writePlanPNG(t, wide, 300, 240)
	writePlanPNG(t, tileInput, 600, 600)

	lines := make([]string, watermarkMaxTextLines)
	for index := range lines {
		lines[index] = fmt.Sprintf("Line %d", index+1)
	}

	cases := []struct {
		name    string
		input   string
		options map[string]any
	}{
		{
			name:    "ten lines on a small canvas",
			input:   small,
			options: map[string]any{"text": strings.Join(lines, "\n"), "scalePercent": 8},
		},
		{
			name:    "wide single line",
			input:   wide,
			options: map[string]any{"text": strings.Repeat("M", watermarkMaxTextRunes), "scalePercent": 8},
		},
		{
			name:    "over-wide tile",
			input:   tileInput,
			options: map[string]any{"text": strings.Repeat("W", watermarkMaxTextRunes), "tile": true, "scalePercent": 8},
		},
	}

	tool := NewWatermarkTool()
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			plan, planErr := tool.Plan(context.Background(), watermarkTestRequest("single", []string{testCase.input}, testCase.options))
			if planErr == nil {
				t.Fatal("expected the plan to reject text that cannot fit at the 12 px floor")
			}
			if planErr.DetailCode != "IMAGE_WATERMARK_TEXT_DOES_NOT_FIT" {
				t.Fatalf("expected IMAGE_WATERMARK_TEXT_DOES_NOT_FIT, got %q (%s)", planErr.DetailCode, planErr.Message)
			}
			if !strings.Contains(planErr.Message, "minimum font size") {
				t.Fatalf("expected the minimum font size explanation, got %q", planErr.Message)
			}
			if plan.Success {
				t.Fatal("expected the plan to report failure")
			}
		})
	}
}

func TestWatermarkPlanExplicitOutputSkipsOutputDir(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 300, 240)
	custom := filepath.Join(dir, "custom.png")

	options := watermarkRequestOptions()
	options["outputPath"] = custom

	request := watermarkTestRequest("single", []string{input}, options)
	request.OutputDir = filepath.Join(dir, "missing-dir")

	tool := NewWatermarkTool()
	plan, planErr := tool.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected an explicit output path to skip outputDir validation, got %v", planErr)
	}
	if len(plan.Outputs) != 1 || plan.Outputs[0].OutputPath != custom {
		t.Fatalf("expected the explicit output %q, got %+v", custom, plan.Outputs)
	}
}
