package analysis

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestAnalyzeRealWAVOffset(t *testing.T) {
	requireFFmpeg(t)

	const (
		sampleRate = 48_000
		seconds    = 30
	)

	master := synthSpeech(101, seconds, sampleRate)
	directory := t.TempDir()

	tests := []struct {
		name       string
		source     []int16
		wantOffset float64
		minConfid  float64
	}{
		{
			name:       "source started 3.276 s after the reference",
			source:     trimHead(master, int(3.276*sampleRate)),
			wantOffset: 3.276,
			minConfid:  0.6,
		},
		{
			name:       "source started 0.5 s before the reference",
			source:     prependSilence(master, int(0.5*sampleRate)),
			wantOffset: -0.5,
			minConfid:  0.6,
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			referencePath := filepath.Join(directory, strings.ReplaceAll(test.name, " ", "_")+"_reference.wav")
			sourcePath := filepath.Join(directory, strings.ReplaceAll(test.name, " ", "_")+"_source.wav")
			writeWAV(t, referencePath, sampleRate, master)
			writeWAV(t, sourcePath, sampleRate, test.source)

			result, analysisErr := Analyze(context.Background(), NewExecRunner(), Request{
				ReferencePath: referencePath,
				SourcePath:    sourcePath,
			})
			if analysisErr != nil {
				t.Fatalf("analyze: %s", describeError(analysisErr))
			}

			t.Logf("offset=%.4fs confidence=%.3f drift=%v windows=%d notes=%v",
				result.OffsetSeconds, result.Confidence, formatDrift(result.DriftPPM), len(result.Windows), result.Notes)

			if delta := math.Abs(result.OffsetSeconds - test.wantOffset); delta > 0.005 {
				t.Fatalf("offset = %.4f, want %.4f (delta %.4f)", result.OffsetSeconds, test.wantOffset, delta)
			}
			if result.Confidence < test.minConfid {
				t.Fatalf("confidence = %.3f, want at least %.2f", result.Confidence, test.minConfid)
			}
			if len(result.Windows) != 3 {
				t.Fatalf("windows = %d, want 3", len(result.Windows))
			}
			for _, window := range result.Windows {
				if math.Abs(window.ResidualMs) > 5 {
					t.Fatalf("window %s residual = %.2f ms, want within 5 ms", window.Label, window.ResidualMs)
				}
			}
		})
	}
}

func TestAnalyzeDrift(t *testing.T) {
	requireFFmpeg(t)

	const (
		sampleRate = 48_000
		seconds    = 60
		ratio      = 1.0001 // the source plays 100 ppm faster
	)

	master := synthSpeech(103, seconds, sampleRate)
	source := resampleLinear(master, ratio)

	directory := t.TempDir()
	referencePath := filepath.Join(directory, "drift_reference.wav")
	sourcePath := filepath.Join(directory, "drift_source.wav")
	writeWAV(t, referencePath, sampleRate, master)
	writeWAV(t, sourcePath, sampleRate, source)

	result, analysisErr := Analyze(context.Background(), NewExecRunner(), Request{
		ReferencePath: referencePath,
		SourcePath:    sourcePath,
	})
	if analysisErr != nil {
		t.Fatalf("analyze: %s", describeError(analysisErr))
	}

	t.Logf("offset=%.4fs confidence=%.3f drift=%v windows=%d notes=%v",
		result.OffsetSeconds, result.Confidence, formatDrift(result.DriftPPM), len(result.Windows), result.Notes)
	for _, window := range result.Windows {
		t.Logf("window %s anchor=%.3f offset=%.4f residual=%.2fms confidence=%.3f",
			window.Label, window.AnchorSeconds, window.OffsetSeconds, window.ResidualMs, window.Confidence)
	}

	if result.DriftPPM == nil {
		t.Fatal("drift is nil, want a measured slope")
	}
	if drift := *result.DriftPPM; math.Abs(drift-100) > 40 {
		t.Fatalf("drift = %.1f ppm, want about 100", drift)
	}
	if math.Abs(result.OffsetSeconds-0.003) > 0.01 {
		t.Fatalf("offset = %.4f, want about 0.003 (the middle window's drift)", result.OffsetSeconds)
	}
	if result.Confidence < 0.6 {
		t.Fatalf("confidence = %.3f, want at least 0.6", result.Confidence)
	}
	// 100 ppm over the ~48 s window span is 4.8 ms, below the note threshold.
	for _, note := range result.Notes {
		if strings.Contains(strings.ToLower(note), "drift") {
			t.Fatalf("unexpected drift note for a small drift: %q", note)
		}
	}
}

