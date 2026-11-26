package main

import (
	"encoding/json"
	"fmt"
	"strings"

	"fileforge-desktop/internal/tooloptions"
)

// stringList is a repeatable flag value.
type stringList []string

func (s *stringList) String() string {
	return strings.Join(*s, ",")
}

func (s *stringList) Set(value string) error {
	*s = append(*s, value)
	return nil
}

// parseOptions turns repeated key=value pairs into job or recipe options. A
// value is decoded as JSON when it is valid JSON, so `--option quality=80`
// becomes a number and `--option compress=true` a boolean. Everything else,
// including `--option strategy=ranges`, stays a string.
func parseOptions(raw []string) (map[string]any, error) {
	if len(raw) == 0 {
		return nil, nil
	}

	options := make(map[string]any, len(raw))
	for _, item := range raw {
		key, value, found := strings.Cut(item, "=")
		key = strings.TrimSpace(key)
		if !found || key == "" {
			return nil, fmt.Errorf("invalid --option %q: expected key=value", item)
		}
		options[key] = coerceOptionValueForKey(key, value)
	}

	return options, nil
}

// stringOptionKeys are option keys whose value is a string even when the text
// looks like a number. Without this, `--option pageSelection=1` would become a
// JSON number and a tool that reads a string option would silently ignore it.
var stringOptionKeys = map[string]struct{}{
	"anchor":          {},
	"background":      {},
	"backgroundColor": {},
	"color":           {},
	"cropPreset":      {},
	"fit":             {},
	"format":          {},
	"kind":            {},
	"language":        {},
	"logoPath":        {},
	"mergeMode":       {},
	"modelPath":       {},
	"order":           {},
	"orientation":     {},
	"outputDir":       {},
	"outputPath":      {},
	"pageSelection":   {},
	"pageSize":        {},
	"qualityPreset":   {},
	"range":           {},
	"ranges":          {},
	"ratioPreset":     {},
	"resolution":      {},
	"strategy":        {},
	"targetFormat":    {},
	"text":            {},
	"transition":      {},
	"trimMode":        {},
	"whisperPath":     {},
}

func coerceOptionValueForKey(key, raw string) any {
	if _, isString := stringOptionKeys[key]; isString {
		return raw
	}

	return coerceOptionValue(raw)
}

// coerceOptionValue decodes a raw flag value as JSON when possible. A literal
// null is kept as the original text so it can never erase an option silently.
func coerceOptionValue(raw string) any {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return raw
	}

	var decoded any
	if err := json.Unmarshal([]byte(trimmed), &decoded); err == nil && decoded != nil {
		return decoded
	}

	return raw
}

// knownToolOptions returns the sorted allowed keys for a tool. It delegates to
// the shared catalog so the CLI and the MCP server cannot disagree.
func knownToolOptions(toolID string) (keys []string, found bool) {
	return tooloptions.KnownKeys(toolID)
}

// unknownToolOption returns the first option key the tool does not read, or ""
// when every key is allowed.
func unknownToolOption(toolID string, options map[string]any) (string, []string, bool) {
	return tooloptions.UnknownKey(toolID, options)
}
