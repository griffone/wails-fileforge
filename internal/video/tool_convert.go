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
	ToolIDVideoConvertV1 = "tool.video.convert"

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
	return ToolIDVideoConvertV1
}

func (t *ConvertTool) Capability() string {
	return ToolIDVideoConvertV1
}

func (t *ConvertTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Video Convert",
		Description:      "Convert videos to mp4 or webm with quality presets",
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
		Tags:             []string{"video", "convert", "ffmpeg"},
	}
}

func (t *ConvertTool) RuntimeState(ctx context.Context) models.ToolRuntimeStateV1 {
	if err := resolveRuntimeProbe(t.probe).Check(ctx); err != nil {
		return models.ToolRuntimeStateV1{Status: "degraded", Healthy: false, Reason: err.Error()}
	}

	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *ConvertTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves every output path without converting anything. Explicit
// options.outputPath keeps the classic contract; otherwise outputs default to
// <stem>_converted.<format> and never overwrite an existing file.
func (t *ConvertTool) Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	if runtimeErr := checkVideoRuntime(ctx, t.probe); runtimeErr != nil {
		return planFailure(plan, runtimeErr)
	}

	mode := strings.TrimSpace(req.Mode)
	switch mode {
	case "single":
		if len(req.InputPaths) != 1 {
			return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "exactly 1 input video is required"})
		}
	case "batch":
		if len(req.InputPaths) < 1 {
			return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "at least 1 input video is required"})
		}
	default:
		return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "mode must be single or batch"})
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
	outputs := make([]models.PlannedOutputV1, 0, len(req.InputPaths))
	warnings := make([]string, 0, len(req.InputPaths))

	for _, rawInputPath := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "inputPaths must not contain empty values"})
		}

		var planned models.PlannedOutputV1
		var planErr *models.JobErrorV1
		if explicitOutput != "" {
			planned, planErr = planExplicitConvertOutput(inputPath, explicitOutput, targetFormat, qualityPreset, videoOptions)
		} else {
			planned = planAutoVideoOutput(inputPath, outputDir, videoConvertOutputSuffix, targetFormat, used)
			planErr = mapVideoError(engine.ValidateConvertRequest(engine.ConvertRequest{
				InputPath:     planned.InputPath,
				OutputPath:    planned.OutputPath,
				TargetFormat:  targetFormat,
				QualityPreset: qualityPreset,
				Resolution:    videoOptions.resolution,
				BitrateKbps:   videoOptions.bitrateKbps,
				RemoveAudio:   videoOptions.removeAudio,
			}))
		}

		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
		if planErr != nil {
			plan.Outputs = outputs
			return planFailure(plan, planErr)
		}
	}

	plan.Success = true
	plan.Message = videoConvertPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = map[string]any{
		"targetFormat":  targetFormat,
		"qualityPreset": qualityPreset,
		"resolution":    videoOptions.resolution,
		"bitrateKbps":   videoOptions.bitrateKbps,
		"removeAudio":   videoOptions.removeAudio,
	}

	return plan, nil
}

func (t *ConvertTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
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
		jobErr = &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: "video convert execution returned no result"}
	}

	return videoPlanFailureItem(firstInputPath(req.InputPaths), firstPlannedOutputPath(plan), jobErr), jobErr
}

