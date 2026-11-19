package image

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/toolchain"
)

const ToolIDImageBackgroundV1 = "tool.image.background"

const (
	imageBackgroundOutputExtension = "png"
	imageBackgroundOutputSuffix    = "_nobg"
	imageBackgroundAnimatedWarning = "animated GIF input: only the first frame is processed"

	// BackgroundRemoveRuntimeMissingReason and BackgroundRemoveModelMissingReason
	// are the user-facing degraded states. They always point at the only repair
	// path: the managed toolchain installer.
	BackgroundRemoveRuntimeMissingReason = "The background removal pack is not installed. Open Settings and run Install / repair."
	BackgroundRemoveModelMissingReason   = "The background removal model is not installed or is incomplete. Open Settings and run Install / repair."
)

// BackgroundRemoveTool removes image backgrounds locally with the opt-in rembg
// pack. The runner is injectable so tests can drive the exact process contract
// without a real installation.
type BackgroundRemoveTool struct {
	runner RembgCommandRunner
}

// backgroundRemoveRuntime is the resolved managed pack.
type backgroundRemoveRuntime struct {
	PythonPath string
	HomeDir    string
	ModelPath  string
	Model      string
}

// backgroundRemoveRequest is the parsed, normalized request.
type backgroundRemoveRequest struct {
	mode       string
	inputPaths []string
	outputDir  string
	options    BackgroundRemoveOptions
}

// preparedBackgroundRemove is one resolved input/output pair plus the runtime
// and options captured at planning time.
type preparedBackgroundRemove struct {
	inputPath  string
	outputPath string
	runtime    backgroundRemoveRuntime
	options    BackgroundRemoveOptions
}

// resolveBackgroundRemoveRuntime is a package variable so tests can simulate a
// missing pack without touching the real application data directory.
var resolveBackgroundRemoveRuntime = func() (backgroundRemoveRuntime, *BackgroundRemoveError) {
	dataDir, err := toolchain.DataDir()
	if err != nil {
		return backgroundRemoveRuntime{}, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeRuntimeUnavailable,
			Message: BackgroundRemoveRuntimeMissingReason,
			Cause:   err,
		}
	}

	return resolveBackgroundRemoveRuntimeAt(dataDir)
}

// runBackgroundRemove is the engine entry point used by the tool. Tests replace
// it to exercise planning, progress and error mapping without Python.
var runBackgroundRemove = RemoveBackgrounds

func NewBackgroundRemoveTool() *BackgroundRemoveTool {
	return &BackgroundRemoveTool{}
}

// NewBackgroundRemoveToolWithRunner injects the process runner. nil keeps the
// real exec-based runner used in production.
func NewBackgroundRemoveToolWithRunner(runner RembgCommandRunner) *BackgroundRemoveTool {
	return &BackgroundRemoveTool{runner: runner}
}

func (t *BackgroundRemoveTool) ID() string {
	return ToolIDImageBackgroundV1
}

func (t *BackgroundRemoveTool) Capability() string {
	return ToolIDImageBackgroundV1
}

func (t *BackgroundRemoveTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Remove Background",
		Description:      "Remove image backgrounds locally with a transparent PNG or a chosen background color",
		Domain:           "image",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  append([]string(nil), imageInputExtensions...),
		OutputExtensions: []string{imageBackgroundOutputExtension},
		RuntimeDeps:      []string{"rembg", "rembg-model"},
		Tags:             []string{"image", "background", "rembg"},
	}
}

func (t *BackgroundRemoveTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	if _, runtimeErr := resolveBackgroundRemoveRuntime(); runtimeErr != nil {
		return models.ToolRuntimeStateV1{
			Status:  "degraded",
			Healthy: false,
			Reason:  runtimeErr.Message,
		}
	}

	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *BackgroundRemoveTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan validates the pack, the inputs and the options, then resolves one
// collision-safe <stem>_nobg.png per input without running Python.
func (t *BackgroundRemoveTool) Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan, _, planErr := t.planBackgroundRemoveRequest(ctx, req)
	return plan, planErr
}

func (t *BackgroundRemoveTool) planBackgroundRemoveRequest(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, []preparedBackgroundRemove, *models.JobErrorV1) {
	parsed, parseErr := parseBackgroundRemoveRequest(req)
	if parseErr != nil {
		plan, _ := planFailure(models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}, parseErr)
		return plan, nil, parseErr
	}

	return t.planParsedBackgroundRemoveRequest(parsed)
}

