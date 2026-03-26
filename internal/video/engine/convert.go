package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	pathpkg "path"
	"path/filepath"
	"strings"
)

const (
	TargetFormatMP4  = "mp4"
	TargetFormatWebM = "webm"

	QualityPresetHigh   = "high"
	QualityPresetMedium = "medium"
	QualityPresetLow    = "low"

	ErrorCodeVideoFormatMismatch        = "VIDEO_CONVERT_FORMAT_MISMATCH"
	ErrorCodeVideoOutputExists          = "VIDEO_CONVERT_OUTPUT_EXISTS"
	ErrorCodeVideoOutputCollides        = "VIDEO_CONVERT_OUTPUT_COLLIDES_INPUT"
	ErrorCodeVideoOutputDirNotFound     = "VIDEO_CONVERT_OUTPUT_DIR_NOT_FOUND"
	ErrorCodeVideoOutputDirNotDirectory = "VIDEO_CONVERT_OUTPUT_DIR_NOT_DIRECTORY"
	ErrorCodeVideoOutputDirNotWritable  = "VIDEO_CONVERT_OUTPUT_DIR_NOT_WRITABLE"
	ErrorCodeVideoValidation            = "VIDEO_CONVERT_VALIDATION_ERROR"
	ErrorCodeVideoExecutionFailed       = "VIDEO_CONVERT_EXECUTION_FAILED"
	ErrorCodeVideoInputOpenFailed       = "VIDEO_CONVERT_INPUT_OPEN_FAILED"
	ErrorCodeVideoOutputWriteFailed     = "VIDEO_CONVERT_OUTPUT_WRITE_FAILED"
	ErrorCodeVideoCodecUnavailable      = "VIDEO_CONVERT_CODEC_UNAVAILABLE"
)

type VideoError struct {
	Code    string
	Message string
	Cause   error
}

func (e *VideoError) Error() string {
	if e == nil {
		return ""
	}

	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *VideoError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

type CommandRunner interface {
	Run(ctx context.Context, name string, args []string) error
}

type ExecCommandRunner struct{}

func (r *ExecCommandRunner) Run(ctx context.Context, name string, args []string) error {
	cmd := exec.CommandContext(ctx, name, args...)
	if output, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("command failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	return nil
}

type ConvertRequest struct {
	InputPath     string
	OutputPath    string
	TargetFormat  string
	QualityPreset string
}

func ValidateConvertRequest(req ConvertRequest) *VideoError {
	inputPath := strings.TrimSpace(req.InputPath)
	if inputPath == "" {
		return &VideoError{Code: ErrorCodeVideoValidation, Message: "inputPath is required"}
	}

	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		return &VideoError{Code: ErrorCodeVideoValidation, Message: "outputPath is required"}
	}

	targetFormat := strings.ToLower(strings.TrimSpace(req.TargetFormat))
	if targetFormat != TargetFormatMP4 && targetFormat != TargetFormatWebM {
		return &VideoError{Code: ErrorCodeVideoValidation, Message: "targetFormat must be mp4 or webm"}
	}

	qualityPreset := strings.ToLower(strings.TrimSpace(req.QualityPreset))
	if qualityPreset != QualityPresetHigh && qualityPreset != QualityPresetMedium && qualityPreset != QualityPresetLow {
		return &VideoError{Code: ErrorCodeVideoValidation, Message: "qualityPreset must be high, medium, or low"}
	}

	if outputExt := strings.TrimPrefix(strings.ToLower(filepath.Ext(outputPath)), "."); outputExt != targetFormat {
		return &VideoError{Code: ErrorCodeVideoFormatMismatch, Message: fmt.Sprintf("output extension .%s does not match targetFormat=%s", outputExt, targetFormat)}
	}

	if normalizePathKey(inputPath) == normalizePathKey(outputPath) {
		return &VideoError{Code: ErrorCodeVideoOutputCollides, Message: "outputPath collides with inputPath"}
	}

	if dirErr := validateOutputDir(outputPath); dirErr != nil {
		return dirErr
	}

	if _, err := os.Stat(outputPath); err == nil {
		return &VideoError{Code: ErrorCodeVideoOutputExists, Message: fmt.Sprintf("output already exists: %s", outputPath)}
	} else if !errors.Is(err, os.ErrNotExist) {
		return &VideoError{Code: ErrorCodeVideoValidation, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: err}
	}

	if _, err := os.Stat(inputPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &VideoError{Code: ErrorCodeVideoValidation, Message: fmt.Sprintf("input does not exist: %s", inputPath), Cause: err}
		}
		return &VideoError{Code: ErrorCodeVideoValidation, Message: fmt.Sprintf("input is not accessible: %s", inputPath), Cause: err}
	}

	return nil
}

