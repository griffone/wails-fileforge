package pdf

import "strings"

// Helpers for reading the resolved options stored on a plan. They let an
// executor run exactly what Plan resolved instead of re-deriving values from
// the request.

// planBoolOption reads a boolean effective option and reports whether the plan
// supplied a usable value.
func planBoolOption(options map[string]any, key string) (bool, bool) {
	if options == nil {
		return false, false
	}

	switch value := options[key].(type) {
	case bool:
		return value, true
	case string:
		if strings.EqualFold(strings.TrimSpace(value), "true") {
			return true, true
		}
		if strings.EqualFold(strings.TrimSpace(value), "false") {
			return false, true
		}
	}

	return false, false
}

// planInt64Option reads a whole-number effective option without dropping the
// int64 range used by byte-sized values.
func planInt64Option(options map[string]any, key string) (int64, bool) {
	if options == nil {
		return 0, false
	}

	switch value := options[key].(type) {
	case int:
		return int64(value), true
	case int32:
		return int64(value), true
	case int64:
		return value, true
	case float64:
		if value != float64(int64(value)) {
			return 0, false
		}
		return int64(value), true
	default:
		return 0, false
	}
}
