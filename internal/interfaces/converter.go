package interfaces

import "fileforge-desktop/internal/models"

// Converter defines the interface for file conversion operations.
type Converter interface {
	SupportedFormats() []string
	// Single file conversion
	ConvertSingle(inputPath, outputPath, format string) error
	// Batch conversion
	ConvertBatch(inputPaths []string, outputDir, format string, keepStructure bool, workers int) []models.ConversionResult
}
