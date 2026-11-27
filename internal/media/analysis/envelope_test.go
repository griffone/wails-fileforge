package analysis

import (
	"math"
	"testing"
)

func TestEnvelopeConstantRMS(t *testing.T) {
	t.Parallel()

	const value = 16384 // half of full scale
	samples := make([]int16, 1000)
	for index := range samples {
		samples[index] = value
	}

	envelope := Envelope(samples, 1000, 100)
	if len(envelope) != 100 {
		t.Fatalf("frame count = %d, want 100", len(envelope))
	}
	for index, frame := range envelope {
		if math.Abs(frame-0.5) > 1e-9 {
			t.Fatalf("frame %d = %g, want 0.5", index, frame)
		}
	}
}

func TestEnvelopeHalfSilence(t *testing.T) {
	t.Parallel()

	samples := make([]int16, 1000)
	for index := 0; index < 500; index++ {
		samples[index] = 32767
	}

	envelope := Envelope(samples, 1000, 100)
	if len(envelope) != 100 {
		t.Fatalf("frame count = %d, want 100", len(envelope))
	}
	if envelope[0] < 0.99 {
		t.Fatalf("first frame = %g, want about 1", envelope[0])
	}
	if envelope[49] < 0.99 {
		t.Fatalf("last loud frame = %g, want about 1", envelope[49])
	}
	if envelope[50] != 0 || envelope[99] != 0 {
		t.Fatalf("silent frames are not zero: %g %g", envelope[50], envelope[99])
	}
}

func TestEnvelopeFrameRounding(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		sampleCount int
		wantFrames  int
	}{
		{name: "exact", sampleCount: 100, wantFrames: 10},
		{name: "partial tail dropped", sampleCount: 109, wantFrames: 10},
		{name: "less than one frame", sampleCount: 9, wantFrames: 0},
		{name: "empty", sampleCount: 0, wantFrames: 0},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			envelope := Envelope(make([]int16, test.sampleCount), 1000, 100)
			if len(envelope) != test.wantFrames {
				t.Fatalf("frames = %d, want %d", len(envelope), test.wantFrames)
			}
		})
	}
}

func TestEnvelopeHalfRateFrameSize(t *testing.T) {
	t.Parallel()

	// 48 kHz in, 100 Hz out: 480 samples per frame, 4 full frames.
	samples := make([]int16, 48_000)
	for index := range samples {
		samples[index] = -32768
	}

	envelope := Envelope(samples, 48_000, 100)
	if len(envelope) != 100 {
		t.Fatalf("frame count = %d, want 100", len(envelope))
	}
	if math.Abs(envelope[0]-1) > 1e-9 {
		t.Fatalf("frame 0 = %g, want 1 for a full-scale negative constant", envelope[0])
	}
}
