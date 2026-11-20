package video

import (
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

const (
	ToolIDVideoMerge = "tool.video.merge"

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
	return ToolIDVideoMerge
}

func (t *MergeTool) Capability() string {
	return ToolIDVideoMerge
}

func (t *MergeTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "Video Merge",
		Description:      "Merge multiple videos in input order into one mp4 or webm output",
		Domain:           "video",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    false,
		InputExtensions:  []string{"mp4", "mov", "mkv", "webm"},
		OutputExtensions: []string{"mp4", "webm"},
		RuntimeDeps:      []string{"ffmpeg", "ffprobe"},
		ResourceClass:    "external",
		MaxWorkers:       1,
		Tags:             []string{"video", "merge", "concat", "ffmpeg"},
	}
}

func (t *MergeTool) RuntimeState(ctx context.Context) models.ToolRuntimeState {
	if err := resolveRuntimeProbe(t.probe).Check(ctx); err != nil {
		return models.ToolRuntimeState{Status: "degraded", Healthy: false, Reason: err.Error()}
	}

	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *MergeTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves the merge destination without writing anything. An explicit
// outputPath keeps the classic contract (it must not exist); otherwise the
// output defaults to <firstStem>_merged.<format> next to the first input.
func (t *MergeTool) Plan(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	if runtimeErr := checkVideoRuntime(ctx, t.probe); runtimeErr != nil {
		return planFailure(plan, runtimeErr)
	}

	if strings.TrimSpace(req.Mode) != "single" {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoMergeValidation, Message: "mode must be single"})
	}

	if len(req.InputPaths) < 2 {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoMergeInsufficientInputs, Message: "at least 2 input videos are required"})
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawInputPath := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoMergeValidation, Message: "inputPaths must not contain empty values"})
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
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeVideoMergeModeInvalid, Message: "mergeMode must be auto, copy, or reencode"})
	}

	removeAudio, audioErr := videoOptionBool(req.Options, "removeAudio", engine.ErrorCodeVideoMergeValidation)
	if audioErr != nil {
		return planFailure(plan, audioErr)
	}

	transition, transitionDuration, transitionErr := resolveMergeTransitionOptions(req.Options)
	if transitionErr != nil {
		return planFailure(plan, transitionErr)
	}

	mergeRequest := engine.MergeRequest{
		InputPaths:         inputPaths,
		TargetFormat:       targetFormat,
		QualityPreset:      qualityPreset,
		MergeMode:          mergeMode,
		RemoveAudio:        removeAudio,
		Transition:         transition,
		TransitionDuration: transitionDuration,
	}

	explicitOutput := optionString(req.Options, "outputPath")
	var planned models.PlannedOutput
	var planErr *models.JobError
	if explicitOutput != "" {
		planned, planErr = planExplicitMergeOutput(explicitOutput, mergeRequest)
	} else {
		planned = planAutoVideoOutput(inputPaths[0], req.OutputDir, videoMergeOutputSuffix, targetFormat, reserveInputPaths(inputPaths))
		mergeRequest.OutputPath = planned.OutputPath
		planErr = mapVideoError(engine.ValidateMergeRequest(mergeRequest))
	}

	if planErr != nil {
		if planned.OutputPath != "" {
			plan.Outputs = []models.PlannedOutput{planned}
		}
		return planFailure(plan, planErr)
	}

	plan.Success = true
	plan.Message = videoMergePlanMessage(len(inputPaths))
	plan.Outputs = []models.PlannedOutput{planned}
	plan.Warnings = append(plan.Warnings, planned.Warnings...)
	plan.EffectiveOptions = map[string]any{
		"targetFormat":       targetFormat,
		"qualityPreset":      qualityPreset,
		"mergeMode":          mergeMode,
		"removeAudio":        removeAudio,
		"transition":         transition,
		"transitionDuration": transitionDuration,
	}

	return plan, nil
}

func (t *MergeTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	inputPath := strings.Join(trimInputPaths(req.InputPaths), ",")

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return videoPlanFailureItem(inputPath, firstPlannedOutputPath(plan), planErr), planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, nil)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = &models.JobError{Code: "EXECUTION_ERROR", Message: "video merge execution returned no result"}
	}

	return videoPlanFailureItem(inputPath, firstPlannedOutputPath(plan), jobErr), jobErr
}

