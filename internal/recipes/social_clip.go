package recipes

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/verify"
	"fileforge-desktop/internal/video"
)

const (
	stepIDSocialMP4    = "mp4"
	stepIDSocialWebM   = "webm"
	stepIDSocialPoster = "poster"

	socialMP4StepIndex    = 0
	socialWebMStepIndex   = 1
	socialPosterStepIndex = 2
	socialClipStepCount   = 3

	socialClipMP4Suffix             = "-social"
	socialClipPosterSuffix          = "-poster"
	socialClipPosterTempPattern     = ".fileforge-social-poster-*.jpg"
	socialClipPosterMinimumDuration = 2.0

	socialClipExpectedVideoCodecCheck = "expected_video_codec"
	socialClipExpectedAudioCodecCheck = "expected_audio_codec"
	socialClipSourceDurationCheck     = "source_duration"
)

// socialClipPosterCommand extracts one JPEG frame at atSeconds. It is a package
// variable so tests can simulate a poster failure without a broken ffmpeg.
var socialClipPosterCommand = func(ctx context.Context, inputPath, outputPath string, atSeconds float64) error {
	args := []string{
		"-y",
		"-ss", strconv.FormatFloat(atSeconds, 'f', 3, 64),
		"-i", inputPath,
		"-frames:v", "1",
		"-q:v", "2",
		outputPath,
	}

	output, err := exec.CommandContext(ctx, "ffmpeg", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("ffmpeg poster extraction failed: %w (%s)", err, strings.TrimSpace(string(output)))
	}

	return nil
}

// socialClipResolution is the recipe-owned plan: the two encodes, the poster
// destination and the source metadata used for naming, warnings and
// verification. It is resolved during Plan and reused by Run.
type socialClipResolution struct {
	SourcePath string
	OutputDir  string
	MP4Path    string
	WebMPath   string
	PosterPath string
	Metadata   inspect.VideoMetadata
	Warnings   []string
}

// socialClipEncodeStage describes one video encode of the recipe.
type socialClipEncodeStage struct {
	Step          models.RecipeStepV1
	StepIndex     int
	Format        string
	Path          string
	Label         string
	SuccessDetail string
	FromPercent   int
	ToPercent     int
}

func socialClipRecipe() models.RecipeV1 {
	return models.RecipeV1{
		ID:               RecipeIDSocialClip,
		Name:             "Social clip",
		Description:      "Encode one video as MP4 and WebM and export a poster frame, ready for social uploads.",
		Category:         "video",
		InputKind:        "video",
		AcceptExtensions: []string{"mp4", "mov", "mkv", "webm", "avi"},
		MaxInputs:        1,
		Highlights:       []string{"MP4 + WebM", "Source resolution", "Poster frame"},
		Steps: []models.RecipeStepV1{
			{
				ID:     stepIDSocialMP4,
				Name:   "Encode MP4",
				ToolID: video.ToolIDVideoConvertV1,
				Detail: "H.264 + AAC at the source resolution",
			},
			{
				ID:     stepIDSocialWebM,
				Name:   "Encode WebM",
				ToolID: video.ToolIDVideoConvertV1,
				Detail: "VP9 + Opus at the source resolution",
			},
			{
				ID:     stepIDSocialPoster,
				Name:   "Extract poster",
				Detail: "JPEG frame from the first second",
			},
		},
	}
}

