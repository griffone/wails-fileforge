package video

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/jobs"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/video/engine"
)

const (
	// ToolIDVideoTranscribeV1 is the local Whisper transcription tool.
	ToolIDVideoTranscribeV1 = "tool.video.transcribe"

	transcribeOutputKindFile = "file"
)

// TranscribeTool turns video/audio into SRT/VTT/TXT with the local whisper.cpp
// runtime. The runner and the capability detector are injectable so tests can
// simulate missing dependencies without touching the machine.
type TranscribeTool struct {
	runner       engine.TranscribeCommandRunner
	capabilities func(whisperPath, modelPath string) engine.TranscriptionCapabilities
}

func NewTranscribeTool() *TranscribeTool {
	return &TranscribeTool{}
}

func NewTranscribeToolWithDeps(runner engine.TranscribeCommandRunner, capabilities func(whisperPath, modelPath string) engine.TranscriptionCapabilities) *TranscribeTool {
	return &TranscribeTool{runner: runner, capabilities: capabilities}
}

func (t *TranscribeTool) ID() string {
	return ToolIDVideoTranscribeV1
}

func (t *TranscribeTool) Capability() string {
	return ToolIDVideoTranscribeV1
}

func (t *TranscribeTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Video Transcribe",
		Description:      "Transcribe video and audio locally with Whisper",
		Domain:           "video",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  engine.SupportedTranscriptionInputExtensions(),
		OutputExtensions: engine.SupportedTranscriptionFormats(),
		RuntimeDeps:      []string{"whisper-cli", "ffmpeg"},
		Tags:             []string{"video", "audio", "transcribe", "whisper", "subtitles"},
	}
}

func (t *TranscribeTool) RuntimeState(ctx context.Context) models.ToolRuntimeStateV1 {
	capabilities := t.detectCapabilities("", "")
	if capabilities.Available() {
		return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
	}

	return models.ToolRuntimeStateV1{
		Status:  "degraded",
		Healthy: false,
		Reason:  TranscriptionUnavailableReason(capabilities),
	}
}

func (t *TranscribeTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan validates the Whisper runtime, the language, the formats and the inputs,
// then resolves one output per requested format without writing anything.
// Outputs default to <stem>.<format> next to each input and are collision-safe.
func (t *TranscribeTool) Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	fields, fieldsErr := transcribeReqFields(req)
	if fieldsErr != nil {
		return planFailure(plan, fieldsErr)
	}

	capabilities := t.detectCapabilities(fields.WhisperPath, fields.ModelPath)
	if !capabilities.Available() {
		detailCode := engine.ErrorCodeTranscribeRuntimeUnavailable
		if capabilities.WhisperPresent && !capabilities.ModelPresent {
			detailCode = engine.ErrorCodeTranscribeModelMissing
		}

		return planFailure(plan, models.NewJobError(
			models.ErrorCodeRuntimeDepMissing,
			detailCode,
			TranscriptionUnavailableReason(capabilities),
			map[string]any{
				"whisperPath":    capabilities.WhisperPath,
				"whisperPresent": capabilities.WhisperPresent,
				"modelPath":      capabilities.ModelPath,
				"modelPresent":   capabilities.ModelPresent,
			},
		))
	}

	if fields.OutputDir != "" {
		if dirErr := engine.ValidateTranscribeDestination(fields.OutputDir); dirErr != nil {
			return planFailure(plan, mapTranscriptionError(dirErr))
		}
	}

	// Reserve every input path so an auto-derived output can never overwrite a
	// different input of the same batch.
	used := reserveInputPaths(fields.InputPaths)
	outputs := make([]models.PlannedOutputV1, 0, len(fields.InputPaths)*len(fields.Formats))
	warnings := make([]string, 0, len(fields.InputPaths)*len(fields.Formats))

	for _, inputPath := range fields.InputPaths {
		if validationErr := engine.ValidateTranscribeInput(inputPath); validationErr != nil {
			return planFailure(plan, mapTranscriptionError(validationErr))
		}

		targetDir := fields.OutputDir
		if targetDir == "" {
			targetDir = filepath.Dir(inputPath)
		}
		targetDir = filepath.Clean(targetDir)

		stem := transcriptionBaseName(inputPath)
		for _, format := range fields.Formats {
			naturalPath := filepath.Join(targetDir, fmt.Sprintf("%s.%s", stem, format))
			outputPath, adjusted := outputplan.UniqueFilePath(targetDir, stem, format, used)

			planned := models.PlannedOutputV1{
				InputPath:   inputPath,
				OutputPath:  outputPath,
				OutputKind:  transcribeOutputKindFile,
				OutputCount: 1,
			}
			if adjusted {
				planned.Adjusted = true
				planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", filepath.Base(naturalPath), filepath.Base(outputPath)))
			}

			outputs = append(outputs, planned)
			warnings = append(warnings, planned.Warnings...)
		}
	}

	plan.Success = true
	plan.Message = transcribePlanMessage(len(fields.InputPaths))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = map[string]any{
		"language":    fields.Language,
		"formats":     fields.Formats,
		"whisperPath": capabilities.WhisperPath,
		"modelPath":   capabilities.ModelPath,
	}

	return plan, nil
}

