package ffmpegex

import (
	"context"
	"fmt"
	"math"
	"strconv"
	"strings"

	"fileforge-desktop/internal/platform/execx"
)

const (
	// ffmpegProgressOption and ffmpegProgressTarget turn on the machine-readable
	// progress stream. With -nostats already in the global arguments, stdout
	// carries only the key=value progress blocks.
	ffmpegProgressOption = "-progress"
	ffmpegProgressTarget = "pipe:1"
)

// FFmpegProgressSample is one parsed line of ffmpeg's `-progress pipe:1`
// stream. Only the fields ffmpeg reports are modelled: a rendered position in
// microseconds and the end marker.
type FFmpegProgressSample struct {
	// Microseconds is the rendered output position. It is zero on the end line.
	Microseconds int64
	// Done is true when the stream reported progress=end.
	Done bool
}

// ProgressCommandRunner runs one ffmpeg command and streams progress while it
// runs. durationSeconds is the expected output length used to turn ffmpeg's
// reported position into a percentage; zero disables percentage reporting while
// the command still runs. onProgress receives monotonic percentages in 0..100.
//
// It extends CommandRunner instead of replacing it: ExecCommandRunner implements
// both, and a runner that only implements Run keeps working through the coarse
// fallback in RunWithProgress.
type ProgressCommandRunner interface {
	RunWithProgress(ctx context.Context, name string, args []string, durationSeconds float64, onProgress func(percent float64, message string)) error
}

// CaptureCommandRunner runs one ffmpeg command and returns its stdout and the
// bounded stderr tail in addition to the run error. stdout is collected line by
// line; when onProgress is set, the `-progress pipe:1` blocks are consumed from
// that same stream. stderr keeps the tail because filters such as loudnorm print
// their JSON summary there near the end of the run.
type CaptureCommandRunner interface {
	RunCapture(ctx context.Context, name string, args []string, durationSeconds float64, onProgress func(percent float64, message string)) (stdout string, stderr string, err error)
}

// ParseFFmpegProgress parses one line of ffmpeg's -progress output. It accepts
// out_time_us and out_time_ms (both carry microseconds), out_time=HH:MM:SS.fff
// and progress=end. ok is false for every other line, including malformed
// values and progress=continue.
func ParseFFmpegProgress(line string) (FFmpegProgressSample, bool) {
	key, value, found := strings.Cut(line, "=")
	if !found {
		return FFmpegProgressSample{}, false
	}

	switch strings.TrimSpace(key) {
	case "out_time_us", "out_time_ms":
		microseconds, err := strconv.ParseInt(strings.TrimSpace(value), 10, 64)
		if err != nil || microseconds < 0 {
			return FFmpegProgressSample{}, false
		}
		return FFmpegProgressSample{Microseconds: microseconds}, true
	case "out_time":
		microseconds, ok := ParseFFmpegTimestamp(strings.TrimSpace(value))
		if !ok {
			return FFmpegProgressSample{}, false
		}
		return FFmpegProgressSample{Microseconds: microseconds}, true
	case "progress":
		if strings.EqualFold(strings.TrimSpace(value), "end") {
			return FFmpegProgressSample{Done: true}, true
		}
	}

	return FFmpegProgressSample{}, false
}

// ParseFFmpegTimestamp parses ffmpeg's HH:MM:SS.ffffff timestamps into
// microseconds. Hours may exceed 23 for long inputs; minutes and seconds must
// be valid. A value of N/A and other malformed input return ok=false.
func ParseFFmpegTimestamp(value string) (int64, bool) {
	parts := strings.Split(value, ":")
	if len(parts) != 3 {
		return 0, false
	}

	hours, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || hours < 0 || hours > 100000 {
		return 0, false
	}

	minutes, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil || minutes < 0 || minutes > 59 {
		return 0, false
	}

	secondsText, fractionText, hasFraction := strings.Cut(parts[2], ".")
	seconds, err := strconv.ParseInt(secondsText, 10, 64)
	if err != nil || seconds < 0 || seconds > 59 {
		return 0, false
	}

	var fraction int64
	if hasFraction {
		if fractionText == "" || len(fractionText) > 6 {
			return 0, false
		}
		digits := fractionText + strings.Repeat("0", 6-len(fractionText))
		fraction, err = strconv.ParseInt(digits, 10, 64)
		if err != nil {
			return 0, false
		}
	}

	return (hours*3600+minutes*60+seconds)*1_000_000 + fraction, true
}

// ProgressReporter converts positions into monotonic percentages. It is not
// safe for concurrent use; the runner feeds it from the single stdout scanner
// goroutine.
type ProgressReporter struct {
	durationSeconds float64
	lastPercent     float64
	hasPercent      bool
}

// NewProgressReporter creates a reporter for an expected output length. A zero,
// negative or non-finite duration disables percentage reporting.
func NewProgressReporter(durationSeconds float64) *ProgressReporter {
	if math.IsNaN(durationSeconds) || math.IsInf(durationSeconds, 0) || durationSeconds <= 0 {
		durationSeconds = 0
	}

	return &ProgressReporter{durationSeconds: durationSeconds}
}

