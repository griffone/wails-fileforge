package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	ErrorCodeVideoMergeValidation            = "VIDEO_MERGE_VALIDATION_ERROR"
	ErrorCodeVideoMergeInsufficientInputs    = "VIDEO_MERGE_INSUFFICIENT_INPUTS"
	ErrorCodeVideoMergeFormatMismatch        = "VIDEO_MERGE_FORMAT_MISMATCH"
	ErrorCodeVideoMergeOutputExists          = "VIDEO_MERGE_OUTPUT_EXISTS"
	ErrorCodeVideoMergeOutputCollides        = "VIDEO_MERGE_OUTPUT_COLLIDES_INPUT"
	ErrorCodeVideoMergeOutputDirNotFound     = "VIDEO_MERGE_OUTPUT_DIR_NOT_FOUND"
	ErrorCodeVideoMergeOutputDirNotDirectory = "VIDEO_MERGE_OUTPUT_DIR_NOT_DIRECTORY"
	ErrorCodeVideoMergeOutputDirNotWritable  = "VIDEO_MERGE_OUTPUT_DIR_NOT_WRITABLE"
	ErrorCodeVideoMergeExecutionFailed       = "VIDEO_MERGE_EXECUTION_FAILED"
	ErrorCodeVideoMergeInputOpenFailed       = "VIDEO_MERGE_INPUT_OPEN_FAILED"
	ErrorCodeVideoMergeOutputWriteFailed     = "VIDEO_MERGE_OUTPUT_WRITE_FAILED"
	ErrorCodeVideoMergeCodecUnavailable      = "VIDEO_MERGE_CODEC_UNAVAILABLE"
	ErrorCodeVideoMergeModeInvalid           = "VIDEO_MERGE_MODE_INVALID"
	ErrorCodeVideoMergeCopyFailed            = "VIDEO_MERGE_COPY_FAILED"
	ErrorCodeVideoMergeAutoFallbackFailed    = "VIDEO_MERGE_AUTO_FALLBACK_REENCODE_FAILED"

	MergeModeAuto     = "auto"
	MergeModeCopy     = "copy"
	MergeModeReencode = "reencode"
)

type MergeRequest struct {
	InputPaths     []string
	OutputPath     string
	TargetFormat   string
	QualityPreset  string
	MergeMode      string
	OrderedInputs  []string
	ConcatListPath string
}

func ValidateMergeRequest(req MergeRequest) *VideoError {
	if len(req.InputPaths) < 2 {
		return &VideoError{Code: ErrorCodeVideoMergeInsufficientInputs, Message: "at least 2 inputPaths are required for merge"}
	}

	cleanedInputs := make([]string, 0, len(req.InputPaths))
	for _, inputPath := range req.InputPaths {
		trimmed := strings.TrimSpace(inputPath)
		if trimmed == "" {
			return &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "inputPaths must not contain empty values"}
		}
		if _, err := os.Stat(trimmed); err != nil {
			if errors.Is(err, os.ErrNotExist) {
				return &VideoError{Code: ErrorCodeVideoMergeValidation, Message: fmt.Sprintf("input does not exist: %s", trimmed), Cause: err}
			}
			return &VideoError{Code: ErrorCodeVideoMergeValidation, Message: fmt.Sprintf("input is not accessible: %s", trimmed), Cause: err}
		}
		cleanedInputs = append(cleanedInputs, trimmed)
	}

	outputPath := strings.TrimSpace(req.OutputPath)
	if outputPath == "" {
		return &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "outputPath is required"}
	}

	targetFormat := strings.ToLower(strings.TrimSpace(req.TargetFormat))
	if targetFormat != TargetFormatMP4 && targetFormat != TargetFormatWebM {
		return &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "targetFormat must be mp4 or webm"}
	}

	qualityPreset := strings.ToLower(strings.TrimSpace(req.QualityPreset))
	if qualityPreset != QualityPresetHigh && qualityPreset != QualityPresetMedium && qualityPreset != QualityPresetLow {
		return &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "qualityPreset must be high, medium, or low"}
	}

	mergeMode := strings.ToLower(strings.TrimSpace(req.MergeMode))
	if mergeMode == "" {
		mergeMode = MergeModeAuto
	}
	if mergeMode != MergeModeAuto && mergeMode != MergeModeCopy && mergeMode != MergeModeReencode {
		return &VideoError{Code: ErrorCodeVideoMergeModeInvalid, Message: "mergeMode must be auto, copy, or reencode"}
	}

	if outputExt := strings.TrimPrefix(strings.ToLower(filepath.Ext(outputPath)), "."); outputExt != targetFormat {
		return &VideoError{Code: ErrorCodeVideoMergeFormatMismatch, Message: fmt.Sprintf("output extension .%s does not match targetFormat=%s", outputExt, targetFormat)}
	}

	for _, inputPath := range cleanedInputs {
		if normalizePathKey(inputPath) == normalizePathKey(outputPath) {
			return &VideoError{Code: ErrorCodeVideoMergeOutputCollides, Message: "outputPath collides with one of inputPaths"}
		}
	}

	if dirErr := validateMergeOutputDir(outputPath); dirErr != nil {
		return dirErr
	}

	if _, err := os.Stat(outputPath); err == nil {
		return &VideoError{Code: ErrorCodeVideoMergeOutputExists, Message: fmt.Sprintf("output already exists: %s", outputPath)}
	} else if !errors.Is(err, os.ErrNotExist) {
		return &VideoError{Code: ErrorCodeVideoMergeValidation, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: err}
	}

	return nil
}

