package app

import (
	"encoding/base64"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGetPDFPreviewSourceV1_Success(t *testing.T) {
	app := New()

	dir := t.TempDir()
	pdfPath := filepath.Join(dir, "sample.pdf")
	content := []byte("%PDF-1.4\n1 0 obj\n<<>>\nendobj\ntrailer\n<<>>\n%%EOF")
	if err := os.WriteFile(pdfPath, content, 0o600); err != nil {
		t.Fatalf("write pdf fixture: %v", err)
	}

	res := app.GetPDFPreviewSourceV1(pdfPath)
	if !res.Success {
		t.Fatalf("expected success, got error: %+v", res.Error)
	}
	if res.MimeType != "application/pdf" {
		t.Fatalf("unexpected mime type: %s", res.MimeType)
	}
	if strings.TrimSpace(res.DataBase64) == "" {
		t.Fatalf("expected dataBase64 content")
	}

	decoded, err := base64.StdEncoding.DecodeString(res.DataBase64)
	if err != nil {
		t.Fatalf("decode base64: %v", err)
	}
	if string(decoded) != string(content) {
		t.Fatalf("decoded payload mismatch")
	}
}

func TestGetPDFPreviewSourceV1_ValidationErrors(t *testing.T) {
	app := New()

	t.Run("empty path", func(t *testing.T) {
		res := app.GetPDFPreviewSourceV1("  ")
		if res.Success {
			t.Fatalf("expected validation error")
		}
		if res.Error == nil || res.Error.Code != "PDF_PREVIEW_INVALID_PATH" {
			t.Fatalf("expected PDF_PREVIEW_INVALID_PATH, got %+v", res.Error)
		}
	})

	t.Run("non-pdf extension", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "input.txt")
		if err := os.WriteFile(filePath, []byte("hello"), 0o600); err != nil {
			t.Fatalf("write fixture: %v", err)
		}

		res := app.GetPDFPreviewSourceV1(filePath)
		if res.Success {
			t.Fatalf("expected validation error")
		}
		if res.Error == nil || res.Error.Code != "PDF_PREVIEW_NOT_PDF" {
			t.Fatalf("expected PDF_PREVIEW_NOT_PDF, got %+v", res.Error)
		}
	})

	t.Run("directory path", func(t *testing.T) {
		dir := t.TempDir()
		dirAsPDF := filepath.Join(dir, "folder.pdf")
		if err := os.Mkdir(dirAsPDF, 0o755); err != nil {
			t.Fatalf("mkdir dir fixture: %v", err)
		}

		res := app.GetPDFPreviewSourceV1(dirAsPDF)
		if res.Success {
			t.Fatalf("expected read/validation error")
		}
		if res.Error == nil || res.Error.Code != "PDF_PREVIEW_INVALID_PATH" {
			t.Fatalf("expected PDF_PREVIEW_INVALID_PATH, got %+v", res.Error)
		}
	})

	t.Run("too large", func(t *testing.T) {
		dir := t.TempDir()
		filePath := filepath.Join(dir, "big.pdf")
		large := make([]byte, 8*1024*1024+1)
		for i := range large {
			large[i] = 'A'
		}
		if err := os.WriteFile(filePath, large, 0o600); err != nil {
			t.Fatalf("write large file: %v", err)
		}

		res := app.GetPDFPreviewSourceV1(filePath)
		if res.Success {
			t.Fatalf("expected size validation error")
		}
		if res.Error == nil || res.Error.Code != "PDF_PREVIEW_TOO_LARGE" {
			t.Fatalf("expected PDF_PREVIEW_TOO_LARGE, got %+v", res.Error)
		}
	})

	t.Run("read fail", func(t *testing.T) {
		dir := t.TempDir()
		missing := filepath.Join(dir, "missing.pdf")

		res := app.GetPDFPreviewSourceV1(missing)
		if res.Success {
			t.Fatalf("expected read failure")
		}
		if res.Error == nil || res.Error.Code != "PDF_PREVIEW_READ_FAILED" {
			t.Fatalf("expected PDF_PREVIEW_READ_FAILED, got %+v", res.Error)
		}
	})
}
