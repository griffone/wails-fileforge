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
	"fileforge-desktop/internal/platform/fsx"
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

	MergeTransitionNone      = "none"
	MergeTransitionCrossfade = "crossfade"

	MergeTransitionDurationDefault = 0.5
	MergeTransitionDurationMin     = 0.1
	MergeTransitionDurationMax     = 3.0
)

// MergeInputDurationProbe reports clip durations before a crossfade is built.
// It is a package variable so tests can simulate durations without ffprobe.
var MergeInputDurationProbe = inspect.ProbeVideoDuration

type MergeRequest struct {
	InputPaths     []string
	OutputPath     string
	TargetFormat   string
	QualityPreset  string
	MergeMode      string
	OrderedInputs  []string
	ConcatListPath string
	// RemoveAudio drops the audio streams from the merged output.
	RemoveAudio bool
	// Transition applies a transition between clips. Empty or "none"
	// concatenates without one; "crossfade" always forces a re-encode.
	Transition string
	// TransitionDuration is the crossfade length in seconds (0.1-3.0).
	TransitionDuration float64
	// TargetSizeBytes forces a two-pass re-encode sized to the requested
	// bytes. Copy mode and auto cannot honour it, so they are re-encoded.
	TargetSizeBytes int64
}

// resolveMergeTransition normalizes the transition options. An empty transition
// means none, and a zero crossfade duration keeps the default. Durations are
// only meaningful for crossfade; a requested transition is ignored otherwise.
func resolveMergeTransition(req MergeRequest) (string, float64, *VideoError) {
	transition := strings.ToLower(strings.TrimSpace(req.Transition))
	if transition == "" {
		transition = MergeTransitionNone
	}

	switch transition {
	case MergeTransitionNone:
		return transition, 0, nil
	case MergeTransitionCrossfade:
	default:
		return "", 0, &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "transition must be none or crossfade"}
	}

	duration := req.TransitionDuration
	if math.IsNaN(duration) || math.IsInf(duration, 0) {
		return "", 0, &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "transitionDuration must be a finite number"}
	}
	if duration == 0 {
		duration = MergeTransitionDurationDefault
	}
	if duration < MergeTransitionDurationMin || duration > MergeTransitionDurationMax {
		return "", 0, &VideoError{Code: ErrorCodeVideoMergeValidation, Message: fmt.Sprintf("transitionDuration must be between %.1f and %.1f seconds", MergeTransitionDurationMin, MergeTransitionDurationMax)}
	}

	return transition, duration, nil
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

	if req.TargetSizeBytes < 0 {
		return &VideoError{Code: ErrorCodeVideoTargetInvalid, Message: "targetSizeBytes must be >= 0"}
	}

	if _, _, transitionErr := resolveMergeTransition(req); transitionErr != nil {
		return transitionErr
	}

	if outputExt := strings.TrimPrefix(strings.ToLower(filepath.Ext(outputPath)), "."); outputExt != targetFormat {
		return &VideoError{Code: ErrorCodeVideoMergeFormatMismatch, Message: fmt.Sprintf("output extension .%s does not match targetFormat=%s", outputExt, targetFormat)}
	}

	for _, inputPath := range cleanedInputs {
		if fsx.NormalizePathKey(inputPath) == fsx.NormalizePathKey(outputPath) {
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
	return MergeWithProgress(ctx, probe, runner, req, nil)
}

// MergeWithProgress keeps the historical merge progress contract while the
// target path is shared with convert and trim.
func MergeWithProgress(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req MergeRequest, onProgress func(percent float64, message string)) *VideoError {
	_, mergeErr := MergeWithOutcome(ctx, probe, runner, req, onProgress)
	return mergeErr
}

// MergeWithOutcome is MergeWithProgress plus the target-size outcome. A target
// always forces a re-encode and its budget uses the summed input duration, so a
// crossfade (which shortens the real output) stays an approximation.
func MergeWithOutcome(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req MergeRequest, onProgress func(percent float64, message string)) (TargetEncodeOutcome, *VideoError) {
	var outcome TargetEncodeOutcome
	if req.TargetSizeBytes <= 0 {
		return outcome, mergeWithProgressLegacy(ctx, probe, runner, req, onProgress)
	}

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
				return outcome, &VideoError{Code: ErrorCodeRuntimeUnavailable, Message: "ffmpeg/ffprobe runtime is unavailable", Cause: vErr}
			}
			return outcome, vErr
		}
		return outcome, &VideoError{Code: ErrorCodeRuntimeUnavailable, Message: ErrFFmpegNotFound.Error(), Cause: runtimeErr}
	}

	if validationErr := ValidateMergeRequest(req); validationErr != nil {
		return outcome, validationErr
	}

	transition, _, transitionErr := resolveMergeTransition(req)
	if transitionErr != nil {
		return outcome, transitionErr
	}

	// One probe per input supplies the summed budget duration, the audio
	// reservation and the floor tier.
	durations := make([]float64, 0, len(req.InputPaths))
	hasAudio := false
	maxHeight := 0
	for _, inputPath := range req.InputPaths {
		media, mediaOK := ProbeTargetMediaOrDuration(ctx, strings.TrimSpace(inputPath))
		if !mediaOK || media.DurationSeconds <= 0 {
			return outcome, &VideoError{
				Code:    ErrorCodeVideoTargetDurationUnavailable,
				Message: fmt.Sprintf("the duration of %s could not be read; a size target cannot be computed", filepath.Base(strings.TrimSpace(inputPath))),
			}
		}
		durations = append(durations, media.DurationSeconds)
		hasAudio = hasAudio || media.HasAudio
		if media.Height > maxHeight {
			maxHeight = media.Height
		}
	}

	tempPath, tempErr := tempOutputPath(req.OutputPath)
	if tempErr != nil {
		return outcome, &VideoError{Code: ErrorCodeVideoMergeOutputWriteFailed, Message: "video merge output could not be prepared", Cause: tempErr}
	}
	published := false
	defer func() {
		if !published {
			removeTempOutput(tempPath)
		}
	}()

	spec := TargetEncodeSpec{
		TargetBytes:     req.TargetSizeBytes,
		DurationSeconds: sumDurations(durations),
		AudioKbps:       TargetAudioKbps(req.TargetFormat, hasAudio && !req.RemoveAudio),
		FloorKbps:       MinVideoBitrateKbps(ResolutionOriginal, maxHeight),
		OutputPath:      tempPath,
		MapRunError:     mapMergeExecutionError,
		OnEvent: func(evt TargetProgressEvent) {
			if onProgress == nil {
				return
			}
			onProgress(evt.Percent, evt.Message)
		},
	}

	if transition == MergeTransitionCrossfade {
		spec.BuildPassArgs = func(bitrateKbps, pass int, passLogPrefix, outputPath string) ([]string, *VideoError) {
			return buildMergeCrossfadeTargetPassArgs(req.InputPaths, durations, req, bitrateKbps, pass, passLogPrefix, outputPath)
		}
	} else {
		listFilePath, listErr := createConcatListFile(req.InputPaths)
		if listErr != nil {
			return outcome, listErr
		}
		defer func() {
			_ = os.Remove(listFilePath)
		}()
		spec.BuildPassArgs = func(bitrateKbps, pass int, passLogPrefix, outputPath string) ([]string, *VideoError) {
			return buildMergeTargetPassArgs(listFilePath, req, bitrateKbps, pass, passLogPrefix, outputPath)
		}
	}

	targetOutcome, targetErr := TargetEncode(ctx, runner, spec)
	if targetErr != nil {
		return targetOutcome, targetErr
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return targetOutcome, mapMergeExecutionError(ctxErr)
	}
	if publishErr := publishOutput(tempPath, req.OutputPath); publishErr != nil {
		return targetOutcome, &VideoError{Code: ErrorCodeVideoMergeOutputWriteFailed, Message: "video merge output could not be published", Cause: publishErr}
	}
	published = true

	return targetOutcome, nil
}

