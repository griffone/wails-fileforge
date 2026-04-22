package preview

import (
	"context"
	"fmt"
	"strings"

	cachepkg "fileforge-desktop/internal/utils/cache"
)

const (
	mimePNG  = "image/png"
	mimeJPEG = "image/jpeg"
	mimeWebP = "image/webp"
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

	page := pr.Start + req.PageOffset
	if page < pr.Start {
		page = pr.Start
	}
	if page > pr.End {
		page = pr.End
	}

	format := strings.ToLower(req.Format)
	if format == "" {
		format = "auto"
	}

	pdftoppmFormatFlag := "-png"
	contentType := mimePNG
	if format == "jpeg" {
		pdftoppmFormatFlag = "-jpeg"
		contentType = mimeJPEG
	}

	// Build pdftoppm args and stream one rendered page to stdout.
	args := []string{
		"-f", fmt.Sprintf("%d", page),
		"-l", fmt.Sprintf("%d", page),
		"-singlefile",
		pdftoppmFormatFlag,
		"-scale-to-x", fmt.Sprintf("%d", req.Width),
		"-scale-to-y", fmt.Sprintf("%d", req.Height),
		req.Path,
	}

	stdout, stderr, err := p.runner.Run(ctx, "pdftoppm", args, nil)
	if err != nil {
		return nil, "", fmt.Errorf("pdf processor: pdftoppm failed: %w; stderr=%s", err, string(stderr))
	}
	if len(stdout) == 0 {
		return nil, "", fmt.Errorf("pdf processor: pdftoppm produced no output")
	}

	// Attempt webp conversion via cwebp. If unavailable, gracefully fall back to png.
	if format == "webp" || format == "auto" {
		wargs := []string{"-lossy", "-q", "80", "-", "-o", "-"}
		wout, _, werr := p.runner.Run(ctx, "cwebp", wargs, stdout)
		if werr == nil && len(wout) > 0 {
			return wout, mimeWebP, nil
		}
		if format == "webp" {
			// Keep preview resilient even when cwebp is unavailable.
			return stdout, mimePNG, nil
		}
		return stdout, mimePNG, nil
	}

	return stdout, contentType, nil
}
