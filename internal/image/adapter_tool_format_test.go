package image

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"fileforge-desktop/internal/models"

	"github.com/h2non/bimg"
)

func TestPlanAutoFormatSingleKeepsSourceExtension(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.jpg")
	writePlanFixture(t, input)

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{"format": "auto"}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "photo.jpeg"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if output.Adjusted {
		t.Fatal("expected photo.jpeg to be a fresh name next to photo.jpg")
	}
	if got := plan.EffectiveOptions["format"]; got != "auto" {
		t.Fatalf("expected effective format auto, got %v", got)
	}
}

func TestPlanAutoFormatKeepsTheOriginalFileForSameExtension(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanFixture(t, input)

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{"format": "original"}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	output := plan.Outputs[0]
	if want := filepath.Join(dir, "photo-converted.png"); output.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, output.OutputPath)
	}
	if !output.Adjusted {
		t.Fatal("expected the same-extension output to be renamed")
	}
	if got := plan.EffectiveOptions["format"]; got != "auto" {
		t.Fatalf("expected the original alias to resolve to auto, got %v", got)
	}
}

func TestPlanAutoFormatBatchResolvesEachInput(t *testing.T) {
	dir := t.TempDir()
	inputs := []string{
		filepath.Join(dir, "a.png"),
		filepath.Join(dir, "b.jpg"),
		filepath.Join(dir, "c.bmp"),
		filepath.Join(dir, "d.tif"),
	}
	for _, input := range inputs {
		writePlanFixture(t, input)
	}

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("batch", inputs, map[string]any{"format": "auto"}))
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	wanted := []string{
		filepath.Join(dir, "a-converted.png"),
		filepath.Join(dir, "b.jpeg"),
		filepath.Join(dir, "c.png"),
		filepath.Join(dir, "d.tiff"),
	}
	if len(plan.Outputs) != len(wanted) {
		t.Fatalf("expected %d planned outputs, got %d", len(wanted), len(plan.Outputs))
	}
	for index, want := range wanted {
		if plan.Outputs[index].OutputPath != want {
			t.Fatalf("output %d: expected %q, got %q", index, want, plan.Outputs[index].OutputPath)
		}
	}

	if got := plan.EffectiveOptions["format"]; got != "auto" {
		t.Fatalf("expected the mixed batch to keep the auto marker, got %v", got)
	}
}

func TestPlanAutoFormatFallsBackToPNGForUnwritableSourceFormats(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "scan.bmp")
	writePlanFixture(t, input)

	adapter := NewImageToolAdapter(nil)
	plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{"format": "auto"}))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	if want := filepath.Join(dir, "scan.png"); plan.Outputs[0].OutputPath != want {
		t.Fatalf("expected the BMP fallback output %q, got %q", want, plan.Outputs[0].OutputPath)
	}
	if got := plan.EffectiveOptions["format"]; got != "auto" {
		t.Fatalf("expected effective format auto, got %v", got)
	}
}

func TestPlanRejectsUnsupportedExplicitFormat(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanFixture(t, input)

	adapter := NewImageToolAdapter(nil)
	_, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{"format": "bmp"}))
	if planErr == nil {
		t.Fatal("expected an unsupported explicit format error")
	}
	if planErr.DetailCode != "IMAGE_FORMAT_INVALID" {
		t.Fatalf("expected IMAGE_FORMAT_INVALID, got %q", planErr.DetailCode)
	}
}

func TestPlanExplicitTIFFAndAVIFFormats(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanFixture(t, input)

	adapter := NewImageToolAdapter(nil)
	for _, format := range []string{"tiff", "avif"} {
		t.Run(format, func(t *testing.T) {
			plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{"format": format}))
			if planErr != nil {
				t.Fatalf("expected %s plan to succeed, got %v", format, planErr)
			}

			if want := filepath.Join(dir, "photo."+format); plan.Outputs[0].OutputPath != want {
				t.Fatalf("expected output %q, got %q", want, plan.Outputs[0].OutputPath)
			}
			if got := plan.EffectiveOptions["format"]; got != format {
				t.Fatalf("expected effective format %q, got %v", format, got)
			}
		})
	}
}

func TestPlanEffectiveOptionsIncludeTransformOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanFixture(t, input)

	adapter := NewImageToolAdapter(nil)

	t.Run("longestEdge replaces width and height", func(t *testing.T) {
		plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{
			"format":        "jpeg",
			"quality":       float64(72),
			"width":         float64(10),
			"height":        float64(10),
			"longestEdge":   float64(1200),
			"rotate":        float64(270),
			"flipH":         true,
			"flipV":         false,
			"stripMetadata": true,
		}))
		if planErr != nil {
			t.Fatalf("expected plan to succeed, got %v", planErr)
		}

		want := map[string]any{
			"format":        "jpeg",
			"quality":       72,
			"longestEdge":   1200,
			"rotate":        270,
			"flipH":         true,
			"flipV":         false,
			"stripMetadata": true,
		}
		if !reflect.DeepEqual(plan.EffectiveOptions, want) {
			t.Fatalf("expected effective options %v, got %v", want, plan.EffectiveOptions)
		}
	})

	t.Run("width and height without longestEdge", func(t *testing.T) {
		plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{
			"format": "jpeg",
			"width":  float64(640),
			"height": float64(480),
		}))
		if planErr != nil {
			t.Fatalf("expected plan to succeed, got %v", planErr)
		}

		if got := plan.EffectiveOptions["width"]; got != 640 {
			t.Fatalf("expected effective width 640, got %v", got)
		}
		if got := plan.EffectiveOptions["height"]; got != 480 {
			t.Fatalf("expected effective height 480, got %v", got)
		}
		if _, exists := plan.EffectiveOptions["longestEdge"]; exists {
			t.Fatalf("expected no longestEdge in %v", plan.EffectiveOptions)
		}
	})
}

