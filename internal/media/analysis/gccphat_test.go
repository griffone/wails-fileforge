package analysis

import (
	"math"
	"math/rand"
	"testing"
)

func TestGCCPHATKnownLags(t *testing.T) {
	t.Parallel()

	reference := synthSpeech(41, 4, 1000)

	tests := []struct {
		name   string
		source []int16
		want   float64
	}{
		{
			name:   "delayed copy",
			source: prependSilence(reference, 123),
			want:   123,
		},
		{
			name:   "advanced copy",
			source: trimHead(reference, 173),
			want:   -173,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			lag, peak, ratio, _ := GCCPHAT(reference, test.source, 1000, 400)
			if math.Abs(lag-test.want) > 0.5 {
				t.Fatalf("lag = %g, want %g", lag, test.want)
			}
			if peak < 0.5 {
				t.Fatalf("peak = %g, want at least 0.5", peak)
			}
			if ratio < 3 {
				t.Fatalf("peak ratio = %g, want a clear peak", ratio)
			}
		})
	}
}

func TestGCCPHATFractionalLag(t *testing.T) {
	t.Parallel()

	reference := toneSignal(2048, 0)
	halfSampleShifted := toneSignal(2048, 0.5)

	lag, peak, _, _ := GCCPHAT(reference, halfSampleShifted, 1000, 8)
	if math.Abs(lag-0.5) > 1 {
		t.Fatalf("lag = %g, want about 0.5", lag)
	}
	if peak < 0.5 {
		t.Fatalf("peak = %g, want at least 0.5", peak)
	}
}

func TestGCCPHATNoisyReverberant(t *testing.T) {
	t.Parallel()

	reference := synthSpeech(43, 4, 1000)
	source := mixDelayed(reference, []delayedPart{
		{delay: 200, gain: 1},
		{delay: 120, gain: 0.25}, // reflection 80 samples before the direct path, -12 dB
	}, 0.05)

	lag, peak, ratio, secondPeak := GCCPHAT(reference, source, 1000, 400)
	if math.Abs(lag-200) > 1 {
		t.Fatalf("lag = %g, want about 200", lag)
	}
	if peak < 0.3 {
		t.Fatalf("peak = %g, want at least 0.3", peak)
	}
	if ratio < 2 {
		t.Fatalf("peak ratio = %g, want the direct path above the reflection", ratio)
	}
	if secondPeak >= peak {
		t.Fatalf("second peak %g is not below the main peak %g", secondPeak, peak)
	}
}

func TestGCCPHATEmptyInputs(t *testing.T) {
	t.Parallel()

	lag, peak, ratio, second := GCCPHAT(nil, nil, 1000, 10)
	if lag != 0 || peak != 0 || ratio != 0 || second != 0 {
		t.Fatalf("expected a zero result, got %g %g %g %g", lag, peak, ratio, second)
	}
}

// toneSignal builds a deterministic sum of incommensurate tones with an exact
// fractional delay, which gives the PHAT stage a sub-sample ground truth.
func toneSignal(count int, delaySamples float64) []int16 {
	frequencies := []float64{37.3, 111.7, 199.1, 233.3, 311.9}
	phases := []float64{0.3, 1.7, 2.4, 4.1, 5.3}
	levels := []float64{0.25, 0.2, 0.18, 0.15, 0.12}

	samples := make([]int16, count)
	for index := range samples {
		moment := (float64(index) - delaySamples) / 1000
		var value float64
		for tone, frequency := range frequencies {
			value += levels[tone] * math.Sin(2*math.Pi*frequency*moment+phases[tone])
		}
		samples[index] = int16(math.Max(-1, math.Min(1, value)) * 32000)
	}

	return samples
}

// delayedPart is one delayed copy used to fake a reverberant recording.
type delayedPart struct {
	delay int
	gain  float64
}

// mixDelayed sums delayed copies of base with optional white noise.
func mixDelayed(base []int16, parts []delayedPart, noise float64) []int16 {
	length := 0
	for _, part := range parts {
		length = max(length, part.delay+len(base))
	}
	output := make([]int16, length)
	for index := range output {
		output[index] = 0
	}

	mixed := make([]float64, length)
	for _, part := range parts {
		for index, sample := range base {
			mixed[part.delay+index] += part.gain * float64(sample)
		}
	}
	if noise > 0 {
		random := rand.New(rand.NewSource(44))
		for index := range mixed {
			mixed[index] += noise * 32768 * (random.Float64()*2 - 1)
		}
	}
	for index, value := range mixed {
		output[index] = int16(math.Max(-32768, math.Min(32767, value)))
	}

	return output
}
