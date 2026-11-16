package engine

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/jung-kurt/gofpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func writeMultiPagePDF(t *testing.T, path string, pages int) {
	t.Helper()

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetFont("Helvetica", "", 12)
	for i := 1; i <= pages; i++ {
		pdf.AddPage()
		pdf.Cell(0, 10, fmt.Sprintf("Page %d", i))
	}

	if err := pdf.OutputFileAndClose(path); err != nil {
		t.Fatalf("write multi-page pdf fixture: %v", err)
	}
}

func pdfPageCount(t *testing.T, path string) int {
	t.Helper()

	dims, err := api.PageDimsFile(path)
	if err != nil {
		t.Fatalf("read page dimensions from %s: %v", path, err)
	}

	return len(dims)
}

func TestValidatePageSelection(t *testing.T) {
	valid := []string{"", "1", "1-3", "1-3,5", "1-3, 5", "odd", "even", "3-"}
	for _, selection := range valid {
		if err := ValidatePageSelection(selection); err != nil {
			t.Fatalf("expected %q to be valid, got %v", selection, err)
		}
	}

	invalid := []string{"abc", "1,,3", "1-2-3", "page 2", "1-3,"}
	for _, selection := range invalid {
		if err := ValidatePageSelection(selection); err == nil {
			t.Fatalf("expected %q to be rejected", selection)
		}
	}
}

func TestApplyPageSelectionKeepsSelectedPages(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "converted.pdf")
	writeMultiPagePDF(t, outputPath, 5)

	if err := ApplyPageSelection(filepath.Join(dir, "report.docx"), outputPath, "1-3"); err != nil {
		t.Fatalf("expected page selection to succeed, got %v", err)
	}

	if got := pdfPageCount(t, outputPath); got != 3 {
		t.Fatalf("expected 3 pages after selection, got %d", got)
	}
}

func TestApplyPageSelectionEmptyIsNoOp(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "converted.pdf")
	writeMultiPagePDF(t, outputPath, 4)

	if err := ApplyPageSelection(filepath.Join(dir, "report.docx"), outputPath, "  "); err != nil {
		t.Fatalf("expected empty selection to be a no-op, got %v", err)
	}

	if got := pdfPageCount(t, outputPath); got != 4 {
		t.Fatalf("expected 4 untouched pages, got %d", got)
	}
}

func TestApplyPageSelectionOutOfRangeRemovesOutputAndTempFiles(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "converted.pdf")
	writeMultiPagePDF(t, outputPath, 3)

	err := ApplyPageSelection(filepath.Join(dir, "report.docx"), outputPath, "9-12")
	if err == nil {
		t.Fatal("expected out-of-range page selection to fail")
	}

	var selectionErr *PageSelectionError
	if !errors.As(err, &selectionErr) {
		t.Fatalf("expected PageSelectionError, got %T", err)
	}
	if selectionErr.Selection != "9-12" {
		t.Fatalf("expected error to keep the selection, got %q", selectionErr.Selection)
	}

	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the partial output to be removed, got %v", statErr)
	}

	leftovers, globErr := filepath.Glob(filepath.Join(dir, ".fileforge-pages-*"))
	if globErr != nil {
		t.Fatalf("glob temp files: %v", globErr)
	}
	if len(leftovers) != 0 {
		t.Fatalf("expected no temporary files left behind, got %v", leftovers)
	}
}
