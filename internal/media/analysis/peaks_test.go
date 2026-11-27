package analysis

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestPeaksFullFile(t *testing.T) {
	requireFFmpeg(t)

	const sampleRate = 48_000
	samples := make([]int16, sampleRate*2)
	for index := range samples {
		samples[index] = int16(0.5 * 32767 * math.Sin(2*math.Pi*200*float64(index)/sampleRate))
	}

	path := filepath.Join(t.TempDir(), "tone.wav")
	writeWAV(t, path, sampleRate, samples)

	result, analysisErr := Peaks(context.Background(), NewExecRunner(), PeaksRequest{Path: path, Buckets: 256})
	if analysisErr != nil {
		t.Fatalf("peaks: %s", describeError(analysisErr))
	}
	if !result.HasAudio {
		t.Fatal("HasAudio = false, want true")
	}
	if math.Abs(result.DurationSeconds-2) > 0.05 {
		t.Fatalf("duration = %g, want about 2", result.DurationSeconds)
	}
	if result.StartSeconds != 0 || math.Abs(result.SpanSeconds-2) > 0.05 {
		t.Fatalf("window = %g + %g, want 0 + 2", result.StartSeconds, result.SpanSeconds)
	}
	if len(result.Min) != 256 || len(result.Max) != 256 {
		t.Fatalf("bucket count = %d/%d, want 256", len(result.Min), len(result.Max))
	}

	highest := float32(-1)
	lowest := float32(1)
	for index := range result.Min {
		if result.Max[index] > highest {
			highest = result.Max[index]
		}
		if result.Min[index] < lowest {
			lowest = result.Min[index]
		}
		if result.Min[index] > result.Max[index] {
			t.Fatalf("bucket %d has min %g above max %g", index, result.Min[index], result.Max[index])
		}
		if result.Min[index] < -1 || result.Max[index] > 1 {
			t.Fatalf("bucket %d is outside -1..1: %g..%g", index, result.Min[index], result.Max[index])
		}
	}
	if math.Abs(float64(highest)-0.5) > 0.05 {
		t.Fatalf("highest sample = %g, want about 0.5", highest)
	}
	if math.Abs(float64(lowest)+0.5) > 0.05 {
		t.Fatalf("lowest sample = %g, want about -0.5", lowest)
	}
}

func TestPeaksWindowedRange(t *testing.T) {
	requireFFmpeg(t)

	const sampleRate = 48_000
	samples := make([]int16, sampleRate*4)
	// Loud first second, silence, loud last second: the requested middle window
	// must not see the loud parts.
	for index := 0; index < sampleRate; index++ {
		samples[index] = 30000
		samples[len(samples)-1-index] = -30000
	}

	path := filepath.Join(t.TempDir(), "windows.wav")
	writeWAV(t, path, sampleRate, samples)

	result, analysisErr := Peaks(context.Background(), NewExecRunner(), PeaksRequest{
		Path:         path,
		StartSeconds: 1.2,
		SpanSeconds:  1.6,
		Buckets:      128,
	})
	if analysisErr != nil {
		t.Fatalf("peaks: %s", describeError(analysisErr))
	}
	if !result.HasAudio {
		t.Fatal("HasAudio = false, want true")
	}
	if result.StartSeconds != 1.2 || result.SpanSeconds != 1.6 {
		t.Fatalf("window = %g + %g, want 1.2 + 1.6", result.StartSeconds, result.SpanSeconds)
	}
	if math.Abs(result.DurationSeconds-4) > 0.05 {
		t.Fatalf("duration = %g, want the absolute file duration 4", result.DurationSeconds)
	}
	for index := range result.Min {
		if result.Min[index] != 0 || result.Max[index] != 0 {
			t.Fatalf("bucket %d of a silent window is %g..%g, want 0..0", index, result.Min[index], result.Max[index])
		}
	}
}

func TestPeaksSilence(t *testing.T) {
	requireFFmpeg(t)

	path := filepath.Join(t.TempDir(), "silence.wav")
	writeWAV(t, path, 48_000, make([]int16, 48_000))

	result, analysisErr := Peaks(context.Background(), NewExecRunner(), PeaksRequest{Path: path, Buckets: 128})
	if analysisErr != nil {
		t.Fatalf("peaks: %s", describeError(analysisErr))
	}
	if !result.HasAudio {
		t.Fatal("a silent file still has an audio stream")
	}
	for index := range result.Min {
		if result.Min[index] != 0 || result.Max[index] != 0 {
			t.Fatalf("bucket %d = %g..%g, want 0..0", index, result.Min[index], result.Max[index])
		}
	}
}