func TestAnalyzeUnrelatedNoise(t *testing.T) {
	requireFFmpeg(t)

	const (
		sampleRate = 48_000
		seconds    = 30
	)

	directory := t.TempDir()
	referencePath := filepath.Join(directory, "noise_reference.wav")
	sourcePath := filepath.Join(directory, "noise_source.wav")
	writeWAV(t, referencePath, sampleRate, synthSpeech(201, seconds, sampleRate))
	writeWAV(t, sourcePath, sampleRate, synthSpeech(202, seconds, sampleRate))

	result, analysisErr := Analyze(context.Background(), NewExecRunner(), Request{
		ReferencePath: referencePath,
		SourcePath:    sourcePath,
	})
	if analysisErr != nil {
		t.Fatalf("analyze: %s", describeError(analysisErr))
	}

	t.Logf("offset=%.4fs confidence=%.3f drift=%v windows=%d notes=%v",
		result.OffsetSeconds, result.Confidence, formatDrift(result.DriftPPM), len(result.Windows), result.Notes)

	if result.Confidence >= 0.3 {
		t.Fatalf("confidence = %.3f for unrelated files, want below 0.3", result.Confidence)
	}
	assertNoteContains(t, result.Notes, "guess")
	assertNoteContains(t, result.Notes, "check the sync by ear")
}

func TestAnalyzeNoAudio(t *testing.T) {
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

	_, analysisErr := Analyze(context.Background(), NewExecRunner(), Request{
		ReferencePath: path,
		SourcePath:    path,
	})
	if analysisErr == nil || analysisErr.Code != CodeNoAudio {
		t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeNoAudio)
	}
}

func TestAnalyzeDecodeCommandShape(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	referencePath := filepath.Join(directory, "reference.wav")
	sourcePath := filepath.Join(directory, "source.wav")
	for _, path := range []string{referencePath, sourcePath} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	samples := synthSpeech(55, 5, 1000)
	runner := &pcmRunner{samples: map[string][]int16{
		referencePath: samples,
		sourcePath:    padInt16(samples, 300, 0),
	}}

	if _, analysisErr := Analyze(context.Background(), runner, Request{
		ReferencePath: referencePath,
		SourcePath:    sourcePath,
	}); analysisErr != nil {
		t.Fatalf("analyze: %s", describeError(analysisErr))
	}

	decodes := runner.decodedInputs()
	if len(decodes) != 2 || decodes[0] != referencePath || decodes[1] != sourcePath {
		t.Fatalf("decoded inputs = %v, want each file exactly once in order", decodes)
	}
	for _, args := range runner.calls {
		if containsArg(args, "-f") && containsArg(args, "-ss") {
			t.Fatalf("decode args must never seek: %v", args)
		}
	}

	want := []string{"-y", "-hide_banner", "-loglevel", "error", "-nostdin", "-i", referencePath,
		"-map", "0:a:0", "-ac", "1", "-ar", "1000", "-f", "s16le", "-acodec", "pcm_s16le"}
	got := runner.calls[0]
	if len(got) != len(want)+1 {
		t.Fatalf("decode args = %v, want the fixed command plus the temp output", got)
	}
	for index, expected := range want {
		if got[index] != expected {
			t.Fatalf("decode args[%d] = %q, want %q (full: %v)", index, got[index], expected, got)
		}
	}
	if output := got[len(got)-1]; !strings.HasSuffix(output, "audio.pcm") {
		t.Fatalf("decode output = %q, want a .pcm temp file", output)
	}
}

func TestAnalyzeProgress(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	referencePath := filepath.Join(directory, "reference.wav")
	sourcePath := filepath.Join(directory, "source.wav")
	for _, path := range []string{referencePath, sourcePath} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	samples := synthSpeech(56, 20, 1000)
	runner := &pcmRunner{samples: map[string][]int16{
		referencePath: samples,
		sourcePath:    padInt16(samples, 1500, 0),
	}}

	var stages []string
	lastPercent := -1.0
	if _, analysisErr := Analyze(context.Background(), runner, Request{
		ReferencePath: referencePath,
		SourcePath:    sourcePath,
		OnProgress: func(stage string, percent float64) {
			stages = append(stages, stage)
			if percent < lastPercent {
				t.Fatalf("progress went backwards: %g after %g", percent, lastPercent)
			}
			lastPercent = percent
		},
	}); analysisErr != nil {
		t.Fatalf("analyze: %s", describeError(analysisErr))
	}

	wantStages := []string{stageDecode, stageDecode, stageDecode, stageCoarse, stageFine, stageFine, stageFine, stageDone}
	if fmt.Sprint(stages) != fmt.Sprint(wantStages) {
		t.Fatalf("stages = %v, want %v", stages, wantStages)
	}
	if lastPercent != 100 {
		t.Fatalf("final percent = %g, want 100", lastPercent)
	}
}

