package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

// targetArgsRunner emulates ffmpeg for two-pass target tests. Pass 1 does
// nothing; pass 2 writes a file whose size the test chooses from the bitrate and
// attempt number.
type targetArgsRunner struct {
	calls          [][]string
	pass2Bitrates  []int
	pass2Attempts  int
	sizeForAttempt func(attempt int, bitrateKbps int) int
	onPass         func(ctx context.Context, pass, attempt, bitrateKbps int, args []string) error
}

func (r *targetArgsRunner) Run(ctx context.Context, _ string, args []string) error {
	r.calls = append(r.calls, append([]string(nil), args...))

	pass, _ := argValue(args, "-pass")
	bitrate, _ := argValue(args, "-b:v")
	bitrateKbps := parseKbps(bitrate)

	isPassTwo := pass == "2"
	attempt := r.pass2Attempts
	if !isPassTwo {
		attempt = r.pass2Attempts + 1
	}
	if r.onPass != nil {
		if err := r.onPass(ctx, atoiOrZero(pass), attempt, bitrateKbps, args); err != nil {
			return err
		}
	}
	if !isPassTwo {
		return nil
	}

	r.pass2Attempts++
	r.pass2Bitrates = append(r.pass2Bitrates, bitrateKbps)
	size := 4096
	if r.sizeForAttempt != nil {
		size = r.sizeForAttempt(r.pass2Attempts, bitrateKbps)
	}

	return os.WriteFile(args[len(args)-1], make([]byte, size), 0o644)
}

func argValue(args []string, flag string) (string, bool) {
	for index := 0; index+1 < len(args); index++ {
		if args[index] == flag {
			return args[index+1], true
		}
	}

	return "", false
}

func parseKbps(value string) int {
	trimmed := value
	if len(trimmed) > 0 && trimmed[len(trimmed)-1] == 'k' {
		trimmed = trimmed[:len(trimmed)-1]
	}
	parsed, err := strconv.Atoi(trimmed)
	if err != nil {
		return 0
	}

	return parsed
}

func atoiOrZero(value string) int {
	parsed, err := strconv.Atoi(value)
	if err != nil {
		return 0
	}

	return parsed
}

func targetTestSpec(outputPath string) TargetEncodeSpec {
	return TargetEncodeSpec{
		DurationSeconds: 10,
		FloorKbps:       300,
		OutputPath:      outputPath,
		BuildPassArgs: func(bitrateKbps, pass int, passLogPrefix, path string) ([]string, *VideoError) {
			args := []string{"-y", "-b:v", fmt.Sprintf("%dk", bitrateKbps), "-pass", strconv.Itoa(pass), "-passlogfile", passLogPrefix}
			if pass == 1 {
				return append(args, "-f", "null", os.DevNull), nil
			}

			return append(args, path), nil
		},
	}
}

func TestComputeTargetVideoBitrateKbps(t *testing.T) {
	tests := []struct {
		name      string
		target    int64
		duration  float64
		audioKbps int
		wantKbps  int
	}{
		{name: "with aac audio", target: 10_000_000, duration: 10, audioKbps: TargetAudioBitrateAACKbps, wantKbps: 7872},
		{name: "no audio", target: 10_000_000, duration: 10, audioKbps: 0, wantKbps: 8000},
		{name: "webm opus", target: 10_000_000, duration: 10, audioKbps: TargetAudioBitrateOpusKbps, wantKbps: 7904},
		{name: "audio exceeds target", target: 100_000, duration: 10, audioKbps: 128, wantKbps: 0},
		{name: "zero duration", target: 10_000_000, duration: 0, audioKbps: 128, wantKbps: 0},
		{name: "zero target", target: 0, duration: 10, audioKbps: 128, wantKbps: 0},
	}

	for _, testCase := range tests {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ComputeTargetVideoBitrateKbps(testCase.target, testCase.duration, testCase.audioKbps); got != testCase.wantKbps {
				t.Fatalf("bitrate = %d kbps, want %d", testCase.wantKbps, got)
			}
		})
	}
}

func TestTargetAudioKbpsSelectsCodecBudget(t *testing.T) {
	if got := TargetAudioKbps(TargetFormatMP4, true); got != TargetAudioBitrateAACKbps {
		t.Fatalf("mp4 audio budget = %d, want %d", got, TargetAudioBitrateAACKbps)
	}
	if got := TargetAudioKbps(TargetFormatWebM, true); got != TargetAudioBitrateOpusKbps {
		t.Fatalf("webm audio budget = %d, want %d", got, TargetAudioBitrateOpusKbps)
	}
	if got := TargetAudioKbps(TargetFormatMP4, false); got != 0 {
		t.Fatalf("missing audio budget = %d, want 0", got)
	}
}

func TestMinVideoBitrateKbpsFloorTable(t *testing.T) {
	tests := []struct {
		resolution string
		height     int
		want       int
	}{
		{resolution: Resolution480p, want: 500},
		{resolution: Resolution720p, want: 1000},
		{resolution: Resolution1080p, want: 2000},
		{resolution: ResolutionOriginal, height: 2160, want: 6000},
		{resolution: ResolutionOriginal, height: 1440, want: 4000},
		{resolution: ResolutionOriginal, height: 1080, want: 2000},
		{resolution: ResolutionOriginal, height: 720, want: 1000},
		{resolution: ResolutionOriginal, height: 480, want: 500},
		{resolution: ResolutionOriginal, height: 360, want: 300},
		{resolution: ResolutionOriginal, height: 0, want: 1000},
	}

	for _, testCase := range tests {
		if got := MinVideoBitrateKbps(testCase.resolution, testCase.height); got != testCase.want {
			t.Fatalf("floor(%s, %d) = %d, want %d", testCase.resolution, testCase.height, got, testCase.want)
		}
	}
}

