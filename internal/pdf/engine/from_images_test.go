package engine

import (
	"bytes"
	"context"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/h2non/bimg"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// imagesTestPointsPerMM maps a millimetre position to points. A 72 DPI
// rasterization has one pixel per point, so the same value maps to pixels.
const imagesTestPointsPerMM = 72.0 / 25.4

// writeImagesTestPNG writes a solid-colour PNG fixture.
func writeImagesTestPNG(t *testing.T, path string, width, height int, fill color.NRGBA) {
	t.Helper()

	canvas := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.SetNRGBA(x, y, fill)
		}
	}

	file, createErr := os.Create(path)
	if createErr != nil {
		t.Fatalf("create PNG fixture %s: %v", path, createErr)
	}
	if encodeErr := png.Encode(file, canvas); encodeErr != nil {
		_ = file.Close()
		t.Fatalf("encode PNG fixture %s: %v", path, encodeErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		t.Fatalf("close PNG fixture %s: %v", path, closeErr)
	}
}

// writeImagesTestJPEG writes a JPEG fixture split into a left and a right half
// and, when orientation is not zero, injects that EXIF orientation.
func writeImagesTestJPEG(t *testing.T, path string, width, height int, leftFill, rightFill color.NRGBA, orientation byte) {
	t.Helper()

	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			fill := leftFill
			if x >= width/2 {
				fill = rightFill
			}
			canvas.Set(x, y, fill)
		}
	}

	var buffer bytes.Buffer
	if encodeErr := jpeg.Encode(&buffer, canvas, &jpeg.Options{Quality: 95}); encodeErr != nil {
		t.Fatalf("encode JPEG fixture %s: %v", path, encodeErr)
	}

	data := buffer.Bytes()
	if orientation > 0 {
		data = withImagesTestEXIFOrientation(t, data, orientation)
	}
	if writeErr := os.WriteFile(path, data, 0o644); writeErr != nil {
		t.Fatalf("write JPEG fixture %s: %v", path, writeErr)
	}
}

// withImagesTestEXIFOrientation prepends a minimal EXIF APP1 segment carrying
// one IFD0 Orientation tag to an existing JPEG.
func withImagesTestEXIFOrientation(t *testing.T, jpegData []byte, orientation byte) []byte {
	t.Helper()

	if len(jpegData) < 2 || jpegData[0] != 0xFF || jpegData[1] != 0xD8 {
		t.Fatal("expected a JPEG to inject EXIF into")
	}

	tiff := []byte{
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, // TIFF header, IFD0 at offset 8
		0x01, 0x00, // one IFD entry
		0x12, 0x01, // tag 0x0112 = Orientation
		0x03, 0x00, // type SHORT
		0x01, 0x00, 0x00, 0x00, // count 1
		orientation, 0x00, 0x00, 0x00, // inline value
		0x00, 0x00, 0x00, 0x00, // no next IFD
	}

	payload := append([]byte("Exif\x00\x00"), tiff...)
	segmentLength := len(payload) + 2

	out := make([]byte, 0, len(jpegData)+4+len(payload))
	out = append(out, 0xFF, 0xD8, 0xFF, 0xE1, byte(segmentLength>>8), byte(segmentLength))
	out = append(out, payload...)
	out = append(out, jpegData[2:]...)

	return out
}

// requireImagesTestPDFToPPM skips a raster test when poppler is unavailable.
func requireImagesTestPDFToPPM(t *testing.T) {
	t.Helper()

	if _, lookupErr := exec.LookPath("pdftoppm"); lookupErr != nil {
		t.Skip("pdftoppm is not installed")
	}
}

// rasterizeImagesTestPDF renders every PDF page to an image at 72 DPI, which
// means one pixel per point. pdftoppm pads the page number, so the files are
// globbed and sorted by their numeric suffix instead of their name.
func rasterizeImagesTestPDF(t *testing.T, pdfPath string, expectedPages int) []image.Image {
	t.Helper()

	requireImagesTestPDFToPPM(t)

	rasterDir := t.TempDir()
	prefix := filepath.Join(rasterDir, "page")
	command := exec.Command("pdftoppm", "-png", "-r", "72", pdfPath, prefix)
	if output, runErr := command.CombinedOutput(); runErr != nil {
		t.Fatalf("pdftoppm failed: %v: %s", runErr, strings.TrimSpace(string(output)))
	}

	matches, globErr := filepath.Glob(prefix + "-*.png")
	if globErr != nil {
		t.Fatalf("glob rasterized pages: %v", globErr)
	}
	if len(matches) != expectedPages {
		t.Fatalf("expected %d rasterized page(s), got %d (%v)", expectedPages, len(matches), matches)
	}

	sort.Slice(matches, func(left, right int) bool {
		return imagesTestPageNumber(t, matches[left]) < imagesTestPageNumber(t, matches[right])
	})

	pages := make([]image.Image, 0, len(matches))
	for _, match := range matches {
		file, openErr := os.Open(match)
		if openErr != nil {
			t.Fatalf("open rasterized page %s: %v", match, openErr)
		}
		decoded, decodeErr := png.Decode(file)
		closeErr := file.Close()
		if decodeErr != nil {
			t.Fatalf("decode rasterized page %s: %v", match, decodeErr)
		}
		if closeErr != nil {
			t.Fatalf("close rasterized page %s: %v", match, closeErr)
		}
		pages = append(pages, decoded)
	}

	return pages
}

func imagesTestPageNumber(t *testing.T, path string) int {
	t.Helper()

	base := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	separator := strings.LastIndex(base, "-")
	if separator < 0 {
		t.Fatalf("unexpected rasterized page name: %s", path)
	}

	number, parseErr := strconv.Atoi(base[separator+1:])
	if parseErr != nil {
		t.Fatalf("unexpected rasterized page name %s: %v", path, parseErr)
	}

	return number
}

