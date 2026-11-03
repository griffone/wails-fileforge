package engine

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestMergeRefusesToOverwriteExistingOutput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestPDF(t, first)
	writeTestPDF(t, second)

	outputPath := filepath.Join(dir, "merged.pdf")
	original := []byte("original output content")
	if err := os.WriteFile(outputPath, original, 0o644); err != nil {
		t.Fatalf("write pre-existing output: %v", err)
	}

	err := Merge(context.Background(), []string{first, second}, outputPath)
	if err == nil {
		t.Fatal("expected merge to refuse an existing output")
	}
	if !IsMergeErrorCode(err, ErrorCodeMergeOutputExists) {
		t.Fatalf("expected code %q, got %v", ErrorCodeMergeOutputExists, err)
	}

	content, readErr := os.ReadFile(outputPath)
	if readErr != nil {
		t.Fatalf("read output: %v", readErr)
	}
	if !bytes.Equal(content, original) {
		t.Fatalf("expected output to be untouched, got %q", string(content))
	}
}

func TestValidateMergePathsRejectsExistingOutput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestPDF(t, first)
	writeTestPDF(t, second)

	outputPath := filepath.Join(dir, "merged.pdf")
	if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
		t.Fatalf("write pre-existing output: %v", err)
	}

	validationErr := ValidateMergePaths([]string{first, second}, outputPath)
	if validationErr == nil {
		t.Fatal("expected validation to reject an existing output")
	}
	if validationErr.Code != ErrorCodeMergeOutputExists {
		t.Fatalf("expected code %q, got %q", ErrorCodeMergeOutputExists, validationErr.Code)
	}
}

func TestMergeSucceedsWhenOutputDoesNotExist(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestPDF(t, first)
	writeTestPDF(t, second)

	outputPath := filepath.Join(dir, "merged.pdf")
	if err := Merge(context.Background(), []string{first, second}, outputPath); err != nil {
		t.Fatalf("expected merge to succeed, got %v", err)
	}

	info, statErr := os.Stat(outputPath)
	if statErr != nil {
		t.Fatalf("expected merged output to exist: %v", statErr)
	}
	if info.Size() == 0 {
		t.Fatal("expected merged output to be non-empty")
	}
}
