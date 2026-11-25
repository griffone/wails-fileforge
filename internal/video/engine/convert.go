package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/platform/fsx"
)

const (
	TargetFormatMP4  = "mp4"
	TargetFormatWebM = "webm"

	QualityPresetHigh   = "high"
	QualityPresetMedium = "medium"
	QualityPresetLow    = "low"

	ResolutionOriginal = "original"
	Resolution1080p    = "1080p"
	Resolution720p     = "720p"
	Resolution480p     = "480p"

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

type ConvertRequest struct {
	InputPath     string
	OutputPath    string
	TargetFormat  string
	QualityPreset string
	// Resolution scales the output frame height when it is not "original".
	Resolution string
	// BitrateKbps overrides the quality preset video bitrate when > 0.
	BitrateKbps int
	// RemoveAudio drops the audio stream from the output.
	RemoveAudio bool
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

	if _, ok := convertResolutionHeight(req.Resolution); !ok {
		return &VideoError{Code: ErrorCodeVideoValidation, Message: "resolution must be original, 1080p, 720p, or 480p"}
	}

	if req.BitrateKbps < 0 {
		return &VideoError{Code: ErrorCodeVideoValidation, Message: "bitrateKbps must be >= 0"}
	}

	if outputExt := strings.TrimPrefix(strings.ToLower(filepath.Ext(outputPath)), "."); outputExt != targetFormat {
		return &VideoError{Code: ErrorCodeVideoFormatMismatch, Message: fmt.Sprintf("output extension .%s does not match targetFormat=%s", outputExt, targetFormat)}
	}

	if fsx.NormalizePathKey(inputPath) == fsx.NormalizePathKey(outputPath) {
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

// normalizeConvertResolution treats an omitted resolution as "original" so
// older callers keep the previous behaviour.
func normalizeConvertResolution(rawResolution string) string {
	resolution := strings.ToLower(strings.TrimSpace(rawResolution))
	if resolution == "" {
		return ResolutionOriginal
	}
	return resolution
}

// convertResolutionHeight maps a resolution option to its target height. The
// second return value is false when the resolution is not supported.
func convertResolutionHeight(rawResolution string) (int, bool) {
	switch normalizeConvertResolution(rawResolution) {
	case ResolutionOriginal:
		return 0, true
	case Resolution1080p:
		return 1080, true
	case Resolution720p:
		return 720, true
	case Resolution480p:
		return 480, true
	default:
		return 0, false
	}
}

func BuildConvertArgs(req ConvertRequest) ([]string, *VideoError) {
	if err := ValidateConvertRequest(req); err != nil {
		return nil, err
	}

	targetFormat := strings.ToLower(strings.TrimSpace(req.TargetFormat))
	qualityPreset := strings.ToLower(strings.TrimSpace(req.QualityPreset))

	inputPath := strings.TrimSpace(req.InputPath)
	outputPath := strings.TrimSpace(req.OutputPath)

	encoding, encodingErr := resolveVideoEncoding(targetFormat, qualityPreset)
	if encodingErr != nil {
		return nil, &VideoError{Code: ErrorCodeVideoValidation, Message: encodingErr.Error()}
	}
	if req.BitrateKbps > 0 {
		encoding = encoding.withBitrateOnly(req.BitrateKbps)
	}

	args := withFFmpegGlobalArgs([]string{
		"-y",
		"-i", inputPath,
	})
	args = appendVideoEncodingArgs(args, encoding, req.RemoveAudio)

	if scaleHeight, _ := convertResolutionHeight(req.Resolution); scaleHeight > 0 {
		args = append(args, "-vf", fmt.Sprintf("scale=-2:%d", scaleHeight))
	}

	args = append(args, outputPath)

	return args, nil
}

func Convert(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req ConvertRequest) *VideoError {
	return ConvertWithProgress(ctx, probe, runner, req, nil)
}

// ConvertWithProgress converts one video and reports monotonic percent updates
// in 0..100 while ffmpeg runs. The source duration comes from ffprobe; when it
// is unavailable the command still runs but no intermediate percentage is
// reported.
func ConvertWithProgress(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req ConvertRequest, onProgress func(percent float64, message string)) *VideoError {
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

	if validationErr := ValidateConvertRequest(req); validationErr != nil {
		return validationErr
	}

	tempPath, tempErr := tempOutputPath(req.OutputPath)
	if tempErr != nil {
		return &VideoError{Code: ErrorCodeVideoOutputWriteFailed, Message: "video output could not be prepared", Cause: tempErr}
	}
	published := false
	defer func() {
		if !published {
			removeTempOutput(tempPath)
		}
	}()

	tempRequest := req
	tempRequest.OutputPath = tempPath
	args, argsErr := BuildConvertArgs(tempRequest)
	if argsErr != nil {
		return argsErr
	}

	durationSeconds := 0.0
	if onProgress != nil {
		durationSeconds = probeSourceDuration(ctx, req.InputPath)
	}
	if runErr := runFFmpegWithProgress(ctx, runner, args, durationSeconds, onProgress); runErr != nil {
		return mapExecutionError(runErr)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return mapExecutionError(ctxErr)
	}

	if publishErr := publishOutput(tempPath, req.OutputPath); publishErr != nil {
		return &VideoError{Code: ErrorCodeVideoOutputWriteFailed, Message: "video output could not be published", Cause: publishErr}
	}
	published = true

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
