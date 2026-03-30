package image

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"fileforge-desktop/internal/models"
)

func TestAnnotateToolValidateOperations(t *testing.T) {
	tool := NewAnnotateTool()
	input := writeSolidPNGFixture(t, 40, 30, color.NRGBA{R: 200, G: 200, B: 200, A: 255}, "annotate-validate.png")

	t.Run("rejects unsupported operation", func(t *testing.T) {
		err := tool.Validate(context.Background(), models.JobRequestV1{
			ToolID:     ToolIDImageAnnotateV1,
			Mode:       "single",
			InputPaths: []string{input},
			Options: map[string]any{
				"operations": []any{
					map[string]any{"type": "circle", "x": float64(1), "y": float64(1), "width": float64(5), "height": float64(5)},
				},
			},
		})
		if err == nil {
			t.Fatalf("expected validation error")
		}
		if err.DetailCode != "IMAGE_ANNOTATE_OPERATION_UNSUPPORTED" {
			t.Fatalf("expected IMAGE_ANNOTATE_OPERATION_UNSUPPORTED, got %+v", err)
		}
	})

	t.Run("rejects blur intensity out of range", func(t *testing.T) {
		err := tool.Validate(context.Background(), models.JobRequestV1{
			ToolID:     ToolIDImageAnnotateV1,
			Mode:       "single",
			InputPaths: []string{input},
			Options: map[string]any{
				"operations": []any{
					map[string]any{"type": "blur", "x": float64(1), "y": float64(1), "width": float64(10), "height": float64(10), "blurIntensity": float64(101)},
				},
			},
		})
		if err == nil {
			t.Fatalf("expected validation error")
		}
		if err.DetailCode != "IMAGE_ANNOTATE_BLUR_INTENSITY_INVALID" {
			t.Fatalf("expected IMAGE_ANNOTATE_BLUR_INTENSITY_INVALID, got %+v", err)
		}
	})

	t.Run("accepts multiline text and stroke tools", func(t *testing.T) {
		err := tool.Validate(context.Background(), models.JobRequestV1{
			ToolID:     ToolIDImageAnnotateV1,
			Mode:       "single",
			InputPaths: []string{input},
			Options: map[string]any{
				"operations": []any{
					map[string]any{"type": "text", "x": float64(3), "y": float64(3), "text": "line 1\nline 2", "fontSize": float64(18), "color": "#ff0000"},
					map[string]any{"type": "rect", "x": float64(2), "y": float64(2), "width": float64(15), "height": float64(10), "strokeWidth": float64(2), "opacity": float64(0.7), "color": "#00ff00"},
					map[string]any{"type": "arrow", "x": float64(5), "y": float64(5), "x2": float64(20), "y2": float64(15), "strokeWidth": float64(3), "opacity": float64(0.8), "color": "#0000ff"},
				},
			},
		})
		if err != nil {
			t.Fatalf("expected valid operations, got %+v", err)
		}
	})
}

func TestAnnotateToolExecuteBatch_PartialSuccess(t *testing.T) {
	tool := NewAnnotateTool()
	inputA := writeSolidPNGFixture(t, 30, 30, color.NRGBA{R: 240, G: 240, B: 240, A: 255}, "a.png")
	inputB := writeSolidPNGFixture(t, 8, 8, color.NRGBA{R: 240, G: 240, B: 240, A: 255}, "b.png")
	outDir := filepath.Join(t.TempDir(), "annotated")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output: %v", err)
	}

	req := models.JobRequestV1{
		ToolID:     ToolIDImageAnnotateV1,
		Mode:       "batch",
		InputPaths: []string{inputA, inputB},
		OutputDir:  outDir,
		Options: map[string]any{
			"operations": []any{
				map[string]any{"type": "blur", "x": float64(0), "y": float64(0), "width": float64(10), "height": float64(10), "blurIntensity": float64(30)},
			},
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
		t.Fatalf("expected second item failure, got %+v", items[1])
	}
	if firstErr == nil {
		t.Fatalf("expected firstErr for mixed batch")
	}
	if firstErr.DetailCode != "IMAGE_ANNOTATE_OUT_OF_BOUNDS" {
		t.Fatalf("expected IMAGE_ANNOTATE_OUT_OF_BOUNDS, got %+v", firstErr)
	}
}

func writeSolidPNGFixture(t *testing.T, width, height int, fill color.NRGBA, filename string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filename)

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
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
