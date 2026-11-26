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
	"strings"
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

// TestRunToolMergeHonorsExplicitOutputPath proves a listed outputPath is not
// just accepted: the merge actually writes to the requested file.
func TestRunToolMergeHonorsExplicitOutputPath(t *testing.T) {
	isolateEnvironment(t)

	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writePDFFixture(t, first)
	writePDFFixture(t, second)

	chosen := filepath.Join(dir, "chosen.pdf")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"run", "tool", "tool.pdf.merge",
		"--input", first,
		"--input", second,
		"--option", "outputPath=" + chosen,
		"--json",
	}, &stdout, &stderr)
	if code != exitSuccess {
		t.Fatalf("expected exit 0, got %d\nstdout: %s\nstderr: %s", code, stdout.String(), stderr.String())
	}

	var report struct {
		Success bool `json:"success"`
		Outputs []struct {
			Path string `json:"path"`
		} `json:"outputs"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &report); err != nil {
		t.Fatalf("decode json %q: %v", stdout.String(), err)
	}
	if !report.Success || len(report.Outputs) != 1 || report.Outputs[0].Path != chosen {
		t.Fatalf("expected the explicit output %q, got %#v", chosen, report)
	}
	if _, err := os.Stat(chosen); err != nil {
		t.Fatalf("explicit output does not exist: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "first_merged.pdf")); !os.IsNotExist(err) {
		t.Fatalf("expected no auto-named output, got %v", err)
	}
}

// TestRunSplitRejectsOutputPath proves a tool that never reads outputPath now
// fails instead of silently ignoring the option.
func TestRunSplitRejectsOutputPath(t *testing.T) {
	isolateEnvironment(t)

	dir := t.TempDir()
	ignored := filepath.Join(dir, "ignored.pdf")
	var stdout, stderr bytes.Buffer
	code := run(context.Background(), []string{
		"run", "tool", "tool.pdf.split",
		"--input", "whatever.pdf",
		"--option", "outputPath=" + ignored,
		"--json",
	}, &stdout, &stderr)
	if code != exitValidation {
		t.Fatalf("expected exit 2, got %d (stderr=%s)", code, stderr.String())
	}

	var decoded struct {
		Success bool `json:"success"`
		Error   *struct {
			DetailCode string `json:"detail_code"`
			Message    string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(stdout.Bytes(), &decoded); err != nil {
		t.Fatalf("decode json %q: %v", stdout.String(), err)
	}
	if decoded.Success || decoded.Error == nil || decoded.Error.DetailCode != "TOOL_OPTION_UNKNOWN" {
		t.Fatalf("unexpected report: %#v", decoded)
	}
	for _, want := range []string{"outputPath", "not supported", "outputDir", "strategy"} {
		if !strings.Contains(decoded.Error.Message, want) {
			t.Fatalf("message %q must name %q", decoded.Error.Message, want)
		}
	}
	if _, err := os.Stat(ignored); !os.IsNotExist(err) {
		t.Fatalf("rejected option must not write an output, got %v", err)
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
