package audio

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/audio/engine"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/platform/ffmpegex"
)

const (
	ToolIDAudioNormalize = "tool.audio.normalize"

	audioNormalizeSuffix = "_normalized"
)

// NormalizeTool runs two-pass EBU R128 loudness normalization. The input
// container is preserved when it is one of the supported formats.
type NormalizeTool struct {
	probe    engine.RuntimeProbe
	runner   ffmpegex.CommandRunner
	encoders *engine.EncoderProbe
}

func NewNormalizeTool() *NormalizeTool {
	return NewNormalizeToolWithDeps(nil, nil, nil)
}

func NewNormalizeToolWithDeps(probe engine.RuntimeProbe, runner ffmpegex.CommandRunner, encoders *engine.EncoderProbe) *NormalizeTool {
	return &NormalizeTool{probe: probe, runner: runner, encoders: encoders}
}

func (t *NormalizeTool) ID() string {
	return ToolIDAudioNormalize
}

func (t *NormalizeTool) Capability() string {
	return ToolIDAudioNormalize
}

func (t *NormalizeTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "Normalize Audio",
		Description:      "Two-pass EBU R128 loudness normalization to streaming, broadcast or podcast targets",
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
		Tags:             []string{"audio", "normalize", "loudness", "ffmpeg"},
	}
}

func (t *NormalizeTool) RuntimeState(ctx context.Context) models.ToolRuntimeState {
	return audioRuntimeState(ctx, t.probe, t.encoders)
}

func (t *NormalizeTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)

	return planErr
}

// Plan resolves one output per input. The target format follows the input
// container when possible; an explicit outputPath overrides it and must use a
// supported extension.
func (t *NormalizeTool) Plan(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	if err := resolveRuntimeProbe(t.probe).Check(ctx); err != nil {
		return planFailure(plan, mapAudioError(err))
	}

	if rawFormat, supplied := optionNumberString(req.Options, "format"); supplied && rawFormat != "" {
		return planFailure(plan, &models.JobError{
			Code:    engine.ErrorCodeValidation,
			Message: "normalize preserves the input container and does not accept a format option; use outputPath to choose a different format",
		})
	}

	mode := strings.TrimSpace(req.Mode)
	switch mode {
	case "single":
		if len(req.InputPaths) != 1 {
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeValidation, Message: "exactly 1 input file is required"})
		}
	case "batch":
		if len(req.InputPaths) < 1 {
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeValidation, Message: "at least 1 input file is required"})
		}
	default:
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeValidation, Message: "mode must be single or batch"})
	}

	target, targetErr := resolveLoudnormTargetOption(req.Options)
	if targetErr != nil {
		return planFailure(plan, targetErr)
	}

	truePeak, truePeakErr := resolveTruePeakOption(req.Options)
	if truePeakErr != nil {
		return planFailure(plan, truePeakErr)
	}

	bitrate, bitrateErr := audioOptionInt(req.Options, "bitrateKbps")
	if bitrateErr != nil {
		return planFailure(plan, bitrateErr)
	}
	if bitrate < 0 {
		return planFailure(plan, &models.JobError{Code: engine.ErrorCodeBitrateInvalid, Message: "bitrateKbps must be >= 0"})
	}

	explicitOutput := ""
	if mode == "single" {
		explicitOutput = optionString(req.Options, "outputPath")
	}

	used := reserveInputPaths(req.InputPaths)
	outputs := make([]models.PlannedOutput, 0, len(req.InputPaths))
	warnings := make([]string, 0, len(req.InputPaths))
	checkedEncoders := make(map[string]struct{}, len(req.InputPaths))
	losslessWarned := make(map[string]struct{}, len(engine.SupportedFormatNames()))

	for _, rawInputPath := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeValidation, Message: "inputPaths must not contain empty values"})
		}
		if inputErr := validateAudioInputExtension(inputPath, false); inputErr != nil {
			return planFailure(plan, inputErr)
		}

		format, formatErr := normalizeTargetFormat(inputPath, explicitOutput)
		if formatErr != nil {
			return planFailure(plan, formatErr)
		}

		if _, checked := checkedEncoders[format.Encoder]; !checked {
			if runtimeErr := checkAudioRuntime(ctx, t.probe, t.encoders, format); runtimeErr != nil {
				plan.Outputs = outputs
				return planFailure(plan, runtimeErr)
			}
			checkedEncoders[format.Encoder] = struct{}{}
		}
		if bitrate > 0 && format.Lossless {
			if _, warned := losslessWarned[format.Name]; !warned {
				warnings = append(warnings, fmt.Sprintf("bitrateKbps is ignored for %s output because the format is lossless.", format.Name))
				losslessWarned[format.Name] = struct{}{}
			}
		}

		var planned models.PlannedOutput
		if explicitOutput != "" {
			planned = models.PlannedOutput{
				InputPath:  inputPath,
				OutputPath: filepath.Clean(strings.TrimSpace(explicitOutput)),
				OutputKind: audioOutputKindFile,
			}
			if _, err := os.Stat(planned.OutputPath); err == nil {
				planned.Exists = true
			}
		} else {
			planned = planAutoAudioOutput(inputPath, req.OutputDir, audioNormalizeSuffix, format.Name, used)
		}

		planErr := mapAudioError(engine.ValidateRequest(engine.Request{
			InputPath:   planned.InputPath,
			OutputPath:  planned.OutputPath,
			Format:      format.Name,
			BitrateKbps: bitrate,
		}))

		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
		if planErr != nil {
			plan.Outputs = outputs
			return planFailure(plan, planErr)
		}
	}

	plan.Success = true
	plan.Message = fmt.Sprintf("Ready to normalize %d %s to %.0f LUFS (%s).", len(outputs), pluralize("file", len(outputs)), target.IntegratedLoudness, target.Name)
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = map[string]any{
		"format":          "preserve",
		"bitrateKbps":     bitrate,
		"targetLufs":      target.Name,
		"targetLufsValue": target.IntegratedLoudness,
		"truePeak":        truePeak,
	}

	return plan, nil
}