// sampleImagesTestColor reads the pixel at a millimetre position of a 72 DPI
// rasterized page.
func sampleImagesTestColor(t *testing.T, page image.Image, xMM, yMM float64) color.NRGBA {
	t.Helper()

	pixelX := int(math.Round(xMM * imagesTestPointsPerMM))
	pixelY := int(math.Round(yMM * imagesTestPointsPerMM))
	bounds := page.Bounds()
	if pixelX < bounds.Min.X || pixelX >= bounds.Max.X || pixelY < bounds.Min.Y || pixelY >= bounds.Max.Y {
		t.Fatalf("sample point (%.2f, %.2f) mm maps outside the rasterized page %v", xMM, yMM, bounds)
	}

	return color.NRGBAModel.Convert(page.At(pixelX, pixelY)).(color.NRGBA)
}

func assertImagesTestColor(t *testing.T, label string, got, want color.NRGBA, tolerance int) {
	t.Helper()

	spread := imagesTestChannelSpread(got, want)
	t.Logf("%s: sampled rgb(%d,%d,%d), expected rgb(%d,%d,%d) tolerance ±%d", label, got.R, got.G, got.B, want.R, want.G, want.B, tolerance)
	if spread > tolerance {
		t.Fatalf("%s: sampled rgb(%d,%d,%d), expected rgb(%d,%d,%d) ±%d", label, got.R, got.G, got.B, want.R, want.G, want.B, tolerance)
	}
}

func imagesTestChannelSpread(left, right color.NRGBA) int {
	spread := imagesTestAbs(int(left.R) - int(right.R))
	if candidate := imagesTestAbs(int(left.G) - int(right.G)); candidate > spread {
		spread = candidate
	}
	if candidate := imagesTestAbs(int(left.B) - int(right.B)); candidate > spread {
		spread = candidate
	}

	return spread
}

func imagesTestAbs(value int) int {
	if value < 0 {
		return -value
	}

	return value
}

// imagesTestCellCenter returns the centre of a grid cell in millimetres.
func imagesTestCellCenter(layout ImagesLayout, pageWidthMM, pageHeightMM float64, cell int) (float64, float64) {
	contentWidthMM := pageWidthMM - 2*layout.MarginMM
	contentHeightMM := pageHeightMM - 2*layout.MarginMM
	cellWidthMM := (contentWidthMM - layout.GapMM*float64(layout.Columns-1)) / float64(layout.Columns)
	cellHeightMM := (contentHeightMM - layout.GapMM*float64(layout.Rows-1)) / float64(layout.Rows)

	column := cell % layout.Columns
	row := cell / layout.Columns

	return layout.MarginMM + float64(column)*(cellWidthMM+layout.GapMM) + cellWidthMM/2,
		layout.MarginMM + float64(row)*(cellHeightMM+layout.GapMM) + cellHeightMM/2
}

// assertImagesTestPDFPages checks the page count and that every page has the
// expected size in points within one point.
func assertImagesTestPDFPages(t *testing.T, pdfPath string, expectedPages int, pageWidthMM, pageHeightMM float64) {
	t.Helper()

	dimensions, dimsErr := api.PageDimsFile(pdfPath)
	if dimsErr != nil {
		t.Fatalf("read page dimensions of %s: %v", pdfPath, dimsErr)
	}
	if len(dimensions) != expectedPages {
		t.Fatalf("expected %d page(s), got %d", expectedPages, len(dimensions))
	}

	expectedWidthPT := pageWidthMM * imagesTestPointsPerMM
	expectedHeightPT := pageHeightMM * imagesTestPointsPerMM
	t.Logf("measured pages=%d first page=%.2fx%.2f pt, expected %.2fx%.2f pt", len(dimensions), dimensions[0].Width, dimensions[0].Height, expectedWidthPT, expectedHeightPT)

	for index, dimension := range dimensions {
		if math.Abs(dimension.Width-expectedWidthPT) > 1 || math.Abs(dimension.Height-expectedHeightPT) > 1 {
			t.Fatalf("page %d is %.2fx%.2f pt, expected %.2fx%.2f pt", index+1, dimension.Width, dimension.Height, expectedWidthPT, expectedHeightPT)
		}
	}
}

// assertImagesTestNoOutput checks that neither the destination nor a leftover
// images temp file exist in dir.
func assertImagesTestNoOutput(t *testing.T, dir, outputPath string) {
	t.Helper()

	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("expected %s to be absent, stat error = %v", outputPath, statErr)
	}

	entries, readErr := os.ReadDir(dir)
	if readErr != nil {
		t.Fatalf("read output directory %s: %v", dir, readErr)
	}
	for _, entry := range entries {
		if strings.HasPrefix(entry.Name(), ".fileforge-images-") {
			t.Fatalf("expected no images temp file, found %s", entry.Name())
		}
	}
}

