package preview

import (
	"context"
	"fmt"
	"math"
	"os"
	"strings"

	"github.com/h2non/bimg"
)

// JobProcessor performs image processing for preview jobs.
type JobProcessor interface {
	Process(ctx context.Context, req PreviewRequest) (data []byte, contentType string, err error)
}

// NewBimgProcessor returns a processor that uses github.com/h2non/bimg.
// If libvips is not available, operations will return an error wrapped with context.
func NewBimgProcessor() JobProcessor {
	return &bimgProcessor{}
}

type bimgProcessor struct{}

func (p *bimgProcessor) Process(ctx context.Context, req PreviewRequest) ([]byte, string, error) {
	const DefaultMaxInputSizeMB = 50

	// validate path size before reading
	fi, err := os.Stat(req.Path)
	if err != nil {
		return nil, "", fmt.Errorf("preview: %w", err)
	}
	if fi.Size() > int64(DefaultMaxInputSizeMB)*1024*1024 {
		return nil, "", fmt.Errorf("preview: %w", &ValidationError{Field: "path", Message: "file exceeds max input size"})
	}

	input, err := os.ReadFile(req.Path)
	if err != nil {
		return nil, "", fmt.Errorf("preview: %w", err)
	}

	// auto-apply EXIF orientation
	img := bimg.NewImage(input)
	oriented, err := img.AutoRotate()
	if err != nil {
		// If libvips not available, surface clear error for tests to skip
		return nil, "", fmt.Errorf("preview: %w", err)
	}

	out, contentType, err := processImageBytes(oriented, req.Width, req.Height, req.Format)
	if err != nil {
		return nil, "", fmt.Errorf("preview: %w", err)
	}

	return out, contentType, nil
}

// processImageBytes fits input inside the requested box while preserving the
// aspect ratio and encodes it with the requested format. WEBP is preferred for
// "auto" and JPEG is the fallback.
func processImageBytes(input []byte, width, height int, format string) ([]byte, string, error) {
	img := bimg.NewImage(input)

	size, err := img.Size()
	if err != nil {
		return nil, "", err
	}

	// compute resize keeping aspect ratio and fitting inside requested box
	origW := float64(size.Width)
	origH := float64(size.Height)
	maxW := float64(width)
	maxH := float64(height)
	scale := math.Min(maxW/origW, maxH/origH)
	if scale <= 0 || math.IsNaN(scale) || math.IsInf(scale, 0) {
		scale = 1.0
	}
	targetW := int(math.Max(1, math.Floor(origW*scale)))
	targetH := int(math.Max(1, math.Floor(origH*scale)))

	// helper to attempt encode to a target format
	tryEncode := func(imageType bimg.ImageType) ([]byte, string, error) {
		opts := bimg.Options{
			Width:   targetW,
			Height:  targetH,
			Quality: 80,
			Type:    imageType,
		}
		out, e := img.Process(opts)
		if e != nil {
			return nil, "", e
		}
		ct := "image/jpeg"
		if imageType == bimg.WEBP {
			ct = "image/webp"
		}
		return out, ct, nil
	}

	switch strings.ToLower(strings.TrimSpace(format)) {
	case "webp":
		return tryEncode(bimg.WEBP)
	case "jpeg":
		return tryEncode(bimg.JPEG)
	default: // auto
		out, ct, webpErr := tryEncode(bimg.WEBP)
		if webpErr == nil {
			return out, ct, nil
		}
		// fallback to jpeg
		out2, ct2, jpegErr := tryEncode(bimg.JPEG)
		if jpegErr != nil {
			// return original webp error wrapped behind jpeg
			return nil, "", fmt.Errorf("webp: %v; jpeg: %w", webpErr, jpegErr)
		}
		return out2, ct2, nil
	}
}