func TestExecuteSingleAutoFormatWritesSourceFormat(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 20)

	adapter := NewImageToolAdapter(nil)
	item, execErr := adapter.ExecuteSingle(context.Background(), planImageRequest("single", []string{input}, map[string]any{"format": "auto"}))
	if execErr != nil {
		t.Fatalf("expected auto conversion to succeed, got %v", execErr)
	}
	if !item.Success {
		t.Fatalf("expected a successful item, got %+v", item)
	}
	if want := filepath.Join(dir, "photo-converted.png"); item.OutputPath != want {
		t.Fatalf("expected output %q, got %q", want, item.OutputPath)
	}

	output, readErr := os.ReadFile(item.OutputPath)
	if readErr != nil {
		t.Fatalf("read output: %v", readErr)
	}
	if !bytes.HasPrefix(output, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("expected the auto conversion to keep the PNG encoding")
	}
}

func TestExecuteBatchAutoFormatUsesEachInputFormat(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.jpg")
	writePlanPNG(t, first, 40, 20)
	writePlanPNG(t, second, 30, 10)

	adapter := NewImageToolAdapter(nil)
	request := planImageRequest("batch", []string{first, second}, map[string]any{"format": "auto"})

	plan, planErr := adapter.Plan(context.Background(), request)
	if planErr != nil {
		t.Fatalf("expected batch plan to succeed, got %v", planErr)
	}

	items, execErr := adapter.ExecuteBatch(context.Background(), request, nil)
	if execErr != nil {
		t.Fatalf("expected auto batch conversion to succeed, got %v", execErr)
	}
	if len(items) != 2 {
		t.Fatalf("expected 2 result items, got %d", len(items))
	}

	for index, item := range items {
		if !item.Success {
			t.Fatalf("expected item %d to succeed, got %+v", index, item)
		}
		if item.OutputPath != plan.Outputs[index].OutputPath {
			t.Fatalf("item %d wrote %q but the plan said %q", index, item.OutputPath, plan.Outputs[index].OutputPath)
		}
	}

	firstOutput, readErr := os.ReadFile(items[0].OutputPath)
	if readErr != nil {
		t.Fatalf("read png output: %v", readErr)
	}
	if !bytes.HasPrefix(firstOutput, []byte{0x89, 'P', 'N', 'G'}) {
		t.Fatal("expected the PNG input to produce a PNG output")
	}

	secondOutput, readErr := os.ReadFile(items[1].OutputPath)
	if readErr != nil {
		t.Fatalf("read jpeg output: %v", readErr)
	}
	if len(secondOutput) < 2 || secondOutput[0] != 0xFF || secondOutput[1] != 0xD8 {
		t.Fatal("expected the JPG input to produce a JPEG output")
	}
}

func TestExecuteExplicitTIFFAndAVIFFormats(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 20)

	adapter := NewImageToolAdapter(nil)
	cases := []struct {
		format    string
		imageType bimg.ImageType
	}{
		{format: "tiff", imageType: bimg.TIFF},
		{format: "avif", imageType: bimg.AVIF},
	}

	for _, testCase := range cases {
		t.Run(testCase.format, func(t *testing.T) {
			request := planImageRequest("single", []string{input}, map[string]any{"format": testCase.format})
			item, execErr := adapter.ExecuteSingle(context.Background(), request)
			if execErr != nil {
				if !bimg.IsTypeSupportedSave(testCase.imageType) {
					t.Skipf("this libvips build cannot save %s: %v", testCase.format, execErr.Message)
				}
				t.Fatalf("convert to %s: %v", testCase.format, execErr.Message)
			}

			output, readErr := os.ReadFile(item.OutputPath)
			if readErr != nil {
				t.Fatalf("read %s output: %v", testCase.format, readErr)
			}
			size, sizeErr := bimg.NewImage(output).Size()
			if sizeErr != nil {
				t.Fatalf("read %s size: %v", testCase.format, sizeErr)
			}
			if size.Width != 40 || size.Height != 20 {
				t.Fatalf("expected 40x20 output, got %dx%d", size.Width, size.Height)
			}
		})
	}
}

func TestManifestOutputExtensionsFollowTheConverter(t *testing.T) {
	adapter := NewImageToolAdapter(nil)
	manifest := adapter.Manifest()

	want := NewImageConverter().SupportedFormats()
	if !reflect.DeepEqual(manifest.OutputExtensions, want) {
		t.Fatalf("expected manifest output extensions %v, got %v", want, manifest.OutputExtensions)
	}
}

func TestValidateAcceptsAutoFormatForBatch(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.png")
	second := filepath.Join(dir, "second.gif")
	writePlanFixture(t, first)
	writePlanFixture(t, second)

	adapter := NewImageToolAdapter(nil)
	request := models.JobRequestV1{
		ToolID:     ToolIDImageConvertV1,
		Mode:       "batch",
		InputPaths: []string{first, second},
		Options:    map[string]any{"format": "auto"},
	}

	if validationErr := adapter.Validate(context.Background(), request); validationErr != nil {
		t.Fatalf("expected auto batch validation to pass, got %v", validationErr)
	}
}