// planParsedBackgroundRemoveRequest resolves the runtime once per job and builds
// every planned output plus the executor input.
func (t *BackgroundRemoveTool) planParsedBackgroundRemoveRequest(parsed backgroundRemoveRequest) (models.PlanJobResponseV1, []preparedBackgroundRemove, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	runtime, runtimeErr := resolveBackgroundRemoveRuntime()
	if runtimeErr != nil {
		jobErr := backgroundRemoveEngineJobError(runtimeErr, nil)
		failedPlan, _ := planFailure(plan, jobErr)

		return failedPlan, nil, jobErr
	}

	inputWarnings := make([]string, 0)
	for _, inputPath := range parsed.inputPaths {
		if inputErr := ValidateBackgroundRemoveInput(inputPath); inputErr != nil {
			jobErr := backgroundRemoveEngineJobError(inputErr, map[string]any{"inputPath": inputPath})
			failedPlan, _ := planFailure(plan, jobErr)

			return failedPlan, nil, jobErr
		}
		if compressInputFrameCount(inputPath) > 1 {
			inputWarnings = append(inputWarnings, fmt.Sprintf("%s: %s", filepath.Base(inputPath), imageBackgroundAnimatedWarning))
		}
	}

	// Resolve and write-probe every effective destination now, so the plan never
	// promises a folder the executor cannot publish into.
	targetDirs := make([]string, len(parsed.inputPaths))
	validatedDirs := make(map[string]struct{}, len(parsed.inputPaths))
	for index, inputPath := range parsed.inputPaths {
		targetDir := parsed.outputDir
		if targetDir == "" {
			targetDir = filepath.Dir(inputPath)
		}
		targetDir = filepath.Clean(targetDir)
		targetDirs[index] = targetDir

		if _, checked := validatedDirs[targetDir]; checked {
			continue
		}
		validatedDirs[targetDir] = struct{}{}

		if dirErr := validateRembgOutputDir(targetDir); dirErr != nil {
			jobErr := backgroundRemoveEngineJobError(dirErr, map[string]any{"outputDir": targetDir, "inputPath": inputPath})
			failedPlan, _ := planFailure(plan, jobErr)

			return failedPlan, nil, jobErr
		}
	}

	// Reserve every input path so an auto-derived output can never overwrite a
	// different input of the same batch.
	used := make(map[string]struct{}, len(parsed.inputPaths))
	for _, inputPath := range parsed.inputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	options := BackgroundRemoveOptions{
		Model:           runtime.Model,
		BackgroundColor: parsed.options.BackgroundColor,
	}

	outputs := make([]models.PlannedOutputV1, 0, len(parsed.inputPaths))
	prepared := make([]preparedBackgroundRemove, 0, len(parsed.inputPaths))
	warnings := make([]string, 0, len(parsed.inputPaths))

	for index, inputPath := range parsed.inputPaths {
		base := backgroundRemoveOutputBaseName(inputPath)
		outputPath, adjusted := outputplan.UniqueFilePath(targetDirs[index], base, imageBackgroundOutputExtension, used)

		output := models.PlannedOutputV1{
			InputPath:   inputPath,
			OutputPath:  outputPath,
			OutputKind:  imageOutputKindFile,
			OutputCount: 1,
		}
		if adjusted {
			output.Adjusted = true
			output.Warnings = append(output.Warnings, fmt.Sprintf(
				"%s is already in use. Saving as %s.",
				base+"."+imageBackgroundOutputExtension,
				filepath.Base(outputPath),
			))
		}

		outputs = append(outputs, output)
		warnings = append(warnings, output.Warnings...)
		prepared = append(prepared, preparedBackgroundRemove{
			inputPath:  inputPath,
			outputPath: outputPath,
			runtime:    runtime,
			options:    options,
		})
	}

	plan.Success = true
	plan.Message = backgroundRemovePlanMessage(len(prepared))
	plan.Outputs = outputs
	plan.Warnings = append(warnings, inputWarnings...)
	plan.EffectiveOptions = map[string]any{
		"backgroundColor": options.BackgroundColor,
	}

	return plan, prepared, nil
}

