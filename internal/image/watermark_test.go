package image

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h2non/bimg"
)

// watermarkTextOptions is the shared valid text baseline for engine tests.
func watermarkTextOptions() WatermarkOptions {
	return WatermarkOptions{
		Kind:          watermarkKindText,
		Text:          "W",
		Bold:          true,
		Color:         "#FF0000",
		Anchor:        "bottom-right",
		MarginPercent: 3,
		ScalePercent:  5,
		Opacity:       1,
		Format:        "png",
		Quality:       92,
	}
}

// encodeWatermarkSolidPNG returns a deterministic solid PNG.
func encodeWatermarkSolidPNG(t *testing.T, width, height int, fill color.NRGBA) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, fill)
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode solid png: %v", err)
	}

	return buffer.Bytes()
}

func writeWatermarkSolidPNG(t *testing.T, path string, width, height int, fill color.NRGBA) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, encodeWatermarkSolidPNG(t, width, height, fill), 0o644); err != nil {
		t.Fatalf("write solid png %s: %v", path, err)
	}
}

// encodeWatermarkLogo builds a PNG logo. With transparentLeftHalf the left half
// is fully transparent so alpha compositing can be observed.
func encodeWatermarkLogo(t *testing.T, width, height int, transparentLeftHalf bool) []byte {
	t.Helper()

	logo := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			fill := color.NRGBA{R: 20, G: 60, B: 220, A: 255}
			if transparentLeftHalf && x < width/2 {
				fill = color.NRGBA{}
			}
			logo.SetNRGBA(x, y, fill)
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, logo); err != nil {
		t.Fatalf("encode logo png: %v", err)
	}

	return buffer.Bytes()
}

func watermarkDecodedImage(t *testing.T, data []byte) image.Image {
	t.Helper()

	decoded, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode watermark output: %v", err)
	}

	return decoded
}

// watermarkChangedBounds returns the inclusive-exclusive bounding box of every
// pixel that differs between the two decoded images.
func watermarkChangedBounds(t *testing.T, before, after image.Image) image.Rectangle {
	t.Helper()

	bounds := before.Bounds().Intersect(after.Bounds())
	changed := image.Rectangle{}
	found := false
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			left := color.NRGBAModel.Convert(before.At(x, y)).(color.NRGBA)
			right := color.NRGBAModel.Convert(after.At(x, y)).(color.NRGBA)
			if left == right {
				continue
			}

			pixel := image.Rect(x, y, x+1, y+1)
			if !found {
				changed = pixel
				found = true
				continue
			}
			changed = changed.Union(pixel)
		}
	}

	if !found {
		t.Fatal("expected the watermark to change at least one pixel")
	}

	return changed
}

func watermarkRegionEqual(before, after image.Image, region image.Rectangle) bool {
	region = region.Intersect(before.Bounds()).Intersect(after.Bounds())
	for y := region.Min.Y; y < region.Max.Y; y++ {
		for x := region.Min.X; x < region.Max.X; x++ {
			left := color.NRGBAModel.Convert(before.At(x, y)).(color.NRGBA)
			right := color.NRGBAModel.Convert(after.At(x, y)).(color.NRGBA)
			if left != right {
				return false
			}
		}
	}

	return true
}

func watermarkPixelDifferenceCount(before, after image.Image) int {
	bounds := before.Bounds().Intersect(after.Bounds())
	differences := 0
	for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
		for x := bounds.Min.X; x < bounds.Max.X; x++ {
			left := color.NRGBAModel.Convert(before.At(x, y)).(color.NRGBA)
			right := color.NRGBAModel.Convert(after.At(x, y)).(color.NRGBA)
			if left != right {
				differences++
			}
		}
	}

	return differences
}

// watermarkChangedColumnRuns counts separated vertical bands that contain at
// least one changed pixel. Tiled watermarks must produce several bands with
// untouched columns between them.
func watermarkChangedColumnRuns(before, after image.Image) (int, int) {
	bounds := before.Bounds().Intersect(after.Bounds())
	runs := 0
	unchanged := 0
	inRun := false
	for x := bounds.Min.X; x < bounds.Max.X; x++ {
		columnChanged := false
		for y := bounds.Min.Y; y < bounds.Max.Y; y++ {
			left := color.NRGBAModel.Convert(before.At(x, y)).(color.NRGBA)
			right := color.NRGBAModel.Convert(after.At(x, y)).(color.NRGBA)
			if left != right {
				columnChanged = true
				break
			}
		}

		switch {
		case columnChanged && !inRun:
			runs++
			inRun = true
		case !columnChanged && inRun:
			unchanged++
			inRun = false
		}
	}

	return runs, unchanged
}

// watermarkAssertThird fails when the changed box leaves the expected third of
// the image, which is how the nine anchors are verified.
func watermarkAssertThird(t *testing.T, changed image.Rectangle, width, height int, horizontal, vertical string) {
	t.Helper()

	switch horizontal {
	case "left":
		if changed.Min.X >= width/3 || changed.Max.X > width/3 {
			t.Fatalf("expected the watermark in the left third, got %v (width %d)", changed, width)
		}
	case "center":
		if changed.Min.X < width/3 || changed.Max.X > 2*width/3 {
			t.Fatalf("expected the watermark in the center third, got %v (width %d)", changed, width)
		}
	case "right":
		if changed.Min.X < 2*width/3 {
			t.Fatalf("expected the watermark in the right third, got %v (width %d)", changed, width)
		}
	}

	switch vertical {
	case "top":
		if changed.Min.Y >= height/3 || changed.Max.Y > height/3 {
			t.Fatalf("expected the watermark in the top third, got %v (height %d)", changed, height)
		}
	case "middle":
		if changed.Min.Y < height/3 || changed.Max.Y > 2*height/3 {
			t.Fatalf("expected the watermark in the middle third, got %v (height %d)", changed, height)
		}
	case "bottom":
		if changed.Min.Y < 2*height/3 {
			t.Fatalf("expected the watermark in the bottom third, got %v (height %d)", changed, height)
		}
	}
}

