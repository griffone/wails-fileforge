package video

import (
	"context"
	"strings"

	"fileforge-desktop/internal/jobs"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

const ToolIDVideoTrimV1 = "tool.video.trim"

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
		Description:      "Trim a single video by start/end time to mp4 or webm",
		Domain:           "video",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    false,
		InputExtensions:  []string{"mp4", "mov", "mkv"},
		OutputExtensions: []string{"mp4", "webm"},
		RuntimeDeps:      []string{"ffmpeg", "ffprobe"},
		Tags:             []string{"video", "trim", "ffmpeg"},
	}
}

func (t *TrimTool) RuntimeState(ctx context.Context) models.ToolRuntimeStateV1 {
	probe := t.probe
	if probe == nil {
		probe = engine.NewFFmpegRuntimeProbe()
	}

	if err := probe.Check(ctx); err != nil {
		return models.ToolRuntimeStateV1{Status: "degraded", Healthy: false, Reason: err.Error()}
	}

	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *TrimTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	trimReq, jobErr := parseTrimRequest(req)
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

	if validationErr := engine.ValidateTrimRequest(trimReq); validationErr != nil {
		return mapVideoError(validationErr)
	}

	return nil
}

func (t *TrimTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	return t.executeSingle(ctx, req, nil)
}

func (t *TrimTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
	return t.executeSingle(ctx, req, onProgress)
}

func (t *TrimTool) executeSingle(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
	trimReq, parseErr := parseTrimRequest(req)
	if parseErr != nil {
		return models.JobResultItemV1{InputPath: firstInputPath(req.InputPaths), OutputPath: optionString(req.Options, "outputPath"), Success: false, Message: parseErr.Message, Error: parseErr}, parseErr
	}

	var fallbackInfo string
	trimErr := engine.TrimWithProgress(ctx, t.probe, t.runner, trimReq, func(evt engine.TrimProgressEvent) {
		if onProgress != nil {
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
		}
		if strings.Contains(evt.Stage, "fallback") {
			fallbackInfo = evt.Message
		}
	})

	if trimErr != nil {
		jobErr := mapVideoError(trimErr)
		if trimErr.Code == engine.ErrorCodeVideoTrimAutoFallbackFailed {
			jobErr.Details = map[string]any{"fallbackUsed": true, "fallbackStatus": "failed"}
		}
		if trimErr.Code == engine.ErrorCodeVideoTrimCopyFailed && trimReq.TrimMode == engine.TrimModeAuto {
			jobErr.Details = map[string]any{"fallbackUsed": false}
		}
		return models.JobResultItemV1{InputPath: trimReq.InputPath, OutputPath: trimReq.OutputPath, Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	message := "Video trim successful"
	item := models.JobResultItemV1{InputPath: trimReq.InputPath, OutputPath: trimReq.OutputPath, Outputs: []string{trimReq.OutputPath}, OutputCount: 1, Success: true, Message: message}

	if trimReq.TrimMode == engine.TrimModeAuto && fallbackInfo != "" {
		item.Message = message + " (fallback re-encode used)"
	}

	if onProgress != nil {
		onProgress(models.JobProgressV1{Current: 100, Total: 100, Stage: "trim.done", Message: item.Message})
	}

	return item, nil
}

func parseTrimRequest(req models.JobRequestV1) (engine.TrimRequest, *models.JobErrorV1) {
	if strings.TrimSpace(req.Mode) != "single" {
		return engine.TrimRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "mode must be single"}
	}

	if len(req.InputPaths) != 1 {
		return engine.TrimRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "exactly 1 input video is required"}
	}

	outputPath := optionString(req.Options, "outputPath")
	if outputPath == "" {
		return engine.TrimRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.outputPath is required"}
	}

	startTime, ok := optionNumber(req.Options, "startTime")
	if !ok {
		return engine.TrimRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.startTime is required"}
	}

	endTime, ok := optionNumber(req.Options, "endTime")
	if !ok {
		return engine.TrimRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.endTime is required"}
	}

	targetFormat := strings.ToLower(optionString(req.Options, "targetFormat"))
	if targetFormat == "" {
		return engine.TrimRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.targetFormat is required"}
	}

	qualityPreset := strings.ToLower(optionString(req.Options, "qualityPreset"))
	if qualityPreset == "" {
		return engine.TrimRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.qualityPreset is required"}
	}

	trimMode := strings.ToLower(optionString(req.Options, "trimMode"))
	if trimMode == "" {
		trimMode = engine.TrimModeAuto
	}

	return engine.TrimRequest{
		InputPath:     strings.TrimSpace(req.InputPaths[0]),
		OutputPath:    outputPath,
		StartTime:     startTime,
		EndTime:       endTime,
		TargetFormat:  targetFormat,
		QualityPreset: qualityPreset,
		TrimMode:      trimMode,
	}, nil
}

func optionNumber(options map[string]any, key string) (float64, bool) {
	if options == nil {
		return 0, false
	}

	v, ok := options[key]
	if !ok || v == nil {
		return 0, false
	}

	n, ok := v.(float64)
	if !ok {
		return 0, false
	}

	return n, true
}
