// Package analysis measures audio timing and waveform peaks for the sync
// editor. It decodes each file once to 1 kHz mono PCM with ffmpeg and then does
// every calculation in Go: RMS envelope, radix-2 FFT, cross-correlation,
// GCC-PHAT and the drift fit.
//
// The package is deterministic and offline-testable. All math works on plain
// slices, so the unit tests never need ffmpeg, and only the decode step talks
// to an external process through the small Runner seam.
//
// # Sign convention
//
// OffsetSeconds is the position of the source recording's t = 0 on the
// reference timeline, exactly as the media-sync-pack annex defines it:
//
//   - The source started 2 s after the reference: offset = +2 and
//     sourceTime = referenceTime - 2 (the source file is missing its head).
//   - The source started 40 s before the reference: offset = -40 and
//     sourceTime = referenceTime + 40 (the source file has extra head).
//
// Internally the correlation returns how much the source content is delayed
// relative to the reference; the reported offset is the negation of that delay.
package analysis

import "math"

const (
	// envelopeHz is the frame rate of the coarse RMS envelope. 10 ms frames
	// keep onsets and speech structure while shrinking an hour of audio to
	// 360 000 values.
	envelopeHz = 100

	// defaultWindowSeconds and its bounds size the three verification windows
	// at 1 kHz.
	defaultWindowSeconds = 10.0
	minWindowSeconds     = 1.0
	maxWindowSeconds     = 60.0

	// minSharedSeconds is the shortest overlap that can be measured. Below it
	// the fine windows would all be the same samples, so the result would be a
	// guess dressed as a measurement.
	minSharedSeconds = 1.0

	// coarseFrameTolerance is how many envelope frames the coarse peak can be
	// off by; fineSearchSeconds adds the guard band used by the GCC-PHAT pass.
	coarseFrameTolerance = 2
	fineSearchSeconds    = 0.2

	// minWindowPeak and minWindowPeakRatio decide whether a window produced a
	// measurement at all. A peak that does not stand clearly above its
	// competitor lag is not a measurement.
	minWindowPeak      = 0.03
	minWindowPeakRatio = 1.15

	// maxWindowDeviationMs is the disagreement budget between windows after
	// removing the fitted drift. Beyond it the offset is not trustworthy.
	maxWindowDeviationMs = 5.0

	// driftNoticeMs is the drift over the measured span that earns a note.
	driftNoticeMs = 33.0

	noteCheckByEar    = "confidence is low; check the sync by ear before exporting"
	noteOffsetGuess   = "no clear match was found; the offset is a guess"
	noteNoSharedSound = "no shared sound was found between the files"
	noteDisagree      = "the measuring windows disagree after removing drift; a cut or a non-linear clock jump is likely"
)

// nextPowerOfTwo returns the smallest power of two that is at least n.
func nextPowerOfTwo(n int) int {
	if n <= 1 {
		return 1
	}

	value := 1
	for value < n {
		value <<= 1
	}

	return value
}

// clamp01 bounds a value to 0..1 and maps NaN to 0.
func clamp01(value float64) float64 {
	if math.IsNaN(value) || value < 0 {
		return 0
	}
	if value > 1 {
		return 1
	}

	return value
}

// clampPercent bounds a progress value to 0..100 and maps NaN to 0.
func clampPercent(value float64) float64 {
	if math.IsNaN(value) || value < 0 {
		return 0
	}
	if value > 100 {
		return 100
	}

	return value
}

// meanRemove subtracts the mean of values in place so a cross-correlation is
// driven by shape and not by the envelope's DC level.
func meanRemove(values []float64) {
	if len(values) == 0 {
		return
	}

	var sum float64
	for _, value := range values {
		sum += value
	}
	mean := sum / float64(len(values))
	for index := range values {
		values[index] -= mean
	}
}

// sliceSamples returns up to count samples starting at start, clamped to the
// slice bounds. It never panics on edge windows.
func sliceSamples(samples []int16, start, count int) []int16 {
	if count <= 0 || len(samples) == 0 {
		return nil
	}
	if start < 0 {
		count += start
		start = 0
	}
	if start >= len(samples) || count <= 0 {
		return nil
	}
	end := start + count
	if end > len(samples) {
		end = len(samples)
	}

	return samples[start:end]
}

// offsetSample is one measured window used for the drift fit.
type offsetSample struct {
	anchorSeconds float64
	offsetSeconds float64
}

// linearFit returns the least-squares slope and intercept of offset versus
// anchor time. ok is false when the samples cannot define a line.
func linearFit(samples []offsetSample) (slope, intercept float64, ok bool) {
	if len(samples) < 2 {
		return 0, 0, false
	}

	count := float64(len(samples))
	var meanX, meanY float64
	for _, sample := range samples {
		meanX += sample.anchorSeconds
		meanY += sample.offsetSeconds
	}
	meanX /= count
	meanY /= count

	var numerator, denominator float64
	for _, sample := range samples {
		deltaX := sample.anchorSeconds - meanX
		numerator += deltaX * (sample.offsetSeconds - meanY)
		denominator += deltaX * deltaX
	}
	if denominator <= 0 {
		return 0, 0, false
	}

	slope = numerator / denominator
	intercept = meanY - slope*meanX

	return slope, intercept, true
}

// appendNote adds a note once, keeping the order stable for deterministic
// results.
func appendNote(notes []string, note string) []string {
	for _, existing := range notes {
		if existing == note {
			return notes
		}
	}

	return append(notes, note)
}