// ExecuteSingle implements tools.SingleExecutor by delegating to the progress
// variant with a no-op callback.
func (t *BackgroundRemoveTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

// ExecuteSingleWithProgress resolves the plan once and executes exactly that
// plan, mapping bridge progress to job progress.
func (t *BackgroundRemoveTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
	inputPath := firstInputPath(req.InputPaths)

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return imageFailureItem(inputPath, plannedOutputPath(plan), planErr), planErr
	}

	if onProgress != nil {
		onProgress(models.JobProgressV1{Current: 0, Total: 1, Stage: models.JobStatusRunning, Message: "removing background 1/1"})
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, onProgress)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("IMAGE_BACKGROUND_EXECUTION", "background removal returned no result", nil)
	}

	return imageFailureItem(inputPath, plannedOutputPath(plan), jobErr), jobErr
}

// ExecuteBatch plans the request once and then executes exactly that plan. It
// never re-resolves output paths from the request.
func (t *BackgroundRemoveTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, parseErr := parseBackgroundRemoveRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}
	if parsed.mode != "batch" {
		return nil, models.NewCanonicalJobError("IMAGE_BACKGROUND_MODE_INVALID", "mode must be batch", nil)
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan removes backgrounds for exactly the outputs resolved by Plan. The
// runtime is resolved once and the bridge runs once for the whole plan, so a
// reduced retry plan only processes the failed items.
func (t *BackgroundRemoveTool) ExecutePlan(ctx context.Context, req models.JobRequestV1, plan models.PlanJobResponseV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, parseErr := parseBackgroundRemoveRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}

	total := len(plan.Outputs)
	if total == 0 {
		return nil, models.NewCanonicalJobError("IMAGE_BACKGROUND_INPUT_INVALID", "Choose at least one image to process.", nil)
	}

	runtime, runtimeErr := resolveBackgroundRemoveRuntime()
	if runtimeErr != nil {
		return nil, backgroundRemoveEngineJobError(runtimeErr, nil)
	}

	backgroundColor := parsed.options.BackgroundColor
	if value, ok := plan.EffectiveOptions["backgroundColor"].(string); ok {
		backgroundColor = value
	}

	options := BackgroundRemoveOptions{
		Model:           runtime.Model,
		BackgroundColor: backgroundColor,
	}

	prepared := make([]preparedBackgroundRemove, total)
	for index, planned := range plan.Outputs {
		prepared[index] = preparedBackgroundRemove{
			inputPath:  planned.InputPath,
			outputPath: planned.OutputPath,
			runtime:    runtime,
			options:    options,
		}
	}

	return t.executePreparedBackgroundRemove(ctx, req.Mode, prepared, onProgress)
}

// executePreparedBackgroundRemove runs one bridge process for the prepared
// items and maps its outcomes, progress, cancellations and per-item failures.
func (t *BackgroundRemoveTool) executePreparedBackgroundRemove(ctx context.Context, mode string, prepared []preparedBackgroundRemove, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	detailCode := "IMAGE_BACKGROUND_BATCH_ITEM"
	if mode == "single" {
		detailCode = "IMAGE_BACKGROUND_EXECUTION"
	}

	items := make([]BackgroundRemoveItem, len(prepared))
	for index, item := range prepared {
		items[index] = BackgroundRemoveItem{InputPath: item.inputPath, OutputPath: item.outputPath}
	}

	outcomes, execErr := runBackgroundRemove(
		ctx,
		t.runner,
		prepared[0].runtime.PythonPath,
		prepared[0].runtime.ModelPath,
		"",
		items,
		prepared[0].options,
		func(current, total int) {
			if onProgress == nil {
				return
			}
			onProgress(models.JobProgressV1{
				Current: current,
				Total:   total,
				Stage:   models.JobStatusRunning,
				Message: fmt.Sprintf("processed %d/%d", current, total),
			})
		},
	)

	// The engine rolls published outputs back on cancellation, so every planned
	// item is reported as cancelled rather than as a retryable failure. A nil
	// error is a successful run even if the context was cancelled afterwards.
	if backgroundRemoveIsCancellation(execErr) {
		cancelled := backgroundRemoveCancelledJobError(ctx, execErr)
		results := make([]models.JobResultItemV1, 0, len(prepared))
		for _, item := range prepared {
			results = append(results, imageFailureItem(item.inputPath, item.outputPath, cancelled))
		}

		return results, cancelled
	}

	if execErr == nil && len(outcomes) == 0 && mode == "single" {
		item := prepared[0]
		jobErr := models.NewCanonicalJobError(
			BackgroundRemoveCodeProcessFailed,
			fmt.Sprintf("The background removal engine produced no result for %s.", filepath.Base(item.inputPath)),
			map[string]any{"inputPath": item.inputPath, "outputPath": item.outputPath},
		)
		return []models.JobResultItemV1{imageFailureItem(item.inputPath, item.outputPath, jobErr)}, jobErr
	}

	outcomeByPath := make(map[string]BackgroundRemoveOutcome, len(outcomes))
	for _, outcome := range outcomes {
		outcomeByPath[rembgPathKey(outcome.OutputPath)] = outcome
	}

	failures := make(map[int]BackgroundRemoveItemFailure)
	var engineErr *BackgroundRemoveError
	if execErr != nil {
		errors.As(execErr, &engineErr)
	}
	var failureDetails map[string]any
	if engineErr != nil {
		failureDetails = engineErr.Details
		for _, failure := range engineErr.Failures {
			failures[failure.Index] = failure
		}
	}

	results := make([]models.JobResultItemV1, 0, len(prepared))
	var firstErr *models.JobErrorV1

	for index, item := range prepared {
		if failure, failed := failures[index]; failed {
			jobErr := backgroundRemoveFailureJobError(failure, item.inputPath, item.outputPath, failureDetails)
			if firstErr == nil {
				firstErr = jobErr
			}
			results = append(results, imageFailureItem(item.inputPath, item.outputPath, jobErr))
			continue
		}

		if outcome, ok := outcomeByPath[rembgPathKey(item.outputPath)]; ok {
			results = append(results, backgroundRemoveSuccessItem(item, outcome))
			continue
		}

		jobErr := backgroundRemoveExecutionError(ctx, detailCode, item, execErr)
		if firstErr == nil {
			firstErr = jobErr
		}
		results = append(results, imageFailureItem(item.inputPath, item.outputPath, jobErr))
	}

	return results, firstErr
}

