package recipes

import (
	"context"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/tools"
	"fileforge-desktop/internal/verify"
	"fileforge-desktop/internal/video"
	"fileforge-desktop/internal/video/engine"
)

const (
	// RecipeIDBurnedSubtitles is the curated outcome "Burned-in subtitles".
	RecipeIDBurnedSubtitles = "recipe.burned_subtitles"

	burnedSubtitlesStepTranscribe = "transcribe"
	burnedSubtitlesStepBurn       = "burn"
	burnedSubtitlesStepVerify     = "verify"

	burnedSubtitlesStepIndexTranscribe = 0
	burnedSubtitlesStepIndexBurn       = 1
	burnedSubtitlesStepIndexVerify     = 2
	burnedSubtitlesStepCount           = 3

	burnedSubtitlesSuffix = "_subtitled"

	burnedSubtitlesProgressStart     = 5
	burnedSubtitlesTranscribeEnd     = 55
	burnedSubtitlesBurnStart         = 60
	burnedSubtitlesBurnEnd           = 95
	burnedSubtitlesPublishStart      = 96
	burnedSubtitlesVerifyingProgress = 97
	burnedSubtitlesProgressEnd       = 100

	burnedSubtitlesDurationTolerance = 0.25
	burnedSubtitlesTimingSlack       = 0.5
	burnedSubtitlesReasonLimit       = 500
	burnedSubtitlesTempPattern       = ".fileforge-burned-subs-*.srt"
	burnedSubtitlesFrameTempPattern  = "fileforge-burned-frames-*"

	burnedSubtitlesCueMinimumSeconds = 0.4
	burnedSubtitlesGapMinimumSeconds = 0.5
	// The proof counts changed pixels instead of averaging the whole band:
	// a short cue on a dark scene changes ~0.6% of the band while the mean
	// difference stays below 1.5, and the ratio stays stable from 360p to 4K.
	burnedSubtitlesCueChangedRatio       = 0.003
	burnedSubtitlesMaxControlRatio       = 0.01
	burnedSubtitlesControlMultiplier     = 3.0
	burnedSubtitlesChangedPixelThreshold = 32.0
	burnedSubtitlesBottomBandRatio       = 0.35

	burnedSubtitlesDurationCheck  = "recipe.duration_match"
	burnedSubtitlesFrameSizeCheck = "recipe.frame_size"
	burnedSubtitlesAudioCheck     = "recipe.audio_present"
	burnedSubtitlesCueCountCheck  = "recipe.srt_cues"
	burnedSubtitlesCueTimingCheck = "recipe.srt_timing"
	burnedSubtitlesRenderedCheck  = "recipe.subtitles_rendered"

	burnedSubtitlesFrameWarning = "Rendered-frame verification could not run."
)

// burnedSubtitlesBurn is the engine entry point. It is a package variable so
// tests can render (or fail) a burn without running ffmpeg.
var burnedSubtitlesBurn = engine.Burn

// burnedSubtitlesVerifyFrames proves that the cues reached the picture. It is a
// package variable so execution tests can stub the frame comparison.
var burnedSubtitlesVerifyFrames = verifyBurnedSubtitlesFrames

// burnedSubtitlesResolution is the recipe-owned plan: the source video, the
// burned MP4 and the SRT sidecar destinations plus the probed metadata used by
// verification. It is resolved during Plan and reused by Run.
type burnedSubtitlesResolution struct {
	SourcePath   string
	VideoPath    string
	SubtitlePath string
	Metadata     inspect.VideoMetadata
}

// srtCue is one subtitle cue parsed from an SRT file.
type srtCue struct {
	Start float64
	End   float64
	Text  string
}

func burnSubtitlesRecipe() models.RecipeV1 {
	return models.RecipeV1{
		ID:               RecipeIDBurnedSubtitles,
		Name:             "Burned-in subtitles",
		Description:      "Transcribe a video locally and burn synchronized subtitles into a new MP4.",
		Category:         "video",
		InputKind:        "video",
		AcceptExtensions: []string{"mp4", "mov", "mkv", "webm", "avi"},
		MaxInputs:        1,
		Highlights:       []string{"Local Whisper", "Synchronized burn-in", "SRT sidecar"},
		Steps: []models.RecipeStepV1{
			{
				ID:     burnedSubtitlesStepTranscribe,
				Name:   "Transcribe audio",
				ToolID: video.ToolIDVideoTranscribeV1,
				Detail: "Whisper writes timed subtitles locally",
			},
			{
				ID:     burnedSubtitlesStepBurn,
				Name:   "Burn subtitles",
				Detail: "ffmpeg renders the cues into the picture; audio is copied",
			},
			{
				ID:     burnedSubtitlesStepVerify,
				Name:   "Verify sync",
				Detail: "Duration, cues and rendered frames are checked",
			},
		},
		DefaultTargetSizeBytes: 0,
	}
}

