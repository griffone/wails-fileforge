package image

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/h2non/bimg"
)

const (
	DefaultFilePermissions = 0644
	OperationCancelledMsg  = "operation cancelled: %w"
)

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

	bimgOptions := bimg.Options{
		Type: imageType,
	}

	if quality, ok := opts["quality"].(float64); ok {
		bimgOptions.Quality = int(quality)
	} else if quality, ok := opts["quality"].(int); ok {
		bimgOptions.Quality = quality
	}

	if width, ok := opts["width"].(float64); ok {
		bimgOptions.Width = int(width)
	} else if width, ok := opts["width"].(int); ok {
		bimgOptions.Width = width
	}

	if height, ok := opts["height"].(float64); ok {
		bimgOptions.Height = int(height)
	} else if height, ok := opts["height"].(int); ok {
		bimgOptions.Height = height
	}

	return img.Process(bimgOptions)
}

func (c *ImageConverter) SupportedFormats() []string {
	var formats []string
	for format := range c.formats {
		formats = append(formats, format)
	}
	return formats
}

func (c *ImageConverter) ConvertSingle(ctx context.Context, inputPath, outputPath, format string, options map[string]any) error {
	// Check context before starting
	select {
	case <-ctx.Done():
		return fmt.Errorf(OperationCancelledMsg, ctx.Err())
	default:
	}

	// Validate input file extension
	if err := c.validateInputFile(inputPath); err != nil {
		return fmt.Errorf("input validation failed: %w", err)
	}

	// Validate output format
	if err := c.validateOutputFormat(format); err != nil {
		return fmt.Errorf("format validation failed: %w", err)
	}

	input, err := os.ReadFile(inputPath)
	if err != nil {
		return fmt.Errorf("error reading file: %w", err)
	}

	// Check context before conversion
	select {
	case <-ctx.Done():
		return fmt.Errorf(OperationCancelledMsg, ctx.Err())
	default:
	}

	opts := map[string]any{"format": format}
	for k, v := range options {
		opts[k] = v
	}
	output, err := c.Convert(input, opts)
	if err != nil {
		return fmt.Errorf("error converting file: %w", err)
	}

	// Check context before writing
	select {
	case <-ctx.Done():
		return fmt.Errorf(OperationCancelledMsg, ctx.Err())
	default:
	}

	err = os.WriteFile(outputPath, output, DefaultFilePermissions)
	if err != nil {
		return fmt.Errorf("error writing output file: %w", err)
	}

	return nil
}

// validateInputFile checks if the input file has a supported image extension
func (c *ImageConverter) validateInputFile(inputPath string) error {
	if inputPath == "" {
		return fmt.Errorf("input path cannot be empty")
	}

	ext := strings.ToLower(filepath.Ext(inputPath))
	if ext == "" {
		return fmt.Errorf("input file has no extension")
	}

	// Remove the dot from extension
	ext = ext[1:]

	// Define supported input extensions
	// TODO: Make this configurable or extendable
	supportedInputExts := map[string]bool{
		"jpg":  true,
		"jpeg": true,
		"png":  true,
		"gif":  true,
		"webp": true,
		"bmp":  true,
		"tiff": true,
		"tif":  true,
	}

	if !supportedInputExts[ext] {
		var supported []string
		for supportedExt := range supportedInputExts {
			supported = append(supported, supportedExt)
		}
		return fmt.Errorf("unsupported input file extension '%s'. Supported extensions: %v", ext, supported)
	}

	return nil
}

// validateOutputFormat checks if the output format is supported
func (c *ImageConverter) validateOutputFormat(format string) error {
	if format == "" {
		return fmt.Errorf("output format cannot be empty")
	}

	format = strings.ToLower(format)

	if _, exists := c.formats[format]; !exists {
		return fmt.Errorf("unsupported output format '%s'. Supported formats: %v", format, c.SupportedFormats())
	}

	return nil
}