func parseBackgroundRemoveRequest(req models.JobRequestV1) (backgroundRemoveRequest, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return backgroundRemoveRequest{}, models.NewCanonicalJobError("IMAGE_BACKGROUND_MODE_INVALID", "mode must be single or batch", nil)
	}

	if len(req.InputPaths) == 0 {
		return backgroundRemoveRequest{}, models.NewCanonicalJobError("IMAGE_BACKGROUND_INPUT_INVALID", "Choose at least one image to process.", nil)
	}
	if mode == "single" && len(req.InputPaths) != 1 {
		return backgroundRemoveRequest{}, models.NewCanonicalJobError("IMAGE_BACKGROUND_SINGLE_INPUT_INVALID", "single mode requires exactly one input", nil)
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawPath := range req.InputPaths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			return backgroundRemoveRequest{}, models.NewCanonicalJobError("IMAGE_BACKGROUND_INPUT_INVALID", "inputPaths contains empty item", nil)
		}
		inputPaths = append(inputPaths, trimmed)
	}

	backgroundColor, colorErr := backgroundRemoveColorOption(req.Options)
	if colorErr != nil {
		return backgroundRemoveRequest{}, colorErr
	}

	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = convertOptionString(req.Options, "outputDir")
	}

	return backgroundRemoveRequest{
		mode:       mode,
		inputPaths: inputPaths,
		outputDir:  outputDir,
		options: BackgroundRemoveOptions{
			Model:           toolchain.DefaultRembgModel,
			BackgroundColor: backgroundColor,
		},
	}, nil
}

// backgroundRemoveColorOption validates the optional #RRGGBB color. An absent or
// empty value means transparent output.
func backgroundRemoveColorOption(options map[string]any) (string, *models.JobErrorV1) {
	if !optionSupplied(options, "backgroundColor") {
		return "", nil
	}

	raw, ok := options["backgroundColor"].(string)
	if !ok {
		return "", models.NewCanonicalJobError(
			BackgroundRemoveCodeInvalidOption,
			"options.backgroundColor must be a #RRGGBB color",
			nil,
		)
	}

	normalized, _, valid := normalizeBackgroundColor(raw)
	if !valid {
		return "", models.NewCanonicalJobError(
			BackgroundRemoveCodeInvalidOption,
			"options.backgroundColor must be a #RRGGBB color",
			map[string]any{"backgroundColor": raw},
		)
	}

	return normalized, nil
}

