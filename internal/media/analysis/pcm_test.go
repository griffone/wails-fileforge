package analysis

import (
	"context"
	"errors"
	"math"
	"os"
	"path/filepath"
	"testing"
)

func TestDecodeMonoReadsSamples(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "input.wav")
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	samples := []int16{0, 1, -1, 32767, -32768, 1234}
	runner := &pcmRunner{samples: map[string][]int16{path: samples}}

	pcm, analysisErr := DecodeMono(context.Background(), runner, path)
	if analysisErr != nil {
		t.Fatalf("decode: %s", describeError(analysisErr))
	}
	if pcm.SampleRate != decodeSampleRate {
		t.Fatalf("sample rate = %d, want %d", pcm.SampleRate, decodeSampleRate)
	}
	if len(pcm.Samples) != len(samples) {
		t.Fatalf("samples = %d, want %d", len(pcm.Samples), len(samples))
	}
	for index, sample := range samples {
		if pcm.Samples[index] != sample {
			t.Fatalf("sample %d = %d, want %d", index, pcm.Samples[index], sample)
		}
	}
	if want := float64(len(samples)) / decodeSampleRate; math.Abs(pcm.DurationSeconds()-want) > 1e-12 {
		t.Fatalf("duration = %g, want %g", pcm.DurationSeconds(), want)
	}
}

func TestDecodeMonoNoAudio(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "input.mp4")
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	runner := &pcmRunner{failures: map[string]fakeFailure{
		path: {
			stderr: "Stream map '0:a:0' matches no streams.\nTo ignore this, add a trailing '?' to the map.",
			err:    errors.New("exit status 1"),
		},
	}}

	_, analysisErr := DecodeMono(context.Background(), runner, path)
	if analysisErr == nil || analysisErr.Code != CodeNoAudio {
		t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeNoAudio)
	}
}

func TestDecodeMonoTooLong(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "input.wav")
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	runner := &pcmRunner{truncate: map[string]int64{path: maxDecodeSamples*bytesPerSample + 2}}

	_, analysisErr := DecodeMono(context.Background(), runner, path)
	if analysisErr == nil || analysisErr.Code != CodeTooLong {
		t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeTooLong)
	}
}

func TestDecodeMonoEmptyFile(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "input.wav")
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	runner := &pcmRunner{samples: map[string][]int16{path: nil}}

	_, analysisErr := DecodeMono(context.Background(), runner, path)
	if analysisErr == nil || analysisErr.Code != CodeNoAudio {
		t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeNoAudio)
	}
}

func TestDecodeMonoInvalidInput(t *testing.T) {
	t.Parallel()

	if _, analysisErr := DecodeMono(context.Background(), &pcmRunner{}, " "); analysisErr == nil || analysisErr.Code != CodeInputInvalid {
		t.Fatalf("empty path error = %s, want %s", describeError(analysisErr), CodeInputInvalid)
	}

	missing := filepath.Join(t.TempDir(), "missing.wav")
	if _, analysisErr := DecodeMono(context.Background(), &pcmRunner{}, missing); analysisErr == nil || analysisErr.Code != CodeInputInvalid {
		t.Fatalf("missing file error = %s, want %s", describeError(analysisErr), CodeInputInvalid)
	}
}

func TestDecodeMonoNilRunner(t *testing.T) {
	t.Parallel()

	path := filepath.Join(t.TempDir(), "input.wav")
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	_, analysisErr := DecodeMono(context.Background(), nil, path)
	if analysisErr == nil || analysisErr.Code != CodeRuntimeUnavailable {
		t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeRuntimeUnavailable)
	}
}

func TestParseDurationReport(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		report string
		want   float64
		ok     bool
	}{
		{
			name:   "plain duration",
			report: "  Duration: 00:01:02.50, start: 0.000000, bitrate: 128 kb/s\n",
			want:   62.5,
			ok:     true,
		},
		{
			name:   "unknown duration",
			report: "  Duration: N/A, bitrate: 128 kb/s\n",
			ok:     false,
		},
		{
			name:   "no duration line",
			report: "Invalid data found when processing input\n",
			ok:     false,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			got, ok := parseDurationReport(test.report)
			if ok != test.ok {
				t.Fatalf("ok = %v, want %v", ok, test.ok)
			}
			if test.ok && math.Abs(got-test.want) > 1e-9 {
				t.Fatalf("duration = %g, want %g", got, test.want)
			}
		})
	}
}
