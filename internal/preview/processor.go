package preview

import (
	"context"
	"fmt"
	"math"
	"os"

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

	img = bimg.NewImage(oriented)
	size, err := img.Size()
	if err != nil {
		return nil, "", fmt.Errorf("preview: %w", err)
	}

	// compute resize keeping aspect ratio and fitting inside requested box
	origW := float64(size.Width)
	origH := float64(size.Height)
	maxW := float64(req.Width)
	maxH := float64(req.Height)
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

	want := req.Format
	switch want {
	case "webp":
		out, ct, err := tryEncode(bimg.WEBP)
		if err != nil {
			return nil, "", fmt.Errorf("preview: %w", err)
		}
		return out, ct, nil
	case "jpeg":
		out, ct, err := tryEncode(bimg.JPEG)
		if err != nil {
			return nil, "", fmt.Errorf("preview: %w", err)
		}
		return out, ct, nil
	default: // auto
		out, ct, err := tryEncode(bimg.WEBP)
		if err == nil {
			return out, ct, nil
		}
		// fallback to jpeg
		out2, ct2, err2 := tryEncode(bimg.JPEG)
		if err2 != nil {
			// return original webp error wrapped behind preview
			return nil, "", fmt.Errorf("preview: webp: %v; jpeg: %w", err, err2)
		}
		return out2, ct2, nil
	}
}
