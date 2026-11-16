package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
)

// minimalValidPDF returns a small but structurally valid one-page PDF. It
// keeps Ghostscript candidates smaller than a pdfcpu-optimized image import so
// tests can exercise candidate selection deterministically.
func minimalValidPDF() []byte {
	var builder strings.Builder
	builder.WriteString("%PDF-1.4\n")
	offsets := make([]int, 5)
	addObject := func(index int, body string) {
		offsets[index] = builder.Len()
		fmt.Fprintf(&builder, "%d 0 obj\n%s\nendobj\n", index, body)
	}
	addObject(1, "<< /Type /Catalog /Pages 2 0 R >>")
	addObject(2, "<< /Type /Pages /Kids [3 0 R] /Count 1 >>")
	addObject(3, "<< /Type /Page /Parent 2 0 R /MediaBox [0 0 612 792] /Resources << >> /Contents 4 0 R >>")
	addObject(4, "<< /Length 0 >>\nstream\n\nendstream")

	xrefOffset := builder.Len()
	builder.WriteString("xref\n0 5\n0000000000 65535 f \n")
	for index := 1; index < 5; index++ {
		fmt.Fprintf(&builder, "%010d 00000 n \n", offsets[index])
	}
	fmt.Fprintf(&builder, "trailer\n<< /Size 5 /Root 1 0 R >>\nstartxref\n%d\n%%%%EOF\n", xrefOffset)

	return []byte(builder.String())
}

func stubGhostscript(t *testing.T, available bool, runner func(ctx context.Context, inputPath, outputPath, preset string) error) {
	t.Helper()

	originalRunner := GhostscriptRunner
	originalLookPath := GhostscriptLookPath
	t.Cleanup(func() {
		GhostscriptRunner = originalRunner
		GhostscriptLookPath = originalLookPath
	})

	GhostscriptLookPath = func() bool { return available }
	if runner != nil {
		GhostscriptRunner = runner
	}
}

func writeStubBytes(t *testing.T, path, content string) error {
	t.Helper()

	return os.WriteFile(path, []byte(content), 0o644)
}

func readPDFInfoForTest(t *testing.T, path string) *pdfcpu.PDFInfo {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open pdf: %v", err)
	}
	defer file.Close()

	info, err := api.PDFInfo(file, filepath.Base(path), nil, false, nil)
	if err != nil {
		t.Fatalf("read pdf info: %v", err)
	}

	return info
}

func TestOptimizeWithOptionsLargeTargetKeepsPDFCPUOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.pdf")
	output := filepath.Join(dir, "output.pdf")
	writeTestPDF(t, input)

	stubGhostscript(t, true, func(context.Context, string, string, string) error {
		t.Fatal("ghostscript must not run when the pdfcpu output already meets the target")
		return nil
	})

	outcome, err := OptimizeWithOptions(context.Background(), input, output, OptimizeOptions{
		Compress:        true,
		TargetSizeBytes: 100 * 1024 * 1024,
	})
	if err != nil {
		t.Fatalf("expected optimize to succeed, got %v", err)
	}
	if !outcome.TargetMet {
		t.Fatalf("expected a large target to be met, got %+v", outcome)
	}
	if outcome.UsedGhostscript {
		t.Fatal("expected Ghostscript to be skipped for a large target")
	}
	if outcome.Strategy != OptimizeStrategyPDFCPU {
		t.Fatalf("expected pdfcpu strategy, got %q", outcome.Strategy)
	}
	if err := api.ValidateFile(output, nil); err != nil {
		t.Fatalf("expected a valid output PDF: %v", err)
	}
}

func TestOptimizeWithOptionsTinyTargetPicksSmallestCandidate(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.pdf")
	output := filepath.Join(dir, "output.pdf")
	writeTestPDF(t, input)

	var presets []string
	stubGhostscript(t, true, func(_ context.Context, _ string, outputPath, preset string) error {
		presets = append(presets, preset)
		content := "not tiny enough"
		if preset == "screen" {
			content = "tiny"
		}
		return writeStubBytes(t, outputPath, content)
	})

	outcome, err := OptimizeWithOptions(context.Background(), input, output, OptimizeOptions{
		Compress:        true,
		TargetSizeBytes: int64(len("tiny")),
	})
	if err != nil {
		t.Fatalf("expected optimize to succeed, got %v", err)
	}
	if len(presets) != 2 || presets[0] != "ebook" || presets[1] != "screen" {
		t.Fatalf("expected ebook then screen, got %v", presets)
	}
	if !outcome.TargetMet {
		t.Fatalf("expected the smallest candidate to meet the target, got %+v", outcome)
	}
	if !outcome.UsedGhostscript || outcome.Strategy != OptimizeStrategyGhostscript {
		t.Fatalf("expected the Ghostscript candidate to win, got %+v", outcome)
	}

	content, readErr := os.ReadFile(output)
	if readErr != nil {
		t.Fatalf("read output: %v", readErr)
	}
	if string(content) != "tiny" {
		t.Fatalf("expected the smallest candidate, got %q", string(content))
	}
	if outcome.SizeBytes != int64(len("tiny")) {
		t.Fatalf("expected reported size %d, got %d", len("tiny"), outcome.SizeBytes)
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read dir: %v", readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fileforge-optimize-") {
			t.Fatalf("expected temporary files to be cleaned up, found %s", entry.Name())
		}
	}
}

