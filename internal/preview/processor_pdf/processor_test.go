package processor_pdf

import (
	"context"
	"fmt"
	"testing"
	"time"

	previewpkg "fileforge-desktop/internal/preview"
)

// fakeRunner implements ExecRunner for tests.
type fakeRunner struct {
	FailCwebp bool
	FailJpeg  bool
}

func (f *fakeRunner) Run(ctx context.Context, name string, args []string, stdin []byte) ([]byte, []byte, error) {
	// return response based on command name
	switch name {
	case "pdftoppm":
		// return a PNG-like byte slice
		return []byte("PNGDATA"), nil, nil
	case "cwebp":
		if f.FailCwebp {
			return nil, []byte("cwebp error"), fmt.Errorf("cwebp failed")
		}
		return []byte("WEBPDATA"), nil, nil
	case "jpegtool":
		if f.FailJpeg {
			return nil, []byte("jpeg error"), fmt.Errorf("jpeg failed")
		}
		return []byte("JPEGDATA"), nil, nil
	default:
		return nil, nil, nil
	}
}

func TestPDFProcessorPrefersWebP(t *testing.T) {
	fr := &fakeRunner{}
	p := NewPDFProcessor(fr)
	req := previewpkg.PreviewRequest{Path: "/tmp/doc.pdf", Width: 128, Height: 128, Format: "webp", PageRange: &previewpkg.PageRange{Start: 1, End: 1}}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	data, mime, err := p.Process(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/webp" {
		t.Fatalf("expected webp mime, got %s", mime)
	}
	if string(data) != "WEBPDATA" {
		t.Fatalf("unexpected data: %s", string(data))
	}
}

func TestPDFProcessorFallsBackToJPEG(t *testing.T) {
	// override to simulate cwebp failing
	frFail := &fakeRunner{FailCwebp: true}
	p := NewPDFProcessor(frFail)
	req := previewpkg.PreviewRequest{Path: "/tmp/doc.pdf", Width: 128, Height: 128, Format: "auto", PageRange: &previewpkg.PageRange{Start: 1, End: 1}}
	ctx, cancel := context.WithTimeout(context.Background(), 250*time.Millisecond)
	defer cancel()
	data, mime, err := p.Process(ctx, req)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if mime != "image/jpeg" {
		t.Fatalf("expected jpeg mime, got %s", mime)
	}
	if string(data) != "JPEGDATA" {
		t.Fatalf("unexpected data: %s", string(data))
	}
}