// resolveBurnedSubtitles validates the single video, requires an audio track
// and resolves the collision-safe MP4 plus SRT destinations.
func resolveBurnedSubtitles(recipe *models.RecipeV1, req models.RecipeRequestV1) (*resolvedRecipe, *models.JobErrorV1) {
	inputs, inputErr := validateBurnedSubtitlesInputs(req.InputPaths)
	if inputErr != nil {
		return nil, inputErr
	}
	sourcePath := inputs[0]

	stat, statErr := os.Stat(sourcePath)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return nil, newRecipeError(
				"RECIPE_INPUT_UNREADABLE",
				fmt.Sprintf("The source video was not found: %s", filepath.Base(sourcePath)),
				map[string]any{"inputPath": sourcePath},
			)
		}
		return nil, newRecipeError(
			"RECIPE_INPUT_UNREADABLE",
			fmt.Sprintf("The source video cannot be read: %s", filepath.Base(sourcePath)),
			map[string]any{"inputPath": sourcePath},
		)
	}
	if !stat.Mode().IsRegular() {
		return nil, newRecipeError(
			"RECIPE_INPUT_UNREADABLE",
			fmt.Sprintf("The source is not a regular file: %s", filepath.Base(sourcePath)),
			map[string]any{"inputPath": sourcePath},
		)
	}

	metadata, ok := inspect.ProbeVideoMetadata(context.Background(), sourcePath)
	if !ok || metadata.DurationSeconds <= 0 || metadata.Width <= 0 || metadata.Height <= 0 || strings.TrimSpace(metadata.VideoCodec) == "" {
		return nil, newRecipeError(
			"RECIPE_INPUT_UNREADABLE",
			fmt.Sprintf("Could not read the video metadata for %s.", filepath.Base(sourcePath)),
			map[string]any{"inputPath": sourcePath},
		)
	}
	if strings.TrimSpace(metadata.AudioCodec) == "" {
		return nil, newRecipeError(
			"RECIPE_INPUT_NO_AUDIO",
			"This video has no audio track to transcribe.",
			map[string]any{"inputPath": sourcePath},
		)
	}

	outputDir, dirErr := resolveRecipeOutputDir(req.OutputDir, sourcePath)
	if dirErr != nil {
		return nil, dirErr
	}

	stem := recipeSourceStem(sourcePath)
	used := make(map[string]struct{}, 3)
	outputplan.MarkUsed(used, sourcePath)

	videoPath, _ := outputplan.UniqueFilePath(outputDir, stem+burnedSubtitlesSuffix, "mp4", used)
	subtitlePath, _ := outputplan.UniqueFilePath(outputDir, stem+burnedSubtitlesSuffix, "srt", used)

	planned := []models.PlannedOutputV1{
		{InputPath: sourcePath, OutputPath: videoPath, OutputKind: "file", OutputCount: 1},
		{InputPath: sourcePath, OutputPath: subtitlePath, OutputKind: "file", OutputCount: 1},
	}

	warnings := make([]string, 0, 1)
	if !burnedSubtitlesAudioCopyable(metadata.AudioCodec) {
		warnings = append(warnings, "Audio will be re-encoded to AAC for compatibility.")
	}

	return &resolvedRecipe{
		Recipe:         recipe,
		Inputs:         inputs,
		OutputDir:      outputDir,
		PlannedOutputs: planned,
		PlanMessage:    fmt.Sprintf("Ready to transcribe and burn subtitles into %s.", filepath.Base(sourcePath)),
		Burn: &burnedSubtitlesResolution{
			SourcePath:   sourcePath,
			VideoPath:    videoPath,
			SubtitlePath: subtitlePath,
			Metadata:     metadata,
		},
		Warnings: warnings,
	}, nil
}

func validateBurnedSubtitlesInputs(rawInputs []string) ([]string, *models.JobErrorV1) {
	if len(rawInputs) == 0 {
		return nil, newRecipeError("RECIPE_INVALID_INPUT", "Choose one video for Burned-in subtitles.", nil)
	}

	inputs := make([]string, 0, len(rawInputs))
	for _, rawInput := range rawInputs {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return nil, newRecipeError("RECIPE_INVALID_INPUT", "Every input must be a video file path.", nil)
		}
		inputs = append(inputs, inputPath)
	}

	if len(inputs) != 1 {
		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			"Burned-in subtitles takes exactly one video. Remove the extra files.",
			nil,
		)
	}

	inputPath := inputs[0]
	if !supportsBurnedSubtitlesExtension(inputPath) {
		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			fmt.Sprintf("Only .mp4, .mov, .mkv, .webm and .avi videos are supported: %s", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}

	return inputs, nil
}

func supportsBurnedSubtitlesExtension(inputPath string) bool {
	switch strings.ToLower(filepath.Ext(inputPath)) {
	case ".mp4", ".mov", ".mkv", ".webm", ".avi":
		return true
	default:
		return false
	}
}

// burnedSubtitlesAudioCopyable mirrors the engine rule: these codecs can be
// copied into the MP4 container, everything else is re-encoded to AAC.
func burnedSubtitlesAudioCopyable(codec string) bool {
	switch strings.ToLower(strings.TrimSpace(codec)) {
	case "aac", "mp3", "ac3", "eac3", "alac", "opus":
		return true
	default:
		return false
	}
}