func TestResolveTargetVideoBitrateAppliesFloor(t *testing.T) {
	bitrate, floorApplied := ResolveTargetVideoBitrate(1_000_000, 100, 128, 1000)
	if bitrate != 1000 || !floorApplied {
		t.Fatalf("expected the floor to apply (1000 kbps), got %d floorApplied=%v", bitrate, floorApplied)
	}

	bitrate, floorApplied = ResolveTargetVideoBitrate(50_000_000, 10, 128, 1000)
	if bitrate != 39872 || floorApplied {
		t.Fatalf("expected 39872 kbps without a floor, got %d floorApplied=%v", bitrate, floorApplied)
	}
}

func TestTargetEncodeRetriesReduceBitrateAndStopAtTwo(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.mp4")

	runner := &targetArgsRunner{sizeForAttempt: func(attempt, _ int) int {
		switch attempt {
		case 1:
			return 3_000_000
		case 2:
			return 2_500_000
		default:
			return 1_500_000
		}
	}}

	spec := targetTestSpec(outputPath)
	spec.TargetBytes = 2_000_000
	spec.DurationSeconds = 10
	spec.AudioKbps = 0
	spec.FloorKbps = 300

	outcome, encodeErr := TargetEncode(context.Background(), runner, spec)
	if encodeErr != nil {
		t.Fatalf("unexpected target encode error: %v", encodeErr)
	}
	if outcome.Attempts != 3 {
		t.Fatalf("attempts = %d, want 3 (initial + two retries)", outcome.Attempts)
	}
	if !outcome.TargetMet {
		t.Fatalf("expected the third attempt to meet the target, got %+v", outcome)
	}
	if len(runner.pass2Bitrates) != 3 {
		t.Fatalf("expected 3 pass-2 encodes, got %v", runner.pass2Bitrates)
	}
	if runner.pass2Bitrates[0] != 1600 || runner.pass2Bitrates[0] <= runner.pass2Bitrates[1] || runner.pass2Bitrates[1] <= runner.pass2Bitrates[2] {
		t.Fatalf("bitrates must decrease every retry from 1600, got %v", runner.pass2Bitrates)
	}
}

func TestTargetEncodeImpossibleTargetReportsBestEffortAtFloor(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.mp4")

	runner := &targetArgsRunner{sizeForAttempt: func(int, int) int { return 5000 }}
	spec := targetTestSpec(outputPath)
	spec.TargetBytes = 1000
	spec.FloorKbps = 1000

	outcome, encodeErr := TargetEncode(context.Background(), runner, spec)
	if encodeErr != nil {
		t.Fatalf("unexpected target encode error: %v", encodeErr)
	}
	if !outcome.FloorApplied {
		t.Fatal("expected the quality floor to be applied")
	}
	if outcome.BitrateKbps != 1000 {
		t.Fatalf("bitrate = %d, want the 1000 kbps floor", outcome.BitrateKbps)
	}
	if outcome.TargetMet {
		t.Fatal("an impossible target must not be reported as met")
	}
	if outcome.ActualBytes != 5000 {
		t.Fatalf("actual bytes = %d, want 5000", outcome.ActualBytes)
	}
}

func TestTargetEncodeCancellationDuringPassOneCleansPassLogAndOutput(t *testing.T) {
	dir := t.TempDir()
	outputPath := filepath.Join(dir, "out.mp4")

	started := make(chan string, 1)
	runner := &targetArgsRunner{onPass: func(ctx context.Context, pass, _ int, _ int, args []string) error {
		if prefix, ok := argValue(args, "-passlogfile"); ok {
			_ = os.WriteFile(prefix+"-0.log", []byte("stats"), 0o644)
			_ = os.MkdirAll(prefix+"-0.log.mbtree", 0o755)
			select {
			case started <- filepath.Dir(prefix):
			default:
			}
		}
		if pass == 1 {
			<-ctx.Done()
			return ctx.Err()
		}

		return nil
	}}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan *VideoError, 1)
	go func() {
		spec := targetTestSpec(outputPath)
		spec.TargetBytes = 5_000_000
		_, err := TargetEncode(ctx, runner, spec)
		done <- err
	}()

	var passLogDir string
	select {
	case passLogDir = <-started:
	case <-time.After(2 * time.Second):
		t.Fatal("pass 1 never started")
	}

	cancel()
	var encodeErr *VideoError
	select {
	case encodeErr = <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("cancelled target encode did not return promptly")
	}
	if encodeErr == nil || encodeErr.Code != "CANCELED" {
		t.Fatalf("expected CANCELED, got %+v", encodeErr)
	}
	if _, statErr := os.Stat(passLogDir); !os.IsNotExist(statErr) {
		t.Fatalf("pass log directory was left behind: %v", statErr)
	}
	if _, statErr := os.Stat(outputPath); !os.IsNotExist(statErr) {
		t.Fatalf("cancelled encode left a partial output: %v", statErr)
	}
}