func TestPeaksNoAudioStream(t *testing.T) {
	requireFFmpeg(t)

	path := filepath.Join(t.TempDir(), "video_only.mp4")
	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", "testsrc=size=64x64:rate=5",
		"-t", "1",
		"-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p",
		path,
	}
	if output, err := exec.Command("ffmpeg", args...).CombinedOutput(); err != nil {
		t.Fatalf("generate video-only fixture: %v (%s)", err, strings.TrimSpace(string(output)))
	}

	result, analysisErr := Peaks(context.Background(), NewExecRunner(), PeaksRequest{Path: path, Buckets: 200})
	if analysisErr != nil {
		t.Fatalf("peaks returned an error for a video-only file: %s", describeError(analysisErr))
	}
	if result.HasAudio {
		t.Fatal("HasAudio = true, want false")
	}
	if len(result.Min) != 200 || len(result.Max) != 200 {
		t.Fatalf("bucket count = %d/%d, want 200 zero buckets", len(result.Min), len(result.Max))
	}
	for index := range result.Min {
		if result.Min[index] != 0 || result.Max[index] != 0 {
			t.Fatalf("bucket %d = %g..%g, want 0..0", index, result.Min[index], result.Max[index])
		}
	}
	if math.Abs(result.DurationSeconds-1) > 0.05 {
		t.Fatalf("duration = %g, want about 1", result.DurationSeconds)
	}
}

func TestPeaksClampsBuckets(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "fixture.wav")
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	runner := &pcmRunner{samples: map[string][]int16{
		path: synthSpeech(81, 2, 1000),
	}}

	for _, test := range []struct {
		requested int
		want      int
	}{
		{requested: 1, want: minPeakBuckets},
		{requested: 10_000, want: maxPeakBuckets},
		{requested: 500, want: 500},
	} {
		result, analysisErr := Peaks(context.Background(), runner, PeaksRequest{Path: path, Buckets: test.requested})
		if analysisErr != nil {
			t.Fatalf("peaks: %s", describeError(analysisErr))
		}
		if len(result.Min) != test.want {
			t.Fatalf("requested %d buckets gave %d, want %d", test.requested, len(result.Min), test.want)
		}
	}
}

func TestPeaksSeeksBeforeTheInput(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	path := filepath.Join(directory, "fixture.wav")
	if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	runner := &pcmRunner{
		samples:     map[string][]int16{path: synthSpeech(82, 4, 1000)},
		probeStderr: "  Duration: 00:00:04.00, start: 0.000000, bitrate: 128 kb/s\n",
	}

	result, analysisErr := Peaks(context.Background(), runner, PeaksRequest{
		Path:         path,
		StartSeconds: 1.5,
		SpanSeconds:  0.5,
		Buckets:      128,
	})
	if analysisErr != nil {
		t.Fatalf("peaks: %s", describeError(analysisErr))
	}
	if math.Abs(result.DurationSeconds-4) > 1e-9 {
		t.Fatalf("duration = %g, want the probed 4", result.DurationSeconds)
	}

	var decode []string
	for _, args := range runner.calls {
		if containsArg(args, "-f") {
			decode = args
		}
	}
	if decode == nil {
		t.Fatal("no decode call was made")
	}
	seekIndex := indexOfArg(decode, "-ss")
	inputIndex := indexOfArg(decode, "-i")
	if seekIndex < 0 || inputIndex < 0 || seekIndex > inputIndex {
		t.Fatalf("expected an input seek before -i, got %v", decode)
	}
	if got := argAfter(decode, "-ss"); got != "1.500" {
		t.Fatalf("-ss = %q, want 1.500", got)
	}
	if got := argAfter(decode, "-t"); got != "0.500" {
		t.Fatalf("-t = %q, want 0.500", got)
	}
}

func TestPeaksInvalidRequests(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request PeaksRequest
	}{
		{name: "missing path", request: PeaksRequest{Buckets: 128}},
		{name: "negative start", request: PeaksRequest{Path: "file.wav", StartSeconds: -1, Buckets: 128}},
		{name: "negative span", request: PeaksRequest{Path: "file.wav", SpanSeconds: -1, Buckets: 128}},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, analysisErr := Peaks(context.Background(), &pcmRunner{}, test.request)
			if analysisErr == nil || analysisErr.Code != CodeInputInvalid {
				t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeInputInvalid)
			}
		})
	}
}

func indexOfArg(args []string, flag string) int {
	for index, arg := range args {
		if arg == flag {
			return index
		}
	}

	return -1
}
