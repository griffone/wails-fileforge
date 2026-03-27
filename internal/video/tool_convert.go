package video

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/video/engine"
)

const ToolIDVideoConvertV1 = "tool.video.convert"

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
		Tags:             []string{"video", "convert", "ffmpeg"},
	}
}

func (t *ConvertTool) RuntimeState(ctx context.Context) models.ToolRuntimeStateV1 {
	probe := t.probe
	if probe == nil {
		probe = engine.NewFFmpegRuntimeProbe()
	}

	if err := probe.Check(ctx); err != nil {
		return models.ToolRuntimeStateV1{Status: "degraded", Healthy: false, Reason: err.Error()}
	}

	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *ConvertTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	probe := t.probe
	if probe == nil {
		probe = engine.NewFFmpegRuntimeProbe()
	}
	if runtimeErr := probe.Check(ctx); runtimeErr != nil {
		return mapVideoError(runtimeErr)
	}

	switch strings.TrimSpace(req.Mode) {
	case "single":
		convertReq, jobErr := parseConvertRequest(req)
		if jobErr != nil {
			return jobErr
		}

		if validationErr := engine.ValidateConvertRequest(convertReq); validationErr != nil {
			return mapVideoError(validationErr)
		}
	case "batch":
		convertReqs, jobErr := parseConvertBatchRequests(req)
		if jobErr != nil {
			return jobErr
		}

		for _, convertReq := range convertReqs {
			if validationErr := engine.ValidateConvertRequest(convertReq); validationErr != nil {
				return mapVideoError(validationErr)
			}
		}
	default:
		return &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "mode must be single or batch"}
	}

	return nil
}

func (t *ConvertTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	convertReq, parseErr := parseConvertRequest(req)
	if parseErr != nil {
		return models.JobResultItemV1{InputPath: firstInputPath(req.InputPaths), OutputPath: optionString(req.Options, "outputPath"), Success: false, Message: parseErr.Message, Error: parseErr}, parseErr
	}

	if err := engine.Convert(ctx, t.probe, t.runner, convertReq); err != nil {
		jobErr := mapVideoError(err)
		return models.JobResultItemV1{InputPath: convertReq.InputPath, OutputPath: convertReq.OutputPath, Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	return models.JobResultItemV1{InputPath: convertReq.InputPath, OutputPath: convertReq.OutputPath, Outputs: []string{convertReq.OutputPath}, OutputCount: 1, Success: true, Message: "Video conversion successful"}, nil
}

func (t *ConvertTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	convertReqs, parseErr := parseConvertBatchRequests(req)
	if parseErr != nil {
		return nil, parseErr
	}

	items := make([]models.JobResultItemV1, 0, len(convertReqs))
	var firstErr *models.JobErrorV1

	for index, convertReq := range convertReqs {
		select {
		case <-ctx.Done():
			cancelErr := &models.JobErrorV1{Code: "CANCELED", Message: ctx.Err().Error()}
			return items, cancelErr
		default:
		}

		if err := engine.Convert(ctx, t.probe, t.runner, convertReq); err != nil {
			jobErr := mapVideoError(err)
			if firstErr == nil {
				firstErr = jobErr
			}
			items = append(items, models.JobResultItemV1{InputPath: convertReq.InputPath, OutputPath: convertReq.OutputPath, Success: false, Message: jobErr.Message, Error: jobErr})
		} else {
			items = append(items, models.JobResultItemV1{InputPath: convertReq.InputPath, OutputPath: convertReq.OutputPath, Outputs: []string{convertReq.OutputPath}, OutputCount: 1, Success: true, Message: "Video conversion successful"})
		}

		if onProgress != nil {
			onProgress(models.JobProgressV1{Current: index + 1, Total: len(convertReqs), Stage: "running", Message: fmt.Sprintf("processed %d/%d", index+1, len(convertReqs))})
		}
	}

	if firstErr != nil {
		return items, firstErr
	}

	return items, nil
}

func parseConvertRequest(req models.JobRequestV1) (engine.ConvertRequest, *models.JobErrorV1) {
	if strings.TrimSpace(req.Mode) != "single" {
		return engine.ConvertRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "mode must be single"}
	}

	if len(req.InputPaths) != 1 {
		return engine.ConvertRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "exactly 1 input video is required"}
	}

	outputPath := optionString(req.Options, "outputPath")
	if outputPath == "" {
		return engine.ConvertRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "options.outputPath is required"}
	}

	targetFormat := strings.ToLower(optionString(req.Options, "targetFormat"))
	if targetFormat == "" {
		return engine.ConvertRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "options.targetFormat is required"}
	}

	qualityPreset := strings.ToLower(optionString(req.Options, "qualityPreset"))
	if qualityPreset == "" {
		return engine.ConvertRequest{}, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "options.qualityPreset is required"}
	}

	return engine.ConvertRequest{
		InputPath:     strings.TrimSpace(req.InputPaths[0]),
		OutputPath:    outputPath,
		TargetFormat:  targetFormat,
		QualityPreset: qualityPreset,
	}, nil
}

func parseConvertBatchRequests(req models.JobRequestV1) ([]engine.ConvertRequest, *models.JobErrorV1) {
	if strings.TrimSpace(req.Mode) != "batch" {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "mode must be batch"}
	}

	if len(req.InputPaths) < 1 {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "at least 1 input video is required"}
	}

	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "outputDir is required for batch mode"}
	}

	targetFormat := strings.ToLower(optionString(req.Options, "targetFormat"))
	if targetFormat == "" {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "options.targetFormat is required"}
	}

	qualityPreset := strings.ToLower(optionString(req.Options, "qualityPreset"))
	if qualityPreset == "" {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "options.qualityPreset is required"}
	}

	reqs := make([]engine.ConvertRequest, 0, len(req.InputPaths))
	plannedOutputs := make(map[string]struct{}, len(req.InputPaths))

	for index, rawInputPath := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "inputPaths must not contain empty values"}
		}

		baseName := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
		if baseName == "" {
			baseName = fmt.Sprintf("video_%d", index+1)
		}

		outputFileName := fmt.Sprintf("%s_converted.%s", baseName, targetFormat)
		outputPath := filepath.Join(outputDir, outputFileName)
		normalizedOutputKey := strings.ToLower(filepath.Clean(outputPath))
		if _, exists := plannedOutputs[normalizedOutputKey]; exists {
			return nil, &models.JobErrorV1{Code: engine.ErrorCodeVideoValidation, Message: "batch output collision detected; input filenames must be unique"}
		}
		plannedOutputs[normalizedOutputKey] = struct{}{}

		reqs = append(reqs, engine.ConvertRequest{
			InputPath:     inputPath,
			OutputPath:    outputPath,
			TargetFormat:  targetFormat,
			QualityPreset: qualityPreset,
		})
	}

	return reqs, nil
}

func mapVideoError(err error) *models.JobErrorV1 {
	var videoErr *engine.VideoError
	if !errors.As(err, &videoErr) {
		return &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	if videoErr.Code == engine.ErrorCodeRuntimeFFmpegNotFound || videoErr.Code == engine.ErrorCodeRuntimeFFprobeNotFound {
		return &models.JobErrorV1{Code: engine.ErrorCodeRuntimeUnavailable, Message: "ffmpeg/ffprobe runtime is unavailable"}
	}

	return &models.JobErrorV1{Code: videoErr.Code, Message: videoErr.Message}
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