// ExecuteSingle implements tools.SingleExecutor by delegating to the progress
// variant with a no-op callback.
func (t *TranscribeTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

// ExecuteSingleWithProgress transcribes one media file and reports the coarse
// extract/transcribe/publish stages.
func (t *TranscribeTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
	fields, fieldsErr := transcribeReqFields(req)
	if fieldsErr != nil {
		return transcribeFailureItem(firstInputPath(req.InputPaths), nil, fieldsErr), fieldsErr
	}

	if fields.Mode != "single" {
		jobErr := transcriptionValidationError("mode must be single")
		return transcribeFailureItem(fields.InputPaths[0], nil, jobErr), jobErr
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return transcribeFailureItem(fields.InputPaths[0], plannedOutputPaths(plan.Outputs), planErr), planErr
	}

	jobs, groupErr := groupTranscribeOutputs(plan.Outputs, transcriptionFormatsFromOptions(plan.EffectiveOptions))
	if groupErr != nil {
		return transcribeFailureItem(fields.InputPaths[0], plannedOutputPaths(plan.Outputs), groupErr), groupErr
	}

	item := jobs[0]
	result, transcribeErr := t.runTranscribe(ctx, item, plan.EffectiveOptions, onProgress)
	if transcribeErr != nil {
		jobErr := mapTranscriptionError(transcribeErr)
		return transcribeFailureItem(item.InputPath, item.OrderedOutputs, jobErr), jobErr
	}

	successItem := transcribeSuccessItem(item, result)
	if onProgress != nil {
		onProgress(models.JobProgressV1{Current: 100, Total: 100, Stage: "transcribe.done", Message: successItem.Message})
	}

	return successItem, nil
}

// ExecuteBatch transcribes every input with per-item progress and per-item
// errors, mirroring the other video batch tools.
func (t *TranscribeTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	fields, fieldsErr := transcribeReqFields(req)
	if fieldsErr != nil {
		return nil, fieldsErr
	}

	if fields.Mode != "batch" {
		return nil, transcriptionValidationError("mode must be batch")
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	plannedJobs, groupErr := groupTranscribeOutputs(plan.Outputs, transcriptionFormatsFromOptions(plan.EffectiveOptions))
	if groupErr != nil {
		return nil, groupErr
	}

	items := make([]models.JobResultItemV1, 0, len(plannedJobs))
	var firstErr *models.JobErrorV1

	for index, job := range plannedJobs {
		select {
		case <-ctx.Done():
			return items, &models.JobErrorV1{Code: "CANCELED", Message: ctx.Err().Error()}
		default:
		}

		result, transcribeErr := t.runTranscribe(ctx, job, plan.EffectiveOptions, nil)
		if transcribeErr != nil {
			jobErr := mapTranscriptionError(transcribeErr)
			if firstErr == nil {
				firstErr = jobErr
			}
			items = append(items, transcribeFailureItem(job.InputPath, job.OrderedOutputs, jobErr))
		} else {
			items = append(items, transcribeSuccessItem(job, result))
		}

		if onProgress != nil {
			onProgress(models.JobProgressV1{
				Current: index + 1,
				Total:   len(plannedJobs),
				Stage:   jobs.StatusRunning,
				Message: fmt.Sprintf("processed %d/%d", index+1, len(plannedJobs)),
			})
		}
	}

	if firstErr != nil {
		return items, firstErr
	}

	return items, nil
}

// transcribePlannedJob is one resolved input plus its format -> final path map.
type transcribePlannedJob struct {
	InputPath      string
	OutputPaths    map[string]string
	OrderedOutputs []string
}

func (j transcribePlannedJob) firstOutputPath() string {
	if len(j.OrderedOutputs) == 0 {
		return ""
	}

	return j.OrderedOutputs[0]
}

func (t *TranscribeTool) runTranscribe(ctx context.Context, job transcribePlannedJob, effectiveOptions map[string]any, onProgress func(models.JobProgressV1)) (engine.TranscribeResult, *engine.TranscriptionError) {
	progress := func(event engine.TranscribeProgressEvent) {
		if onProgress == nil {
			return
		}
		onProgress(jobProgressFromTranscription(event))
	}

	return engine.Transcribe(ctx, t.runner, engine.TranscribeRequest{
		InputPath:   job.InputPath,
		OutputPaths: job.OutputPaths,
		WhisperPath: optionString(effectiveOptions, "whisperPath"),
		ModelPath:   optionString(effectiveOptions, "modelPath"),
		Language:    optionString(effectiveOptions, "language"),
	}, progress)
}

func transcribeSuccessItem(job transcribePlannedJob, result engine.TranscribeResult) models.JobResultItemV1 {
	message := fmt.Sprintf("Transcribed: %d segments, %d characters.", result.Segments, result.TextCharacters)
	if len(result.Warnings) > 0 {
		message += " " + strings.Join(result.Warnings, " ")
	}

	return models.JobResultItemV1{
		InputPath:   job.InputPath,
		OutputPath:  job.firstOutputPath(),
		Outputs:     job.OrderedOutputs,
		OutputCount: len(job.OrderedOutputs),
		Success:     true,
		Message:     message,
	}
}

func transcribeFailureItem(inputPath string, outputs []string, jobErr *models.JobErrorV1) models.JobResultItemV1 {
	item := models.JobResultItemV1{
		InputPath: inputPath,
		Success:   false,
		Message:   jobErr.Message,
		Error:     jobErr,
	}
	if len(outputs) > 0 {
		item.OutputPath = outputs[0]
		item.Outputs = outputs
		item.OutputCount = len(outputs)
	}

	return item
}

func jobProgressFromTranscription(event engine.TranscribeProgressEvent) models.JobProgressV1 {
	percent := event.Percent
	if percent < 0 {
		percent = 0
	}
	if percent > 100 {
		percent = 100
	}

	message := strings.TrimSpace(event.Message)
	if message == "" {
		message = event.Stage
	}
	stage := strings.TrimSpace(event.Stage)
	if stage == "" {
		stage = jobs.StatusRunning
	}

	return models.JobProgressV1{Current: percent, Total: 100, Stage: stage, Message: message}
}

// groupTranscribeOutputs pairs the flat planned outputs with their source
// paths. Every input contributes one output per requested format in the same
// canonical order, so the stride is the format count.
func groupTranscribeOutputs(outputs []models.PlannedOutputV1, formats []string) ([]transcribePlannedJob, *models.JobErrorV1) {
	if len(formats) == 0 {
		return nil, transcriptionValidationError("the plan resolved no output formats")
	}
	if len(outputs) == 0 || len(outputs)%len(formats) != 0 {
		return nil, transcriptionValidationError("the plan resolved an unexpected number of outputs")
	}

	jobs := make([]transcribePlannedJob, 0, len(outputs)/len(formats))
	for index := 0; index < len(outputs); index += len(formats) {
		inputPath := outputs[index].InputPath
		paths := make(map[string]string, len(formats))
		ordered := make([]string, 0, len(formats))

		for offset, format := range formats {
			planned := outputs[index+offset]
			if planned.InputPath != inputPath {
				return nil, transcriptionValidationError("the plan mixed inputs in one output group")
			}
			if extension := strings.TrimPrefix(strings.ToLower(filepath.Ext(planned.OutputPath)), "."); extension != format {
				return nil, transcriptionValidationError("the plan resolved an unexpected output order")
			}

			paths[format] = planned.OutputPath
			ordered = append(ordered, planned.OutputPath)
		}

		jobs = append(jobs, transcribePlannedJob{InputPath: inputPath, OutputPaths: paths, OrderedOutputs: ordered})
	}

	return jobs, nil
}

// transcribeRequestFields is the canonical request form used by planning and
// execution.
type transcribeRequestFields struct {
	Mode        string
	InputPaths  []string
	OutputDir   string
	Language    string
	Formats     []string
	WhisperPath string
	ModelPath   string
}

func transcribeReqFields(req models.JobRequestV1) (transcribeRequestFields, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return transcribeRequestFields{}, transcriptionValidationError("mode must be single or batch")
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return transcribeRequestFields{}, transcriptionValidationError("exactly 1 input file is required")
	}
	if mode == "batch" && len(req.InputPaths) < 1 {
		return transcribeRequestFields{}, transcriptionValidationError("at least 1 input file is required")
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawInput := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return transcribeRequestFields{}, transcriptionValidationError("inputPaths must not contain empty values")
		}
		inputPaths = append(inputPaths, inputPath)
	}

	// The chosen folder is optional: without it every output is written next to
	// its input. req.OutputDir is the current field; options.outputDir stays
	// supported for older clients.
	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = optionString(req.Options, "outputDir")
	}
	if outputDir != "" {
		outputDir = filepath.Clean(outputDir)
	}

	language, languageErr := engine.NormalizeTranscriptionLanguage(optionString(req.Options, "language"))
	if languageErr != nil {
		return transcribeRequestFields{}, transcriptionValidationError("options.language must be auto, en, or es")
	}

	formats, formatsErr := transcribeOptionFormats(req.Options)
	if formatsErr != nil {
		return transcribeRequestFields{}, formatsErr
	}

	return transcribeRequestFields{
		Mode:        mode,
		InputPaths:  inputPaths,
		OutputDir:   outputDir,
		Language:    language,
		Formats:     formats,
		WhisperPath: optionString(req.Options, "whisperPath"),
		ModelPath:   optionString(req.Options, "modelPath"),
	}, nil
}

