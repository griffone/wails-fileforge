package audio

import (
	"context"

	"fileforge-desktop/internal/audio/engine"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/ffmpegex"
)

const (
	ToolIDAudioExtract = "tool.audio.extract"

	audioExtractSuffix = "_audio"
)

var extractSpec = planConversionSpec{
	suffix:         audioExtractSuffix,
	allowVideo:     true,
	successMessage: "Audio extraction successful",
	stage:          "audio.extract",
	verb:           "extract audio from",
}

// ExtractTool pulls the audio track out of a video or audio file and re-encodes
// it to the chosen audio format.
type ExtractTool struct {
	probe    engine.RuntimeProbe
	runner   ffmpegex.CommandRunner
	encoders *engine.EncoderProbe
}

func NewExtractTool() *ExtractTool {
	return NewExtractToolWithDeps(nil, nil, nil)
}

func NewExtractToolWithDeps(probe engine.RuntimeProbe, runner ffmpegex.CommandRunner, encoders *engine.EncoderProbe) *ExtractTool {
	return &ExtractTool{probe: probe, runner: runner, encoders: encoders}
}

func (t *ExtractTool) ID() string {
	return ToolIDAudioExtract
}

func (t *ExtractTool) Capability() string {
	return ToolIDAudioExtract
}

func (t *ExtractTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "Extract Audio",
		Description:      "Extract the audio track from a video or audio file as mp3, m4a, opus, wav or flac",
		Domain:           "audio",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  engine.ExtractInputExtensions(),
		OutputExtensions: engine.SupportedFormatNames(),
		RuntimeDeps:      []string{"ffmpeg", "ffprobe"},
		ResourceClass:    "audio",
		MaxWorkers:       1,
		Tags:             []string{"audio", "extract", "ffmpeg"},
	}
}

func (t *ExtractTool) RuntimeState(ctx context.Context) models.ToolRuntimeState {
	return audioRuntimeState(ctx, t.probe, t.encoders)
}

func (t *ExtractTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)

	return planErr
}

func (t *ExtractTool) Plan(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	return planConversion(ctx, req, t.probe, t.encoders, extractSpec)
}

func (t *ExtractTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

func (t *ExtractTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	return executeSingleWithPlan(ctx, req, t.Plan, t.ExecutePlan, onProgress)
}

func (t *ExtractTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

func (t *ExtractTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	return executeConversionPlan(ctx, plan, onProgress, extractSpec, t.probe, t.runner)
}