func TestValidateImagesPDFRequest(t *testing.T) {
	dir := t.TempDir()

	pngRed := filepath.Join(dir, "red.png")
	writeImagesTestPNG(t, pngRed, 32, 32, color.NRGBA{R: 255, A: 255})
	pngGreen := filepath.Join(dir, "green.png")
	writeImagesTestPNG(t, pngGreen, 32, 32, color.NRGBA{G: 255, A: 255})
	pngBlue := filepath.Join(dir, "blue.png")
	writeImagesTestPNG(t, pngBlue, 32, 32, color.NRGBA{B: 255, A: 255})
	jpegYellow := filepath.Join(dir, "yellow.jpg")
	writeImagesTestJPEG(t, jpegYellow, 32, 32, color.NRGBA{R: 255, G: 255, A: 255}, color.NRGBA{R: 255, G: 255, A: 255}, 0)
	jpegMagenta := filepath.Join(dir, "magenta.jpeg")
	writeImagesTestJPEG(t, jpegMagenta, 32, 32, color.NRGBA{R: 255, B: 255, A: 255}, color.NRGBA{R: 255, B: 255, A: 255}, 0)

	unsupported := filepath.Join(dir, "notes.txt")
	if writeErr := os.WriteFile(unsupported, []byte("not an image"), 0o644); writeErr != nil {
		t.Fatalf("write unsupported fixture: %v", writeErr)
	}
	existingOutput := filepath.Join(dir, "existing.pdf")
	if writeErr := os.WriteFile(existingOutput, []byte("already here"), 0o644); writeErr != nil {
		t.Fatalf("write existing output fixture: %v", writeErr)
	}
	missingOutputDir := filepath.Join(dir, "missing-dir", "output.pdf")

	defaultLayout := func() ImagesLayout {
		return ImagesLayout{
			PageSize:    ImagesPageA4,
			Orientation: ImagesOrientationPortrait,
			Columns:     2,
			Rows:        2,
			MarginMM:    10,
			GapMM:       4,
			Fit:         ImagesFitContain,
		}
	}
	withSlots := func(layout ImagesLayout, slots ...ImageSlot) ImagesLayout {
		layout.Slots = slots
		return layout
	}

	tooManyInputs := make([]string, 0, imagesMaxInputs+1)
	for index := 0; index <= imagesMaxInputs; index++ {
		tooManyInputs = append(tooManyInputs, filepath.Join(dir, fmt.Sprintf("missing-%d.png", index)))
	}

	cases := []struct {
		name            string
		inputs          []string
		outputPath      string
		layout          ImagesLayout
		wantCode        string
		wantPages       int
		wantSlots       []ImageSlot
		wantDetailKey   string
		wantDetailValue any
		check           func(t *testing.T, layout ImagesLayout, pageCount int)
	}{
		{
			name:      "auto fills slots in input order",
			inputs:    []string{pngRed, pngGreen, pngBlue},
			layout:    defaultLayout(),
			wantPages: 1,
			wantSlots: []ImageSlot{{Page: 0, Cell: 0, InputIndex: 0}, {Page: 0, Cell: 1, InputIndex: 1}, {Page: 0, Cell: 2, InputIndex: 2}},
		},
		{
			name:      "auto fill continues on the next page",
			inputs:    []string{pngRed, pngGreen, pngBlue},
			layout:    withSlots(ImagesLayout{PageSize: ImagesPageA4, Orientation: ImagesOrientationPortrait, Columns: 1, Rows: 2, MarginMM: 10, GapMM: 4, Fit: ImagesFitContain}),
			wantPages: 2,
			wantSlots: []ImageSlot{{Page: 0, Cell: 0, InputIndex: 0}, {Page: 0, Cell: 1, InputIndex: 1}, {Page: 1, Cell: 0, InputIndex: 2}},
		},
		{
			name:      "keeps explicit slots",
			inputs:    []string{pngRed, pngGreen},
			layout:    withSlots(defaultLayout(), ImageSlot{Page: 1, Cell: 3, InputIndex: 1}, ImageSlot{Page: 1, Cell: 0, InputIndex: 0}),
			wantPages: 2,
			wantSlots: []ImageSlot{{Page: 1, Cell: 3, InputIndex: 1}, {Page: 1, Cell: 0, InputIndex: 0}},
		},
		{
			name:      "allows placing the same input twice",
			inputs:    []string{pngRed},
			layout:    withSlots(defaultLayout(), ImageSlot{Page: 0, Cell: 0, InputIndex: 0}, ImageSlot{Page: 0, Cell: 1, InputIndex: 0}),
			wantPages: 1,
			wantSlots: []ImageSlot{{Page: 0, Cell: 0, InputIndex: 0}, {Page: 0, Cell: 1, InputIndex: 0}},
		},
		{
			name:            "unplaced image",
			inputs:          []string{pngRed, pngGreen},
			layout:          withSlots(defaultLayout(), ImageSlot{Page: 0, Cell: 0, InputIndex: 0}),
			wantCode:        ImagesErrorUnplacedImages,
			wantDetailKey:   "missing",
			wantDetailValue: 1,
		},
		{
			name:     "page index out of range",
			inputs:   []string{pngRed},
			layout:   withSlots(defaultLayout(), ImageSlot{Page: imagesMaxPageIndex, Cell: 0, InputIndex: 0}),
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "negative page index",
			inputs:   []string{pngRed},
			layout:   withSlots(defaultLayout(), ImageSlot{Page: -1, Cell: 0, InputIndex: 0}),
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "cell index out of range",
			inputs:   []string{pngRed},
			layout:   withSlots(defaultLayout(), ImageSlot{Page: 0, Cell: 4, InputIndex: 0}),
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "negative cell index",
			inputs:   []string{pngRed},
			layout:   withSlots(defaultLayout(), ImageSlot{Page: 0, Cell: -1, InputIndex: 0}),
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "input index out of range",
			inputs:   []string{pngRed},
			layout:   withSlots(defaultLayout(), ImageSlot{Page: 0, Cell: 0, InputIndex: 1}),
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "columns must be positive",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: ImagesPageA4, Orientation: ImagesOrientationPortrait, Columns: 0, Rows: 2, MarginMM: 10, GapMM: 4, Fit: ImagesFitContain},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "columns above the grid limit",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: ImagesPageA4, Orientation: ImagesOrientationPortrait, Columns: 5, Rows: 1, MarginMM: 10, GapMM: 4, Fit: ImagesFitContain},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "rows above the grid limit",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: ImagesPageA4, Orientation: ImagesOrientationPortrait, Columns: 1, Rows: 5, MarginMM: 10, GapMM: 4, Fit: ImagesFitContain},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "margin above the limit",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: ImagesPageA4, Orientation: ImagesOrientationPortrait, Columns: 1, Rows: 1, MarginMM: 51, GapMM: 0, Fit: ImagesFitContain},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "negative margin",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: ImagesPageA4, Orientation: ImagesOrientationPortrait, Columns: 1, Rows: 1, MarginMM: -1, GapMM: 0, Fit: ImagesFitContain},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "non finite margin",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: ImagesPageA4, Orientation: ImagesOrientationPortrait, Columns: 1, Rows: 1, MarginMM: math.NaN(), GapMM: 0, Fit: ImagesFitContain},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "negative gap",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: ImagesPageA4, Orientation: ImagesOrientationPortrait, Columns: 1, Rows: 1, MarginMM: 10, GapMM: -1, Fit: ImagesFitContain},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "gap above the limit",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: ImagesPageA4, Orientation: ImagesOrientationPortrait, Columns: 1, Rows: 1, MarginMM: 10, GapMM: 21, Fit: ImagesFitContain},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "unsupported fit",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: ImagesPageA4, Orientation: ImagesOrientationPortrait, Columns: 1, Rows: 1, MarginMM: 10, GapMM: 0, Fit: "stretch"},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "unsupported page size",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: "a3", Orientation: ImagesOrientationPortrait, Columns: 1, Rows: 1, MarginMM: 10, GapMM: 0, Fit: ImagesFitContain},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "unsupported orientation",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: ImagesPageA4, Orientation: "diagonal", Columns: 1, Rows: 1, MarginMM: 10, GapMM: 0, Fit: ImagesFitContain},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "margins and gaps leave no cell",
			inputs:   []string{pngRed},
			layout:   ImagesLayout{PageSize: ImagesPageA5, Orientation: ImagesOrientationPortrait, Columns: 4, Rows: 1, MarginMM: 50, GapMM: 20, Fit: ImagesFitContain},
			wantCode: ImagesErrorInvalidLayout,
		},
		{
			name:     "duplicate input paths",
			inputs:   []string{pngRed, pngRed},
			layout:   defaultLayout(),
			wantCode: ImagesErrorDuplicateInput,
		},
		{
			name:     "unsupported input extension",
			inputs:   []string{unsupported},
			layout:   defaultLayout(),
			wantCode: ImagesErrorUnsupportedFormat,
		},
		{
			name:     "missing input file",
			inputs:   []string{filepath.Join(dir, "missing.png")},
			layout:   defaultLayout(),
			wantCode: ImagesErrorInvalidInput,
		},
		{
			name:     "empty input path",
			inputs:   []string{"  "},
			layout:   defaultLayout(),
			wantCode: ImagesErrorInvalidInput,
		},
		{
			name:     "no inputs",
			inputs:   nil,
			layout:   defaultLayout(),
			wantCode: ImagesErrorInvalidInput,
		},
		{
			name:            "too many inputs",
			inputs:          tooManyInputs,
			layout:          defaultLayout(),
			wantCode:        ImagesErrorInvalidInput,
			wantDetailKey:   "inputCount",
			wantDetailValue: imagesMaxInputs + 1,
		},
		{
			name:       "output must be a pdf",
			inputs:     []string{pngRed},
			outputPath: filepath.Join(dir, "output.png"),
			layout:     defaultLayout(),
			wantCode:   ImagesErrorInvalidInput,
		},
		{
			name:       "empty output path",
			inputs:     []string{pngRed},
			outputPath: "   ",
			layout:     defaultLayout(),
			wantCode:   ImagesErrorInvalidInput,
		},
		{
			name:       "output already exists",
			inputs:     []string{pngRed},
			outputPath: existingOutput,
			layout:     defaultLayout(),
			wantCode:   ImagesErrorOutputExists,
		},
		{
			name:       "output collides with an input",
			inputs:     []string{pngRed},
			outputPath: pngRed,
			layout:     defaultLayout(),
			wantCode:   ImagesErrorOutputExists,
		},
		{
			name:       "output directory is missing",
			inputs:     []string{pngRed},
			outputPath: missingOutputDir,
			layout:     defaultLayout(),
			wantCode:   ImagesErrorOutputDir,
		},
		{
			name:      "normalizes layout values",
			inputs:    []string{pngRed},
			layout:    ImagesLayout{PageSize: " A4 ", Orientation: "Portrait", Columns: 1, Rows: 1, MarginMM: 0, GapMM: 0, Fit: "COVER"},
			wantPages: 1,
			wantSlots: []ImageSlot{{Page: 0, Cell: 0, InputIndex: 0}},
			check: func(t *testing.T, layout ImagesLayout, pageCount int) {
				if layout.PageSize != ImagesPageA4 || layout.Orientation != ImagesOrientationPortrait || layout.Fit != ImagesFitCover {
					t.Fatalf("expected a normalized layout, got %+v", layout)
				}
			},
		},
		{
			name:      "defaults empty layout values",
			inputs:    []string{pngRed},
			layout:    ImagesLayout{Columns: 1, Rows: 1},
			wantPages: 1,
			wantSlots: []ImageSlot{{Page: 0, Cell: 0, InputIndex: 0}},
			check: func(t *testing.T, layout ImagesLayout, pageCount int) {
				if layout.PageSize != ImagesPageA4 || layout.Orientation != ImagesOrientationPortrait || layout.Fit != ImagesFitContain {
					t.Fatalf("expected a4/portrait/contain defaults, got %+v", layout)
				}
			},
		},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			outputPath := testCase.outputPath
			if outputPath == "" {
				outputPath = filepath.Join(dir, "output.pdf")
			}

			layout, pageCount, validateErr := ValidateImagesPDFRequest(testCase.inputs, outputPath, testCase.layout)

			if testCase.wantCode != "" {
				if validateErr == nil {
					t.Fatalf("expected %s, got success (layout=%+v pages=%d)", testCase.wantCode, layout, pageCount)
				}
				if validateErr.Code != testCase.wantCode {
					t.Fatalf("expected code %s, got %s (%s)", testCase.wantCode, validateErr.Code, validateErr.Message)
				}
				if testCase.wantDetailKey != "" {
					value, exists := validateErr.Details[testCase.wantDetailKey]
					if !exists {
						t.Fatalf("expected details[%q], got %v", testCase.wantDetailKey, validateErr.Details)
					}
					if !reflect.DeepEqual(value, testCase.wantDetailValue) {
						t.Fatalf("expected details[%q] = %v, got %v", testCase.wantDetailKey, testCase.wantDetailValue, value)
					}
				}

				return
			}

			if validateErr != nil {
				t.Fatalf("expected the request to validate, got %s: %s", validateErr.Code, validateErr.Message)
			}
			if pageCount != testCase.wantPages {
				t.Fatalf("expected %d page(s), got %d", testCase.wantPages, pageCount)
			}
			if testCase.wantSlots != nil && !reflect.DeepEqual(layout.Slots, testCase.wantSlots) {
				t.Fatalf("expected slots %+v, got %+v", testCase.wantSlots, layout.Slots)
			}
			if testCase.check != nil {
				testCase.check(t, layout, pageCount)
			}
		})
	}
}

