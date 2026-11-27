package analysis

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"fileforge-desktop/internal/platform/execx"
	"fileforge-desktop/internal/platform/ffmpegex"
)

const (
	// decodeSampleRate is the only rate the analyzer decodes to. 1 kHz keeps
	// onsets and speech envelopes (Nyquist 500 Hz) and costs about 7.2 MB per
	// hour per file.
	decodeSampleRate = 1000

	// MaxDecodeHours bounds a single decoded file. Longer inputs are refused
	// instead of exhausting memory; decoding happens once, front to back, so
	// the bound applies to the entire file.
	MaxDecodeHours = 6

	// ffmpegBinary is resolved through PATH by the runner.
	ffmpegBinary = "ffmpeg"

	bytesPerSample = 2
	// maxDecodeSamples is MaxDecodeHours at decodeSampleRate.
	maxDecodeSamples = int64(MaxDecodeHours) * 3600 * decodeSampleRate
)

// PCM is one decoded file: mono int16 samples at SampleRate hertz.
type PCM struct {
	Samples    []int16
	SampleRate int
}

// DurationSeconds returns the decoded length in seconds.
func (p *PCM) DurationSeconds() float64 {
	if p == nil || p.SampleRate <= 0 {
		return 0
	}

	return float64(len(p.Samples)) / float64(p.SampleRate)
}

// DecodeMono decodes the first audio stream of path to 1 kHz mono int16 in one
// front-to-back pass. It never seeks: a seek inside a long file introduces a
// packet bias that would turn into a systematic offset error.
func DecodeMono(ctx context.Context, runner Runner, path string) (*PCM, *Error) {
	pcm, analysisErr := decodePCM(ctx, runner, path, 0, 0)
	if analysisErr != nil {
		return nil, analysisErr
	}
	if len(pcm.Samples) == 0 {
		return nil, &Error{Code: CodeNoAudio, Message: "the file has no audio samples"}
	}

	return pcm, nil
}

// decodePCM decodes a span of path to 1 kHz mono int16. startSeconds and
// spanSeconds are zero for a whole-file pass; Peaks uses them for the one
// place where a seek is acceptable. An empty result is not an error here:
// DecodeMono turns it into CodeNoAudio and Peaks treats it as an empty span.
func decodePCM(ctx context.Context, runner Runner, path string, startSeconds, spanSeconds float64) (*PCM, *Error) {
	trimmed := strings.TrimSpace(path)
	if trimmed == "" {
		return nil, &Error{Code: CodeInputInvalid, Message: "an input path is required"}
	}
	if runner == nil {
		return nil, &Error{Code: CodeRuntimeUnavailable, Message: "the analysis runner is not configured"}
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, &Error{Code: CodeCanceled, Message: "the analysis was canceled", Cause: err}
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		return nil, &Error{
			Code:    CodeInputInvalid,
			Message: fmt.Sprintf("the input file is not available: %s", trimmed),
			Cause:   err,
		}
	}
	if !info.Mode().IsRegular() {
		return nil, &Error{
			Code:    CodeInputInvalid,
			Message: fmt.Sprintf("the input path is not a regular file: %s", trimmed),
		}
	}

	tempDir, err := os.MkdirTemp("", "fileforge-analysis-*")
	if err != nil {
		return nil, &Error{
			Code:    CodeDecodeFailed,
			Message: "a temporary decode directory could not be created",
			Cause:   err,
		}
	}
	defer os.RemoveAll(tempDir)

	outputPath := filepath.Join(tempDir, "audio.pcm")
	stderr, runErr := runner.Run(ctx, ffmpegBinary, decodeArgs(trimmed, outputPath, startSeconds, spanSeconds))
	if runErr != nil {
		return nil, classifyRunError(ctx, runErr, stderr)
	}

	decoded, err := os.Stat(outputPath)
	if err != nil {
		return nil, &Error{
			Code:    CodeDecodeFailed,
			Message: "ffmpeg finished without producing audio",
			Cause:   err,
		}
	}
	if decoded.Size()/bytesPerSample > maxDecodeSamples {
		return nil, &Error{
			Code:    CodeTooLong,
			Message: fmt.Sprintf("the file is longer than the %d hour analysis limit", MaxDecodeHours),
		}
	}

	samples, err := readPCMSamples(outputPath)
	if err != nil {
		return nil, &Error{
			Code:    CodeDecodeFailed,
			Message: "the decoded audio could not be read",
			Cause:   err,
		}
	}

	return &PCM{Samples: samples, SampleRate: decodeSampleRate}, nil
}

// decodeArgs builds the ffmpeg command. With zero start and span this is the
// exact single-pass command the annex specifies. -ss before -i is an input
// seek, used only by Peaks where timing does not matter.
func decodeArgs(path, outputPath string, startSeconds, spanSeconds float64) []string {
	args := make([]string, 0, 24)
	args = append(args, "-y", "-hide_banner", "-loglevel", "error", "-nostdin")
	if startSeconds > 0 {
		args = append(args, "-ss", formatSeconds(startSeconds))
	}
	args = append(args,
		"-i", path,
		"-map", "0:a:0",
		"-ac", "1",
		"-ar", strconv.Itoa(decodeSampleRate),
	)
	if spanSeconds > 0 {
		args = append(args, "-t", formatSeconds(spanSeconds))
	}
	args = append(args, "-f", "s16le", "-acodec", "pcm_s16le", outputPath)

	return args
}

