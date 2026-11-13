package image

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"sort"
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
			"tiff": bimg.TIFF,
			"avif": bimg.AVIF,
		},
	}
}

func (c *ImageConverter) Convert(input []byte, opts map[string]any) ([]byte, error) {
	img := bimg.NewImage(input)

	format, ok := opts["format"].(string)
	if !ok || strings.TrimSpace(format) == "" {
		format = "webp"
	}

	format = normalizeFormatName(format)
	imageType, exists := c.formats[format]
	if !exists {
		return nil, fmt.Errorf("unsupported image format: %s", format)
	}

	bimgOptions := bimg.Options{
		Type: imageType,
	}

	if quality, ok := numericOption(opts, "quality"); ok {
		bimgOptions.Quality = quality
	}

	if rotate, ok := numericOption(opts, "rotate"); ok && rotate != 0 {
		angle, angleErr := rotateAngle(rotate)
		if angleErr != nil {
			return nil, angleErr
		}
		bimgOptions.Rotate = angle
	}

	// bimg names the left-right mirror Flip (about the vertical axis) and the
	// top-bottom mirror Flop, which is the opposite of what the option names
	// suggest at first glance.
	bimgOptions.Flip = boolOptionValue(opts, "flipH")
	bimgOptions.Flop = boolOptionValue(opts, "flipV")
	bimgOptions.StripMetadata = boolOptionValue(opts, "stripMetadata")

	// longestEdge wins over width/height: it constrains only the larger side
	// and keeps the aspect ratio of portrait and landscape images alike.
	if longestEdge, ok := numericOption(opts, "longestEdge"); ok && longestEdge > 0 {
		width, height, sizeErr := resizeSize(img, bimgOptions.Rotate)
		if sizeErr != nil {
			return nil, fmt.Errorf("read image dimensions: %w", sizeErr)
		}

		// bimg forces the exact target size when a single axis is set, so an
		// image that already fits the limit must not be resized at all.
		if height > width {
			if height > longestEdge {
				bimgOptions.Height = longestEdge
			}
		} else if width > longestEdge {
			bimgOptions.Width = longestEdge
		}
	} else {
		if width, ok := numericOption(opts, "width"); ok && width > 0 {
			bimgOptions.Width = width
		}

		if height, ok := numericOption(opts, "height"); ok && height > 0 {
			bimgOptions.Height = height
		}
	}

	// Enlarge stays false. Explicit width/height still land on the requested
	// size (bimg behavior, unchanged); longestEdge guards against upscaling.
	return img.Process(bimgOptions)
}

// SupportedFormats lists the encodable formats in a deterministic order.
func (c *ImageConverter) SupportedFormats() []string {
	formats := make([]string, 0, len(c.formats))
	for format := range c.formats {
		formats = append(formats, format)
	}
	sort.Strings(formats)
	return formats
}

// sourceFormat maps a file extension to the converter format that can
// represent it. It reports false for inputs the converter can read but cannot
// write, such as BMP.
func (c *ImageConverter) sourceFormat(inputPath string) (string, bool) {
	format := normalizeFormatName(strings.TrimPrefix(filepath.Ext(inputPath), "."))
	if _, ok := c.formats[format]; !ok {
		return "", false
	}

	return format, true
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

	opts := make(map[string]any, len(options)+1)
	for k, v := range options {
		opts[k] = v
	}
	// The resolved format always wins over the request option, which may still
	// carry the per-input "auto" marker.
	opts["format"] = format

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
	if strings.TrimSpace(format) == "" {
		return fmt.Errorf("output format cannot be empty")
	}

	format = normalizeFormatName(format)

	if _, exists := c.formats[format]; !exists {
		return fmt.Errorf("unsupported output format '%s'. Supported formats: %v", format, c.SupportedFormats())
	}

	return nil
}

// normalizeFormatName lowercases a format name and folds the extension aliases
// into the names used by the converter map.
func normalizeFormatName(format string) string {
	switch normalized := strings.ToLower(strings.TrimSpace(format)); normalized {
	case "jpg":
		return "jpeg"
	case "tif":
		return "tiff"
	default:
		return normalized
	}
}

// rotateAngle validates a quarter-turn rotation in degrees and maps it to the
// matching bimg angle.
func rotateAngle(degrees int) (bimg.Angle, error) {
	switch degrees {
	case 0:
		return bimg.D0, nil
	case 90:
		return bimg.D90, nil
	case 180:
		return bimg.D180, nil
	case 270:
		return bimg.D270, nil
	default:
		return bimg.D0, fmt.Errorf("unsupported rotate value %d: use 0, 90, 180 or 270", degrees)
	}
}

// resizeSize returns the pixel size bimg will resize from. A plain conversion
// follows the EXIF orientation (5-8 rotate by 90 or 270 degrees), while an
// explicit rotation disables that auto-rotation and turns the stored axes.
func resizeSize(img *bimg.Image, rotate bimg.Angle) (int, int, error) {
	metadata, err := img.Metadata()
	if err != nil {
		return 0, 0, err
	}

	width := metadata.Size.Width
	height := metadata.Size.Height

	switch {
	case rotate == bimg.D90 || rotate == bimg.D270:
		width, height = height, width
	case rotate == bimg.D0 && metadata.Orientation >= 5 && metadata.Orientation <= 8:
		width, height = height, width
	}

	return width, height, nil
}
