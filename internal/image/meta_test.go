package image

import (
	"bytes"
	"image"
	"image/color"
	"image/gif"
	"image/jpeg"
	"image/png"
	"os"
	"path/filepath"
	"testing"
)

func writeMetaPNG(t *testing.T, path string, width, height int, alpha bool) {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			a := uint8(255)
			if alpha {
				a = 128
			}
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 96, A: a})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}
	writeMetaBytes(t, path, buffer.Bytes())
}

func writeMetaJPEG(t *testing.T, path string, width, height, orientation int) {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 96, A: 255})
		}
	}

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, &jpeg.Options{Quality: 85}); err != nil {
		t.Fatalf("encode jpeg fixture: %v", err)
	}
	writeMetaBytes(t, path, withMetaEXIFOrientation(t, buffer.Bytes(), orientation))
}

func writeMetaGIF(t *testing.T, path string, frames, width, height int) {
	t.Helper()

	palette := color.Palette{
		color.NRGBA{R: 30, G: 160, B: 220, A: 255},
		color.NRGBA{R: 240, G: 30, B: 20, A: 255},
	}
	images := make([]*image.Paletted, frames)
	delays := make([]int, frames)
	for frame := 0; frame < frames; frame++ {
		paletted := image.NewPaletted(image.Rect(0, 0, width, height), palette)
		for x := 0; x < width; x++ {
			paletted.SetColorIndex(x, frame%height, 1)
		}
		images[frame] = paletted
		delays[frame] = 6
	}

	var buffer bytes.Buffer
	if err := gif.EncodeAll(&buffer, &gif.GIF{Image: images, Delay: delays}); err != nil {
		t.Fatalf("encode gif fixture: %v", err)
	}
	writeMetaBytes(t, path, buffer.Bytes())
}

func writeMetaBytes(t *testing.T, path string, data []byte) {
	t.Helper()

	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("create fixture dir: %v", err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatalf("write fixture %s: %v", path, err)
	}
}

// withMetaEXIFOrientation injects a minimal EXIF APP1 segment right after the
// JPEG SOI marker. Orientation 0 leaves the stream untouched.
func withMetaEXIFOrientation(t *testing.T, data []byte, orientation int) []byte {
	t.Helper()

	if orientation <= 0 {
		return data
	}
	if len(data) < 2 || data[0] != 0xFF || data[1] != 0xD8 {
		t.Fatal("jpeg fixture does not start with SOI")
	}

	tiff := []byte{
		'I', 'I', 0x2A, 0x00, 0x08, 0x00, 0x00, 0x00, // little-endian TIFF header
		0x01, 0x00, // one IFD entry
		0x12, 0x01, // Orientation tag
		0x03, 0x00, // SHORT type
		0x01, 0x00, 0x00, 0x00, // one value
		byte(orientation), byte(orientation >> 8), 0x00, 0x00, // value plus padding
		0x00, 0x00, 0x00, 0x00, // no next IFD
	}
	payload := append([]byte("Exif\x00\x00"), tiff...)
	length := len(payload) + 2
	segment := []byte{0xFF, 0xE1, byte(length >> 8), byte(length)}
	segment = append(segment, payload...)

	injected := make([]byte, 0, len(data)+len(segment))
	injected = append(injected, data[:2]...)
	injected = append(injected, segment...)
	injected = append(injected, data[2:]...)

	return injected
}

func TestReadPlanImageMeta(t *testing.T) {
	dir := t.TempDir()

	pngPath := filepath.Join(dir, "photo.png")
	jpegPath := filepath.Join(dir, "photo.jpg")
	gifPath := filepath.Join(dir, "static.gif")
	animatedPath := filepath.Join(dir, "animated.gif")
	rotatedPath := filepath.Join(dir, "rotated.jpg")
	flippedPath := filepath.Join(dir, "flipped.jpg")
	writeMetaPNG(t, pngPath, 40, 20, true)
	writeMetaJPEG(t, jpegPath, 30, 15, 0)
	writeMetaGIF(t, gifPath, 1, 24, 18)
	writeMetaGIF(t, animatedPath, 3, 24, 18)
	writeMetaJPEG(t, rotatedPath, 30, 15, 6)
	writeMetaJPEG(t, flippedPath, 30, 15, 3)

	cases := []struct {
		name string
		path string
		want planImageMeta
	}{
		{
			name: "png keeps format alpha and size",
			path: pngPath,
			want: planImageMeta{Width: 40, Height: 20, Format: "png", Alpha: true, Frames: 1},
		},
		{
			name: "jpeg has no alpha",
			path: jpegPath,
			want: planImageMeta{Width: 30, Height: 15, Format: "jpeg", Alpha: false, Frames: 1},
		},
		{
			name: "static gif counts one frame",
			path: gifPath,
			want: planImageMeta{Width: 24, Height: 18, Format: "gif", Frames: 1},
		},
		{
			name: "animated gif counts every frame",
			path: animatedPath,
			want: planImageMeta{Width: 24, Height: 18, Format: "gif", Frames: 3},
		},
		{
			name: "exif orientation 6 swaps the axes",
			path: rotatedPath,
			want: planImageMeta{Width: 15, Height: 30, Format: "jpeg", Frames: 1},
		},
		{
			name: "exif orientation 3 keeps the stored axes",
			path: flippedPath,
			want: planImageMeta{Width: 30, Height: 15, Format: "jpeg", Frames: 1},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			got, err := readPlanImageMeta(testCase.path)
			if err != nil {
				t.Fatalf("read plan image meta: %v", err)
			}
			if got != testCase.want {
				t.Fatalf("expected %+v, got %+v", testCase.want, got)
			}
		})
	}
}

func TestReadPlanImageMetaReportsReadFailure(t *testing.T) {
	_, err := readPlanImageMeta(filepath.Join(t.TempDir(), "missing.png"))
	if err == nil {
		t.Fatal("expected a read failure")
	}
}

func TestReadPlanImageDimensionsFollowsOrientation(t *testing.T) {
	dir := t.TempDir()
	rotated := filepath.Join(dir, "rotated.jpg")
	writeMetaJPEG(t, rotated, 30, 15, 8)

	width, height, err := readPlanImageDimensions(rotated)
	if err != nil {
		t.Fatalf("read plan dimensions: %v", err)
	}
	if width != 15 || height != 30 {
		t.Fatalf("expected 15x30 after orientation, got %dx%d", width, height)
	}
}