func (s *Service) executeBurnedSubtitles(ctx context.Context, tracked *recipeJob, resolved *resolvedRecipe) {
	burned := resolved.Burn
	if burned == nil || len(resolved.Recipe.Steps) != burnedSubtitlesStepCount {
		s.failJob(tracked, newRecipeError("RECIPE_INVALID_DEFINITION", "The Burned-in subtitles recipe definition is invalid.", nil))
		return
	}
	if ctx.Err() != nil {
		s.cancelJob(tracked)
		return
	}

	transcribeStep := resolved.Recipe.Steps[burnedSubtitlesStepIndexTranscribe]
	burnStep := resolved.Recipe.Steps[burnedSubtitlesStepIndexBurn]
	verifyStep := resolved.Recipe.Steps[burnedSubtitlesStepIndexVerify]

	// lastPercent keeps the progress monotonic and silent when a stage repeats
	// the same percentage.
	lastPercent := -1
	emit := func(stepName, stage, message string, percent int) {
		if percent < 0 {
			percent = 0
		}
		if percent > burnedSubtitlesProgressEnd {
			percent = burnedSubtitlesProgressEnd
		}
		if percent <= lastPercent {
			return
		}
		lastPercent = percent
		s.emitProgress(tracked, models.JobStatusRunning, stepName, stage, message, percent)
	}

	tracked.begin(1)
	emit(transcribeStep.Name, "starting", fmt.Sprintf("Starting %s.", resolved.Recipe.Name), burnedSubtitlesProgressStart)

	tempDir, tempErr := s.tempDir()
	if tempErr != nil {
		s.failJob(tracked, newRecipeError(
			"RECIPE_TEMP_WORKSPACE_FAILED",
			fmt.Sprintf("Could not create the recipe workspace: %v", tempErr),
			nil,
		))
		return
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	// created tracks every path this run may have written. It is only used to
	// clean up on failure or cancel, so a pre-existing file is never removed.
	created := make([]string, 0, 2)

	srtPath, cues, jobErr := s.runBurnedSubtitlesTranscribeStep(ctx, tracked, burned, transcribeStep, tempDir, func(stage, message string, percent int) {
		emit(transcribeStep.Name, stage, message, percent)
	})
	if jobErr != nil {
		s.abortBurnedSubtitles(ctx, tracked, transcribeStep, burnedSubtitlesStepIndexTranscribe, created, jobErr)
		return
	}

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}
	tracked.setCurrentStep(burnedSubtitlesStepIndexBurn + 1)

	videoPath, jobErr := s.runBurnedSubtitlesBurnStep(ctx, tracked, burned, burnStep, srtPath, &created, func(stage, message string, percent int) {
		emit(burnStep.Name, stage, message, percent)
	})
	if jobErr != nil {
		s.abortBurnedSubtitles(ctx, tracked, burnStep, burnedSubtitlesStepIndexBurn, created, jobErr)
		return
	}

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}
	tracked.setCurrentStep(burnedSubtitlesStepIndexVerify + 1)

	emit(verifyStep.Name, "verify", "Saving the SRT sidecar.", burnedSubtitlesPublishStart)

	srtContent, readErr := os.ReadFile(srtPath)
	if readErr != nil {
		sidecarErr := newRecipeError(
			"RECIPE_SIDECAR_FAILED",
			fmt.Sprintf("Could not read the transcribed subtitles: %v", readErr),
			map[string]any{"outputPath": srtPath},
		)
		s.abortBurnedSubtitles(ctx, tracked, verifyStep, burnedSubtitlesStepIndexVerify, created, sidecarErr)
		return
	}

	if _, statErr := os.Stat(burned.SubtitlePath); statErr == nil {
		existsErr := newRecipeError(
			"RECIPE_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another output folder.", filepath.Base(burned.SubtitlePath)),
			map[string]any{"outputPath": burned.SubtitlePath},
		)
		s.abortBurnedSubtitles(ctx, tracked, verifyStep, burnedSubtitlesStepIndexVerify, created, existsErr)
		return
	} else if !errors.Is(statErr, os.ErrNotExist) {
		accessErr := newRecipeError(
			"RECIPE_SIDECAR_FAILED",
			fmt.Sprintf("%s cannot be accessed.", filepath.Base(burned.SubtitlePath)),
			map[string]any{"outputPath": burned.SubtitlePath},
		)
		s.abortBurnedSubtitles(ctx, tracked, verifyStep, burnedSubtitlesStepIndexVerify, created, accessErr)
		return
	}

	// The sidecar is claimed before it is written so a partial file is cleaned
	// up on every failure.
	created = append(created, burned.SubtitlePath)
	if publishErr := publishBurnedSubtitlesSRT(burned.SubtitlePath, srtContent); publishErr != nil {
		s.abortBurnedSubtitles(ctx, tracked, verifyStep, burnedSubtitlesStepIndexVerify, created, publishErr)
		return
	}

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}

	emit(verifyStep.Name, "verify", "Verifying duration, cues and rendered frames.", burnedSubtitlesVerifyingProgress)

	outputs := []string{videoPath, burned.SubtitlePath}
	report := verify.VerifyPaths(outputs)
	report = addBurnedSubtitlesChecks(ctx, report, burned, cues, videoPath, burned.SubtitlePath)

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}

	status := models.JobStatusSuccess
	message := fmt.Sprintf("Burned-in subtitles ready: %s and %s.", filepath.Base(videoPath), filepath.Base(burned.SubtitlePath))
	verifyMessage := "Duration, cues and rendered frames verified."
	if !report.Success {
		status = models.JobStatusPartialSuccess
		message = fmt.Sprintf("Burned-in subtitles ready, but verification reported: %s.", report.Message)
		verifyMessage = "Verification found: " + report.Message + "."
	}

	if s.history != nil {
		s.history.RecordOutputs(tracked.jobID(), burnedSubtitlesHistoryToolID, outputs)
	}

	tracked.setStep(burnedSubtitlesStepIndexVerify, models.RecipeStepResultV1{
		ID:      verifyStep.ID,
		Name:    verifyStep.Name,
		ToolID:  verifyStep.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: verifyMessage,
		Outputs: outputs,
	})

	tracked.finish(status, message, outputs, &report, nil, true)
	s.emitProgress(tracked, status, verifyStep.Name, status, message, burnedSubtitlesProgressEnd)
}