// transcribeOptionFormats parses options.formats. Absent means every format;
// []string and []any of strings are accepted, and the result is deduped and
// returned in canonical SRT/VTT/TXT order.
func transcribeOptionFormats(options map[string]any) ([]string, *models.JobErrorV1) {
	raw, supplied := options["formats"]
	if options == nil || !supplied || raw == nil {
		return engine.SupportedTranscriptionFormats(), nil
	}

	values := make([]string, 0, len(engine.SupportedTranscriptionFormats()))
	switch typed := raw.(type) {
	case []string:
		values = append(values, typed...)
	case []any:
		for index, item := range typed {
			text, ok := item.(string)
			if !ok {
				return nil, transcriptionValidationError(fmt.Sprintf("options.formats[%d] must be a string", index))
			}
			values = append(values, text)
		}
	default:
		return nil, transcriptionValidationError("options.formats must be a list of strings")
	}

	requested := make(map[string]struct{}, len(values))
	for _, value := range values {
		format := strings.ToLower(strings.TrimSpace(value))
		switch format {
		case engine.TranscribeFormatSRT, engine.TranscribeFormatVTT, engine.TranscribeFormatTXT:
			requested[format] = struct{}{}
		default:
			return nil, transcriptionValidationError("options.formats must contain only srt, vtt, or txt")
		}
	}
	if len(requested) == 0 {
		return nil, transcriptionValidationError("options.formats must include at least one of srt, vtt, txt")
	}

	formats := make([]string, 0, len(requested))
	for _, format := range engine.SupportedTranscriptionFormats() {
		if _, ok := requested[format]; ok {
			formats = append(formats, format)
		}
	}

	return formats, nil
}

