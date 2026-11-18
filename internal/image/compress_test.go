package image

import (
	"bytes"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h2non/bimg"
)

// compressNoiseNRGBA renders deterministic incompressible pixels so every
// encoder decision is driven by the target and not by smooth-image luck.
func compressNoiseNRGBA(width, height int) *image.NRGBA {
	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	state := uint32(0x9e3779b9)
	next := func() uint8 {
		state ^= state << 13
		state ^= state >> 17
		state ^= state << 5
		return uint8(state)
	}

	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: next(), G: next(), B: next(), A: 255})
		}
	}

	return img
}

func encodeCompressNoiseJPEG(t *testing.T, width, height, quality int) []byte {
	t.Helper()

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, compressNoiseNRGBA(width, height), &jpeg.Options{Quality: quality}); err != nil {
		t.Fatalf("encode noise jpeg: %v", err)
	}

	return buffer.Bytes()
}

func writeCompressNoiseJPEG(t *testing.T, path string, width, height, quality int) {
	t.Helper()

	if err := writeCompressBytes(path, encodeCompressNoiseJPEG(t, width, height, quality)); err != nil {
		t.Fatalf("write noise jpeg %s: %v", path, err)
	}
}

func writeCompressNoisePNG(t *testing.T, path string, width, height int) {
	t.Helper()

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, compressNoiseNRGBA(width, height)); err != nil {
		t.Fatalf("encode noise png: %v", err)
	}
	if err := writeCompressBytes(path, buffer.Bytes()); err != nil {
		t.Fatalf("write noise png %s: %v", path, err)
	}
}

func compressDecodedSize(t *testing.T, data []byte) (int, int) {
	t.Helper()

	config, _, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("decode output config: %v", err)
	}

	return config.Width, config.Height
}

// compressPaletteSupported mirrors the libvips version gate in bimg's PNG save
// bridge: older builds ignore the palette, quality and speed options.
func compressPaletteSupported() bool {
	return bimg.VipsMajorVersion >= 8 && bimg.VipsMinorVersion >= 7
}

func writeCompressBytes(path string, data []byte) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}

	return os.WriteFile(path, data, 0o644)
}

func TestCompressQualitySearchReachesTarget(t *testing.T) {
	input := encodeCompressNoiseJPEG(t, 640, 480, 92)
	target := int64(len(input) / 2)

	output, outcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: target,
		Format:          "jpeg",
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("compress noise jpeg: %v", err)
	}

	if !outcome.TargetMet {
		t.Fatalf("expected the target to be reached, got %+v", outcome)
	}
	if outcome.AlreadyUnderTarget {
		t.Fatal("expected a real encode, not the original bytes")
	}
	if outcome.DownscaleRounds != 0 {
		t.Fatalf("expected no downscale rounds, got %d", outcome.DownscaleRounds)
	}
	if outcome.SizeBytes != int64(len(output)) {
		t.Fatalf("outcome size %d does not match output size %d", outcome.SizeBytes, len(output))
	}
	if outcome.SizeBytes > target {
		t.Fatalf("expected output <= %d bytes, got %d", target, outcome.SizeBytes)
	}
	if outcome.Quality < compressJPEGMinQuality || outcome.Quality > 92 {
		t.Fatalf("expected quality within [%d, 92], got %d", compressJPEGMinQuality, outcome.Quality)
	}
	if len(output) == len(input) {
		t.Fatal("expected a re-encoded image")
	}

	width, height := compressDecodedSize(t, output)
	if width != 640 || height != 480 {
		t.Fatalf("expected 640x480 output, got %dx%d", width, height)
	}

	// The chosen quality must be the highest one that fits: one step higher has
	// to exceed the target.
	if outcome.Quality < 92 {
		larger, encodeErr := bimg.NewImage(input).Process(bimg.Options{
			Type:          bimg.JPEG,
			Quality:       outcome.Quality + 1,
			StripMetadata: true,
			Interlace:     true,
		})
		if encodeErr != nil {
			t.Fatalf("encode control quality: %v", encodeErr)
		}
		if int64(len(larger)) <= target {
			t.Fatalf("expected quality %d to exceed the target, got %d bytes", outcome.Quality+1, len(larger))
		}
	}
}

