package image

import (
	"fmt"
	"image"
	_ "image/gif"
	_ "image/png"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"

	_ "golang.org/x/image/bmp"
)

// readPlanImageDimensions returns the pixel size used to validate operation
// bounds at planning time without decoding whole images when possible. JPEG,
// TIFF and WebP can carry an EXIF orientation that the executor applies before
// drawing, so those formats are normalized exactly like execution; the
// remaining formats only need their header decoded.
func readPlanImageDimensions(inputPath string) (int, int, error) {
	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(inputPath), "."))
	if extension == "jpg" || extension == "jpeg" || extension == "tif" || extension == "tiff" || extension == "webp" {
		_, _, width, height, err := readAndNormalize(inputPath)
		if err != nil {
			return 0, 0, err
		}
		return width, height, nil
	}

	file, err := os.Open(inputPath)
	if err != nil {
		return 0, 0, fmt.Errorf("open input file: %w", err)
	}
	defer file.Close()

	config, _, err := image.DecodeConfig(file)
	if err != nil {
		return 0, 0, fmt.Errorf("read image dimensions: %w", err)
	}

	return config.Width, config.Height, nil
}

// commonPlannedFormat returns the shared output format of a batch so the plan
// can expose a single effective value. Mixed batches leave it empty; each
// planned output extension carries its own format.
func commonPlannedFormat(formats []string) (string, bool) {
	if len(formats) == 0 {
		return "", false
	}

	first := formats[0]
	for _, format := range formats[1:] {
		if format != first {
			return "", false
		}
	}

	return first, true
}

// plannedOutputFormat resolves the encoding of one planned output. A shared
// effective format wins so an explicit single output path keeps the requested
// encoding; otherwise the planned extension decides.
func plannedOutputFormat(planned models.PlannedOutputV1, effective map[string]any) string {
	if format := convertOptionString(effective, "format"); format != "" {
		return strings.ToLower(format)
	}

	return strings.ToLower(strings.TrimPrefix(filepath.Ext(planned.OutputPath), "."))
}
