package engine

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/platform/ffmpegex"
	"fileforge-desktop/internal/platform/fsx"
)

// SourceDurationProbe reports a media duration in seconds. It is a variable so
// tests can supply durations without ffprobe.
var SourceDurationProbe = inspect.ProbeVideoDuration

// Request describes one audio conversion. Format is one of the supported
// format names; BitrateKbps is optional and ignored by lossless formats.
type Request struct {
	InputPath   string
	OutputPath  string
	Format      string
	BitrateKbps int
}

// ValidateRequest checks everything that can be checked without decoding:
// required paths, a supported format, matching output extension, output
// directory writability, and an input that exists.
func ValidateRequest(req Request) *AudioError {
	inputPath := strings.TrimSpace(req.InputPath)
	if inputPath == "" {
		return &AudioError{Code: ErrorCodeInputRequired, Message: "inputPath is required"}
	}

	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		return &AudioError{Code: ErrorCodeValidation, Message: "outputPath is required"}
	}

	format, formatErr := FormatByName(req.Format)
	if formatErr != nil {
		return formatErr
	}

	if req.BitrateKbps < 0 {
		return &AudioError{Code: ErrorCodeBitrateInvalid, Message: "bitrateKbps must be >= 0"}
	}

	outputExtension := strings.TrimPrefix(strings.ToLower(filepath.Ext(outputPath)), ".")
	if outputExtension != format.Name {
		return &AudioError{
			Code:    ErrorCodeFormatMismatch,
			Message: fmt.Sprintf("output extension .%s does not match format=%s", outputExtension, format.Name),
		}
	}

	if fsx.NormalizePathKey(inputPath) == fsx.NormalizePathKey(outputPath) {
		return &AudioError{Code: ErrorCodeOutputCollidesInput, Message: "outputPath collides with inputPath"}
	}

	if dirErr := validateOutputDir(outputPath); dirErr != nil {
		return dirErr
	}

	if _, err := os.Stat(outputPath); err == nil {
		return &AudioError{Code: ErrorCodeOutputExists, Message: fmt.Sprintf("output already exists: %s", outputPath)}
	} else if !errors.Is(err, os.ErrNotExist) {
		return &AudioError{Code: ErrorCodeValidation, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: err}
	}

	if _, err := os.Stat(inputPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &AudioError{Code: ErrorCodeInputNotFound, Message: fmt.Sprintf("input does not exist: %s", inputPath), Cause: err}
		}
		return &AudioError{Code: ErrorCodeInputNotFound, Message: fmt.Sprintf("input is not accessible: %s", inputPath), Cause: err}
	}

	return nil
}

// BuildArgs builds the ffmpeg arguments for one audio encode. The video stream
// is always dropped: extract must keep only audio, and convert/normalize must
// not carry album art into the new container.
func BuildArgs(req Request) ([]string, *AudioError) {
	if err := ValidateRequest(req); err != nil {
		return nil, err
	}

	format, formatErr := FormatByName(req.Format)
	if formatErr != nil {
		return nil, formatErr
	}

	inputPath := strings.TrimSpace(req.InputPath)
	outputPath := strings.TrimSpace(req.OutputPath)

	args := ffmpegex.WithGlobalArgs([]string{
		"-y",
		"-i", inputPath,
		"-vn",
		"-c:a", format.Encoder,
	})

	if !format.Lossless {
		bitrate := req.BitrateKbps
		if bitrate <= 0 {
			bitrate = format.DefaultBitrateKbps
		}
		args = append(args, "-b:a", fmt.Sprintf("%dk", bitrate))
	}

	if format.Name == FormatM4A {
		args = append(args, "-movflags", "+faststart")
	}

	return append(args, outputPath), nil
}