func TestWatermarkTextBottomRightChangesOnlyItsCorner(t *testing.T) {
	const width, height = 300, 240

	inputPath := filepath.Join(t.TempDir(), "photo.png")
	writePlanPNG(t, inputPath, width, height)
	input := readFixtureBytes(t, inputPath)

	output, outcome, err := ApplyWatermark(input, watermarkTextOptions())
	if err != nil {
		t.Fatalf("apply text watermark: %v", err)
	}
	if outcome.Width != width || outcome.Height != height {
		t.Fatalf("expected %dx%d output, got %dx%d", width, height, outcome.Width, outcome.Height)
	}
	if outcome.Anchor != "bottom-right" {
		t.Fatalf("expected bottom-right anchor, got %q", outcome.Anchor)
	}
	if outcome.FontSize <= 0 || outcome.MarginPixels <= 0 {
		t.Fatalf("expected a positive font size and margin, got %+v", outcome)
	}
	if outcome.LogoWidth != 0 {
		t.Fatalf("expected no logo width for a text watermark, got %d", outcome.LogoWidth)
	}

	before := watermarkDecodedImage(t, input)
	after := watermarkDecodedImage(t, output)

	changed := watermarkChangedBounds(t, before, after)
	watermarkAssertThird(t, changed, width, height, "right", "bottom")

	opposite := image.Rect(0, 0, 90, 70)
	if !watermarkRegionEqual(before, after, opposite) {
		t.Fatalf("expected the opposite corner %v to stay identical, changed box %v", opposite, changed)
	}
}

func TestWatermarkTextAnchorPlacement(t *testing.T) {
	const width, height = 300, 240

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 40, G: 40, B: 40, A: 255})
	before := watermarkDecodedImage(t, input)

	cases := []struct {
		anchor     string
		horizontal string
		vertical   string
	}{
		{anchor: "top-left", horizontal: "left", vertical: "top"},
		{anchor: "top-center", horizontal: "center", vertical: "top"},
		{anchor: "top-right", horizontal: "right", vertical: "top"},
		{anchor: "center-left", horizontal: "left", vertical: "middle"},
		{anchor: "center", horizontal: "center", vertical: "middle"},
		{anchor: "center-right", horizontal: "right", vertical: "middle"},
		{anchor: "bottom-left", horizontal: "left", vertical: "bottom"},
		{anchor: "bottom-center", horizontal: "center", vertical: "bottom"},
		{anchor: "bottom-right", horizontal: "right", vertical: "bottom"},
	}

	for _, testCase := range cases {
		t.Run(testCase.anchor, func(t *testing.T) {
			options := watermarkTextOptions()
			options.Text = "W"
			options.Anchor = testCase.anchor

			output, outcome, err := ApplyWatermark(input, options)
			if err != nil {
				t.Fatalf("apply %s watermark: %v", testCase.anchor, err)
			}
			if outcome.Anchor != testCase.anchor {
				t.Fatalf("expected anchor %q, got %q", testCase.anchor, outcome.Anchor)
			}

			changed := watermarkChangedBounds(t, before, watermarkDecodedImage(t, output))
			watermarkAssertThird(t, changed, width, height, testCase.horizontal, testCase.vertical)
		})
	}
}

func TestWatermarkTextOpacityBlendsWithBackground(t *testing.T) {
	const width, height = 400, 300

	background := color.NRGBA{R: 20, G: 100, B: 160, A: 255}
	input := encodeWatermarkSolidPNG(t, width, height, background)
	before := watermarkDecodedImage(t, input)

	options := watermarkTextOptions()
	options.Text = "W"
	options.ScalePercent = 8
	options.Opacity = 0.5

	output, outcome, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("apply translucent watermark: %v", err)
	}
	if outcome.FontSize != 24 {
		t.Fatalf("expected a 24 px font for an 8%% scale of 300, got %d", outcome.FontSize)
	}

	changed := watermarkChangedBounds(t, before, watermarkDecodedImage(t, output))

	// The most-covered glyph pixel must be a blend strictly between the
	// background and the pure text color, never equal to either.
	after := watermarkDecodedImage(t, output)
	best := image.Point{}
	bestScore := -1
	for y := changed.Min.Y; y < changed.Max.Y; y++ {
		for x := changed.Min.X; x < changed.Max.X; x++ {
			pixel := color.NRGBAModel.Convert(after.At(x, y)).(color.NRGBA)
			score := absInt(int(pixel.R)-int(background.R)) + absInt(int(pixel.G)-int(background.G)) + absInt(int(pixel.B)-int(background.B))
			if score > bestScore {
				bestScore = score
				best = image.Pt(x, y)
			}
		}
	}
	if bestScore <= 0 {
		t.Fatalf("expected a blended pixel inside %v", changed)
	}

	blended := color.NRGBAModel.Convert(after.At(best.X, best.Y)).(color.NRGBA)
	if blended.R <= background.R || blended.R >= 255 {
		t.Fatalf("expected red between %d and 255, got %d at %v", background.R, blended.R, best)
	}
	if blended.G >= background.G || blended.G < 0 {
		t.Fatalf("expected green between 0 and %d, got %d at %v", background.G, blended.G, best)
	}
	if blended.B >= background.B {
		t.Fatalf("expected blue between 0 and %d, got %d at %v", background.B, blended.B, best)
	}
}

func absInt(value int) int {
	if value < 0 {
		return -value
	}

	return value
}

func TestWatermarkBoldAndRegularDiffer(t *testing.T) {
	input := encodeWatermarkSolidPNG(t, 300, 240, color.NRGBA{R: 30, G: 30, B: 30, A: 255})

	regularOptions := watermarkTextOptions()
	regularOptions.Bold = false
	regularOptions.Text = "Watermark"

	boldOptions := regularOptions
	boldOptions.Bold = true

	regularOutput, _, err := ApplyWatermark(input, regularOptions)
	if err != nil {
		t.Fatalf("apply regular watermark: %v", err)
	}
	boldOutput, _, err := ApplyWatermark(input, boldOptions)
	if err != nil {
		t.Fatalf("apply bold watermark: %v", err)
	}

	regular := watermarkDecodedImage(t, regularOutput)
	bold := watermarkDecodedImage(t, boldOutput)
	if differences := watermarkPixelDifferenceCount(regular, bold); differences == 0 {
		t.Fatal("expected bold and regular faces to render different pixels")
	}
	if watermarkPixelDifferenceCount(watermarkDecodedImage(t, input), regular) == 0 {
		t.Fatal("expected the regular face to draw pixels")
	}
}