func resolveSocialClip(recipe *models.RecipeV1, req models.RecipeRequestV1) (*resolvedRecipe, *models.JobErrorV1) {
	inputs, inputErr := validateSocialClipInputs(req.InputPaths)
	if inputErr != nil {
		return nil, inputErr
	}
	sourcePath := inputs[0]

	outputDir, dirErr := resolveRecipeOutputDir(req.OutputDir, sourcePath)
	if dirErr != nil {
		return nil, dirErr
	}

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

	stem := recipeSourceStem(sourcePath)
	used := make(map[string]struct{}, 3)

	mp4Path, _ := outputplan.UniqueFilePath(outputDir, stem+socialClipMP4Suffix, "mp4", used)
	webmPath, _ := outputplan.UniqueFilePath(outputDir, stem+socialClipMP4Suffix, "webm", used)
	posterPath, _ := outputplan.UniqueFilePath(outputDir, stem+socialClipPosterSuffix, "jpg", used)

	planned := []models.PlannedOutputV1{
		{InputPath: sourcePath, OutputPath: mp4Path, OutputKind: "file", OutputCount: 1},
		{InputPath: sourcePath, OutputPath: webmPath, OutputKind: "file", OutputCount: 1},
		{InputPath: sourcePath, OutputPath: posterPath, OutputKind: "file", OutputCount: 1},
	}

	warnings := socialClipPlanWarnings(metadata)

	return &resolvedRecipe{
		Recipe:         recipe,
		Inputs:         inputs,
		OutputDir:      outputDir,
		PlannedOutputs: planned,
		PlanMessage:    fmt.Sprintf("Ready to build %d files from %s.", len(planned), filepath.Base(sourcePath)),
		Social: &socialClipResolution{
			SourcePath: sourcePath,
			OutputDir:  outputDir,
			MP4Path:    mp4Path,
			WebMPath:   webmPath,
			PosterPath: posterPath,
			Metadata:   metadata,
			Warnings:   warnings,
		},
		Warnings: warnings,
	}, nil
}

func validateSocialClipInputs(rawInputs []string) ([]string, *models.JobErrorV1) {
	if len(rawInputs) == 0 {
		return nil, newRecipeError("RECIPE_INVALID_INPUT", "Choose one video for Social clip.", nil)
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
			"Social clip takes exactly one video. Remove the extra files.",
			nil,
		)
	}

	inputPath := inputs[0]
	if !supportsSocialClipExtension(inputPath) {
		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			fmt.Sprintf("Only .mp4, .mov, .mkv, .webm and .avi videos are supported: %s", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}

	return inputs, nil
}

func supportsSocialClipExtension(inputPath string) bool {
	switch strings.ToLower(filepath.Ext(inputPath)) {
	case ".mp4", ".mov", ".mkv", ".webm", ".avi":
		return true
	default:
		return false
	}
}

func socialClipPlanWarnings(metadata inspect.VideoMetadata) []string {
	warnings := make([]string, 0, 2)
	if metadata.DurationSeconds < socialClipPosterMinimumDuration {
		warnings = append(warnings, "Poster frame is taken from the middle of this short clip.")
	}
	if strings.TrimSpace(metadata.AudioCodec) == "" {
		warnings = append(warnings, "The source has no audio; the outputs will be video-only.")
	}

	return warnings
}

// socialClipPosterTime picks the poster timestamp: the first second for clips
// of at least 2 s, the midpoint for shorter clips.
func socialClipPosterTime(durationSeconds float64) float64 {
	if durationSeconds >= socialClipPosterMinimumDuration {
		return 1.0
	}
	if durationSeconds <= 0 {
		return 0
	}

	return durationSeconds / 2
}

