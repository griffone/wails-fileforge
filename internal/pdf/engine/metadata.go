package engine

import (
	"context"
	"io"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// ReadDocumentTitle returns the document information Title without rendering,
// rasterizing or running OCR. PDFInfo uses relaxed validation because the goal
// is metadata, not a full integrity check. An empty title is not an error: the
// caller falls back to another naming source.
func ReadDocumentTitle(ctx context.Context, reader io.ReadSeeker, fileName string) (string, error) {
	info, err := api.PDFInfo(pdfCPUContext(ctx), reader, fileName, nil, false, newPDFCPUConfig())
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(info.Title), nil
}
