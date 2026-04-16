package preview

import (
	"context"
	"fmt"
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
	// TODO: (image-preview) actual implementation using bimg. For now, attempt to import
	// and use bimg; if libvips not present, return wrapped error.
	// This placeholder demonstrates structure and error wrapping.
	return nil, "", fmt.Errorf("preview: bimg processor not implemented or libvips missing")
}
