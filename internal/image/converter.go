package image

import (
	"fileforge-desktop/internal/interfaces"
	"fileforge-desktop/internal/models"
	"fmt"
	"os"
	"path/filepath"

	"github.com/h2non/bimg"
)

// Ensure ImageConverter implements the Converter interface
var _ interfaces.Converter = (*ImageConverter)(nil)

type ImageConverter struct {
	formats map[string]bimg.ImageType
}

func NewImageConverter() *ImageConverter {
	return &ImageConverter{
		formats: map[string]bimg.ImageType{
			"webp": bimg.WEBP,
			"jpeg": bimg.JPEG,
			"png":  bimg.PNG,
			"gif":  bimg.GIF,
		},
	}
}

func (c *ImageConverter) Convert(input []byte, opts map[string]any) ([]byte, error) {
	img := bimg.NewImage(input)

	format, ok := opts["format"].(string)
	if !ok {
		format = "webp"
	}

	imageType, exists := c.formats[format]
	if !exists {
		return nil, fmt.Errorf("unsupported image format: %s", format)
	}

	return img.Convert(imageType)
}

func (c *ImageConverter) SupportedFormats() []string {
	var formats []string
	for format := range c.formats {
		formats = append(formats, format)
	}
	return formats
}

func (c *ImageConverter) ConvertSingle(inputPath, outputPath, format string) error {
	input, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("error reading file: %v", err)
	}

	opts := map[string]any{"format": format}
	output, err := c.Convert(input, opts)
	if err != nil {
		return fmt.Errorf("error converting file: %v", err)
	}

	return os.WriteFile(outputPath, output, 0644)
}

// ConvertBatch converts multiple files in batch
func (c *ImageConverter) ConvertBatch(inputPaths []string, outputDir, format string, keepStructure bool) []models.FileConversionResult {
	results := make([]models.FileConversionResult, len(inputPaths))

	for i, inputPath := range inputPaths {
		result := models.FileConversionResult{
			InputPath: inputPath,
			Success:   false,
		}

		// Generate output path
		var outputPath string
		if keepStructure {
			// Maintain directory structure relative to common base
			outputPath = c.generateStructuredOutputPath(inputPath, outputDir, format)
		} else {
			// Flatten all files to output directory
			outputPath = c.generateFlatOutputPath(inputPath, outputDir, format)
		}

		result.OutputPath = outputPath

		// Ensure output directory exists
		if err := os.MkdirAll(filepath.Dir(outputPath), 0755); err != nil {
			result.Message = fmt.Sprintf("error creating output directory: %v", err)
			results[i] = result
			continue
		}

		// Convert the file
		if err := c.ConvertSingle(inputPath, outputPath, format); err != nil {
			result.Message = fmt.Sprintf("conversion failed: %v", err)
		} else {
			result.Success = true
			result.Message = "conversion successful"
		}

		results[i] = result
	}

	return results
}

func (c *ImageConverter) generateFlatOutputPath(inputPath, outputDir, format string) string {
	baseName := filepath.Base(inputPath)
	ext := filepath.Ext(baseName)
	nameWithoutExt := baseName[:len(baseName)-len(ext)]
	return filepath.Join(outputDir, fmt.Sprintf("%s.%s", nameWithoutExt, format))
}

func (c *ImageConverter) generateStructuredOutputPath(inputPath, outputDir, format string) string {
	// For now, implement flat structure. Can be enhanced later for complex directory structures
	return c.generateFlatOutputPath(inputPath, outputDir, format)
}