func TestWatermarkLogoChangesAnchorCorner(t *testing.T) {
	const width, height = 400, 300

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 60, G: 60, B: 60, A: 255})
	before := watermarkDecodedImage(t, input)
	logo := encodeWatermarkLogo(t, 64, 32, false)

	options := WatermarkOptions{
		Kind:          watermarkKindLogo,
		Logo:          logo,
		Anchor:        "top-left",
		MarginPercent: 3,
		ScalePercent:  20,
		Opacity:       1,
		Format:        "png",
		Quality:       92,
	}

	output, outcome, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("apply logo watermark: %v", err)
	}
	if outcome.LogoWidth != 64 {
		t.Fatalf("expected the source logo width 64 (no upscale), got %d", outcome.LogoWidth)
	}
	if outcome.FontSize != 0 {
		t.Fatalf("expected no font size for a logo watermark, got %d", outcome.FontSize)
	}

	after := watermarkDecodedImage(t, output)
	changed := watermarkChangedBounds(t, before, after)

	margin := outcome.MarginPixels
	expected := image.Rect(margin, margin, margin+64, margin+32)
	if !changed.In(expected) {
		t.Fatalf("expected the logo inside %v, got %v", expected, changed)
	}

	untouched := image.Rect(200, 150, width, height)
	if !watermarkRegionEqual(before, after, untouched) {
		t.Fatalf("expected the opposite corner %v to stay identical", untouched)
	}
}

func TestWatermarkLogoAlphaComposites(t *testing.T) {
	const width, height = 400, 300

	background := color.NRGBA{R: 90, G: 90, B: 90, A: 255}
	input := encodeWatermarkSolidPNG(t, width, height, background)
	logo := encodeWatermarkLogo(t, 100, 100, true)

	options := WatermarkOptions{
		Kind:          watermarkKindLogo,
		Logo:          logo,
		Anchor:        "center",
		MarginPercent: 3,
		ScalePercent:  20,
		Opacity:       1,
		Format:        "png",
		Quality:       92,
	}

	output, outcome, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("apply translucent logo: %v", err)
	}
	if outcome.LogoWidth != 80 {
		t.Fatalf("expected an 80 px logo (20%% of 400), got %d", outcome.LogoWidth)
	}

	after := watermarkDecodedImage(t, output)

	// The logo is centered at (200,150) and 80 px wide: x 160..240.
	transparentPixel := color.NRGBAModel.Convert(after.At(180, 150)).(color.NRGBA)
	if transparentPixel != background {
		t.Fatalf("expected the transparent half to keep the background %v, got %v", background, transparentPixel)
	}

	opaquePixel := color.NRGBAModel.Convert(after.At(220, 150)).(color.NRGBA)
	if !watermarkNearColor(opaquePixel, color.NRGBA{R: 20, G: 60, B: 220, A: 255}, 4) {
		t.Fatalf("expected the opaque half to composite the logo color, got %v", opaquePixel)
	}
}

func watermarkNearColor(got, want color.NRGBA, tolerance int) bool {
	if absInt(int(got.R)-int(want.R)) > tolerance {
		return false
	}
	if absInt(int(got.G)-int(want.G)) > tolerance {
		return false
	}
	if absInt(int(got.B)-int(want.B)) > tolerance {
		return false
	}

	return absInt(int(got.A)-int(want.A)) <= tolerance
}

func TestWatermarkLogoNeverUpscales(t *testing.T) {
	const width, height = 400, 300

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 50, G: 50, B: 50, A: 255})
	before := watermarkDecodedImage(t, input)
	logo := encodeWatermarkLogo(t, 40, 40, false)

	options := WatermarkOptions{
		Kind:          watermarkKindLogo,
		Logo:          logo,
		Anchor:        "bottom-right",
		MarginPercent: 3,
		ScalePercent:  60,
		Opacity:       1,
		Format:        "png",
		Quality:       92,
	}

	output, outcome, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("apply oversized logo request: %v", err)
	}
	if outcome.LogoWidth != 40 {
		t.Fatalf("expected the source width 40 to win over a 240 px request, got %d", outcome.LogoWidth)
	}

	changed := watermarkChangedBounds(t, before, watermarkDecodedImage(t, output))
	if changed.Dx() > 40 || changed.Dy() > 40 {
		t.Fatalf("expected a logo no larger than 40x40, got %v", changed)
	}
}

func TestWatermarkTileSpreadsSeparateRegions(t *testing.T) {
	const width, height = 500, 350

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 35, G: 35, B: 35, A: 255})
	before := watermarkDecodedImage(t, input)

	options := watermarkTextOptions()
	options.Text = "WM"
	options.Tile = true
	options.RotationDegrees = -30

	output, outcome, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("apply tiled watermark: %v", err)
	}
	if outcome.FontSize != 18 {
		t.Fatalf("expected an 18 px font for a 5%% scale of 350, got %d", outcome.FontSize)
	}

	after := watermarkDecodedImage(t, output)
	changed := watermarkChangedBounds(t, before, after)
	if changed.Dx() < width/2 || changed.Dy() < height/2 {
		t.Fatalf("expected the tiled watermark to cover most of the image, got %v", changed)
	}

	runs, unchangedColumns := watermarkChangedColumnRuns(before, after)
	if runs < 3 {
		t.Fatalf("expected at least 3 separated tile bands, got %d", runs)
	}
	if unchangedColumns == 0 {
		t.Fatal("expected untouched columns between the tile bands")
	}
}

