package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"
)

const (
	ErrorCodeVideoTrimFormatMismatch        = "VIDEO_TRIM_FORMAT_MISMATCH"
	ErrorCodeVideoTrimOutputExists          = "VIDEO_TRIM_OUTPUT_EXISTS"
	ErrorCodeVideoTrimOutputCollides        = "VIDEO_TRIM_OUTPUT_COLLIDES_INPUT"
	ErrorCodeVideoTrimOutputDirNotFound     = "VIDEO_TRIM_OUTPUT_DIR_NOT_FOUND"
	ErrorCodeVideoTrimOutputDirNotDirectory = "VIDEO_TRIM_OUTPUT_DIR_NOT_DIRECTORY"
	ErrorCodeVideoTrimOutputDirNotWritable  = "VIDEO_TRIM_OUTPUT_DIR_NOT_WRITABLE"
	ErrorCodeVideoTrimValidation            = "VIDEO_TRIM_VALIDATION_ERROR"
	ErrorCodeVideoTrimInvalidTimeRange      = "VIDEO_TRIM_INVALID_TIME_RANGE"
	ErrorCodeVideoTrimExecutionFailed       = "VIDEO_TRIM_EXECUTION_FAILED"
	ErrorCodeVideoTrimInputOpenFailed       = "VIDEO_TRIM_INPUT_OPEN_FAILED"
	ErrorCodeVideoTrimOutputWriteFailed     = "VIDEO_TRIM_OUTPUT_WRITE_FAILED"
	ErrorCodeVideoTrimCodecUnavailable      = "VIDEO_TRIM_CODEC_UNAVAILABLE"
	ErrorCodeVideoTrimModeInvalid           = "VIDEO_TRIM_MODE_INVALID"
	ErrorCodeVideoTrimCopyFailed            = "VIDEO_TRIM_COPY_FAILED"
	ErrorCodeVideoTrimAutoFallbackFailed    = "VIDEO_TRIM_AUTO_FALLBACK_REENCODE_FAILED"

	TrimModeAuto     = "auto"
	TrimModeCopy     = "copy"
	TrimModeReencode = "reencode"
)

type TrimRequest struct {
	InputPath     string
	OutputPath    string
	StartTime     float64
	EndTime       float64
	TargetFormat  string
	QualityPreset string
	TrimMode      string
}

type TrimProgressEvent struct {
	Percent float64
	Stage   string
	Message string
}

type TrimProgressRunner interface {
	RunWithProgress(ctx context.Context, name string, args []string, onProgress func(percent float64, rawMessage string)) error
}