func mergeWithProgressLegacy(ctx context.Context, probe RuntimeProbe, runner CommandRunner, req MergeRequest, onProgress func(percent float64, message string)) *VideoError {
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

	tempPath, tempErr := tempOutputPath(req.OutputPath)
	if tempErr != nil {
		return &VideoError{Code: ErrorCodeVideoMergeOutputWriteFailed, Message: "video merge output could not be prepared", Cause: tempErr}
	}
	published := false
	defer func() {
		if !published {
			removeTempOutput(tempPath)
		}
	}()

	tempRequest := req
	tempRequest.OutputPath = tempPath

	publish := func() *VideoError {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return mapMergeExecutionError(ctxErr)
		}
		if publishErr := publishOutput(tempPath, req.OutputPath); publishErr != nil {
			return &VideoError{Code: ErrorCodeVideoMergeOutputWriteFailed, Message: "video merge output could not be published", Cause: publishErr}
		}
		published = true
		return nil
	}

	mergeMode := strings.ToLower(strings.TrimSpace(req.MergeMode))
	if mergeMode == "" {
		mergeMode = MergeModeAuto
	}

	transition, _, transitionErr := resolveMergeTransition(req)
	if transitionErr != nil {
		return transitionErr
	}
	if transition == MergeTransitionCrossfade {
		durations := make([]float64, 0, len(req.InputPaths))
		for _, inputPath := range req.InputPaths {
			trimmed := strings.TrimSpace(inputPath)
			duration, ok := MergeInputDurationProbe(ctx, trimmed)
			if !ok || duration <= 0 {
				return &VideoError{Code: ErrorCodeVideoMergeValidation, Message: fmt.Sprintf("could not determine duration for input: %s", trimmed)}
			}
			durations = append(durations, duration)
		}

		progressDuration := 0.0
		if onProgress != nil {
			progressDuration = sumDurations(durations)
		}

		crossfadeArgs, argsErr := BuildMergeCrossfadeArgs(req.InputPaths, durations, tempRequest)
		if argsErr != nil {
			return argsErr
		}
		if err := runFFmpegWithProgress(ctx, runner, crossfadeArgs, progressDuration, onProgress); err != nil {
			return mapMergeExecutionError(err)
		}
		return publish()
	}

	progressDuration := 0.0
	if onProgress != nil {
		progressDuration, _ = mergeProgressDuration(ctx, req.InputPaths)
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
		copyArgs := buildMergeCopyArgs(listFilePath, strings.TrimSpace(tempRequest.OutputPath), req.RemoveAudio)
		if err := runFFmpegWithProgress(ctx, runner, copyArgs, progressDuration, onProgress); err != nil {
			mapped := mapMergeExecutionError(err)
			return &VideoError{Code: ErrorCodeVideoMergeCopyFailed, Message: "ffmpeg merge copy execution failed", Cause: mapped}
		}
		return publish()
	case MergeModeReencode:
		reencodeArgs, argsErr := BuildMergeArgs(listFilePath, tempRequest)
		if argsErr != nil {
			return argsErr
		}
		if err := runFFmpegWithProgress(ctx, runner, reencodeArgs, progressDuration, onProgress); err != nil {
			return mapMergeExecutionError(err)
		}
		return publish()
	case MergeModeAuto:
		copyArgs := buildMergeCopyArgs(listFilePath, strings.TrimSpace(tempRequest.OutputPath), req.RemoveAudio)
		if err := runFFmpegWithProgress(ctx, runner, copyArgs, progressDuration, onProgress); err == nil {
			return publish()
		} else {
			mappedCopyErr := mapMergeExecutionError(err)
			if !isMergeFallbackEligible(mappedCopyErr) {
				return &VideoError{Code: ErrorCodeVideoMergeCopyFailed, Message: "ffmpeg merge copy execution failed", Cause: mappedCopyErr}
			}

			// A failed stream copy may leave a partial temp file; discard it
			// before the re-encode fallback writes a fresh one.
			removeTempOutput(tempPath)

			reencodeArgs, argsErr := BuildMergeArgs(listFilePath, tempRequest)
			if argsErr != nil {
				return argsErr
			}

			if reencodeErr := runFFmpegWithProgress(ctx, runner, reencodeArgs, progressDuration, onProgress); reencodeErr != nil {
				mappedReencodeErr := mapMergeExecutionError(reencodeErr)
				return &VideoError{Code: ErrorCodeVideoMergeAutoFallbackFailed, Message: "auto merge fallback to re-encode failed", Cause: mappedReencodeErr}
			}
			return publish()
		}
	default:
		return &VideoError{Code: ErrorCodeVideoMergeModeInvalid, Message: "mergeMode must be auto, copy, or reencode"}
	}
}

