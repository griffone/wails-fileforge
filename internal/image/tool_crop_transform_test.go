package image

import (
	"context"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func decodePNGSize(t *testing.T, path string) (int, int) {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open output %s: %v", path, err)
	}
	defer file.Close()

	config, err := png.DecodeConfig(file)
	if err != nil {
		t.Fatalf("decode output %s: %v", path, err)
	}

	return config.Width, config.Height
}

func TestCropEffectiveOptionsIncludeTransformDefaults(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, cropOptions(10, 10, 40, 30)))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	for _, key := range []string{"outputWidth", "outputHeight", "rotate"} {
		if got, ok := plan.EffectiveOptions[key]; !ok || got != 0 {
			t.Fatalf("expected effective %s to default to 0, got %v", key, got)
		}
	}
	for _, key := range []string{"flipH", "flipV"} {
		if got, ok := plan.EffectiveOptions[key]; !ok || got != false {
			t.Fatalf("expected effective %s to default to false, got %v", key, got)
		}
	}
}

func TestCropEffectiveOptionsIncludeSuppliedTransform(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	options := cropOptions(10, 10, 40, 30)
	options["outputWidth"] = 20
	options["outputHeight"] = 10
	options["rotate"] = 180
	options["flipH"] = true
	options["flipV"] = true

	tool := NewCropTool()
	plan, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, options))
	if planErr != nil {
		t.Fatalf("expected plan to succeed, got %v", planErr)
	}

	wanted := map[string]any{
		"outputWidth":  20,
		"outputHeight": 10,
		"rotate":       180,
		"flipH":        true,
		"flipV":        true,
	}
	for key, want := range wanted {
		if got := plan.EffectiveOptions[key]; got != want {
			t.Fatalf("expected effective %s = %v, got %v", key, want, got)
		}
	}
}

func TestCropPlanRejectsInvalidTransformOptions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	cases := []struct {
		name     string
		options  map[string]any
		wantCode string
	}{
		{
			name: "rotate not a quarter turn",
			options: func() map[string]any {
				options := cropOptions(0, 0, 20, 20)
				options["rotate"] = 45
				return options
			}(),
			wantCode: "IMAGE_CROP_ROTATE_INVALID",
		},
		{
			name: "negative output width",
			options: func() map[string]any {
				options := cropOptions(0, 0, 20, 20)
				options["outputWidth"] = -1
				return options
			}(),
			wantCode: "IMAGE_CROP_OUTPUT_SIZE_INVALID",
		},
		{
			name: "negative output height",
			options: func() map[string]any {
				options := cropOptions(0, 0, 20, 20)
				options["outputHeight"] = -20
				return options
			}(),
			wantCode: "IMAGE_CROP_OUTPUT_SIZE_INVALID",
		},
		{
			name: "flip not a boolean",
			options: func() map[string]any {
				options := cropOptions(0, 0, 20, 20)
				options["flipH"] = "sideways"
				return options
			}(),
			wantCode: "IMAGE_CROP_OPTION_INVALID",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			tool := NewCropTool()
			plan, planErr := tool.Plan(context.Background(), cropTestRequest("single", []string{input}, testCase.options))
			if planErr == nil {
				t.Fatalf("expected plan to fail with %s", testCase.wantCode)
			}
			if planErr.DetailCode != testCase.wantCode {
				t.Fatalf("expected detail code %q, got %q (%s)", testCase.wantCode, planErr.DetailCode, planErr.Message)
			}
			if plan.Success {
				t.Fatal("expected failure plan")
			}
		})
	}
}

func TestCropExecutionAppliesOutputResize(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	options := cropOptions(10, 10, 40, 30)
	options["outputWidth"] = 20

	tool := NewCropTool()
	item, execErr := tool.ExecuteSingle(context.Background(), cropTestRequest("single", []string{input}, options))
	if execErr != nil {
		t.Fatalf("expected crop to succeed, got %v", execErr)
	}

	width, height := decodePNGSize(t, item.OutputPath)
	if width != 20 || height != 15 {
		t.Fatalf("expected the 40x30 crop to be resized to 20x15, got %dx%d", width, height)
	}
}