func Merge(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req MergeRequest) *VideoError {
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

	if validationErr := ValidateMergeRequest(req); validationErr != nil {
		return validationErr
	}

	mergeMode := strings.ToLower(strings.TrimSpace(req.MergeMode))
	if mergeMode == "" {
		mergeMode = MergeModeAuto
	}

	listFilePath, listErr := createConcatListFile(req.InputPaths)
	if listErr != nil {
		return listErr
	}
	defer func() {
		_ = os.Remove(listFilePath)
	}()

	switch mergeMode {
	case MergeModeCopy:
		copyArgs := buildMergeCopyArgs(listFilePath, strings.TrimSpace(req.OutputPath))
		if err := runner.Run(ctx, "ffmpeg", copyArgs); err != nil {
			mapped := mapMergeExecutionError(err)
			return &VideoError{Code: ErrorCodeVideoMergeCopyFailed, Message: "ffmpeg merge copy execution failed", Cause: mapped}
		}
		return nil
	case MergeModeReencode:
		reencodeArgs, argsErr := buildMergeReencodeArgs(listFilePath, req)
		if argsErr != nil {
			return argsErr
		}
		if err := runner.Run(ctx, "ffmpeg", reencodeArgs); err != nil {
			return mapMergeExecutionError(err)
		}
		return nil
	case MergeModeAuto:
		copyArgs := buildMergeCopyArgs(listFilePath, strings.TrimSpace(req.OutputPath))
		if err := runner.Run(ctx, "ffmpeg", copyArgs); err == nil {
			return nil
		} else {
			mappedCopyErr := mapMergeExecutionError(err)
			if !isMergeFallbackEligible(mappedCopyErr) {
				return &VideoError{Code: ErrorCodeVideoMergeCopyFailed, Message: "ffmpeg merge copy execution failed", Cause: mappedCopyErr}
			}

			reencodeArgs, argsErr := buildMergeReencodeArgs(listFilePath, req)
			if argsErr != nil {
				return argsErr
			}

			if reencodeErr := runner.Run(ctx, "ffmpeg", reencodeArgs); reencodeErr != nil {
				mappedReencodeErr := mapMergeExecutionError(reencodeErr)
				return &VideoError{Code: ErrorCodeVideoMergeAutoFallbackFailed, Message: "auto merge fallback to re-encode failed", Cause: mappedReencodeErr}
			}
			return nil
		}
	default:
		return &VideoError{Code: ErrorCodeVideoMergeModeInvalid, Message: "mergeMode must be auto, copy, or reencode"}
	}
}

func buildMergeCopyArgs(listFilePath string, outputPath string) []string {
	return []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listFilePath,
		"-c", "copy",
		outputPath,
	}
}

func buildMergeReencodeArgs(listFilePath string, req MergeRequest) ([]string, *VideoError) {
	targetFormat := strings.ToLower(strings.TrimSpace(req.TargetFormat))
	qualityPreset := strings.ToLower(strings.TrimSpace(req.QualityPreset))
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
	default:
		return nil, &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "qualityPreset must be high, medium, or low"}
	}

	args := []string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listFilePath,
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