func TestImagesPageDimensionsAndCells(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "single.png")
	writeImagesTestPNG(t, input, 40, 20, color.NRGBA{R: 20, G: 120, B: 220, A: 255})

	cases := []struct {
		name               string
		pageSize           string
		orientation        string
		wantWidthMM        float64
		wantHeightMM       float64
		wantPortraitWidth  float64
		wantPortraitHeight float64
	}{
		{name: "a4 portrait", pageSize: ImagesPageA4, orientation: ImagesOrientationPortrait, wantWidthMM: 210, wantHeightMM: 297, wantPortraitWidth: 210, wantPortraitHeight: 297},
		{name: "a4 landscape", pageSize: ImagesPageA4, orientation: ImagesOrientationLandscape, wantWidthMM: 297, wantHeightMM: 210, wantPortraitWidth: 210, wantPortraitHeight: 297},
		{name: "letter portrait", pageSize: ImagesPageLetter, orientation: ImagesOrientationPortrait, wantWidthMM: 215.9, wantHeightMM: 279.4, wantPortraitWidth: 215.9, wantPortraitHeight: 279.4},
		{name: "letter landscape", pageSize: ImagesPageLetter, orientation: ImagesOrientationLandscape, wantWidthMM: 279.4, wantHeightMM: 215.9, wantPortraitWidth: 215.9, wantPortraitHeight: 279.4},
		{name: "a5 portrait", pageSize: ImagesPageA5, orientation: ImagesOrientationPortrait, wantWidthMM: 148, wantHeightMM: 210, wantPortraitWidth: 148, wantPortraitHeight: 210},
		{name: "a5 landscape", pageSize: ImagesPageA5, orientation: ImagesOrientationLandscape, wantWidthMM: 210, wantHeightMM: 148, wantPortraitWidth: 148, wantPortraitHeight: 210},
	}

	for _, testCase := range cases {
		testCase := testCase
		t.Run(testCase.name, func(t *testing.T) {
			output := filepath.Join(dir, strings.ReplaceAll(testCase.name, " ", "-")+".pdf")
			layout := ImagesLayout{
				PageSize:    testCase.pageSize,
				Orientation: testCase.orientation,
				Columns:     2,
				Rows:        2,
				MarginMM:    10,
				GapMM:       4,
				Fit:         ImagesFitContain,
			}

			normalized, pageCount, validateErr := ValidateImagesPDFRequest([]string{input}, output, layout)
			if validateErr != nil {
				t.Fatalf("expected the layout to validate, got %s: %s", validateErr.Code, validateErr.Message)
			}
			if pageCount != 1 {
				t.Fatalf("expected 1 page, got %d", pageCount)
			}
			if normalized.PageSize != testCase.pageSize || normalized.Orientation != testCase.orientation {
				t.Fatalf("expected the layout to keep %s/%s, got %s/%s", testCase.pageSize, testCase.orientation, normalized.PageSize, normalized.Orientation)
			}

			widthMM, heightMM := imagesPageDimensions(normalized)
			if math.Abs(widthMM-testCase.wantWidthMM) > 0.001 || math.Abs(heightMM-testCase.wantHeightMM) > 0.001 {
				t.Fatalf("expected %.2fx%.2f mm, got %.2fx%.2f mm", testCase.wantWidthMM, testCase.wantHeightMM, widthMM, heightMM)
			}

			contentWidthMM := widthMM - 2*normalized.MarginMM
			contentHeightMM := heightMM - 2*normalized.MarginMM
			cellWidthMM := (contentWidthMM - normalized.GapMM*float64(normalized.Columns-1)) / float64(normalized.Columns)
			cellHeightMM := (contentHeightMM - normalized.GapMM*float64(normalized.Rows-1)) / float64(normalized.Rows)
			t.Logf("%s: page %.2fx%.2f mm, cell %.2fx%.2f mm", testCase.name, widthMM, heightMM, cellWidthMM, cellHeightMM)
			if cellWidthMM <= 0 || cellHeightMM <= 0 {
				t.Fatalf("expected a positive cell size, got %.2fx%.2f mm", cellWidthMM, cellHeightMM)
			}
			if math.Abs((contentWidthMM-2*cellWidthMM)-normalized.GapMM) > 0.001 {
				t.Fatalf("expected one gap between the columns, content=%.2f cell=%.2f", contentWidthMM, cellWidthMM)
			}

			result, buildErr := BuildImagesPDF(context.Background(), []string{input}, output, layout, nil)
			if buildErr != nil {
				t.Fatalf("expected the build to succeed, got %s: %s", buildErr.Code, buildErr.Message)
			}
			if result.PageCount != 1 || result.ImageCount != 1 || result.OutputPath != output {
				t.Fatalf("unexpected result: %+v", result)
			}
			if math.Abs(result.PageWidthMM-testCase.wantWidthMM) > 0.001 || math.Abs(result.PageHeightMM-testCase.wantHeightMM) > 0.001 {
				t.Fatalf("expected result page %.2fx%.2f mm, got %.2fx%.2f mm", testCase.wantWidthMM, testCase.wantHeightMM, result.PageWidthMM, result.PageHeightMM)
			}

			assertImagesTestPDFPages(t, output, 1, testCase.wantWidthMM, testCase.wantHeightMM)
		})
	}
}