func TestCropExecutionHonorsLargerOutputSize(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	options := cropOptions(10, 10, 40, 30)
	options["outputWidth"] = 400

	tool := NewCropTool()
	item, execErr := tool.ExecuteSingle(context.Background(), cropTestRequest("single", []string{input}, options))
	if execErr != nil {
		t.Fatalf("expected crop to succeed, got %v", execErr)
	}

	width, height := decodePNGSize(t, item.OutputPath)
	if width != 400 || height != 300 {
		t.Fatalf("expected the requested 400x300 output, got %dx%d", width, height)
	}
}

func TestCropExecutionHonorsExactOutputBox(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	options := cropOptions(10, 10, 40, 30)
	options["outputWidth"] = 20
	options["outputHeight"] = 20

	tool := NewCropTool()
	item, execErr := tool.ExecuteSingle(context.Background(), cropTestRequest("single", []string{input}, options))
	if execErr != nil {
		t.Fatalf("expected crop to succeed, got %v", execErr)
	}

	width, height := decodePNGSize(t, item.OutputPath)
	if width != 20 || height != 20 {
		t.Fatalf("expected an exact 20x20 output box, got %dx%d", width, height)
	}
}

func TestCropExecutionAppliesRotation(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 120, 90)

	cases := []struct {
		name         string
		rotate       int
		outputWidth  int
		wantedWidth  int
		wantedHeight int
	}{
		{name: "quarter turn", rotate: 90, wantedWidth: 30, wantedHeight: 40},
		{name: "half turn", rotate: 180, wantedWidth: 40, wantedHeight: 30},
		{name: "quarter turn with resize", rotate: 90, outputWidth: 15, wantedWidth: 15, wantedHeight: 20},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output := filepath.Join(dir, "cropped_"+testCase.name+".png")
			options := cropOptions(10, 10, 40, 30)
			options["rotate"] = testCase.rotate
			if testCase.outputWidth > 0 {
				options["outputWidth"] = testCase.outputWidth
			}
			options["outputPath"] = output

			tool := NewCropTool()
			item, execErr := tool.ExecuteSingle(context.Background(), cropTestRequest("single", []string{input}, options))
			if execErr != nil {
				t.Fatalf("expected crop to succeed, got %v", execErr)
			}

			width, height := decodePNGSize(t, item.OutputPath)
			if width != testCase.wantedWidth || height != testCase.wantedHeight {
				t.Fatalf("expected %dx%d output, got %dx%d", testCase.wantedWidth, testCase.wantedHeight, width, height)
			}
		})
	}
}

func TestCropExecutionAppliesFlips(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "halves.png")
	writeHalfPNG(t, input, 40, 20)
	inputBytes := readFixtureBytes(t, input)
	if pixel := pixelAt(t, inputBytes, 0, 0); pixel.R < 200 {
		t.Fatalf("expected the fixture to start with a red half, got %+v", pixel)
	}

	tool := NewCropTool()

	t.Run("flipH mirrors left and right", func(t *testing.T) {
		options := cropOptions(0, 0, 40, 20)
		options["flipH"] = true
		options["outputPath"] = filepath.Join(dir, "flip_h.png")

		item, execErr := tool.ExecuteSingle(context.Background(), cropTestRequest("single", []string{input}, options))
		if execErr != nil {
			t.Fatalf("expected crop to succeed, got %v", execErr)
		}

		if pixel := pixelAt(t, readFixtureBytes(t, item.OutputPath), 0, 0); pixel.B < 200 || pixel.R > 60 {
			t.Fatalf("expected flipH to move the blue half to the left, got %+v", pixel)
		}
	})

	t.Run("flipV keeps left and right", func(t *testing.T) {
		options := cropOptions(0, 0, 40, 20)
		options["flipV"] = true
		options["outputPath"] = filepath.Join(dir, "flip_v.png")

		item, execErr := tool.ExecuteSingle(context.Background(), cropTestRequest("single", []string{input}, options))
		if execErr != nil {
			t.Fatalf("expected crop to succeed, got %v", execErr)
		}

		if pixel := pixelAt(t, readFixtureBytes(t, item.OutputPath), 0, 0); pixel.R < 200 || pixel.B > 60 {
			t.Fatalf("expected flipV to keep the red half on the left, got %+v", pixel)
		}
	})
}
