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

func TestAnnotateToolIntegration_PipelineOrderDeterministic(t *testing.T) {
	tool := NewAnnotateTool()
	input := writePatternPNGFixture(t, 40, 40, "annotate-order.png")
	outPath := filepath.Join(t.TempDir(), "annotated.png")

	item, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDImageAnnotateV1,
		Mode:       "single",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputPath": outPath,
			"operations": []any{
				map[string]any{"type": "blur", "x": float64(4), "y": float64(4), "width": float64(20), "height": float64(20), "blurIntensity": float64(50)},
				map[string]any{"type": "redact", "x": float64(8), "y": float64(8), "width": float64(10), "height": float64(10), "color": "#112233"},
				map[string]any{"type": "rect", "x": float64(2), "y": float64(2), "width": float64(30), "height": float64(30), "strokeWidth": float64(2), "opacity": float64(1), "color": "#ff0000"},
			},
		},
	})

	if jobErr != nil {
		t.Fatalf("expected success, got %+v", jobErr)
	}
	if !item.Success {
		t.Fatalf("expected successful item, got %+v", item)
	}

	outFile, err := os.Open(outPath)
	if err != nil {
		t.Fatalf("open output: %v", err)
	}
	defer func() { _ = outFile.Close() }()

	decoded, err := png.Decode(outFile)
	if err != nil {
		t.Fatalf("decode output png: %v", err)
	}

	insideRedact := color.NRGBAModel.Convert(decoded.At(12, 12)).(color.NRGBA)
	if insideRedact.R != 0x11 || insideRedact.G != 0x22 || insideRedact.B != 0x33 {
		t.Fatalf("expected redact color #112233 at (12,12), got %#v", insideRedact)
	}

	insideStroke := color.NRGBAModel.Convert(decoded.At(2, 10)).(color.NRGBA)
	if insideStroke.R < 200 {
		t.Fatalf("expected strong red stroke around rect, got %#v", insideStroke)
	}
}

func TestAnnotateToolIntegration_BlurAndRedactAreaSpecificSmoke(t *testing.T) {
	tool := NewAnnotateTool()
	input := writePatternPNGFixture(t, 36, 24, "annotate-smoke.png")
	outPath := filepath.Join(t.TempDir(), "annotate-smoke-out.png")

	_, jobErr := tool.ExecuteSingle(context.Background(), models.JobRequestV1{
		ToolID:     ToolIDImageAnnotateV1,
		Mode:       "single",
		InputPaths: []string{input},
		Options: map[string]any{
			"outputPath": outPath,
			"operations": []any{
				map[string]any{"type": "blur", "x": float64(0), "y": float64(0), "width": float64(12), "height": float64(12), "blurIntensity": float64(90)},
				map[string]any{"type": "redact", "x": float64(20), "y": float64(8), "width": float64(10), "height": float64(8), "color": "#000000"},
			},
		},
	})

	if jobErr != nil {
		t.Fatalf("expected success, got %+v", jobErr)
	}

	inputImg := mustReadPNG(t, input)
	outputImg := mustReadPNG(t, outPath)

	inBefore := color.NRGBAModel.Convert(inputImg.At(4, 4)).(color.NRGBA)
	inAfter := color.NRGBAModel.Convert(outputImg.At(4, 4)).(color.NRGBA)
	if inBefore == inAfter {
		t.Fatalf("expected blurred region pixel to change at (4,4), got %#v", inAfter)
	}

	redacted := color.NRGBAModel.Convert(outputImg.At(22, 10)).(color.NRGBA)
	if redacted.R != 0 || redacted.G != 0 || redacted.B != 0 {
		t.Fatalf("expected redacted pixel black at (22,10), got %#v", redacted)
	}
}

func writePatternPNGFixture(t *testing.T, width, height int, filename string) string {
	t.Helper()
	dir := t.TempDir()
	path := filepath.Join(dir, filename)

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8((x*9)%255 + 1), G: uint8((y*11)%255 + 1), B: uint8(((x+y)*7)%255 + 1), A: 255})
		}
	}

	f, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	defer func() { _ = f.Close() }()

	if err := png.Encode(f, img); err != nil {
		t.Fatalf("encode fixture: %v", err)
	}

	return path
}

func mustReadPNG(t *testing.T, path string) image.Image {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatalf("open png: %v", err)
	}
	defer func() { _ = f.Close() }()

	img, err := png.Decode(f)
	if err != nil {
		t.Fatalf("decode png: %v", err)
	}
	return img
}
