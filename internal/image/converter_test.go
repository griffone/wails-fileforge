package image

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h2non/bimg"
)

func readFixtureBytes(t *testing.T, path string) []byte {
	t.Helper()

	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read fixture %s: %v", path, err)
	}

	return content
}

func convertedSize(t *testing.T, output []byte) (int, int) {
	t.Helper()

	size, err := bimg.NewImage(output).Size()
	if err != nil {
		t.Fatalf("read converted size: %v", err)
	}

	return size.Width, size.Height
}

func writeHalfPNG(t *testing.T, path string, width, height int) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			if x < width/2 {
				img.SetNRGBA(x, y, color.NRGBA{R: 255, A: 255})
			} else {
				img.SetNRGBA(x, y, color.NRGBA{B: 255, A: 255})
			}
		}
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create png fixture %s: %v", path, err)
	}
	if err := png.Encode(file, img); err != nil {
		_ = file.Close()
		t.Fatalf("encode png fixture %s: %v", path, err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close png fixture %s: %v", path, err)
	}
}

func pixelAt(t *testing.T, encoded []byte, x, y int) color.NRGBA {
	t.Helper()

	decoded, err := png.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode png output: %v", err)
	}

	return color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
}

func TestSupportedFormatsAreSortedAndIncludeNewFormats(t *testing.T) {
	formats := NewImageConverter().SupportedFormats()

	wanted := map[string]bool{"jpeg": false, "png": false, "webp": false, "gif": false, "tiff": false, "avif": false}
	for _, format := range formats {
		if _, ok := wanted[format]; ok {
			wanted[format] = true
		}
	}
	for format, found := range wanted {
		if !found {
			t.Fatalf("expected %q in supported formats, got %v", format, formats)
		}
	}

	for index := 1; index < len(formats); index++ {
		if formats[index-1] >= formats[index] {
			t.Fatalf("expected sorted formats, got %v", formats)
		}
	}
}

func TestValidateOutputFormatAcceptsTIFFAndAVIF(t *testing.T) {
	converter := NewImageConverter()

	for _, format := range []string{"tiff", "TIF", "avif", "AVIF", "jpg"} {
		if err := converter.validateOutputFormat(format); err != nil {
			t.Fatalf("expected %q to validate, got %v", format, err)
		}
	}

	if err := converter.validateOutputFormat("bmp"); err == nil {
		t.Fatal("expected bmp to stay unsupported as an output format")
	}
}

func TestConvertLongestEdgeConstrainsLandscapeImages(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "wide.png")
	writePlanPNG(t, input, 200, 100)

	cases := []struct {
		name   string
		option any
	}{
		{name: "json number", option: float64(100)},
		{name: "int", option: 100},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			output, err := NewImageConverter().Convert(readFixtureBytes(t, input), map[string]any{
				"format":      "png",
				"longestEdge": testCase.option,
			})
			if err != nil {
				t.Fatalf("convert with longestEdge: %v", err)
			}

			width, height := convertedSize(t, output)
			if width != 100 || height != 50 {
				t.Fatalf("expected 100x50 output, got %dx%d", width, height)
			}
		})
	}
}

func TestConvertLongestEdgeConstrainsPortraitImages(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "tall.png")
	writePlanPNG(t, input, 100, 200)

	output, err := NewImageConverter().Convert(readFixtureBytes(t, input), map[string]any{
		"format":      "png",
		"longestEdge": float64(100),
	})
	if err != nil {
		t.Fatalf("convert with longestEdge: %v", err)
	}

	width, height := convertedSize(t, output)
	if width != 50 || height != 100 {
		t.Fatalf("expected 50x100 output, got %dx%d", width, height)
	}
}

func TestConvertLongestEdgeNeverUpscales(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "small.png")
	writePlanPNG(t, input, 80, 40)

	output, err := NewImageConverter().Convert(readFixtureBytes(t, input), map[string]any{
		"format":      "png",
		"longestEdge": float64(400),
	})
	if err != nil {
		t.Fatalf("convert with longestEdge: %v", err)
	}

	width, height := convertedSize(t, output)
	if width != 80 || height != 40 {
		t.Fatalf("expected the small image to stay 80x40, got %dx%d", width, height)
	}
}