// transcriptionFormatsFromOptions reads the planned formats. The plan stores
// []string; []any is accepted after a JSON round-trip.
func transcriptionFormatsFromOptions(options map[string]any) []string {
	raw, ok := options["formats"]
	if !ok || raw == nil {
		return engine.SupportedTranscriptionFormats()
	}

	values := make([]string, 0, len(engine.SupportedTranscriptionFormats()))
	switch typed := raw.(type) {
	case []string:
		values = append(values, typed...)
	case []any:
		for _, item := range typed {
			if text, isString := item.(string); isString {
				values = append(values, text)
			}
		}
	}

	requested := make(map[string]struct{}, len(values))
	for _, value := range values {
		requested[strings.ToLower(strings.TrimSpace(value))] = struct{}{}
	}

	formats := make([]string, 0, len(requested))
	for _, format := range engine.SupportedTranscriptionFormats() {
		if _, ok := requested[format]; ok {
			formats = append(formats, format)
		}
	}

	return formats
}

func transcriptionBaseName(inputPath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		stem = "media"
	}

	return stem
}

func transcribePlanMessage(inputCount int) string {
	if inputCount == 1 {
		return "Ready to transcribe 1 file."
	}

	return fmt.Sprintf("Ready to transcribe %d files.", inputCount)
}

