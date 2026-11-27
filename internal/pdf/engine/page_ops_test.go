package engine

import (
	"context"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// writePageOpPNG writes a PNG with an exact pixel size so an imported page has
// a predictable geometry.
func writePageOpPNG(t *testing.T, path string, width, height int) {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.Set(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 120, A: 255})
		}
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png: %v", err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode png: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close png: %v", err)
	}
}

// writePageOpSizedPDF builds a one-image-per-page PDF whose page sizes identify
// each page, so a reorder can be verified from the page geometry.
func writePageOpSizedPDF(t *testing.T, path string, sizes [][2]int) {
	t.Helper()

	pngPaths := make([]string, 0, len(sizes))
	for i, size := range sizes {
		pngPath := filepath.Join(t.TempDir(), fmt.Sprintf("page-%d.png", i+1))
		writePageOpPNG(t, pngPath, size[0], size[1])
		pngPaths = append(pngPaths, pngPath)
	}

	if err := api.ImportImagesFile(context.Background(), pngPaths, path, nil, nil); err != nil {
		t.Fatalf("import images into pdf: %v", err)
	}
}

// writePageOpBlankPDF builds a PDF with blank pages and no embedded images.
func writePageOpBlankPDF(t *testing.T, path string, pages int) {
	t.Helper()

	var builder strings.Builder
	builder.WriteString(`{"pages":{`)
	for page := 1; page <= pages; page++ {
		if page > 1 {
			builder.WriteString(",")
		}
		fmt.Fprintf(&builder, `"%d":{"content":{}}`, page)
	}
	builder.WriteString(`}}`)

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create blank pdf: %v", err)
	}
	if err := api.Create(context.Background(), nil, strings.NewReader(builder.String()), file, model.NewDefaultConfiguration()); err != nil {
		_ = file.Close()
		t.Fatalf("create blank pdf: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close blank pdf: %v", err)
	}
}

func pageOpTestDims(t *testing.T, path string) []string {
	t.Helper()

	dims, err := api.PageDimsFile(context.Background(), path)
	if err != nil {
		t.Fatalf("read page dims of %s: %v", filepath.Base(path), err)
	}

	formatted := make([]string, 0, len(dims))
	for _, dim := range dims {
		formatted = append(formatted, dim.String())
	}

	return formatted
}

func pageOpTestRotations(t *testing.T, path string) []int {
	t.Helper()

	file, err := os.Open(path)
	if err != nil {
		t.Fatalf("open %s: %v", filepath.Base(path), err)
	}
	defer func() { _ = file.Close() }()

	info, err := api.PDFInfo(context.Background(), file, path, nil, false, model.NewDefaultConfiguration())
	if err != nil {
		t.Fatalf("read info of %s: %v", filepath.Base(path), err)
	}

	rotations := make([]int, 0, len(info.PageBoundaries))
	for _, boundaries := range info.PageBoundaries {
		rotations = append(rotations, boundaries.Rot)
	}

	return rotations
}

func pageOpTestImageFiles(t *testing.T, dir string) []string {
	t.Helper()

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read extracted dir: %v", err)
	}

	files := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			t.Fatalf("unexpected nested directory in extraction output: %s", entry.Name())
		}
		files = append(files, filepath.Join(dir, entry.Name()))
	}

	return files
}

func TestRotateAllPagesTurnsEveryPage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "source_rotated.pdf")
	writePageOpSizedPDF(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}})

	if err := Rotate(context.Background(), input, output, "", RotateAngleCW90); err != nil {
		t.Fatalf("rotate all pages: %v", err)
	}

	rotations := pageOpTestRotations(t, output)
	if len(rotations) != 3 {
		t.Fatalf("expected 3 pages, got %v", rotations)
	}
	for i, rotation := range rotations {
		if rotation != 90 {
			t.Fatalf("page %d rotation = %d, want 90", i+1, rotation)
		}
	}
	if count, err := api.PageCountFile(context.Background(), output); err != nil || count != 3 {
		t.Fatalf("page count = %d, err = %v; want 3", count, err)
	}
}

func TestRotateSelectedPagesOnly(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "source_rotated.pdf")
	writePageOpSizedPDF(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}, {400, 500}})

	if err := Rotate(context.Background(), input, output, "2,4", RotateAngleCW180); err != nil {
		t.Fatalf("rotate selection: %v", err)
	}

	rotations := pageOpTestRotations(t, output)
	want := []int{0, 180, 0, 180}
	for i := range want {
		if rotations[i] != want[i] {
			t.Fatalf("page %d rotation = %d, want %d", i+1, rotations[i], want[i])
		}
	}
}