func ValidateTrimRequest(req TrimRequest) *VideoError {
	inputPath := strings.TrimSpace(req.InputPath)
	if inputPath == "" {
		return &VideoError{Code: ErrorCodeVideoTrimValidation, Message: "inputPath is required"}
	}

	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		return &VideoError{Code: ErrorCodeVideoTrimValidation, Message: "outputPath is required"}
	}

	targetFormat := strings.ToLower(strings.TrimSpace(req.TargetFormat))
	if targetFormat != TargetFormatMP4 && targetFormat != TargetFormatWebM {
		return &VideoError{Code: ErrorCodeVideoTrimValidation, Message: "targetFormat must be mp4 or webm"}
	}

	qualityPreset := strings.ToLower(strings.TrimSpace(req.QualityPreset))
	if qualityPreset != QualityPresetHigh && qualityPreset != QualityPresetMedium && qualityPreset != QualityPresetLow {
		return &VideoError{Code: ErrorCodeVideoTrimValidation, Message: "qualityPreset must be high, medium, or low"}
	}

	trimMode := strings.ToLower(strings.TrimSpace(req.TrimMode))
	if trimMode == "" {
		trimMode = TrimModeAuto
	}
	if trimMode != TrimModeAuto && trimMode != TrimModeCopy && trimMode != TrimModeReencode {
		return &VideoError{Code: ErrorCodeVideoTrimModeInvalid, Message: "trimMode must be auto, copy, or reencode"}
	}

	if req.StartTime < 0 {
		return &VideoError{Code: ErrorCodeVideoTrimInvalidTimeRange, Message: "startTime must be >= 0"}
	}

	if req.EndTime <= req.StartTime {
		return &VideoError{Code: ErrorCodeVideoTrimInvalidTimeRange, Message: "endTime must be greater than startTime"}
	}

	if outputExt := strings.TrimPrefix(strings.ToLower(filepath.Ext(outputPath)), "."); outputExt != targetFormat {
		return &VideoError{Code: ErrorCodeVideoTrimFormatMismatch, Message: fmt.Sprintf("output extension .%s does not match targetFormat=%s", outputExt, targetFormat)}
	}

	if normalizeTrimPathKey(inputPath) == normalizeTrimPathKey(outputPath) {
		return &VideoError{Code: ErrorCodeVideoTrimOutputCollides, Message: "outputPath collides with inputPath"}
	}

	if dirErr := validateTrimOutputDir(outputPath); dirErr != nil {
		return dirErr
	}

	if _, err := os.Stat(outputPath); err == nil {
		return &VideoError{Code: ErrorCodeVideoTrimOutputExists, Message: fmt.Sprintf("output already exists: %s", outputPath)}
	} else if !errors.Is(err, os.ErrNotExist) {
		return &VideoError{Code: ErrorCodeVideoTrimValidation, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: err}
	}

	if _, err := os.Stat(inputPath); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &VideoError{Code: ErrorCodeVideoTrimValidation, Message: fmt.Sprintf("input does not exist: %s", inputPath), Cause: err}
		}
		return &VideoError{Code: ErrorCodeVideoTrimValidation, Message: fmt.Sprintf("input is not accessible: %s", inputPath), Cause: err}
	}

	return nil
}

func BuildTrimCopyArgs(req TrimRequest) ([]string, *VideoError) {
	if err := ValidateTrimRequest(req); err != nil {
		return nil, err
	}

	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", req.StartTime),
		"-to", fmt.Sprintf("%.3f", req.EndTime),
		"-i", strings.TrimSpace(req.InputPath),
		"-c", "copy",
		strings.TrimSpace(req.OutputPath),
	}

	return args, nil
}

func BuildTrimReencodeArgs(req TrimRequest) ([]string, *VideoError) {
	if err := ValidateTrimRequest(req); err != nil {
		return nil, err
	}

	args := []string{
		"-y",
		"-ss", fmt.Sprintf("%.3f", req.StartTime),
		"-to", fmt.Sprintf("%.3f", req.EndTime),
		"-i", strings.TrimSpace(req.InputPath),
	}

	videoCRF := "23"
	videoBitrate := "2500k"
	targetFormat := strings.ToLower(strings.TrimSpace(req.TargetFormat))
	qualityPreset := strings.ToLower(strings.TrimSpace(req.QualityPreset))

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
	case QualityPresetLow:
		if targetFormat == TargetFormatWebM {
			videoCRF = "36"
			videoBitrate = "1200k"
		} else {
			videoCRF = "28"
			videoBitrate = "1400k"
		}
	}

	if targetFormat == TargetFormatWebM {
		args = append(args, "-c:v", "libvpx-vp9", "-b:v", videoBitrate, "-crf", videoCRF, "-c:a", "libopus")
	} else {
		args = append(args, "-c:v", "libx264", "-b:v", videoBitrate, "-crf", videoCRF, "-preset", "medium", "-c:a", "aac", "-movflags", "+faststart")
	}

	args = append(args, strings.TrimSpace(req.OutputPath))

	return args, nil
}

func BuildTrimArgs(req TrimRequest) ([]string, *VideoError) {
	return BuildTrimReencodeArgs(req)
}

func Trim(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req TrimRequest) *VideoError {
	return TrimWithProgress(ctx, probe, runner, req, nil)
}