// Percent maps a sample onto 0..100. ok is false when the duration is unknown,
// the position is unusable, or the value would move the bar backwards (ffmpeg
// can report a slightly smaller out_time between blocks).
func (r *ProgressReporter) Percent(sample FFmpegProgressSample) (float64, bool) {
	if r == nil || sample.Done || sample.Microseconds <= 0 || r.durationSeconds <= 0 {
		return 0, false
	}

	percent := clampPercent(float64(sample.Microseconds) / (r.durationSeconds * 1_000_000) * 100)
	if r.hasPercent && percent <= r.lastPercent {
		return 0, false
	}

	r.hasPercent = true
	r.lastPercent = percent

	return percent, true
}

// NewProgressHandler builds the stdout line handler used by the real runner. It
// is also the seam engine tests use to feed a fake progress stream.
func NewProgressHandler(durationSeconds float64, onProgress func(percent float64, message string)) func(line string) {
	reporter := NewProgressReporter(durationSeconds)

	return func(line string) {
		sample, ok := ParseFFmpegProgress(line)
		if !ok || sample.Done {
			return
		}

		percent, ok := reporter.Percent(sample)
		if !ok {
			return
		}
		if onProgress != nil {
			onProgress(percent, fmt.Sprintf("Encoding… %.0f%%", percent))
		}
	}
}

// WithProgressArgs injects -progress pipe:1 and -nostats when the caller did
// not add them. Both are global ffmpeg options, so prepending keeps output
// arguments last.
func WithProgressArgs(args []string) []string {
	prefix := make([]string, 0, 4)
	if !hasFFmpegFlag(args, ffmpegProgressOption) {
		prefix = append(prefix, ffmpegProgressOption, ffmpegProgressTarget)
	}
	if !hasFFmpegFlag(args, "-nostats") {
		prefix = append(prefix, "-nostats")
	}
	if len(prefix) == 0 {
		return args
	}

	combined := make([]string, 0, len(prefix)+len(args))
	combined = append(combined, prefix...)
	combined = append(combined, args...)

	return combined
}

func hasFFmpegFlag(args []string, flag string) bool {
	for _, arg := range args {
		if arg == flag {
			return true
		}
	}

	return false
}

// RunWithProgress runs ffmpeg through execx, which keeps the process-group
// cancellation path and the bounded stderr tail. Only the progress stream is
// new. A runner without progress support still runs the command unchanged; the
// caller then sees only the completion value.
func RunWithProgress(ctx context.Context, runner CommandRunner, args []string, durationSeconds float64, onProgress func(percent float64, message string)) error {
	if onProgress == nil {
		return runner.Run(ctx, "ffmpeg", args)
	}

	var err error
	if progressRunner, ok := runner.(ProgressCommandRunner); ok {
		err = progressRunner.RunWithProgress(ctx, "ffmpeg", args, durationSeconds, onProgress)
	} else {
		err = runner.Run(ctx, "ffmpeg", args)
	}
	if err != nil {
		return err
	}

	onProgress(100, "completed")

	return nil
}

// RunWithProgress runs ffmpeg through execx and streams monotonic percentages
// while the command runs.
func (r *ExecCommandRunner) RunWithProgress(ctx context.Context, name string, args []string, durationSeconds float64, onProgress func(percent float64, message string)) error {
	result, err := execx.Run(ctx, execx.Command{
		Path:            name,
		Args:            WithProgressArgs(args),
		Stdout:          NewProgressHandler(durationSeconds, onProgress),
		StderrTailLines: StderrTailLines,
		StderrTailBytes: StderrTailBytes,
	})

	return CommandRunError(result, err)
}

// RunCapture runs one ffmpeg command, collects its stdout lines and returns the
// bounded stderr tail. Progress blocks are consumed from stdout when onProgress
// is set. The stderr tail is returned even on failure so a caller can surface a
// filter report or a decoder complaint.
func (r *ExecCommandRunner) RunCapture(ctx context.Context, name string, args []string, durationSeconds float64, onProgress func(percent float64, message string)) (string, string, error) {
	var stdout strings.Builder
	handler := NewProgressHandler(durationSeconds, onProgress)

	runArgs := args
	if onProgress != nil {
		runArgs = WithProgressArgs(args)
	}

	result, err := execx.Run(ctx, execx.Command{
		Path: name,
		Args: runArgs,
		Stdout: func(line string) {
			stdout.WriteString(line)
			stdout.WriteByte('\n')
			handler(line)
		},
		StderrTailLines: CaptureStderrTailLines,
		StderrTailBytes: CaptureStderrTailBytes,
	})
	if err != nil {
		return stdout.String(), result.Stderr, CommandRunError(result, err)
	}

	if onProgress != nil {
		onProgress(100, "completed")
	}

	return stdout.String(), result.Stderr, nil
}

func clampPercent(percent float64) float64 {
	if math.IsNaN(percent) || percent < 0 {
		return 0
	}
	if percent > 100 {
		return 100
	}

	return percent
}