// abortBurnedSubtitles removes the files this run created and reports the step
// failure or the cancellation, never both.
func (s *Service) abortBurnedSubtitles(ctx context.Context, tracked *recipeJob, step models.RecipeStepV1, stepIndex int, created []string, jobErr *models.JobErrorV1) {
	removeCreatedFiles(created)
	if isCancellation(ctx, jobErr) {
		s.cancelJob(tracked)
		return
	}

	tracked.setStep(stepIndex, failedRecipeStep(step, jobErr))
	s.failJob(tracked, jobErr)
}

// runBurnedSubtitlesTranscribeStep plans and runs Whisper into a temp SRT and
// returns the parsed cues. The temp SRT never appears in the step outputs.
func (s *Service) runBurnedSubtitlesTranscribeStep(ctx context.Context, tracked *recipeJob, burned *burnedSubtitlesResolution, step models.RecipeStepV1, tempDir string, emit func(stage, message string, percent int)) (string, []srtCue, *models.JobErrorV1) {
	if ctx.Err() != nil {
		return "", nil, recipeCancelledError()
	}

	executor, planner, jobErr := s.singleExecutor(video.ToolIDVideoTranscribeV1)
	if jobErr != nil {
		return "", nil, jobErr
	}

	req := models.JobRequestV1{
		ToolID:     video.ToolIDVideoTranscribeV1,
		Mode:       "single",
		InputPaths: []string{burned.SourcePath},
		OutputDir:  tempDir,
		Options: map[string]any{
			"formats":  []string{"srt"},
			"language": "auto",
		},
	}

	plan, planErr := planner.Plan(ctx, req)
	if planErr != nil {
		return "", nil, planErr
	}
	if len(plan.Outputs) == 0 {
		return "", nil, newRecipeError("RECIPE_STEP_FAILED", "The transcription step planned no subtitle output.", nil)
	}
	srtPath := strings.TrimSpace(plan.Outputs[0].OutputPath)
	if !strings.EqualFold(filepath.Ext(srtPath), ".srt") {
		return "", nil, newRecipeError(
			"RECIPE_STEP_FAILED",
			"The transcription step did not plan an SRT output.",
			map[string]any{"outputPath": srtPath},
		)
	}

	progress := func(event models.JobProgressV1) {
		percent := burnedSubtitlesProgressStart
		if event.Total > 0 {
			percent = burnedSubtitlesProgressStart + int(math.Round(
				float64(event.Current)/float64(event.Total)*float64(burnedSubtitlesTranscribeEnd-burnedSubtitlesProgressStart),
			))
			if percent > burnedSubtitlesTranscribeEnd {
				percent = burnedSubtitlesTranscribeEnd
			}
		}
		emit("transcribe", event.Message, percent)
	}

	var item models.JobResultItemV1
	if progressExecutor, ok := executor.(tools.SingleExecutorWithProgress); ok {
		item, jobErr = progressExecutor.ExecuteSingleWithProgress(ctx, req, progress)
	} else {
		emit("transcribe", "Recognizing speech locally.", 15)
		item, jobErr = executor.ExecuteSingle(ctx, req)
	}
	if jobErr != nil {
		return "", nil, jobErr
	}
	if !item.Success {
		return "", nil, stepItemError(item, video.ToolIDVideoTranscribeV1)
	}

	content, readErr := os.ReadFile(srtPath)
	if readErr != nil {
		return "", nil, newRecipeError(
			"RECIPE_STEP_FAILED",
			fmt.Sprintf("The transcription step did not write its subtitles: %v", readErr),
			map[string]any{"outputPath": srtPath},
		)
	}

	cues := parseSRTCues(string(content))
	if len(cues) == 0 {
		return "", nil, newRecipeError(
			"RECIPE_NO_SPEECH",
			"No speech was recognized, so there are no subtitles to burn.",
			map[string]any{"outputPath": srtPath},
		)
	}

	tracked.setStep(burnedSubtitlesStepIndexTranscribe, models.RecipeStepResultV1{
		ID:      step.ID,
		Name:    step.Name,
		ToolID:  step.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: fmt.Sprintf("%d cues transcribed.", len(cues)),
	})
	emit("transcribe", fmt.Sprintf("%d cues transcribed.", len(cues)), burnedSubtitlesTranscribeEnd)

	return srtPath, cues, nil
}