func TestBuildImagesPDFRendersGridColors(t *testing.T) {
	dir := t.TempDir()

	fills := []color.NRGBA{
		{R: 255, A: 255},
		{G: 255, A: 255},
		{B: 255, A: 255},
		{R: 255, G: 255, A: 255},
		{R: 255, B: 255, A: 255},
	}
	names := []string{"red", "green", "blue", "yellow", "magenta"}
	inputs := make([]string, 0, len(fills))
	for index, fill := range fills {
		inputPath := filepath.Join(dir, names[index]+".png")
		writeImagesTestPNG(t, inputPath, 64, 64, fill)
		inputs = append(inputs, inputPath)
	}

	output := filepath.Join(dir, "grid.pdf")
	layout := ImagesLayout{
		PageSize:    ImagesPageA4,
		Orientation: ImagesOrientationPortrait,
		Columns:     2,
		Rows:        2,
		MarginMM:    10,
		GapMM:       4,
		Fit:         ImagesFitContain,
	}

	result, buildErr := BuildImagesPDF(context.Background(), inputs, output, layout, nil)
	if buildErr != nil {
		t.Fatalf("expected the build to succeed, got %s: %s", buildErr.Code, buildErr.Message)
	}
	if result.PageCount != 2 || result.ImageCount != 5 {
		t.Fatalf("expected 2 pages and 5 images, got %+v", result)
	}

	assertImagesTestPDFPages(t, output, 2, 210, 297)

	pages := rasterizeImagesTestPDF(t, output, 2)
	t.Logf("rasterized page 1 size: %v, page 2 size: %v", pages[0].Bounds(), pages[1].Bounds())

	for cell := 0; cell < 4; cell++ {
		xMM, yMM := imagesTestCellCenter(layout, 210, 297, cell)
		sampled := sampleImagesTestColor(t, pages[0], xMM, yMM)
		assertImagesTestColor(t, fmt.Sprintf("page 1 cell %d (%s)", cell, names[cell]), sampled, fills[cell], 12)
	}

	xMM, yMM := imagesTestCellCenter(layout, 210, 297, 0)
	assertImagesTestColor(t, "page 2 cell 0 (magenta)", sampleImagesTestColor(t, pages[1], xMM, yMM), fills[4], 12)

	white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
	for cell := 1; cell < 4; cell++ {
		xMM, yMM := imagesTestCellCenter(layout, 210, 297, cell)
		assertImagesTestColor(t, fmt.Sprintf("page 2 cell %d (empty)", cell), sampleImagesTestColor(t, pages[1], xMM, yMM), white, 4)
	}
}