func createConcatListFile(inputPaths []string) (string, *VideoError) {
	tmpFile, err := os.CreateTemp("", ".fileforge-video-merge-*.txt")
	if err != nil {
		return "", &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "failed to create temporary concat list file", Cause: err}
	}

	tmpFilePath := tmpFile.Name()

	for _, inputPath := range inputPaths {
		trimmed := strings.TrimSpace(inputPath)
		absPath := trimmed
		if !filepath.IsAbs(absPath) {
			resolved, absErr := filepath.Abs(absPath)
			if absErr != nil {
				_ = tmpFile.Close()
				_ = os.Remove(tmpFilePath)
				return "", &VideoError{Code: ErrorCodeVideoMergeValidation, Message: fmt.Sprintf("failed to resolve absolute path for input: %s", trimmed), Cause: absErr}
			}
			absPath = resolved
		}

		line := fmt.Sprintf("file '%s'\n", escapeConcatFilePath(absPath))
		if _, writeErr := tmpFile.WriteString(line); writeErr != nil {
			_ = tmpFile.Close()
			_ = os.Remove(tmpFilePath)
			return "", &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "failed to write temporary concat list file", Cause: writeErr}
		}
	}

	if err = tmpFile.Close(); err != nil {
		_ = os.Remove(tmpFilePath)
		return "", &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "failed to finalize temporary concat list file", Cause: err}
	}

	return tmpFilePath, nil
}

func escapeConcatFilePath(path string) string {
	return strings.ReplaceAll(path, "'", "'\\''")
}

func validateMergeOutputDir(outputPath string) *VideoError {
	dir := filepath.Dir(strings.TrimSpace(outputPath))
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &VideoError{Code: ErrorCodeVideoMergeOutputDirNotFound, Message: fmt.Sprintf("output directory does not exist: %s", dir), Cause: err}
		}
		return &VideoError{Code: ErrorCodeVideoMergeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not accessible: %s", dir), Cause: err}
	}

	if !info.IsDir() {
		return &VideoError{Code: ErrorCodeVideoMergeOutputDirNotDirectory, Message: fmt.Sprintf("output path parent is not a directory: %s", dir)}
	}

	tmp, err := os.CreateTemp(dir, ".fileforge-video-merge-writecheck-*.tmp")
	if err != nil {
		return &VideoError{Code: ErrorCodeVideoMergeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: err}
	}
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmp.Name())
		return &VideoError{Code: ErrorCodeVideoMergeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: closeErr}
	}
	if removeErr := os.Remove(tmp.Name()); removeErr != nil {
		return &VideoError{Code: ErrorCodeVideoMergeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: removeErr}
	}

	return nil
}

func mapMergeExecutionError(err error) *VideoError {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.Canceled) {
		return &VideoError{Code: "CANCELED", Message: "video merge canceled", Cause: err}
	}

	lower := strings.ToLower(err.Error())
	switch {
	case strings.Contains(lower, "no such file or directory") || strings.Contains(lower, "not found"):
		return &VideoError{Code: ErrorCodeVideoMergeInputOpenFailed, Message: "ffmpeg could not read one or more input files", Cause: err}
	case strings.Contains(lower, "permission denied") || strings.Contains(lower, "read-only file system"):
		return &VideoError{Code: ErrorCodeVideoMergeOutputWriteFailed, Message: "ffmpeg could not write output file", Cause: err}
	case strings.Contains(lower, "unknown encoder") || strings.Contains(lower, "encoder"):
		return &VideoError{Code: ErrorCodeVideoMergeCodecUnavailable, Message: "required video/audio codec is unavailable in current ffmpeg build", Cause: err}
	default:
		return &VideoError{Code: ErrorCodeVideoMergeExecutionFailed, Message: "ffmpeg merge execution failed", Cause: err}
	}
}

func isMergeFallbackEligible(err *VideoError) bool {
	if err == nil {
		return false
	}

	if err.Code == ErrorCodeVideoMergeExecutionFailed || err.Code == ErrorCodeVideoMergeCodecUnavailable {
		return true
	}

	var nested *VideoError
	if errors.As(err.Cause, &nested) {
		return isMergeFallbackEligible(nested)
	}

	return false
}
