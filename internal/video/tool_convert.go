package video

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

const (
	ToolIDVideoConvert = "tool.video.convert"

	videoConvertOutputSuffix = "_converted"
)

type ConvertTool struct {
	probe  engine.RuntimeProbe
	runner engine.CommandRunner
}

func NewConvertTool() *ConvertTool {
	return &ConvertTool{}
}

func NewConvertToolWithDeps(probe engine.RuntimeProbe, runner engine.CommandRunner) *ConvertTool {
	return &ConvertTool{probe: probe, runner: runner}
}

func (t *ConvertTool) ID() string {
	return ToolIDVideoConvert
}

func (t *ConvertTool) Capability() string {
	return ToolIDVideoConvert
}

func (t *ConvertTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "Video Convert",
		Description:      "Convert videos to mp4 or webm with quality presets",
		Domain:           "video",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"mp4", "mov", "mkv"},
		OutputExtensions: []string{"mp4", "webm"},
		RuntimeDeps:      []string{"ffmpeg", "ffprobe"},
		ResourceClass:    "external",
		MaxWorkers:       1,
		Tags:             []string{"video", "convert", "ffmpeg"},
	}
}

func (t *ConvertTool) RuntimeState(ctx context.Context) models.ToolRuntimeState {
	if err := resolveRuntimeProbe(t.probe).Check(ctx); err != nil {
		return models.ToolRuntimeState{Status: "degraded", Healthy: false, Reason: err.Error()}
	}

	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *ConvertTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves every output path without converting anything. Explicit
// options.outputPath keeps the classic contract; otherwise outputs default to
// <stem>_converted.<format> and never overwrite an existing file.
func (t *ConvertTool) Plan(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	if runtimeErr := checkVideoRuntime(ctx, t.probe); runtimeErr != nil {
		return planFailure(plan, runtimeErr)
	}

	mode := strings.TrimSpace(req.Mode)
	switch mode {
	case "single":
		if len(req.InputPaths) != 1 {
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoValidation, Message: "exactly 1 input video is required"})
		}
	case "batch":
		if len(req.InputPaths) < 1 {
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoValidation, Message: "at least 1 input video is required"})
		}
	default:
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoValidation, Message: "mode must be single or batch"})
	}

	targetFormat, qualityPreset, optionsErr := resolveVideoOutputOptions(req.Options, engine.ErrorCodeVideoValidation)
	if optionsErr != nil {
		return planFailure(plan, optionsErr)
	}

	videoOptions, videoOptionsErr := resolveConvertVideoOptions(req.Options, engine.ErrorCodeVideoValidation)
	if videoOptionsErr != nil {
		return planFailure(plan, videoOptionsErr)
	}

	outputDir := strings.TrimSpace(req.OutputDir)
	explicitOutput := ""
	if mode == "single" {
		explicitOutput = optionString(req.Options, "outputPath")
	}

	used := reserveInputPaths(req.InputPaths)
	outputs := make([]models.PlannedOutput, 0, len(req.InputPaths))
	warnings := make([]string, 0, len(req.InputPaths))

	for _, rawInputPath := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoValidation, Message: "inputPaths must not contain empty values"})
		}

		var planned models.PlannedOutput
		var planErr *models.JobError
		if explicitOutput != "" {
			planned, planErr = planExplicitConvertOutput(inputPath, explicitOutput, targetFormat, qualityPreset, videoOptions)
		} else {
			planned = planAutoVideoOutput(inputPath, outputDir, videoConvertOutputSuffix, targetFormat, used)
			planErr = mapVideoError(engine.ValidateConvertRequest(engine.ConvertRequest{
				InputPath:       planned.InputPath,
				OutputPath:      planned.OutputPath,
				TargetFormat:    targetFormat,
				QualityPreset:   qualityPreset,
				Resolution:      videoOptions.resolution,
				BitrateKbps:     videoOptions.bitrateKbps,
				RemoveAudio:     videoOptions.removeAudio,
				TargetSizeBytes: videoOptions.targetSizeBytes,
			}))
		}

		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
		if planErr != nil {
			plan.Outputs = outputs
			return planFailure(plan, planErr)
		}

		if videoOptions.targetSizeBytes > 0 {
			media, mediaOK := targetMediaForPath(ctx, planned.InputPath)
			spec := targetPlanSpec{
				targetBytes:      videoOptions.targetSizeBytes,
				resolution:       videoOptions.resolution,
				audioKbps:        engine.TargetAudioKbps(targetFormat, media.HasAudio && !videoOptions.removeAudio),
				overridesBitrate: videoOptions.bitrateKbps > 0,
			}
			if mediaOK {
				spec.durationKnown = true
				spec.durationSeconds = media.DurationSeconds
				spec.sourceHeight = media.Height
			}
			warnings = append(warnings, targetPlanWarnings(spec)...)
		}
	}

	plan.Success = true
	plan.Message = videoConvertPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	effectiveOptions := map[string]any{
		"targetFormat":  targetFormat,
		"qualityPreset": qualityPreset,
		"resolution":    videoOptions.resolution,
		"bitrateKbps":   videoOptions.bitrateKbps,
		"removeAudio":   videoOptions.removeAudio,
	}
	if videoOptions.targetSizeBytes > 0 {
		effectiveOptions["targetSizeBytes"] = videoOptions.targetSizeBytes
	}
	plan.EffectiveOptions = effectiveOptions

	return plan, nil
}