func TestWatermarkEncodesRequestedFormats(t *testing.T) {
	const width, height = 200, 150

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 120, G: 80, B: 40, A: 255})

	cases := []struct {
		format string
	}{
		{format: "jpeg"},
		{format: "png"},
		{format: "webp"},
	}

	for _, testCase := range cases {
		t.Run(testCase.format, func(t *testing.T) {
			options := watermarkTextOptions()
			options.Format = testCase.format

			output, outcome, err := ApplyWatermark(input, options)
			if err != nil {
				t.Fatalf("apply %s watermark: %v", testCase.format, err)
			}
			if got := bimg.DetermineImageTypeName(output); got != testCase.format {
				t.Fatalf("expected %s output, got %s", testCase.format, got)
			}
			if outcome.Width != width || outcome.Height != height {
				t.Fatalf("expected %dx%d output, got %dx%d", width, height, outcome.Width, outcome.Height)
			}

			size, sizeErr := bimg.NewImage(output).Size()
			if sizeErr != nil {
				t.Fatalf("read %s output size: %v", testCase.format, sizeErr)
			}
			if size.Width != width || size.Height != height {
				t.Fatalf("expected encoded %dx%d, got %dx%d", width, height, size.Width, size.Height)
			}
		})
	}
}

func TestWatermarkRejectsInvalidOptions(t *testing.T) {
	input := encodeWatermarkSolidPNG(t, 200, 150, color.NRGBA{R: 60, G: 60, B: 60, A: 255})
	logo := encodeWatermarkLogo(t, 40, 40, false)

	logoCase := func() WatermarkOptions {
		return WatermarkOptions{
			Kind:          watermarkKindLogo,
			Logo:          logo,
			Anchor:        "bottom-right",
			MarginPercent: 3,
			ScalePercent:  20,
			Opacity:       1,
			Format:        "png",
			Quality:       92,
		}
	}

	cases := []struct {
		name   string
		mutate func(WatermarkOptions) WatermarkOptions
		want   string
	}{
		{
			name:   "invalid kind",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.Kind = "sticker"; return options },
			want:   "kind must be text or logo",
		},
		{
			name:   "empty text",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.Text = "   "; return options },
			want:   "text is required",
		},
		{
			name: "too many characters",
			mutate: func(options WatermarkOptions) WatermarkOptions {
				options.Text = strings.Repeat("a", 201)
				return options
			},
			want: "at most 200 characters",
		},
		{
			name: "too many lines",
			mutate: func(options WatermarkOptions) WatermarkOptions {
				options.Text = strings.Repeat("a\n", 10) + "z"
				return options
			},
			want: "at most 10 lines",
		},
		{
			name:   "control characters",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.Text = "a\x01b"; return options },
			want:   "control characters",
		},
		{
			name: "missing logo bytes",
			mutate: func(options WatermarkOptions) WatermarkOptions {
				options = logoCase()
				options.Logo = nil
				return options
			},
			want: "logo bytes are required",
		},
		{
			name: "tile with logo",
			mutate: func(options WatermarkOptions) WatermarkOptions {
				options = logoCase()
				options.Tile = true
				return options
			},
			want: "tile is only supported for text watermarks",
		},
		{
			name:   "invalid anchor",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.Anchor = "middle"; return options },
			want:   "anchor is invalid",
		},
		{
			name:   "margin below range",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.MarginPercent = 1.5; return options },
			want:   "marginPercent must be between 2 and 5",
		},
		{
			name:   "margin above range",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.MarginPercent = 5.5; return options },
			want:   "marginPercent must be between 2 and 5",
		},
		{
			name:   "text scale below range",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.ScalePercent = 2; return options },
			want:   "scalePercent must be between 3 and 8",
		},
		{
			name:   "text scale above range",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.ScalePercent = 9; return options },
			want:   "scalePercent must be between 3 and 8",
		},
		{
			name: "logo scale below range",
			mutate: func(options WatermarkOptions) WatermarkOptions {
				options = logoCase()
				options.ScalePercent = 4
				return options
			},
			want: "scalePercent must be between 5 and 60",
		},
		{
			name: "logo scale above range",
			mutate: func(options WatermarkOptions) WatermarkOptions {
				options = logoCase()
				options.ScalePercent = 61
				return options
			},
			want: "scalePercent must be between 5 and 60",
		},
		{
			name:   "opacity below range",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.Opacity = 0.01; return options },
			want:   "opacity must be between 0.05 and 1",
		},
		{
			name:   "opacity above range",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.Opacity = 1.5; return options },
			want:   "opacity must be between 0.05 and 1",
		},
		{
			name:   "rotation below range",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.RotationDegrees = -91; return options },
			want:   "rotationDegrees must be between -90 and 90",
		},
		{
			name:   "rotation above range",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.RotationDegrees = 91; return options },
			want:   "rotationDegrees must be between -90 and 90",
		},
		{
			name:   "unsupported format",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.Format = "gif"; return options },
			want:   "format must be jpeg, png or webp",
		},
		{
			name:   "missing format",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.Format = ""; return options },
			want:   "format is required",
		},
		{
			name:   "invalid color",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.Color = "red"; return options },
			want:   "#RRGGBB",
		},
		{
			name:   "quality above range",
			mutate: func(options WatermarkOptions) WatermarkOptions { options.Quality = 101; return options },
			want:   "quality must be between 1 and 100",
		},
		{
			name: "undecodable logo",
			mutate: func(options WatermarkOptions) WatermarkOptions {
				options = logoCase()
				options.Logo = []byte("not a logo")
				return options
			},
			want: "decode logo",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			options := testCase.mutate(watermarkTextOptions())

			if _, _, err := ApplyWatermark(input, options); err == nil {
				t.Fatalf("expected an error containing %q", testCase.want)
			} else if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected an error containing %q, got %q", testCase.want, err.Error())
			}
		})
	}

	t.Run("undecodable input", func(t *testing.T) {
		if _, _, err := ApplyWatermark([]byte("not an image"), watermarkTextOptions()); err == nil {
			t.Fatal("expected undecodable input to fail")
		} else if !strings.Contains(err.Error(), "decode image") {
			t.Fatalf("expected a decode image error, got %q", err.Error())
		}
	})
}

