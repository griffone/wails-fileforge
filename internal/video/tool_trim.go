package video

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/jobs"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

const (
	ToolIDVideoTrim = "tool.video.trim"

	videoTrimOutputSuffix = "_trimmed"
)

// probeVideoDuration is a package variable so tests can simulate source
// durations without invoking ffprobe.
var probeVideoDuration = inspect.ProbeVideoDuration

type TrimTool struct {
	probe  engine.RuntimeProbe
	runner engine.CommandRunner
}

func NewTrimTool() *TrimTool {
	return &TrimTool{}
}

func NewTrimToolWithDeps(probe engine.RuntimeProbe, runner engine.CommandRunner) *TrimTool {
	return &TrimTool{probe: probe, runner: runner}
}

func (t *TrimTool) ID() string {
	return ToolIDVideoTrim
}

func (t *TrimTool) Capability() string {
	return ToolIDVideoTrim
}

func (t *TrimTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "Video Trim",
		Description:      "Trim videos by start/end time to mp4 or webm",
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
		Tags:             []string{"video", "trim", "ffmpeg"},
	}
}

func (t *TrimTool) RuntimeState(ctx context.Context) models.ToolRuntimeState {
	if err := resolveRuntimeProbe(t.probe).Check(ctx); err != nil {
		return models.ToolRuntimeState{Status: "degraded", Healthy: false, Reason: err.Error()}
	}

	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *TrimTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves trim outputs without running ffmpeg. Explicit options.outputPath
// keeps the classic contract; otherwise outputs default to
// <stem>_trimmed.<format> and never overwrite an existing file.
func (t *TrimTool) Plan(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	if runtimeErr := checkVideoRuntime(ctx, t.probe); runtimeErr != nil {
		return planFailure(plan, runtimeErr)
	}

	mode := strings.TrimSpace(req.Mode)
	switch mode {
	case "single":
		if len(req.InputPaths) != 1 {
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoTrimValidation, Message: "exactly 1 input video is required"})
		}
	case "batch":
		if len(req.InputPaths) < 1 {
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoTrimValidation, Message: "at least 1 input video is required"})
		}
	default:
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoTrimValidation, Message: "mode must be single or batch"})
	}

	startTime, ok := optionNumber(req.Options, "startTime")
	if !ok {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.startTime is required"})
	}

	endTime, ok := optionNumber(req.Options, "endTime")
	if !ok {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.endTime is required"})
	}

	if startTime < 0 {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoTrimInvalidTimeRange, Message: "startTime must be >= 0"})
	}

	if endTime <= startTime {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoTrimInvalidTimeRange, Message: "endTime must be greater than startTime"})
	}

	trimMode := strings.ToLower(optionString(req.Options, "trimMode"))
	if trimMode == "" {
		trimMode = engine.TrimModeAuto
	}
	switch trimMode {
	case engine.TrimModeAuto, engine.TrimModeCopy, engine.TrimModeReencode:
	default:
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoTrimModeInvalid, Message: "trimMode must be auto, copy, or reencode"})
	}

	targetSizeBytes, targetErr := videoOptionTargetSize(req.Options, engine.ErrorCodeVideoTrimValidation)
	if targetErr != nil {
		return planFailure(plan, targetErr)
	}
	copyModeForcedToReencode := targetSizeBytes > 0 && trimMode != engine.TrimModeReencode
	if copyModeForcedToReencode {
		trimMode = engine.TrimModeReencode
	}

	targetFormat, qualityPreset, optionsErr := resolveVideoOutputOptions(req.Options, engine.ErrorCodeVideoTrimValidation)
	if optionsErr != nil {
		return planFailure(plan, optionsErr)
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
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoTrimValidation, Message: "inputPaths must not contain empty values"})
		}

		var planned models.PlannedOutput
		var planErr *models.JobError
		if explicitOutput != "" {
			planned, planErr = planExplicitTrimOutput(inputPath, explicitOutput, startTime, endTime, targetFormat, qualityPreset, trimMode, targetSizeBytes)
		} else {
			planned = planAutoVideoOutput(inputPath, outputDir, videoTrimOutputSuffix, targetFormat, used)
			planErr = mapVideoError(engine.ValidateTrimRequest(engine.TrimRequest{
				InputPath:       planned.InputPath,
				OutputPath:      planned.OutputPath,
				StartTime:       startTime,
				EndTime:         endTime,
				TargetFormat:    targetFormat,
				QualityPreset:   qualityPreset,
				TrimMode:        trimMode,
				TargetSizeBytes: targetSizeBytes,
			}))
		}

		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
		if planErr != nil {
			plan.Outputs = outputs
			return planFailure(plan, planErr)
		}

		if rangeErr := validateTrimRangeAgainstDuration(ctx, planned.InputPath, startTime); rangeErr != nil {
			plan.Outputs = outputs
			return planFailure(plan, rangeErr)
		}

		if targetSizeBytes > 0 {
			media, mediaOK := targetMediaForPath(ctx, planned.InputPath)
			spec := targetPlanSpec{
				targetBytes:    targetSizeBytes,
				resolution:     engine.ResolutionOriginal,
				forcedReencode: copyModeForcedToReencode,
			}
			if mediaOK {
				spec.durationKnown = true
				spec.durationSeconds = endTime - startTime
				if media.DurationSeconds > 0 && endTime > media.DurationSeconds {
					spec.durationSeconds = media.DurationSeconds - startTime
				}
				spec.sourceHeight = media.Height
				spec.audioKbps = engine.TargetAudioKbps(targetFormat, media.HasAudio)
			}
			warnings = append(warnings, targetPlanWarnings(spec)...)
		}
	}

	plan.Success = true
	plan.Message = videoTrimPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	effectiveOptions := map[string]any{
		"targetFormat":  targetFormat,
		"qualityPreset": qualityPreset,
		"trimMode":      trimMode,
		"startTime":     startTime,
		"endTime":       endTime,
	}
	if targetSizeBytes > 0 {
		effectiveOptions["targetSizeBytes"] = targetSizeBytes
	}
	plan.EffectiveOptions = effectiveOptions

	return plan, nil
}