// resolveBackgroundRemoveRuntimeAt resolves the managed pack inside one data
// directory. It only stats files; it never starts Python.
func resolveBackgroundRemoveRuntimeAt(dataDir string) (backgroundRemoveRuntime, *BackgroundRemoveError) {
	pythonPath, pythonOK := toolchain.ManagedRembgPythonPath(dataDir)
	if !pythonOK {
		return backgroundRemoveRuntime{}, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeRuntimeUnavailable,
			Message: BackgroundRemoveRuntimeMissingReason,
			Details: map[string]any{"pythonPath": pythonPath},
		}
	}

	spec := toolchain.DefaultRembgModelSpec()
	modelPath, modelOK := toolchain.ManagedRembgModelPath(dataDir, spec.Model)
	if !modelOK {
		return backgroundRemoveRuntime{}, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeModelUnavailable,
			Message: BackgroundRemoveModelMissingReason,
			Details: map[string]any{"model": spec.Model, "modelPath": modelPath},
		}
	}

	modelInfo, statErr := os.Stat(modelPath)
	if statErr != nil || !modelInfo.Mode().IsRegular() || modelInfo.Size() != spec.SizeBytes {
		var size int64
		if statErr == nil {
			size = modelInfo.Size()
		}

		return backgroundRemoveRuntime{}, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeModelUnavailable,
			Message: BackgroundRemoveModelMissingReason,
			Details: map[string]any{
				"model":             spec.Model,
				"modelPath":         modelPath,
				"sizeBytes":         size,
				"expectedSizeBytes": spec.SizeBytes,
			},
			Cause: statErr,
		}
	}

	homeDir, homeOK := toolchain.ManagedRembgHome(dataDir)
	if !homeOK {
		return backgroundRemoveRuntime{}, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeRuntimeUnavailable,
			Message: BackgroundRemoveRuntimeMissingReason,
			Details: map[string]any{"rembgHome": homeDir},
		}
	}

	// The managed engine is more than an interpreter and a model: require the
	// installed packages and the post-import marker so a half-repaired venv is
	// rejected before Python starts.
	if ready, reason := toolchain.ManagedRembgRuntimeState(dataDir); !ready {
		return backgroundRemoveRuntime{}, &BackgroundRemoveError{
			Code:    BackgroundRemoveCodeRuntimeUnavailable,
			Message: reason,
			Details: map[string]any{"pythonPath": pythonPath, "rembgHome": homeDir},
		}
	}

	return backgroundRemoveRuntime{
		PythonPath: pythonPath,
		HomeDir:    homeDir,
		ModelPath:  modelPath,
		Model:      spec.Model,
	}, nil
}

func backgroundRemoveOutputBaseName(inputPath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		stem = "image"
	}

	return stem + imageBackgroundOutputSuffix
}

func backgroundRemovePlanMessage(count int) string {
	if count == 1 {
		return "Ready to remove the background of 1 image."
	}

	return fmt.Sprintf("Ready to remove the background of %d images.", count)
}