func TestOptimizeWithOptionsStopsWhenEbookMeetsTarget(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.pdf")
	output := filepath.Join(dir, "output.pdf")
	writeTestPDF(t, input)

	var presets []string
	stubGhostscript(t, true, func(_ context.Context, _ string, outputPath, preset string) error {
		presets = append(presets, preset)
		return writeStubBytes(t, outputPath, "tiny")
	})

	outcome, err := OptimizeWithOptions(context.Background(), input, output, OptimizeOptions{
		Compress:        true,
		TargetSizeBytes: int64(len("tiny")),
	})
	if err != nil {
		t.Fatalf("expected optimize to succeed, got %v", err)
	}
	if len(presets) != 1 || presets[0] != "ebook" {
		t.Fatalf("expected only ebook to run, got %v", presets)
	}
	if !outcome.TargetMet {
		t.Fatalf("expected the target to be met, got %+v", outcome)
	}
}

func TestOptimizeWithOptionsMissingGhostscriptKeepsPDFCPUBestEffort(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.pdf")
	output := filepath.Join(dir, "output.pdf")
	writeTestPDF(t, input)

	stubGhostscript(t, false, func(context.Context, string, string, string) error {
		t.Fatal("ghostscript runner must not run when gs is unavailable")
		return nil
	})

	outcome, err := OptimizeWithOptions(context.Background(), input, output, OptimizeOptions{
		Compress:        true,
		TargetSizeBytes: 1,
	})
	if err != nil {
		t.Fatalf("expected the pdfcpu best effort to succeed, got %v", err)
	}
	if outcome.GhostscriptAvailable {
		t.Fatal("expected Ghostscript to be reported as unavailable")
	}
	if outcome.UsedGhostscript {
		t.Fatal("expected the pdfcpu output to be kept")
	}
	if outcome.TargetMet {
		t.Fatalf("expected a tiny target to be impossible without gs, got %+v", outcome)
	}
	if err := api.ValidateFile(output, nil); err != nil {
		t.Fatalf("expected a valid pdfcpu output: %v", err)
	}
}

func TestOptimizeWithOptionsAppliesMetadataAfterGhostscript(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.pdf")
	output := filepath.Join(dir, "output.pdf")
	writeTestPDF(t, input)

	tinyPDF := minimalValidPDF()
	tinyPath := filepath.Join(dir, "tiny.pdf")
	if err := os.WriteFile(tinyPath, tinyPDF, 0o644); err != nil {
		t.Fatalf("write tiny pdf: %v", err)
	}
	if err := api.ValidateFile(tinyPath, nil); err != nil {
		t.Fatalf("expected the minimal fixture to be valid: %v", err)
	}

	stubGhostscript(t, true, func(_ context.Context, _ string, outputPath, _ string) error {
		return os.WriteFile(outputPath, tinyPDF, 0o644)
	})

	outcome, err := OptimizeWithOptions(context.Background(), input, output, OptimizeOptions{
		Compress:        true,
		TargetSizeBytes: 1,
		Properties:      map[string]string{"Title": "Field notes"},
	})
	if err != nil {
		t.Fatalf("expected optimize to succeed, got %v", err)
	}
	if !outcome.UsedGhostscript {
		t.Fatalf("expected the Ghostscript path to be used, got %+v", outcome)
	}

	properties := readPDFInfoForTest(t, output)
	if properties.Title != "Field notes" {
		t.Fatalf("expected metadata applied after compression, got %q", properties.Title)
	}
}

func TestOptimizeWithOptionsRejectsNegativeTarget(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "input.pdf")
	output := filepath.Join(dir, "output.pdf")
	writeTestPDF(t, input)

	_, err := OptimizeWithOptions(context.Background(), input, output, OptimizeOptions{
		Compress:        true,
		TargetSizeBytes: -1,
	})
	if err == nil {
		t.Fatal("expected a negative target to fail")
	}
	var optimizeErr *OptimizeError
	if !errors.As(err, &optimizeErr) {
		t.Fatalf("expected an OptimizeError, got %T", err)
	}
	if optimizeErr.Code != ErrorCodeOptimizeTargetInvalid {
		t.Fatalf("expected %q, got %q", ErrorCodeOptimizeTargetInvalid, optimizeErr.Code)
	}
}
