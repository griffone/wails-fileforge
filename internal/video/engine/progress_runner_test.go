package engine

import (
	"context"
	"os"
	"reflect"
	"testing"
)

// fakeProgressRunner emulates ffmpeg with a canned progress stream per call. It
// implements both CommandRunner and ProgressCommandRunner so tests cover the
// real wiring without a process.
type fakeProgressRunner struct {
	lines     [][]string
	durations []float64
	calls     int
}

func (r *fakeProgressRunner) Run(_ context.Context, _ string, args []string) error {
	r.calls++
	return writeFakeProgressOutput(args)
}

func (r *fakeProgressRunner) RunWithProgress(_ context.Context, _ string, args []string, durationSeconds float64, onProgress func(percent float64, message string)) error {
	index := r.calls
	r.calls++
	r.durations = append(r.durations, durationSeconds)

	handler := newFFmpegProgressHandler(durationSeconds, onProgress)
	if index < len(r.lines) {
		for _, line := range r.lines[index] {
			handler(line)
		}
	}

	return writeFakeProgressOutput(args)
}

func writeFakeProgressOutput(args []string) error {
	if len(args) == 0 {
		return nil
	}

	return os.WriteFile(args[len(args)-1], []byte("fixture"), 0o644)
}

func stubProgressSourceDuration(t *testing.T, duration float64, ok bool) {
	t.Helper()

	original := SourceDurationProbe
	t.Cleanup(func() {
		SourceDurationProbe = original
	})

	SourceDurationProbe = func(context.Context, string) (float64, bool) {
		return duration, ok
	}
}

func TestConvertWithProgressStreamsMonotonicPercent(t *testing.T) {
	inputPath, outputPath := writeConvertFixtures(t)
	stubProgressSourceDuration(t, 10, true)

	runner := &fakeProgressRunner{lines: [][]string{{
		"out_time_ms=2500000",
		"out_time_us=5000000",
		"out_time=00:00:07.500000",
		"out_time_ms=6000000", // out of order, must be dropped
		"progress=end",
	}}}

	var percents []float64
	convertErr := ConvertWithProgress(context.Background(), stubMergeRuntimeProbe{}, runner, convertTestRequest(inputPath, outputPath), func(percent float64, _ string) {
		percents = append(percents, percent)
	})
	if convertErr != nil {
		t.Fatalf("expected the conversion to succeed, got %v", convertErr)
	}

	if want := []float64{25, 50, 75, 100}; !reflect.DeepEqual(percents, want) {
		t.Fatalf("percents = %v, want %v", percents, want)
	}
	if want := []float64{10}; !reflect.DeepEqual(runner.durations, want) {
		t.Fatalf("durations = %v, want %v", runner.durations, want)
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("expected the published output %q: %v", outputPath, statErr)
	}
}

func TestConvertWithProgressSkipsPercentWhenDurationIsUnknown(t *testing.T) {
	inputPath, outputPath := writeConvertFixtures(t)
	stubProgressSourceDuration(t, 0, false)

	runner := &fakeProgressRunner{lines: [][]string{{
		"out_time_ms=2500000",
		"out_time_ms=5000000",
	}}}

	var percents []float64
	convertErr := ConvertWithProgress(context.Background(), stubMergeRuntimeProbe{}, runner, convertTestRequest(inputPath, outputPath), func(percent float64, _ string) {
		percents = append(percents, percent)
	})
	if convertErr != nil {
		t.Fatalf("expected the conversion to succeed, got %v", convertErr)
	}

	if want := []float64{100}; !reflect.DeepEqual(percents, want) {
		t.Fatalf("percents = %v, want only the completion value %v", percents, want)
	}
	if want := []float64{0}; !reflect.DeepEqual(runner.durations, want) {
		t.Fatalf("durations = %v, want %v", runner.durations, want)
	}
}

func TestMergeWithProgressMeasuresAgainstSummedInputDurations(t *testing.T) {
	req := mergeValidationRequest(t)
	stubProgressSourceDuration(t, 10, true)

	runner := &fakeProgressRunner{lines: [][]string{{
		"out_time_ms=5000000",
		"out_time_ms=15000000",
	}}}

	var percents []float64
	mergeErr := MergeWithProgress(context.Background(), stubMergeRuntimeProbe{}, runner, req, func(percent float64, _ string) {
		percents = append(percents, percent)
	})
	if mergeErr != nil {
		t.Fatalf("expected the merge to succeed, got %v", mergeErr)
	}

	if want := []float64{25, 75, 100}; !reflect.DeepEqual(percents, want) {
		t.Fatalf("percents = %v, want %v", percents, want)
	}
	if want := []float64{20}; !reflect.DeepEqual(runner.durations, want) {
		t.Fatalf("durations = %v, want the summed 20s", runner.durations)
	}
}

func TestMergeWithoutProgressRunnerFallsBackToCoarseProgress(t *testing.T) {
	req := mergeValidationRequest(t)
	runner := &stubMergeRunner{}

	var percents []float64
	mergeErr := MergeWithProgress(context.Background(), stubMergeRuntimeProbe{}, runner, req, func(percent float64, _ string) {
		percents = append(percents, percent)
	})
	if mergeErr != nil {
		t.Fatalf("expected the merge to succeed, got %v", mergeErr)
	}

	if want := []float64{100}; !reflect.DeepEqual(percents, want) {
		t.Fatalf("percents = %v, want only the completion value %v", percents, want)
	}
}
