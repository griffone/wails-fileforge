package video

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

const (
	ToolIDVideoMergeV1 = "tool.video.merge"

	videoMergeOutputSuffix = "_merged"
)

type MergeTool struct {
	probe  engine.RuntimeProbe
	runner engine.CommandRunner
}

func NewMergeTool() *MergeTool {
	return &MergeTool{}
}

func NewMergeToolWithDeps(probe engine.RuntimeProbe, runner engine.CommandRunner) *MergeTool {
	return &MergeTool{probe: probe, runner: runner}
}

func (t *MergeTool) ID() string {
	return ToolIDVideoMergeV1
}

func (t *MergeTool) Capability() string {
	return ToolIDVideoMergeV1
}

func (t *MergeTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Video Merge",
		Description:      "Merge multiple videos in input order into one mp4 or webm output",
		Domain:           "video",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    false,
		InputExtensions:  []string{"mp4", "mov", "mkv", "webm"},
		OutputExtensions: []string{"mp4", "webm"},
		RuntimeDeps:      []string{"ffmpeg", "ffprobe"},
		Tags:             []string{"video", "merge", "concat", "ffmpeg"},
	}
}

func (t *MergeTool) RuntimeState(ctx context.Context) models.ToolRuntimeStateV1 {
	if err := resolveRuntimeProbe(t.probe).Check(ctx); err != nil {
		return models.ToolRuntimeStateV1{Status: "degraded", Healthy: false, Reason: err.Error()}
	}

	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *MergeTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves the merge destination without writing anything. An explicit
// outputPath keeps the classic contract (it must not exist); otherwise the
// output defaults to <firstStem>_merged.<format> next to the first input.
func (t *MergeTool) Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	if runtimeErr := checkVideoRuntime(ctx, t.probe); runtimeErr != nil {
		return planFailure(plan, runtimeErr)
	}

	if strings.TrimSpace(req.Mode) != "single" {
		return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoMergeValidation, Message: "mode must be single"})
	}

	if len(req.InputPaths) < 2 {
		return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoMergeInsufficientInputs, Message: "at least 2 input videos are required"})
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawInputPath := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoMergeValidation, Message: "inputPaths must not contain empty values"})
		}
		inputPaths = append(inputPaths, inputPath)
	}

	targetFormat, qualityPreset, optionsErr := resolveVideoOutputOptions(req.Options, engine.ErrorCodeVideoMergeValidation)
	if optionsErr != nil {
		return planFailure(plan, optionsErr)
	}

	mergeMode := strings.ToLower(optionString(req.Options, "mergeMode"))
	if mergeMode == "" {
		mergeMode = engine.MergeModeAuto
	}
	switch mergeMode {
	case engine.MergeModeAuto, engine.MergeModeCopy, engine.MergeModeReencode:
	default:
		return planFailure(plan, &models.JobErrorV1{Code: engine.ErrorCodeVideoMergeModeInvalid, Message: "mergeMode must be auto, copy, or reencode"})
	}

	removeAudio, audioErr := videoOptionBool(req.Options, "removeAudio", engine.ErrorCodeVideoMergeValidation)
	if audioErr != nil {
		return planFailure(plan, audioErr)
	}

	explicitOutput := optionString(req.Options, "outputPath")
	var planned models.PlannedOutputV1
	var planErr *models.JobErrorV1
	if explicitOutput != "" {
		planned, planErr = planExplicitMergeOutput(inputPaths, explicitOutput, targetFormat, qualityPreset, mergeMode, removeAudio)
	} else {
		planned = planAutoVideoOutput(inputPaths[0], req.OutputDir, videoMergeOutputSuffix, targetFormat, reserveInputPaths(inputPaths))
		planErr = mapVideoError(engine.ValidateMergeRequest(engine.MergeRequest{
			InputPaths:    inputPaths,
			OutputPath:    planned.OutputPath,
			TargetFormat:  targetFormat,
			QualityPreset: qualityPreset,
			MergeMode:     mergeMode,
			RemoveAudio:   removeAudio,
		}))
	}

	if planErr != nil {
		if planned.OutputPath != "" {
			plan.Outputs = []models.PlannedOutputV1{planned}
		}
		return planFailure(plan, planErr)
	}

	plan.Success = true
	plan.Message = videoMergePlanMessage(len(inputPaths))
	plan.Outputs = []models.PlannedOutputV1{planned}
	plan.Warnings = append(plan.Warnings, planned.Warnings...)
	plan.EffectiveOptions = map[string]any{
		"targetFormat":  targetFormat,
		"qualityPreset": qualityPreset,
		"mergeMode":     mergeMode,
		"removeAudio":   removeAudio,
	}

	return plan, nil
}

func (t *MergeTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	inputPath := strings.Join(trimInputPaths(req.InputPaths), ",")

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return videoPlanFailureItem(inputPath, firstPlannedOutputPath(plan), planErr), planErr
	}

	mergeReq := mergeRequestFromPlan(plan, req.InputPaths)
	if err := engine.Merge(ctx, t.probe, t.runner, mergeReq); err != nil {
		jobErr := mapVideoError(err)
		return videoPlanFailureItem(firstInputPath(mergeReq.InputPaths), mergeReq.OutputPath, jobErr), jobErr
	}

	return models.JobResultItemV1{InputPath: firstInputPath(mergeReq.InputPaths), OutputPath: mergeReq.OutputPath, Outputs: []string{mergeReq.OutputPath}, OutputCount: 1, Success: true, Message: "Video merge successful"}, nil
}

func planExplicitMergeOutput(inputPaths []string, outputPath, targetFormat, qualityPreset, mergeMode string, removeAudio bool) (models.PlannedOutputV1, *models.JobErrorV1) {
	planned := models.PlannedOutputV1{
		InputPath:  strings.Join(inputPaths, ","),
		OutputPath: filepath.Clean(strings.TrimSpace(outputPath)),
		OutputKind: videoOutputKindFile,
	}

	if _, err := os.Stat(planned.OutputPath); err == nil {
		planned.Exists = true
	}

	validationErr := engine.ValidateMergeRequest(engine.MergeRequest{
		InputPaths:    inputPaths,
		OutputPath:    planned.OutputPath,
		TargetFormat:  targetFormat,
		QualityPreset: qualityPreset,
		MergeMode:     mergeMode,
		RemoveAudio:   removeAudio,
	})

	return planned, mapVideoError(validationErr)
}

func mergeRequestFromPlan(plan models.PlanJobResponseV1, inputPaths []string) engine.MergeRequest {
	planned := plan.Outputs[0]
	mergeMode := optionString(plan.EffectiveOptions, "mergeMode")
	if mergeMode == "" {
		mergeMode = engine.MergeModeAuto
	}
	removeAudio, _ := plan.EffectiveOptions["removeAudio"].(bool)

	return engine.MergeRequest{
		InputPaths:    trimInputPaths(inputPaths),
		OutputPath:    planned.OutputPath,
		TargetFormat:  optionString(plan.EffectiveOptions, "targetFormat"),
		QualityPreset: optionString(plan.EffectiveOptions, "qualityPreset"),
		MergeMode:     mergeMode,
		RemoveAudio:   removeAudio,
	}
}

func trimInputPaths(inputPaths []string) []string {
	trimmed := make([]string, 0, len(inputPaths))
	for _, inputPath := range inputPaths {
		trimmed = append(trimmed, strings.TrimSpace(inputPath))
	}
	return trimmed
}

func videoMergePlanMessage(count int) string {
	return fmt.Sprintf("Ready to merge %d videos.", count)
}
