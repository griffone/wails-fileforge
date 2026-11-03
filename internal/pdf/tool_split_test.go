package pdf

import (
	"context"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"fileforge-desktop/internal/models"
)

func writePDFFixture(t *testing.T, path string) {
	t.Helper()

	pngPath := filepath.Join(t.TempDir(), "page.png")
	img := image.NewRGBA(image.Rect(0, 0, 16, 16))
	for y := 0; y < 16; y++ {
		for x := 0; x < 16; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x * 8), G: uint8(y * 8), B: 90, A: 255})
		}
	}

	file, err := os.Create(pngPath)
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

	if err := api.ImportImagesFile([]string{pngPath}, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}

func TestSplitToolExecuteBatchReportsPartialSuccess(t *testing.T) {
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
	writePDFFixture(t, first)
	writePDFFixture(t, second)
	if err := os.WriteFile(broken, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write invalid pdf: %v", err)
	}

	req := models.JobRequestV1{
		ToolID:     ToolIDPDFSplitV1,
		Mode:       "batch",
		InputPaths: []string{first, broken, second},
		Options: map[string]any{
			"outputDir":   outputDir,
			"strategy":    "every_page",
			"perInputDir": false,
		},
	}

	tool := NewSplitTool()
	items, jobErr := tool.ExecuteBatch(context.Background(), req, nil)
	if len(items) != 3 {
		t.Fatalf("expected 3 items, got %d", len(items))
	}
	if jobErr == nil {
		t.Fatal("expected aggregate batch error")
	}
	if jobErr.Code != models.CanonicalErrorCode("PDF_SPLIT_FAILED") {
		t.Fatalf("expected canonical split failure code, got %q", jobErr.Code)
	}
	if jobErr.DetailCode != "PDF_SPLIT_FAILED" {
		t.Fatalf("expected detail code PDF_SPLIT_FAILED, got %q", jobErr.DetailCode)
	}

	fileErrors, ok := jobErr.Details["fileErrors"].([]map[string]any)
	if !ok {
		t.Fatalf("expected fileErrors slice in details, got %T", jobErr.Details["fileErrors"])
	}
	if len(fileErrors) != 1 {
		t.Fatalf("expected 1 file error, got %d", len(fileErrors))
	}
	if fileErrors[0]["path"] != broken {
		t.Fatalf("expected file error path %q, got %v", broken, fileErrors[0]["path"])
	}
	if fileErrors[0]["code"] != "PDF_INVALID_INPUT" {
		t.Fatalf("expected file error code PDF_INVALID_INPUT, got %v", fileErrors[0]["code"])
	}

	if !items[0].Success || items[0].Error != nil {
		t.Fatalf("expected first item to succeed, got %+v", items[0])
	}
	if len(items[0].Outputs) != 1 || items[0].OutputCount != 1 {
		t.Fatalf("expected first item to expose its output, got %+v", items[0])
	}
	if _, statErr := os.Stat(items[0].Outputs[0]); statErr != nil {
		t.Fatalf("expected first item output on disk: %v", statErr)
	}

	if items[1].Success || items[1].Error == nil {
		t.Fatalf("expected second item to fail, got %+v", items[1])
	}

	if !items[2].Success || items[2].Error != nil {
		t.Fatalf("expected third item to succeed, got %+v", items[2])
	}
	if len(items[2].Outputs) != 1 {
		t.Fatalf("expected third item to expose its output, got %+v", items[2])
	}
}
