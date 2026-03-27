package video

import (
	"context"
	"fmt"
	"path/filepath"
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
		Description:      "Trim videos by start/end time to mp4 or webm",
		Domain:           "video",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
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
	probe := t.probe
	if probe == nil {
		probe = engine.NewFFmpegRuntimeProbe()
	}
	if runtimeErr := probe.Check(ctx); runtimeErr != nil {
		return mapVideoError(runtimeErr)
	}

	switch strings.TrimSpace(req.Mode) {
	case "single":
		trimReq, jobErr := parseTrimRequest(req)
		if jobErr != nil {
			return jobErr
		}

		if validationErr := engine.ValidateTrimRequest(trimReq); validationErr != nil {
			return mapVideoError(validationErr)
		}
	case "batch":
		trimReqs, jobErr := parseTrimBatchRequests(req)
		if jobErr != nil {
			return jobErr
		}

		for _, trimReq := range trimReqs {
			if validationErr := engine.ValidateTrimRequest(trimReq); validationErr != nil {
				return mapVideoError(validationErr)
			}
		}
	default:
		return &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "mode must be single or batch"}
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

func (t *TrimTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	trimReqs, parseErr := parseTrimBatchRequests(req)
	if parseErr != nil {
		return nil, parseErr
	}

	items := make([]models.JobResultItemV1, 0, len(trimReqs))
	var firstErr *models.JobErrorV1

	for index, trimReq := range trimReqs {
		select {
		case <-ctx.Done():
			cancelErr := &models.JobErrorV1{Code: "CANCELED", Message: ctx.Err().Error()}
			return items, cancelErr
		default:
		}

		if err := engine.Trim(ctx, t.probe, t.runner, trimReq); err != nil {
			jobErr := mapVideoError(err)
			if firstErr == nil {
				firstErr = jobErr
			}
			items = append(items, models.JobResultItemV1{InputPath: trimReq.InputPath, OutputPath: trimReq.OutputPath, Success: false, Message: jobErr.Message, Error: jobErr})
		} else {
			items = append(items, models.JobResultItemV1{InputPath: trimReq.InputPath, OutputPath: trimReq.OutputPath, Outputs: []string{trimReq.OutputPath}, OutputCount: 1, Success: true, Message: "Video trim successful"})
		}

		if onProgress != nil {
			onProgress(models.JobProgressV1{Current: index + 1, Total: len(trimReqs), Stage: jobs.StatusRunning, Message: fmt.Sprintf("processed %d/%d", index+1, len(trimReqs))})
		}
	}

	if firstErr != nil {
		return items, firstErr
	}

	return items, nil
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

func parseTrimBatchRequests(req models.JobRequestV1) ([]engine.TrimRequest, *models.JobErrorV1) {
	if strings.TrimSpace(req.Mode) != "batch" {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "mode must be batch"}
	}

	if len(req.InputPaths) < 1 {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "at least 1 input video is required"}
	}

	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "outputDir is required for batch mode"}
	}

	startTime, ok := optionNumber(req.Options, "startTime")
	if !ok {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.startTime is required"}
	}

	endTime, ok := optionNumber(req.Options, "endTime")
	if !ok {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.endTime is required"}
	}

	targetFormat := strings.ToLower(optionString(req.Options, "targetFormat"))
	if targetFormat == "" {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.targetFormat is required"}
	}

	qualityPreset := strings.ToLower(optionString(req.Options, "qualityPreset"))
	if qualityPreset == "" {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "options.qualityPreset is required"}
	}

	trimMode := strings.ToLower(optionString(req.Options, "trimMode"))
	if trimMode == "" {
		trimMode = engine.TrimModeAuto
	}

	reqs := make([]engine.TrimRequest, 0, len(req.InputPaths))
	plannedOutputs := make(map[string]struct{}, len(req.InputPaths))

	for index, rawInputPath := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "inputPaths must not contain empty values"}
		}

		baseName := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
		if baseName == "" {
			baseName = fmt.Sprintf("video_%d", index+1)
		}

		outputFileName := fmt.Sprintf("%s_trimmed.%s", baseName, targetFormat)
		outputPath := filepath.Join(outputDir, outputFileName)
		normalizedOutputKey := strings.ToLower(filepath.Clean(outputPath))
		if _, exists := plannedOutputs[normalizedOutputKey]; exists {
			return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoTrimValidation, Message: "batch output collision detected; input filenames must be unique"}
		}
		plannedOutputs[normalizedOutputKey] = struct{}{}

		reqs = append(reqs, engine.TrimRequest{
			InputPath:     inputPath,
			OutputPath:    outputPath,
			StartTime:     startTime,
			EndTime:       endTime,
			TargetFormat:  targetFormat,
			QualityPreset: qualityPreset,
			TrimMode:      trimMode,
		})
	}

	return reqs, nil
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
