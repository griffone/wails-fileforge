package audio

import (
	"context"

	"fileforge-desktop/internal/audio/engine"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/ffmpegex"
)

const (
	ToolIDAudioConvert = "tool.audio.convert"

	audioConvertSuffix = "_converted"
)

var convertSpec = planConversionSpec{
	suffix:         audioConvertSuffix,
	allowVideo:     false,
	successMessage: "Audio conversion successful",
	stage:          "audio.convert",
	verb:           "convert",
}

// ConvertTool converts audio between the supported formats.
type ConvertTool struct {
	probe    engine.RuntimeProbe
	runner   ffmpegex.CommandRunner
	encoders *engine.EncoderProbe
}

func NewConvertTool() *ConvertTool {
	return NewConvertToolWithDeps(nil, nil, nil)
}

func NewConvertToolWithDeps(probe engine.RuntimeProbe, runner ffmpegex.CommandRunner, encoders *engine.EncoderProbe) *ConvertTool {
	return &ConvertTool{probe: probe, runner: runner, encoders: encoders}
}

func (t *ConvertTool) ID() string {
	return ToolIDAudioConvert
}

func (t *ConvertTool) Capability() string {
	return ToolIDAudioConvert
}

func (t *ConvertTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "Audio Convert",
		Description:      "Convert audio files between mp3, m4a, opus, wav and flac",
		Domain:           "audio",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  engine.AudioInputExtensions(),
		OutputExtensions: engine.SupportedFormatNames(),
		RuntimeDeps:      []string{"ffmpeg", "ffprobe"},
		ResourceClass:    "audio",
		MaxWorkers:       1,
		Tags:             []string{"audio", "convert", "ffmpeg"},
	}
}

func (t *ConvertTool) RuntimeState(ctx context.Context) models.ToolRuntimeState {
	return audioRuntimeState(ctx, t.probe, t.encoders)
}

func (t *ConvertTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)

	return planErr
}

func (t *ConvertTool) Plan(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	return planConversion(ctx, req, t.probe, t.encoders, convertSpec)
}

func (t *ConvertTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

func (t *ConvertTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	return executeSingleWithPlan(ctx, req, t.Plan, t.ExecutePlan, onProgress)
}

func (t *ConvertTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

func (t *ConvertTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	return executeConversionPlan(ctx, plan, onProgress, convertSpec, t.probe, t.runner)
}