func TestConvertLongestEdgeWinsOverWidthAndHeight(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 200, 100)

	output, err := NewImageConverter().Convert(readFixtureBytes(t, input), map[string]any{
		"format":      "png",
		"width":       float64(20),
		"height":      float64(20),
		"longestEdge": float64(100),
	})
	if err != nil {
		t.Fatalf("convert with longestEdge: %v", err)
	}

	width, height := convertedSize(t, output)
	if width != 100 || height != 50 {
		t.Fatalf("expected longestEdge to win with a 100x50 output, got %dx%d", width, height)
	}
}

func TestConvertLongestEdgeAfterRotation(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 200, 100)

	output, err := NewImageConverter().Convert(readFixtureBytes(t, input), map[string]any{
		"format":      "png",
		"rotate":      float64(90),
		"longestEdge": float64(100),
	})
	if err != nil {
		t.Fatalf("convert with rotate and longestEdge: %v", err)
	}

	// The quarter turn makes the image portrait, so the height is constrained.
	width, height := convertedSize(t, output)
	if width != 50 || height != 100 {
		t.Fatalf("expected a 50x100 output, got %dx%d", width, height)
	}
}

func TestConvertLongestEdgeFollowsEXIFOrientation(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 200, 100)

	stored, err := bimg.NewImage(readFixtureBytes(t, input)).Convert(bimg.JPEG)
	if err != nil {
		t.Fatalf("encode base jpeg: %v", err)
	}

	// Orientation 6 rotates the 200x100 stored image to a 100x200 portrait.
	withOrientation := jpegWithEXIFOrientation(t, stored, 6)
	metadata, metaErr := bimg.Metadata(withOrientation)
	if metaErr != nil || metadata.Orientation != 6 {
		t.Skipf("libvips cannot read the injected EXIF orientation (orientation=%d err=%v)", metadata.Orientation, metaErr)
	}

	output, err := NewImageConverter().Convert(withOrientation, map[string]any{
		"format":      "jpeg",
		"longestEdge": float64(100),
	})
	if err != nil {
		t.Fatalf("convert with longestEdge: %v", err)
	}

	width, height := convertedSize(t, output)
	if width != 50 || height != 100 {
		t.Fatalf("expected the EXIF orientation to constrain the height, got %dx%d", width, height)
	}
}

func TestConvertRotateSwapsDimensions(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 200, 100)

	output, err := NewImageConverter().Convert(readFixtureBytes(t, input), map[string]any{
		"format": "png",
		"rotate": float64(90),
	})
	if err != nil {
		t.Fatalf("convert with rotate: %v", err)
	}

	width, height := convertedSize(t, output)
	if width != 100 || height != 200 {
		t.Fatalf("expected a 90 degree turn to produce 100x200, got %dx%d", width, height)
	}
}

func TestConvertRejectsUnsupportedRotateValues(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 20)

	for _, angle := range []any{float64(45), float64(-90), float64(360)} {
		_, err := NewImageConverter().Convert(readFixtureBytes(t, input), map[string]any{
			"format": "png",
			"rotate": angle,
		})
		if err == nil {
			t.Fatalf("expected rotate %v to be rejected", angle)
		}
		if !strings.Contains(err.Error(), "rotate") {
			t.Fatalf("expected a rotate error, got %v", err)
		}
	}
}

func TestConvertFlipsMirrorTheImage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "halves.png")
	writeHalfPNG(t, input, 8, 4)
	inputBytes := readFixtureBytes(t, input)

	converter := NewImageConverter()

	flopped, err := converter.Convert(inputBytes, map[string]any{"format": "png", "flipH": true})
	if err != nil {
		t.Fatalf("convert with flipH: %v", err)
	}
	if pixel := pixelAt(t, flopped, 0, 0); pixel.B < 200 || pixel.R > 60 {
		t.Fatalf("expected flipH to move the blue half to the left, got %+v", pixel)
	}

	flipped, err := converter.Convert(inputBytes, map[string]any{"format": "png", "flipV": true})
	if err != nil {
		t.Fatalf("convert with flipV: %v", err)
	}
	if pixel := pixelAt(t, flipped, 0, 0); pixel.R < 200 || pixel.B > 60 {
		t.Fatalf("expected flipV to keep the red half on the left, got %+v", pixel)
	}

	untouched, err := converter.Convert(inputBytes, map[string]any{"format": "png"})
	if err != nil {
		t.Fatalf("plain convert: %v", err)
	}
	if pixel := pixelAt(t, untouched, 0, 0); pixel.R < 200 {
		t.Fatalf("expected the untouched copy to start with the red half, got %+v", pixel)
	}
}

