package preview

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"path/filepath"
	"strings"
	"testing"

	"github.com/h2non/bimg"
)

type stubExecRunner struct {
	name   string
	args   []string
	stdin  []byte
	stdout []byte
	stderr []byte
	err    error
}

func (r *stubExecRunner) Run(_ context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
	r.name = name
	r.args = append([]string(nil), args...)
	r.stdin = stdin
	return r.stdout, r.stderr, r.err
}

// encodeTestPNG builds a decodable frame for processors that expect ffmpeg to
// pipe PNG bytes to stdout.
func encodeTestPNG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewNRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetNRGBA(x, y, color.NRGBA{R: uint8(x % 256), G: uint8(y % 256), B: 96, A: 255})
		}
	}

	var buffer bytes.Buffer
	if err := png.Encode(&buffer, img); err != nil {
		t.Fatalf("encode png fixture: %v", err)
	}
	return buffer.Bytes()
}

func hasArgSequence(args []string, want ...string) bool {
	if len(want) == 0 {
		return true
	}

	for index := 0; index+len(want) <= len(args); index++ {
		matched := true
		for offset, value := range want {
			if args[index+offset] != value {
				matched = false
				break
			}
		}
		if matched {
			return true
		}
	}

	return false
}

func TestVideoProcessorExtractsFirstFrameAndEncodesPreview(t *testing.T) {
	frame := encodeTestPNG(t, 1280, 720)
	runner := &stubExecRunner{stdout: frame}
	req := PreviewRequest{Path: filepath.Join(t.TempDir(), "clip.mp4"), Width: 320, Height: 180, Format: "webp"}

	data, contentType, err := NewVideoProcessor(runner).Process(context.Background(), req)
	if err != nil {
		t.Fatalf("expected the video preview to succeed, got %v", err)
	}

	if runner.name != "ffmpeg" {
		t.Fatalf("expected ffmpeg to be invoked, got %q", runner.name)
	}
	if !hasArgSequence(runner.args, "-frames:v", "1") {
		t.Fatalf("expected -frames:v 1 in ffmpeg args, got %v", runner.args)
	}
	if !hasArgSequence(runner.args, "-vf", "scale=320:180:force_original_aspect_ratio=decrease") {
		t.Fatalf("expected the scale filter in ffmpeg args, got %v", runner.args)
	}
	if !hasArgSequence(runner.args, "-f", "image2pipe", "-vcodec", "png", "-") {
		t.Fatalf("expected a png frame streamed to stdout, got %v", runner.args)
	}
	if runner.stdin != nil {
		t.Fatal("expected ffmpeg to run without stdin")
	}

	if contentType != "image/webp" {
		t.Fatalf("expected image/webp, got %q", contentType)
	}

	size, sizeErr := bimg.NewImage(data).Size()
	if sizeErr != nil {
		t.Fatalf("expected a valid encoded image, got %v", sizeErr)
	}
	if size.Width != 320 || size.Height != 180 {
		t.Fatalf("expected a 320x180 preview, got %dx%d", size.Width, size.Height)
	}
}

func TestVideoProcessorUsesJPEGWhenRequested(t *testing.T) {
	frame := encodeTestPNG(t, 640, 480)
	runner := &stubExecRunner{stdout: frame}
	req := PreviewRequest{Path: "clip.mp4", Width: 160, Height: 120, Format: "jpeg"}

	data, contentType, err := NewVideoProcessor(runner).Process(context.Background(), req)
	if err != nil {
		t.Fatalf("expected the video preview to succeed, got %v", err)
	}
	if contentType != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", contentType)
	}
	if _, sizeErr := bimg.NewImage(data).Size(); sizeErr != nil {
		t.Fatalf("expected a valid encoded image, got %v", sizeErr)
	}
}

func TestVideoProcessorReportsFFmpegFailure(t *testing.T) {
	runner := &stubExecRunner{
		stderr: []byte("Invalid data found when processing input"),
		err:    errors.New("exit status 1"),
	}
	req := PreviewRequest{Path: "clip.mp4", Width: 160, Height: 90, Format: "auto"}

	_, _, err := NewVideoProcessor(runner).Process(context.Background(), req)
	if err == nil {
		t.Fatal("expected ffmpeg failure")
	}
	if !strings.Contains(err.Error(), "ffmpeg failed") {
		t.Fatalf("expected a clear ffmpeg error, got %v", err)
	}
	if !strings.Contains(err.Error(), "Invalid data found") {
		t.Fatalf("expected the stderr detail, got %v", err)
	}
}

func TestVideoProcessorReportsEmptyOutput(t *testing.T) {
	runner := &stubExecRunner{}
	req := PreviewRequest{Path: "clip.mp4", Width: 160, Height: 90, Format: "auto"}

	_, _, err := NewVideoProcessor(runner).Process(context.Background(), req)
	if err == nil {
		t.Fatal("expected an error when ffmpeg writes no frame")
	}
	if !strings.Contains(err.Error(), "produced no frame") {
		t.Fatalf("expected the empty output error, got %v", err)
	}
}
