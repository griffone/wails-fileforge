// Package tooloptions is the single source of truth for the option keys each
// tool planner reads.
//
// The headless CLI and the local MCP server both reject an option a tool does
// not read instead of letting the planner silently ignore it. Keeping the map in
// one package means the two entry points cannot drift apart.
package tooloptions

import "sort"

// common are accepted by every tool. Every tool writes relative to a
// directory, so outputDir is universal. outputPath is not: a tool whose planner
// never reads it must reject it instead of silently writing elsewhere, so it is
// listed per tool in toolKeys.
var common = []string{"outputDir"}

// toolKeys mirrors the option keys each tool planner reads. Keep it in sync
// when a tool starts reading a new option.
var toolKeys = map[string][]string{
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

// KnownKeys returns the sorted allowed keys for a tool. found is false for a
// tool this catalog does not know yet; the caller then trusts the plan instead
// of rejecting an option it cannot classify.
func KnownKeys(toolID string) (keys []string, found bool) {
	specific, found := toolKeys[toolID]
	if !found {
		return nil, false
	}

	keys = append(keys, common...)
	keys = append(keys, specific...)
	sort.Strings(keys)

	return keys, true
}

// UnknownKey returns the first option key the tool does not read, or "" when
// every key is allowed. found is false for an uncatalogued tool.
func UnknownKey(toolID string, options map[string]any) (unknown string, keys []string, found bool) {
	keys, found = KnownKeys(toolID)
	if !found {
		return "", nil, false
	}

	allowed := make(map[string]struct{}, len(keys))
	for _, key := range keys {
		allowed[key] = struct{}{}
	}

	unknownKeys := make([]string, 0, len(options))
	for key := range options {
		if _, ok := allowed[key]; !ok {
			unknownKeys = append(unknownKeys, key)
		}
	}
	if len(unknownKeys) == 0 {
		return "", keys, true
	}
	sort.Strings(unknownKeys)

	return unknownKeys[0], keys, true
}