func TestCompressDownscalesForTinyTarget(t *testing.T) {
	input := encodeCompressNoiseJPEG(t, 900, 600, 90)

	// Learn the smallest no-downscale size first so the target is reachable by
	// shrinking while staying far below it.
	baseline, baselineOutcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: 1,
		Format:          "jpeg",
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("baseline compress: %v", err)
	}
	if baselineOutcome.TargetMet {
		t.Fatalf("expected the baseline to miss a 1-byte target, got %+v", baselineOutcome)
	}
	if baselineOutcome.Quality != compressJPEGMinQuality {
		t.Fatalf("expected the baseline to end at quality %d, got %d", compressJPEGMinQuality, baselineOutcome.Quality)
	}

	target := int64(len(baseline) * 60 / 100)
	output, outcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: target,
		Format:          "jpeg",
		AllowDownscale:  true,
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("compress with downscale: %v", err)
	}

	if !outcome.TargetMet {
		t.Fatalf("expected the downscaled target to be reached, got %+v", outcome)
	}
	if outcome.DownscaleRounds < 1 || outcome.DownscaleRounds > compressMaxDownscale {
		t.Fatalf("expected 1-%d downscale rounds, got %d", compressMaxDownscale, outcome.DownscaleRounds)
	}
	if outcome.Width >= 900 || outcome.Height >= 600 {
		t.Fatalf("expected smaller dimensions, got %dx%d", outcome.Width, outcome.Height)
	}
	if outcome.Width*outcome.Height >= 900*600 {
		t.Fatalf("expected fewer pixels than the source, got %dx%d", outcome.Width, outcome.Height)
	}
	if outcome.SizeBytes > target {
		t.Fatalf("expected output <= %d bytes, got %d", target, outcome.SizeBytes)
	}

	width, height := compressDecodedSize(t, output)
	if width != outcome.Width || height != outcome.Height {
		t.Fatalf("expected the outcome to report the encoded size %dx%d, got %dx%d", width, height, outcome.Width, outcome.Height)
	}
}

func TestCompressWithoutDownscaleReportsBestEffort(t *testing.T) {
	input := encodeCompressNoiseJPEG(t, 900, 600, 90)

	output, outcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: 1024,
		Format:          "jpeg",
		AllowDownscale:  false,
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("compress without downscale: %v", err)
	}

	if outcome.TargetMet {
		t.Fatalf("expected an unreachable target, got %+v", outcome)
	}
	if outcome.DownscaleRounds != 0 {
		t.Fatalf("expected no downscale rounds, got %d", outcome.DownscaleRounds)
	}
	if outcome.Width != 900 || outcome.Height != 600 {
		t.Fatalf("expected the source dimensions, got %dx%d", outcome.Width, outcome.Height)
	}
	if outcome.Quality != compressJPEGMinQuality {
		t.Fatalf("expected the lowest quality as best effort, got %d", outcome.Quality)
	}
	if outcome.SizeBytes <= 1024 {
		t.Fatalf("expected a best-effort image larger than the target, got %d", outcome.SizeBytes)
	}
	if len(output) == 0 {
		t.Fatal("expected the best candidate to be returned")
	}

	width, height := compressDecodedSize(t, output)
	if width != 900 || height != 600 {
		t.Fatalf("expected 900x600 output, got %dx%d", width, height)
	}
}

func TestCompressNeverUpscalesSmallImage(t *testing.T) {
	input := encodeCompressNoiseJPEG(t, 200, 150, 90)

	output, outcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: 1,
		Format:          "jpeg",
		AllowDownscale:  true,
		MaxDimension:    4000,
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("compress small image: %v", err)
	}

	if outcome.Width != 200 || outcome.Height != 150 {
		t.Fatalf("expected the source dimensions, got %dx%d", outcome.Width, outcome.Height)
	}
	if outcome.DownscaleRounds != 0 {
		t.Fatalf("expected no downscale rounds, got %d", outcome.DownscaleRounds)
	}

	width, height := compressDecodedSize(t, output)
	if width != 200 || height != 150 {
		t.Fatalf("expected 200x150 output, got %dx%d", width, height)
	}
}

func TestCompressMaxDimensionCapsLongestEdge(t *testing.T) {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, compressNoiseNRGBA(500, 400)); err != nil {
		t.Fatalf("encode noise png: %v", err)
	}
	input := buffer.Bytes()

	output, outcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: 1024 * 1024,
		Format:          "png",
		MaxDimension:    200,
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("compress with max dimension: %v", err)
	}

	if outcome.Width != 200 || outcome.Height != 160 {
		t.Fatalf("expected a 200x160 cap, got %dx%d", outcome.Width, outcome.Height)
	}
	if outcome.AlreadyUnderTarget {
		t.Fatal("expected a re-encode whenever the cap changes the image")
	}
	if bytes.Equal(output, input) {
		t.Fatal("expected the capped image to differ from the source")
	}

	width, height := compressDecodedSize(t, output)
	if width != 200 || height != 160 {
		t.Fatalf("expected 200x160 output, got %dx%d", width, height)
	}
}

