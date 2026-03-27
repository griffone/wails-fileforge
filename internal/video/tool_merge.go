package video

import (
	"context"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

const ToolIDVideoMergeV1 = "tool.video.merge"

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
	probe := t.probe
	if probe == nil {
		probe = engine.NewFFmpegRuntimeProbe()
	}

	if err := probe.Check(ctx); err != nil {
		return models.ToolRuntimeStateV1{Status: "degraded", Healthy: false, Reason: err.Error()}
	}

	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *MergeTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	mergeReq, jobErr := parseMergeRequest(req)
	if jobErr != nil {
		return jobErr
	}

	probe := t.probe
	if probe == nil {
		probe = engine.NewFFmpegRuntimeProbe()
	}
	if runtimeErr := probe.Check(ctx); runtimeErr != nil {
		return mapVideoError(runtimeErr)
	}

	if validationErr := engine.ValidateMergeRequest(mergeReq); validationErr != nil {
		return mapVideoError(validationErr)
	}

	return nil
}

func (t *MergeTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	mergeReq, parseErr := parseMergeRequest(req)
	if parseErr != nil {
		return models.JobResultItemV1{InputPath: firstInputPath(req.InputPaths), OutputPath: optionString(req.Options, "outputPath"), Success: false, Message: parseErr.Message, Error: parseErr}, parseErr
	}

	if err := engine.Merge(ctx, t.probe, t.runner, mergeReq); err != nil {
		jobErr := mapVideoError(err)
		return models.JobResultItemV1{InputPath: firstInputPath(mergeReq.InputPaths), OutputPath: mergeReq.OutputPath, Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	return models.JobResultItemV1{InputPath: firstInputPath(mergeReq.InputPaths), OutputPath: mergeReq.OutputPath, Outputs: []string{mergeReq.OutputPath}, OutputCount: 1, Success: true, Message: "Video merge successful"}, nil
}

func parseMergeRequest(req models.JobRequestV1) (engine.MergeRequest, *models.JobErrorV1) {
	if strings.TrimSpace(req.Mode) != "single" {
		return engine.MergeRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoMergeValidation, Message: "mode must be single"}
	}

	if len(req.InputPaths) < 2 {
		return engine.MergeRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoMergeInsufficientInputs, Message: "at least 2 input videos are required"}
	}

	trimmedInputPaths := make([]string, 0, len(req.InputPaths))
	for _, inputPath := range req.InputPaths {
		trimmed := strings.TrimSpace(inputPath)
		if trimmed == "" {
			return engine.MergeRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoMergeValidation, Message: "inputPaths must not contain empty values"}
		}
		trimmedInputPaths = append(trimmedInputPaths, trimmed)
	}

	outputPath := optionString(req.Options, "outputPath")
	if outputPath == "" {
		return engine.MergeRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoMergeValidation, Message: "options.outputPath is required"}
	}

	targetFormat := strings.ToLower(optionString(req.Options, "targetFormat"))
	if targetFormat == "" {
		return engine.MergeRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoMergeValidation, Message: "options.targetFormat is required"}
	}

	qualityPreset := strings.ToLower(optionString(req.Options, "qualityPreset"))
	if qualityPreset == "" {
		return engine.MergeRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoMergeValidation, Message: "options.qualityPreset is required"}
	}

	mergeMode := strings.ToLower(optionString(req.Options, "mergeMode"))
	if mergeMode == "" {
		mergeMode = engine.MergeModeAuto
	}

	return engine.MergeRequest{
		InputPaths:    trimmedInputPaths,
		OutputPath:    outputPath,
		TargetFormat:  targetFormat,
		QualityPreset: qualityPreset,
		MergeMode:     mergeMode,
	}, nil
}