// watermarkQuadrantColors is the stored top-left, top-right, bottom-left,
// bottom-right arrangement used by the EXIF orientation fixtures.
var watermarkQuadrantColors = [4]color.NRGBA{
	{R: 255, A: 255},
	{G: 255, A: 255},
	{B: 255, A: 255},
	{R: 255, G: 255, A: 255},
}

// encodeWatermarkQuadrantJPEG renders a solid 2x2 quadrant JPEG. JPEG keeps the
// fixture cheap while the four saturated colors make every EXIF mapping
// unambiguous.
func encodeWatermarkQuadrantJPEG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		row := 0
		if y >= height/2 {
			row = 2
		}
		for x := 0; x < width; x++ {
			column := 0
			if x >= width/2 {
				column = 1
			}
			img.SetNRGBA(x, y, watermarkQuadrantColors[row+column])
		}
	}

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: 100}); err != nil {
		t.Fatalf("encode quadrant jpeg: %v", err)
	}

	return buffer.Bytes()
}

// injectWatermarkEXIFOrientation inserts an APP1 EXIF segment with the given
// orientation tag. The segment layout mirrors the existing compressor fixture.
func injectWatermarkEXIFOrientation(t *testing.T, jpegBytes []byte, orientation byte) []byte {
	t.Helper()

	if len(jpegBytes) < 2 || jpegBytes[0] != 0xFF || jpegBytes[1] != 0xD8 {
		t.Fatal("expected a jpeg fixture")
	}

	payload := []byte("Exif\x00\x00")
	payload = append(payload,
		0x49, 0x49, 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, // little-endian TIFF header
		0x01, 0x00, // one IFD entry
		0x12, 0x01, 0x03, 0x00, 0x01, 0x00, 0x00, 0x00, orientation, 0x00, 0x00, 0x00,
		0x00, 0x00, 0x00, 0x00, // no next IFD
	)

	segmentLength := len(payload) + 2
	segment := []byte{0xFF, 0xE1, byte(segmentLength >> 8), byte(segmentLength & 0xFF)}
	segment = append(segment, payload...)

	out := make([]byte, 0, len(jpegBytes)+len(segment))
	out = append(out, jpegBytes[:2]...)
	out = append(out, segment...)
	out = append(out, jpegBytes[2:]...)

	return out
}

// watermarkDisplayColors returns the stored TL/TR/BL/BR quadrant colors after
// the standard EXIF display transform for the orientation.
func watermarkDisplayColors(orientation int) [4]color.NRGBA {
	red := watermarkQuadrantColors[0]
	green := watermarkQuadrantColors[1]
	blue := watermarkQuadrantColors[2]
	yellow := watermarkQuadrantColors[3]

	switch orientation {
	case 2: // mirror horizontal
		return [4]color.NRGBA{green, red, yellow, blue}
	case 3: // rotate 180
		return [4]color.NRGBA{yellow, blue, green, red}
	case 4: // mirror vertical
		return [4]color.NRGBA{blue, yellow, red, green}
	case 5: // transpose
		return [4]color.NRGBA{red, blue, green, yellow}
	case 6: // rotate 90 CW
		return [4]color.NRGBA{blue, red, yellow, green}
	case 7: // transverse
		return [4]color.NRGBA{yellow, green, blue, red}
	case 8: // rotate 270 CW
		return [4]color.NRGBA{green, yellow, red, blue}
	default:
		return [4]color.NRGBA{red, green, blue, yellow}
	}
}

// watermarkAveragePatch averages an interior patch so JPEG block noise does not
// make the quadrant mapping assertion brittle.
func watermarkAveragePatch(img image.Image, center image.Point, radius int) color.NRGBA {
	var red, green, blue, alpha uint64
	var count uint64
	for y := center.Y - radius; y <= center.Y+radius; y++ {
		for x := center.X - radius; x <= center.X+radius; x++ {
			if !image.Pt(x, y).In(img.Bounds()) {
				continue
			}
			pixel := color.NRGBAModel.Convert(img.At(x, y)).(color.NRGBA)
			red += uint64(pixel.R)
			green += uint64(pixel.G)
			blue += uint64(pixel.B)
			alpha += uint64(pixel.A)
			count++
		}
	}
	if count == 0 {
		return color.NRGBA{}
	}

	return color.NRGBA{
		R: uint8(red / count),
		G: uint8(green / count),
		B: uint8(blue / count),
		A: uint8(alpha / count),
	}
}

func TestWatermarkNormalizesEXIFOrientation(t *testing.T) {
	const storedWidth, storedHeight = 240, 160

	stored := encodeWatermarkQuadrantJPEG(t, storedWidth, storedHeight)
	cases := []struct {
		orientation int
		swapped     bool
	}{
		{orientation: 2},
		{orientation: 3},
		{orientation: 4},
		{orientation: 5, swapped: true},
		{orientation: 6, swapped: true},
		{orientation: 7, swapped: true},
		{orientation: 8, swapped: true},
	}

	for _, testCase := range cases {
		t.Run(fmt.Sprintf("orientation-%d", testCase.orientation), func(t *testing.T) {
			input := injectWatermarkEXIFOrientation(t, stored, byte(testCase.orientation))

			metadata, metaErr := bimg.NewImage(input).Metadata()
			if metaErr != nil || metadata.Orientation != testCase.orientation {
				t.Skipf("libvips build does not read the injected EXIF orientation (orientation=%d err=%v)", metadata.Orientation, metaErr)
			}

			options := watermarkTextOptions()
			options.Anchor = "bottom-right"

			output, outcome, err := ApplyWatermark(input, options)
			if err != nil {
				t.Fatalf("watermark oriented jpeg: %v", err)
			}

			// The reference mapping is hard-coded from the EXIF specification; it
			// never goes through bimg's normalization path.
			wantWidth, wantHeight := storedWidth, storedHeight
			if testCase.swapped {
				wantWidth, wantHeight = storedHeight, storedWidth
			}
			if outcome.Width != wantWidth || outcome.Height != wantHeight {
				t.Fatalf("expected the visual axes %dx%d, got %dx%d", wantWidth, wantHeight, outcome.Width, outcome.Height)
			}

			outputMetadata, outputMetaErr := bimg.NewImage(output).Metadata()
			if outputMetaErr == nil && outputMetadata.Orientation == testCase.orientation {
				t.Fatal("expected the stale orientation tag to be stripped")
			}

			decoded := watermarkDecodedImage(t, output)
			expected := watermarkDisplayColors(testCase.orientation)
			centers := [4]image.Point{
				{X: wantWidth / 4, Y: wantHeight / 4},
				{X: 3 * wantWidth / 4, Y: wantHeight / 4},
				{X: wantWidth / 4, Y: 3 * wantHeight / 4},
				{X: 3 * wantWidth / 4, Y: 3 * wantHeight / 4},
			}
			for index, center := range centers {
				got := watermarkAveragePatch(decoded, center, 10)
				if !watermarkNearColor(got, expected[index], 16) {
					t.Fatalf("quadrant %d: expected the EXIF mapping color %v, got %v", index, expected[index], got)
				}
			}
		})
	}
}