func TestBuildImagesPDFContainVersusCover(t *testing.T) {
	dir := t.TempDir()

	fill := color.NRGBA{R: 220, G: 20, B: 60, A: 255}
	input := filepath.Join(dir, "wide.png")
	writeImagesTestPNG(t, input, 400, 200, fill)

	layoutFor := func(fit string) ImagesLayout {
		return ImagesLayout{
			PageSize:    ImagesPageA4,
			Orientation: ImagesOrientationPortrait,
			Columns:     1,
			Rows:        1,
			MarginMM:    10,
			GapMM:       0,
			Fit:         fit,
		}
	}

	assertTopAndCenter := func(t *testing.T, output string, layout ImagesLayout) (color.NRGBA, color.NRGBA) {
		t.Helper()

		pages := rasterizeImagesTestPDF(t, output, 1)
		cellWidthMM := 210 - 2*layout.MarginMM
		cellHeightMM := 297 - 2*layout.MarginMM
		centerXMM := layout.MarginMM + cellWidthMM/2
		topYMM := layout.MarginMM + cellHeightMM*0.1
		centerYMM := layout.MarginMM + cellHeightMM/2

		return sampleImagesTestColor(t, pages[0], centerXMM, topYMM), sampleImagesTestColor(t, pages[0], centerXMM, centerYMM)
	}

	t.Run("contain leaves white above the image", func(t *testing.T) {
		output := filepath.Join(dir, "contain.pdf")
		layout := layoutFor(ImagesFitContain)
		if _, buildErr := BuildImagesPDF(context.Background(), []string{input}, output, layout, nil); buildErr != nil {
			t.Fatalf("expected the build to succeed, got %s: %s", buildErr.Code, buildErr.Message)
		}

		top, center := assertTopAndCenter(t, output, layout)
		white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		assertImagesTestColor(t, "contain top-center (empty page area)", top, white, 4)
		assertImagesTestColor(t, "contain center (image)", center, fill, 12)
	})

	t.Run("cover fills the cell", func(t *testing.T) {
		output := filepath.Join(dir, "cover.pdf")
		layout := layoutFor(ImagesFitCover)
		if _, buildErr := BuildImagesPDF(context.Background(), []string{input}, output, layout, nil); buildErr != nil {
			t.Fatalf("expected the build to succeed, got %s: %s", buildErr.Code, buildErr.Message)
		}

		top, center := assertTopAndCenter(t, output, layout)
		assertImagesTestColor(t, "cover top-center (image)", top, fill, 12)
		assertImagesTestColor(t, "cover center (image)", center, fill, 12)
	})
}

