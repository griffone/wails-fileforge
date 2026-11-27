package video

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

// probeTargetMedia feeds plan-time target estimates. Tests replace it so a plan
// never needs real ffprobe.
var probeTargetMedia = engine.ProbeTargetMediaOrDuration

// videoOptionTargetSize reads options.targetSizeBytes. Absent or null means no
// target; every numeric shape a JSON or Go caller can supply is accepted when
// it is positive and whole.
func videoOptionTargetSize(options map[string]any, validationCode string) (int64, *models.JobError) {
	raw, ok := options["targetSizeBytes"]
	if !ok || raw == nil {
		return 0, nil
	}

	invalid := func() (int64, *models.JobError) {
		return 0, &models.JobError{Code: validationCode, Message: "options.targetSizeBytes must be a whole number of bytes greater than zero"}
	}

	switch value := raw.(type) {
	case int:
		if value <= 0 {
			return invalid()
		}
		return int64(value), nil
	case int8:
		if value <= 0 {
			return invalid()
		}
		return int64(value), nil
	case int16:
		if value <= 0 {
			return invalid()
		}
		return int64(value), nil
	case int32:
		if value <= 0 {
			return invalid()
		}
		return int64(value), nil
	case int64:
		if value <= 0 {
			return invalid()
		}
		return value, nil
	case uint:
		if value == 0 {
			return invalid()
		}
		return videoFloatTargetSize(float64(value), validationCode)
	case uint8:
		if value == 0 {
			return invalid()
		}
		return int64(value), nil
	case uint16:
		if value == 0 {
			return invalid()
		}
		return int64(value), nil
	case uint32:
		if value == 0 {
			return invalid()
		}
		return int64(value), nil
	case uint64:
		return videoFloatTargetSize(float64(value), validationCode)
	case float32:
		return videoFloatTargetSize(float64(value), validationCode)
	case float64:
		return videoFloatTargetSize(value, validationCode)
	case json.Number:
		if integer, err := value.Int64(); err == nil {
			if integer <= 0 {
				return invalid()
			}
			return integer, nil
		}
		parsed, err := value.Float64()
		if err != nil {
			return invalid()
		}
		return videoFloatTargetSize(parsed, validationCode)
	default:
		return invalid()
	}
}

func videoFloatTargetSize(value float64, validationCode string) (int64, *models.JobError) {
	invalid := func() (int64, *models.JobError) {
		return 0, &models.JobError{Code: validationCode, Message: "options.targetSizeBytes must be a whole number of bytes greater than zero"}
	}
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 || value > float64(math.MaxInt64) {
		return invalid()
	}
	if math.Trunc(value) != value {
		return invalid()
	}
	converted := int64(value)
	if converted <= 0 {
		return invalid()
	}

	return converted, nil
}

// targetPlanSpec collects everything the plan warning needs to explain the
// strategy before the encode starts.
type targetPlanSpec struct {
	targetBytes      int64
	durationSeconds  float64
	durationKnown    bool
	resolution       string
	sourceHeight     int
	audioKbps        int
	forcedReencode   bool
	mergeCrossfade   bool
	overridesBitrate bool
}

// targetPlanWarnings explains the two-pass strategy, the quality floor and the
// best-effort promise. It never claims the target will be met.
func targetPlanWarnings(spec targetPlanSpec) []string {
	if spec.targetBytes <= 0 {
		return nil
	}

	floorKbps := engine.MinVideoBitrateKbps(spec.resolution, spec.sourceHeight)
	label := engine.FloorLabel(spec.resolution, spec.sourceHeight)
	warnings := []string{
		fmt.Sprintf(
			"Target of %s uses two-pass encoding with a %d kbps minimum video bitrate for %s; it is best effort and two passes take roughly twice as long as a single pass.",
			formatVideoBytes(spec.targetBytes),
			floorKbps,
			label,
		),
	}
	if spec.overridesBitrate {
		warnings = append(warnings, "targetSizeBytes overrides bitrateKbps; the video bitrate is computed from the target instead.")
	}
	if spec.forcedReencode {
		warnings = append(warnings, "copy mode cannot meet a size target; the plan switches to a two-pass re-encode.")
	}
	if spec.durationKnown {
		impliedKbps := engine.ComputeTargetVideoBitrateKbps(spec.targetBytes, spec.durationSeconds, spec.audioKbps)
		if impliedKbps < floorKbps {
			warnings = append(warnings, fmt.Sprintf(
				"The target implies %d kbps of video for %s, below the %d kbps floor; the floor is applied and the target may not be reachable.",
				impliedKbps,
				label,
				floorKbps,
			))
		}
	} else {
		warnings = append(warnings, "The real output duration could not be read at plan time; the target bitrate is computed at encode time and this estimate may change.")
	}
	if spec.mergeCrossfade {
		if spec.durationKnown {
			warnings = append(warnings, fmt.Sprintf(
				"The target is computed from the summed input duration (%s); crossfades shorten the real output, so the target is an approximation.",
				formatVideoDuration(spec.durationSeconds),
			))
		} else {
			warnings = append(warnings, "The target is computed from the summed input duration; crossfades shorten the real output and the input durations could not all be read, so the target is an approximation.")
		}
	}

	return warnings
}

