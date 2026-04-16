package preview

import (
	"context"
	"fmt"

	cachepkg "fileforge-desktop/internal/utils/cache"
)

// PDFProcessor renders PDF pages into image bytes using an ExecRunner to
// invoke external tools. Kept in this package to avoid import cycles.
type PDFProcessor struct {
	runner ExecRunner
}

func NewPDFProcessor(runner ExecRunner) JobProcessor {
	return &PDFProcessor{runner: runner}
}

func (p *PDFProcessor) Process(ctx context.Context, req PreviewRequest) ([]byte, string, error) {
	pr := req.PageRange
	cp := cachepkg.PageRange{Start: pr.Start, End: pr.End}
	key := cachepkg.GeneratePreviewCacheKey(req.Path, 0, 0, cp, req.PageOffset, req.Width, req.Height, req.Format, 80)
	_ = key // TODO: integrate with cache

	// Build pdftoppm args (not executed in unit tests)
	args := []string{"-f", fmt.Sprintf("%d", pr.Start), "-l", fmt.Sprintf("%d", pr.End), "-png", "-scale-to-x", fmt.Sprintf("%d", req.Width), "-scale-to-y", fmt.Sprintf("%d", req.Height), req.Path}

	stdout, stderr, err := p.runner.Run(ctx, "pdftoppm", args, nil)
	if err != nil {
		return nil, "", fmt.Errorf("pdf processor: pdftoppm failed: %w; stderr=%s", err, string(stderr))
	}

	// Attempt webp conversion via cwebp
	if req.Format == "webp" || req.Format == "auto" {
		wargs := []string{"-lossy", "-q", "80", "-", "-o", "-"}
		wout, _, werr := p.runner.Run(ctx, "cwebp", wargs, stdout)
		if werr == nil && len(wout) > 0 {
			return wout, "image/webp", nil
		}
	}

	// Fallback: jpeg via jpegtool (simulated)
	jargs := []string{"-quality", "80", "-", "-o", "-"}
	jout, _, jerr := p.runner.Run(ctx, "jpegtool", jargs, stdout)
	if jerr != nil {
		return nil, "", fmt.Errorf("pdf processor: jpeg conversion failed: %w", jerr)
	}
	return jout, "image/jpeg", nil
}
