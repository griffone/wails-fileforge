package analysis

import (
	"math"
	"math/cmplx"
	"strconv"
	"testing"
)

func TestFFTRoundTripIdentity(t *testing.T) {
	t.Parallel()

	for _, size := range []int{1, 2, 4, 16, 256, 4096} {
		size := size
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			t.Parallel()

			rng := newDeterministicRNG(int64(size))
			original := make([]complex128, size)
			values := make([]complex128, size)
			for index := range values {
				original[index] = complex(rng.float64(), rng.float64())
				values[index] = original[index]
			}

			FFT(values, false)
			FFT(values, true)

			for index := range values {
				if delta := cmplx.Abs(values[index] - original[index]); delta > 1e-9 {
					t.Fatalf("index %d: round trip delta %g exceeds 1e-9", index, delta)
				}
			}
		})
	}
}

func TestFFTMatchesNaiveDFT(t *testing.T) {
	t.Parallel()

	for _, size := range []int{1, 2, 4, 8, 16, 32} {
		size := size
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			t.Parallel()

			rng := newDeterministicRNG(int64(size) + 100)
			values := make([]complex128, size)
			for index := range values {
				values[index] = complex(rng.float64(), rng.float64())
			}

			want := naiveDFT(values)
			FFT(values, false)

			for index := range values {
				if delta := cmplx.Abs(values[index] - want[index]); delta > 1e-9 {
					t.Fatalf("bin %d: delta %g exceeds 1e-9 (got %v want %v)", index, delta, values[index], want[index])
				}
			}
		})
	}
}

func TestFFTParseval(t *testing.T) {
	t.Parallel()

	const size = 512
	rng := newDeterministicRNG(7)
	values := make([]complex128, size)
	var timeEnergy float64
	for index := range values {
		values[index] = complex(rng.float64(), rng.float64())
		timeEnergy += math.Pow(cmplx.Abs(values[index]), 2)
	}

	FFT(values, false)

	var frequencyEnergy float64
	for _, value := range values {
		frequencyEnergy += math.Pow(cmplx.Abs(value), 2)
	}

	got := frequencyEnergy / size
	if relative := math.Abs(got-timeEnergy) / timeEnergy; relative > 1e-9 {
		t.Fatalf("Parseval drift %g exceeds 1e-9", relative)
	}
}

func TestFFTZeroAndImpulse(t *testing.T) {
	t.Parallel()

	zeros := make([]complex128, 64)
	FFT(zeros, false)
	for index, value := range zeros {
		if value != 0 {
			t.Fatalf("bin %d of a zero signal is %v", index, value)
		}
	}

	impulse := make([]complex128, 64)
	impulse[0] = 1
	FFT(impulse, false)
	for index, value := range impulse {
		if cmplx.Abs(value-1) > 1e-12 {
			t.Fatalf("bin %d of an impulse is %v, want 1", index, value)
		}
	}
}

func TestFFTSingleBin(t *testing.T) {
	t.Parallel()

	const (
		size = 64
		bin  = 5
	)
	values := make([]complex128, size)
	for index := range values {
		phase := 2 * math.Pi * float64(bin) * float64(index) / size
		values[index] = cmplx.Rect(1, phase)
	}

	FFT(values, false)

	for index, value := range values {
		switch index {
		case bin:
			if cmplx.Abs(value-complex(size, 0)) > 1e-9 {
				t.Fatalf("bin %d is %v, want %d", index, value, size)
			}
		default:
			if magnitude := cmplx.Abs(value); magnitude > 1e-9 {
				t.Fatalf("bin %d leaked %g", index, magnitude)
			}
		}
	}
}

func TestFFTInverseSingleBin(t *testing.T) {
	t.Parallel()

	const (
		size = 32
		bin  = 3
	)
	values := make([]complex128, size)
	values[bin] = complex(size, 0)

	FFT(values, true)

	for index, value := range values {
		phase := 2 * math.Pi * float64(bin) * float64(index) / size
		want := cmplx.Rect(1, phase)
		if delta := cmplx.Abs(value - want); delta > 1e-9 {
			t.Fatalf("index %d: delta %g (got %v want %v)", index, delta, value, want)
		}
	}
}

func TestFFTPanicsOnBadLength(t *testing.T) {
	t.Parallel()

	defer func() {
		if recovered := recover(); recovered == nil {
			t.Fatal("expected a panic for a non power-of-two transform")
		}
	}()

	FFT(make([]complex128, 6), false)
}

// naiveDFT is the O(n^2) reference the radix-2 transform is checked against.
func naiveDFT(values []complex128) []complex128 {
	count := len(values)
	output := make([]complex128, count)
	for frequency := 0; frequency < count; frequency++ {
		var sum complex128
		for index := 0; index < count; index++ {
			angle := -2 * math.Pi * float64(frequency*index) / float64(count)
			sum += values[index] * cmplx.Rect(1, angle)
		}
		output[frequency] = sum
	}

	return output
}

// deterministicRNG is a tiny LCG so the tests do not depend on math/rand
// version details.
type deterministicRNG struct {
	state uint64
}

func newDeterministicRNG(seed int64) *deterministicRNG {
	return &deterministicRNG{state: uint64(seed)*6364136223846793005 + 1442695040888963407}
}

func (r *deterministicRNG) next() uint64 {
	r.state = r.state*6364136223846793005 + 1442695040888963407

	return r.state
}

func (r *deterministicRNG) float64() float64 {
	return float64(r.next()>>11)/float64(1<<53)*2 - 1
}