func TestAnalyzeCanceled(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	referencePath := filepath.Join(directory, "reference.wav")
	sourcePath := filepath.Join(directory, "source.wav")
	for _, path := range []string{referencePath, sourcePath} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	t.Run("before the work starts", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, analysisErr := Analyze(ctx, &pcmRunner{}, Request{ReferencePath: referencePath, SourcePath: sourcePath})
		if analysisErr == nil || analysisErr.Code != CodeCanceled {
			t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeCanceled)
		}
	})

	t.Run("while decoding", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		runner := runnerFunc(func(ctx context.Context, _ string, _ []string) (string, error) {
			cancel()

			return "", ctx.Err()
		})

		_, analysisErr := Analyze(ctx, runner, Request{ReferencePath: referencePath, SourcePath: sourcePath})
		if analysisErr == nil || analysisErr.Code != CodeCanceled {
			t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeCanceled)
		}
	})
}

func TestAnalyzeTooLong(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	referencePath := filepath.Join(directory, "reference.wav")
	sourcePath := filepath.Join(directory, "source.wav")
	for _, path := range []string{referencePath, sourcePath} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	runner := &pcmRunner{truncate: map[string]int64{
		referencePath: maxDecodeSamples*bytesPerSample + 2,
	}}

	_, analysisErr := Analyze(context.Background(), runner, Request{ReferencePath: referencePath, SourcePath: sourcePath})
	if analysisErr == nil || analysisErr.Code != CodeTooLong {
		t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeTooLong)
	}
}

func TestAnalyzeRuntimeUnavailable(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	referencePath := filepath.Join(directory, "reference.wav")
	sourcePath := filepath.Join(directory, "source.wav")
	for _, path := range []string{referencePath, sourcePath} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	runner := &pcmRunner{failures: map[string]fakeFailure{
		referencePath: {err: errors.New("exec: \"ffmpeg\": executable file not found in $PATH")},
	}}

	_, analysisErr := Analyze(context.Background(), runner, Request{ReferencePath: referencePath, SourcePath: sourcePath})
	if analysisErr == nil || analysisErr.Code != CodeRuntimeUnavailable {
		t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeRuntimeUnavailable)
	}
}

func TestAnalyzeInsufficientOverlap(t *testing.T) {
	t.Parallel()

	directory := t.TempDir()
	referencePath := filepath.Join(directory, "reference.wav")
	sourcePath := filepath.Join(directory, "source.wav")
	for _, path := range []string{referencePath, sourcePath} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	samples := synthSpeech(57, 0.5, 1000)
	runner := &pcmRunner{samples: map[string][]int16{
		referencePath: samples,
		sourcePath:    samples,
	}}

	_, analysisErr := Analyze(context.Background(), runner, Request{ReferencePath: referencePath, SourcePath: sourcePath})
	if analysisErr == nil || analysisErr.Code != CodeInsufficientOverlap {
		t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeInsufficientOverlap)
	}
}

func TestAnalyzeInvalidRequest(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		request Request
	}{
		{name: "missing reference", request: Request{SourcePath: "source.wav"}},
		{name: "missing source", request: Request{ReferencePath: "reference.wav"}},
		{
			name:    "negative maximum offset",
			request: Request{ReferencePath: "reference.wav", SourcePath: "source.wav", MaxOffsetSeconds: -1},
		},
		{
			name:    "negative window",
			request: Request{ReferencePath: "reference.wav", SourcePath: "source.wav", WindowSeconds: -2},
		},
	}

	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			_, analysisErr := Analyze(context.Background(), &pcmRunner{}, test.request)
			if analysisErr == nil || analysisErr.Code != CodeInputInvalid {
				t.Fatalf("error = %s, want %s", describeError(analysisErr), CodeInputInvalid)
			}
		})
	}
}

