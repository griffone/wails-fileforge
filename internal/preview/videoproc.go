package preview

import (
	"context"
	"fmt"
	"strings"
)

// videoPreviewExtensions lists the container extensions handled by ffmpeg for
// thumbnail previews. Keys are lowercase and include the leading dot.
var videoPreviewExtensions = map[string]struct{}{
	".mp4":  {},
	".mov":  {},
	".mkv":  {},
	".webm": {},
	".avi":  {},
}

func isVideoPreviewExtension(ext string) bool {
	_, ok := videoPreviewExtensions[strings.ToLower(ext)]
	return ok
}

// VideoProcessor extracts the first frame of a video with ffmpeg and encodes
// it into a preview image using bimg. External tools are injected through
// ExecRunner so the processor stays testable.
type VideoProcessor struct {
	runner ExecRunner
}

// NewVideoProcessor returns a processor for video thumbnail previews.
func NewVideoProcessor(runner ExecRunner) JobProcessor {
	return &VideoProcessor{runner: runner}
}

func (p *VideoProcessor) Process(ctx context.Context, req PreviewRequest) ([]byte, string, error) {
	width := clampPreviewDimension(req.Width)
	height := clampPreviewDimension(req.Height)

	// Scale the extracted frame inside the requested box; bimg then applies the
	// exact fit and encodes the requested output format.
	args := []string{
		"-v", "error",
		"-ss", "0",
		"-i", req.Path,
		"-frames:v", "1",
		"-an",
		"-sn",
		"-vf", fmt.Sprintf("scale=%d:%d:force_original_aspect_ratio=decrease", width, height),
		"-f", "image2pipe",
		"-vcodec", "png",
		"-",
	}

	stdout, stderr, err := p.runner.Run(ctx, "ffmpeg", args, nil)
	if err != nil {
		message := strings.TrimSpace(string(stderr))
		if message == "" {
			message = err.Error()
		}
		return nil, "", fmt.Errorf("video processor: ffmpeg failed: %w (stderr=%s)", err, message)
	}
	if len(stdout) == 0 {
		return nil, "", fmt.Errorf("video processor: ffmpeg produced no frame")
	}

	out, contentType, encodeErr := processImageBytes(stdout, width, height, req.Format)
	if encodeErr != nil {
		return nil, "", fmt.Errorf("preview: video frame encode failed: %w", encodeErr)
	}

	return out, contentType, nil
}

// clampPreviewDimension keeps ffmpeg scale targets inside the preview bounds.
func clampPreviewDimension(value int) int {
	if value < minPreviewDimension {
		return minPreviewDimension
	}
	if value > maxPreviewDimension {
		return maxPreviewDimension
	}
	return value
}
