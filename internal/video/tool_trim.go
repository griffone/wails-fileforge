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
	ToolIDVideoTrimV1 = "tool.video.trim"

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
	return ToolIDVideoTrimV1
}

func (t *TrimTool) Capability() string {
	return ToolIDVideoTrimV1
}

func (t *TrimTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Video Trim",
		Description:      "Trim videos by start/end time to mp4 or webm",
		Domain:           "video",
		Capability:       t.Capability(),
		Version:          "v1",
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

func (t *TrimTool) RuntimeState(ctx context.Context) models.ToolRuntimeStateV1 {
	if err := resolveRuntimeProbe(t.probe).Check(ctx); err != nil {
		return models.ToolRuntimeStateV1{Status: "degraded", Healthy: false, Reason: err.Error()}
	}

	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *TrimTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves trim outputs without running ffmpeg. Explicit options.outputPath
// keeps the classic contract; otherwise outputs default to
// <stem>_trimmed.<format> and never overwrite an existing file.
func (t *TrimTool) Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	if runtimeErr := checkVideoRuntime(ctx, t.probe); runtimeErr != nil {
		return planFailure(plan, runtimeErr)
	}

	mode := strings.TrimSpace(req.Mode)
	switch mode {
	case "single":
		if len(req.InputPaths) != 1 {
			return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "exactly 1 input video is required"})
		}
	case "batch":
		if len(req.InputPaths) < 1 {
			return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "at least 1 input video is required"})
		}
	default:
		return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "mode must be single or batch"})
	}

	startTime, ok := optionNumber(req.Options, "startTime")
	if !ok {
		return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.startTime is required"})
	}

	endTime, ok := optionNumber(req.Options, "endTime")
	if !ok {
		return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.endTime is required"})
	}

	if startTime < 0 {
		return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimInvalidTimeRange, Message: "startTime must be >= 0"})
	}

	if endTime <= startTime {
		return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimInvalidTimeRange, Message: "endTime must be greater than startTime"})
	}

	trimMode := strings.ToLower(optionString(req.Options, "trimMode"))
	if trimMode == "" {
		trimMode = engine.TrimModeAuto
	}
	switch trimMode {
	case engine.TrimModeAuto, engine.TrimModeCopy, engine.TrimModeReencode:
	default:
		return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimModeInvalid, Message: "trimMode must be auto, copy, or reencode"})
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
	outputs := make([]models.PlannedOutputV1, 0, len(req.InputPaths))
	warnings := make([]string, 0, len(req.InputPaths))

	for _, rawInputPath := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "inputPaths must not contain empty values"})
		}

		var planned models.PlannedOutputV1
		var planErr *models.JobErrorV1
		if explicitOutput != "" {
			planned, planErr = planExplicitTrimOutput(inputPath, explicitOutput, startTime, endTime, targetFormat, qualityPreset, trimMode)
		} else {
			planned = planAutoVideoOutput(inputPath, outputDir, videoTrimOutputSuffix, targetFormat, used)
			planErr = mapVideoError(engine.ValidateTrimRequest(engine.TrimRequest{
				InputPath:     planned.InputPath,
				OutputPath:    planned.OutputPath,
				StartTime:     startTime,
				EndTime:       endTime,
				TargetFormat:  targetFormat,
				QualityPreset: qualityPreset,
				TrimMode:      trimMode,
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
	}

	plan.Success = true
	plan.Message = videoTrimPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = map[string]any{
		"targetFormat":  targetFormat,
		"qualityPreset": qualityPreset,
		"trimMode":      trimMode,
		"startTime":     startTime,
		"endTime":       endTime,
	}

	return plan, nil
}

func (t *TrimTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

func (t *TrimTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
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
		jobErr = &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: "video trim execution returned no result"}
	}

	return videoPlanFailureItem(firstInputPath(req.InputPaths), firstPlannedOutputPath(plan), jobErr), jobErr
}

func (t *TrimTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan trims exactly the outputs resolved by Plan. Single requests keep
// the fine-grained trim progress; batch requests report one processed item at a
// time. Trims stay sequential because the external class holds a single slot.
func (t *TrimTool) ExecutePlan(ctx context.Context, req models.JobRequestV1, plan models.PlanJobResponseV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	items := make([]models.JobResultItemV1, 0, len(plan.Outputs))
	reportItemProgress := strings.TrimSpace(req.Mode) == "single"
	reportBatchProgress := strings.TrimSpace(req.Mode) == "batch" && onProgress != nil
	var firstErr *models.JobErrorV1

	for index, planned := range plan.Outputs {
		select {
		case <-ctx.Done():
			cancelErr := &models.JobErrorV1{Code: "CANCELED", Message: ctx.Err().Error()}
			return items, cancelErr
		default:
		}

		trimReq := trimRequestFromPlannedOutput(planned, plan.EffectiveOptions)
		var fallbackInfo string
		var trimErr *engine.VideoError
		if reportItemProgress {
			trimErr = engine.TrimWithProgress(ctx, t.probe, t.runner, trimReq, func(evt engine.TrimProgressEvent) {
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
				onProgress(models.JobProgressV1{Current: current, Total: total, Stage: stage, Message: message})
			})
		} else {
			trimErr = engine.Trim(ctx, t.probe, t.runner, trimReq)
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
			item := models.JobResultItemV1{InputPath: trimReq.InputPath, OutputPath: trimReq.OutputPath, Outputs: []string{trimReq.OutputPath}, OutputCount: 1, Success: true, Message: message}
			if trimReq.TrimMode == engine.TrimModeAuto && fallbackInfo != "" {
				item.Message = message + " (fallback re-encode used)"
			}
			items = append(items, item)
			if reportItemProgress && onProgress != nil {
				onProgress(models.JobProgressV1{Current: 100, Total: 100, Stage: "trim.done", Message: item.Message})
			}
		}

		if reportBatchProgress {
			onProgress(models.JobProgressV1{Current: index + 1, Total: len(plan.Outputs), Stage: jobs.StatusRunning, Message: fmt.Sprintf("processed %d/%d", index+1, len(plan.Outputs))})
		}
	}

	if firstErr != nil {
		return items, firstErr
	}

	return items, nil
}

func planExplicitTrimOutput(inputPath, outputPath string, startTime, endTime float64, targetFormat, qualityPreset, trimMode string) (models.PlannedOutputV1, *models.JobErrorV1) {
	planned := models.PlannedOutputV1{
		InputPath:  strings.TrimSpace(inputPath),
		OutputPath: filepath.Clean(strings.TrimSpace(outputPath)),
		OutputKind: videoOutputKindFile,
	}

	if _, err := os.Stat(planned.OutputPath); err == nil {
		planned.Exists = true
	}

	validationErr := engine.ValidateTrimRequest(engine.TrimRequest{
		InputPath:     planned.InputPath,
		OutputPath:    planned.OutputPath,
		StartTime:     startTime,
		EndTime:       endTime,
		TargetFormat:  targetFormat,
		QualityPreset: qualityPreset,
		TrimMode:      trimMode,
	})

	return planned, mapVideoError(validationErr)
}

func trimRequestFromPlannedOutput(planned models.PlannedOutputV1, effectiveOptions map[string]any) engine.TrimRequest {
	startTime, _ := effectiveOptions["startTime"].(float64)
	endTime, _ := effectiveOptions["endTime"].(float64)

	return engine.TrimRequest{
		InputPath:     planned.InputPath,
		OutputPath:    planned.OutputPath,
		StartTime:     startTime,
		EndTime:       endTime,
		TargetFormat:  optionString(effectiveOptions, "targetFormat"),
		QualityPreset: optionString(effectiveOptions, "qualityPreset"),
		TrimMode:      optionString(effectiveOptions, "trimMode"),
	}
}

// validateTrimRangeAgainstDuration rejects a range that starts at or after the
// end of the source. An endTime beyond the duration is intentionally allowed:
// ffmpeg stops at EOF, and the current trim form defaults to a fixed end time.
func validateTrimRangeAgainstDuration(ctx context.Context, inputPath string, startTime float64) *models.JobErrorV1 {
	duration, ok := probeVideoDuration(ctx, inputPath)
	if !ok {
		return nil
	}

	if startTime >= duration {
		return &models.JobErrorV1{
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
