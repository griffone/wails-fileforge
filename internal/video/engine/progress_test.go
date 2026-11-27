package engine

import (
	"context"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestParseFFmpegProgressPositions(t *testing.T) {
	tests := []struct {
		name string
		line string
		want int64
	}{
		{name: "out_time_ms microseconds", line: "out_time_ms=1500000", want: 1_500_000},
		{name: "out_time_us microseconds", line: "out_time_us=2500000", want: 2_500_000},
		{name: "out_time full timestamp", line: "out_time=00:00:01.500000", want: 1_500_000},
		{name: "out_time short fraction", line: "out_time=00:00:01.5", want: 1_500_000},
		{name: "out_time beyond one day", line: "out_time=25:00:00.000000", want: 90_000_000_000},
		{name: "out_time zero", line: "out_time=00:00:00.000000", want: 0},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			sample, ok := ParseFFmpegProgress(testCase.line)
			if !ok {
				t.Fatalf("expected %q to parse", testCase.line)
			}
			if sample.Done {
				t.Fatalf("expected %q to be a position, not end", testCase.line)
			}
			if sample.Microseconds != testCase.want {
				t.Fatalf("microseconds = %d, want %d", sample.Microseconds, testCase.want)
			}
		})
	}
}

func TestParseFFmpegProgressEndMarker(t *testing.T) {
	sample, ok := ParseFFmpegProgress("progress=end")
	if !ok || !sample.Done {
		t.Fatalf("expected progress=end to be recognised, got %+v ok=%v", sample, ok)
	}
	if sample.Microseconds != 0 {
		t.Fatalf("end marker must not carry a position, got %d", sample.Microseconds)
	}

	if _, ok := ParseFFmpegProgress("progress=continue"); ok {
		t.Fatal("progress=continue must not be reported as a sample")
	}
	if _, ok := ParseFFmpegProgress("progress="); ok {
		t.Fatal("an empty progress value must not be recognised")
	}
}

func TestParseFFmpegProgressMalformedLines(t *testing.T) {
	malformed := []string{
		"",
		"garbage",
		"out_time_ms",
		"out_time_ms=",
		"out_time_ms=abc",
		"out_time_ms=-5",
		"out_time_us=NaN",
		"out_time=",
		"out_time=00:00",
		"out_time=N/A",
		"out_time=00:99:00.000000",
		"out_time=00:00:60.000000",
		"out_time=00:00:01.",
		"out_time=00:00:01.1234567",
		"frame=12",
		"speed=1.2x",
	}

	for _, line := range malformed {
		if sample, ok := ParseFFmpegProgress(line); ok {
			t.Fatalf("expected %q to be ignored, got %+v", line, sample)
		}
	}
}

func TestFFmpegProgressReporterClampsAndNeverRegresses(t *testing.T) {
	reporter := newFFmpegProgressReporter(10)
	tests := []struct {
		name        string
		micros      int64
		wantPercent float64
		wantOK      bool
	}{
		{name: "first tick", micros: 2_500_000, wantPercent: 25, wantOK: true},
		{name: "backwards tick is dropped", micros: 1_000_000, wantOK: false},
		{name: "duplicate tick is dropped", micros: 2_500_000, wantOK: false},
		{name: "monotonic advance", micros: 7_500_000, wantPercent: 75, wantOK: true},
		{name: "clamped at 100", micros: 12_000_000, wantPercent: 100, wantOK: true},
		{name: "above 100 is still clamped", micros: 15_000_000, wantOK: false},
		{name: "zero position is ignored", micros: 0, wantOK: false},
		{name: "negative position is ignored", micros: -1, wantOK: false},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			percent, ok := reporter.Percent(FFmpegProgressSample{Microseconds: testCase.micros})
			if ok != testCase.wantOK {
				t.Fatalf("ok = %v, want %v", ok, testCase.wantOK)
			}
			if ok && math.Abs(percent-testCase.wantPercent) > 0.001 {
				t.Fatalf("percent = %v, want %v", percent, testCase.wantPercent)
			}
		})
	}
}

func TestFFmpegProgressReporterWithoutDuration(t *testing.T) {
	for _, duration := range []float64{0, -1, math.NaN(), math.Inf(1)} {
		reporter := newFFmpegProgressReporter(duration)
		if percent, ok := reporter.Percent(FFmpegProgressSample{Microseconds: 5_000_000}); ok {
			t.Fatalf("duration %v must not produce a percentage, got %v", duration, percent)
		}
	}
}

func TestFFmpegProgressHandlerConsumesFakeStream(t *testing.T) {
	var percents []float64
	handler := newFFmpegProgressHandler(4, func(percent float64, _ string) {
		percents = append(percents, percent)
	})

	for _, line := range []string{
		"out_time_ms=1000000",
		"out_time=00:00:01.500000",
		"out_time_us=1500000",
		"progress=continue",
		"progress=end",
	} {
		handler(line)
	}

	if len(percents) != 2 {
		t.Fatalf("percents = %v, want two updates", percents)
	}
	if percents[0] != 25 || percents[1] != 37.5 {
		t.Fatalf("percents = %v, want [25 37.5]", percents)
	}
}

func TestWithFFmpegProgressArgsDoesNotDuplicate(t *testing.T) {
	once := withFFmpegProgressArgs([]string{"-y", "-i", "in.mp4", "out.mp4"})
	if len(once) != 7 || once[0] != "-progress" || once[1] != "pipe:1" || once[2] != "-nostats" {
		t.Fatalf("unexpected args: %v", once)
	}

	twice := withFFmpegProgressArgs(once)
	if len(twice) != len(once) {
		t.Fatalf("progress args were duplicated: %v", twice)
	}
}

func TestExecCommandRunnerStreamsRealFFmpegProgress(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the real ffmpeg progress test in short mode")
	}
	ffmpegPath, lookupErr := exec.LookPath("ffmpeg")
	if lookupErr != nil {
		t.Skipf("ffmpeg is not available: %v", lookupErr)
	}

	outputPath := filepath.Join(t.TempDir(), "progress.mp4")
	args := []string{
		"-hide_banner", "-loglevel", "error", "-nostats", "-y",
		"-f", "lavfi", "-i", "testsrc=duration=3:size=64x64:rate=10",
		"-c:v", "libx264", "-pix_fmt", "yuv420p",
		outputPath,
	}

	var percents []float64
	runErr := (&ExecCommandRunner{}).RunWithProgress(context.Background(), ffmpegPath, args, 3, func(percent float64, _ string) {
		percents = append(percents, percent)
	})
	if runErr != nil {
		t.Fatalf("ffmpeg progress run failed: %v", runErr)
	}
	if len(percents) == 0 {
		t.Fatal("expected at least one intermediate percent from real ffmpeg")
	}

	previous := 0.0
	for _, percent := range percents {
		if percent < previous || percent > 100 {
			t.Fatalf("percent %.2f is out of order or above 100 in %v", percent, percents)
		}
		previous = percent
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("expected ffmpeg to write the output: %v", statErr)
	}
}