func TestWatermarkNormalizesLogoOrientation(t *testing.T) {
	stored := encodeWatermarkQuadrantJPEG(t, 60, 60)
	input := encodeWatermarkSolidPNG(t, 400, 300, color.NRGBA{R: 60, G: 60, B: 60, A: 255})

	for orientation := 2; orientation <= 8; orientation++ {
		t.Run(fmt.Sprintf("orientation-%d", orientation), func(t *testing.T) {
			logo := injectWatermarkEXIFOrientation(t, stored, byte(orientation))

			metadata, metaErr := bimg.NewImage(logo).Metadata()
			if metaErr != nil || metadata.Orientation != orientation {
				t.Skipf("libvips build does not read the injected logo orientation (orientation=%d err=%v)", metadata.Orientation, metaErr)
			}

			options := WatermarkOptions{
				Kind:          watermarkKindLogo,
				Logo:          logo,
				Anchor:        "top-left",
				MarginPercent: 3,
				ScalePercent:  20,
				Opacity:       1,
				Format:        "png",
				Quality:       92,
			}

			output, outcome, err := ApplyWatermark(input, options)
			if err != nil {
				t.Fatalf("apply oriented logo: %v", err)
			}
			if outcome.LogoWidth != 60 {
				t.Fatalf("expected the normalized logo width 60, got %d", outcome.LogoWidth)
			}

			// The expected mapping is hard-coded from the EXIF specification, so
			// this does not reuse bimg's normalization path.
			decoded := watermarkDecodedImage(t, output)
			margin := outcome.MarginPixels
			centers := [4]image.Point{
				{X: margin + 15, Y: margin + 15},
				{X: margin + 45, Y: margin + 15},
				{X: margin + 15, Y: margin + 45},
				{X: margin + 45, Y: margin + 45},
			}
			expected := watermarkDisplayColors(orientation)
			for index, center := range centers {
				got := watermarkAveragePatch(decoded, center, 3)
				if !watermarkNearColor(got, expected[index], 18) {
					t.Fatalf("logo quadrant %d: expected the EXIF mapping color %v, got %v", index, expected[index], got)
				}
			}
		})
	}
}

func TestWatermarkRejectsUnsupportedScripts(t *testing.T) {
	input := encodeWatermarkSolidPNG(t, 400, 300, color.NRGBA{R: 60, G: 60, B: 60, A: 255})

	cases := []struct {
		name string
		text string
	}{
		{name: "cjk", text: "漢字"},
		{name: "arabic", text: "مرحبا"},
		{name: "devanagari", text: "नमस्ते"},
		{name: "emoji", text: "😀"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			options := watermarkTextOptions()
			options.Text = testCase.text

			_, _, err := ApplyWatermark(input, options)
			if err == nil {
				t.Fatalf("expected %q to be rejected", testCase.text)
			}
			if !strings.Contains(err.Error(), "bundled font does not include") {
				t.Fatalf("expected a missing-glyph error, got %q", err.Error())
			}
			if !strings.Contains(err.Error(), "logo watermark") {
				t.Fatalf("expected a logo watermark suggestion, got %q", err.Error())
			}
		})
	}
}

func TestWatermarkAcceptsLatinGreekCyrillic(t *testing.T) {
	input := encodeWatermarkSolidPNG(t, 400, 300, color.NRGBA{R: 60, G: 60, B: 60, A: 255})
	before := watermarkDecodedImage(t, input)

	cases := []struct {
		name string
		text string
	}{
		{name: "latin", text: "Watermark"},
		{name: "greek", text: "Γειά σου"},
		{name: "cyrillic", text: "Привет мир"},
		{name: "decomposed accent", text: "e\u0301"},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			options := watermarkTextOptions()
			options.Text = testCase.text

			output, outcome, err := ApplyWatermark(input, options)
			if err != nil {
				t.Fatalf("expected %q to render, got %v", testCase.text, err)
			}
			if outcome.FontSize <= 0 {
				t.Fatalf("expected a positive font size, got %+v", outcome)
			}

			changed := watermarkChangedBounds(t, before, watermarkDecodedImage(t, output))
			if changed.Empty() {
				t.Fatalf("expected %q to change pixels", testCase.text)
			}
		})
	}
}

func TestWatermarkTextAutoFitsVerticalBox(t *testing.T) {
	const width, height = 300, 240

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 30, G: 30, B: 30, A: 255})
	lines := make([]string, watermarkMaxTextLines)
	for index := range lines {
		lines[index] = fmt.Sprintf("Line %d", index+1)
	}

	options := watermarkTextOptions()
	options.Text = strings.Join(lines, "\n")
	options.ScalePercent = 8

	output, outcome, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("apply auto-fit watermark: %v", err)
	}
	if outcome.FontSize >= 19 {
		t.Fatalf("expected auto-fit below the requested 19 px, got %d", outcome.FontSize)
	}
	if outcome.FontSize < watermarkMinFontSize {
		t.Fatalf("expected the 12 px floor at minimum, got %d", outcome.FontSize)
	}

	before := watermarkDecodedImage(t, input)
	changed := watermarkChangedBounds(t, before, watermarkDecodedImage(t, output))
	if changed.Min.X <= 0 || changed.Min.Y <= 0 || changed.Max.X >= width || changed.Max.Y >= height {
		t.Fatalf("expected ink strictly inside the canvas, got %v", changed)
	}
}

