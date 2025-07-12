package image

import (
	"fmt"
	"os"

	"github.com/h2non/bimg"
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
