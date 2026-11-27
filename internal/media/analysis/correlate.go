package analysis

import (
	"math"
	"math/cmplx"
)

const (
	// correlationExclusionSamples is how close a competitor peak may sit to the
	// winner before it is treated as the same main lobe.
	correlationExclusionSamples = 2

	// maxPeakRatio caps peak/secondPeak so a perfectly clean signal reports a
	// finite number instead of +Inf.
	maxPeakRatio = 100.0

	// minOverlapSamples and minOverlapDivisor keep a lag that only compares a
	// handful of edge samples from winning. The divisor scales the guard with
	// the signal length.
	minOverlapSamples = 8
	minOverlapDivisor = 50
)

// CrossCorrelate correlates two real signals over the lag range
// maxLag <= lag <= maxLag and returns the winning lag in samples, the peak
// (a normalised correlation coefficient in 0..1) and peakRatio (peak divided by
// the strongest competitor outside the main lobe, capped at 100).
//
// The correlation is linear, not circular: both inputs are zero-padded to a
// power of two that covers len(ref)+len(src)-1. Every candidate lag is
// normalised by the total energy of both signals, so all lags share one scale,
// and lags that compare fewer than max(8, shorter/50) samples are ignored.
//
// A negative lag means the source content is earlier than the reference; the
// reported offset in Result is the negation of that delay (see the package
// documentation). maxLag <= 0 means the full representable range. Callers
// should remove the mean first: the functions here never rescale the input.
func CrossCorrelate(ref, src []float64, maxLag int) (lag int, peak, peakRatio float64) {
	if len(ref) == 0 || len(src) == 0 {
		return 0, 0, 0
	}

	negativeLimit := len(ref) - 1
	positiveLimit := len(src) - 1
	if maxLag > 0 {
		negativeLimit = min(negativeLimit, maxLag)
		positiveLimit = min(positiveLimit, maxLag)
	}

	transformSize := nextPowerOfTwo(len(ref) + len(src) - 1)
	paddedRef := make([]complex128, transformSize)
	paddedSrc := make([]complex128, transformSize)
	for index, value := range ref {
		paddedRef[index] = complex(value, 0)
	}
	for index, value := range src {
		paddedSrc[index] = complex(value, 0)
	}

	FFT(paddedRef, false)
	FFT(paddedSrc, false)
	for index := range paddedRef {
		// conj(R) * S transforms back into sum(ref[i] * src[i+lag]).
		paddedSrc[index] = cmplx.Conj(paddedRef[index]) * paddedSrc[index]
	}
	FFT(paddedSrc, true)

	var totalRef, totalSrc float64
	for _, value := range ref {
		totalRef += value * value
	}
	for _, value := range src {
		totalSrc += value * value
	}
	if totalRef <= 0 || totalSrc <= 0 {
		return 0, 0, 0
	}
	normalizer := math.Sqrt(totalRef * totalSrc)
	minOverlap := minOverlapSamples
	if scaled := min(len(ref), len(src)) / minOverlapDivisor; scaled > minOverlap {
		minOverlap = scaled
	}

	// The score is the correlation normalised by the *total* energy of both
	// signals, so every lag is measured on the same scale. Normalising by the
	// energy of the overlap instead would let a lag that compares eight edge
	// samples report a perfect coefficient.
	score := func(candidate int) float64 {
		if overlapCount(candidate, len(ref), len(src)) < minOverlap {
			return -1
		}

		return math.Abs(real(paddedSrc[correlationIndex(candidate, transformSize)])) / normalizer
	}

	bestScore := -1.0
	bestLag := 0
	for candidate := -negativeLimit; candidate <= positiveLimit; candidate++ {
		if value := score(candidate); value > bestScore {
			bestScore = value
			bestLag = candidate
		}
	}
	if bestScore < 0 {
		return 0, 0, 0
	}

	// The competitor is the strongest *local maximum* outside the main lobe. A
	// shoulder of the winning lobe is not a competing peak: for a smoothed
	// envelope the lobe is several frames wide, so an adjacent sample would
	// make every real match look ambiguous.
	competitor := 0.0
	for candidate := -negativeLimit; candidate <= positiveLimit; candidate++ {
		if candidate-bestLag <= correlationExclusionSamples && bestLag-candidate <= correlationExclusionSamples {
			continue
		}
		value := score(candidate)
		if value < 0 || value <= score(candidate-1) || value < score(candidate+1) {
			continue
		}
		if value > competitor {
			competitor = value
		}
	}

	peakRatio = maxPeakRatio
	if competitor > 1e-12 {
		peakRatio = math.Min(maxPeakRatio, bestScore/competitor)
	}

	return bestLag, bestScore, peakRatio
}

