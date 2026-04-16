package app

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"testing"
)

// helper to create a small PDF-like file
func writeSmallPDF(t *testing.T, dir, name string) string {
	t.Helper()
	p := filepath.Join(dir, name)
	// minimal PDF header bytes
	data := []byte("%PDF-1.4\n%\u00e2\u00e3\u00cf\u00d3\n")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write temp pdf: %v", err)
	}
	return p
}

func TestGetPDFPreviewSource_FileURIPrefix(t *testing.T) {
	dir := t.TempDir()
	p := writeSmallPDF(t, dir, "test.pdf")

	app := New()

	// craft file URI
	uri := "file://" + p
	res := app.GetPDFPreviewSourceV1(uri)
	if !res.Success {
		t.Fatalf("expected success, got error: %v", res.Message)
	}
	if res.DataBase64 == "" {
		t.Fatalf("expected data, got empty")
	}
	// verify decoded content length > 0
	b, err := base64.StdEncoding.DecodeString(res.DataBase64)
	if err != nil || len(b) == 0 {
		t.Fatalf("invalid base64 data: %v", err)
	}
}

func TestGetPDFPreviewSource_TildeExpansion(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skipf("cannot determine home dir: %v", err)
	}
	// create file under home
	p := filepath.Join(home, "tmp_test_pdf_preview.pdf")
	data := []byte("%PDF-1.4\n%test\n")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatalf("write home pdf: %v", err)
	}
	defer os.Remove(p)

	app := New()
	// path with ~ expanded by our function should resolve to same as explicit
	tilde := "~" + string(os.PathSeparator) + "tmp_test_pdf_preview.pdf"
	res := app.GetPDFPreviewSourceV1(tilde)
	if !res.Success {
		t.Fatalf("expected success for tilde expansion, got: %v", res.Message)
	}
	res2 := app.GetPDFPreviewSourceV1(p)
	if !res2.Success {
		t.Fatalf("expected success for explicit path, got: %v", res2.Message)
	}
}

func TestGetPDFPreviewSource_TooLarge(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "big.pdf")
	// create sparse file > 8MiB
	f, err := os.Create(p)
	if err != nil {
		t.Fatalf("create big file: %v", err)
	}
	const size = 9 * 1024 * 1024
	if err := f.Truncate(size); err != nil {
		f.Close()
		t.Fatalf("truncate: %v", err)
	}
	f.Close()

	app := New()
	res := app.GetPDFPreviewSourceV1(p)
	if res.Success {
		t.Fatalf("expected failure for too large file")
	}
	if res.Error == nil || res.Error.DetailCode != "PDF_PREVIEW_TOO_LARGE" {
		t.Fatalf("expected PDF_PREVIEW_TOO_LARGE, got: %+v", res.Error)
	}
}

func TestGetPDFPreviewSource_NotPDF(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "notpdf.txt")
	if err := os.WriteFile(p, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write txt: %v", err)
	}

	app := New()
	res := app.GetPDFPreviewSourceV1(p)
	if res.Success {
		t.Fatalf("expected failure for non-pdf")
	}
	if res.Error == nil || res.Error.DetailCode != "PDF_PREVIEW_NOT_PDF" {
		t.Fatalf("expected PDF_PREVIEW_NOT_PDF, got: %+v", res.Error)
	}
}
