package interfaces

import (
	"context"
	"fileforge-desktop/internal/models"
)

// Converter defines the interface for file conversion operations.
type Converter interface {
	SupportedFormats() []string
	// Single file conversion - returns error for internal operations
	ConvertSingle(ctx context.Context, inputPath, outputPath, format string, options map[string]any) error
	// Batch conversion - returns results with errors
	ConvertBatch(ctx context.Context, inputPaths []string, outputDir, format string, keepStructure bool, workers int, options map[string]any, onProgress func(result models.ConversionResult)) ([]models.ConversionResult, error)
}