func (t *ConvertTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return videoPlanFailureItem(firstInputPath(req.InputPaths), firstPlannedOutputPath(plan), planErr), planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, nil)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = &models.JobError{Code: "EXECUTION_ERROR", Message: "video convert execution returned no result"}
	}

	return videoPlanFailureItem(firstInputPath(req.InputPaths), firstPlannedOutputPath(plan), jobErr), jobErr
}

func (t *ConvertTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan converts exactly the outputs resolved by Plan. Conversions stay
// sequential because ffmpeg already uses every core and the external class holds
// a single slot. A reduced retry plan only processes its own outputs, and the
// result keeps plan order with per-item failures isolated.
func (t *ConvertTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	items := make([]models.JobResultItem, 0, len(plan.Outputs))
	var firstErr *models.JobError
	total := len(plan.Outputs)

	for index, planned := range plan.Outputs {
		select {
		case <-ctx.Done():
			cancelErr := &models.JobError{Code: "CANCELED", Message: ctx.Err().Error()}
			return items, cancelErr
		default:
		}

		convertReq := convertRequestFromPlannedOutput(planned, plan.EffectiveOptions)
		var itemProgress func(percent float64, message string)
		if onProgress != nil {
			itemProgress = func(percent float64, _ string) {
				overall := overallBatchPercent(index, total, percent)
				onProgress(models.JobProgress{
					Current: overall,
					Total:   jobProgressTotal,
					Stage:   "convert.encode",
					Message: convertProgressMessage(index, total, overall),
				})
			}
		}

		outcome, err := engine.ConvertWithOutcome(ctx, t.probe, t.runner, convertReq, itemProgress)
		if err != nil {
			jobErr := mapVideoError(err)
			if firstErr == nil {
				firstErr = jobErr
			}
			items = append(items, videoPlanFailureItem(convertReq.InputPath, convertReq.OutputPath, jobErr))
		} else {
			item := models.JobResultItem{InputPath: convertReq.InputPath, OutputPath: convertReq.OutputPath, Outputs: []string{convertReq.OutputPath}, OutputCount: 1, Success: true, Message: "Video conversion successful"}
			if outcome.TargetAttempted {
				item.Message = targetResultMessage(outcome, "Encoded")
				applyTargetDetails(&item, outcome)
			}
			items = append(items, item)
		}
	}

	if firstErr != nil {
		return items, firstErr
	}

	return items, nil
}

// overallBatchPercent maps one item's local percentage onto the whole plan so
// the drawer shows a single continuous bar. A single-file plan is the identity.
func overallBatchPercent(index, total int, itemPercent float64) int {
	if total <= 0 {
		return 0
	}

	clamped := itemPercent
	if clamped < 0 {
		clamped = 0
	}
	if clamped > 100 {
		clamped = 100
	}

	return jobProgressPercent((float64(index)*100 + clamped) / float64(total))
}

func convertProgressMessage(index, total, percent int) string {
	if total <= 1 {
		return fmt.Sprintf("Converting video… %d%%", percent)
	}

	return fmt.Sprintf("Converting item %d of %d… %d%%", index+1, total, percent)
}

// convertVideoOptions carries the optional output controls shared by single
// and batch conversion plans.
type convertVideoOptions struct {
	resolution      string
	bitrateKbps     int
	removeAudio     bool
	targetSizeBytes int64
}

// resolveConvertVideoOptions parses the optional resolution/bitrate/audio
// controls, applying defaults for omitted values.
func resolveConvertVideoOptions(options map[string]any, validationCode string) (convertVideoOptions, *models.JobError) {
	resolution := strings.ToLower(optionString(options, "resolution"))
	switch resolution {
	case "":
		resolution = engine.ResolutionOriginal
	case engine.ResolutionOriginal, engine.Resolution1080p, engine.Resolution720p, engine.Resolution480p:
	default:
		return convertVideoOptions{}, &models.JobError{Code: validationCode, Message: "resolution must be original, 1080p, 720p, or 480p"}
	}

	bitrateKbps, bitrateErr := videoOptionInt(options, "bitrateKbps", validationCode)
	if bitrateErr != nil {
		return convertVideoOptions{}, bitrateErr
	}
	if bitrateKbps < 0 {
		return convertVideoOptions{}, &models.JobError{Code: validationCode, Message: "bitrateKbps must be >= 0"}
	}

	removeAudio, audioErr := videoOptionBool(options, "removeAudio", validationCode)
	if audioErr != nil {
		return convertVideoOptions{}, audioErr
	}

	targetSizeBytes, targetErr := videoOptionTargetSize(options, validationCode)
	if targetErr != nil {
		return convertVideoOptions{}, targetErr
	}

	return convertVideoOptions{
		resolution:      resolution,
		bitrateKbps:     bitrateKbps,
		removeAudio:     removeAudio,
		targetSizeBytes: targetSizeBytes,
	}, nil
}

func planExplicitConvertOutput(inputPath, outputPath, targetFormat, qualityPreset string, videoOptions convertVideoOptions) (models.PlannedOutput, *models.JobError) {
	planned := models.PlannedOutput{
		InputPath:  strings.TrimSpace(inputPath),
		OutputPath: filepath.Clean(strings.TrimSpace(outputPath)),
		OutputKind: videoOutputKindFile,
	}

	if _, err := os.Stat(planned.OutputPath); err == nil {
		planned.Exists = true
	}

	validationErr := engine.ValidateConvertRequest(engine.ConvertRequest{
		InputPath:       planned.InputPath,
		OutputPath:      planned.OutputPath,
		TargetFormat:    targetFormat,
		QualityPreset:   qualityPreset,
		Resolution:      videoOptions.resolution,
		BitrateKbps:     videoOptions.bitrateKbps,
		RemoveAudio:     videoOptions.removeAudio,
		TargetSizeBytes: videoOptions.targetSizeBytes,
	})

	return planned, mapVideoError(validationErr)
}

func convertRequestFromPlannedOutput(planned models.PlannedOutput, effectiveOptions map[string]any) engine.ConvertRequest {
	bitrateKbps, _ := effectiveOptions["bitrateKbps"].(int)
	removeAudio, _ := effectiveOptions["removeAudio"].(bool)
	targetSizeBytes := videoInt64Option(effectiveOptions, "targetSizeBytes")
	resolution := optionString(effectiveOptions, "resolution")
	if resolution == "" {
		resolution = engine.ResolutionOriginal
	}

	return engine.ConvertRequest{
		InputPath:       planned.InputPath,
		OutputPath:      planned.OutputPath,
		TargetFormat:    optionString(effectiveOptions, "targetFormat"),
		QualityPreset:   optionString(effectiveOptions, "qualityPreset"),
		Resolution:      resolution,
		BitrateKbps:     bitrateKbps,
		RemoveAudio:     removeAudio,
		TargetSizeBytes: targetSizeBytes,
	}
}

func videoConvertPlanMessage(count int) string {
	if count == 1 {
		return "Ready to convert 1 video."
	}
	return fmt.Sprintf("Ready to convert %d videos.", count)
}

func mapVideoError(err error) *models.JobError {
	if err == nil {
		return nil
	}

	var videoErr *engine.VideoError
	if !errors.As(err, &videoErr) {
		return &models.JobError{Code: "EXECUTION_ERROR", Message: err.Error()}
	}
	if videoErr == nil {
		// A typed nil *VideoError wrapped in an error interface is a success.
		return nil
	}

	if videoErr.Code == engine.ErrorCodeRuntimeFFmpegNotFound || videoErr.Code == engine.ErrorCodeRuntimeFFprobeNotFound {
		return &models.JobError{Code: engine.ErrorCodeRuntimeUnavailable, Message: "ffmpeg/ffprobe runtime is unavailable"}
	}

	return &models.JobError{Code: videoErr.Code, DetailCode: videoErr.Code, Message: videoErr.Message}
}

func optionString(options map[string]any, key string) string {
	if options == nil {
		return ""
	}

	v, ok := options[key].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(v)
}

func firstInputPath(inputPaths []string) string {
	if len(inputPaths) == 0 {
		return ""
	}

	return strings.TrimSpace(inputPaths[0])
}