func TestCompressPNGPaletteLadderMeetsTarget(t *testing.T) {
	if !compressPaletteSupported() {
		t.Skip("libvips build ignores png palette options")
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, compressNoiseNRGBA(500, 400)); err != nil {
		t.Fatalf("encode noise png: %v", err)
	}
	input := buffer.Bytes()

	output, outcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: 350 * 1024,
		Format:          "png",
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("compress png: %v", err)
	}

	if !outcome.TargetMet {
		t.Fatalf("expected the palette path to reach the target, got %+v", outcome)
	}
	if outcome.Quality != 80 {
		t.Fatalf("expected the 80-quality palette rung, got %d", outcome.Quality)
	}
	if outcome.DownscaleRounds != 0 {
		t.Fatalf("expected no downscale rounds, got %d", outcome.DownscaleRounds)
	}
	if outcome.Width != 500 || outcome.Height != 400 {
		t.Fatalf("expected the source dimensions, got %dx%d", outcome.Width, outcome.Height)
	}
	if len(output) >= len(input) {
		t.Fatalf("expected a smaller png, got %d bytes from %d", len(output), len(input))
	}

	decoded, format, decodeErr := image.Decode(bytes.NewReader(output))
	if decodeErr != nil {
		t.Fatalf("decode palette output: %v", decodeErr)
	}
	if format != "png" {
		t.Fatalf("expected png output, got %s", format)
	}
	if _, ok := decoded.(*image.Paletted); !ok {
		t.Fatalf("expected a paletted png, got %T", decoded)
	}
}

func TestCompressPNGUnreachableTargetReportsBestEffort(t *testing.T) {
	if !compressPaletteSupported() {
		t.Skip("libvips build ignores png palette options")
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, compressNoiseNRGBA(500, 400)); err != nil {
		t.Fatalf("encode noise png: %v", err)
	}

	output, outcome, err := CompressToTarget(buffer.Bytes(), CompressOptions{
		TargetSizeBytes: 1024,
		Format:          "png",
		AllowDownscale:  false,
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("compress unreachable png: %v", err)
	}

	if outcome.TargetMet {
		t.Fatalf("expected an unreachable png target, got %+v", outcome)
	}
	if outcome.Quality != 80 {
		t.Fatalf("expected the smallest palette rung, got %d", outcome.Quality)
	}
	if outcome.Width != 500 || outcome.Height != 400 {
		t.Fatalf("expected the source dimensions, got %dx%d", outcome.Width, outcome.Height)
	}
	if len(output) != int(outcome.SizeBytes) {
		t.Fatalf("expected the best-effort bytes, got %d vs %d", len(output), outcome.SizeBytes)
	}
}

func TestCompressAlreadyUnderTargetReturnsOriginal(t *testing.T) {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, compressNoiseNRGBA(40, 30)); err != nil {
		t.Fatalf("encode small png: %v", err)
	}
	input := buffer.Bytes()

	output, outcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: 1024 * 1024,
		Format:          "png",
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("compress already small png: %v", err)
	}

	if !bytes.Equal(output, input) {
		t.Fatal("expected the original bytes untouched")
	}
	if !outcome.AlreadyUnderTarget {
		t.Fatalf("expected AlreadyUnderTarget, got %+v", outcome)
	}
	if !outcome.TargetMet {
		t.Fatal("expected an already satisfied target to be met")
	}
	if outcome.Quality != 0 {
		t.Fatalf("expected no encoder quality for unchanged bytes, got %d", outcome.Quality)
	}
	if outcome.SizeBytes != int64(len(input)) {
		t.Fatalf("expected size %d, got %d", len(input), outcome.SizeBytes)
	}
	if outcome.Width != 40 || outcome.Height != 30 {
		t.Fatalf("expected 40x30, got %dx%d", outcome.Width, outcome.Height)
	}
}

func TestCompressFormatChangeReencodesUnderTargetImage(t *testing.T) {
	var buffer bytes.Buffer
	if err := png.Encode(&buffer, compressNoiseNRGBA(40, 30)); err != nil {
		t.Fatalf("encode small png: %v", err)
	}
	input := buffer.Bytes()

	output, outcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: 1024 * 1024,
		Format:          "jpeg",
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("compress png to jpeg: %v", err)
	}

	if bytes.Equal(output, input) {
		t.Fatal("expected a re-encode when the format changes")
	}
	if outcome.AlreadyUnderTarget {
		t.Fatal("expected AlreadyUnderTarget to stay false for a format change")
	}
	if !outcome.TargetMet {
		t.Fatalf("expected the target to be met, got %+v", outcome)
	}
	if outcome.Quality <= 0 {
		t.Fatalf("expected a real encoder quality, got %d", outcome.Quality)
	}

	width, height := compressDecodedSize(t, output)
	if width != 40 || height != 30 {
		t.Fatalf("expected 40x30 output, got %dx%d", width, height)
	}
}