func TestBuildImagesPDFAppliesEXIFOrientation(t *testing.T) {
	dir := t.TempDir()

	red := color.NRGBA{R: 255, A: 255}
	blue := color.NRGBA{B: 255, A: 255}
	input := filepath.Join(dir, "rotated.jpg")
	writeImagesTestJPEG(t, input, 200, 100, red, blue, 6)

	source, readErr := os.ReadFile(input)
	if readErr != nil {
		t.Fatalf("read EXIF fixture: %v", readErr)
	}
	metadata, metadataErr := bimg.NewImage(source).Metadata()
	if metadataErr != nil || metadata.Orientation != 6 {
		t.Skipf("libvips cannot read the injected EXIF orientation (orientation=%d err=%v)", metadata.Orientation, metadataErr)
	}

	output := filepath.Join(dir, "oriented.pdf")
	layout := ImagesLayout{
		PageSize:    ImagesPageA4,
		Orientation: ImagesOrientationPortrait,
		Columns:     1,
		Rows:        1,
		MarginMM:    10,
		GapMM:       0,
		Fit:         ImagesFitContain,
	}
	if _, buildErr := BuildImagesPDF(context.Background(), []string{input}, output, layout, nil); buildErr != nil {
		t.Fatalf("expected the build to succeed, got %s: %s", buildErr.Code, buildErr.Message)
	}

	pages := rasterizeImagesTestPDF(t, output, 1)
	cellWidthMM := 210 - 2*layout.MarginMM
	cellHeightMM := 297 - 2*layout.MarginMM
	centerXMM := layout.MarginMM + cellWidthMM/2
	topYMM := layout.MarginMM + cellHeightMM*0.1
	bottomYMM := layout.MarginMM + cellHeightMM*0.9

	assertImagesTestColor(t, "top-center after EXIF rotation", sampleImagesTestColor(t, pages[0], centerXMM, topYMM), red, 24)
	assertImagesTestColor(t, "bottom-center after EXIF rotation", sampleImagesTestColor(t, pages[0], centerXMM, bottomYMM), blue, 24)
}

func TestBuildImagesPDFAcceptsWebP(t *testing.T) {
	dir := t.TempDir()

	pngPath := filepath.Join(dir, "source.png")
	writeImagesTestPNG(t, pngPath, 80, 60, color.NRGBA{R: 0, G: 200, B: 120, A: 255})

	source, readErr := os.ReadFile(pngPath)
	if readErr != nil {
		t.Fatalf("read PNG fixture: %v", readErr)
	}
	webpData, convertErr := bimg.NewImage(source).Convert(bimg.WEBP)
	if convertErr != nil {
		t.Skipf("libvips cannot encode WebP: %v", convertErr)
	}

	webpPath := filepath.Join(dir, "source.webp")
	if writeErr := os.WriteFile(webpPath, webpData, 0o644); writeErr != nil {
		t.Fatalf("write WebP fixture: %v", writeErr)
	}

	output := filepath.Join(dir, "webp.pdf")
	layout := ImagesLayout{
		PageSize:    ImagesPageA5,
		Orientation: ImagesOrientationPortrait,
		Columns:     1,
		Rows:        1,
		MarginMM:    10,
		GapMM:       0,
		Fit:         ImagesFitContain,
	}

	result, buildErr := BuildImagesPDF(context.Background(), []string{webpPath}, output, layout, nil)
	if buildErr != nil {
		t.Fatalf("expected the build to succeed, got %s: %s", buildErr.Code, buildErr.Message)
	}
	if result.PageCount != 1 || result.ImageCount != 1 {
		t.Fatalf("expected 1 page and 1 image, got %+v", result)
	}

	assertImagesTestPDFPages(t, output, 1, 148, 210)
}

func TestBuildImagesPDFReportsProgress(t *testing.T) {
	dir := t.TempDir()

	inputs := make([]string, 0, 5)
	for index := 0; index < 5; index++ {
		inputPath := filepath.Join(dir, fmt.Sprintf("progress-%d.png", index))
		writeImagesTestPNG(t, inputPath, 24, 24, color.NRGBA{R: uint8(20 * index), G: 90, B: 200, A: 255})
		inputs = append(inputs, inputPath)
	}

	output := filepath.Join(dir, "progress.pdf")
	layout := ImagesLayout{
		PageSize:    ImagesPageA4,
		Orientation: ImagesOrientationPortrait,
		Columns:     3,
		Rows:        2,
		MarginMM:    10,
		GapMM:       4,
		Fit:         ImagesFitContain,
	}

	pairs := make([][2]int, 0, len(inputs))
	result, buildErr := BuildImagesPDF(context.Background(), inputs, output, layout, func(processed, total int) {
		pairs = append(pairs, [2]int{processed, total})
	})
	if buildErr != nil {
		t.Fatalf("expected the build to succeed, got %s: %s", buildErr.Code, buildErr.Message)
	}
	if result.PageCount != 1 {
		t.Fatalf("expected a single page, got %d", result.PageCount)
	}

	t.Logf("progress pairs: %v", pairs)
	if len(pairs) != len(inputs) {
		t.Fatalf("expected %d progress calls, got %d", len(inputs), len(pairs))
	}
	for index, pair := range pairs {
		if pair[1] != len(inputs) {
			t.Fatalf("progress call %d reported total %d, expected %d", index, pair[1], len(inputs))
		}
		if pair[0] != index+1 {
			t.Fatalf("progress call %d reported processed %d, expected %d", index, pair[0], index+1)
		}
		if index > 0 && pair[0] <= pairs[index-1][0] {
			t.Fatalf("progress is not monotonic: %v", pairs)
		}
	}
	if pairs[len(pairs)-1] != [2]int{len(inputs), len(inputs)} {
		t.Fatalf("expected the last pair to be %d/%d, got %v", len(inputs), len(inputs), pairs[len(pairs)-1])
	}
}

