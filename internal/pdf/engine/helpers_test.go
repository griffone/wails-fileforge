package engine

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func writeTestPNG(t *testing.T, path string) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, 32, 32))
	for y := 0; y < 32; y++ {
		for x := 0; x < 32; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 4), G: uint8(y * 4), B: 160, A: 255})
		}
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode png: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close png: %v", err)
	}
}

func writeTestPDF(t *testing.T, path string) {
	t.Helper()

	pngPath := filepath.Join(t.TempDir(), "page.png")
	writeTestPNG(t, pngPath)

	if err := api.ImportImagesFile(context.Background(), []string{pngPath}, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}