// formatSeconds writes millisecond precision, enough for a decode range and
// stable enough to keep command assertions readable.
func formatSeconds(value float64) string {
	return strconv.FormatFloat(value, 'f', 3, 64)
}

// readPCMSamples reads a little-endian int16 file. An odd trailing byte is
// ignored because a truncated ffmpeg write should not fail the whole analysis.
func readPCMSamples(path string) ([]int16, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	count := len(data) / bytesPerSample
	if count == 0 {
		return nil, nil
	}

	samples := make([]int16, count)
	for index := 0; index < count; index++ {
		offset := index * bytesPerSample
		samples[index] = int16(binary.LittleEndian.Uint16(data[offset : offset+bytesPerSample]))
	}

	return samples, nil
}

// classifyRunError maps a failed ffmpeg run onto a stable detail code. The
// context is checked first so a cancellation is never reported as a decode or
// runtime problem.
func classifyRunError(ctx context.Context, err error, stderr string) *Error {
	if ctx != nil && ctx.Err() != nil {
		return &Error{Code: CodeCanceled, Message: "ffmpeg was canceled", Cause: ctx.Err()}
	}
	if err == nil {
		return nil
	}
	if isRuntimeUnavailable(err) {
		return &Error{
			Code:    CodeRuntimeUnavailable,
			Message: "ffmpeg is not available; install it or configure the media runtime",
			Cause:   err,
		}
	}
	if IsNoAudioStderr(stderr) {
		return &Error{Code: CodeNoAudio, Message: "the file has no audio stream", Cause: err}
	}
	if isKilledProcess(err) {
		return &Error{Code: CodeCanceled, Message: "the ffmpeg process was stopped", Cause: err}
	}

	message := "ffmpeg could not decode the audio"
	if tail := strings.TrimSpace(stderr); tail != "" {
		return &Error{Code: CodeDecodeFailed, Message: message, Cause: fmt.Errorf("%w: %s", err, tail)}
	}

	return &Error{Code: CodeDecodeFailed, Message: message, Cause: err}
}

// IsNoAudioStderr reports whether ffmpeg's bounded stderr tail says the mapped
// audio stream does not exist. It is exported because Peaks treats the same
// condition as "no audio" instead of an error.
func IsNoAudioStderr(stderr string) bool {
	lowered := strings.ToLower(stderr)
	switch {
	case strings.Contains(lowered, "matches no streams"):
		return true
	case strings.Contains(lowered, "does not contain any stream"):
		return true
	case strings.Contains(lowered, "no audio stream"):
		return true
	}

	return false
}

// isRuntimeUnavailable reports a process that never started because ffmpeg is
// absent or cannot be executed.
func isRuntimeUnavailable(err error) bool {
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}

	var execErr *execx.Error
	if errors.As(err, &execErr) && execErr.Path != "" && execErr.ExitCode < 0 {
		// execx sets Path only when the start failed; a wait failure leaves it
		// empty. A start failure is a runtime problem, not a bad input.
		return true
	}

	lowered := strings.ToLower(err.Error())
	return strings.Contains(lowered, "executable file not found") ||
		strings.Contains(lowered, "no such file or directory")
}

// isKilledProcess reports a process killed by a signal while the context was
// still alive.
func isKilledProcess(err error) bool {
	var execErr *execx.Error
	if errors.As(err, &execErr) && execErr.Path == "" && execErr.ExitCode < 0 {
		return true
	}

	lowered := strings.ToLower(err.Error())
	return strings.Contains(lowered, "signal: killed") || strings.Contains(lowered, "signal: terminated")
}

// probeDurationSeconds reads the container duration from ffmpeg's input report.
// ffmpeg exits non-zero when it has no output, but it prints the Duration line
// to stderr first and the bounded tail keeps it. The probe never decodes audio.
// ok is false when the container does not expose a duration.
func probeDurationSeconds(ctx context.Context, runner Runner, path string) (float64, bool) {
	if runner == nil {
		return 0, false
	}

	stderr, _ := runner.Run(ctx, ffmpegBinary, []string{"-hide_banner", "-nostdin", "-i", path})

	return parseDurationReport(stderr)
}

// parseDurationReport extracts the "Duration: HH:MM:SS.ff" value from an
// ffmpeg input report.
func parseDurationReport(report string) (float64, bool) {
	const marker = "Duration: "

	index := strings.Index(report, marker)
	if index < 0 {
		return 0, false
	}
	rest := report[index+len(marker):]
	if end := strings.IndexAny(rest, ",\r\n"); end >= 0 {
		rest = rest[:end]
	}
	value := strings.TrimSpace(rest)
	if value == "" || value == "N/A" {
		return 0, false
	}

	microseconds, ok := ffmpegex.ParseFFmpegTimestamp(value)
	if !ok || microseconds <= 0 {
		return 0, false
	}

	return float64(microseconds) / 1e6, true
}
