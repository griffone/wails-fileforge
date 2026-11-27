package analysis

import (
	"context"
	"fmt"
	"math"
	"strings"
)

// Progress stage names reported through Request.OnProgress.
const (
	stageDecode = "decode"
	stageCoarse = "coarse"
	stageFine   = "fine"
	stageDone   = "done"
)

// Request is one alignment analysis.
type Request struct {
	// ReferencePath is the file that acts as the clock (the video's own audio).
	ReferencePath string
	// SourcePath is the file whose offset is measured.
	SourcePath string
	// MaxOffsetSeconds bounds the coarse search when positive. Zero searches
	// the full representable range.
	MaxOffsetSeconds float64
	// WindowSeconds is the length of each fine measuring window. Zero uses the
	// 10 second default; values are clamped to 1..60.
	WindowSeconds float64
	// OnProgress receives the current stage and an overall percentage 0..100.
	OnProgress func(stage string, percent float64)
}

// Window is one fine measurement around a reference-time anchor.
type Window struct {
	Label string
	// AnchorSeconds is the reference time at the centre of the window.
	AnchorSeconds float64
	// OffsetSeconds is the offset measured by this window alone.
	OffsetSeconds float64
	// Confidence is this window's own 0..1 measurement quality.
	Confidence float64
	// ResidualMs is OffsetSeconds - Result.OffsetSeconds, in milliseconds.
	ResidualMs float64
}

// Result is a finished analysis. OffsetSeconds is where the source's t = 0 sits
// on the reference timeline (see the package documentation). Confidence is 0..1
// and low confidence is a result, never an error.
type Result struct {
	OffsetSeconds float64
	Confidence    float64
	// DriftPPM is the fitted clock drift in parts per million. Nil when fewer
	// than two windows could be measured.
	DriftPPM *float64
	// Windows holds only the windows that produced a measurement.
	Windows []Window
	// Notes are human-readable warnings for the editor, in English.
	Notes []string

	ReferenceDurationSeconds float64
	SourceDurationSeconds    float64
}

// measuredWindow is the internal per-window measurement before the residual is
// known.
type measuredWindow struct {
	label         string
	anchorSeconds float64
	offsetSeconds float64
	confidence    float64
	usable        bool
}

var windowLabels = [...]string{"head", "middle", "tail"}

// windowAnchors are the fractions of the shared stretch measured by the three
// windows.
var windowAnchors = [...]float64{0.1, 0.5, 0.9}

// Analyze decodes both files once, finds the coarse offset from the 100 Hz RMS
// envelopes, refines it with three GCC-PHAT windows and reports the residual,
// the drift and a confidence. It never seeks inside either file.
func Analyze(ctx context.Context, runner Runner, req Request) (*Result, *Error) {
	if ctx == nil {
		ctx = context.Background()
	}

	referencePath := strings.TrimSpace(req.ReferencePath)
	sourcePath := strings.TrimSpace(req.SourcePath)
	if referencePath == "" || sourcePath == "" {
		return nil, &Error{Code: CodeInputInvalid, Message: "the reference and the source path are both required"}
	}
	if math.IsNaN(req.MaxOffsetSeconds) || math.IsInf(req.MaxOffsetSeconds, 0) || req.MaxOffsetSeconds < 0 {
		return nil, &Error{Code: CodeInputInvalid, Message: "the maximum offset must be zero or a positive number of seconds"}
	}
	windowSeconds, windowErr := normalizeWindowSeconds(req.WindowSeconds)
	if windowErr != nil {
		return nil, windowErr
	}

	report := func(stage string, percent float64) {
		if req.OnProgress != nil {
			req.OnProgress(stage, clampPercent(percent))
		}
	}

	report(stageDecode, 0)
	reference, analysisErr := DecodeMono(ctx, runner, referencePath)
	if analysisErr != nil {
		return nil, analysisErr
	}
	report(stageDecode, 25)

	source, analysisErr := DecodeMono(ctx, runner, sourcePath)
	if analysisErr != nil {
		return nil, analysisErr
	}
	report(stageDecode, 50)

	refDuration := reference.DurationSeconds()
	srcDuration := source.DurationSeconds()

	coarseOffset, _, _ := coarseOffsetSeconds(reference.Samples, source.Samples, req.MaxOffsetSeconds)
	report(stageCoarse, 70)

	sharedStart := math.Max(0, coarseOffset)
	sharedEnd := math.Min(refDuration, srcDuration+coarseOffset)
	sharedSpan := sharedEnd - sharedStart
	if sharedSpan < minSharedSeconds {
		return nil, &Error{
			Code:    CodeInsufficientOverlap,
			Message: fmt.Sprintf("the files share only %.2f s of audio, less than the %.0f s needed to measure an offset", sharedSpan, minSharedSeconds),
		}
	}
	windowSeconds = math.Min(windowSeconds, sharedSpan)

	measured, analysisErr := measureWindows(
		reference.Samples,
		source.Samples,
		coarseOffset,
		windowSeconds,
		sharedStart,
		sharedEnd,
		func(index int) { report(stageFine, 70+float64(index+1)/float64(len(windowAnchors))*25) },
	)
	if analysisErr != nil {
		return nil, analysisErr
	}

	result := &Result{
		ReferenceDurationSeconds: refDuration,
		SourceDurationSeconds:    srcDuration,
	}
	buildResult(result, measured, coarseOffset)
	report(stageDone, 100)

	return result, nil
}

