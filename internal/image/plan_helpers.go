package image

import (
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
)

// readPlanImageDimensions returns the pixel size used to validate operation
// bounds at planning time without decoding whole images. JPEG, TIFF and WebP
// can carry an EXIF orientation that the executor applies before drawing, so
// the dimensions follow that orientation exactly like execution.
func readPlanImageDimensions(inputPath string) (int, int, error) {
	meta, err := readPlanImageMeta(inputPath)
	if err != nil {
		return 0, 0, err
	}

	return meta.Width, meta.Height, nil
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

// planStringSlice reads a per-input string list from the effective options. A
// plan that went through JSON decoding carries []any, so both shapes are
// accepted.
func planStringSlice(effective map[string]any, key string) []string {
	if effective == nil {
		return nil
	}

	switch values := effective[key].(type) {
	case []string:
		return values
	case []any:
		out := make([]string, 0, len(values))
		for _, value := range values {
			if text, ok := value.(string); ok {
				out = append(out, text)
			}
		}
		return out
	default:
		return nil
	}
}

// planBoolSlice reads a per-input boolean list from the effective options,
// accepting the []any shape produced by JSON decoding.
func planBoolSlice(effective map[string]any, key string) []bool {
	if effective == nil {
		return nil
	}

	switch values := effective[key].(type) {
	case []bool:
		return values
	case []any:
		out := make([]bool, 0, len(values))
		for _, value := range values {
			if flag, ok := value.(bool); ok {
				out = append(out, flag)
			}
		}
		return out
	default:
		return nil
	}
}

// plannedOutputFormat resolves the encoding of one planned output. A shared
// effective format wins so an explicit single output path keeps the requested
// encoding; otherwise the planned extension decides. The per-input "auto"
// marker is not an encoding, so callers resolve it from the planned input.
func plannedOutputFormat(planned models.PlannedOutputV1, effective map[string]any) string {
	if format := convertOptionString(effective, "format"); format != "" && format != imageAutoFormat && format != "original" {
		return strings.ToLower(format)
	}

	return strings.ToLower(strings.TrimPrefix(filepath.Ext(planned.OutputPath), "."))
}
