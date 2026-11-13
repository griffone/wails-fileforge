package engine

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func writeTestMultiPagePDF(t *testing.T, path string, pageCount int) {
	t.Helper()

	pngPaths := make([]string, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		pngPath := filepath.Join(t.TempDir(), fmt.Sprintf("page-%d.png", page+1))
		writeTestPNG(t, pngPath)
		pngPaths = append(pngPaths, pngPath)
	}

	if err := api.ImportImagesFile(pngPaths, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}

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

func TestMergeWithPageRangesSelectsPagesPerInput(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestMultiPagePDF(t, first, 3)
	writeTestMultiPagePDF(t, second, 2)

	outputPath := filepath.Join(dir, "merged.pdf")
	if err := MergeWithPageRanges(context.Background(), []string{first, second}, outputPath, []string{"2", "1-2"}); err != nil {
		t.Fatalf("expected merge with ranges to succeed, got %v", err)
	}

	pageCount, err := api.PageCountFile(outputPath)
	if err != nil {
		t.Fatalf("read merged page count: %v", err)
	}
	if pageCount != 3 {
		t.Fatalf("expected 3 merged pages (1 + 2), got %d", pageCount)
	}
}

func TestMergeWithPageRangesTreatsMissingRangesAsWholeFile(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestMultiPagePDF(t, first, 3)
	writeTestMultiPagePDF(t, second, 2)

	outputPath := filepath.Join(dir, "merged.pdf")
	if err := MergeWithPageRanges(context.Background(), []string{first, second}, outputPath, []string{"3"}); err != nil {
		t.Fatalf("expected merge with partial ranges to succeed, got %v", err)
	}

	pageCount, err := api.PageCountFile(outputPath)
	if err != nil {
		t.Fatalf("read merged page count: %v", err)
	}
	if pageCount != 3 {
		t.Fatalf("expected 3 merged pages (1 selected + 2 whole), got %d", pageCount)
	}
}

func TestMergeWithPageRangesEmptyRangesKeepExistingBehavior(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestMultiPagePDF(t, first, 2)
	writeTestMultiPagePDF(t, second, 2)

	outputPath := filepath.Join(dir, "merged.pdf")
	if err := MergeWithPageRanges(context.Background(), []string{first, second}, outputPath, []string{"", ""}); err != nil {
		t.Fatalf("expected merge with empty ranges to succeed, got %v", err)
	}

	pageCount, err := api.PageCountFile(outputPath)
	if err != nil {
		t.Fatalf("read merged page count: %v", err)
	}
	if pageCount != 4 {
		t.Fatalf("expected 4 merged pages, got %d", pageCount)
	}
}

func TestMergeWithPageRangesRejectsOutOfBoundsRange(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestMultiPagePDF(t, first, 2)
	writeTestMultiPagePDF(t, second, 2)

	outputPath := filepath.Join(dir, "merged.pdf")
	err := MergeWithPageRanges(context.Background(), []string{first, second}, outputPath, []string{"5", ""})
	if err == nil {
		t.Fatal("expected an out-of-bounds range to fail")
	}
	if !IsMergeErrorCode(err, ErrorCodeMergePageRangeInvalid) {
		t.Fatalf("expected code %q, got %v", ErrorCodeMergePageRangeInvalid, err)
	}
	if !strings.Contains(err.Error(), "first.pdf") {
		t.Fatalf("expected the error to name the offending input, got %v", err)
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected no output file, got stat error %v", statErr)
	}
}

func TestMergeWithPageRangesRejectsInvalidRangeSyntax(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestMultiPagePDF(t, first, 2)
	writeTestMultiPagePDF(t, second, 2)

	outputPath := filepath.Join(dir, "merged.pdf")
	err := MergeWithPageRanges(context.Background(), []string{first, second}, outputPath, []string{"one", ""})
	if err == nil {
		t.Fatal("expected an invalid range to fail")
	}
	if !IsMergeErrorCode(err, ErrorCodeMergePageRangeInvalid) {
		t.Fatalf("expected code %q, got %v", ErrorCodeMergePageRangeInvalid, err)
	}
}

func TestMergeWithPageRangesRejectsOverlappingRange(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestMultiPagePDF(t, first, 3)
	writeTestMultiPagePDF(t, second, 2)

	outputPath := filepath.Join(dir, "merged.pdf")
	err := MergeWithPageRanges(context.Background(), []string{first, second}, outputPath, []string{"1-2,2-3", ""})
	if err == nil {
		t.Fatal("expected an overlapping range to fail")
	}
	if !IsMergeErrorCode(err, ErrorCodeMergePageRangeInvalid) {
		t.Fatalf("expected code %q, got %v", ErrorCodeMergePageRangeInvalid, err)
	}
}

func TestValidateMergePageRangesAllowsWholeFiles(t *testing.T) {
	dir := t.TempDir()
	first := filepath.Join(dir, "first.pdf")
	second := filepath.Join(dir, "second.pdf")
	writeTestMultiPagePDF(t, first, 2)
	writeTestMultiPagePDF(t, second, 2)

	if err := ValidateMergePageRanges([]string{first, second}, nil); err != nil {
		t.Fatalf("expected nil ranges to validate, got %v", err)
	}
	if err := ValidateMergePageRanges([]string{first, second}, []string{"", "2"}); err != nil {
		t.Fatalf("expected valid ranges to pass validation, got %v", err)
	}
}