// mergeProgressDuration sums the input durations used to turn ffmpeg's
// concatenated timeline position into a percentage. ok is false when any input
// duration is unavailable; the run still proceeds without intermediate ticks.
func mergeProgressDuration(ctx context.Context, inputPaths []string) (float64, bool) {
	total := 0.0
	for _, inputPath := range inputPaths {
		duration := probeSourceDuration(ctx, inputPath)
		if duration <= 0 {
			return 0, false
		}
		total += duration
	}

	return total, true
}

func sumDurations(durations []float64) float64 {
	total := 0.0
	for _, duration := range durations {
		total += duration
	}

	return total
}

func buildMergeCopyArgs(listFilePath string, outputPath string, removeAudio bool) []string {
	args := withFFmpegGlobalArgs([]string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listFilePath,
		"-c", "copy",
	})

	if removeAudio {
		args = append(args, "-an")
	}

	return append(args, outputPath)
}

// resolveMergeEncoding maps a target format and quality preset to the shared
// encoder settings used by the concat and crossfade re-encode paths.
func resolveMergeEncoding(targetFormat, qualityPreset string) (videoEncoding, *VideoError) {
	encoding, err := resolveVideoEncoding(targetFormat, qualityPreset)
	if err != nil {
		return videoEncoding{}, &VideoError{Code: ErrorCodeVideoMergeValidation, Message: err.Error()}
	}

	return encoding, nil
}

