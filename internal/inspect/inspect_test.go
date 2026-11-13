package inspect

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

func writeTestPNG(t *testing.T, path string, width, height int) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	defer func() {
		_ = file.Close()
	}()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x), G: uint8(y), B: 120, A: 255})
		}
	}

	if err := png.Encode(file, img); err != nil {
		t.Fatalf("encode png: %v", err)
	}
}

func writeTestPDF(t *testing.T, path string, pageCount int) {
	t.Helper()

	pngPaths := make([]string, 0, pageCount)
	for page := 0; page < pageCount; page++ {
		pngPath := filepath.Join(t.TempDir(), fmt.Sprintf("page-%d.png", page+1))
		writeTestPNG(t, pngPath, 32, 24)
		pngPaths = append(pngPaths, pngPath)
	}

	if err := api.ImportImagesFile(pngPaths, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}

func TestInspectFileImage(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "photo.png")
	writeTestPNG(t, path, 4, 3)

	info := InspectFile(path)
	if !info.Exists {
		t.Fatal("expected image to exist")
	}
	if info.Error != "" {
		t.Fatalf("expected no error, got %q", info.Error)
	}
	if info.Kind != "image" {
		t.Fatalf("expected image kind, got %q", info.Kind)
	}
	if info.Name != "photo.png" {
		t.Fatalf("expected name photo.png, got %q", info.Name)
	}
	if info.Directory != dir {
		t.Fatalf("expected directory %q, got %q", dir, info.Directory)
	}
	if info.Extension != "png" {
		t.Fatalf("expected extension png, got %q", info.Extension)
	}
	if info.Width != 4 || info.Height != 3 {
		t.Fatalf("expected 4x3 dimensions, got %dx%d", info.Width, info.Height)
	}
	if info.SizeBytes <= 0 {
		t.Fatal("expected a positive file size")
	}
	if info.ModifiedAt <= 0 {
		t.Fatal("expected a modification timestamp")
	}
	if info.MimeType != "image/png" {
		t.Fatalf("expected image/png, got %q", info.MimeType)
	}
}

func TestInspectFilePDFPageDims(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "doc.pdf")
	writeTestPDF(t, path, 3)

	info := InspectFile(path)
	if !info.Exists {
		t.Fatal("expected PDF to exist")
	}
	if info.Error != "" {
		t.Fatalf("expected no error, got %q", info.Error)
	}
	if info.Kind != "pdf" {
		t.Fatalf("expected pdf kind, got %q", info.Kind)
	}
	if info.PageCount != 3 {
		t.Fatalf("expected 3 pages, got %d", info.PageCount)
	}
	if info.PageWidthPoints <= 0 || info.PageHeightPoints <= 0 {
		t.Fatalf("expected positive page dimensions, got %fx%f", info.PageWidthPoints, info.PageHeightPoints)
	}
}

func TestInspectFileInvalidPDFStaysTolerant(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "broken.pdf")
	if err := os.WriteFile(path, []byte("this is not a pdf document"), 0o644); err != nil {
		t.Fatalf("write broken pdf: %v", err)
	}

	info := InspectFile(path)
	if !info.Exists {
		t.Fatal("expected broken PDF to exist")
	}
	if info.Error != "" {
		t.Fatalf("expected inspection to stay tolerant, got %q", info.Error)
	}
	if info.PageCount != 0 || info.PageWidthPoints != 0 || info.PageHeightPoints != 0 {
		t.Fatalf("expected no PDF metadata for a broken file, got %+v", info)
	}
}

func TestInspectFileDocument(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(path, []byte("# Title\n"), 0o644); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	info := InspectFile(path)
	if !info.Exists {
		t.Fatal("expected document to exist")
	}
	if info.Kind != "document" {
		t.Fatalf("expected document kind, got %q", info.Kind)
	}
	if !strings.HasPrefix(info.MimeType, "text/markdown") {
		t.Fatalf("expected a markdown mime type, got %q", info.MimeType)
	}
	if info.Width != 0 || info.Height != 0 || info.PageCount != 0 {
		t.Fatal("expected no image or pdf metadata for a document")
	}
}

func TestInspectFileMissing(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "missing.png")

	info := InspectFile(path)
	if info.Exists {
		t.Fatal("expected missing file to report Exists=false")
	}
	if info.Error == "" {
		t.Fatal("expected an error reason for the missing file")
	}
	if info.Name != "missing.png" {
		t.Fatalf("expected name missing.png, got %q", info.Name)
	}
}

