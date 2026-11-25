package preview

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/doc/engine"
)

// docPreviewMaxInputBytes keeps document previews below the global 50 MiB
// preview cap. In-process DOCX layout and Markdown rendering allocate the whole
// document, and a 200-page file with embedded images can grow large; 20 MiB is
// enough for real documents while bounding memory and render time. Hitting the
// cap degrades to "no preview" instead of blocking the drawer. The service also
// wraps every job in Config.JobTimeout, so a pathological document is canceled
// and reported as timed out, which the card hides the same way.
const docPreviewMaxInputBytes = 20 << 20

// documentPreviewExtensions lists the document formats previewed by rendering
// them to a temporary PDF first. Keys are lowercase and include the dot.
var documentPreviewExtensions = map[string]struct{}{
	".docx":     {},
	".md":       {},
	".markdown": {},
}

// ErrNoPreview is the typed outcome for a file that cannot produce a
// thumbnail. It is an expected result, not a render error: callers hide the
// preview instead of showing a broken image or a card-level error.
var ErrNoPreview = errors.New("preview: no preview available")

// NoPreviewError wraps the detail behind a missing document preview. The error
// string stays constant so renderer internals never reach the card; the cause
// remains available through errors.Unwrap for diagnostics and tests.
type NoPreviewError struct {
	Cause error
}

func (e *NoPreviewError) Error() string {
	return ErrNoPreview.Error()
}

func (e *NoPreviewError) Unwrap() error {
	return e.Cause
}

func (e *NoPreviewError) Is(target error) bool {
	return target == ErrNoPreview
}

func noPreview(cause error) error {
	return &NoPreviewError{Cause: cause}
}

func isDocumentPreviewExtension(ext string) bool {
	_, ok := documentPreviewExtensions[strings.ToLower(ext)]
	return ok
}

// DocumentProcessor renders the first page of DOCX and Markdown inputs to an
// image. It converts the document to a temporary PDF with the same in-process
// engines the real tools use, then delegates to the PDF processor so the
// established first-page pipeline (pdftoppm/cwebp) and its limits stay in one
// place.
//
// Preview intentionally renders with conversion defaults only: no PDF/A, no
// page selection, no header/footer customization. Those options belong to the
// conversion tools; duplicating them here would make the card silently disagree
// with the real output.
type DocumentProcessor struct {
	pdf      JobProcessor
	renderer engine.DOCXRenderer
}

// NewDocumentProcessor returns a processor for document thumbnail previews.
func NewDocumentProcessor(runner ExecRunner) JobProcessor {
	return &DocumentProcessor{
		pdf:      NewPDFProcessor(runner),
		renderer: &engine.LibraryRenderer{},
	}
}

func (p *DocumentProcessor) Process(ctx context.Context, req PreviewRequest) ([]byte, string, error) {
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	info, err := os.Stat(req.Path)
	if err != nil {
		return nil, "", noPreview(err)
	}
	if info.IsDir() || info.Size() > docPreviewMaxInputBytes {
		return nil, "", noPreview(errors.New("document is not previewable or exceeds the preview size limit"))
	}

	ext := strings.ToLower(filepath.Ext(req.Path))
	if !isDocumentPreviewExtension(ext) {
		return nil, "", noPreview(errors.New("document format is not previewable"))
	}

	// Each conversion gets its own scratch directory so a failed render never
	// leaves a partial PDF next to the user's document.
	workDir, err := os.MkdirTemp("", "fileforge-doc-preview-*")
	if err != nil {
		return nil, "", noPreview(err)
	}
	defer func() { _ = os.RemoveAll(workDir) }()

	pdfPath := filepath.Join(workDir, "preview.pdf")
	switch ext {
	case ".docx":
		_, err = engine.ConvertDOCX(ctx, p.renderer, nil, engine.ConvertDOCXRequest{
			InputPath:  req.Path,
			OutputPath: pdfPath,
		})
	case ".md", ".markdown":
		err = engine.RenderMarkdownToPDF(ctx, engine.RenderConfig{
			InputPath:  req.Path,
			OutputPath: pdfPath,
		})
	}
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, "", ctxErr
		}
		return nil, "", noPreview(err)
	}
	if err := ctx.Err(); err != nil {
		return nil, "", err
	}

	pdfReq := req
	pdfReq.Path = pdfPath
	data, contentType, err := p.pdf.Process(ctx, pdfReq)
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, "", ctxErr
		}
		return nil, "", noPreview(err)
	}

	return data, contentType, nil
}
