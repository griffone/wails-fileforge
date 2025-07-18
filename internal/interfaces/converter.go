package interfaces

import "fileforge-desktop/internal/models"

// Converter defines the interface for file conversion operations.
type Converter interface {
	SupportedFormats() []string
	// Single file conversion - returns error for internal operations
	ConvertSingle(inputPath, outputPath, format string) error
	// Batch conversion - returns results with errors
	ConvertBatch(inputPaths []string, outputDir, format string, keepStructure bool, workers int) ([]models.ConversionResult, error)
}