func (s *Service) executeSocialClip(ctx context.Context, tracked *recipeJob, resolved *resolvedRecipe) {
	social := resolved.Social
	if social == nil || len(resolved.Recipe.Steps) != socialClipStepCount {
		s.failJob(tracked, newRecipeError("RECIPE_INVALID_DEFINITION", "The Social clip recipe definition is invalid.", nil))
		return
	}
	if ctx.Err() != nil {
		s.cancelJob(tracked)
		return
	}

	mp4Step := resolved.Recipe.Steps[socialMP4StepIndex]
	webmStep := resolved.Recipe.Steps[socialWebMStepIndex]
	posterStep := resolved.Recipe.Steps[socialPosterStepIndex]

	tracked.begin(1)
	s.emitProgress(tracked, models.JobStatusRunning, mp4Step.Name, "starting", fmt.Sprintf("Starting %s.", resolved.Recipe.Name), 5)

	// created tracks every path this run may have written. It is only used to
	// clean up on failure, so a pre-existing file is never removed.
	created := make([]string, 0, 4)

	mp4Path, jobErr := s.runSocialClipEncode(ctx, tracked, social, socialClipEncodeStage{
		Step:          mp4Step,
		StepIndex:     socialMP4StepIndex,
		Format:        "mp4",
		Path:          social.MP4Path,
		Label:         "MP4 · H.264/AAC",
		SuccessDetail: "MP4 encoded.",
		FromPercent:   10,
		ToPercent:     35,
	}, &created)
	if jobErr != nil {
		s.abortSocialClip(ctx, tracked, mp4Step, socialMP4StepIndex, created, jobErr)
		return
	}

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}
	tracked.setCurrentStep(socialWebMStepIndex + 1)

	webmPath, jobErr := s.runSocialClipEncode(ctx, tracked, social, socialClipEncodeStage{
		Step:          webmStep,
		StepIndex:     socialWebMStepIndex,
		Format:        "webm",
		Path:          social.WebMPath,
		Label:         "WebM · VP9/Opus",
		SuccessDetail: "WebM encoded.",
		FromPercent:   40,
		ToPercent:     70,
	}, &created)
	if jobErr != nil {
		s.abortSocialClip(ctx, tracked, webmStep, socialWebMStepIndex, created, jobErr)
		return
	}

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}
	tracked.setCurrentStep(socialPosterStepIndex + 1)

	posterPath, jobErr := s.runSocialClipPosterStep(ctx, tracked, social, posterStep, &created)
	if jobErr != nil {
		s.abortSocialClip(ctx, tracked, posterStep, socialPosterStepIndex, created, jobErr)
		return
	}

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}

	outputs := []string{mp4Path, webmPath, posterPath}
	report := verify.VerifyPaths(outputs)
	report = addSocialClipChecks(report, social, mp4Path, webmPath)

	status := models.JobStatusSuccess
	message := fmt.Sprintf("Social clip ready: %d verified files.", len(outputs))
	if !report.Success {
		status = models.JobStatusPartialSuccess
		message = fmt.Sprintf("Social clip ready, but verification reported: %s.", report.Message)
	}

	if s.history != nil {
		s.history.RecordOutputs(tracked.jobID(), socialClipHistoryToolID, outputs)
	}

	tracked.finish(status, message, outputs, &report, nil, true)
	s.emitProgress(tracked, status, posterStep.Name, status, message, 100)
}

// abortSocialClip removes the files this run created and reports the step
// failure or the cancellation, never both.
func (s *Service) abortSocialClip(ctx context.Context, tracked *recipeJob, step models.RecipeStepV1, stepIndex int, created []string, jobErr *models.JobErrorV1) {
	removeCreatedFiles(created)
	if isCancellation(ctx, jobErr) {
		s.cancelJob(tracked)
		return
	}

	tracked.setStep(stepIndex, failedRecipeStep(step, jobErr))
	s.failJob(tracked, jobErr)
}

// runSocialClipEncode converts the source into one planned video output and
// returns the path the tool actually wrote.
func (s *Service) runSocialClipEncode(ctx context.Context, tracked *recipeJob, social *socialClipResolution, stage socialClipEncodeStage, created *[]string) (string, *models.JobErrorV1) {
	if ctx.Err() != nil {
		return "", recipeCancelledError()
	}

	executor, planner, jobErr := s.singleExecutor(video.ToolIDVideoConvertV1)
	if jobErr != nil {
		return "", jobErr
	}

	// The name was free during planning; confirm it still is, then claim it for
	// cleanup before the tool can write a partial file.
	if _, statErr := os.Stat(stage.Path); statErr == nil {
		return "", newRecipeError(
			"RECIPE_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another output folder.", filepath.Base(stage.Path)),
			map[string]any{"outputPath": stage.Path},
		)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", newRecipeError(
			"RECIPE_STEP_FAILED",
			fmt.Sprintf("%s cannot be accessed.", filepath.Base(stage.Path)),
			map[string]any{"outputPath": stage.Path},
		)
	}
	*created = append(*created, stage.Path)

	s.emitProgress(tracked, models.JobStatusRunning, stage.Step.Name, "running", stage.Label, stage.FromPercent)

	req := socialClipEncodeRequest(social, stage)
	plan, planErr := planner.Plan(ctx, req)
	if planErr != nil {
		return "", planErr
	}

	item, execErr := executor.ExecuteSingle(ctx, req)
	if execErr != nil {
		return "", execErr
	}
	if !item.Success {
		return "", stepItemError(item, video.ToolIDVideoConvertV1)
	}

	outputPath := strings.TrimSpace(item.OutputPath)
	if outputPath == "" && len(plan.Outputs) > 0 {
		outputPath = strings.TrimSpace(plan.Outputs[0].OutputPath)
	}
	if outputPath == "" {
		return "", newRecipeError("RECIPE_STEP_FAILED", "The video step did not report an output.", nil)
	}
	if outputPath != stage.Path {
		*created = append(*created, outputPath)
	}

	tracked.setStep(stage.StepIndex, models.RecipeStepResultV1{
		ID:      stage.Step.ID,
		Name:    stage.Step.Name,
		ToolID:  stage.Step.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: stage.SuccessDetail,
		Outputs: []string{outputPath},
	})
	s.emitProgress(tracked, models.JobStatusRunning, stage.Step.Name, "running", stage.Label, stage.ToPercent)

	return outputPath, nil
}

