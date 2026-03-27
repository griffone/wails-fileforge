package image

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"

	"github.com/h2non/bimg"
)

func TestCropToolIntegration_CropsFixtureWithExactOutputSize(t *testing.T) {
	fixturePath := filepath.Clean(filepath.Join("..", "..", "build", "appicon.png"))
	if _, err := os.Stat(fixturePath); err != nil {
		t.Fatalf("expected fixture at %s: %v", fixturePath, err)
	}

	outDir := t.TempDir()
	outPath := filepath.Join(outDir, "appicon-crop.png")

	tool := NewCropTool()
	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDImageCropV1,
		Mode:       "single",
		InputPaths: []string{fixturePath},
		Options: map[string]any{
			"outputPath":  outPath,
			"x":           float64(4),
			"y":           float64(5),
			"width":       float64(16),
			"height":      float64(10),
			"ratioPreset": "free",
		},
	})

	if jobErr != nil {
		t.Fatalf("expected success, got %+v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected successful item, got %+v", item)
	}

	outputBytes, err := os.ReadFile(outPath)
	if err != nil {
		t.Fatalf("read output: %v", err)
	}

	size, err := bimg.NewImage(outputBytes).Size()
	if err != nil {
		t.Fatalf("output metadata read: %v", err)
	}

	if size.Width != 16 || size.Height != 10 {
		t.Fatalf("expected output dimensions 16x10, got %dx%d", size.Width, size.Height)
	}
}