// runBurnedSubtitlesBurnStep claims the final video and renders the cues with
// the burn engine. The final path is claimed before the call so a partial
// render is removed on every failure.
func (s *Service) runBurnedSubtitlesBurnStep(ctx context.Context, tracked *recipeJob, burned *burnedSubtitlesResolution, step models.RecipeStepV1, srtPath string, created *[]string, emit func(stage, message string, percent int)) (string, *models.JobErrorV1) {
	if ctx.Err() != nil {
		return "", recipeCancelledError()
	}

	if _, statErr := os.Stat(burned.VideoPath); statErr == nil {
		return "", newRecipeError(
			"RECIPE_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another output folder.", filepath.Base(burned.VideoPath)),
			map[string]any{"outputPath": burned.VideoPath},
		)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", newRecipeError(
			"RECIPE_STEP_FAILED",
			fmt.Sprintf("%s cannot be accessed.", filepath.Base(burned.VideoPath)),
			map[string]any{"outputPath": burned.VideoPath},
		)
	}
	*created = append(*created, burned.VideoPath)

	emit("burn", "Burning subtitles into the video.", burnedSubtitlesBurnStart)

	progress := func(event engine.BurnProgressEvent) {
		percent := burnedSubtitlesBurnStart + int(math.Round(float64(event.Percent)*0.35))
		if percent > burnedSubtitlesBurnEnd {
			percent = burnedSubtitlesBurnEnd
		}
		message := strings.TrimSpace(event.Message)
		if message == "" {
			message = "Burning subtitles into the video."
		}
		emit("burn", message, percent)
	}

	result, burnErr := burnedSubtitlesBurn(ctx, &engine.ExecBurnCommandRunner{}, "ffmpeg", engine.BurnRequest{
		InputPath:    burned.SourcePath,
		SubtitlePath: srtPath,
		OutputPath:   burned.VideoPath,
		AudioCodec:   burned.Metadata.AudioCodec,
	}, progress)
	if burnErr != nil {
		return "", burnedSubtitlesBurnError(burnErr)
	}

	outputPath := strings.TrimSpace(result.OutputPath)
	if outputPath == "" {
		outputPath = burned.VideoPath
	}
	if outputPath != burned.VideoPath {
		*created = append(*created, outputPath)
	}

	tracked.setStep(burnedSubtitlesStepIndexBurn, models.RecipeStepResultV1{
		ID:      step.ID,
		Name:    step.Name,
		ToolID:  step.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: "Subtitles burned into the video.",
		Outputs: []string{outputPath},
	})
	emit("burn", "Subtitles burned into the video.", burnedSubtitlesBurnEnd)

	return outputPath, nil
}

// burnedSubtitlesBurnError maps the typed engine failure to a canonical job
// error with a bounded cause for the UI.
func burnedSubtitlesBurnError(err error) *models.JobErrorV1 {
	if err == nil {
		return nil
	}

	var burnErr *engine.BurnError
	if errors.As(err, &burnErr) && burnErr != nil {
		code := strings.TrimSpace(burnErr.Code)
		if code == "" {
			code = engine.ErrorCodeBurnFailed
		}
		message := strings.TrimSpace(burnErr.Message)
		if message == "" {
			message = "Could not burn the subtitles."
		}

		details := map[string]any{}
		if burnErr.Cause != nil {
			reason := strings.TrimSpace(burnErr.Cause.Error())
			if reason != "" {
				if len(reason) > burnedSubtitlesReasonLimit {
					reason = reason[:burnedSubtitlesReasonLimit] + "…"
				}
				details["reason"] = reason
			}
		}

		return models.NewCanonicalJobError(code, message, details)
	}

	return models.NewCanonicalJobError(
		engine.ErrorCodeBurnFailed,
		fmt.Sprintf("Could not burn the subtitles: %v", err),
		nil,
	)
}

// publishBurnedSubtitlesSRT writes the sidecar to a hidden temp file in the
// destination folder and renames it into place, so a half-written SRT is never
// left at the final path. An existing destination is never overwritten.
func publishBurnedSubtitlesSRT(subtitlePath string, content []byte) *models.JobErrorV1 {
	tempFile, err := os.CreateTemp(filepath.Dir(subtitlePath), burnedSubtitlesTempPattern)
	if err != nil {
		return newRecipeError("RECIPE_SIDECAR_FAILED", fmt.Sprintf("Could not create the SRT sidecar: %v", err), nil)
	}
	tempPath := tempFile.Name()

	if _, writeErr := tempFile.Write(content); writeErr != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return newRecipeError("RECIPE_SIDECAR_FAILED", fmt.Sprintf("Could not write the SRT sidecar: %v", writeErr), nil)
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(tempPath)
		return newRecipeError("RECIPE_SIDECAR_FAILED", fmt.Sprintf("Could not write the SRT sidecar: %v", closeErr), nil)
	}
	if chmodErr := os.Chmod(tempPath, 0o644); chmodErr != nil {
		_ = os.Remove(tempPath)
		return newRecipeError("RECIPE_SIDECAR_FAILED", fmt.Sprintf("Could not save the SRT sidecar: %v", chmodErr), nil)
	}

	if _, statErr := os.Stat(subtitlePath); statErr == nil {
		_ = os.Remove(tempPath)
		return newRecipeError(
			"RECIPE_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another output folder.", filepath.Base(subtitlePath)),
			map[string]any{"outputPath": subtitlePath},
		)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = os.Remove(tempPath)
		return newRecipeError(
			"RECIPE_SIDECAR_FAILED",
			fmt.Sprintf("%s cannot be accessed.", filepath.Base(subtitlePath)),
			map[string]any{"outputPath": subtitlePath},
		)
	}

	if renameErr := os.Rename(tempPath, subtitlePath); renameErr != nil {
		_ = os.Remove(tempPath)
		return newRecipeError("RECIPE_SIDECAR_FAILED", fmt.Sprintf("Could not save the SRT sidecar: %v", renameErr), nil)
	}

	return nil
}

