package image

import (
	"context"
	"image"
	"os"
	"path/filepath"
	"testing"
)

// TestUpscaleRealModel runs one real 2x inference through the bridge. It is
// skipped unless both the interpreter and the ONNX model are provided, because
// the model is a consent-driven download and never shipped with the tests.
//
//	FILEFORGE_UPSCALE_TEST_PYTHON=/path/to/onnx-venv/bin/python \
//	FILEFORGE_UPSCALE_TEST_MODEL=/path/to/real_esrgan_general_x4v3.onnx \
//	go test ./internal/image -run TestUpscaleRealModel -count=1 -v
func TestUpscaleRealModel(t *testing.T) {
	python := os.Getenv("FILEFORGE_UPSCALE_TEST_PYTHON")
	model := os.Getenv("FILEFORGE_UPSCALE_TEST_MODEL")
	if python == "" || model == "" {
		t.Skip("set FILEFORGE_UPSCALE_TEST_PYTHON and FILEFORGE_UPSCALE_TEST_MODEL to run the real inference")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "input.png")
	output := filepath.Join(dir, "output.png")
	writeUpscalePNG(t, input, 40, 24)

	outcomes, err := UpscaleImages(context.Background(), nil, python, model, "", []UpscaleItem{
		{InputPath: input, OutputPath: output},
	}, UpscaleOptions{Scale: 2, TileSize: 128, Format: "png"}, nil)
	if err != nil {
		t.Fatalf("real upscale failed: %v", err)
	}
	if len(outcomes) != 1 {
		t.Fatalf("expected one outcome, got %+v", outcomes)
	}
	if outcomes[0].Width != 80 || outcomes[0].Height != 48 {
		t.Fatalf("output dimensions = %dx%d, want 80x48", outcomes[0].Width, outcomes[0].Height)
	}

	file, openErr := os.Open(output)
	if openErr != nil {
		t.Fatalf("open the real output: %v", openErr)
	}
	defer file.Close()

	config, _, decodeErr := image.DecodeConfig(file)
	if decodeErr != nil {
		t.Fatalf("decode the real output: %v", decodeErr)
	}
	if config.Width != 80 || config.Height != 48 {
		t.Fatalf("decoded output = %dx%d, want 80x48", config.Width, config.Height)
	}
	t.Logf("real 2x inference produced %dx%d at %s", config.Width, config.Height, output)
}