func TestCompressBackgroundFlattensAlphaForJPEG(t *testing.T) {
	input := encodeTransparentPNG(t, 16, 16)
	background, err := parseBackgroundColor("#ffffff")
	if err != nil {
		t.Fatalf("parse background: %v", err)
	}

	output, outcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: 1024 * 1024,
		Format:          "jpeg",
		StripMetadata:   true,
		Background:      &background,
	})
	if err != nil {
		t.Fatalf("compress transparent png: %v", err)
	}
	if !outcome.TargetMet {
		t.Fatalf("expected the target to be met, got %+v", outcome)
	}

	pixel := decodeJPEGPixel(t, output, 8, 8)
	if pixel.R < 240 || pixel.G < 240 || pixel.B < 240 {
		t.Fatalf("expected the alpha to flatten onto white, got %+v", pixel)
	}
}

func TestCompressWebPQualitySearch(t *testing.T) {
	if !bimg.IsTypeSupportedSave(bimg.WEBP) {
		t.Skip("libvips build cannot save webp")
	}

	input := encodeCompressNoiseJPEG(t, 400, 300, 90)
	target := int64(60 * 1024)

	output, outcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: target,
		Format:          "webp",
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("compress webp: %v", err)
	}

	if !outcome.TargetMet {
		t.Fatalf("expected the webp target to be reached, got %+v", outcome)
	}
	if outcome.Quality < compressWebPMinQuality || outcome.Quality > compressMaxQuality {
		t.Fatalf("expected webp quality within [%d, %d], got %d", compressWebPMinQuality, compressMaxQuality, outcome.Quality)
	}
	if outcome.SizeBytes > target {
		t.Fatalf("expected <= %d bytes, got %d", target, outcome.SizeBytes)
	}

	size, sizeErr := bimg.NewImage(output).Size()
	if sizeErr != nil {
		t.Fatalf("read webp size: %v", sizeErr)
	}
	if size.Width != 400 || size.Height != 300 {
		t.Fatalf("expected 400x300 output, got %dx%d", size.Width, size.Height)
	}
}

func TestCompressRejectsInvalidInput(t *testing.T) {
	cases := []struct {
		name    string
		input   []byte
		options CompressOptions
		want    string
	}{
		{
			name:    "undecodable bytes",
			input:   []byte("this is not an image"),
			options: CompressOptions{TargetSizeBytes: 1024, Format: "jpeg"},
			want:    "decode image",
		},
		{
			name:    "unsupported output format",
			input:   encodeCompressNoiseJPEG(t, 40, 30, 90),
			options: CompressOptions{TargetSizeBytes: 1024, Format: "gif"},
			want:    "unsupported image format",
		},
		{
			name:    "missing output format",
			input:   encodeCompressNoiseJPEG(t, 40, 30, 90),
			options: CompressOptions{TargetSizeBytes: 1024},
			want:    "output format is required",
		},
		{
			name:    "non-positive target",
			input:   encodeCompressNoiseJPEG(t, 40, 30, 90),
			options: CompressOptions{TargetSizeBytes: 0, Format: "jpeg"},
			want:    "target size must be greater than zero",
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			_, _, err := CompressToTarget(testCase.input, testCase.options)
			if err == nil {
				t.Fatalf("expected an error containing %q", testCase.want)
			}
			if !strings.Contains(err.Error(), testCase.want) {
				t.Fatalf("expected error containing %q, got %v", testCase.want, err)
			}
		})
	}
}

func TestCompressAcceptsJPGAlias(t *testing.T) {
	input := encodeCompressNoiseJPEG(t, 120, 90, 90)

	output, outcome, err := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: int64(len(input) / 2),
		Format:          "jpg",
		StripMetadata:   true,
	})
	if err != nil {
		t.Fatalf("compress with jpg alias: %v", err)
	}
	if !outcome.TargetMet {
		t.Fatalf("expected the target to be met, got %+v", outcome)
	}
	if len(output) == 0 {
		t.Fatal("expected encoded bytes")
	}

	_, format, decodeErr := image.DecodeConfig(bytes.NewReader(output))
	if decodeErr != nil {
		t.Fatalf("decode alias output: %v", decodeErr)
	}
	if format != "jpeg" {
		t.Fatalf("expected jpeg output, got %s", format)
	}
}