// ExecutePlan merges into exactly the output resolved by Plan. The request only
// supplies the already validated input order; options and the destination are
// read from the plan.
func (t *MergeTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, _ func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	if len(plan.Outputs) == 0 {
		jobErr := &models.JobError{Code: "EXECUTION_ERROR", Message: "video merge execution returned no planned output"}
		return nil, jobErr
	}

	mergeReq := mergeRequestFromPlan(plan, req.InputPaths)
	if err := engine.Merge(ctx, t.probe, t.runner, mergeReq); err != nil {
		jobErr := mapVideoError(err)
		return []models.JobResultItem{videoPlanFailureItem(firstInputPath(mergeReq.InputPaths), mergeReq.OutputPath, jobErr)}, jobErr
	}

	return []models.JobResultItem{{InputPath: firstInputPath(mergeReq.InputPaths), OutputPath: mergeReq.OutputPath, Outputs: []string{mergeReq.OutputPath}, OutputCount: 1, Success: true, Message: "Video merge successful"}}, nil
}

func planExplicitMergeOutput(outputPath string, request engine.MergeRequest) (models.PlannedOutput, *models.JobError) {
	planned := models.PlannedOutput{
		InputPath:  strings.Join(request.InputPaths, ","),
		OutputPath: filepath.Clean(strings.TrimSpace(outputPath)),
		OutputKind: videoOutputKindFile,
	}

	if _, err := os.Stat(planned.OutputPath); err == nil {
		planned.Exists = true
	}

	request.OutputPath = planned.OutputPath
	validationErr := engine.ValidateMergeRequest(request)

	return planned, mapVideoError(validationErr)
}

func mergeRequestFromPlan(plan models.PlanJobResponse, inputPaths []string) engine.MergeRequest {
	planned := plan.Outputs[0]
	mergeMode := optionString(plan.EffectiveOptions, "mergeMode")
	if mergeMode == "" {
		mergeMode = engine.MergeModeAuto
	}
	removeAudio, _ := plan.EffectiveOptions["removeAudio"].(bool)
	transition := optionString(plan.EffectiveOptions, "transition")
	if transition == "" {
		transition = engine.MergeTransitionNone
	}
	transitionDuration, _ := plan.EffectiveOptions["transitionDuration"].(float64)

	return engine.MergeRequest{
		InputPaths:         trimInputPaths(inputPaths),
		OutputPath:         planned.OutputPath,
		TargetFormat:       optionString(plan.EffectiveOptions, "targetFormat"),
		QualityPreset:      optionString(plan.EffectiveOptions, "qualityPreset"),
		MergeMode:          mergeMode,
		RemoveAudio:        removeAudio,
		Transition:         transition,
		TransitionDuration: transitionDuration,
	}
}

func trimInputPaths(inputPaths []string) []string {
	trimmed := make([]string, 0, len(inputPaths))
	for _, inputPath := range inputPaths {
		trimmed = append(trimmed, strings.TrimSpace(inputPath))
	}
	return trimmed
}

// resolveMergeTransitionOptions normalizes the transition options and rejects
// invalid values before any output path is planned.
func resolveMergeTransitionOptions(options map[string]any) (string, float64, *models.JobError) {
	transition := strings.ToLower(optionString(options, "transition"))
	if transition == "" {
		transition = engine.MergeTransitionNone
	}
	switch transition {
	case engine.MergeTransitionNone:
	case engine.MergeTransitionCrossfade:
	default:
		return "", 0, &models.JobError{Code: engine.ErrorCodeVideoMergeValidation, Message: "options.transition must be none or crossfade"}
	}

	duration := engine.MergeTransitionDurationDefault
	supplied, isSupplied, durationErr := videoOptionFloat(options, "transitionDuration", engine.ErrorCodeVideoMergeValidation)
	if durationErr != nil {
		return "", 0, durationErr
	}
	if isSupplied {
		if math.IsNaN(supplied) || math.IsInf(supplied, 0) {
			return "", 0, &models.JobError{Code: engine.ErrorCodeVideoMergeValidation, Message: "options.transitionDuration must be a finite number"}
		}
		if supplied != 0 {
			duration = supplied
		}
	}

	if transition == engine.MergeTransitionCrossfade && (duration < engine.MergeTransitionDurationMin || duration > engine.MergeTransitionDurationMax) {
		return "", 0, &models.JobError{Code: engine.ErrorCodeVideoMergeValidation, Message: "options.transitionDuration must be between 0.1 and 3.0 seconds"}
	}

	return transition, duration, nil
}

func videoMergePlanMessage(count int) string {
	return fmt.Sprintf("Ready to merge %d videos.", count)
}