func (t *ConvertTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
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
func (t *ConvertTool) ExecutePlan(ctx context.Context, req models.JobRequestV1, plan models.PlanJobResponseV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	items := make([]models.JobResultItemV1, 0, len(plan.Outputs))
	reportProgress := strings.TrimSpace(req.Mode) == "batch" && onProgress != nil
	var firstErr *models.JobErrorV1

	for index, planned := range plan.Outputs {
		select {
		case <-ctx.Done():
			cancelErr := &models.JobErrorV1{Code: "CANCELED", Message: ctx.Err().Error()}
			return items, cancelErr
		default:
		}

		convertReq := convertRequestFromPlannedOutput(planned, plan.EffectiveOptions)
		if err := engine.Convert(ctx, t.probe, t.runner, convertReq); err != nil {
			jobErr := mapVideoError(err)
			if firstErr == nil {
				firstErr = jobErr
			}
			items = append(items, videoPlanFailureItem(convertReq.InputPath, convertReq.OutputPath, jobErr))
		} else {
			items = append(items, models.JobResultItemV1{InputPath: convertReq.InputPath, OutputPath: convertReq.OutputPath, Outputs: []string{convertReq.OutputPath}, OutputCount: 1, Success: true, Message: "Video conversion successful"})
		}

		if reportProgress {
			onProgress(models.JobProgressV1{Current: index + 1, Total: len(plan.Outputs), Stage: "running", Message: fmt.Sprintf("processed %d/%d", index+1, len(plan.Outputs))})
		}
	}

	if firstErr != nil {
		return items, firstErr
	}

	return items, nil
}

// convertVideoOptions carries the optional output controls shared by single
// and batch conversion plans.
type convertVideoOptions struct {
	resolution  string
	bitrateKbps int
	removeAudio bool
}

// resolveConvertVideoOptions parses the optional resolution/bitrate/audio
// controls, applying defaults for omitted values.
func resolveConvertVideoOptions(options map[string]any, validationCode string) (convertVideoOptions, *models.JobErrorV1) {
	resolution := strings.ToLower(optionString(options, "resolution"))
	switch resolution {
	case "":
		resolution = engine.ResolutionOriginal
	case engine.ResolutionOriginal, engine.Resolution1080p, engine.Resolution720p, engine.Resolution480p:
	default:
		return convertVideoOptions{}, &models.JobErrorV1{Code: validationCode, Message: "resolution must be original, 1080p, 720p, or 480p"}
	}

	bitrateKbps, bitrateErr := videoOptionInt(options, "bitrateKbps", validationCode)
	if bitrateErr != nil {
		return convertVideoOptions{}, bitrateErr
	}
	if bitrateKbps < 0 {
		return convertVideoOptions{}, &models.JobErrorV1{Code: validationCode, Message: "bitrateKbps must be >= 0"}
	}

	removeAudio, audioErr := videoOptionBool(options, "removeAudio", validationCode)
	if audioErr != nil {
		return convertVideoOptions{}, audioErr
	}

	return convertVideoOptions{resolution: resolution, bitrateKbps: bitrateKbps, removeAudio: removeAudio}, nil
}

func planExplicitConvertOutput(inputPath, outputPath, targetFormat, qualityPreset string, videoOptions convertVideoOptions) (models.PlannedOutputV1, *models.JobErrorV1) {
	planned := models.PlannedOutputV1{
		InputPath:  strings.TrimSpace(inputPath),
		OutputPath: filepath.Clean(strings.TrimSpace(outputPath)),
		OutputKind: videoOutputKindFile,
	}

	if _, err := os.Stat(planned.OutputPath); err == nil {
		planned.Exists = true
	}

	validationErr := engine.ValidateConvertRequest(engine.ConvertRequest{
		InputPath:     planned.InputPath,
		OutputPath:    planned.OutputPath,
		TargetFormat:  targetFormat,
		QualityPreset: qualityPreset,
		Resolution:    videoOptions.resolution,
		BitrateKbps:   videoOptions.bitrateKbps,
		RemoveAudio:   videoOptions.removeAudio,
	})

	return planned, mapVideoError(validationErr)
}

func convertRequestFromPlannedOutput(planned models.PlannedOutputV1, effectiveOptions map[string]any) engine.ConvertRequest {
	bitrateKbps, _ := effectiveOptions["bitrateKbps"].(int)
	removeAudio, _ := effectiveOptions["removeAudio"].(bool)
	resolution := optionString(effectiveOptions, "resolution")
	if resolution == "" {
		resolution = engine.ResolutionOriginal
	}

	return engine.ConvertRequest{
		InputPath:     planned.InputPath,
		OutputPath:    planned.OutputPath,
		TargetFormat:  optionString(effectiveOptions, "targetFormat"),
		QualityPreset: optionString(effectiveOptions, "qualityPreset"),
		Resolution:    resolution,
		BitrateKbps:   bitrateKbps,
		RemoveAudio:   removeAudio,
	}
}

func videoConvertPlanMessage(count int) string {
	if count == 1 {
		return "Ready to convert 1 video."
	}
	return fmt.Sprintf("Ready to convert %d videos.", count)
}

func mapVideoError(err error) *models.JobErrorV1 {
	if err == nil {
		return nil
	}

	var videoErr *engine.VideoError
	if !errors.As(err, &videoErr) {
		return &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}
	if videoErr == nil {
		// A typed nil *VideoError wrapped in an error interface is a success.
		return nil
	}

	if videoErr.Code == engine.ErrorCodeRuntimeFFmpegNotFound || videoErr.Code == engine.ErrorCodeRuntimeFFprobeNotFound {
		return &models.JobErrorV1{Code: engine.ErrorCodeRuntimeUnavailable, Message: "ffmpeg/ffprobe runtime is unavailable"}
	}

	return &models.JobErrorV1{Code: videoErr.Code, DetailCode: videoErr.Code, Message: videoErr.Message}
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
