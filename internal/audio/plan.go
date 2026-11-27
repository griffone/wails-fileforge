package audio

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fileforge-desktop/internal/audio/engine"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/platform/ffmpegex"
)

const (
	audioOutputKindFile = "file"

	// jobProgressTotal turns a percentage into the job progress contract, which
	// only carries a current/total pair.
	jobProgressTotal = 100
)

// planConversionSpec parameterizes the two conversion-shaped tools: extract and
// convert differ only in the accepted inputs, the output suffix and the tool
// identity.
type planConversionSpec struct {
	suffix         string
	allowVideo     bool
	successMessage string
	stage          string
	verb           string
}

func resolveRuntimeProbe(probe engine.RuntimeProbe) engine.RuntimeProbe {
	if probe != nil {
		return probe
	}

	return engine.NewFFmpegRuntimeProbe()
}

func resolveEncoderProbe(probe *engine.EncoderProbe) *engine.EncoderProbe {
	if probe != nil {
		return probe
	}

	return engine.DefaultEncoderProbe()
}

// audioRuntimeState reports a degraded tool when ffmpeg/ffprobe or any encoder
// the domain advertises is missing. The reason always says what to install.
func audioRuntimeState(ctx context.Context, probe engine.RuntimeProbe, encoders *engine.EncoderProbe) models.ToolRuntimeState {
	if err := resolveRuntimeProbe(probe).Check(ctx); err != nil {
		reason := err.Error()
		var audioErr *engine.AudioError
		if errors.As(err, &audioErr) && audioErr != nil {
			reason = audioErr.Message
		}
		return models.ToolRuntimeState{Status: "degraded", Healthy: false, Reason: reason}
	}

	missing, err := resolveEncoderProbe(encoders).Missing(ctx, engine.AllEncoderNames())
	if err != nil {
		return models.ToolRuntimeState{
			Status:  "degraded",
			Healthy: false,
			Reason:  "ffmpeg encoders could not be inspected: " + err.Error(),
		}
	}
	if len(missing) > 0 {
		return models.ToolRuntimeState{
			Status:  "degraded",
			Healthy: false,
			Reason: fmt.Sprintf(
				"this ffmpeg build is missing audio encoders: %s. Install a full ffmpeg build to enable every audio format.",
				strings.Join(missing, ", "),
			),
		}
	}

	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

// checkAudioRuntime checks the binaries and the encoder the resolved format
// needs, so a missing encoder fails the plan instead of the encode.
func checkAudioRuntime(ctx context.Context, probe engine.RuntimeProbe, encoders *engine.EncoderProbe, format engine.Format) *models.JobError {
	if err := resolveRuntimeProbe(probe).Check(ctx); err != nil {
		return mapAudioError(err)
	}

	available, err := resolveEncoderProbe(encoders).Has(ctx, format.Encoder)
	if err != nil {
		return &models.JobError{
			Code:    engine.ErrorCodeRuntimeUnavailable,
			Message: "ffmpeg encoder availability could not be checked: " + err.Error(),
		}
	}
	if !available {
		return &models.JobError{
			Code:    engine.ErrorCodeEncoderUnavailable,
			Message: fmt.Sprintf("%s output needs the %s encoder, which this ffmpeg build does not provide. Install a full ffmpeg build or choose another format.", format.Name, format.Encoder),
		}
	}

	return nil
}

// mapAudioError translates an engine failure into the tool contract. A missing
// binary collapses into the single runtime-unavailable code; everything else
// keeps its stable detail code.
func mapAudioError(err error) *models.JobError {
	if err == nil {
		return nil
	}

	var audioErr *engine.AudioError
	if errors.As(err, &audioErr) {
		if audioErr == nil {
			// A typed nil *AudioError wrapped in the error interface is a success.
			return nil
		}

		switch audioErr.Code {
		case engine.ErrorCodeRuntimeFFmpegNotFound, engine.ErrorCodeRuntimeFFprobeNotFound:
			return &models.JobError{Code: engine.ErrorCodeRuntimeUnavailable, Message: "ffmpeg/ffprobe runtime is unavailable"}
		case engine.ErrorCodeCancelled:
			return &models.JobError{Code: "CANCELED", Message: audioErr.Message}
		default:
			return &models.JobError{Code: audioErr.Code, DetailCode: audioErr.Code, Message: audioErr.Message}
		}
	}

	return &models.JobError{Code: "EXECUTION_ERROR", Message: err.Error()}
}

// resolveAudioFormat reads the target format, defaulting to mp3.
func resolveAudioFormat(options map[string]any) (engine.Format, *models.JobError) {
	format, err := engine.FormatByName(optionString(options, "format"))
	if err != nil {
		return engine.Format{}, &models.JobError{Code: err.Code, Message: err.Message}
	}

	return format, nil
}

// resolveAudioBitrate reads the optional bitrate and warns when a lossless
// format makes it meaningless instead of silently dropping it.
func resolveAudioBitrate(options map[string]any, format engine.Format) (int, []string, *models.JobError) {
	bitrate, err := audioOptionInt(options, "bitrateKbps")
	if err != nil {
		return 0, nil, err
	}
	if bitrate < 0 {
		return 0, nil, &models.JobError{Code: engine.ErrorCodeBitrateInvalid, Message: "bitrateKbps must be >= 0"}
	}
	if bitrate > 0 && format.Lossless {
		return 0, []string{fmt.Sprintf("bitrateKbps is ignored for %s output because the format is lossless.", format.Name)}, nil
	}

	return bitrate, nil, nil
}

// validateAudioInputExtension rejects containers the tool cannot decode.
func validateAudioInputExtension(path string, allowVideo bool) *models.JobError {
	extension := engine.ExtensionOf(path)
	if engine.IsAudioInputExtension(extension) {
		return nil
	}
	if allowVideo && engine.IsVideoInputExtension(extension) {
		return nil
	}
	if extension == "" {
		return &models.JobError{Code: engine.ErrorCodeInputUnsupported, Message: "input path has no file extension"}
	}

	return &models.JobError{
		Code:    engine.ErrorCodeInputUnsupported,
		Message: fmt.Sprintf("unsupported input format .%s", extension),
	}
}

// planAutoAudioOutput resolves "<stem><suffix>.<format>" next to the input, or
// inside outputDir when the user chose a folder. Reserved names and files on
// disk are never reused; a suffixed name marks the plan as adjusted.
func planAutoAudioOutput(inputPath, outputDir, suffix, format string, used map[string]struct{}) models.PlannedOutput {
	planned := models.PlannedOutput{
		InputPath:  inputPath,
		OutputKind: audioOutputKindFile,
	}

	targetDir := strings.TrimSpace(outputDir)
	if targetDir == "" {
		targetDir = filepath.Dir(inputPath)
	}
	targetDir = filepath.Clean(targetDir)

	stem := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if strings.TrimSpace(stem) == "" {
		stem = "audio"
	}

	base := stem + suffix
	naturalPath := filepath.Join(targetDir, fmt.Sprintf("%s.%s", base, format))
	outputPath, adjusted := outputplan.UniqueFilePath(targetDir, base, format, used)

	planned.OutputPath = outputPath
	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", filepath.Base(naturalPath), filepath.Base(outputPath)))
	}

	return planned
}