func TestWatermarkTextAutoFitRejectsUnfittableBlock(t *testing.T) {
	const width, height = 100, 100

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 30, G: 30, B: 30, A: 255})
	lines := make([]string, watermarkMaxTextLines)
	for index := range lines {
		lines[index] = fmt.Sprintf("Line %d", index+1)
	}

	options := watermarkTextOptions()
	options.Text = strings.Join(lines, "\n")
	options.ScalePercent = 8

	_, _, err := ApplyWatermark(input, options)
	if err == nil {
		t.Fatal("expected an unfittable block to fail instead of clipping")
	}
	if !strings.Contains(err.Error(), "text does not fit the image at the minimum font size") {
		t.Fatalf("expected a clear auto-fit error, got %q", err.Error())
	}
}

func TestWatermarkTileCoversTallCanvas(t *testing.T) {
	const width, height = 400, 800

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 35, G: 35, B: 35, A: 255})
	before := watermarkDecodedImage(t, input)

	options := watermarkTextOptions()
	options.Text = "TILE"
	options.Tile = true
	options.RotationDegrees = 0

	output, _, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("apply tall tiled watermark: %v", err)
	}

	changed := watermarkChangedBounds(t, before, watermarkDecodedImage(t, output))
	if changed.Min.Y >= height/4 {
		t.Fatalf("expected ink in the first quarter, got %v", changed)
	}
	if changed.Max.Y <= 3*height/4 {
		t.Fatalf("expected ink in the last quarter, got %v", changed)
	}
}

func TestWatermarkTileCapsStampWidth(t *testing.T) {
	const width, height = 3000, 2000

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 35, G: 35, B: 35, A: 255})

	options := watermarkTextOptions()
	options.Text = strings.Repeat("W", watermarkMaxTextRunes)
	options.Tile = true
	options.RotationDegrees = 0
	options.ScalePercent = 8

	output, outcome, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("expected the stamp to shrink instead of failing, got %v", err)
	}
	if len(output) == 0 {
		t.Fatal("expected encoded output")
	}
	if outcome.FontSize >= 160 {
		t.Fatalf("expected the stamp font to shrink below the requested 160 px, got %d", outcome.FontSize)
	}
	if outcome.FontSize < watermarkMinFontSize {
		t.Fatalf("expected the 12 px floor at minimum, got %d", outcome.FontSize)
	}
	if outcome.Width != width || outcome.Height != height {
		t.Fatalf("expected %dx%d output, got %dx%d", width, height, outcome.Width, outcome.Height)
	}

	// The drawn stamp and its visible ink must both fit the canvas width.
	stamp, stampErr := watermarkRenderTileStamp(options.Text, options.Bold, outcome.FontSize, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
	if stampErr != nil {
		t.Fatalf("render the capped stamp: %v", stampErr)
	}
	if stamp.Bounds().Dx() > width {
		t.Fatalf("expected the stamp width %d to fit the canvas %d", stamp.Bounds().Dx(), width)
	}
	ink := watermarkAlphaBounds(stamp)
	if ink.Empty() {
		t.Fatal("expected visible stamp ink")
	}
	if ink.Dx() > width {
		t.Fatalf("expected the drawn ink width %d to fit the canvas %d", ink.Dx(), width)
	}
}

func TestWatermarkTileRejectsStampWiderThanCanvas(t *testing.T) {
	input := encodeWatermarkSolidPNG(t, 600, 600, color.NRGBA{R: 35, G: 35, B: 35, A: 255})

	options := watermarkTextOptions()
	options.Text = strings.Repeat("W", watermarkMaxTextRunes)
	options.Tile = true
	options.RotationDegrees = 0
	options.ScalePercent = 8

	_, _, err := ApplyWatermark(input, options)
	if err == nil {
		t.Fatal("expected an over-wide stamp to fail")
	}
	if !strings.Contains(err.Error(), "too wide to tile") {
		t.Fatalf("expected a clear over-wide error, got %q", err.Error())
	}
}

func TestWatermarkTileRotationAndTransparency(t *testing.T) {
	const width, height = 500, 350

	background := color.NRGBA{R: 30, G: 30, B: 30, A: 255}
	input := encodeWatermarkSolidPNG(t, width, height, background)
	before := watermarkDecodedImage(t, input)

	rotationOptions := func(rotation float64) WatermarkOptions {
		options := watermarkTextOptions()
		options.Text = "WM"
		options.Tile = true
		options.RotationDegrees = rotation
		return options
	}

	flatOutput, _, err := ApplyWatermark(input, rotationOptions(0))
	if err != nil {
		t.Fatalf("apply unrotated tile: %v", err)
	}
	rotatedOutput, rotatedOutcome, err := ApplyWatermark(input, rotationOptions(-30))
	if err != nil {
		t.Fatalf("apply rotated tile: %v", err)
	}
	if rotatedOutcome.FontSize != 18 {
		t.Fatalf("expected an 18 px tile font for a 5%% scale of 350, got %d", rotatedOutcome.FontSize)
	}

	flat := watermarkDecodedImage(t, flatOutput)
	rotated := watermarkDecodedImage(t, rotatedOutput)

	flatChanged := watermarkChangedBounds(t, before, flat)
	rotatedChanged := watermarkChangedBounds(t, before, rotated)
	if flatChanged.Empty() || rotatedChanged.Empty() {
		t.Fatal("expected both tile renders to change pixels")
	}
	if differences := watermarkPixelDifferenceCount(flat, rotated); differences < 200 {
		t.Fatalf("expected the rotation to change the drawn pixels materially, got %d differences", differences)
	}

	// Text is red over a dark gray background: a correct alpha composite can
	// only raise the red channel. A transparent rotation corner incorrectly
	// composited as black would lower it.
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			got := color.NRGBAModel.Convert(rotated.At(x, y)).(color.NRGBA)
			original := color.NRGBAModel.Convert(before.At(x, y)).(color.NRGBA)
			if got == original {
				continue
			}
			if got.R < original.R {
				t.Fatalf("pixel (%d,%d) darkened the background: %v -> %v", x, y, original, got)
			}
		}
	}
}

