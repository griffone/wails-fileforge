package analysis

import "math"

// Envelope reduces samples to the RMS of consecutive frames. hz is the frame
// rate; a sample rate of 1000 and hz of 100 produces 10 ms frames. Values are
// normalised to 0..1 (full-scale int16 is 1), so an envelope is independent of
// the PCM bit depth.
//
// A trailing partial frame is dropped: every frame averages exactly
// sampleRate/hz samples, which keeps the frame index directly convertible to
// seconds. An empty result means there was less than one full frame.
func Envelope(samples []int16, sampleRate, hz int) []float64 {
	if len(samples) == 0 || sampleRate <= 0 || hz <= 0 {
		return nil
	}

	frameSize := sampleRate / hz
	if frameSize < 1 {
		frameSize = 1
	}
	frameCount := len(samples) / frameSize
	if frameCount == 0 {
		return nil
	}

	envelope := make([]float64, frameCount)
	const fullScale = 32768.0
	for frame := 0; frame < frameCount; frame++ {
		base := frame * frameSize
		var energy float64
		for _, sample := range samples[base : base+frameSize] {
			value := float64(sample) / fullScale
			energy += value * value
		}
		envelope[frame] = math.Sqrt(energy / float64(frameSize))
	}

	return envelope
}