// reserveInputPaths prevents an automatic output from overwriting a different
// input of the same batch.
func reserveInputPaths(inputPaths []string) map[string]struct{} {
	used := make(map[string]struct{}, len(inputPaths))
	for _, inputPath := range inputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	return used
}

// planConversion is the shared plan for extract and convert: one target format
// for the whole request, one output per input.
func planConversion(ctx context.Context, req models.JobRequest, probe engine.RuntimeProbe, encoders *engine.EncoderProbe, spec planConversionSpec) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	format, formatErr := resolveAudioFormat(req.Options)
	if formatErr != nil {
		return planFailure(plan, formatErr)
	}
	if runtimeErr := checkAudioRuntime(ctx, probe, encoders, format); runtimeErr != nil {
		return planFailure(plan, runtimeErr)
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

	bitrate, bitrateWarnings, bitrateErr := resolveAudioBitrate(req.Options, format)
	if bitrateErr != nil {
		return planFailure(plan, bitrateErr)
	}

	explicitOutput := ""
	if mode == "single" {
		explicitOutput = optionString(req.Options, "outputPath")
	}

	used := reserveInputPaths(req.InputPaths)
	outputs := make([]models.PlannedOutput, 0, len(req.InputPaths))
	warnings := append([]string(nil), bitrateWarnings...)

	for _, rawInputPath := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return planFailure(plan, &models.JobError{Code: engine.ErrorCodeValidation, Message: "inputPaths must not contain empty values"})
		}
		if inputErr := validateAudioInputExtension(inputPath, spec.allowVideo); inputErr != nil {
			return planFailure(plan, inputErr)
		}

		var planned models.PlannedOutput
		var planErr *models.JobError
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
			planned = planAutoAudioOutput(inputPath, req.OutputDir, spec.suffix, format.Name, used)
		}

		planErr = mapAudioError(engine.ValidateRequest(engine.Request{
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
	plan.Message = fmt.Sprintf("Ready to %s %d %s.", spec.verb, len(outputs), pluralize("file", len(outputs)))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = map[string]any{
		"format":      format.Name,
		"bitrateKbps": bitrate,
	}

	return plan, nil
}

// executeConversionPlan runs the planned encodes sequentially. ffmpeg already
// uses every core, so one item at a time keeps the machine responsive while the
// audio class still allows a second independent job.
func executeConversionPlan(ctx context.Context, plan models.PlanJobResponse, onProgress func(models.JobProgress), spec planConversionSpec, probe engine.RuntimeProbe, runner ffmpegex.CommandRunner) ([]models.JobResultItem, *models.JobError) {
	formatName := optionString(plan.EffectiveOptions, "format")
	bitrate, _ := plan.EffectiveOptions["bitrateKbps"].(int)

	return executeAudioPlan(ctx, plan, onProgress, spec.stage, func(ctx context.Context, planned models.PlannedOutput, itemProgress func(float64, string)) (models.JobResultItem, *models.JobError) {
		convertErr := engine.Convert(ctx, probe, runner, engine.Request{
			InputPath:   planned.InputPath,
			OutputPath:  planned.OutputPath,
			Format:      formatName,
			BitrateKbps: bitrate,
		}, itemProgress)
		if convertErr != nil {
			return models.JobResultItem{}, mapAudioError(convertErr)
		}

		return successItem(planned, spec.successMessage), nil
	})
}

// executeAudioPlan is the shared loop: per-item failures are isolated, the
// first error is returned for the job state, and cancellation stops immediately.
func executeAudioPlan(ctx context.Context, plan models.PlanJobResponse, onProgress func(models.JobProgress), stage string, execute func(ctx context.Context, planned models.PlannedOutput, itemProgress func(float64, string)) (models.JobResultItem, *models.JobError)) ([]models.JobResultItem, *models.JobError) {
	items := make([]models.JobResultItem, 0, len(plan.Outputs))
	total := len(plan.Outputs)
	var firstErr *models.JobError

	for index, planned := range plan.Outputs {
		select {
		case <-ctx.Done():
			return items, &models.JobError{Code: "CANCELED", Message: ctx.Err().Error()}
		default:
		}

		var itemProgress func(float64, string)
		if onProgress != nil {
			itemProgress = func(percent float64, _ string) {
				overall := overallBatchPercent(index, total, percent)
				onProgress(models.JobProgress{
					Current: overall,
					Total:   jobProgressTotal,
					Stage:   stage,
					Message: audioProgressMessage(stage, index, total, overall),
				})
			}
		}

		item, jobErr := execute(ctx, planned, itemProgress)
		if jobErr != nil {
			if firstErr == nil {
				firstErr = jobErr
			}
			items = append(items, audioPlanFailureItem(planned.InputPath, planned.OutputPath, jobErr))
			continue
		}
		items = append(items, item)
	}

	return items, firstErr
}

func executeSingleWithPlan(ctx context.Context, req models.JobRequest, plan func(context.Context, models.JobRequest) (models.PlanJobResponse, *models.JobError), execute func(context.Context, models.JobRequest, models.PlanJobResponse, func(models.JobProgress)) ([]models.JobResultItem, *models.JobError), onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	planned, planErr := plan(ctx, req)
	if planErr != nil {
		return audioPlanFailureItem(firstInputPath(req.InputPaths), firstPlannedOutputPath(planned), planErr), planErr
	}

	items, execErr := execute(ctx, req, planned, onProgress)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = &models.JobError{Code: "EXECUTION_ERROR", Message: "audio execution returned no result"}
	}

	return audioPlanFailureItem(firstInputPath(req.InputPaths), firstPlannedOutputPath(planned), jobErr), jobErr
}

func successItem(planned models.PlannedOutput, message string) models.JobResultItem {
	return models.JobResultItem{
		InputPath:   planned.InputPath,
		OutputPath:  planned.OutputPath,
		Outputs:     []string{planned.OutputPath},
		OutputCount: 1,
		Success:     true,
		Message:     message,
	}
}

// overallBatchPercent maps one item's local percentage onto the whole plan so
// the drawer shows a single continuous bar. A single-file plan is the identity.
func overallBatchPercent(index, total int, itemPercent float64) int {
	if total <= 0 {
		return 0
	}

	clamped := itemPercent
	if math.IsNaN(clamped) || clamped < 0 {
		clamped = 0
	}
	if clamped > 100 {
		clamped = 100
	}

	return jobProgressPercent((float64(index)*100 + clamped) / float64(total))
}

func jobProgressPercent(percent float64) int {
	if math.IsNaN(percent) || percent <= 0 {
		return 0
	}
	if percent >= 100 {
		return 100
	}

	return int(math.Round(percent))
}

func audioProgressMessage(stage string, index, total, percent int) string {
	label := stage
	switch stage {
	case "audio.extract":
		label = "Extracting audio"
	case "audio.convert":
		label = "Converting audio"
	case "audio.normalize":
		label = "Normalizing audio"
	}
	if total <= 1 {
		return fmt.Sprintf("%s… %d%%", label, percent)
	}

	return fmt.Sprintf("%s item %d of %d… %d%%", label, index+1, total, percent)
}

func pluralize(word string, count int) string {
	if count == 1 {
		return word
	}

	return word + "s"
}

func firstInputPath(inputPaths []string) string {
	if len(inputPaths) == 0 {
		return ""
	}

	return strings.TrimSpace(inputPaths[0])
}

func firstPlannedOutputPath(plan models.PlanJobResponse) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
}