func (t *NormalizeTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

func (t *NormalizeTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	return executeSingleWithPlan(ctx, req, t.Plan, t.ExecutePlan, onProgress)
}

func (t *NormalizeTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

func (t *NormalizeTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	target, targetErr := engine.ResolveLoudnormTarget(optionString(plan.EffectiveOptions, "targetLufs"))
	if targetErr != nil {
		return nil, mapAudioError(targetErr)
	}
	truePeak, ok := plan.EffectiveOptions["truePeak"].(float64)
	if !ok {
		truePeak = engine.DefaultTruePeak
	}
	bitrate, _ := plan.EffectiveOptions["bitrateKbps"].(int)

	return executeAudioPlan(ctx, plan, onProgress, "audio.normalize", func(ctx context.Context, planned models.PlannedOutput, itemProgress func(float64, string)) (models.JobResultItem, *models.JobError) {
		format, ok := engine.FormatForExtension(engine.ExtensionOf(planned.OutputPath))
		if !ok {
			return models.JobResultItem{}, &models.JobError{
				Code:    engine.ErrorCodeFormatUnsupported,
				Message: fmt.Sprintf("unsupported output format .%s", engine.ExtensionOf(planned.OutputPath)),
			}
		}

		result, normalizeErr := engine.Normalize(ctx, t.probe, t.runner, engine.Request{
			InputPath:   planned.InputPath,
			OutputPath:  planned.OutputPath,
			Format:      format.Name,
			BitrateKbps: bitrate,
		}, target, truePeak, itemProgress)
		if normalizeErr != nil {
			return models.JobResultItem{}, mapAudioError(normalizeErr)
		}

		return normalizeSuccessItem(planned, result), nil
	})
}

func normalizeSuccessItem(planned models.PlannedOutput, result engine.LoudnormResult) models.JobResultItem {
	item := successItem(planned, fmt.Sprintf(
		"Normalized to %.0f LUFS. Input %.1f LUFS, output %.1f LUFS.",
		result.TargetLUFS,
		result.InputLUFS,
		result.OutputLUFS,
	))
	item.Details = map[string]any{
		"targetName":     result.TargetName,
		"targetLufs":     result.TargetLUFS,
		"truePeak":       result.TruePeak,
		"inputLufs":      result.InputLUFS,
		"inputTruePeak":  result.InputTruePeak,
		"outputLufs":     result.OutputLUFS,
		"outputTruePeak": result.OutputTruePeak,
	}

	return item
}

// normalizeTargetFormat preserves the input container, or follows an explicit
// output path when the user chose one.
func normalizeTargetFormat(inputPath, explicitOutput string) (engine.Format, *models.JobError) {
	if trimmed := strings.TrimSpace(explicitOutput); trimmed != "" {
		if format, ok := engine.FormatForExtension(engine.ExtensionOf(trimmed)); ok {
			return format, nil
		}
		return engine.Format{}, &models.JobError{
			Code:    engine.ErrorCodeFormatUnsupported,
			Message: fmt.Sprintf("output extension .%s is not a supported audio format", engine.ExtensionOf(trimmed)),
		}
	}

	return engine.TargetFormatForInput(inputPath), nil
}

func resolveLoudnormTargetOption(options map[string]any) (engine.LoudnormTarget, *models.JobError) {
	raw, supplied := optionNumberString(options, "targetLufs")
	if !supplied {
		raw = engine.LoudnormStreaming
	}

	target, err := engine.ResolveLoudnormTarget(raw)
	if err != nil {
		return engine.LoudnormTarget{}, &models.JobError{Code: err.Code, Message: err.Message}
	}

	return target, nil
}

func resolveTruePeakOption(options map[string]any) (float64, *models.JobError) {
	value, supplied, err := audioOptionFloat(options, "truePeak", engine.ErrorCodeTruePeakInvalid)
	if err != nil {
		return 0, err
	}
	if !supplied {
		return engine.DefaultTruePeak, nil
	}
	if validateErr := engine.ValidateTruePeak(value); validateErr != nil {
		return 0, &models.JobError{Code: validateErr.Code, Message: validateErr.Message}
	}

	return value, nil
}
