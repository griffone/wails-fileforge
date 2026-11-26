package engine

import (
	"context"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

func requireTargetMediaTools(t *testing.T) (string, string) {
	t.Helper()

	if testing.Short() {
		t.Skip("skipping real ffmpeg target tests in short mode")
	}
	ffmpegPath, ffmpegErr := exec.LookPath("ffmpeg")
	if ffmpegErr != nil {
		t.Skipf("ffmpeg is not available: %v", ffmpegErr)
	}
	ffprobePath, ffprobeErr := exec.LookPath("ffprobe")
	if ffprobeErr != nil {
		t.Skipf("ffprobe is not available: %v", ffprobeErr)
	}

	return ffmpegPath, ffprobePath
}

func generateTargetClip(t *testing.T, ffmpegPath, path string, durationSeconds float64, withAudio bool) {
	t.Helper()

	args := []string{
		"-hide_banner", "-loglevel", "error", "-y",
		"-f", "lavfi", "-i", fmt.Sprintf("testsrc=duration=%g:size=320x240:rate=15", durationSeconds),
	}
	if withAudio {
		args = append(args,
			"-f", "lavfi", "-i", fmt.Sprintf("sine=frequency=440:duration=%g", durationSeconds),
			"-c:a", "aac", "-b:a", "96k", "-shortest",
		)
	}
	args = append(args, "-c:v", "libx264", "-preset", "ultrafast", "-pix_fmt", "yuv420p", path)

	output, runErr := exec.Command(ffmpegPath, args...).CombinedOutput()
	if runErr != nil {
		t.Fatalf("generate clip failed: %v: %s", runErr, strings.TrimSpace(string(output)))
	}
}

func probeTargetDuration(t *testing.T, ffprobePath, path string) float64 {
	t.Helper()

	output, runErr := exec.Command(ffprobePath,
		"-v", "error",
		"-show_entries", "format=duration",
		"-of", "default=noprint_wrappers=1:nokey=1",
		path,
	).Output()
	if runErr != nil {
		t.Fatalf("ffprobe duration failed: %v", runErr)
	}
	duration, parseErr := strconv.ParseFloat(strings.TrimSpace(string(output)), 64)
	if parseErr != nil {
		t.Fatalf("parse ffprobe duration %q: %v", strings.TrimSpace(string(output)), parseErr)
	}

	return duration
}

func targetFileSize(t *testing.T, path string) int64 {
	t.Helper()

	info, statErr := os.Stat(path)
	if statErr != nil {
		t.Fatalf("stat output %s: %v", path, statErr)
	}

	return info.Size()
}

func TestTargetSizeReachableProducesOutputUnderTarget(t *testing.T) {
	ffmpegPath, ffprobePath := requireTargetMediaTools(t)
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	output := filepath.Join(dir, "output.mp4")
	generateTargetClip(t, ffmpegPath, input, 2, true)

	req := ConvertRequest{
		InputPath:       input,
		OutputPath:      output,
		TargetFormat:    TargetFormatMP4,
		QualityPreset:   QualityPresetMedium,
		Resolution:      ResolutionOriginal,
		TargetSizeBytes: 400_000,
	}

	outcome, convertErr := ConvertWithOutcome(context.Background(), NewFFmpegRuntimeProbe(), &ExecCommandRunner{}, req, nil)
	if convertErr != nil {
		t.Fatalf("target convert failed: %v", convertErr)
	}
	if !outcome.TargetMet {
		t.Fatalf("expected the reachable target to be met, got %+v", outcome)
	}
	if size := targetFileSize(t, output); size > req.TargetSizeBytes {
		t.Fatalf("output is %d bytes, above the %d byte target", size, req.TargetSizeBytes)
	}
	if outcome.BitrateKbps <= outcome.FloorKbps {
		t.Fatalf("expected a bitrate above the floor, got %+v", outcome)
	}

	duration := probeTargetDuration(t, ffprobePath, output)
	if math.Abs(duration-2) > 0.35 {
		t.Fatalf("output duration = %.2f s, want about 2 s", duration)
	}
}

func TestTargetSizeImpossibleReportsBestEffortAtFloor(t *testing.T) {
	ffmpegPath, ffprobePath := requireTargetMediaTools(t)
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	output := filepath.Join(dir, "output.mp4")
	generateTargetClip(t, ffmpegPath, input, 2, true)

	req := ConvertRequest{
		InputPath:       input,
		OutputPath:      output,
		TargetFormat:    TargetFormatMP4,
		QualityPreset:   QualityPresetMedium,
		Resolution:      ResolutionOriginal,
		TargetSizeBytes: 5_000,
	}

	outcome, convertErr := ConvertWithOutcome(context.Background(), NewFFmpegRuntimeProbe(), &ExecCommandRunner{}, req, nil)
	if convertErr != nil {
		t.Fatalf("best-effort target convert failed: %v", convertErr)
	}
	if !outcome.FloorApplied {
		t.Fatalf("expected the floor to be applied, got %+v", outcome)
	}
	if outcome.TargetMet {
		t.Fatalf("an impossible target must not be reported as met, got %+v", outcome)
	}
	if size := targetFileSize(t, output); size <= req.TargetSizeBytes {
		t.Fatalf("expected a best-effort output above %d bytes, got %d", req.TargetSizeBytes, size)
	}
	if outcome.ActualBytes != targetFileSize(t, output) {
		t.Fatalf("reported actual bytes %d do not match the file", outcome.ActualBytes)
	}
	if duration := probeTargetDuration(t, ffprobePath, output); math.Abs(duration-2) > 0.35 {
		t.Fatalf("best-effort output duration = %.2f s, want about 2 s", duration)
	}
}

func TestTrimTargetUsesRealOutputDuration(t *testing.T) {
	ffmpegPath, ffprobePath := requireTargetMediaTools(t)
	dir := t.TempDir()
	input := filepath.Join(dir, "input.mp4")
	output := filepath.Join(dir, "output.mp4")
	generateTargetClip(t, ffmpegPath, input, 2, true)

	req := TrimRequest{
		InputPath:       input,
		OutputPath:      output,
		StartTime:       0.5,
		EndTime:         1.5,
		TargetFormat:    TargetFormatMP4,
		QualityPreset:   QualityPresetMedium,
		TrimMode:        TrimModeCopy,
		TargetSizeBytes: 150_000,
	}

	outcome, trimErr := TrimWithOutcome(context.Background(), NewFFmpegRuntimeProbe(), &ExecCommandRunner{}, req, nil)
	if trimErr != nil {
		t.Fatalf("target trim failed: %v", trimErr)
	}
	if !outcome.TargetMet {
		t.Fatalf("expected the trim target to be met, got %+v", outcome)
	}
	if size := targetFileSize(t, output); size > req.TargetSizeBytes {
		t.Fatalf("trim output is %d bytes, above the %d byte target", size, req.TargetSizeBytes)
	}
	if duration := probeTargetDuration(t, ffprobePath, output); math.Abs(duration-1) > 0.3 {
		t.Fatalf("trim output duration = %.2f s, want about 1 s", duration)
	}
}

func TestMergeTargetUsesSummedInputDuration(t *testing.T) {
	ffmpegPath, ffprobePath := requireTargetMediaTools(t)
	dir := t.TempDir()
	first := filepath.Join(dir, "first.mp4")
	second := filepath.Join(dir, "second.mp4")
	output := filepath.Join(dir, "merged.mp4")
	generateTargetClip(t, ffmpegPath, first, 0.8, true)
	generateTargetClip(t, ffmpegPath, second, 0.8, true)

	req := MergeRequest{
		InputPaths:      []string{first, second},
		OutputPath:      output,
		TargetFormat:    TargetFormatMP4,
		QualityPreset:   QualityPresetMedium,
		MergeMode:       MergeModeCopy,
		TargetSizeBytes: 250_000,
	}

	outcome, mergeErr := MergeWithOutcome(context.Background(), NewFFmpegRuntimeProbe(), &ExecCommandRunner{}, req, nil)
	if mergeErr != nil {
		t.Fatalf("target merge failed: %v", mergeErr)
	}
	if !outcome.TargetMet {
		t.Fatalf("expected the merge target to be met, got %+v", outcome)
	}
	if size := targetFileSize(t, output); size > req.TargetSizeBytes {
		t.Fatalf("merge output is %d bytes, above the %d byte target", size, req.TargetSizeBytes)
	}
	if duration := probeTargetDuration(t, ffprobePath, output); math.Abs(duration-1.6) > 0.35 {
		t.Fatalf("merge output duration = %.2f s, want about 1.6 s", duration)
	}
}
