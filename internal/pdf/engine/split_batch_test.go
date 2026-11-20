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

func writeInvalidPDF(t *testing.T, path string) {
	t.Helper()

	if err := os.WriteFile(path, []byte("this is not a pdf document"), 0o644); err != nil {
		t.Fatalf("write invalid pdf: %v", err)
	}
}

func TestSplitBatchReportsPartialSuccess(t *testing.T) {
	dir := t.TempDir()
	inputDir := filepath.Join(dir, "in")
	outputDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(inputDir, 0o755); err != nil {
		t.Fatalf("create input dir: %v", err)
	}
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	first := filepath.Join(inputDir, "first.pdf")
	second := filepath.Join(inputDir, "second.pdf")
	broken := filepath.Join(inputDir, "broken.pdf")
	writeTestPDF(t, first)
	writeTestPDF(t, second)
	writeInvalidPDF(t, broken)

	results, err := SplitBatch(context.Background(), []string{first, broken, second}, outputDir, SplitStrategyEveryPage, "", false)
	if err != nil {
		t.Fatalf("SplitBatch returned error: %v", err)
	}
	if len(results) != 3 {
		t.Fatalf("expected 3 results, got %d", len(results))
	}

	if results[0].InputPath != first || results[0].Error != nil {
		t.Fatalf("expected first input to succeed, got %+v", results[0])
	}
	if len(results[0].Outputs) != 1 {
		t.Fatalf("expected 1 output for first input, got %d", len(results[0].Outputs))
	}
	if _, statErr := os.Stat(results[0].Outputs[0]); statErr != nil {
		t.Fatalf("expected first output to exist: %v", statErr)
	}

	if results[1].InputPath != broken {
		t.Fatalf("expected result order to follow input order, got %q", results[1].InputPath)
	}
	if results[1].Error == nil {
		t.Fatal("expected per-item error for invalid pdf")
	}
	if results[1].Error.Code != ErrorCodeInvalidInputPDF {
		t.Fatalf("expected code %q, got %q", ErrorCodeInvalidInputPDF, results[1].Error.Code)
	}
	if len(results[1].Outputs) != 0 {
		t.Fatalf("expected no outputs for invalid pdf, got %v", results[1].Outputs)
	}

	if results[2].InputPath != second || results[2].Error != nil {
		t.Fatalf("expected second input to succeed, got %+v", results[2])
	}
	if len(results[2].Outputs) != 1 {
		t.Fatalf("expected 1 output for second input, got %d", len(results[2].Outputs))
	}
}

func TestValidateSplitBatchRequestIsShapeOnly(t *testing.T) {
	dir := t.TempDir()
	outputDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outputDir, 0o755); err != nil {
		t.Fatalf("create output dir: %v", err)
	}

	valid := filepath.Join(dir, "valid.pdf")
	writeTestPDF(t, valid)
	broken := filepath.Join(dir, "broken.pdf")
	writeInvalidPDF(t, broken)

	if splitErr := ValidateSplitBatchRequest([]string{valid, broken}, outputDir, SplitStrategyEveryPage, "", false); splitErr != nil {
		t.Fatalf("expected shape-only validation to accept an invalid pdf input, got %v", splitErr)
	}

	if splitErr := ValidateSplitBatchRequest([]string{valid}, outputDir, SplitStrategyRanges, "3-1", false); splitErr == nil {
		t.Fatal("expected invalid range syntax to fail batch validation")
	}
}