// GCCPHAT measures the delay between two int16 signals with a generalized
// cross-correlation phase transform: the cross-spectrum is whitened to unit
// magnitude, which turns a broad correlation peak into a sharp one and keeps
// the estimate usable with different microphones and reverberant rooms.
//
// It returns the lag in samples (fractional, refined by parabolic interpolation
// around the peak), the normalised peak height (0..1), the peak-to-second-peak
// ratio and the raw second peak. A negative lag means src content is earlier
// than ref content; the caller negates it for the Result offset.
//
// maxLag <= 0 searches the full range. Internal buffers are zero-padded so the
// correlation is linear even though it is computed with an FFT.
func GCCPHAT(ref, src []int16, sampleRate, maxLag int) (fractionalLag, peak, peakRatio, secondPeak float64) {
	if len(ref) == 0 || len(src) == 0 {
		return 0, 0, 0, 0
	}

	transformSize := nextPowerOfTwo(len(ref) + len(src) - 1)
	refSpectrum := make([]complex128, transformSize)
	srcSpectrum := make([]complex128, transformSize)
	for index, value := range ref {
		refSpectrum[index] = complex(float64(value), 0)
	}
	for index, value := range src {
		srcSpectrum[index] = complex(float64(value), 0)
	}

	FFT(refSpectrum, false)
	FFT(srcSpectrum, false)

	var maxMagnitude float64
	for index := range refSpectrum {
		cross := cmplx.Conj(refSpectrum[index]) * srcSpectrum[index]
		srcSpectrum[index] = cross
		if magnitude := cmplx.Abs(cross); magnitude > maxMagnitude {
			maxMagnitude = magnitude
		}
	}
	if maxMagnitude <= 0 {
		return 0, 0, 0, 0
	}

	// Bins without meaningful energy are zeroed instead of whitened; whitening
	// a numerical zero would amplify noise into a full-amplitude tap.
	epsilon := maxMagnitude * 1e-6
	for index := range srcSpectrum {
		magnitude := cmplx.Abs(srcSpectrum[index])
		if magnitude > epsilon {
			srcSpectrum[index] = srcSpectrum[index] / complex(magnitude, 0)
		} else {
			srcSpectrum[index] = 0
		}
	}

	FFT(srcSpectrum, true)

	limit := maxLag
	maxValid := min(len(ref), len(src)) - 1
	if limit <= 0 || limit > maxValid {
		limit = maxValid
	}
	if limit < 0 {
		limit = 0
	}

	valueAt := func(candidate int) float64 {
		return math.Abs(real(srcSpectrum[correlationIndex(candidate, transformSize)]))
	}

	bestLag := 0
	bestValue := -1.0
	for candidate := -limit; candidate <= limit; candidate++ {
		if value := valueAt(candidate); value > bestValue {
			bestValue = value
			bestLag = candidate
		}
	}

	exclusion := sampleRate / 500
	if exclusion < correlationExclusionSamples {
		exclusion = correlationExclusionSamples
	}
	// Only a local maximum can be a competing peak; a slope of the winning
	// lobe would otherwise masquerade as ambiguity.
	secondPeak = 0
	for candidate := -limit; candidate <= limit; candidate++ {
		if candidate-bestLag <= exclusion && bestLag-candidate <= exclusion {
			continue
		}
		value := valueAt(candidate)
		if value <= valueAt(candidate-1) || value < valueAt(candidate+1) {
			continue
		}
		if value > secondPeak {
			secondPeak = value
		}
	}

	peakRatio = maxPeakRatio
	if secondPeak > 1e-12 {
		peakRatio = math.Min(maxPeakRatio, bestValue/secondPeak)
	}

	// Parabolic interpolation of the three samples around the peak recovers a
	// sub-sample lag for a band-limited signal.
	fraction := 0.0
	left := valueAt(bestLag - 1)
	right := valueAt(bestLag + 1)
	if denominator := left - 2*bestValue + right; math.Abs(denominator) > 1e-12 {
		fraction = 0.5 * (left - right) / denominator
		fraction = math.Max(-0.5, math.Min(0.5, fraction))
	}

	return float64(bestLag) + fraction, bestValue, peakRatio, secondPeak
}

// correlationIndex maps a signed lag onto the FFT output index.
func correlationIndex(candidate, transformSize int) int {
	if candidate >= 0 {
		return candidate
	}

	return transformSize + candidate
}

// overlapCount returns how many samples the two signals share at a lag.
func overlapCount(candidate, refLen, srcLen int) int {
	if candidate >= 0 {
		if candidate >= srcLen {
			return 0
		}

		return min(refLen, srcLen-candidate)
	}

	shift := -candidate
	if shift >= refLen {
		return 0
	}

	return min(refLen-shift, srcLen)
}
