package engine

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jung-kurt/gofpdf"
)

func writeTitledPDF(t *testing.T, path, title string) {
	t.Helper()

	document := gofpdf.New("P", "mm", "A4", "")
	document.AddPage()
	document.SetFont("Helvetica", "", 12)
	document.Cell(40, 10, "Body text")
	document.SetTitle(title, true)
	if err := document.OutputFileAndClose(path); err != nil {
		t.Fatalf("write pdf: %v", err)
	}
}

func TestReadDocumentTitleReturnsTheInfoTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "titled.pdf")
	writeTitledPDF(t, path, "Quarterly Report")

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open pdf: %v", err)
	}
	defer file.Close()

	title, err := ReadDocumentTitle(context.Background(), file, filepath.Base(path))
	if err != nil {
		t.Fatalf("ReadDocumentTitle: %v", err)
	}
	if title != "Quarterly Report" {
		t.Fatalf("title = %q, want %q", title, "Quarterly Report")
	}
}

func TestReadDocumentTitleReturnsEmptyWithoutTitle(t *testing.T) {
	path := filepath.Join(t.TempDir(), "untitled.pdf")
	writeTitledPDF(t, path, "")

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open pdf: %v", err)
	}
	defer file.Close()

	title, err := ReadDocumentTitle(context.Background(), file, filepath.Base(path))
	if err != nil {
		t.Fatalf("ReadDocumentTitle: %v", err)
	}
	if title != "" {
		t.Fatalf("title = %q, want empty", title)
	}
}

func TestReadDocumentTitleReportsBrokenPDF(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken.pdf")
	if err := os.WriteFile(path, []byte("not a pdf"), 0o644); err != nil {
		t.Fatalf("write broken pdf: %v", err)
	}

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open pdf: %v", err)
	}
	defer file.Close()

	if _, err := ReadDocumentTitle(context.Background(), file, filepath.Base(path)); err == nil {
		t.Fatal("ReadDocumentTitle returned no error for a broken PDF")
	}
}