// TestAnalyzeTenMinuteBudget guards against an accidental O(n^2) regression:
// ten minutes of 1 kHz PCM must finish in well under a few seconds, with no
// decoder involved.
func TestAnalyzeTenMinuteBudget(t *testing.T) {
	t.Parallel()

	const (
		minutes = 10
		samples = minutes * 60 * decodeSampleRate
	)

	reference := synthSpeech(61, minutes*60, decodeSampleRate)
	source := prependSilence(reference, 200)

	directory := t.TempDir()
	referencePath := filepath.Join(directory, "reference.wav")
	sourcePath := filepath.Join(directory, "source.wav")
	for _, path := range []string{referencePath, sourcePath} {
		if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
			t.Fatalf("write fixture: %v", err)
		}
	}

	runner := &pcmRunner{samples: map[string][]int16{
		referencePath: reference,
		sourcePath:    source,
	}}

	started := time.Now()
	result, analysisErr := Analyze(context.Background(), runner, Request{ReferencePath: referencePath, SourcePath: sourcePath})
	elapsed := time.Since(started)
	if analysisErr != nil {
		t.Fatalf("analyze: %s", describeError(analysisErr))
	}
	if len(reference) != samples {
		t.Fatalf("fixture length = %d, want %d", len(reference), samples)
	}

	t.Logf("10 minute analysis took %v (offset=%.4f confidence=%.3f)", elapsed, result.OffsetSeconds, result.Confidence)

	if elapsed > 5*time.Second {
		t.Fatalf("analysis took %v, want well under 5 s for ten minutes of audio", elapsed)
	}
	// Prepended silence moves the source's t = 0 earlier on the reference.
	if delta := math.Abs(result.OffsetSeconds - (-0.2)); delta > 0.005 {
		t.Fatalf("offset = %.4f, want -0.2", result.OffsetSeconds)
	}
	if result.Confidence < 0.6 {
		t.Fatalf("confidence = %.3f, want at least 0.6", result.Confidence)
	}
}

func BenchmarkCrossCorrelateTenMinutes(b *testing.B) {
	reference := smoothedNoise(71, 10*60*envelopeHz)
	meanRemove(reference)
	source := reference[300:]

	b.ResetTimer()
	for iteration := 0; iteration < b.N; iteration++ {
		CrossCorrelate(reference, source, 0)
	}
}

func formatDrift(drift *float64) string {
	if drift == nil {
		return "<nil>"
	}

	return fmt.Sprintf("%+.1f ppm", *drift)
}

// runnerFunc adapts an anonymous function to the Runner interface.
type runnerFunc func(ctx context.Context, path string, args []string) (string, error)

func (f runnerFunc) Run(ctx context.Context, path string, args []string) (string, error) {
	return f(ctx, path, args)
}

// padInt16 returns values preceded by lead zeros and followed by tail zeros.
func padInt16(values []int16, lead, tail int) []int16 {
	padded := make([]int16, lead+len(values)+tail)
	copy(padded[lead:], values)

	return padded
}

// TestAnalyzeSignConvention pins the annex contract in both directions:
// OffsetSeconds is where the source's t = 0 sits on the reference timeline.
//
// A source that was already rolling before the reference started carries extra
// audio at its head (prepended silence or earlier sound), so its t = 0 sits
// before the reference start and the offset is negative. A source that started
// later is missing its head, so its t = 0 sits after the reference start and the
// offset is positive. Both directions are asserted with an exact sample count.
func TestAnalyzeSignConvention(t *testing.T) {
	t.Parallel()

	const shift = 700 // samples at 1 kHz
	master := synthSpeech(301, 20, decodeSampleRate)

	tests := []struct {
		name   string
		source []int16
		want   float64
	}{
		{
			name:   "prepended silence moves the source start before the reference",
			source: prependSilence(master, shift),
			want:   -float64(shift) / decodeSampleRate,
		},
		{
			name:   "trimmed head moves the source start after the reference",
			source: trimHead(master, shift),
			want:   float64(shift) / decodeSampleRate,
		},
	}

	directory := t.TempDir()
	for _, test := range tests {
		test := test
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()

			referencePath := filepath.Join(directory, strings.ReplaceAll(test.name, " ", "_")+"_reference.wav")
			sourcePath := filepath.Join(directory, strings.ReplaceAll(test.name, " ", "_")+"_source.wav")
			for _, path := range []string{referencePath, sourcePath} {
				if err := os.WriteFile(path, []byte("fixture"), 0o644); err != nil {
					t.Fatalf("write fixture: %v", err)
				}
			}

			runner := &pcmRunner{samples: map[string][]int16{
				referencePath: master,
				sourcePath:    test.source,
			}}
			result, analysisErr := Analyze(context.Background(), runner, Request{
				ReferencePath: referencePath,
				SourcePath:    sourcePath,
			})
			if analysisErr != nil {
				t.Fatalf("analyze: %s", describeError(analysisErr))
			}

			t.Logf("offset=%.4f confidence=%.3f", result.OffsetSeconds, result.Confidence)
			if delta := math.Abs(result.OffsetSeconds - test.want); delta > 0.005 {
				t.Fatalf("offset = %.4f, want %.4f (delta %.4f)", result.OffsetSeconds, test.want, delta)
			}
			if result.Confidence < 0.6 {
				t.Fatalf("confidence = %.3f, want at least 0.6", result.Confidence)
			}
		})
	}
}