func TestRotateRejectsInvalidAngleAndSelection(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "source_rotated.pdf")
	writePageOpSizedPDF(t, input, [][2]int{{100, 200}, {200, 300}})

	tests := []struct {
		name      string
		angle     int
		selection string
		wantCode  string
	}{
		{name: "angle 45", angle: 45, wantCode: ErrorCodeRotateAngleInvalid},
		{name: "angle 0", angle: 0, wantCode: ErrorCodeRotateAngleInvalid},
		{name: "out of bounds", angle: 90, selection: "3", wantCode: ErrorCodeRotatePageSelectionBounds},
		{name: "malformed", angle: 90, selection: "1,,2", wantCode: ErrorCodeRotatePageSelectionBad},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Rotate(context.Background(), input, output, tt.selection, tt.angle)
			var pageErr *PageOpError
			if err == nil || !asPageOpError(err, &pageErr) {
				t.Fatalf("expected a page op error, got %v", err)
			}
			if pageErr.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q (%s)", pageErr.Code, tt.wantCode, pageErr.Message)
			}
			if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
				t.Fatalf("a rejected rotate must not write output, got %v", statErr)
			}
		})
	}
}

func TestRemovePagesDeletesSelection(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "source_pages_removed.pdf")
	writePageOpSizedPDF(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}, {400, 500}})

	if err := RemovePages(context.Background(), input, output, "2-3"); err != nil {
		t.Fatalf("remove pages: %v", err)
	}

	want := []string{"100.000000x200.000000", "400.000000x500.000000"}
	got := pageOpTestDims(t, output)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("remaining pages = %v, want %v", got, want)
	}
	if count, err := api.PageCountFile(context.Background(), output); err != nil || count != 2 {
		t.Fatalf("page count = %d, err = %v; want 2", count, err)
	}
}

func TestRemovePagesRefusesEveryPage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "source_pages_removed.pdf")
	writePageOpSizedPDF(t, input, [][2]int{{100, 200}, {200, 300}})

	err := RemovePages(context.Background(), input, output, "1-2")
	var pageErr *PageOpError
	if err == nil || !asPageOpError(err, &pageErr) {
		t.Fatalf("expected a page op error, got %v", err)
	}
	if pageErr.Code != ErrorCodeRemovePagesAllPages {
		t.Fatalf("code = %q, want %q", pageErr.Code, ErrorCodeRemovePagesAllPages)
	}
	if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
		t.Fatalf("a refused removal must not write output, got %v", statErr)
	}
}

func TestRemovePagesRejectsOutOfBoundsAndMissingSelection(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "source_pages_removed.pdf")
	writePageOpSizedPDF(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}})

	tests := []struct {
		name      string
		selection string
		wantCode  string
	}{
		{name: "out of bounds", selection: "3-5", wantCode: ErrorCodeRemovePagesSelectionBounds},
		{name: "empty", selection: "", wantCode: ErrorCodeRemovePagesSelectionRequired},
		{name: "malformed", selection: "1-", wantCode: ErrorCodeRemovePagesSelectionBad},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := RemovePages(context.Background(), input, output, tt.selection)
			var pageErr *PageOpError
			if err == nil || !asPageOpError(err, &pageErr) {
				t.Fatalf("expected a page op error, got %v", err)
			}
			if pageErr.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q (%s)", pageErr.Code, tt.wantCode, pageErr.Message)
			}
		})
	}
}

func TestReorderWritesExactPageOrder(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "source_reordered.pdf")
	writePageOpSizedPDF(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}, {400, 500}})

	if err := Reorder(context.Background(), input, output, "3,1,2,4"); err != nil {
		t.Fatalf("reorder: %v", err)
	}

	want := []string{
		"300.000000x400.000000",
		"100.000000x200.000000",
		"200.000000x300.000000",
		"400.000000x500.000000",
	}
	got := pageOpTestDims(t, output)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("page order = %v, want %v", got, want)
	}
}

func TestReorderAcceptsReverse(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "source_reordered.pdf")
	writePageOpSizedPDF(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}})

	if err := Reorder(context.Background(), input, output, "reverse"); err != nil {
		t.Fatalf("reverse reorder: %v", err)
	}

	want := []string{"300.000000x400.000000", "200.000000x300.000000", "100.000000x200.000000"}
	got := pageOpTestDims(t, output)
	if strings.Join(got, "|") != strings.Join(want, "|") {
		t.Fatalf("reversed page order = %v, want %v", got, want)
	}
}