func planFailure(plan models.PlanJobResponse, jobErr *models.JobError) (models.PlanJobResponse, *models.JobError) {
	plan.Success = false
	plan.Error = jobErr
	if jobErr != nil && jobErr.Message != "" {
		plan.Message = jobErr.Message
	}

	return plan, jobErr
}

func audioPlanFailureItem(inputPath, outputPath string, jobErr *models.JobError) models.JobResultItem {
	item := models.JobResultItem{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Success:    false,
		Message:    jobErr.Message,
		Error:      jobErr,
	}
	if outputPath != "" {
		item.Outputs = []string{outputPath}
		item.OutputCount = 1
	}

	return item
}

func optionString(options map[string]any, key string) string {
	if options == nil {
		return ""
	}

	value, ok := options[key].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(value)
}

// optionNumberString renders a string or numeric option as text so enum
// presets accept both "podcast" and -16.
func optionNumberString(options map[string]any, key string) (string, bool) {
	if options == nil {
		return "", false
	}
	value, ok := options[key]
	if !ok || value == nil {
		return "", false
	}

	switch typed := value.(type) {
	case string:
		return strings.TrimSpace(typed), true
	case float64:
		return strconv.FormatFloat(typed, 'f', -1, 64), true
	case float32:
		return strconv.FormatFloat(float64(typed), 'f', -1, 64), true
	case int:
		return strconv.Itoa(typed), true
	case int32:
		return strconv.FormatInt(int64(typed), 10), true
	case int64:
		return strconv.FormatInt(typed, 10), true
	default:
		return "", false
	}
}

