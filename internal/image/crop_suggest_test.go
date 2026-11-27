package image

import (
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"testing"
)

// borderFixture paints a busy interior and a solid border of the given colour
// and thickness. A zero border keeps the interior pattern everywhere.
func borderFixture(width, height, border int, borderColor color.NRGBA) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if border > 0 && x >= border && x < width-border && y >= border && y < height-border {
				img.SetNRGBA(x, y, contentPixel(x, y))
				continue
			}
			if border == 0 {
				img.SetNRGBA(x, y, contentPixel(x, y))
				continue
			}
			img.SetNRGBA(x, y, borderColor)
		}
	}
	return img
}

// contentPixel is intentionally noisy so it can never be mistaken for a flat
// border, even after rounding or lossy compression.
func contentPixel(x, y int) color.NRGBA {
	return color.NRGBA{
		R: uint8((x*37 + y*53) % 256),
		G: uint8((x*67 + y*29) % 256),
		B: uint8((x*13 + y*97) % 256),
		A: 255,
	}
}

func writeSuggestionPNG(t *testing.T, dir, name string, img image.Image) string {
	t.Helper()

	path := filepath.Join(dir, name)
	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create fixture: %v", err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode fixture: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close fixture: %v", err)
	}
	return path
}

func assertRect(t *testing.T, got CropBorderSuggestion, x, y, width, height int) {
	t.Helper()

	if !got.Found {
		t.Fatalf("expected a suggestion, got none (%s)", got.Reason)
	}
	if got.X != x || got.Y != y || got.Width != width || got.Height != height {
		t.Fatalf("expected rect (%d,%d,%d,%d), got (%d,%d,%d,%d)", x, y, width, height, got.X, got.Y, got.Width, got.Height)
	}
}

func TestAnalyzeCropBordersWhiteBorder(t *testing.T) {
	img := borderFixture(240, 180, 12, color.NRGBA{R: 255, G: 255, B: 255, A: 255})

	got := AnalyzeCropBorders(img)

	assertRect(t, got, 12, 12, 216, 156)
	if got.Confidence != cropSuggestionConfidenceHigh {
		t.Fatalf("expected high confidence, got %q", got.Confidence)
	}
}

func TestAnalyzeCropBordersTransparentBorder(t *testing.T) {
	img := borderFixture(180, 140, 10, color.NRGBA{R: 0, G: 0, B: 0, A: 0})

	got := AnalyzeCropBorders(img)

	assertRect(t, got, 10, 10, 160, 120)
}

func TestAnalyzeCropBordersSolidColourBorder(t *testing.T) {
	img := borderFixture(200, 150, 8, color.NRGBA{R: 0, G: 0, B: 255, A: 255})

	got := AnalyzeCropBorders(img)

	assertRect(t, got, 8, 8, 184, 134)
}

func TestAnalyzeCropBordersNoBorder(t *testing.T) {
	img := borderFixture(160, 120, 0, color.NRGBA{})

	got := AnalyzeCropBorders(img)

	if got.Found {
		t.Fatalf("expected no suggestion, got (%d,%d,%d,%d)", got.X, got.Y, got.Width, got.Height)
	}
	if got.Reason == "" {
		t.Fatal("expected a reason when no suggestion is found")
	}
}

func TestAnalyzeCropBordersThickerThanScanLimit(t *testing.T) {
	// 90px of border on a 200px edge is 45%, past the 35% scan limit, and the
	// remaining 20x20 area is far below the minimum area fraction.
	img := borderFixture(200, 200, 90, color.NRGBA{R: 255, G: 255, B: 255, A: 255})

	got := AnalyzeCropBorders(img)

	if got.Found {
		t.Fatalf("expected no suggestion for an oversized border, got (%d,%d,%d,%d)", got.X, got.Y, got.Width, got.Height)
	}
	if got.Reason != cropSuggestionReasonScanLimit {
		t.Fatalf("expected scan-limit reason, got %q", got.Reason)
	}
}

func TestAnalyzeCropBordersLargeImageDownscalesAndMapsBack(t *testing.T) {
	img := borderFixture(2000, 1600, 200, color.NRGBA{R: 255, G: 255, B: 255, A: 255})

	got := AnalyzeCropBorders(img)

	if !got.Found {
		t.Fatalf("expected a suggestion for the large image, got none (%s)", got.Reason)
	}

	// Downscaling to 512px keeps roughly 0.256 of the original scale; the
	// mapped inset may differ from 200 by a couple of pixels.
	assertWithin := func(name string, gotValue, want int) {
		t.Helper()
		if math.Abs(float64(gotValue-want)) > 4 {
			t.Fatalf("expected %s ~%d, got %d", name, want, gotValue)
		}
	}
	assertWithin("x", got.X, 200)
	assertWithin("y", got.Y, 200)
	assertWithin("width", got.Width, 1600)
	assertWithin("height", got.Height, 1200)

	if got.X+got.Width > 2000 || got.Y+got.Height > 1600 {
		t.Fatalf("mapped rect exceeds original bounds: (%d,%d,%d,%d)", got.X, got.Y, got.Width, got.Height)
	}
}

func TestGetImageCropSuggestionReturnsDetectedRectangle(t *testing.T) {
	dir := t.TempDir()
	path := writeSuggestionPNG(t, dir, "framed.png", borderFixture(200, 160, 12, color.NRGBA{R: 255, G: 255, B: 255, A: 255}))

	response := NewCropTool().GetImageCropSuggestion(path)

	if !response.Success {
		t.Fatalf("expected success, got %+v", response.Error)
	}
	if !response.Found {
		t.Fatalf("expected a found suggestion, got reason %q", response.Reason)
	}
	if response.X != 12 || response.Y != 12 || response.Width != 176 || response.Height != 136 {
		t.Fatalf("unexpected rect (%d,%d,%d,%d)", response.X, response.Y, response.Width, response.Height)
	}
	if response.Confidence == "" || response.Reason == "" {
		t.Fatalf("expected reason and confidence, got %+v", response)
	}
}

func TestGetImageCropSuggestionNoBorderIsNotAnError(t *testing.T) {
	dir := t.TempDir()
	path := writeSuggestionPNG(t, dir, "plain.png", borderFixture(120, 90, 0, color.NRGBA{}))

	response := NewCropTool().GetImageCropSuggestion(path)

	if !response.Success {
		t.Fatalf("expected a successful no-suggestion answer, got %+v", response.Error)
	}
	if response.Found {
		t.Fatalf("expected found=false, got (%d,%d,%d,%d)", response.X, response.Y, response.Width, response.Height)
	}
	if response.Error != nil {
		t.Fatalf("expected no error for a plain image, got %+v", response.Error)
	}
}

func TestGetImageCropSuggestionRejectsInvalidPath(t *testing.T) {
	tool := NewCropTool()

	blank := tool.GetImageCropSuggestion("   ")
	if blank.Success || blank.Error == nil || blank.Error.DetailCode != "IMAGE_CROP_SUGGESTION_INVALID_PATH" {
		t.Fatalf("expected invalid-path error, got %+v", blank)
	}

	missing := tool.GetImageCropSuggestion(filepath.Join(t.TempDir(), "missing.png"))
	if missing.Success || missing.Error == nil || missing.Error.DetailCode != "IMAGE_CROP_SUGGESTION_READ_FAILED" {
		t.Fatalf("expected read-failed error, got %+v", missing)
	}
}
