package image

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
	"path/filepath"
	"strings"
	"testing"
)

func encodeTransparentPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: 210, G: 30, B: 40, A: 0})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode transparent png: %v", err)
	}

	return buffer.Bytes()
}

func decodeJPEGPixel(t *testing.T, encoded []byte, x, y int) color.NRGBA {
	t.Helper()

	decoded, err := jpeg.Decode(bytes.NewReader(encoded))
	if err != nil {
		t.Fatalf("decode jpeg output: %v", err)
	}

	return color.NRGBAModel.Convert(decoded.At(x, y)).(color.NRGBA)
}

func TestConvertBackgroundFlattensTransparency(t *testing.T) {
	input := encodeTransparentPNG(t, 16, 16)
	converter := NewImageConverter()

	withWhite, err := converter.Convert(input, map[string]any{
		"format":     "jpeg",
		"quality":    100,
		"background": "#ffffff",
	})
	if err != nil {
		t.Fatalf("convert with a white background: %v", err)
	}

	pixel := decodeJPEGPixel(t, withWhite, 8, 8)
	if pixel.R < 240 || pixel.G < 240 || pixel.B < 240 {
		t.Fatalf("expected a near-white pixel, got %+v", pixel)
	}

	withoutBackground, err := converter.Convert(input, map[string]any{
		"format":  "jpeg",
		"quality": 100,
	})
	if err != nil {
		t.Fatalf("convert without a background: %v", err)
	}

	pixel = decodeJPEGPixel(t, withoutBackground, 8, 8)
	if pixel.R > 200 && pixel.G > 200 && pixel.B > 200 {
		t.Fatalf("expected the default flatten to differ from white, got %+v", pixel)
	}
}

func TestConvertBackgroundAcceptsHexVariants(t *testing.T) {
	input := encodeTransparentPNG(t, 8, 8)
	converter := NewImageConverter()

	for _, background := range []string{"ffffff", "FFFFFF", "FfFfFf", " #FfFfFf "} {
		t.Run(background, func(t *testing.T) {
			output, err := converter.Convert(input, map[string]any{
				"format":     "jpeg",
				"quality":    100,
				"background": background,
			})
			if err != nil {
				t.Fatalf("convert with background %q: %v", background, err)
			}

			pixel := decodeJPEGPixel(t, output, 4, 4)
			if pixel.R < 240 || pixel.G < 240 || pixel.B < 240 {
				t.Fatalf("expected a near-white pixel for %q, got %+v", background, pixel)
			}
		})
	}
}

func TestConvertRejectsInvalidBackground(t *testing.T) {
	input := encodeTransparentPNG(t, 8, 8)
	converter := NewImageConverter()

	for _, background := range []any{"white", "#fff", "#fffff", "#gggggg", "#fffffff", 123} {
		_, err := converter.Convert(input, map[string]any{
			"format":     "jpeg",
			"background": background,
		})
		if err == nil {
			t.Fatalf("expected background %v to be rejected", background)
		}
		if !strings.Contains(err.Error(), "background must be a hex color like #ffffff") {
			t.Fatalf("expected a clear background error for %v, got %v", background, err)
		}
	}
}

func TestPlanNormalizesBackgroundOption(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanFixture(t, input)

	adapter := NewImageToolAdapter(nil)

	plan, planErr := adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{
		"format":     "jpeg",
		"background": "#FFFFFF",
	}))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed, got %v", planErr)
	}
	if got := plan.EffectiveOptions["background"]; got != "#ffffff" {
		t.Fatalf("expected the normalized background #ffffff, got %v", got)
	}

	plan, planErr = adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{
		"format":     "jpeg",
		"background": "FfFfFf",
	}))
	if planErr != nil {
		t.Fatalf("expected the plan to succeed without a leading hash, got %v", planErr)
	}
	if got := plan.EffectiveOptions["background"]; got != "#ffffff" {
		t.Fatalf("expected the normalized background #ffffff, got %v", got)
	}

	_, planErr = adapter.Plan(context.Background(), planImageRequest("single", []string{input}, map[string]any{
		"format":     "jpeg",
		"background": "white",
	}))
	if planErr == nil {
		t.Fatal("expected an invalid background to fail the plan")
	}
	if planErr.DetailCode != "IMAGE_BACKGROUND_INVALID" {
		t.Fatalf("expected IMAGE_BACKGROUND_INVALID, got %q", planErr.DetailCode)
	}
	if !strings.Contains(planErr.Message, "background must be a hex color like #ffffff") {
		t.Fatalf("expected a clear background error, got %q", planErr.Message)
	}
}