// Convert encodes one audio file and publishes it atomically. Progress is
// reported in 0..100 when the runner supports it and the source duration is
// readable.
func Convert(ctx context.Context, probe RuntimeProbe, runner ffmpegex.CommandRunner, req Request, onProgress func(percent float64, message string)) *AudioError {
	probe = resolveRuntimeProbe(probe)
	if runtimeErr := probe.Check(ctx); runtimeErr != nil {
		return &AudioError{Code: runtimeErrorCode(runtimeErr), Message: runtimeErrorMessage(runtimeErr), Cause: runtimeErr}
	}
	if runner == nil {
		runner = &ffmpegex.ExecCommandRunner{}
	}

	if validationErr := ValidateRequest(req); validationErr != nil {
		return validationErr
	}

	tempPath, tempErr := ffmpegex.TempOutputPath(req.OutputPath)
	if tempErr != nil {
		return &AudioError{Code: ErrorCodeOutputWriteFailed, Message: "audio output could not be prepared", Cause: tempErr}
	}
	published := false
	defer func() {
		if !published {
			ffmpegex.RemoveTempOutput(tempPath)
		}
	}()

	tempRequest := req
	tempRequest.OutputPath = tempPath
	args, argsErr := BuildArgs(tempRequest)
	if argsErr != nil {
		return argsErr
	}

	durationSeconds := 0.0
	if onProgress != nil {
		durationSeconds = probeSourceDuration(ctx, req.InputPath)
	}
	if runErr := ffmpegex.RunWithProgress(ctx, runner, args, durationSeconds, onProgress); runErr != nil {
		return mapExecutionError(runErr, "audio conversion")
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return mapExecutionError(ctxErr, "audio conversion")
	}

	if publishErr := ffmpegex.PublishOutput(tempPath, req.OutputPath); publishErr != nil {
		return &AudioError{Code: ErrorCodeOutputWriteFailed, Message: "audio output could not be published", Cause: publishErr}
	}
	published = true

	return nil
}

func probeSourceDuration(ctx context.Context, path string) float64 {
	duration, ok := SourceDurationProbe(ctx, strings.TrimSpace(path))
	if !ok || duration <= 0 || math.IsNaN(duration) || math.IsInf(duration, 0) {
		return 0
	}

	return duration
}

func validateOutputDir(outputPath string) *AudioError {
	dir := filepath.Dir(strings.TrimSpace(outputPath))
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &AudioError{Code: ErrorCodeOutputDirNotFound, Message: fmt.Sprintf("output directory does not exist: %s", dir), Cause: err}
		}
		return &AudioError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not accessible: %s", dir), Cause: err}
	}

	if !info.IsDir() {
		return &AudioError{Code: ErrorCodeOutputDirNotDirectory, Message: fmt.Sprintf("output path parent is not a directory: %s", dir)}
	}

	tmp, err := os.CreateTemp(dir, ".fileforge-audio-writecheck-*.tmp")
	if err != nil {
		return &AudioError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: err}
	}
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmp.Name())
		return &AudioError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: closeErr}
	}
	if removeErr := os.Remove(tmp.Name()); removeErr != nil {
		return &AudioError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: removeErr}
	}

	return nil
}

func runtimeErrorMessage(err error) string {
	var audioErr *AudioError
	if errors.As(err, &audioErr) && audioErr != nil {
		return audioErr.Message
	}

	return "ffmpeg/ffprobe runtime is unavailable"
}

// mapExecutionError classifies a process failure. A cancelled context always
// wins: the job engine reports cancellation from it.
func mapExecutionError(err error, operation string) *AudioError {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return &AudioError{Code: ErrorCodeCancelled, Message: operation + " canceled", Cause: err}
	}

	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "no such file or directory") || strings.Contains(lower, "not found"):
		return &AudioError{Code: ErrorCodeInputOpenFailed, Message: "ffmpeg could not read the input file", Cause: err}
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "read-only file system") || strings.Contains(lower, "no space left"):
		return &AudioError{Code: ErrorCodeOutputWriteFailed, Message: "ffmpeg could not write the output file", Cause: err}
	case strings.Contains(lower, "unknown encoder") || strings.Contains(lower, "encoder not found") || strings.Contains(lower, "error initializing output stream"):
		return &AudioError{Code: ErrorCodeEncoderUnavailable, Message: "the required audio encoder is unavailable in this ffmpeg build", Cause: err}
	default:
		return &AudioError{Code: ErrorCodeExecutionFailed, Message: operation + " failed", Cause: err}
	}
}
