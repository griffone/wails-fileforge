package analysis

import (
	"math"
	"math/rand"
	"testing"
)

func TestCrossCorrelateKnownLags(t *testing.T) {
	t.Parallel()

	const count = 512
	// Uncorrelated noise has a one-sample autocorrelation lobe, so the test
	// measures the lag finder and not the width of a smoothed envelope.
	reference := whiteNoise(11, count)
	meanRemove(reference)

	tests := []struct {
		name        string
		source      []float64
		wantLag     int
		wantMinPeak float64
	}{
		{
			name:        "source delayed by prepending silence",
			source:      padFloat(reference, 37, 0),
			wantLag:     37,
			wantMinPeak: 0.99,
		},
		{
			name:        "source advanced by trimming the head",
			source:      reference[23:],
			wantLag:     -23,
			wantMinPeak: 0.9,
		},
		{
			name:        "source trimmed near the end",
			source:      reference[450:],
			wantLag:     -450,
			wantMinPeak: 0.2,
		},
		{
			name:        "source padded near the end",
			source:      padFloat(reference[:62], 450, 0),
			wantLag:     450,
			wantMinPeak: 0.2,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			lag, peak, ratio := CrossCorrelate(reference, test.source, 0)
			if lag != test.wantLag {
				t.Fatalf("lag = %d, want %d", lag, test.wantLag)
			}
			if peak < test.wantMinPeak {
				t.Fatalf("peak = %g, want at least %g", peak, test.wantMinPeak)
			}
			if ratio < 2 {
				t.Fatalf("peak ratio = %g, want a high ratio for an exact match", ratio)
			}
		})
	}
}

func TestCrossCorrelateMaxLag(t *testing.T) {
	t.Parallel()

	const count = 256
	reference := whiteNoise(21, count)
	source := padFloat(reference, 40, 0)

	lag, _, _ := CrossCorrelate(reference, source, 10)
	if lag < -10 || lag > 10 {
		t.Fatalf("lag = %d, want it inside the requested +-10 range", lag)
	}
}

func TestCrossCorrelateUnrelatedNoise(t *testing.T) {
	t.Parallel()

	reference := smoothedNoise(31, 1024)
	source := smoothedNoise(32, 1024)
	meanRemove(reference)
	meanRemove(source)

	_, peak, ratio := CrossCorrelate(reference, source, 0)
	if ratio >= 2 {
		t.Fatalf("peak ratio = %g for unrelated noise, want below 2", ratio)
	}
	if peak >= 0.4 {
		t.Fatalf("peak = %g for unrelated noise, want a low peak", peak)
	}
}

func TestCrossCorrelateConstantSignals(t *testing.T) {
	t.Parallel()

	// Without mean removal a constant signal only correlates with itself at
	// every lag; the function must still return a finite, bounded answer.
	reference := make([]float64, 64)
	source := make([]float64, 64)
	for index := range reference {
		reference[index] = 1
		source[index] = 1
	}

	lag, peak, ratio := CrossCorrelate(reference, source, 0)
	if lag != 0 {
		t.Fatalf("lag = %d, want 0", lag)
	}
	if math.Abs(peak-1) > 1e-9 {
		t.Fatalf("peak = %g, want 1", peak)
	}
	if ratio > maxPeakRatio {
		t.Fatalf("ratio = %g exceeds the cap %g", ratio, maxPeakRatio)
	}
}

// whiteNoise is a deterministic, uncorrelated signal.
func whiteNoise(seed int64, count int) []float64 {
	random := rand.New(rand.NewSource(seed))
	values := make([]float64, count)
	for index := range values {
		values[index] = random.Float64()*2 - 1
	}

	return values
}

// smoothedNoise is a deterministic, low-frequency-heavy signal whose
// autocorrelation has a wide main lobe, like an RMS envelope.
func smoothedNoise(seed int64, count int) []float64 {
	random := rand.New(rand.NewSource(seed))
	values := make([]float64, count)
	state := 0.0
	for index := range values {
		state = 0.85*state + 0.15*(random.Float64()*2-1)
		values[index] = state
	}

	return values
}

// padFloat returns values preceded by lead zeros and followed by tail zeros.
func padFloat(values []float64, lead, tail int) []float64 {
	padded := make([]float64, lead+len(values)+tail)
	copy(padded[lead:], values)

	return padded
}
