package engine

import (
	"context"
	"math"
	"strings"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/platform/ffmpegex"
)

// SourceDurationProbe reports a media duration in seconds. It is a variable so
// tests can supply durations without ffprobe. The production value is the same
// tolerant helper used by file inspection: an unreadable file returns ok=false
// instead of failing the run.
var SourceDurationProbe = inspect.ProbeVideoDuration

// FFmpegProgressSample, ProgressCommandRunner and TrimProgressRunner keep the
// video contract names while the parsing lives in shared infrastructure.
type FFmpegProgressSample = ffmpegex.FFmpegProgressSample

// ProgressCommandRunner runs one ffmpeg command and streams progress.
type ProgressCommandRunner = ffmpegex.ProgressCommandRunner

// TrimProgressRunner is kept as an alias so trim keeps its historical contract
// name while sharing one implementation.
type TrimProgressRunner = ffmpegex.ProgressCommandRunner

func ParseFFmpegProgress(line string) (FFmpegProgressSample, bool) {
	return ffmpegex.ParseFFmpegProgress(line)
}

func newFFmpegProgressReporter(durationSeconds float64) *ffmpegex.ProgressReporter {
	return ffmpegex.NewProgressReporter(durationSeconds)
}

func newFFmpegProgressHandler(durationSeconds float64, onProgress func(percent float64, message string)) func(line string) {
	return ffmpegex.NewProgressHandler(durationSeconds, onProgress)
}

func withFFmpegProgressArgs(args []string) []string {
	return ffmpegex.WithProgressArgs(args)
}

func parseFFmpegTimestamp(value string) (int64, bool) {
	return ffmpegex.ParseFFmpegTimestamp(value)
}

// runFFmpegWithProgress runs one command and streams percent updates when the
// runner supports them.
func runFFmpegWithProgress(ctx context.Context, runner CommandRunner, args []string, durationSeconds float64, onProgress func(percent float64, message string)) error {
	return ffmpegex.RunWithProgress(ctx, runner, args, durationSeconds, onProgress)
}

// probeSourceDuration returns the duration used for percentage reporting, or
// zero when ffprobe cannot read it.
func probeSourceDuration(ctx context.Context, path string) float64 {
	duration, ok := SourceDurationProbe(ctx, strings.TrimSpace(path))
	if !ok || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return 0
	}

	return duration
}