// addBurnedSubtitlesChecks appends the recipe guarantees: the video keeps the
// source duration, frame size and audio, the SRT cues are ordered and inside the
// video, and the cues are visibly rendered into the picture. Any failure marks
// the whole report as failed.
func addBurnedSubtitlesChecks(ctx context.Context, report models.VerificationReportV1, burned *burnedSubtitlesResolution, cues []srtCue, videoPath, subtitlePath string) models.VerificationReportV1 {
	sourceDuration := burned.Metadata.DurationSeconds
	actual, _ := inspect.ProbeVideoMetadata(ctx, videoPath)
	failed := false

	durationPassed := actual.DurationSeconds > 0 &&
		math.Abs(actual.DurationSeconds-sourceDuration) <= burnedSubtitlesDurationTolerance
	durationDetail := ""
	if !durationPassed {
		failed = true
		durationDetail = fmt.Sprintf("expected %.2f s ± %.2f s", sourceDuration, burnedSubtitlesDurationTolerance)
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"%s: expected %.2f s ± %.2f s of video, got %.2f s",
			filepath.Base(videoPath),
			sourceDuration,
			burnedSubtitlesDurationTolerance,
			actual.DurationSeconds,
		))
	}
	appendBurnedSubtitlesCheck(&report, videoPath, models.VerificationCheckV1{
		Key:    burnedSubtitlesDurationCheck,
		Label:  "Source duration",
		Passed: durationPassed,
		Value:  fmt.Sprintf("%.2f s", actual.DurationSeconds),
		Detail: durationDetail,
	})

	frameSizePassed := actual.Width > 0 && actual.Height > 0 &&
		actual.Width == burned.Metadata.Width && actual.Height == burned.Metadata.Height
	frameSizeDetail := ""
	if !frameSizePassed {
		failed = true
		frameSizeDetail = fmt.Sprintf("expected %d × %d px", burned.Metadata.Width, burned.Metadata.Height)
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"%s: expected %d × %d px, got %d × %d px",
			filepath.Base(videoPath),
			burned.Metadata.Width,
			burned.Metadata.Height,
			actual.Width,
			actual.Height,
		))
	}
	appendBurnedSubtitlesCheck(&report, videoPath, models.VerificationCheckV1{
		Key:    burnedSubtitlesFrameSizeCheck,
		Label:  "Frame size",
		Passed: frameSizePassed,
		Value:  fmt.Sprintf("%d × %d px", actual.Width, actual.Height),
		Detail: frameSizeDetail,
	})

	audioPassed := strings.TrimSpace(actual.AudioCodec) != ""
	audioDetail := ""
	if !audioPassed {
		failed = true
		audioDetail = "the source audio track is missing"
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"%s: expected the source audio track, got none",
			filepath.Base(videoPath),
		))
	}
	appendBurnedSubtitlesCheck(&report, videoPath, models.VerificationCheckV1{
		Key:    burnedSubtitlesAudioCheck,
		Label:  "Audio track",
		Passed: audioPassed,
		Value:  codecLabel(actual.AudioCodec),
		Detail: audioDetail,
	})

	cueCountPassed := len(cues) > 0
	cueCountDetail := ""
	if !cueCountPassed {
		failed = true
		cueCountDetail = "the SRT sidecar has no cues"
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: no subtitle cues", filepath.Base(subtitlePath)))
	}
	appendBurnedSubtitlesCheck(&report, subtitlePath, models.VerificationCheckV1{
		Key:    burnedSubtitlesCueCountCheck,
		Label:  "Subtitle cues",
		Passed: cueCountPassed,
		Value:  fmt.Sprintf("%d cues", len(cues)),
		Detail: cueCountDetail,
	})

	timingPassed, timingReason := srtCueTimingValid(cues, sourceDuration)
	timingDetail := ""
	timingValue := "valid"
	if !timingPassed {
		failed = true
		timingValue = "invalid"
		timingDetail = timingReason
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: %s", filepath.Base(subtitlePath), timingReason))
	}
	appendBurnedSubtitlesCheck(&report, subtitlePath, models.VerificationCheckV1{
		Key:    burnedSubtitlesCueTimingCheck,
		Label:  "Subtitle timing",
		Passed: timingPassed,
		Value:  timingValue,
		Detail: timingDetail,
	})

	renderedPassed, renderedValue, renderedDetail, frameWarnings := burnedSubtitlesVerifyFrames(ctx, burned.SourcePath, videoPath, cues, sourceDuration)
	report.Warnings = append(report.Warnings, frameWarnings...)
	if !renderedPassed {
		failed = true
	}
	appendBurnedSubtitlesCheck(&report, videoPath, models.VerificationCheckV1{
		Key:    burnedSubtitlesRenderedCheck,
		Label:  "Rendered subtitles",
		Passed: renderedPassed,
		Value:  renderedValue,
		Detail: renderedDetail,
	})

	if failed {
		report.Success = false
		report.Message = "verification failed: the burned subtitles do not match the source"
	}

	return report
}

// appendBurnedSubtitlesCheck attaches a check to the verified output with the
// same path. A missing entry falls back to the first output so a check is never
// silently dropped.
func appendBurnedSubtitlesCheck(report *models.VerificationReportV1, path string, check models.VerificationCheckV1) {
	key := canonicalInputKey(path)
	for index := range report.Outputs {
		if canonicalInputKey(report.Outputs[index].Path) == key {
			report.Outputs[index].Checks = append(report.Outputs[index].Checks, check)
			return
		}
	}

	if len(report.Outputs) > 0 {
		report.Outputs[0].Checks = append(report.Outputs[0].Checks, check)
	}
}