// BuildMergeArgs builds the re-encode ffmpeg arguments for a merge request.
// listFilePath is the concat demuxer list written by Merge.
func BuildMergeArgs(listFilePath string, req MergeRequest) ([]string, *VideoError) {
	encoding, encodingErr := resolveMergeEncoding(req.TargetFormat, req.QualityPreset)
	if encodingErr != nil {
		return nil, encodingErr
	}

	args := withFFmpegGlobalArgs([]string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listFilePath,
	})
	args = appendVideoEncodingArgs(args, encoding, req.RemoveAudio)

	return append(args, strings.TrimSpace(req.OutputPath)), nil
}

// BuildMergeCrossfadeArgs builds the ffmpeg arguments that join clips with an
// xfade video transition. durations must hold one probed duration per input.
func BuildMergeCrossfadeArgs(inputPaths []string, durations []float64, req MergeRequest) ([]string, *VideoError) {
	transition, transitionDuration, transitionErr := resolveMergeTransition(req)
	if transitionErr != nil {
		return nil, transitionErr
	}
	if transition != MergeTransitionCrossfade {
		return nil, &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "transition must be crossfade to build crossfade arguments"}
	}
	if len(inputPaths) < 2 {
		return nil, &VideoError{Code: ErrorCodeVideoMergeInsufficientInputs, Message: "at least 2 inputPaths are required for crossfade"}
	}
	if len(durations) != len(inputPaths) {
		return nil, &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "durations must contain one value per input"}
	}

	offsets := make([]float64, 0, len(inputPaths)-1)
	cumulative := 0.0
	for index, duration := range durations {
		if math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= transitionDuration {
			return nil, &VideoError{
				Code:    ErrorCodeVideoMergeValidation,
				Message: fmt.Sprintf("input %d is too short (%.2f s) for a %.2f s crossfade", index+1, duration, transitionDuration),
			}
		}
		if index == 0 {
			continue
		}
		cumulative += durations[index-1]
		offset := cumulative - float64(index)*transitionDuration
		if offset < 0 {
			return nil, &VideoError{
				Code:    ErrorCodeVideoMergeValidation,
				Message: fmt.Sprintf("crossfade offset for input %d must be >= 0", index+1),
			}
		}
		offsets = append(offsets, offset)
	}

	encoding, encodingErr := resolveMergeEncoding(req.TargetFormat, req.QualityPreset)
	if encodingErr != nil {
		return nil, encodingErr
	}

	args := withFFmpegGlobalArgs([]string{"-y"})
	for _, inputPath := range inputPaths {
		args = append(args, "-i", strings.TrimSpace(inputPath))
	}
	args = append(args, "-filter_complex", buildMergeCrossfadeFilter(len(inputPaths), transitionDuration, offsets, !req.RemoveAudio))
	args = append(args, "-map", "[vout]")
	if !req.RemoveAudio {
		args = append(args, "-map", "[aout]")
	}
	args = appendVideoEncodingArgs(args, encoding, req.RemoveAudio)

	return append(args, strings.TrimSpace(req.OutputPath)), nil
}

// buildMergeCrossfadeFilter normalizes every video stream and chains xfade
// transitions. offsets[i] is the xfade offset for input i+1.
func buildMergeCrossfadeFilter(inputCount int, transitionDuration float64, offsets []float64, includeAudio bool) string {
	parts := make([]string, 0, inputCount*2)
	for index := 0; index < inputCount; index++ {
		parts = append(parts, fmt.Sprintf("[%d:v]settb=AVTB,setpts=PTS-STARTPTS[v%d]", index, index))
	}

	videoLabel := "v0"
	for index := 1; index < inputCount; index++ {
		nextLabel := fmt.Sprintf("vc%d", index)
		if index == inputCount-1 {
			nextLabel = "vout"
		}
		parts = append(parts, fmt.Sprintf("[%s][v%d]xfade=transition=fade:duration=%s:offset=%s[%s]",
			videoLabel, index, formatMergeSeconds(transitionDuration), formatMergeSeconds(offsets[index-1]), nextLabel))
		videoLabel = nextLabel
	}

	if includeAudio {
		audioLabel := "0:a"
		for index := 1; index < inputCount; index++ {
			nextLabel := fmt.Sprintf("ac%d", index)
			if index == inputCount-1 {
				nextLabel = "aout"
			}
			parts = append(parts, fmt.Sprintf("[%s][%d:a]acrossfade=d=%s[%s]", audioLabel, index, formatMergeSeconds(transitionDuration), nextLabel))
			audioLabel = nextLabel
		}
	}

	return strings.Join(parts, ";")
}

