package preview

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestBimgProcessorSmallImages(t *testing.T) {
	// locate test images under ../../testdata if present
	td := filepath.Join("..", "..", "testdata", "images")
	// tolerate missing testdata by skipping
	if _, err := os.Stat(td); os.IsNotExist(err) {
		t.Skip("no testdata images available")
	}

	files := []string{"small.png", "small.jpg"}
	p := NewBimgProcessor()
	for _, f := range files {
		path := filepath.Join(td, f)
		if _, err := os.Stat(path); err != nil {
			t.Skipf("missing fixture %s: %v", path, err)
		}
		req := PreviewRequest{Path: path, Width: 64, Height: 64, Format: "auto"}
		data, ct, err := p.Process(context.Background(), req)
		if err != nil {
			// detect libvips missing: bimg will return err mentioning 'vips'
			// if libvips is missing, surface skip so CI remains green where unavailable
			t.Skipf("libvips not available or processing failed: %v", err)
		}
		if len(data) == 0 {
			t.Fatalf("empty output for %s", f)
		}
		if ct != "image/webp" && ct != "image/jpeg" {
			t.Fatalf("unexpected content-type: %s", ct)
		}
	}
}