func socialClipEncodeRequest(social *socialClipResolution, stage socialClipEncodeStage) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     video.ToolIDVideoConvertV1,
		Mode:       "single",
		InputPaths: []string{social.SourcePath},
		OutputDir:  social.OutputDir,
		Options: map[string]any{
			"targetFormat":  stage.Format,
			"qualityPreset": "high",
			"resolution":    "original",
			"outputPath":    stage.Path,
		},
	}
}

// runSocialClipPosterStep extracts the poster frame into a temp file in the
// output folder and only then renames it into place, so a half-written JPEG is
// never left at the final destination.
func (s *Service) runSocialClipPosterStep(ctx context.Context, tracked *recipeJob, social *socialClipResolution, step models.RecipeStepV1, created *[]string) (string, *models.JobErrorV1) {
	if ctx.Err() != nil {
		return "", recipeCancelledError()
	}

	tempFile, err := os.CreateTemp(social.OutputDir, socialClipPosterTempPattern)
	if err != nil {
		return "", newRecipeError("RECIPE_POSTER_FAILED", fmt.Sprintf("Could not create the poster frame: %v", err), nil)
	}
	tempPath := tempFile.Name()
	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(tempPath)
		return "", newRecipeError("RECIPE_POSTER_FAILED", fmt.Sprintf("Could not create the poster frame: %v", closeErr), nil)
	}
	*created = append(*created, tempPath)

	s.emitProgress(tracked, models.JobStatusRunning, step.Name, "running", "Poster · JPEG", 75)

	atSeconds := socialClipPosterTime(social.Metadata.DurationSeconds)
	if posterErr := socialClipPosterCommand(ctx, social.SourcePath, tempPath, atSeconds); posterErr != nil {
		_ = os.Remove(tempPath)
		return "", newRecipeError(
			"RECIPE_POSTER_FAILED",
			fmt.Sprintf("Could not extract the poster frame: %v", posterErr),
			map[string]any{"inputPath": social.SourcePath},
		)
	}

	if _, statErr := os.Stat(social.PosterPath); statErr == nil {
		_ = os.Remove(tempPath)
		return "", newRecipeError(
			"RECIPE_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another output folder.", filepath.Base(social.PosterPath)),
			map[string]any{"outputPath": social.PosterPath},
		)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = os.Remove(tempPath)
		return "", newRecipeError(
			"RECIPE_POSTER_FAILED",
			fmt.Sprintf("%s cannot be accessed.", filepath.Base(social.PosterPath)),
			map[string]any{"outputPath": social.PosterPath},
		)
	}

	if renameErr := os.Rename(tempPath, social.PosterPath); renameErr != nil {
		_ = os.Remove(tempPath)
		return "", newRecipeError("RECIPE_POSTER_FAILED", fmt.Sprintf("Could not save the poster frame: %v", renameErr), nil)
	}
	*created = append(*created, social.PosterPath)

	tracked.setStep(socialPosterStepIndex, models.RecipeStepResultV1{
		ID:      step.ID,
		Name:    step.Name,
		ToolID:  step.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: "Poster frame extracted.",
		Outputs: []string{social.PosterPath},
	})
	s.emitProgress(tracked, models.JobStatusRunning, step.Name, "running", "Poster · JPEG", 90)

	return social.PosterPath, nil
}

