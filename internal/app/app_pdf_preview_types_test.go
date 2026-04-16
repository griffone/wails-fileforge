package app

import (
	"os"
	"path/filepath"
	"testing"

	_ "fileforge-desktop/internal/models"
)

func TestGetPDFPreviewSourceV1_TildeExpansion(t *testing.T) {
	// create a temp file under home dir via t.TempDir is not feasible for ~ expansion tests,
	// instead create a temp file in os.UserHomeDir()
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("no user home dir")
	}
	tmp := filepath.Join(home, ".fileforge_test_preview.pdf")
	if err := os.WriteFile(tmp, []byte("%PDF-1.4\n%EOF"), 0600); err != nil {
		t.Fatalf("write tmp pdf: %v", err)
	}
	defer os.Remove(tmp)

	app := New()
	// call with ~ prefix
	in := "~" + string(os.PathSeparator) + filepath.Base(tmp)
	resp := app.GetPDFPreviewSourceV1(in)
	if !resp.Success {
		t.Fatalf("expected success, got error: %v", resp.Message)
	}
	if resp.MimeType != "application/pdf" {
		t.Fatalf("unexpected mime: %s", resp.MimeType)
	}
}