func TrimWithProgress(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req TrimRequest, onProgress func(TrimProgressEvent)) *VideoError {
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

	if onProgress != nil {
		onProgress(TrimProgressEvent{Percent: 5, Stage: "trim.prepare", Message: "Preparing trim request"})
	}

	if validationErr := ValidateTrimRequest(req); validationErr != nil {
		return validationErr
	}

	trimMode := strings.ToLower(strings.TrimSpace(req.TrimMode))
	if trimMode == "" {
		trimMode = TrimModeAuto
	}

	switch trimMode {
	case TrimModeCopy:
		if onProgress != nil {
			onProgress(TrimProgressEvent{Percent: 20, Stage: "trim.copy", Message: "Running trim with stream copy"})
		}
		args, argsErr := BuildTrimCopyArgs(req)
		if argsErr != nil {
			return argsErr
		}
		if runErr := runTrimCommand(ctx, runner, args, onProgress, 20, 95, "trim.copy"); runErr != nil {
			mapped := mapTrimExecutionError(runErr)
			return &VideoError{Code: ErrorCodeVideoTrimCopyFailed, Message: "ffmpeg trim copy execution failed", Cause: mapped}
		}
		if onProgress != nil {
			onProgress(TrimProgressEvent{Percent: 100, Stage: "trim.done", Message: "Trim completed (copy mode)"})
		}
		return nil
	case TrimModeReencode:
		if onProgress != nil {
			onProgress(TrimProgressEvent{Percent: 20, Stage: "trim.reencode", Message: "Running trim with re-encode"})
		}
		args, argsErr := BuildTrimReencodeArgs(req)
		if argsErr != nil {
			return argsErr
		}
		if runErr := runTrimCommand(ctx, runner, args, onProgress, 20, 95, "trim.reencode"); runErr != nil {
			return mapTrimExecutionError(runErr)
		}
		if onProgress != nil {
			onProgress(TrimProgressEvent{Percent: 100, Stage: "trim.done", Message: "Trim completed (reencode mode)"})
		}
		return nil
	case TrimModeAuto:
		if onProgress != nil {
			onProgress(TrimProgressEvent{Percent: 15, Stage: "trim.auto.copy", Message: "Auto mode: trying stream copy first"})
		}
		copyArgs, argsErr := BuildTrimCopyArgs(req)
		if argsErr != nil {
			return argsErr
		}
		if runErr := runTrimCommand(ctx, runner, copyArgs, onProgress, 15, 60, "trim.auto.copy"); runErr == nil {
			if onProgress != nil {
				onProgress(TrimProgressEvent{Percent: 100, Stage: "trim.done", Message: "Trim completed (auto mode, copy path)"})
			}
			return nil
		} else {
			mappedCopyErr := mapTrimExecutionError(runErr)
			if !isTrimFallbackEligible(mappedCopyErr) {
				return &VideoError{Code: ErrorCodeVideoTrimCopyFailed, Message: "ffmpeg trim copy execution failed", Cause: mappedCopyErr}
			}

			if onProgress != nil {
				onProgress(TrimProgressEvent{Percent: 65, Stage: "trim.auto.fallback", Message: "Auto fallback: retrying with re-encode"})
			}

			reencodeArgs, reencodeArgsErr := BuildTrimReencodeArgs(req)
			if reencodeArgsErr != nil {
				return reencodeArgsErr
			}
			if reencodeErr := runTrimCommand(ctx, runner, reencodeArgs, onProgress, 70, 95, "trim.auto.reencode"); reencodeErr != nil {
				mappedReencodeErr := mapTrimExecutionError(reencodeErr)
				return &VideoError{Code: ErrorCodeVideoTrimAutoFallbackFailed, Message: "auto trim fallback to re-encode failed", Cause: mappedReencodeErr}
			}

			if onProgress != nil {
				onProgress(TrimProgressEvent{Percent: 100, Stage: "trim.done", Message: "Trim completed after auto fallback to re-encode"})
			}
			return nil
		}
	default:
		return &VideoError{Code: ErrorCodeVideoTrimModeInvalid, Message: "trimMode must be auto, copy, or reencode"}
	}
}