func plannedOutputPaths(outputs []models.PlannedOutputV1) []string {
	paths := make([]string, 0, len(outputs))
	for _, output := range outputs {
		paths = append(paths, output.OutputPath)
	}

	return paths
}

// TranscriptionUnavailableReason explains why local transcription cannot run,
// in the order the user has to fix it: missing binary, missing model.
func TranscriptionUnavailableReason(capabilities engine.TranscriptionCapabilities) string {
	switch {
	case !capabilities.WhisperPresent && !capabilities.ModelPresent:
		return fmt.Sprintf(
			"Local transcription is unavailable: whisper-cli and a Whisper model were not found. Install whisper.cpp and a ggml model, or choose both in Settings (expected at %s and %s).",
			capabilities.WhisperPath,
			capabilities.ModelPath,
		)
	case !capabilities.WhisperPresent:
		return "Local transcription is unavailable: " + engine.TranscriptionWhisperMissingMessage(capabilities)
	default:
		return "Local transcription is unavailable: " + engine.TranscriptionModelMissingMessage(capabilities)
	}
}

// TranscriptionCapabilitiesV1 reports the local Whisper state for the app
// capability endpoint. Empty paths resolve the installed defaults.
func TranscriptionCapabilitiesV1(whisperPath, modelPath string) models.TranscriptionCapabilitiesResponseV1 {
	capabilities := engine.DetectTranscriptionCapabilities(whisperPath, modelPath)

	message := TranscriptionUnavailableReason(capabilities)
	if capabilities.Available() {
		message = fmt.Sprintf("Local transcription is available with %s.", capabilities.ModelName)
	}

	languages := capabilities.Languages
	if languages == nil {
		languages = []string{}
	}

	return models.TranscriptionCapabilitiesResponseV1{
		Success:         true,
		Message:         message,
		Available:       capabilities.Available(),
		WhisperPath:     capabilities.WhisperPath,
		WhisperPresent:  capabilities.WhisperPresent,
		ModelPath:       capabilities.ModelPath,
		ModelPresent:    capabilities.ModelPresent,
		ModelName:       capabilities.ModelName,
		ModelSizeBytes:  capabilities.ModelSizeBytes,
		Languages:       languages,
		DefaultLanguage: capabilities.DefaultLanguage,
	}
}

func mapTranscriptionError(err error) *models.JobErrorV1 {
	if err == nil {
		return nil
	}

	var transcribeErr *engine.TranscriptionError
	if !errors.As(err, &transcribeErr) {
		return &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}
	if transcribeErr == nil {
		// A typed nil *TranscriptionError wrapped in an error interface is a
		// success.
		return nil
	}

	switch transcribeErr.Code {
	case engine.ErrorCodeTranscribeRuntimeUnavailable, engine.ErrorCodeTranscribeModelMissing:
		return models.NewJobError(models.ErrorCodeRuntimeDepMissing, transcribeErr.Code, transcribeErr.Message, nil)
	case engine.ErrorCodeTranscribeValidation:
		return models.NewJobError(models.ErrorCodeValidationInvalidInput, transcribeErr.Code, transcribeErr.Message, nil)
	default:
		return models.NewCanonicalJobError(transcribeErr.Code, transcribeErr.Message, nil)
	}
}

func transcriptionValidationError(message string) *models.JobErrorV1 {
	return models.NewJobError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeTranscribeValidation, message, nil)
}

func (t *TranscribeTool) detectCapabilities(whisperPath, modelPath string) engine.TranscriptionCapabilities {
	if t.capabilities != nil {
		return t.capabilities(whisperPath, modelPath)
	}

	return engine.DetectTranscriptionCapabilities(whisperPath, modelPath)
}
