package image

import (
	"context"
	"encoding/base64"
	stdimg "image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/registry"

	"github.com/h2non/bimg"
)

func TestValidateRatioPreset(t *testing.T) {
	tests := []struct {
		name      string
		preset    string
		width     int
		height    int
		wantError bool
		wantCode  string
	}{
		{name: "free allows any ratio", preset: "free", width: 7, height: 5, wantError: false},
		{name: "1:1 valid", preset: "1:1", width: 8, height: 8, wantError: false},
		{name: "4:3 valid", preset: "4:3", width: 12, height: 9, wantError: false},
		{name: "16:9 valid", preset: "16:9", width: 32, height: 18, wantError: false},
		{name: "invalid preset", preset: "3:2", width: 6, height: 4, wantError: true, wantCode: "VALIDATION_INVALID_INPUT"},
		{name: "ratio mismatch", preset: "1:1", width: 10, height: 9, wantError: true, wantCode: "VALIDATION_INVALID_INPUT"},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := validateRatioPreset(tt.preset, tt.width, tt.height)
			if tt.wantError && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %+v", err)
			}
			if tt.wantError && err != nil && err.Code != tt.wantCode {
				t.Fatalf("expected code %s, got %s", tt.wantCode, err.Code)
			}
		})
	}
}

func TestValidateBoundsAgainstSize(t *testing.T) {
	tests := []struct {
		name      string
		x         int
		y         int
		width     int
		height    int
		imgW      int
		imgH      int
		wantError bool
	}{
		{name: "inside bounds", x: 0, y: 0, width: 1, height: 1, imgW: 10, imgH: 10, wantError: false},
		{name: "bottom-right edge valid", x: 8, y: 8, width: 2, height: 2, imgW: 10, imgH: 10, wantError: false},
		{name: "x out of bounds", x: 9, y: 0, width: 2, height: 1, imgW: 10, imgH: 10, wantError: true},
		{name: "y out of bounds", x: 0, y: 9, width: 1, height: 2, imgW: 10, imgH: 10, wantError: true},
		{name: "negative coordinate", x: -1, y: 0, width: 1, height: 1, imgW: 10, imgH: 10, wantError: true},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			err := validateBoundsAgainstSize(tt.x, tt.y, tt.width, tt.height, tt.imgW, tt.imgH)
			if tt.wantError && err == nil {
				t.Fatalf("expected error")
			}
			if !tt.wantError && err != nil {
				t.Fatalf("unexpected error: %+v", err)
			}
		})
	}
}

func TestCropToolExecuteSingle_AutoOutputAndCollisionSuffix(t *testing.T) {
	input := writePNGFixture(t, 12, 8, "input.png")
	tool := NewCropTool()

	req := models.JobRequestV1{
		ToolID:     ToolIDImageCropV1,
		Mode:       "single",
		InputPaths: []string{input},
		Options: map[string]any{
			"x":           float64(1),
			"y":           float64(1),
			"width":       float64(4),
			"height":      float64(3),
			"ratioPreset": "4:3",
		},
	}

	itemA, errA := tool.ExecuteSingle(context.Background(), req)
	if errA != nil {
		t.Fatalf("expected first crop success, got %+v", errA)
	}
	if !itemA.Success {
		t.Fatalf("expected first item success, got %+v", itemA)
	}
	if filepath.Base(itemA.OutputPath) != "input_cropped.png" {
		t.Fatalf("expected auto output input_cropped.png, got %s", itemA.OutputPath)
	}

	itemB, errB := tool.ExecuteSingle(context.Background(), req)
	if errB != nil {
		t.Fatalf("expected second crop success, got %+v", errB)
	}
	if filepath.Base(itemB.OutputPath) != "input_cropped-2.png" {
		t.Fatalf("expected collision suffix output input_cropped-2.png, got %s", itemB.OutputPath)
	}
}

func TestCropToolExecuteBatch_PartialSuccessOutOfBounds(t *testing.T) {
	tool := NewCropTool()
	inputA := writePNGFixture(t, 10, 10, "a.png")
	inputB := writePNGFixture(t, 2, 2, "b.png")
	outputDir := filepath.Join(t.TempDir(), "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	req := models.JobRequestV1{
		ToolID:     ToolIDImageCropV1,
		Mode:       "batch",
		InputPaths: []string{inputA, inputB},
		OutputDir:  outputDir,
		Options: map[string]any{
			"x":           float64(0),
			"y":           float64(0),
			"width":       float64(3),
			"height":      float64(3),
			"ratioPreset": "1:1",
		},
	}

	items, firstErr := tool.ExecuteBatch(context.Background(), req, nil)
	if len(items) != 2 {
		t.Fatalf("expected 2 batch items, got %d", len(items))
	}

	if !items[0].Success {
		t.Fatalf("expected first item success, got %+v", items[0])
	}
	if items[1].Success {
		t.Fatalf("expected second item failure (out-of-bounds), got %+v", items[1])
	}
	if items[1].Error == nil {
		t.Fatalf("expected second item error")
	}
	if firstErr == nil {
		t.Fatalf("expected firstErr for mixed batch")
	}
	if firstErr.DetailCode != "IMAGE_CROP_OUT_OF_BOUNDS" {
		t.Fatalf("expected IMAGE_CROP_OUT_OF_BOUNDS detail code, got %+v", firstErr)
	}
}

func TestCropToolGetImageCropPreview_MatchesRequestedDimensions(t *testing.T) {
	tool := NewCropTool()
	input := writePNGFixture(t, 20, 12, "preview.png")

	preview := tool.GetImageCropPreview(models.ImageCropPreviewRequestV1{
		InputPath:   input,
		X:           2,
		Y:           1,
		Width:       8,
		Height:      6,
		RatioPreset: "4:3",
	})

	if !preview.Success {
		t.Fatalf("expected preview success, got %+v", preview.Error)
	}

	decoded, err := base64.StdEncoding.DecodeString(preview.DataBase64)
	if err != nil {
		t.Fatalf("decode base64 preview: %v", err)
	}

	size, err := bimg.NewImage(decoded).Size()
	if err != nil {
		t.Fatalf("preview size: %v", err)
	}
	if size.Width != 8 || size.Height != 6 {
		t.Fatalf("expected preview dimensions 8x6, got %dx%d", size.Width, size.Height)
	}
}

func TestImageCropToolAppearsInRegistryCatalog(t *testing.T) {
	r := registry.NewRegistry()
	if err := r.RegisterToolV2(NewCropTool()); err != nil {
		t.Fatalf("register crop tool: %v", err)
	}

	entries := r.ListToolsV2(context.Background())
	found := false
	for _, entry := range entries {
		if entry.Manifest.ToolID == ToolIDImageCropV1 {
			found = true
			if !entry.Manifest.SupportsSingle || !entry.Manifest.SupportsBatch {
				t.Fatalf("unexpected manifest capabilities: %+v", entry.Manifest)
			}
		}
	}

	if !found {
		t.Fatalf("expected tool %s in catalog", ToolIDImageCropV1)
	}
}

func writePNGFixture(t *testing.T, width, height int, filename string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filename)

	img := stdimg.NewRGBA(stdimg.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 7), G: uint8(y * 9), B: 120, A: 255})
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode fixture png: %v", err)
	}

	return path
}
