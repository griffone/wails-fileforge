package interfaces

import (
	"context"
	"fileforge-desktop/internal/models"
)

// Converter defines the interface for file conversion operations.
type Converter interface {
	SupportedFormats() []string
	// Single file conversion - returns error for internal operations
	ConvertSingle(ctx context.Context, inputPath, outputPath, format string) error
	// Batch conversion - returns results with errors
	ConvertBatch(ctx context.Context, inputPaths []string, outputDir, format string, keepStructure bool, workers int) ([]models.ConversionResult, error)
}