// targetResultDetails exposes the honest measurement on the result item.
func targetResultDetails(outcome engine.TargetEncodeOutcome) map[string]any {
	if !outcome.TargetAttempted {
		return nil
	}

	return map[string]any{
		"targetSizeBytes":   outcome.TargetBytes,
		"actualSizeBytes":   outcome.ActualBytes,
		"targetBitrateKbps": outcome.BitrateKbps,
		"floorKbps":         outcome.FloorKbps,
		"floorApplied":      outcome.FloorApplied,
		"targetMet":         outcome.TargetMet,
		"attempts":          outcome.Attempts,
	}
}

// targetResultMessage reports the target result without pretending a miss was
// a success.
func targetResultMessage(outcome engine.TargetEncodeOutcome, verb string) string {
	if !outcome.TargetAttempted {
		return ""
	}

	message := fmt.Sprintf(
		"%s %s at %d kbps (target %s)",
		verb,
		formatVideoBytes(outcome.ActualBytes),
		outcome.BitrateKbps,
		formatVideoBytes(outcome.TargetBytes),
	)
	if outcome.FloorApplied {
		message += fmt.Sprintf("; %d kbps floor applied", outcome.FloorKbps)
	}
	if outcome.TargetMet {
		return message + "; target met"
	}

	return message + "; target not met (best effort)"
}

func applyTargetDetails(item *models.JobResultItem, outcome engine.TargetEncodeOutcome) {
	if !outcome.TargetAttempted {
		return
	}

	item.Details = targetResultDetails(outcome)
	item.Attempts = outcome.Attempts
	if outcome.Attempts > 1 {
		item.RetryCount = outcome.Attempts - 1
	}
}

// targetMediaForPath is the plan-time probe shared by the three tools.
func targetMediaForPath(ctx context.Context, inputPath string) (engine.TargetMedia, bool) {
	if strings.TrimSpace(inputPath) == "" {
		return engine.TargetMedia{}, false
	}

	return probeTargetMedia(ctx, inputPath)
}

// videoInt64Option reads an already-validated int64 option from a plan's
// effective options, tolerating the numeric shapes a JSON round trip produces.
func videoInt64Option(options map[string]any, key string) int64 {
	if options == nil {
		return 0
	}

	switch value := options[key].(type) {
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	case float32:
		return int64(value)
	case float64:
		return int64(value)
	case json.Number:
		if integer, err := value.Int64(); err == nil {
			return integer
		}
		if parsed, err := value.Float64(); err == nil {
			return int64(parsed)
		}
	}

	return 0
}

func formatVideoBytes(bytes int64) string {
	if bytes < 0 {
		bytes = 0
	}

	const (
		kib = 1024.0
		mib = kib * 1024.0
		gib = mib * 1024.0
	)

	switch {
	case float64(bytes) >= gib:
		return fmt.Sprintf("%.1f GB", float64(bytes)/gib)
	case float64(bytes) >= mib:
		return fmt.Sprintf("%.1f MB", float64(bytes)/mib)
	case float64(bytes) >= kib:
		return fmt.Sprintf("%.0f KB", float64(bytes)/kib)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}

func formatVideoDuration(seconds float64) string {
	if math.IsNaN(seconds) || math.IsInf(seconds, 0) || seconds <= 0 {
		return "unknown duration"
	}

	return fmt.Sprintf("%.1f s", seconds)
}
