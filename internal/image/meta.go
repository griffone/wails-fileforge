package image

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/h2non/bimg"
)

// planImageMeta is the header-only description of an image used while
// planning. It never decodes or re-encodes pixels: libvips reads the container
// metadata, and the GIF frame count walks the block structure of the same
// bytes.
type planImageMeta struct {
	Width  int
	Height int
	Format string
	Alpha  bool
	Frames int
}

// readPlanImageMeta reads the input once and reports the metadata planning
// needs. Dimensions follow the EXIF orientation exactly like execution, and
// Format is the normalized lowercase type (jpeg, png, webp, gif, tiff) with
// the file extension as a fallback.
func readPlanImageMeta(inputPath string) (planImageMeta, error) {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return planImageMeta{}, fmt.Errorf("read input file: %w", err)
	}

	metadata, err := bimg.Metadata(data)
	if err != nil {
		return planImageMeta{}, fmt.Errorf("read image metadata: %w", err)
	}

	meta := planImageMeta{
		Width:  metadata.Size.Width,
		Height: metadata.Size.Height,
		Format: planImageFormat(metadata.Type, inputPath),
		Alpha:  metadata.Alpha,
		Frames: 1,
	}

	orientation := metadata.Orientation
	if orientation == 0 {
		orientation = metadata.EXIF.Orientation
	}
	// EXIF orientations 5-8 rotate by 90 or 270 degrees, so the displayed size
	// swaps the stored axes.
	if orientation >= 5 && orientation <= 8 {
		meta.Width, meta.Height = meta.Height, meta.Width
	}

	if meta.Format == "gif" {
		meta.Frames = compressGIFFrameCount(data)
		if meta.Frames < 1 {
			meta.Frames = 1
		}
	}

	return meta, nil
}

// planImageFormat normalizes the detected container type and falls back to the
// file extension when libvips cannot name it.
func planImageFormat(detectedType, inputPath string) string {
	format := normalizeFormatName(detectedType)
	if format != "" && format != "unknown" {
		return format
	}

	return normalizeFormatName(strings.TrimPrefix(filepath.Ext(inputPath), "."))
}