func TestInspectFileVideoToleratesMissingDuration(t *testing.T) {
	original := videoDurationCommand
	t.Cleanup(func() {
		videoDurationCommand = original
	})
	videoDurationCommand = func(context.Context, string) ([]byte, error) {
		return nil, errors.New("ffprobe not found")
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(path, []byte("not a real video"), 0o644); err != nil {
		t.Fatalf("write fake video: %v", err)
	}

	info := InspectFile(path)
	if !info.Exists {
		t.Fatal("expected fake video to exist")
	}
	if info.Kind != "video" {
		t.Fatalf("expected video kind, got %q", info.Kind)
	}
	if info.Error != "" {
		t.Fatalf("expected inspection to stay tolerant, got %q", info.Error)
	}
	if info.DurationSeconds != 0 {
		t.Fatalf("expected no readable duration for a fake video, got %v", info.DurationSeconds)
	}
}

func TestInspectFileVideoSetsDuration(t *testing.T) {
	original := videoDurationCommand
	t.Cleanup(func() {
		videoDurationCommand = original
	})
	videoDurationCommand = func(context.Context, string) ([]byte, error) {
		return []byte("9.75\n"), nil
	}

	dir := t.TempDir()
	path := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fake video: %v", err)
	}

	info := InspectFile(path)
	if info.DurationSeconds != 9.75 {
		t.Fatalf("expected 9.75 seconds, got %v", info.DurationSeconds)
	}
}

func TestProbeVideoDurationParsesFFprobeOutput(t *testing.T) {
	original := videoDurationCommand
	t.Cleanup(func() {
		videoDurationCommand = original
	})

	videoDurationCommand = func(context.Context, string) ([]byte, error) {
		return []byte("12.5\n"), nil
	}

	duration, ok := ProbeVideoDuration(context.Background(), "/videos/clip.mp4")
	if !ok {
		t.Fatal("expected duration to be parsed")
	}
	if duration != 12.5 {
		t.Fatalf("expected 12.5 seconds, got %v", duration)
	}
}

func TestProbeVideoDurationIsTolerant(t *testing.T) {
	original := videoDurationCommand
	t.Cleanup(func() {
		videoDurationCommand = original
	})

	cases := []struct {
		name   string
		output []byte
		err    error
	}{
		{name: "missing ffprobe", err: errors.New("executable file not found")},
		{name: "empty output", output: []byte("\n")},
		{name: "not a number", output: []byte("N/A\n")},
		{name: "zero duration", output: []byte("0\n")},
		{name: "negative duration", output: []byte("-1\n")},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			videoDurationCommand = func(context.Context, string) ([]byte, error) {
				return testCase.output, testCase.err
			}

			if duration, ok := ProbeVideoDuration(context.Background(), "/videos/clip.mp4"); ok {
				t.Fatalf("expected no duration, got %v", duration)
			}
		})
	}

	videoDurationCommand = original
	if _, ok := ProbeVideoDuration(context.Background(), "  "); ok {
		t.Fatal("expected empty path to report no duration")
	}
}

func TestInspectFilesKeepsBatchAlive(t *testing.T) {
	dir := t.TempDir()
	pngPath := filepath.Join(dir, "photo.png")
	textPath := filepath.Join(dir, "notes.txt")
	missingPath := filepath.Join(dir, "missing.png")
	writeTestPNG(t, pngPath, 2, 2)
	if err := os.WriteFile(textPath, []byte("hello"), 0o644); err != nil {
		t.Fatalf("write text file: %v", err)
	}

	files := InspectFiles([]string{pngPath, textPath, missingPath, ""})
	if len(files) != 4 {
		t.Fatalf("expected 4 results, got %d", len(files))
	}
	if files[0].Kind != "image" || !files[0].Exists {
		t.Fatalf("expected first result to be an existing image, got %+v", files[0])
	}
	if files[1].Kind != "other" || !files[1].Exists {
		t.Fatalf("expected second result to be an existing other file, got %+v", files[1])
	}
	if files[2].Exists || files[2].Error == "" {
		t.Fatalf("expected third result to report a missing file, got %+v", files[2])
	}
	if files[3].Error == "" {
		t.Fatal("expected empty path to report an error")
	}
}
