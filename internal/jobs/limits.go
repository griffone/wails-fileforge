package jobs

import (
	"runtime"
	"strings"
)

// Class is the resource profile a job consumes. The scheduler keeps an
// independent capacity for every class so heavy external processes do not
// starve cheap CPU work and vice versa.
type Class string

const (
	ClassCPU      Class = "cpu"
	ClassExternal Class = "external"
	ClassAudio    Class = "audio"
	ClassIO       Class = "io"
)

// Priority orders pending jobs. Lower values are scheduled first.
type Priority int

const (
	PriorityInteractive Priority = iota
	PriorityAutomation
	PriorityPreview
)

// Limits holds the global and per-class concurrency caps. Total bounds the
// number of running jobs across every class.
type Limits struct {
	Total    int
	CPU      int
	External int
	Audio    int
	IO       int
}

// DefaultLimits derives limits from the host CPU count:
//
//	Total    = clamp(NumCPU, 2, 6)
//	CPU      = clamp(NumCPU/2, 1, 4)
//	External = 1
//	Audio    = clamp(NumCPU/2, 1, 2)
//	IO       = 2
//
// Total is raised when needed so it is never smaller than any class capacity.
func DefaultLimits() Limits {
	cpus := runtime.NumCPU()
	limits := Limits{
		Total:    clampInt(cpus, 2, 6),
		CPU:      clampInt(cpus/2, 1, 4),
		External: 1,
		Audio:    clampInt(cpus/2, 1, 2),
		IO:       2,
	}
	limits.Total = max(limits.Total, limits.CPU, limits.External, limits.Audio, limits.IO)

	return limits
}

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}

	return value
}

// normalizeLimits fills zero fields from DefaultLimits and enforces
// Total >= max(CPU, External, IO). A fully zero value becomes DefaultLimits.
func normalizeLimits(limits Limits) Limits {
	defaults := DefaultLimits()
	if limits == (Limits{}) {
		return defaults
	}

	if limits.Total <= 0 {
		limits.Total = defaults.Total
	}
	if limits.CPU <= 0 {
		limits.CPU = defaults.CPU
	}
	if limits.External <= 0 {
		limits.External = defaults.External
	}
	if limits.Audio <= 0 {
		limits.Audio = defaults.Audio
	}
	if limits.IO <= 0 {
		limits.IO = defaults.IO
	}
	limits.Total = max(limits.Total, limits.CPU, limits.External, limits.Audio, limits.IO)

	return limits
}

// capacity returns the running cap for a class. Unknown classes use the CPU
// capacity so a misconfigured class never disables execution.
func (l Limits) capacity(class Class) int {
	switch class {
	case ClassExternal:
		return max(1, l.External)
	case ClassAudio:
		return max(1, l.Audio)
	case ClassIO:
		return max(1, l.IO)
	default:
		return max(1, l.CPU)
	}
}

// classForTool maps a stable tool ID to its resource class. The mapping is
// intentionally conservative: tools that spawn an external process (ffmpeg,
// tesseract, Ghostscript) or run a bundled Python engine are External, PDF
// rewrite work is CPU, and unknown tools default to CPU.
func classForTool(toolID string) Class {
	id := strings.ToLower(strings.TrimSpace(toolID))

	switch {
	case id == "tool.pdf.ocr", id == "tool.pdf.to_docx":
		return ClassExternal
	case id == "tool.pdf.merge", id == "tool.pdf.split", id == "tool.pdf.crop", id == "tool.pdf.optimize", id == "tool.pdf.images",
		id == "tool.pdf.rotate", id == "tool.pdf.remove_pages", id == "tool.pdf.reorder", id == "tool.pdf.extract_images":
		return ClassCPU
	case strings.HasPrefix(id, "tool.image."):
		return ClassCPU
	case strings.HasPrefix(id, "tool.video."):
		return ClassExternal
	case strings.HasPrefix(id, "tool.audio."):
		// Audio encodes run in ffmpeg but are much lighter than video; a small
		// dedicated class keeps two audio jobs moving without competing with
		// video and document renders for the single external slot.
		return ClassAudio
	case strings.HasPrefix(id, "tool.doc."):
		return ClassExternal
	default:
		return ClassCPU
	}
}

// ClassForTool exposes classForTool for callers that plan work outside the
// engine (recipes, preview) and for tests in other packages.
func ClassForTool(toolID string) Class {
	return classForTool(toolID)
}