// socialClipCodecExpectation is the expected codec pair of one encoded video.
type socialClipCodecExpectation struct {
	Index      int
	Path       string
	VideoCodec string
	AudioCodec string
}

// addSocialClipChecks appends the recipe-specific codec and duration checks.
// Any mismatch marks the whole report as failed so the run never reports a
// silent success.
func addSocialClipChecks(report models.VerificationReportV1, social *socialClipResolution, mp4Path, webmPath string) models.VerificationReportV1 {
	expectations := []socialClipCodecExpectation{
		{Index: 0, Path: mp4Path, VideoCodec: "h264", AudioCodec: "aac"},
		{Index: 1, Path: webmPath, VideoCodec: "vp9", AudioCodec: "opus"},
	}

	sourceDuration := social.Metadata.DurationSeconds
	durationTolerance := math.Max(0.5, sourceDuration*0.02)
	hasAudio := strings.TrimSpace(social.Metadata.AudioCodec) != ""
	failed := false

	for _, expectation := range expectations {
		if expectation.Index >= len(report.Outputs) {
			continue
		}

		actual := inspect.InspectFile(expectation.Path)

		videoPassed := strings.EqualFold(actual.VideoCodec, expectation.VideoCodec)
		videoDetail := ""
		if !videoPassed {
			failed = true
			videoDetail = fmt.Sprintf("expected %s", expectation.VideoCodec)
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"%s: expected %s video, got %s",
				filepath.Base(expectation.Path),
				expectation.VideoCodec,
				codecLabel(actual.VideoCodec),
			))
		}
		report.Outputs[expectation.Index].Checks = append(report.Outputs[expectation.Index].Checks, models.VerificationCheckV1{
			Key:    socialClipExpectedVideoCodecCheck,
			Label:  "Expected video codec",
			Passed: videoPassed,
			Value:  codecLabel(actual.VideoCodec),
			Detail: videoDetail,
		})

		if hasAudio {
			audioPassed := strings.EqualFold(actual.AudioCodec, expectation.AudioCodec)
			audioDetail := ""
			if !audioPassed {
				failed = true
				audioDetail = fmt.Sprintf("expected %s", expectation.AudioCodec)
				report.Warnings = append(report.Warnings, fmt.Sprintf(
					"%s: expected %s audio, got %s",
					filepath.Base(expectation.Path),
					expectation.AudioCodec,
					codecLabel(actual.AudioCodec),
				))
			}
			report.Outputs[expectation.Index].Checks = append(report.Outputs[expectation.Index].Checks, models.VerificationCheckV1{
				Key:    socialClipExpectedAudioCodecCheck,
				Label:  "Expected audio codec",
				Passed: audioPassed,
				Value:  codecLabel(actual.AudioCodec),
				Detail: audioDetail,
			})
		}

		durationPassed := actual.DurationSeconds > 0 &&
			math.Abs(actual.DurationSeconds-sourceDuration) <= durationTolerance
		durationDetail := ""
		if !durationPassed {
			failed = true
			durationDetail = fmt.Sprintf("expected %.2f s ± %.2f s", sourceDuration, durationTolerance)
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"%s: expected %.2f s ± %.2f s of video, got %.2f s",
				filepath.Base(expectation.Path),
				sourceDuration,
				durationTolerance,
				actual.DurationSeconds,
			))
		}
		report.Outputs[expectation.Index].Checks = append(report.Outputs[expectation.Index].Checks, models.VerificationCheckV1{
			Key:    socialClipSourceDurationCheck,
			Label:  "Source duration",
			Passed: durationPassed,
			Value:  fmt.Sprintf("%.2f s", actual.DurationSeconds),
			Detail: durationDetail,
		})
	}

	if failed {
		report.Success = false
		report.Message = "verification failed: the encoded outputs do not match the source"
	}

	return report
}

func codecLabel(codec string) string {
	trimmed := strings.TrimSpace(codec)
	if trimmed == "" {
		return "none"
	}

	return trimmed
}