func TestBuildImagesPDFCancellation(t *testing.T) {
	dir := t.TempDir()

	first := filepath.Join(dir, "first.png")
	writeImagesTestPNG(t, first, 32, 32, color.NRGBA{R: 10, G: 20, B: 30, A: 255})
	second := filepath.Join(dir, "second.png")
	writeImagesTestPNG(t, second, 32, 32, color.NRGBA{R: 200, G: 20, B: 30, A: 255})

	layout := ImagesLayout{
		PageSize:    ImagesPageA4,
		Orientation: ImagesOrientationPortrait,
		Columns:     1,
		Rows:        2,
		MarginMM:    10,
		GapMM:       0,
		Fit:         ImagesFitContain,
	}

	t.Run("canceled before the build", func(t *testing.T) {
		output := filepath.Join(dir, "canceled-before.pdf")
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, buildErr := BuildImagesPDF(ctx, []string{first}, output, layout, nil)
		if buildErr == nil {
			t.Fatal("expected cancellation to fail the build")
		}
		if buildErr.Code != ImagesErrorCancelled {
			t.Fatalf("expected %s, got %s (%s)", ImagesErrorCancelled, buildErr.Code, buildErr.Message)
		}

		assertImagesTestNoOutput(t, dir, output)
	})

	t.Run("canceled from the progress callback", func(t *testing.T) {
		output := filepath.Join(dir, "canceled-progress.pdf")
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		calls := 0
		_, buildErr := BuildImagesPDF(ctx, []string{first, second}, output, layout, func(processed, total int) {
			calls++
			cancel()
		})
		if buildErr == nil {
			t.Fatal("expected cancellation to fail the build")
		}
		if buildErr.Code != ImagesErrorCancelled {
			t.Fatalf("expected %s, got %s (%s)", ImagesErrorCancelled, buildErr.Code, buildErr.Message)
		}
		if calls != 1 {
			t.Fatalf("expected progress to stop after the first image, got %d calls", calls)
		}

		assertImagesTestNoOutput(t, dir, output)
	})
}

func TestBuildImagesPDFDecodeFailureLeavesNoOutput(t *testing.T) {
	dir := t.TempDir()

	corrupt := filepath.Join(dir, "corrupt.png")
	if writeErr := os.WriteFile(corrupt, []byte("this is not a PNG payload"), 0o644); writeErr != nil {
		t.Fatalf("write corrupt fixture: %v", writeErr)
	}

	output := filepath.Join(dir, "decode.pdf")
	layout := ImagesLayout{
		PageSize:    ImagesPageA4,
		Orientation: ImagesOrientationPortrait,
		Columns:     1,
		Rows:        1,
		MarginMM:    10,
		GapMM:       0,
		Fit:         ImagesFitContain,
	}

	_, buildErr := BuildImagesPDF(context.Background(), []string{corrupt}, output, layout, nil)
	if buildErr == nil {
		t.Fatal("expected the corrupt image to fail the build")
	}
	if buildErr.Code != ImagesErrorDecodeFailed {
		t.Fatalf("expected %s, got %s (%s)", ImagesErrorDecodeFailed, buildErr.Code, buildErr.Message)
	}
	if !strings.Contains(buildErr.Message, "corrupt.png") {
		t.Fatalf("expected the file name in the message, got %q", buildErr.Message)
	}

	assertImagesTestNoOutput(t, dir, output)
}

func TestBuildImagesPDFSingleImageAndEmptyCells(t *testing.T) {
	dir := t.TempDir()

	fill := color.NRGBA{R: 0, G: 160, B: 80, A: 255}
	input := filepath.Join(dir, "single.png")
	writeImagesTestPNG(t, input, 50, 50, fill)

	t.Run("single image in a 1x1 grid", func(t *testing.T) {
		output := filepath.Join(dir, "single.pdf")
		layout := ImagesLayout{
			PageSize:    ImagesPageA4,
			Orientation: ImagesOrientationPortrait,
			Columns:     1,
			Rows:        1,
			MarginMM:    10,
			GapMM:       0,
			Fit:         ImagesFitContain,
		}

		result, buildErr := BuildImagesPDF(context.Background(), []string{input}, output, layout, nil)
		if buildErr != nil {
			t.Fatalf("expected the build to succeed, got %s: %s", buildErr.Code, buildErr.Message)
		}
		if result.PageCount != 1 {
			t.Fatalf("expected 1 page, got %d", result.PageCount)
		}

		assertImagesTestPDFPages(t, output, 1, 210, 297)

		pages := rasterizeImagesTestPDF(t, output, 1)
		xMM, yMM := imagesTestCellCenter(layout, 210, 297, 0)
		assertImagesTestColor(t, "1x1 center", sampleImagesTestColor(t, pages[0], xMM, yMM), fill, 12)
	})

	t.Run("empty cells stay white", func(t *testing.T) {
		output := filepath.Join(dir, "empty-cells.pdf")
		layout := ImagesLayout{
			PageSize:    ImagesPageA4,
			Orientation: ImagesOrientationPortrait,
			Columns:     2,
			Rows:        2,
			MarginMM:    10,
			GapMM:       4,
			Fit:         ImagesFitContain,
			Slots:       []ImageSlot{{Page: 0, Cell: 0, InputIndex: 0}},
		}

		result, buildErr := BuildImagesPDF(context.Background(), []string{input}, output, layout, nil)
		if buildErr != nil {
			t.Fatalf("expected the build to succeed, got %s: %s", buildErr.Code, buildErr.Message)
		}
		if result.PageCount != 1 {
			t.Fatalf("expected 1 page, got %d", result.PageCount)
		}

		assertImagesTestPDFPages(t, output, 1, 210, 297)

		pages := rasterizeImagesTestPDF(t, output, 1)
		xMM, yMM := imagesTestCellCenter(layout, 210, 297, 0)
		assertImagesTestColor(t, "occupied cell 0", sampleImagesTestColor(t, pages[0], xMM, yMM), fill, 12)

		white := color.NRGBA{R: 255, G: 255, B: 255, A: 255}
		for cell := 1; cell < 4; cell++ {
			xMM, yMM := imagesTestCellCenter(layout, 210, 297, cell)
			assertImagesTestColor(t, fmt.Sprintf("empty cell %d", cell), sampleImagesTestColor(t, pages[0], xMM, yMM), white, 4)
		}
	})
}