func formatMergeSeconds(value float64) string {
	return fmt.Sprintf("%.3f", value)
}

// buildMergeTargetPassArgs builds one pass of a concat two-pass target encode.
func buildMergeTargetPassArgs(listFilePath string, req MergeRequest, bitrateKbps, pass int, passLogPrefix, outputPath string) ([]string, *VideoError) {
	encoding, encodingErr := resolveMergeEncoding(req.TargetFormat, req.QualityPreset)
	if encodingErr != nil {
		return nil, encodingErr
	}
	encoding = encoding.withBitrateOnly(bitrateKbps).withTargetAudio(TargetAudioKbps(req.TargetFormat, !req.RemoveAudio))

	args := withFFmpegGlobalArgs([]string{
		"-y",
		"-f", "concat",
		"-safe", "0",
		"-i", listFilePath,
	})

	return appendTargetPassArgs(args, encoding, req.RemoveAudio, pass, passLogPrefix, outputPath), nil
}

// buildMergeCrossfadeTargetPassArgs builds one pass of a crossfade two-pass
// target encode. Pass 1 maps video only, so the statistics pass stays honest.
func buildMergeCrossfadeTargetPassArgs(inputPaths []string, durations []float64, req MergeRequest, bitrateKbps, pass int, passLogPrefix, outputPath string) ([]string, *VideoError) {
	transition, transitionDuration, transitionErr := resolveMergeTransition(req)
	if transitionErr != nil {
		return nil, transitionErr
	}
	if transition != MergeTransitionCrossfade {
		return nil, &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "transition must be crossfade to build crossfade arguments"}
	}
	if len(inputPaths) < 2 || len(durations) != len(inputPaths) {
		return nil, &VideoError{Code: ErrorCodeVideoMergeValidation, Message: "durations must contain one value per input"}
	}

	offsets, offsetsErr := mergeCrossfadeOffsets(durations, transitionDuration)
	if offsetsErr != nil {
		return nil, offsetsErr
	}

	encoding, encodingErr := resolveMergeEncoding(req.TargetFormat, req.QualityPreset)
	if encodingErr != nil {
		return nil, encodingErr
	}
	encoding = encoding.withBitrateOnly(bitrateKbps).withTargetAudio(TargetAudioKbps(req.TargetFormat, !req.RemoveAudio))

	includeAudio := !req.RemoveAudio && pass == 2
	args := withFFmpegGlobalArgs([]string{"-y"})
	for _, inputPath := range inputPaths {
		args = append(args, "-i", strings.TrimSpace(inputPath))
	}
	args = append(args, "-filter_complex", buildMergeCrossfadeFilter(len(inputPaths), transitionDuration, offsets, includeAudio))
	args = append(args, "-map", "[vout]")
	if includeAudio {
		args = append(args, "-map", "[aout]")
	}

	return appendTargetPassArgs(args, encoding, req.RemoveAudio || pass == 1, pass, passLogPrefix, outputPath), nil
}

// mergeCrossfadeOffsets validates the inputs and computes every xfade offset.
func mergeCrossfadeOffsets(durations []float64, transitionDuration float64) ([]float64, *VideoError) {
	offsets := make([]float64, 0, len(durations)-1)
	cumulative := 0.0
	for index, duration := range durations {
		if math.IsNaN(duration) || math.IsInf(duration, 0) || duration <= transitionDuration {
			return nil, &VideoError{
				Code:    ErrorCodeVideoMergeValidation,
				Message: fmt.Sprintf("input %d is too short (%.2f s) for a %.2f s crossfade", index+1, duration, transitionDuration),
			}
		}
		if index == 0 {
			continue
		}
		cumulative += durations[index-1]
		offset := cumulative - float64(index)*transitionDuration
		if offset < 0 {
			return nil, &VideoError{
				Code:    ErrorCodeVideoMergeValidation,
				Message: fmt.Sprintf("crossfade offset for input %d must be >= 0", index+1),
			}
		}
		offsets = append(offsets, offset)
	}

	return offsets, nil
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