func runTrimCommand(ctx context.Context, runner CommandRunner, args []string, onProgress func(TrimProgressEvent), startPercent float64, endPercent float64, stage string) error {
	if onProgress == nil {
		return runner.Run(ctx, "ffmpeg", args)
	}

	if progressRunner, ok := runner.(TrimProgressRunner); ok {
		return progressRunner.RunWithProgress(ctx, "ffmpeg", args, func(percent float64, rawMessage string) {
			mappedPercent := startPercent + ((endPercent - startPercent) * (percent / 100.0))
			onProgress(TrimProgressEvent{Percent: clampPercent(mappedPercent), Stage: stage, Message: strings.TrimSpace(rawMessage)})
		})
	}

	onProgress(TrimProgressEvent{Percent: startPercent, Stage: stage, Message: "Processing trim stage"})
	err := runner.Run(ctx, "ffmpeg", args)
	if err == nil {
		onProgress(TrimProgressEvent{Percent: endPercent, Stage: stage, Message: "Trim stage completed"})
	}
	return err
}

func clampPercent(v float64) float64 {
	if v < 0 {
		return 0
	}
	if v > 100 {
		return 100
	}
	return v
}

func isTrimFallbackEligible(err *VideoError) bool {
	if err == nil {
		return false
	}

	if err.Code == ErrorCodeVideoTrimExecutionFailed || err.Code == ErrorCodeVideoTrimCodecUnavailable {
		return true
	}

	var nested *VideoError
	if errors.As(err.Cause, &nested) {
		return isTrimFallbackEligible(nested)
	}

	return false
}

func IsTrimFallbackEligible(err *VideoError) bool {
	return isTrimFallbackEligible(err)
}

func validateTrimOutputDir(outputPath string) *VideoError {
	dir := filepath.Dir(strings.TrimSpace(outputPath))
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &VideoError{Code: ErrorCodeVideoTrimOutputDirNotFound, Message: fmt.Sprintf("output directory does not exist: %s", dir), Cause: err}
		}
		return &VideoError{Code: ErrorCodeVideoTrimOutputDirNotWritable, Message: fmt.Sprintf("output directory is not accessible: %s", dir), Cause: err}
	}

	if !info.IsDir() {
		return &VideoError{Code: ErrorCodeVideoTrimOutputDirNotDirectory, Message: fmt.Sprintf("output path parent is not a directory: %s", dir)}
	}

	tmp, err := os.CreateTemp(dir, ".fileforge-video-trim-writecheck-*.tmp")
	if err != nil {
		return &VideoError{Code: ErrorCodeVideoTrimOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: err}
	}
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmp.Name())
		return &VideoError{Code: ErrorCodeVideoTrimOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: closeErr}
	}
	if removeErr := os.Remove(tmp.Name()); removeErr != nil {
		return &VideoError{Code: ErrorCodeVideoTrimOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: removeErr}
	}

	return nil
}

func mapTrimExecutionError(err error) *VideoError {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return &VideoError{Code: "CANCELED", Message: "video trim canceled", Cause: err}
	}

	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "no such file or directory") || strings.Contains(lower, "not found"):
		return &VideoError{Code: ErrorCodeVideoTrimInputOpenFailed, Message: "ffmpeg could not read input file", Cause: err}
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "read-only file system"):
		return &VideoError{Code: ErrorCodeVideoTrimOutputWriteFailed, Message: "ffmpeg could not write output file", Cause: err}
	case strings.Contains(lower, "unknown encoder") || strings.Contains(lower, "encoder"):
		return &VideoError{Code: ErrorCodeVideoTrimCodecUnavailable, Message: "required video/audio codec is unavailable in current ffmpeg build", Cause: err}
	default:
		return &VideoError{Code: ErrorCodeVideoTrimExecutionFailed, Message: "ffmpeg trim execution failed", Cause: err}
	}
}

func normalizeTrimPathKey(rawPath string) string {
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
