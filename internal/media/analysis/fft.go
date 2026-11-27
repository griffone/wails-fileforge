package analysis

import "math"

// twiddleRebase is how many butterfly steps reuse a multiplied twiddle factor
// before recomputing it from scratch. Recomputation keeps the phase error of a
// multi-million point transform far below one sample while staying O(n log n).
const twiddleRebase = 256

// FFT computes the discrete Fourier transform of values in place using an
// iterative radix-2 decimation-in-time algorithm. len(values) must be a power of
// two; the function panics otherwise because a silently wrong length would
// corrupt every downstream lag.
//
// inverse selects the sign of the exponent and applies the 1/n scale. Forward
// transforms are unscaled, so FFT followed by FFT with inverse is the identity.
func FFT(values []complex128, inverse bool) {
	count := len(values)
	if count == 0 {
		return
	}
	if count&(count-1) != 0 {
		panic("analysis: FFT length must be a power of two")
	}
	if count == 1 {
		return
	}

	// Bit-reversal permutation: index i moves to the index with its bits
	// reversed, which is the order the in-place butterflies expect.
	for i, j := 1, 0; i < count; i++ {
		bit := count >> 1
		for ; j&bit != 0; bit >>= 1 {
			j ^= bit
		}
		j ^= bit
		if i < j {
			values[i], values[j] = values[j], values[i]
		}
	}

	for length := 2; length <= count; length <<= 1 {
		half := length >> 1
		angle := 2 * math.Pi / float64(length)
		if !inverse {
			angle = -angle
		}
		step := complex(math.Cos(angle), math.Sin(angle))

		for start := 0; start < count; start += length {
			twiddle := complex(1, 0)
			for offset := 0; offset < half; offset++ {
				if offset > 0 && offset%twiddleRebase == 0 {
					phase := angle * float64(offset)
					twiddle = complex(math.Cos(phase), math.Sin(phase))
				}

				even := values[start+offset]
				odd := values[start+offset+half] * twiddle
				values[start+offset] = even + odd
				values[start+offset+half] = even - odd
				twiddle *= step
			}
		}
	}

	if !inverse {
		return
	}

	scale := 1 / float64(count)
	for index := range values {
		values[index] = complex(real(values[index])*scale, imag(values[index])*scale)
	}
}