func BuildConvertArgs(req ConvertRequest) ([]string, *VideoError) {
	if err := ValidateConvertRequest(req); err != nil {
		return nil, err
	}

	targetFormat := strings.ToLower(strings.TrimSpace(req.TargetFormat))
	qualityPreset := strings.ToLower(strings.TrimSpace(req.QualityPreset))

	inputPath := strings.TrimSpace(req.InputPath)
	outputPath := strings.TrimSpace(req.OutputPath)

	videoCRF := "23"
	videoBitrate := "2500k"
	if targetFormat == TargetFormatWebM {
		videoCRF = "32"
		videoBitrate = "1800k"
	}

	switch qualityPreset {
	case QualityPresetHigh:
		if targetFormat == TargetFormatWebM {
			videoCRF = "28"
			videoBitrate = "2600k"
		} else {
			videoCRF = "20"
			videoBitrate = "4500k"
		}
	case QualityPresetMedium:
		// defaults
	case QualityPresetLow:
		if targetFormat == TargetFormatWebM {
			videoCRF = "36"
			videoBitrate = "1200k"
		} else {
			videoCRF = "28"
			videoBitrate = "1400k"
		}
	}

	args := []string{
		"-y",
		"-i", inputPath,
		"-c:v",
	}

	if targetFormat == TargetFormatWebM {
		args = append(args, "libvpx-vp9", "-b:v", videoBitrate, "-crf", videoCRF, "-c:a", "libopus")
	} else {
		args = append(args, "libx264", "-b:v", videoBitrate, "-crf", videoCRF, "-preset", "medium", "-c:a", "aac", "-movflags", "+faststart")
	}

	args = append(args, outputPath)

	return args, nil
}

func Convert(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req ConvertRequest) *VideoError {
	if probe == nil {
		probe = NewFFmpegRuntimeProbe()
	}
	if runner == nil {
		runner = &ExecCommandRunner{}
	}

	if runtimeErr := probe.Check(ctx); runtimeErr != nil {
		var vErr *VideoError
		if errors.As(runtimeErr, &vErr) {
			if vErr.Code == ErrorCodeRuntimeFFmpegNotFound || vErr.Code == ErrorCodeRuntimeFFprobeNotFound {
				return &VideoError{Code: ErrorCodeRuntimeUnavailable, Message: "ffmpeg/ffprobe runtime is unavailable", Cause: vErr}
			}
			return vErr
		}
		return &VideoError{Code: ErrorCodeRuntimeUnavailable, Message: ErrFFmpegNotFound.Error(), Cause: runtimeErr}
	}

	args, validationErr := BuildConvertArgs(req)
	if validationErr != nil {
		return validationErr
	}

	if runErr := runner.Run(ctx, "ffmpeg", args); runErr != nil {
		return mapExecutionError(runErr)
	}

	return nil
}

func validateOutputDir(outputPath string) *VideoError {
	dir := filepath.Dir(strings.TrimSpace(outputPath))
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &VideoError{Code: ErrorCodeVideoOutputDirNotFound, Message: fmt.Sprintf("output directory does not exist: %s", dir), Cause: err}
		}
		return &VideoError{Code: ErrorCodeVideoOutputDirNotWritable, Message: fmt.Sprintf("output directory is not accessible: %s", dir), Cause: err}
	}

	if !info.IsDir() {
		return &VideoError{Code: ErrorCodeVideoOutputDirNotDirectory, Message: fmt.Sprintf("output path parent is not a directory: %s", dir)}
	}

	tmp, err := os.CreateTemp(dir, ".fileforge-video-writecheck-*.tmp")
	if err != nil {
		return &VideoError{Code: ErrorCodeVideoOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: err}
	}
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmp.Name())
		return &VideoError{Code: ErrorCodeVideoOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: closeErr}
	}
	if removeErr := os.Remove(tmp.Name()); removeErr != nil {
		return &VideoError{Code: ErrorCodeVideoOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: removeErr}
	}

	return nil
}

func mapExecutionError(err error) *VideoError {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return &VideoError{Code: "CANCELED", Message: "video convert canceled", Cause: err}
	}

	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "no such file or directory") || strings.Contains(lower, "not found"):
		return &VideoError{Code: ErrorCodeVideoInputOpenFailed, Message: "ffmpeg could not read input file", Cause: err}
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "read-only file system"):
		return &VideoError{Code: ErrorCodeVideoOutputWriteFailed, Message: "ffmpeg could not write output file", Cause: err}
	case strings.Contains(lower, "unknown encoder") || strings.Contains(lower, "encoder"):
		return &VideoError{Code: ErrorCodeVideoCodecUnavailable, Message: "required video/audio codec is unavailable in current ffmpeg build", Cause: err}
	default:
		return &VideoError{Code: ErrorCodeVideoExecutionFailed, Message: "ffmpeg convert execution failed", Cause: err}
	}
}

func normalizePathKey(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return ""
	}

	normalizedSlashes := strings.ReplaceAll(trimmed, "\\", "/")

	if strings.HasPrefix(normalizedSlashes, "//") {
		uncTail := strings.TrimLeft(normalizedSlashes, "/")
		cleanedUNC := pathpkg.Clean("/" + uncTail)
		return "unc://" + strings.ToLower(strings.TrimPrefix(cleanedUNC, "/"))
	}

	if len(normalizedSlashes) >= 2 && normalizedSlashes[1] == ':' {
		drive := strings.ToLower(normalizedSlashes[:1])
		rest := normalizedSlashes[2:]
		cleanedRest := pathpkg.Clean("/" + strings.TrimPrefix(rest, "/"))
		return "win:" + drive + ":" + strings.ToLower(cleanedRest)
	}

	cleaned := pathpkg.Clean(normalizedSlashes)
	return "posix:" + strings.ToLower(cleaned)
}
