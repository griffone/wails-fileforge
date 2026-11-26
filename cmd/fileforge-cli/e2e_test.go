package main

import (
	"bytes"
	"context"
	"encoding/json"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// TestRunToolMergeEndToEnd merges two generated PDFs through the real CLI
// composition and asserts the output file and the zero exit code.
func TestRunToolMergeEndToEnd(t *testing.T) {
	isolateEnvironment(t)

	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"run", "tool", "tool.pdf.merge",
		"--input", first,
		"--input", second,
		"--out", dir,
		"--json",
	}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	var report struct {
		JobID   string `json:"jobId"`
		Status  string `json:"status"`
		Success bool   `json:"success"`
		Outputs []struct {
			Path      string `json:"path"`
			SizeBytes int64  `json:"sizeBytes"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode json %q: %v", stdout.String(), err)
	}
	if report.JobID == "" || !report.Success || report.Status != "success" {
		t.Fatalf("unexpected report: %#v", report)
	}
	if len(report.Outputs) != 1 {
		t.Fatalf("expected one output, got %#v", report.Outputs)
	}

	output := report.Outputs[0]
	if want := filepath.Join(dir, "first_merged.pdf"); output.Path != want {
		t.Fatalf("expected output %q, got %q", want, output.Path)
	}
	info, err := os.Stat(output.Path)
	if err != nil {
		t.Fatalf("output does not exist: %v", err)
	}
	if info.Size() == 0 || output.SizeBytes != info.Size() {
		t.Fatalf("unexpected size: stat=%d report=%d", info.Size(), output.SizeBytes)
	}
}

// writePDFFixture creates a valid one-page PDF from a generated PNG.
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

	if err := api.ImportImagesFile(context.Background(), []string{pngPath}, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}