// watermarkAnalyticBlend returns the expected opaque NRGBA when an opaque source
// color is composited over an opaque background with the opacity byte the
// engine applies.
func watermarkAnalyticBlend(background, source color.NRGBA, opacity float64) color.NRGBA {
	alpha := math.Round(opacity * 255)
	blend := func(back, front uint8) uint8 {
		return uint8(math.Round(float64(front)*alpha/255 + float64(back)*(255-alpha)/255))
	}

	return color.NRGBA{
		R: blend(background.R, source.R),
		G: blend(background.G, source.G),
		B: blend(background.B, source.B),
		A: 255,
	}
}

func TestWatermarkTextOpacityAppliesAlphaOnce(t *testing.T) {
	const width, height = 800, 600

	background := color.NRGBA{R: 20, G: 100, B: 160, A: 255}
	input := encodeWatermarkSolidPNG(t, width, height, background)

	options := watermarkTextOptions()
	options.Text = "W"
	options.ScalePercent = 8
	options.Opacity = 0.5
	options.Anchor = "center"

	output, outcome, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("apply half-opacity text: %v", err)
	}
	if outcome.FontSize != 48 {
		t.Fatalf("expected a 48 px font, got %d", outcome.FontSize)
	}

	expected := watermarkAnalyticBlend(background, color.NRGBA{R: 255, A: 255}, 0.5)
	decoded := watermarkDecodedImage(t, output)
	found := false
	for y := 0; y < height && !found; y++ {
		for x := 0; x < width; x++ {
			pixel := color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
			if watermarkNearColor(pixel, expected, 1) {
				found = true
				break
			}
		}
	}
	if !found {
		t.Fatalf("expected a fully covered glyph pixel at the analytic 50%% blend %v", expected)
	}
}

func TestWatermarkLogoOpacityAppliesAlphaOnce(t *testing.T) {
	const width, height = 400, 300

	background := color.NRGBA{R: 40, G: 80, B: 120, A: 255}
	logoColor := color.NRGBA{R: 20, G: 60, B: 220, A: 255}
	input := encodeWatermarkSolidPNG(t, width, height, background)
	logo := encodeWatermarkLogo(t, 60, 30, false)

	options := WatermarkOptions{
		Kind:          watermarkKindLogo,
		Logo:          logo,
		Anchor:        "top-left",
		MarginPercent: 3,
		ScalePercent:  20,
		Opacity:       0.5,
		Format:        "png",
		Quality:       92,
	}

	output, outcome, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("apply half-opacity logo: %v", err)
	}
	if outcome.LogoWidth != 60 {
		t.Fatalf("expected the source width 60, got %d", outcome.LogoWidth)
	}

	decoded := watermarkDecodedImage(t, output)
	margin := outcome.MarginPixels
	got := watermarkAveragePatch(decoded, image.Pt(margin+30, margin+15), 4)
	expected := watermarkAnalyticBlend(background, logoColor, 0.5)
	if !watermarkNearColor(got, expected, 2) {
		t.Fatalf("expected a single 50%% composite %v, got %v", expected, got)
	}

	double := watermarkAnalyticBlend(background, logoColor, 64.0/255)
	if watermarkNearColor(got, double, 2) {
		t.Fatalf("expected the alpha to be applied once, got the double-multiplied blend %v", double)
	}
}

func TestWatermarkTextAutoFitsHorizontalBox(t *testing.T) {
	const width, height = 300, 240

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 30, G: 30, B: 30, A: 255})

	options := watermarkTextOptions()
	// A long single line that fits only after shrinking both dimensions.
	options.Text = strings.Repeat("M", 20)
	options.ScalePercent = 8

	output, outcome, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("expected the long line to auto-fit, got %v", err)
	}
	if outcome.FontSize >= 19 {
		t.Fatalf("expected the font to shrink below the requested 19 px, got %d", outcome.FontSize)
	}
	if outcome.FontSize < watermarkMinFontSize {
		t.Fatalf("expected the 12 px floor at minimum, got %d", outcome.FontSize)
	}

	before := watermarkDecodedImage(t, input)
	changed := watermarkChangedBounds(t, before, watermarkDecodedImage(t, output))
	if changed.Min.X <= 0 || changed.Min.Y <= 0 || changed.Max.X >= width || changed.Max.Y >= height {
		t.Fatalf("expected ink strictly inside the canvas, got %v", changed)
	}
}

func TestWatermarkTextAutoFitRejectsWideLine(t *testing.T) {
	const width, height = 300, 240

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 30, G: 30, B: 30, A: 255})

	options := watermarkTextOptions()
	options.Text = strings.Repeat("M", watermarkMaxTextRunes)
	options.ScalePercent = 8

	_, _, err := ApplyWatermark(input, options)
	if err == nil {
		t.Fatal("expected an unfittable wide line to fail instead of clipping")
	}
	if !strings.Contains(err.Error(), "text does not fit the image at the minimum font size") {
		t.Fatalf("expected a clear auto-fit error, got %q", err.Error())
	}
}

func TestWatermarkTileInkGapsAreBalanced(t *testing.T) {
	const width, height = 600, 400

	input := encodeWatermarkSolidPNG(t, width, height, color.NRGBA{R: 35, G: 35, B: 35, A: 255})
	before := watermarkDecodedImage(t, input)

	options := watermarkTextOptions()
	options.Text = "WM"
	options.Tile = true
	options.RotationDegrees = 0

	output, outcome, err := ApplyWatermark(input, options)
	if err != nil {
		t.Fatalf("apply unrotated tile: %v", err)
	}

	changed := watermarkChangedBounds(t, before, watermarkDecodedImage(t, output))
	topGap := changed.Min.Y
	bottomGap := height - changed.Max.Y
	if absInt(topGap-bottomGap) > outcome.FontSize {
		t.Fatalf("expected balanced top/bottom ink gaps, got top=%d bottom=%d (font %d)", topGap, bottomGap, outcome.FontSize)
	}
}