func backgroundRemoveSuccessItem(item preparedBackgroundRemove, outcome BackgroundRemoveOutcome) models.JobResultItemV1 {
	outputPath := item.outputPath
	if strings.TrimSpace(outcome.OutputPath) != "" {
		outputPath = outcome.OutputPath
	}

	return models.JobResultItemV1{
		InputPath:   item.inputPath,
		OutputPath:  outputPath,
		Outputs:     []string{outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     backgroundRemoveResultMessage(outcome, item.options.BackgroundColor),
	}
}

// backgroundRemoveResultMessage reports exactly what was produced.
func backgroundRemoveResultMessage(outcome BackgroundRemoveOutcome, background string) string {
	if strings.TrimSpace(background) == "" {
		return fmt.Sprintf("transparent PNG %d×%d", outcome.Width, outcome.Height)
	}

	return fmt.Sprintf("background %s, %d×%d", background, outcome.Width, outcome.Height)
}

// backgroundRemoveEngineJobError maps a typed engine failure to a canonical job
// error. The engine message stays user-facing; the raw cause only reaches
// Details.
func backgroundRemoveEngineJobError(rembgErr *BackgroundRemoveError, extraDetails map[string]any) *models.JobErrorV1 {
	details := make(map[string]any, len(extraDetails)+2)
	for key, value := range extraDetails {
		details[key] = value
	}
	for key, value := range rembgErr.Details {
		details[key] = value
	}
	if rembgErr.Cause != nil {
		details["error"] = rembgErr.Cause.Error()
	}

	switch rembgErr.Code {
	case BackgroundRemoveCodeRuntimeUnavailable, BackgroundRemoveCodeModelUnavailable:
		return models.NewJobError(models.ErrorCodeRuntimeDepMissing, rembgErr.Code, rembgErr.Message, details)
	case BackgroundRemoveCodeInputUnsupported:
		return models.NewJobError(models.ErrorCodeUnsupportedFormat, rembgErr.Code, rembgErr.Message, details)
	case BackgroundRemoveCodeCancelled:
		return models.NewJobError(models.ErrorCodeCancelledByUser, rembgErr.Code, rembgErr.Message, details)
	case BackgroundRemoveCodeInvalidInput,
		BackgroundRemoveCodeInputNotFound,
		BackgroundRemoveCodeInvalidOption,
		BackgroundRemoveCodeOutputInvalid,
		BackgroundRemoveCodeOutputExists:
		return models.NewJobError(models.ErrorCodeValidationInvalidInput, rembgErr.Code, rembgErr.Message, details)
	default:
		return models.NewJobError(models.ErrorCodeExecIOTransient, rembgErr.Code, rembgErr.Message, details)
	}
}

func backgroundRemoveFailureJobError(failure BackgroundRemoveItemFailure, inputPath, outputPath string, extraDetails map[string]any) *models.JobErrorV1 {
	if strings.TrimSpace(inputPath) == "" {
		inputPath = failure.InputPath
	}
	if strings.TrimSpace(outputPath) == "" {
		outputPath = failure.OutputPath
	}

	details := make(map[string]any, len(extraDetails)+3)
	for key, value := range extraDetails {
		details[key] = value
	}
	details["index"] = failure.Index
	details["inputPath"] = inputPath
	details["outputPath"] = outputPath
	if failure.Cause != nil {
		details["error"] = failure.Cause.Error()
	}

	return backgroundRemoveEngineJobError(&BackgroundRemoveError{
		Code:    failure.Code,
		Message: failure.Message,
	}, details)
}

// backgroundRemoveExecutionError maps a failed execution to a job error.
// Cancellation wins over the generic code, including the last batch item.
func backgroundRemoveExecutionError(ctx context.Context, detailCode string, item preparedBackgroundRemove, execErr error) *models.JobErrorV1 {
	if backgroundRemoveIsCancellation(execErr) {
		return backgroundRemoveCancelledJobError(ctx, execErr)
	}

	var rembgErr *BackgroundRemoveError
	if errors.As(execErr, &rembgErr) {
		if len(rembgErr.Failures) == 1 {
			return backgroundRemoveFailureJobError(rembgErr.Failures[0], item.inputPath, item.outputPath, rembgErr.Details)
		}

		return backgroundRemoveEngineJobError(rembgErr, map[string]any{
			"inputPath":  item.inputPath,
			"outputPath": item.outputPath,
		})
	}

	details := map[string]any{"inputPath": item.inputPath, "outputPath": item.outputPath}
	if execErr != nil {
		details["error"] = execErr.Error()
	}

	return models.NewCanonicalJobError(detailCode, "Could not remove the background.", details)
}

// backgroundRemoveIsCancellation classifies only an error returned by the
// engine. A nil error is a successful run even when ctx was cancelled late; an
// ambient cancel must not turn validated output into a cancelled job.
func backgroundRemoveIsCancellation(execErr error) bool {
	if execErr == nil {
		return false
	}

	var rembgErr *BackgroundRemoveError
	if errors.As(execErr, &rembgErr) && rembgErr.Code == BackgroundRemoveCodeCancelled {
		return true
	}

	return errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded)
}

func backgroundRemoveCancelledJobError(ctx context.Context, execErr error) *models.JobErrorV1 {
	var cause error = context.Canceled
	if ctx != nil && ctx.Err() != nil {
		cause = ctx.Err()
	}

	var details map[string]any
	var rembgErr *BackgroundRemoveError
	if errors.As(execErr, &rembgErr) {
		if len(rembgErr.Details) > 0 {
			details = make(map[string]any, len(rembgErr.Details))
			for key, value := range rembgErr.Details {
				details[key] = value
			}
		}
		if cause == context.Canceled && rembgErr.Cause != nil {
			cause = rembgErr.Cause
		}
	}

	message := fmt.Errorf(OperationCancelledMsg, cause).Error()
	return models.NewCanonicalJobError(BackgroundRemoveCodeCancelled, message, details)
}