// normalizeWindowSeconds applies the default and the supported bounds.
func normalizeWindowSeconds(value float64) (float64, *Error) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 {
		return 0, &Error{Code: CodeInputInvalid, Message: "the window length must be zero or a positive number of seconds"}
	}
	if value == 0 {
		return defaultWindowSeconds, nil
	}

	return math.Max(minWindowSeconds, math.Min(maxWindowSeconds, value)), nil
}

// coarseOffsetSeconds estimates the offset from the mean-removed 100 Hz RMS
// envelopes. The returned offset already uses the public sign convention.
func coarseOffsetSeconds(reference, source []int16, maxOffsetSeconds float64) (offset, peak, peakRatio float64) {
	refEnvelope := Envelope(reference, decodeSampleRate, envelopeHz)
	srcEnvelope := Envelope(source, decodeSampleRate, envelopeHz)
	meanRemove(refEnvelope)
	meanRemove(srcEnvelope)

	maxLagFrames := 0
	if maxOffsetSeconds > 0 {
		maxLagFrames = int(math.Round(maxOffsetSeconds * envelopeHz))
		if maxLagFrames < 1 {
			maxLagFrames = 1
		}
	}

	lagFrames, peak, peakRatio := CrossCorrelate(refEnvelope, srcEnvelope, maxLagFrames)
	// lagFrames is how much the source content is delayed; the public offset is
	// the position of the source's t = 0, which negates it.
	offset = -float64(lagFrames) / envelopeHz

	return offset, peak, peakRatio
}

// measureWindows runs the three fine windows over the shared stretch.
func measureWindows(
	refSamples, srcSamples []int16,
	coarseOffset, windowSeconds, sharedStart, sharedEnd float64,
	onWindow func(index int),
) ([]measuredWindow, *Error) {
	fineMaxLag := int(math.Round((float64(coarseFrameTolerance)/envelopeHz + fineSearchSeconds) * decodeSampleRate))
	count := int(math.Round(windowSeconds * decodeSampleRate))
	measured := make([]measuredWindow, 0, len(windowAnchors))

	for index, fraction := range windowAnchors {
		anchor := sharedStart + fraction*(sharedEnd-sharedStart)
		start := anchor - windowSeconds/2
		start = math.Max(sharedStart, math.Min(start, sharedEnd-windowSeconds))

		refStart := int(math.Round(start * decodeSampleRate))
		srcStart := int(math.Round((start - coarseOffset) * decodeSampleRate))
		refSlice := sliceSamples(refSamples, refStart, count)
		srcSlice := sliceSamples(srcSamples, srcStart, count)

		entry := measuredWindow{
			label:         windowLabels[index],
			anchorSeconds: start + windowSeconds/2,
		}
		if len(refSlice) >= 2 && len(srcSlice) >= 2 {
			lag, peak, peakRatio, _ := GCCPHAT(refSlice, srcSlice, decodeSampleRate, fineMaxLag)
			entry.offsetSeconds = coarseOffset - lag/decodeSampleRate
			entry.confidence = windowConfidence(peak, peakRatio)
			entry.usable = peak >= minWindowPeak && peakRatio >= minWindowPeakRatio
		}
		measured = append(measured, entry)

		if onWindow != nil {
			onWindow(index)
		}
	}

	return measured, nil
}