func TestReorderRejectsIncompleteOrInvalidOrder(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "source.pdf")
	output := filepath.Join(dir, "source_reordered.pdf")
	writePageOpSizedPDF(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}})

	tests := []struct {
		name     string
		order    string
		wantCode string
	}{
		{name: "missing page", order: "1,3", wantCode: ErrorCodeReorderPageMissing},
		{name: "duplicate page", order: "1,1,2,3", wantCode: ErrorCodeReorderPageDuplicate},
		{name: "invented page", order: "1,2,3,4", wantCode: ErrorCodeReorderPageOutOfBounds},
		{name: "malformed token", order: "1,x,3", wantCode: ErrorCodeReorderOrderInvalid},
		{name: "descending range", order: "3-1,2", wantCode: ErrorCodeReorderOrderInvalid},
		{name: "empty", order: "", wantCode: ErrorCodeReorderOrderRequired},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := Reorder(context.Background(), input, output, tt.order)
			var pageErr *PageOpError
			if err == nil || !asPageOpError(err, &pageErr) {
				t.Fatalf("expected a page op error, got %v", err)
			}
			if pageErr.Code != tt.wantCode {
				t.Fatalf("code = %q, want %q (%s)", pageErr.Code, tt.wantCode, pageErr.Message)
			}
			if _, statErr := os.Stat(output); !os.IsNotExist(statErr) {
				t.Fatalf("a rejected reorder must not write output, got %v", statErr)
			}
		})
	}
}

func TestExtractImagesWritesEveryImage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "album.pdf")
	outputDir := filepath.Join(dir, "album_images")
	writePageOpSizedPDF(t, input, [][2]int{{100, 200}, {200, 300}, {300, 400}})

	result, err := ExtractImages(context.Background(), input, outputDir, "")
	if err != nil {
		t.Fatalf("extract images: %v", err)
	}
	if result.Count != 3 {
		t.Fatalf("extracted count = %d, want 3", result.Count)
	}

	files := pageOpTestImageFiles(t, outputDir)
	if len(files) != 3 {
		t.Fatalf("files on disk = %d, want 3: %v", len(files), files)
	}

	seen := make(map[string]struct{}, len(files))
	for _, file := range files {
		if ext := strings.ToLower(filepath.Ext(file)); ext != ".png" {
			t.Fatalf("unexpected image extension %q for %s", ext, filepath.Base(file))
		}
		if _, duplicate := seen[file]; duplicate {
			t.Fatalf("duplicate extracted file name: %s", filepath.Base(file))
		}
		seen[file] = struct{}{}

		info, statErr := os.Stat(file)
		if statErr != nil || info.Size() <= 0 {
			t.Fatalf("extracted file is missing or empty: %s (%v)", filepath.Base(file), statErr)
		}
	}
}

func TestExtractImagesReportsPDFWithoutImages(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "blank.pdf")
	outputDir := filepath.Join(dir, "blank_images")
	writePageOpBlankPDF(t, input, 2)

	_, err := ExtractImages(context.Background(), input, outputDir, "")
	var pageErr *PageOpError
	if err == nil || !asPageOpError(err, &pageErr) {
		t.Fatalf("expected a page op error, got %v", err)
	}
	if pageErr.Code != ErrorCodeExtractImagesNone {
		t.Fatalf("code = %q, want %q (%s)", pageErr.Code, ErrorCodeExtractImagesNone, pageErr.Message)
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("a document without images must not create the output folder, got %v", statErr)
	}
}

func TestExtractImagesCancellationLeavesNoOutput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "album.pdf")
	outputDir := filepath.Join(dir, "album_images")
	writePageOpSizedPDF(t, input, [][2]int{{100, 200}, {200, 300}})

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := ExtractImages(ctx, input, outputDir, "")
	var pageErr *PageOpError
	if err == nil || !asPageOpError(err, &pageErr) {
		t.Fatalf("expected a page op error, got %v", err)
	}
	if pageErr.Code != pageOpCanceledCode {
		t.Fatalf("code = %q, want %q", pageErr.Code, pageOpCanceledCode)
	}
	if result.Count != 0 || len(result.Files) != 0 {
		t.Fatalf("canceled extraction reported files: %+v", result)
	}
	if _, statErr := os.Stat(outputDir); !os.IsNotExist(statErr) {
		t.Fatalf("canceled extraction left an output folder: %v", statErr)
	}
}

func TestParseReorderOrderExpandsRanges(t *testing.T) {
	pages, err := ParseReorderOrder("3,1,2,4-6", 6)
	if err != nil {
		t.Fatalf("parse order: %v", err)
	}

	want := []int{3, 1, 2, 4, 5, 6}
	if fmt.Sprint(pages) != fmt.Sprint(want) {
		t.Fatalf("pages = %v, want %v", pages, want)
	}
}

// asPageOpError is errors.As with a PageOpError target, kept local so the tests
// read without an extra import in every call site.
func asPageOpError(err error, target **PageOpError) bool {
	for err != nil {
		if typed, ok := err.(*PageOpError); ok {
			*target = typed
			return true
		}
		unwrapper, ok := err.(interface{ Unwrap() error })
		if !ok {
			return false
		}
		err = unwrapper.Unwrap()
	}

	return false
}
