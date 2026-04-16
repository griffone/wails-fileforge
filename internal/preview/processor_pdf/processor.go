package processor_pdf

import (
	"context"
	"fmt"

	preview "fileforge-desktop/internal/preview"
	cachepkg "fileforge-desktop/internal/utils/cache"
)

// Processor handles PDF preview rendering.
type Processor interface {
	Process(ctx context.Context, req preview.PreviewRequest) (data []byte, mime string, err error)
}

type PDFProcessor struct {
	runner preview.ExecRunner
}

func NewPDFProcessor(runner preview.ExecRunner) Processor {
	return &PDFProcessor{runner: runner}
}

// Process renders a single page from PDF using external tools invoked via runner.
// It computes the cache key and then invokes pdftoppm + cwebp/jpeg conversion via
// the provided runner. For tests we mock runner to return bytes.
func (p *PDFProcessor) Process(ctx context.Context, req preview.PreviewRequest) ([]byte, string, error) {
	pr := preview.PageRange{Start: req.PageRange.Start, End: req.PageRange.End}
	// compute cache key (use file path as identifier)
	key := cachepkg.GeneratePreviewCacheKey(req.Path, 0, 0, cachepkg.PageRange{Start: pr.Start, End: pr.End}, req.PageOffset, req.Width, req.Height, req.Format, 80)
	_ = key // future: use cache

	// Build pdftoppm args (not actually executed in unit tests)
	// pdftoppm -f <page> -l <page> -png -scale-to-x <w> -scale-to-y <h> <pdf> -
	args := []string{"-f", fmt.Sprintf("%d", pr.Start), "-l", fmt.Sprintf("%d", pr.End), "-png", "-scale-to-x", fmt.Sprintf("%d", req.Width), "-scale-to-y", fmt.Sprintf("%d", req.Height), req.Path}

	stdout, stderr, err := p.runner.Run(ctx, "pdftoppm", args, nil)
	if err != nil {
		return nil, "", fmt.Errorf("pdf processor: pdftoppm failed: %w; stderr=%s", err, string(stderr))
	}

	// Attempt to convert to preferred format via cwebp or fallback jpeg
	if req.Format == "webp" || req.Format == "auto" {
		// cwebp -lossy -q <quality> - -o -
		wargs := []string{"-lossy", "-q", "80", "-", "-o", "-"}
		wout, werr, werrCmd := p.runner.Run(ctx, "cwebp", wargs, stdout)
		if werrCmd == nil && len(wout) > 0 {
			return wout, "image/webp", nil
		}
		// fallthrough to jpeg
		_ = werr
	}

	// Fallback: use jpeg conversion via some tool (simulate with runner)
	jargs := []string{"-quality", "80", "-", "-o", "-"}
	jout, _, jerrCmd := p.runner.Run(ctx, "jpegtool", jargs, stdout)
	if jerrCmd != nil {
		return nil, "", fmt.Errorf("pdf processor: jpeg conversion failed: %w", jerrCmd)
	}
	return jout, "image/jpeg", nil
}
