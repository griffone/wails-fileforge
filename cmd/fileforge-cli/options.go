package main

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"
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

// commonToolOptions are accepted by every tool. Every tool writes relative to a
// directory, so outputDir is universal. outputPath is not: a tool whose planner
// never reads it must reject it instead of silently writing elsewhere, so it is
// listed per tool in toolOptionKeys.
var commonToolOptions = []string{"outputDir"}

// toolOptionKeys mirrors the option keys each tool planner reads. It exists so
// the CLI can reject a typo with TOOL_OPTION_UNKNOWN instead of letting the
// tool silently ignore it. Keep it in sync when a tool starts reading a new
// option.
var toolOptionKeys = map[string][]string{
	"tool.pdf.merge":          {"inputPageRanges", "outputPath"},
	"tool.pdf.split":          {"strategy", "ranges", "pagesPerFile"},
	"tool.pdf.crop":           {"cropPreset", "pageSelection", "margins", "outputPath"},
	"tool.pdf.optimize":       {"compress", "targetSizeBytes", "metadata", "outputPath"},
	"tool.pdf.ocr":            {"language", "dpi", "textSidecar", "outputPath"},
	"tool.pdf.images":         {"pageSize", "orientation", "columns", "rows", "marginMm", "gapMm", "fit", "slots", "outputPath"},
	"tool.pdf.to_docx":        {"outputPath"},
	"tool.pdf.rotate":         {"pageSelection", "angle"},
	"tool.pdf.remove_pages":   {"pageSelection"},
	"tool.pdf.reorder":        {"order"},
	"tool.pdf.extract_images": {"pageSelection"},
	"tool.doc.md_to_pdf":      {"pageSize", "orientation", "margins", "header", "footer", "outputPath"},
	"tool.doc.docx_to_pdf":    {"pageSelection", "pdfa", "outputPath"},

	"tool.image.convert":    {"format", "longestEdge", "width", "height", "quality", "rotate", "stripMetadata", "flipH", "flipV", "background", "outputPath"},
	"tool.image.crop":       {"x", "y", "width", "height", "ratioPreset", "outputWidth", "outputHeight", "rotate", "flipH", "flipV", "format", "outputPath"},
	"tool.image.annotate":   {"operations", "format", "outputPath"},
	"tool.image.compress":   {"targetSizeBytes", "format", "quality", "maxDimension", "allowDownscale", "stripMetadata", "background", "outputPath"},
	"tool.image.watermark":  {"kind", "text", "logoPath", "anchor", "color", "opacity", "bold", "tile", "scalePercent", "rotationDegrees", "marginPercent", "quality", "format", "outputPath"},
	"tool.image.ocr":        {"language", "dpi", "writePdf"},
	"tool.image.background": {"backgroundColor"},

	"tool.video.convert":    {"targetFormat", "qualityPreset", "resolution", "bitrateKbps", "removeAudio", "targetSizeBytes", "outputPath"},
	"tool.video.trim":       {"startTime", "endTime", "trimMode", "targetFormat", "qualityPreset", "targetSizeBytes", "outputPath"},
	"tool.video.merge":      {"mergeMode", "transition", "transitionDuration", "targetFormat", "qualityPreset", "removeAudio", "targetSizeBytes", "outputPath"},
	"tool.video.transcribe": {"language", "whisperPath", "modelPath", "formats"},

	"tool.audio.extract":   {"format", "bitrateKbps", "outputPath"},
	"tool.audio.convert":   {"format", "bitrateKbps", "outputPath"},
	"tool.audio.normalize": {"bitrateKbps", "targetLufs", "truePeak", "outputPath"},
}

// knownToolOptions returns the sorted allowed keys for a tool. found is false
// for a tool this catalog does not know yet; the caller then trusts the plan
// instead of rejecting an option it cannot classify.
func knownToolOptions(toolID string) (keys []string, found bool) {
	specific, found := toolOptionKeys[toolID]
	if !found {
		return nil, false
	}

	keys = append(keys, commonToolOptions...)
	keys = append(keys, specific...)
	sort.Strings(keys)

	return keys, true
}

// unknownToolOption returns the first option key the tool does not read, or ""
// when every key is allowed.
func unknownToolOption(toolID string, options map[string]any) (string, []string, bool) {
	keys, found := knownToolOptions(toolID)
	if !found {
		return "", nil, false
	}

	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		allowed[key] = struct{}{}
	}

	unknown := make([]string, 0, len(options))
	for key := range options {
		if _, ok := allowed[key]; !ok {
			unknown = append(unknown, key)
		}
	}
	if len(unknown) == 0 {
		return "", keys, true
	}
	sort.Strings(unknown)

	return unknown[0], keys, true
}