func TestConvertStripMetadataIsAccepted(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 20)

	output, err := NewImageConverter().Convert(readFixtureBytes(t, input), map[string]any{
		"format":        "jpeg",
		"stripMetadata": true,
	})
	if err != nil {
		t.Fatalf("convert with stripMetadata: %v", err)
	}

	if len(output) < 2 || output[0] != 0xFF || output[1] != 0xD8 {
		t.Fatal("expected a JPEG output")
	}
}

func TestConvertStripMetadataRemovesEXIFOrientation(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 20)

	asJPEG, err := bimg.NewImage(readFixtureBytes(t, input)).Convert(bimg.JPEG)
	if err != nil {
		t.Fatalf("encode base jpeg: %v", err)
	}

	withOrientation := jpegWithEXIFOrientation(t, asJPEG, 6)
	metadata, metaErr := bimg.Metadata(withOrientation)
	if metaErr != nil || metadata.Orientation != 6 {
		t.Skipf("libvips cannot read the injected EXIF orientation (orientation=%d err=%v)", metadata.Orientation, metaErr)
	}

	converter := NewImageConverter()

	kept, err := converter.Convert(withOrientation, map[string]any{"format": "jpeg"})
	if err != nil {
		t.Fatalf("convert keeping metadata: %v", err)
	}
	keptMetadata, keptErr := bimg.Metadata(kept)
	if keptErr != nil || keptMetadata.Orientation == 0 {
		t.Skipf("libvips drops EXIF when saving; stripMetadata cannot be observed here (orientation=%d err=%v)", keptMetadata.Orientation, keptErr)
	}

	stripped, err := converter.Convert(withOrientation, map[string]any{"format": "jpeg", "stripMetadata": true})
	if err != nil {
		t.Fatalf("convert stripping metadata: %v", err)
	}

	strippedMetadata, strippedErr := bimg.Metadata(stripped)
	if strippedErr != nil {
		t.Fatalf("read stripped metadata: %v", strippedErr)
	}
	if strippedMetadata.Orientation != 0 {
		t.Fatalf("expected stripMetadata to remove the EXIF orientation, got %d", strippedMetadata.Orientation)
	}
}

// jpegWithEXIFOrientation prepends a minimal EXIF APP1 segment carrying one
// IFD0 Orientation tag to an existing JPEG.
func jpegWithEXIFOrientation(t *testing.T, jpeg []byte, orientation byte) []byte {
	t.Helper()

	if len(jpeg) < 2 || jpeg[0] != 0xFF || jpeg[1] != 0xD8 {
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

	out := make([]byte, 0, len(jpeg)+4+len(payload))
	out = append(out, 0xFF, 0xD8, 0xFF, 0xE1, byte(segmentLength>>8), byte(segmentLength))
	out = append(out, payload...)
	out = append(out, jpeg[2:]...)

	return out
}

func TestConvertExplicitTIFFAndAVIFFormats(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 40, 20)
	inputBytes := readFixtureBytes(t, input)

	cases := []struct {
		format    string
		imageType bimg.ImageType
	}{
		{format: "tiff", imageType: bimg.TIFF},
		{format: "avif", imageType: bimg.AVIF},
	}

	for _, testCase := range cases {
		t.Run(testCase.format, func(t *testing.T) {
			output, err := NewImageConverter().Convert(inputBytes, map[string]any{"format": testCase.format})
			if err != nil {
				if !bimg.IsTypeSupportedSave(testCase.imageType) {
					t.Skipf("this libvips build cannot save %s: %v", testCase.format, err)
				}
				t.Fatalf("convert to %s: %v", testCase.format, err)
			}

			width, height := convertedSize(t, output)
			if width != 40 || height != 20 {
				t.Fatalf("expected 40x20 output, got %dx%d", width, height)
			}
		})
	}
}