func audioOptionInt(options map[string]any, key string) (int, *models.JobError) {
	if !audioOptionSupplied(options, key) {
		return 0, nil
	}
	if floatValue, isFloat := options[key].(float64); isFloat && floatValue != float64(int(floatValue)) {
		return 0, &models.JobError{Code: engine.ErrorCodeBitrateInvalid, Message: fmt.Sprintf("options.%s must be an integer", key)}
	}

	switch value := options[key].(type) {
	case float64:
		return int(value), nil
	case float32:
		return int(value), nil
	case int:
		return value, nil
	case int32:
		return int(value), nil
	case int64:
		return int(value), nil
	default:
		return 0, &models.JobError{Code: engine.ErrorCodeBitrateInvalid, Message: fmt.Sprintf("options.%s must be numeric", key)}
	}
}

func audioOptionFloat(options map[string]any, key string, invalidCode string) (float64, bool, *models.JobError) {
	if !audioOptionSupplied(options, key) {
		return 0, false, nil
	}

	switch value := options[key].(type) {
	case float64:
		return value, true, nil
	case float32:
		return float64(value), true, nil
	case int:
		return float64(value), true, nil
	case int32:
		return float64(value), true, nil
	case int64:
		return float64(value), true, nil
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(value), 64)
		if err != nil {
			return 0, true, &models.JobError{Code: invalidCode, Message: fmt.Sprintf("options.%s must be numeric", key)}
		}
		return parsed, true, nil
	default:
		return 0, true, &models.JobError{Code: invalidCode, Message: fmt.Sprintf("options.%s must be numeric", key)}
	}
}

func audioOptionSupplied(options map[string]any, key string) bool {
	if options == nil {
		return false
	}
	value, ok := options[key]

	return ok && value != nil
}
