package jobs

import (
	"encoding/json"
	"math"
	"strconv"
	"strings"
)

// RequestedTargetBytes reports the positive target size a job requested through
// the targetSizeBytes option. It returns false when the job is unknown, did not
// request a target, or supplied a value that is not a usable byte count.
//
// The request is immutable after submission, so reading it from the live job
// state needs no additional locking beyond the engine map lookup.
func (e *Engine) RequestedTargetBytes(jobID string) (int64, bool) {
	state := e.lookup(strings.TrimSpace(jobID))
	if state == nil {
		return 0, false
	}

	return parseTargetBytes(state.request.Options["targetSizeBytes"])
}

// parseTargetBytes accepts every numeric shape the option can take: JSON
// numbers (float64), Go integers, json.Number, and numeric strings. Values that
// are non-positive, non-finite, fractional after truncation, or too large to
// represent are rejected so a malformed option never becomes a bogus target.
func parseTargetBytes(value any) (int64, bool) {
	switch typed := value.(type) {
	case nil:
		return 0, false
	case float64:
		return targetBytesFromFloat(typed)
	case float32:
		return targetBytesFromFloat(float64(typed))
	case int:
		return positiveInt64(int64(typed))
	case int8:
		return positiveInt64(int64(typed))
	case int16:
		return positiveInt64(int64(typed))
	case int32:
		return positiveInt64(int64(typed))
	case int64:
		return positiveInt64(typed)
	case uint:
		return targetBytesFromUint64(uint64(typed))
	case uint8:
		return positiveInt64(int64(typed))
	case uint16:
		return positiveInt64(int64(typed))
	case uint32:
		return positiveInt64(int64(typed))
	case uint64:
		return targetBytesFromUint64(typed)
	case json.Number:
		if parsed, err := typed.Int64(); err == nil {
			return positiveInt64(parsed)
		}
		if parsed, err := typed.Float64(); err == nil {
			return targetBytesFromFloat(parsed)
		}
		return 0, false
	case string:
		return parseTargetBytesString(typed)
	default:
		return 0, false
	}
}

func parseTargetBytesString(raw string) (int64, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return 0, false
	}

	if parsed, err := strconv.ParseInt(trimmed, 10, 64); err == nil {
		return positiveInt64(parsed)
	}
	if parsed, err := strconv.ParseFloat(trimmed, 64); err == nil {
		return targetBytesFromFloat(parsed)
	}

	return 0, false
}

func targetBytesFromFloat(value float64) (int64, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, false
	}
	// Guard the conversion: float64(math.MaxInt64) rounds up, so a strict
	// comparison keeps the int64 conversion in range.
	if value >= float64(math.MaxInt64) {
		return 0, false
	}

	return int64(value), true
}

func targetBytesFromUint64(value uint64) (int64, bool) {
	if value == 0 || value > math.MaxInt64 {
		return 0, false
	}

	return int64(value), true
}

func positiveInt64(value int64) (int64, bool) {
	if value <= 0 {
		return 0, false
	}

	return value, true
}