func (t *TrimTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

func (t *TrimTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return videoPlanFailureItem(firstInputPath(req.InputPaths), firstPlannedOutputPath(plan), planErr), planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, onProgress)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = &models.JobError{Code: "EXECUTION_ERROR", Message: "video trim execution returned no result"}
	}

	return videoPlanFailureItem(firstInputPath(req.InputPaths), firstPlannedOutputPath(plan), jobErr), jobErr
}

func (t *TrimTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan trims exactly the outputs resolved by Plan. Single requests keep
// the fine-grained trim progress; batch requests report one processed item at a
// time. Trims stay sequential because the external class holds a single slot.
func (t *TrimTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	items := make([]models.JobResultItem, 0, len(plan.Outputs))
	reportItemProgress := strings.TrimSpace(req.Mode) == "single"
	reportBatchProgress := strings.TrimSpace(req.Mode) == "batch" && onProgress != nil
	var firstErr *models.JobError

	for index, planned := range plan.Outputs {
		select {
		case <-ctx.Done():
			cancelErr := &models.JobError{Code: "CANCELED", Message: ctx.Err().Error()}
			return items, cancelErr
		default:
		}

		trimReq := trimRequestFromPlannedOutput(planned, plan.EffectiveOptions)
		var fallbackInfo string
		var trimOutcome engine.TargetEncodeOutcome
		var trimErr *engine.VideoError
		if reportItemProgress {
			trimOutcome, trimErr = engine.TrimWithOutcome(ctx, t.probe, t.runner, trimReq, func(evt engine.TrimProgressEvent) {
				if strings.Contains(evt.Stage, "fallback") {
					fallbackInfo = evt.Message
				}
				if onProgress == nil {
					return
				}
				total := 100
				current := int(evt.Percent)
				if current < 0 {
					current = 0
				}
				if current > total {
					current = total
				}
				message := strings.TrimSpace(evt.Message)
				if message == "" {
					message = evt.Stage
				}
				stage := strings.TrimSpace(evt.Stage)
				if stage == "" {
					stage = jobs.StatusRunning
				}
				onProgress(models.JobProgress{Current: current, Total: total, Stage: stage, Message: message})
			})
		} else {
			trimOutcome, trimErr = engine.TrimWithOutcome(ctx, t.probe, t.runner, trimReq, nil)
		}

		if trimErr != nil {
			jobErr := mapVideoError(trimErr)
			if trimErr.Code == engine.ErrorCodeVideoTrimAutoFallbackFailed {
				jobErr.Details = map[string]any{"fallbackUsed": true, "fallbackStatus": "failed"}
			}
			if trimErr.Code == engine.ErrorCodeVideoTrimCopyFailed && trimReq.TrimMode == engine.TrimModeAuto {
				jobErr.Details = map[string]any{"fallbackUsed": false}
			}
			if firstErr == nil {
				firstErr = jobErr
			}
			items = append(items, videoPlanFailureItem(trimReq.InputPath, trimReq.OutputPath, jobErr))
		} else {
			message := "Video trim successful"
			if trimOutcome.TargetAttempted {
				message = targetResultMessage(trimOutcome, "Trimmed")
			}
			item := models.JobResultItem{InputPath: trimReq.InputPath, OutputPath: trimReq.OutputPath, Outputs: []string{trimReq.OutputPath}, OutputCount: 1, Success: true, Message: message}
			if trimReq.TrimMode == engine.TrimModeAuto && fallbackInfo != "" {
				item.Message = message + " (fallback re-encode used)"
			}
			applyTargetDetails(&item, trimOutcome)
			items = append(items, item)
			if reportItemProgress && onProgress != nil {
				onProgress(models.JobProgress{Current: 100, Total: 100, Stage: "trim.done", Message: item.Message})
			}
		}

		if reportBatchProgress {
			onProgress(models.JobProgress{Current: index + 1, Total: len(plan.Outputs), Stage: jobs.StatusRunning, Message: fmt.Sprintf("processed %d/%d", index+1, len(plan.Outputs))})
		}
	}

	if firstErr != nil {
		return items, firstErr
	}

	return items, nil
}

func planExplicitTrimOutput(inputPath, outputPath string, startTime, endTime float64, targetFormat, qualityPreset, trimMode string, targetSizeBytes int64) (models.PlannedOutput, *models.JobError) {
	planned := models.PlannedOutput{
		InputPath:  strings.TrimSpace(inputPath),
		OutputPath: filepath.Clean(strings.TrimSpace(outputPath)),
		OutputKind: videoOutputKindFile,
	}

	if _, err := os.Stat(planned.OutputPath); err == nil {
		planned.Exists = true
	}

	validationErr := engine.ValidateTrimRequest(engine.TrimRequest{
		InputPath:       planned.InputPath,
		OutputPath:      planned.OutputPath,
		StartTime:       startTime,
		EndTime:         endTime,
		TargetFormat:    targetFormat,
		QualityPreset:   qualityPreset,
		TrimMode:        trimMode,
		TargetSizeBytes: targetSizeBytes,
	})

	return planned, mapVideoError(validationErr)
}

func trimRequestFromPlannedOutput(planned models.PlannedOutput, effectiveOptions map[string]any) engine.TrimRequest {
	startTime, _ := effectiveOptions["startTime"].(float64)
	endTime, _ := effectiveOptions["endTime"].(float64)

	return engine.TrimRequest{
		InputPath:       planned.InputPath,
		OutputPath:      planned.OutputPath,
		StartTime:       startTime,
		EndTime:         endTime,
		TargetFormat:    optionString(effectiveOptions, "targetFormat"),
		QualityPreset:   optionString(effectiveOptions, "qualityPreset"),
		TrimMode:        optionString(effectiveOptions, "trimMode"),
		TargetSizeBytes: videoInt64Option(effectiveOptions, "targetSizeBytes"),
	}
}

// validateTrimRangeAgainstDuration rejects a range that starts at or after the
// end of the source. An endTime beyond the duration is intentionally allowed:
// ffmpeg stops at EOF, and the current trim form defaults to a fixed end time.
func validateTrimRangeAgainstDuration(ctx context.Context, inputPath string, startTime float64) *models.JobError {
	duration, ok := probeVideoDuration(ctx, inputPath)
	if !ok {
		return nil
	}

	if startTime >= duration {
		return &models.JobError{
			Code:       engine.ErrorCodeVideoTrimInvalidTimeRange,
			DetailCode: engine.ErrorCodeVideoTrimInvalidTimeRange,
			Message:    fmt.Sprintf("startTime must be before the video duration (%.1f s)", duration),
		}
	}

	return nil
}

func videoTrimPlanMessage(count int) string {
	if count == 1 {
		return "Ready to trim 1 video."
	}
	return fmt.Sprintf("Ready to trim %d videos.", count)
}

func optionNumber(options map[string]any, key string) (float64, bool) {
	if options == nil {
		return 0, false
	}

	switch value := options[key].(type) {
	case float64:
		return value, true
	case float32:
		return float64(value), true
	case int:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	default:
		return 0, false
	}
}
