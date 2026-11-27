package analysis

import (
	"context"
	"math"
	"strings"
)

const (
	// minPeakBuckets and maxPeakBuckets bound the requested waveform resolution
	// so a caller cannot ask for a million-element payload.
	minPeakBuckets = 128
	maxPeakBuckets = 4000
)

// PeaksRequest asks for the min/max waveform of a visible window.
type PeaksRequest struct {
	Path         string
	StartSeconds float64
	// SpanSeconds is the requested window length. Zero means "to the end".
	SpanSeconds float64
	// Buckets is clamped to 128..4000.
	Buckets int
}

// PeaksResult is the bucketed waveform of one window. DurationSeconds is the
// absolute duration of the file, not of the window.
type PeaksResult struct {
	DurationSeconds float64
	StartSeconds    float64
	SpanSeconds     float64
	// HasAudio is false when the file has no audio stream; Min and Max are then
	// zero-filled and no error is returned.
	HasAudio bool
	Min      []float32
	Max      []float32
}

// Peaks decodes only the requested span at 1 kHz and reduces it to per-bucket
// minimum and maximum values in -1..1 for the editor's waveform lanes.
//
// This is the one place a seek is allowed: the waveform is not used for timing,
// so the ~20 ms packet bias of an input seek cannot influence the alignment.
func Peaks(ctx context.Context, runner Runner, req PeaksRequest) (*PeaksResult, *Error) {
	if strings.TrimSpace(req.Path) == "" {
		return nil, &Error{Code: CodeInputInvalid, Message: "an input path is required"}
	}
	if math.IsNaN(req.StartSeconds) || math.IsInf(req.StartSeconds, 0) || req.StartSeconds < 0 {
		return nil, &Error{Code: CodeInputInvalid, Message: "the start of the window must be zero or a positive number of seconds"}
	}
	if math.IsNaN(req.SpanSeconds) || math.IsInf(req.SpanSeconds, 0) || req.SpanSeconds < 0 {
		return nil, &Error{Code: CodeInputInvalid, Message: "the window length must be zero or a positive number of seconds"}
	}

	buckets := req.Buckets
	if buckets < minPeakBuckets {
		buckets = minPeakBuckets
	}
	if buckets > maxPeakBuckets {
		buckets = maxPeakBuckets
	}

	pcm, analysisErr := decodePCM(ctx, runner, req.Path, req.StartSeconds, req.SpanSeconds)
	if analysisErr != nil {
		if analysisErr.Code != CodeNoAudio {
			return nil, analysisErr
		}

		duration, _ := probeDurationSeconds(ctx, runner, strings.TrimSpace(req.Path))
		return &PeaksResult{
			DurationSeconds: duration,
			StartSeconds:    req.StartSeconds,
			SpanSeconds:     normalizeSpan(req.SpanSeconds, duration, req.StartSeconds),
			HasAudio:        false,
			Min:             make([]float32, buckets),
			Max:             make([]float32, buckets),
		}, nil
	}

	duration, ok := probeDurationSeconds(ctx, runner, strings.TrimSpace(req.Path))
	if !ok || duration <= 0 {
		// The container did not expose a duration; the decoded span still gives
		// the absolute end of what was read.
		duration = req.StartSeconds + pcm.DurationSeconds()
	}

	minValues := make([]float32, buckets)
	maxValues := make([]float32, buckets)
	reducePeaks(pcm.Samples, minValues, maxValues)

	return &PeaksResult{
		DurationSeconds: duration,
		StartSeconds:    req.StartSeconds,
		SpanSeconds:     normalizeSpan(req.SpanSeconds, duration, req.StartSeconds),
		HasAudio:        true,
		Min:             minValues,
		Max:             maxValues,
	}, nil
}

// normalizeSpan reports the window length actually shown: the request when it
// is positive, otherwise the remaining file.
func normalizeSpan(requested, duration, start float64) float64 {
	if requested > 0 {
		return requested
	}
	if duration <= 0 {
		return 0
	}

	return math.Max(0, duration-start)
}

// reducePeaks fills the per-bucket minimum and maximum of samples normalised to
// -1..1. Buckets that cover no samples stay at zero.
func reducePeaks(samples []int16, minValues, maxValues []float32) {
	buckets := len(minValues)
	count := len(samples)
	if buckets == 0 || count == 0 {
		return
	}

	const fullScale = 32768.0
	for bucket := 0; bucket < buckets; bucket++ {
		start := bucket * count / buckets
		end := (bucket + 1) * count / buckets
		if end > count {
			end = count
		}
		if start >= end {
			continue
		}

		low := 1.0
		high := -1.0
		for _, sample := range samples[start:end] {
			value := math.Max(-1, math.Min(1, float64(sample)/fullScale))
			if value < low {
				low = value
			}
			if value > high {
				high = value
			}
		}
		minValues[bucket] = float32(low)
		maxValues[bucket] = float32(high)
	}
}
