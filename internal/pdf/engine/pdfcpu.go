package engine

import (
	"context"

	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// newPDFCPUConfig returns a fresh pdfcpu configuration for one operation. The
// configuration is mutable, so it is never shared across calls or goroutines.
func newPDFCPUConfig() *model.Configuration {
	return model.NewDefaultConfiguration()
}

// pdfCPUContext normalizes a nil context because pdfcpu requires a non-nil one.
func pdfCPUContext(ctx context.Context) context.Context {
	if ctx == nil {
		return context.Background()
	}

	return ctx
}
