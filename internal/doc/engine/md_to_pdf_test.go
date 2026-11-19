package engine

import (
	"context"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestResolvePageSetupDefaults(t *testing.T) {
	setup, err := resolvePageSetup(RenderConfig{})
	if err != nil {
		t.Fatalf("expected default page setup to resolve, got %v", err)
	}

	if setup.PageSize != "A4" || setup.Orientation != "P" {
		t.Fatalf("expected A4 portrait, got %s/%s", setup.PageSize, setup.Orientation)
	}
	if setup.PageWidthMM != 210 || setup.PageHeightMM != 297 {
		t.Fatalf("expected A4 dimensions, got %.1fx%.1f", setup.PageWidthMM, setup.PageHeightMM)
	}
	if setup.MarginTopMM != 20 || setup.MarginRightMM != 15 || setup.MarginBottomMM != 18 || setup.MarginLeftMM != 15 {
		t.Fatalf("unexpected default margins: %+v", setup)
	}
}

func TestResolvePageSetupKeepsExplicitZeroMargin(t *testing.T) {
	setup, err := resolvePageSetup(RenderConfig{MarginTopMM: 0, MarginRightMM: 10, MarginBottomMM: 10, MarginLeftMM: 10})
	if err != nil {
		t.Fatalf("expected page setup to resolve, got %v", err)
	}
	if setup.MarginTopMM != 0 {
		t.Fatalf("expected explicit zero top margin to be kept, got %.2f", setup.MarginTopMM)
	}
}

func TestResolvePageSetupLetterLandscape(t *testing.T) {
	setup, err := resolvePageSetup(RenderConfig{PageSize: "letter", Orientation: "l"})
	if err != nil {
		t.Fatalf("expected page setup to resolve, got %v", err)
	}

	if setup.PageSize != "Letter" {
		t.Fatalf("expected normalized Letter, got %q", setup.PageSize)
	}
	if setup.Orientation != "L" {
		t.Fatalf("expected normalized L, got %q", setup.Orientation)
	}
	if setup.PageWidthMM != 279.4 || setup.PageHeightMM != 215.9 {
		t.Fatalf("expected swapped Letter dimensions, got %.1fx%.1f", setup.PageWidthMM, setup.PageHeightMM)
	}
}

func TestResolvePageSetupRejectsInvalidValues(t *testing.T) {
	cases := []struct {
		name string
		cfg  RenderConfig
	}{
		{name: "page size", cfg: RenderConfig{PageSize: "A5"}},
		{name: "orientation", cfg: RenderConfig{Orientation: "diagonal"}},
		{name: "negative margin", cfg: RenderConfig{MarginLeftMM: -1}},
		{name: "non finite margin", cfg: RenderConfig{MarginBottomMM: math.NaN()}},
		{name: "margins exceed page width", cfg: RenderConfig{MarginLeftMM: 150, MarginRightMM: 150}},
		{name: "margins exceed page height", cfg: RenderConfig{MarginTopMM: 200, MarginBottomMM: 200}},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if _, err := resolvePageSetup(tc.cfg); err == nil {
				t.Fatal("expected an error")
			}
		})
	}
}

func TestRenderMarkdownToPDFLeavesOnlyPublishedFile(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "document.md")
	if err := os.WriteFile(inputPath, []byte("# Title\n\nBody.\n"), 0o644); err != nil {
		t.Fatalf("write markdown fixture: %v", err)
	}

	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}
	outputPath := filepath.Join(outDir, "document.pdf")

	if err := RenderMarkdownToPDF(context.Background(), RenderConfig{InputPath: inputPath, OutputPath: outputPath}); err != nil {
		t.Fatalf("expected render to succeed, got %v", err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected published output: %v", err)
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "document.pdf" {
		t.Fatalf("expected only the published file, got %v", entryNames(entries))
	}
}

func TestRenderMarkdownToPDFRemovesTempWhenPublishFails(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "document.md")
	if err := os.WriteFile(inputPath, []byte("# Title\n"), 0o644); err != nil {
		t.Fatalf("write markdown fixture: %v", err)
	}

	outDir := filepath.Join(dir, "out")
	if err := os.MkdirAll(outDir, 0o755); err != nil {
		t.Fatalf("mkdir output dir: %v", err)
	}

	// A directory in the destination position makes publication fail after the
	// PDF was already written to the sibling temp file.
	outputPath := filepath.Join(outDir, "document.pdf")
	if err := os.Mkdir(outputPath, 0o755); err != nil {
		t.Fatalf("create blocking directory: %v", err)
	}

	if err := RenderMarkdownToPDF(context.Background(), RenderConfig{InputPath: inputPath, OutputPath: outputPath}); err == nil {
		t.Fatal("expected publication to fail")
	}

	entries, err := os.ReadDir(outDir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	if len(entries) != 1 || entries[0].Name() != "document.pdf" {
		t.Fatalf("expected the temp file to be removed, got %v", entryNames(entries))
	}
}

func entryNames(entries []os.DirEntry) []string {
	names := make([]string, 0, len(entries))
	for _, entry := range entries {
		names = append(names, entry.Name())
	}

	return names
}