// windowConfidence combines the whitened peak height (does the correlation find
// anything?) with the peak-to-competitor ratio (is the answer unique?).
func windowConfidence(peak, peakRatio float64) float64 {
	coherence := clamp01(peak / 0.25)
	sharpness := clamp01((peakRatio - 1) / 2)

	return 0.55*coherence + 0.45*sharpness
}

// buildResult fills the offsets, residuals, drift, notes and confidence of
// result from the per-window measurements.
func buildResult(result *Result, measured []measuredWindow, coarseOffset float64) {
	var notes []string

	usable := make([]int, 0, len(measured))
	middleIndex := -1
	for index, entry := range measured {
		if entry.label == "middle" {
			middleIndex = index
		}
		if entry.usable {
			usable = append(usable, index)
		} else {
			notes = appendNote(notes, fmt.Sprintf("the %s window produced no clear peak", entry.label))
		}
	}

	middleUsable := middleIndex >= 0 && measured[middleIndex].usable
	forceZero := false
	switch {
	case middleUsable:
		result.OffsetSeconds = measured[middleIndex].offsetSeconds
	case len(usable) > 0:
		best := usable[0]
		for _, index := range usable[1:] {
			if measured[index].confidence > measured[best].confidence {
				best = index
			}
		}
		result.OffsetSeconds = measured[best].offsetSeconds
		forceZero = true
		notes = appendNote(notes, fmt.Sprintf("the middle window was weak; the offset comes from the %s window", measured[best].label))
	default:
		result.OffsetSeconds = coarseOffset
		forceZero = true
		notes = appendNote(notes, noteNoSharedSound)
	}

	samples := make([]offsetSample, 0, len(usable))
	measuredSpan := 0.0
	if len(usable) > 0 {
		first := measured[usable[0]].anchorSeconds
		last := measured[usable[len(usable)-1]].anchorSeconds
		measuredSpan = last - first
	}
	for _, index := range usable {
		samples = append(samples, offsetSample{
			anchorSeconds: measured[index].anchorSeconds,
			offsetSeconds: measured[index].offsetSeconds,
		})
	}

	slope, intercept, fitted := linearFit(samples)
	if fitted {
		// slope is seconds of offset per second of anchor time; parts per
		// million is the same ratio scaled by 1e6 (100 ppm = 0.36 ms per hour).
		drift := slope * 1e6
		result.DriftPPM = &drift

		if driftMilliseconds := slope * measuredSpan * 1000; math.Abs(driftMilliseconds) > driftNoticeMs {
			notes = appendNote(notes, fmt.Sprintf(
				"the source clock drifts about %+.1f ms over %.0f s (%+.0f ppm); sync long recordings in sections",
				driftMilliseconds, measuredSpan, drift,
			))
		}
	}

	maxDeviationMs := 0.0
	if fitted {
		for _, index := range usable {
			predicted := intercept + slope*measured[index].anchorSeconds
			deviation := math.Abs(measured[index].offsetSeconds-predicted) * 1000
			if deviation > maxDeviationMs {
				maxDeviationMs = deviation
			}
		}
	}

	baseConfidence := 0.0
	for _, index := range usable {
		baseConfidence += measured[index].confidence
	}
	if len(usable) > 0 {
		baseConfidence /= float64(len(usable))
	}
	confidence := baseConfidence * clamp01(1-maxDeviationMs/10)
	if maxDeviationMs > maxWindowDeviationMs {
		confidence = 0
		notes = appendNote(notes, noteDisagree)
	}
	if forceZero {
		confidence = 0
	}
	if len(usable) < 2 {
		// One window proves a peak exists but says nothing about agreement, so
		// the result cannot claim a fully verified measurement.
		confidence *= 0.6
		notes = appendNote(notes, "only one measuring window produced a peak; drift was not checked")
	}
	if confidence < 0.2 {
		notes = appendNote(notes, noteOffsetGuess)
	}
	if confidence < 0.5 {
		notes = appendNote(notes, noteCheckByEar)
	}
	result.Confidence = clamp01(confidence)

	result.Windows = make([]Window, 0, len(usable))
	for _, index := range usable {
		entry := measured[index]
		result.Windows = append(result.Windows, Window{
			Label:         entry.label,
			AnchorSeconds: entry.anchorSeconds,
			OffsetSeconds: entry.offsetSeconds,
			Confidence:    clamp01(entry.confidence),
			ResidualMs:    (entry.offsetSeconds - result.OffsetSeconds) * 1000,
		})
	}

	result.Notes = notes
}