// verifyBurnedSubtitlesFrames samples cue midpoints in the source and the burned
// video and compares the bottom band. It also samples the largest cue gap as a
// negative control, so an unrelated re-encode difference cannot fake a pass.
func verifyBurnedSubtitlesFrames(ctx context.Context, sourcePath, outputPath string, cues []srtCue, durationSeconds float64) (passed bool, value, detail string, warnings []string) {
	if ctx == nil {
		ctx = context.Background()
	}

	eligible := make([]srtCue, 0, len(cues))
	for _, cue := range cues {
		if cue.End-cue.Start >= burnedSubtitlesCueMinimumSeconds {
			eligible = append(eligible, cue)
		}
	}
	if len(eligible) == 0 {
		return false, "", "no cue is long enough to sample a rendered frame", []string{burnedSubtitlesFrameWarning}
	}

	samples := make([]srtCue, 0, 3)
	switch len(eligible) {
	case 1, 2:
		samples = append(samples, eligible...)
	default:
		samples = append(samples, eligible[0], eligible[len(eligible)/2], eligible[len(eligible)-1])
	}

	controlTime, hasControl := burnedSubtitlesControlTime(cues)

	tempDir, tempErr := os.MkdirTemp("", burnedSubtitlesFrameTempPattern)
	if tempErr != nil {
		return false, "", "", []string{burnedSubtitlesFrameWarning}
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	controlRatio := 0.0
	if hasControl {
		controlRatio, tempErr = burnedSubtitlesFrameDifference(ctx, sourcePath, outputPath, controlTime, filepath.Join(tempDir, "control"))
		if tempErr != nil {
			return false, "", "", []string{burnedSubtitlesFrameWarning}
		}
	}

	requiredRatio := burnedSubtitlesCueChangedRatio
	if hasControl && controlRatio*burnedSubtitlesControlMultiplier > requiredRatio {
		requiredRatio = controlRatio * burnedSubtitlesControlMultiplier
	}

	details := make([]string, 0, len(samples)+1)
	signalCues := 0
	for index, cue := range samples {
		atSeconds := cue.Start + (cue.End-cue.Start)/2
		if durationSeconds > 0 && atSeconds > durationSeconds-0.05 {
			atSeconds = durationSeconds - 0.05
		}
		if atSeconds <= 0 {
			return false, "", "a cue starts at the beginning of the video", []string{burnedSubtitlesFrameWarning}
		}

		diff, diffErr := burnedSubtitlesFrameDifference(
			ctx,
			sourcePath,
			outputPath,
			atSeconds,
			filepath.Join(tempDir, fmt.Sprintf("cue-%d", index)),
		)
		if diffErr != nil {
			return false, "", "", []string{burnedSubtitlesFrameWarning}
		}

		details = append(details, fmt.Sprintf("cue %d at %.2f s: %.4f", index+1, atSeconds, diff))
		if diff >= requiredRatio {
			signalCues++
		}
	}
	if hasControl {
		details = append(details, fmt.Sprintf("control at %.2f s: %.4f", controlTime, controlRatio))
	}

	required := 2
	if len(samples) < required {
		required = len(samples)
	}

	value = fmt.Sprintf("%d of %d cues", signalCues, len(samples))
	detail = strings.Join(details, "; ")
	passed = signalCues >= required
	if hasControl && controlRatio >= burnedSubtitlesMaxControlRatio {
		passed = false
		detail += fmt.Sprintf("; control frames differ by %.4f (expected < %.4f)", controlRatio, burnedSubtitlesMaxControlRatio)
	}

	return passed, value, detail, nil
}

// burnedSubtitlesControlTime returns the midpoint of the largest gap before or
// between cues. A gap under 0.5 s is not a usable negative control.
func burnedSubtitlesControlTime(cues []srtCue) (float64, bool) {
	if len(cues) == 0 {
		return 0, false
	}

	bestStart := 0.0
	bestGap := 0.0
	previousEnd := 0.0
	for _, cue := range cues {
		if gap := cue.Start - previousEnd; gap > bestGap {
			bestGap = gap
			bestStart = previousEnd
		}
		if cue.End > previousEnd {
			previousEnd = cue.End
		}
	}

	if bestGap < burnedSubtitlesGapMinimumSeconds {
		return 0, false
	}

	return bestStart + bestGap/2, true
}

// burnedSubtitlesFrameDifference extracts the same timestamp from the source and
// the burned video and returns the fraction of bottom-band pixels that changed
// beyond the re-encode noise floor.
func burnedSubtitlesFrameDifference(ctx context.Context, sourcePath, outputPath string, atSeconds float64, prefix string) (float64, error) {
	sourceFrame := prefix + "-source.png"
	outputFrame := prefix + "-output.png"

	if err := burnedSubtitlesExtractFrame(ctx, sourcePath, atSeconds, sourceFrame); err != nil {
		return 0, err
	}
	if err := burnedSubtitlesExtractFrame(ctx, outputPath, atSeconds, outputFrame); err != nil {
		return 0, err
	}

	return burnedSubtitlesBottomBandChangedRatio(sourceFrame, outputFrame)
}

// burnedSubtitlesExtractFrame writes one PNG frame using an input-side seek so
// both videos are sampled at the exact same timestamp.
func burnedSubtitlesExtractFrame(ctx context.Context, inputPath string, atSeconds float64, outputPath string) error {
	args := []string{
		"-y",
		"-ss", strconv.FormatFloat(atSeconds, 'f', 3, 64),
		"-i", inputPath,
		"-frames:v", "1",
		outputPath,
	}

	output, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg frame extraction failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	return nil
}

func burnedSubtitlesBottomBandChangedRatio(sourcePath, outputPath string) (float64, error) {
	sourceImage, err := burnedSubtitlesReadPNG(sourcePath)
	if err != nil {
		return 0, err
	}
	outputImage, err := burnedSubtitlesReadPNG(outputPath)
	if err != nil {
		return 0, err
	}

	sourceBounds := sourceImage.Bounds()
	outputBounds := outputImage.Bounds()
	width := sourceBounds.Dx()
	if outputBounds.Dx() < width {
		width = outputBounds.Dx()
	}
	height := sourceBounds.Dy()
	if outputBounds.Dy() < height {
		height = outputBounds.Dy()
	}
	if width <= 0 || height <= 0 {
		return 0, errors.New("the extracted frames are empty")
	}

	bandHeight := int(math.Ceil(float64(height) * burnedSubtitlesBottomBandRatio))
	if bandHeight < 1 {
		bandHeight = 1
	}
	if bandHeight > height {
		bandHeight = height
	}
	startY := height - bandHeight

	changed := 0
	for y := startY; y < height; y++ {
		for x := 0; x < width; x++ {
			sourceGray := burnedSubtitlesGray(sourceImage.At(sourceBounds.Min.X+x, sourceBounds.Min.Y+y))
			outputGray := burnedSubtitlesGray(outputImage.At(outputBounds.Min.X+x, outputBounds.Min.Y+y))
			if math.Abs(sourceGray-outputGray) >= burnedSubtitlesChangedPixelThreshold {
				changed++
			}
		}
	}

	return float64(changed) / float64(width*bandHeight), nil
}

func burnedSubtitlesReadPNG(path string) (image.Image, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	return png.Decode(file)
}

func burnedSubtitlesGray(value color.Color) float64 {
	red, green, blue, _ := value.RGBA()

	return (0.299*float64(red) + 0.587*float64(green) + 0.114*float64(blue)) / 256.0
}

// parseSRTCues reads every well-formed cue and silently skips malformed blocks,
// so one broken cue never loses the rest of the subtitle file.
func parseSRTCues(content string) []srtCue {
	normalized := strings.ReplaceAll(content, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	cues := make([]srtCue, 0, 16)
	for _, block := range strings.Split(normalized, "\n\n") {
		lines := strings.Split(block, "\n")

		timingIndex := -1
		for index, line := range lines {
			if strings.Contains(line, "-->") {
				timingIndex = index
				break
			}
		}
		if timingIndex < 0 {
			continue
		}

		startText, endText, found := strings.Cut(lines[timingIndex], "-->")
		if !found {
			continue
		}
		start, startOK := parseSRTTimestamp(startText)
		end, endOK := parseSRTTimestamp(endText)
		if !startOK || !endOK || end <= start {
			continue
		}

		text := strings.TrimSpace(strings.Join(lines[timingIndex+1:], " "))
		cues = append(cues, srtCue{Start: start, End: end, Text: text})
	}

	return cues
}

// parseSRTTimestamp parses HH:MM:SS,mmm and HH:MM:SS.mmm. Hours may exceed 23
// and only the first whitespace-separated field is read, so trailing SRT
// coordinates are ignored.
func parseSRTTimestamp(value string) (float64, bool) {
	fields := strings.Fields(strings.TrimSpace(value))
	if len(fields) == 0 {
		return 0, false
	}

	trimmed := strings.Replace(fields[0], ",", ".", 1)
	parts := strings.Split(trimmed, ":")
	if len(parts) != 3 {
		return 0, false
	}

	hours, err := strconv.Atoi(parts[0])
	if err != nil || hours < 0 {
		return 0, false
	}
	minutes, err := strconv.Atoi(parts[1])
	if err != nil || minutes < 0 || minutes > 59 {
		return 0, false
	}

	secondsText, fractionText, hasFraction := strings.Cut(parts[2], ".")
	seconds, err := strconv.Atoi(secondsText)
	if err != nil || seconds < 0 || seconds > 59 {
		return 0, false
	}

	fraction := 0.0
	if hasFraction {
		if fractionText == "" {
			return 0, false
		}
		digits, digitsErr := strconv.Atoi(fractionText)
		if digitsErr != nil || digits < 0 {
			return 0, false
		}
		fraction = float64(digits) / math.Pow(10, float64(len(fractionText)))
	}

	return float64(hours)*3600 + float64(minutes)*60 + float64(seconds) + fraction, true
}

// srtCueTimingValid checks the order and bounds of the cues. The reason is kept
// short enough to show as a verification detail.
func srtCueTimingValid(cues []srtCue, durationSeconds float64) (bool, string) {
	if len(cues) == 0 {
		return false, "no cues were found"
	}
	if durationSeconds <= 0 {
		return false, "the source duration is unknown"
	}

	for index, cue := range cues {
		if cue.End <= cue.Start {
			return false, fmt.Sprintf("cue %d ends before it starts", index+1)
		}
		if index > 0 && cue.Start < cues[index-1].Start {
			return false, fmt.Sprintf("cue %d starts before cue %d", index+1, index)
		}
	}

	if lastEnd := cues[len(cues)-1].End; lastEnd > durationSeconds+burnedSubtitlesTimingSlack {
		return false, fmt.Sprintf("the last cue ends at %.2f s, after the %.2f s video", lastEnd, durationSeconds)
	}

	return true, ""
}
