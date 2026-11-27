package image

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/toolchain"
)

const ToolIDImageUpscale = "tool.image.upscale"

const (
	imageUpscaleAnimatedWarning = "animated input: only the first frame is upscaled"
	imageUpscalePNGQualityNote  = "quality applies to JPEG and WebP; PNG is always lossless"
	imageUpscaleFixedTileNote   = "the bundled general model runs on fixed 128 px tiles; tileSize applies to custom models"

	// UpscaleRuntimeMissingReason and UpscaleModelMissingReason are the
	// user-facing degraded states. They always point at the only repair path:
	// the managed toolchain installer.
	UpscaleRuntimeMissingReason    = "The upscale pack is not installed. Open Settings and run Install / repair."
	UpscaleModelMissingReason      = "The upscale model is not installed or is incomplete. Open Settings and run Install / repair."
	UpscaleAnimeModelMissingReason = "The anime model is not configured. Choose a RealESRGAN x4 anime ONNX file in Settings."
)

// UpscaleTool upscales images locally with the opt-in ONNX pack. The runner is
// injectable so tests can drive the exact process contract without Python.
type UpscaleTool struct {
	runner UpscaleCommandRunner
}

// upscaleRuntime is the resolved managed engine.
type upscaleRuntime struct {
	PythonPath string
	DataDir    string
}

// upscaleRequestOptions is the parsed, normalized option set.
type upscaleRequestOptions struct {
	Scale      int
	Model      string
	ModelPath  string
	TileSize   int
	Format     string
	Quality    int
	QualitySet bool
}

// upscaleRequest is the parsed, normalized request.
type upscaleRequest struct {
	mode       string
	inputPaths []string
	outputDir  string
	options    upscaleRequestOptions
}

// preparedUpscale is one resolved input/output pair plus the runtime, the model
// and the options captured at planning time.
type preparedUpscale struct {
	inputPath  string
	outputPath string
	runtime    upscaleRuntime
	modelPath  string
	options    UpscaleOptions
}

// resolveUpscaleRuntime is a package variable so tests can simulate a missing
// pack without touching the real application data directory.
var resolveUpscaleRuntime = func() (upscaleRuntime, *UpscaleError) {
	dataDir, err := toolchain.DataDir()
	if err != nil {
		return upscaleRuntime{}, &UpscaleError{
			Code:    UpscaleCodeRuntimeUnavailable,
			Message: UpscaleRuntimeMissingReason,
			Cause:   err,
		}
	}

	return resolveUpscaleRuntimeAt(dataDir)
}

// runUpscale is the engine entry point used by the tool. Tests replace it to
// exercise planning, progress and error mapping without Python.
var runUpscale = UpscaleImages

func NewUpscaleTool() *UpscaleTool {
	return &UpscaleTool{}
}

// NewUpscaleToolWithRunner injects the process runner. nil keeps the real
// exec-based runner used in production.
func NewUpscaleToolWithRunner(runner UpscaleCommandRunner) *UpscaleTool {
	return &UpscaleTool{runner: runner}
}

func (t *UpscaleTool) ID() string {
	return ToolIDImageUpscale
}

func (t *UpscaleTool) Capability() string {
	return ToolIDImageUpscale
}

func (t *UpscaleTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "Upscale Image",
		Description:      "Upscale images 2x or 4x locally with a pinned ONNX model",
		Domain:           "image",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  append([]string(nil), imageInputExtensions...),
		OutputExtensions: []string{"png", "jpg", "webp"},
		RuntimeDeps:      []string{"upscale", "upscale-model"},
		Tags:             []string{"image", "upscale", "enlarge", "onnx", "real-esrgan"},
	}
}

func (t *UpscaleTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	runtime, runtimeErr := resolveUpscaleRuntime()
	if runtimeErr != nil {
		return models.ToolRuntimeState{
			Status:  "degraded",
			Healthy: false,
			Reason:  runtimeErr.Message,
		}
	}

	if _, modelErr := resolveManagedUpscaleModel(runtime); modelErr != nil {
		return models.ToolRuntimeState{
			Status:  "degraded",
			Healthy: false,
			Reason:  modelErr.Message,
		}
	}

	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *UpscaleTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan validates the pack, the inputs and the options, then resolves one
// collision-safe <stem>_2x / <stem>_4x destination per input without running
// Python.
func (t *UpscaleTool) Plan(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan, _, planErr := t.planUpscaleRequest(ctx, req)
	return plan, planErr
}

func (t *UpscaleTool) planUpscaleRequest(_ context.Context, req models.JobRequest) (models.PlanJobResponse, []preparedUpscale, *models.JobError) {
	parsed, parseErr := parseUpscaleRequest(req)
	if parseErr != nil {
		plan, _ := planFailure(models.PlanJobResponse{Outputs: []models.PlannedOutput{}}, parseErr)
		return plan, nil, parseErr
	}

	return t.planParsedUpscaleRequest(parsed)
}

// planParsedUpscaleRequest resolves the runtime and the model once per job and
// builds every planned output plus the executor input.
func (t *UpscaleTool) planParsedUpscaleRequest(parsed upscaleRequest) (models.PlanJobResponse, []preparedUpscale, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	runtime, runtimeErr := resolveUpscaleRuntime()
	if runtimeErr != nil {
		jobErr := upscaleEngineJobError(runtimeErr, nil)
		failedPlan, _ := planFailure(plan, jobErr)

		return failedPlan, nil, jobErr
	}

	modelPath, modelErr := resolveUpscaleModel(runtime, parsed.options)
	if modelErr != nil {
		jobErr := upscaleEngineJobError(modelErr, map[string]any{"model": parsed.options.Model})
		failedPlan, _ := planFailure(plan, jobErr)

		return failedPlan, nil, jobErr
	}

	inputWarnings := make([]string, 0)
	for _, inputPath := range parsed.inputPaths {
		if inputErr := ValidateUpscaleInput(inputPath); inputErr != nil {
			jobErr := upscaleEngineJobError(inputErr, map[string]any{"inputPath": inputPath})
			failedPlan, _ := planFailure(plan, jobErr)

			return failedPlan, nil, jobErr
		}
		meta, metaErr := readPlanImageMeta(inputPath)
		if metaErr != nil {
			jobErr := upscaleEngineJobError(&UpscaleError{
				Code:    UpscaleCodeInvalidInput,
				Message: fmt.Sprintf("%s is not a readable image.", filepath.Base(inputPath)),
				Details: map[string]any{"inputPath": inputPath},
				Cause:   metaErr,
			}, map[string]any{"inputPath": inputPath})
			failedPlan, _ := planFailure(plan, jobErr)

			return failedPlan, nil, jobErr
		}
		outputPixels := int64(meta.Width*parsed.options.Scale) * int64(meta.Height*parsed.options.Scale)
		if outputPixels > upscaleMaxOutputPixels {
			jobErr := upscaleEngineJobError(&UpscaleError{
				Code: UpscaleCodeOutputTooLarge,
				Message: fmt.Sprintf(
					"%s at %dx would be %d×%d px (%.1f MP). The limit is %.0f MP; choose %dx or a smaller image.",
					filepath.Base(inputPath), parsed.options.Scale,
					meta.Width*parsed.options.Scale, meta.Height*parsed.options.Scale,
					float64(outputPixels)/1_000_000,
					float64(upscaleMaxOutputPixels)/1_000_000,
					parsed.options.Scale/2,
				),
				Details: map[string]any{
					"inputPath":       inputPath,
					"outputWidth":     meta.Width * parsed.options.Scale,
					"outputHeight":    meta.Height * parsed.options.Scale,
					"outputPixels":    outputPixels,
					"maxOutputPixels": upscaleMaxOutputPixels,
				},
			}, map[string]any{"inputPath": inputPath})
			failedPlan, _ := planFailure(plan, jobErr)

			return failedPlan, nil, jobErr
		}
		if meta.Frames > 1 {
			inputWarnings = append(inputWarnings, fmt.Sprintf("%s: %s", filepath.Base(inputPath), imageUpscaleAnimatedWarning))
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
			jobErr := upscaleEngineJobError(upscaleFromBackgroundError(dirErr), map[string]any{"outputDir": targetDir, "inputPath": inputPath})
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

	options := UpscaleOptions{
		Scale:    parsed.options.Scale,
		TileSize: parsed.options.TileSize,
		Format:   parsed.options.Format,
		Quality:  parsed.options.Quality,
	}
	extension := upscaleOutputExtension(options.Format)

	outputs := make([]models.PlannedOutput, 0, len(parsed.inputPaths))
	prepared := make([]preparedUpscale, 0, len(parsed.inputPaths))
	warnings := make([]string, 0, len(parsed.inputPaths))

	for index, inputPath := range parsed.inputPaths {
		base := upscaleOutputBaseName(inputPath, parsed.options.Scale)
		outputPath, adjusted := outputplan.UniqueFilePath(targetDirs[index], base, extension, used)

		output := models.PlannedOutput{
			InputPath:   inputPath,
			OutputPath:  outputPath,
			OutputKind:  imageOutputKindFile,
			OutputCount: 1,
		}
		if adjusted {
			output.Adjusted = true
			output.Warnings = append(output.Warnings, fmt.Sprintf(
				"%s is already in use. Saving as %s.",
				base+"."+extension,
				filepath.Base(outputPath),
			))
		}

		outputs = append(outputs, output)
		warnings = append(warnings, output.Warnings...)
		prepared = append(prepared, preparedUpscale{
			inputPath:  inputPath,
			outputPath: outputPath,
			runtime:    runtime,
			modelPath:  modelPath,
			options:    options,
		})
	}

	optionWarnings := upscaleOptionWarnings(parsed.options)
	effective := map[string]any{
		"scale":    options.Scale,
		"model":    parsed.options.Model,
		"tileSize": options.TileSize,
		"format":   options.Format,
		"quality":  options.Quality,
	}
	if parsed.options.ModelPath != "" {
		effective["modelPath"] = modelPath
	}

	plan.Success = true
	plan.Message = upscalePlanMessage(len(prepared), parsed.options.Scale)
	plan.Outputs = outputs
	plan.Warnings = append(warnings, inputWarnings...)
	plan.Warnings = append(plan.Warnings, optionWarnings...)
	plan.EffectiveOptions = effective

	return plan, prepared, nil
}

// ExecuteSingle implements tools.SingleExecutor by delegating to the progress
// variant with a no-op callback.
func (t *UpscaleTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

// ExecuteSingleWithProgress resolves the plan once and executes exactly that
// plan, mapping bridge progress to job progress.
func (t *UpscaleTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	inputPath := firstInputPath(req.InputPaths)

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return imageFailureItem(inputPath, plannedOutputPath(plan), planErr), planErr
	}

	if onProgress != nil {
		onProgress(models.JobProgress{Current: 0, Total: 1, Stage: models.JobStatusRunning, Message: "upscaling 1/1"})
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, onProgress)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("IMAGE_UPSCALE_EXECUTION", "upscaling returned no result", nil)
	}

	return imageFailureItem(inputPath, plannedOutputPath(plan), jobErr), jobErr
}

// ExecuteBatch plans the request once and then executes exactly that plan. It
// never re-resolves output paths from the request.
func (t *UpscaleTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := parseUpscaleRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}
	if parsed.mode != "batch" {
		return nil, models.NewCanonicalJobError("IMAGE_UPSCALE_MODE_INVALID", "mode must be batch", nil)
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan upscales exactly the outputs resolved by Plan. The runtime is
// resolved once and the bridge runs once for the whole plan, so a reduced retry
// plan only processes the failed items.
func (t *UpscaleTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	total := len(plan.Outputs)
	if total == 0 {
		return nil, models.NewCanonicalJobError("IMAGE_UPSCALE_INPUT_INVALID", "Choose at least one image to process.", nil)
	}

	runtime, runtimeErr := resolveUpscaleRuntime()
	if runtimeErr != nil {
		return nil, upscaleEngineJobError(runtimeErr, nil)
	}

	options := upscaleOptionsFromEffective(plan.EffectiveOptions)
	normalized, optionErr := NormalizeUpscaleOptions(options)
	if optionErr != nil {
		return nil, upscaleEngineJobError(optionErr, nil)
	}

	modelPath := strings.TrimSpace(convertOptionString(plan.EffectiveOptions, "modelPath"))
	if modelPath == "" {
		model := strings.TrimSpace(convertOptionString(plan.EffectiveOptions, "model"))
		if model == "" {
			model = toolchain.DefaultUpscaleModel
		}
		resolved, modelErr := resolveUpscaleModel(runtime, upscaleRequestOptions{Model: model})
		if modelErr != nil {
			return nil, upscaleEngineJobError(modelErr, map[string]any{"model": model})
		}
		modelPath = resolved
	}

	prepared := make([]preparedUpscale, total)
	for index, planned := range plan.Outputs {
		prepared[index] = preparedUpscale{
			inputPath:  planned.InputPath,
			outputPath: planned.OutputPath,
			runtime:    runtime,
			modelPath:  modelPath,
			options:    normalized,
		}
	}

	return t.executePreparedUpscale(ctx, req.Mode, prepared, onProgress)
}

// executePreparedUpscale runs one bridge process for the prepared items and
// maps its outcomes, progress, cancellations and per-item failures.
func (t *UpscaleTool) executePreparedUpscale(ctx context.Context, mode string, prepared []preparedUpscale, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	detailCode := "IMAGE_UPSCALE_BATCH_ITEM"
	if mode == "single" {
		detailCode = "IMAGE_UPSCALE_EXECUTION"
	}

	items := make([]UpscaleItem, len(prepared))
	for index, item := range prepared {
		items[index] = UpscaleItem{InputPath: item.inputPath, OutputPath: item.outputPath}
	}

	outcomes, execErr := runUpscale(
		ctx,
		t.runner,
		prepared[0].runtime.PythonPath,
		prepared[0].modelPath,
		"",
		items,
		prepared[0].options,
		func(current, total int) {
			if onProgress == nil {
				return
			}
			onProgress(models.JobProgress{
				Current: current,
				Total:   total,
				Stage:   models.JobStatusRunning,
				Message: fmt.Sprintf("processed %d/%d", current, total),
			})
		},
	)

	// The engine rolls published outputs back on cancellation, so every planned
	// item is reported as cancelled rather than as a retryable failure.
	if upscaleIsCancellation(execErr) {
		cancelled := upscaleCancelledJobError(ctx, execErr)
		results := make([]models.JobResultItem, 0, len(prepared))
		for _, item := range prepared {
			results = append(results, imageFailureItem(item.inputPath, item.outputPath, cancelled))
		}

		return results, cancelled
	}

	if execErr == nil && len(outcomes) == 0 && mode == "single" {
		item := prepared[0]
		jobErr := models.NewCanonicalJobError(
			UpscaleCodeProcessFailed,
			fmt.Sprintf("The upscale engine produced no result for %s.", filepath.Base(item.inputPath)),
			map[string]any{"inputPath": item.inputPath, "outputPath": item.outputPath},
		)
		return []models.JobResultItem{imageFailureItem(item.inputPath, item.outputPath, jobErr)}, jobErr
	}

	outcomeByPath := make(map[string]UpscaleOutcome, len(outcomes))
	for _, outcome := range outcomes {
		outcomeByPath[rembgPathKey(outcome.OutputPath)] = outcome
	}

	failures := make(map[int]UpscaleItemFailure)
	var engineErr *UpscaleError
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

	results := make([]models.JobResultItem, 0, len(prepared))
	var firstErr *models.JobError

	for index, item := range prepared {
		if failure, failed := failures[index]; failed {
			jobErr := upscaleFailureJobError(failure, item.inputPath, item.outputPath, failureDetails)
			if firstErr == nil {
				firstErr = jobErr
			}
			results = append(results, imageFailureItem(item.inputPath, item.outputPath, jobErr))
			continue
		}

		if outcome, ok := outcomeByPath[rembgPathKey(item.outputPath)]; ok {
			results = append(results, upscaleSuccessItem(item, outcome))
			continue
		}

		jobErr := upscaleExecutionError(ctx, detailCode, item, execErr)
		if firstErr == nil {
			firstErr = jobErr
		}
		results = append(results, imageFailureItem(item.inputPath, item.outputPath, jobErr))
	}

	return results, firstErr
}

func parseUpscaleRequest(req models.JobRequest) (upscaleRequest, *models.JobError) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return upscaleRequest{}, models.NewCanonicalJobError("IMAGE_UPSCALE_MODE_INVALID", "mode must be single or batch", nil)
	}

	if len(req.InputPaths) == 0 {
		return upscaleRequest{}, models.NewCanonicalJobError("IMAGE_UPSCALE_INPUT_INVALID", "Choose at least one image to process.", nil)
	}
	if mode == "single" && len(req.InputPaths) != 1 {
		return upscaleRequest{}, models.NewCanonicalJobError("IMAGE_UPSCALE_SINGLE_INPUT_INVALID", "single mode requires exactly one input", nil)
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawPath := range req.InputPaths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			return upscaleRequest{}, models.NewCanonicalJobError("IMAGE_UPSCALE_INPUT_INVALID", "inputPaths contains empty item", nil)
		}
		inputPaths = append(inputPaths, trimmed)
	}

	options, optionErr := upscaleOptionsFromRequest(req.Options)
	if optionErr != nil {
		return upscaleRequest{}, optionErr
	}

	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = convertOptionString(req.Options, "outputDir")
	}

	return upscaleRequest{
		mode:       mode,
		inputPaths: inputPaths,
		outputDir:  outputDir,
		options:    options,
	}, nil
}

func upscaleOptionsFromRequest(options map[string]any) (upscaleRequestOptions, *models.JobError) {
	parsed := upscaleRequestOptions{
		Scale:    2,
		Model:    toolchain.DefaultUpscaleModel,
		TileSize: 128,
		Format:   "png",
		Quality:  upscaleDefaultQuality,
	}

	if optionSupplied(options, "scale") {
		scale, ok := upscaleWholeNumber(options, "scale")
		if !ok {
			return upscaleRequestOptions{}, models.NewCanonicalJobError(UpscaleCodeInvalidOption, "options.scale must be 2 or 4", nil)
		}
		parsed.Scale = scale
	}
	if parsed.Scale != 2 && parsed.Scale != 4 {
		return upscaleRequestOptions{}, models.NewCanonicalJobError(
			UpscaleCodeInvalidOption,
			"options.scale must be 2 or 4",
			map[string]any{"scale": parsed.Scale},
		)
	}

	if optionSupplied(options, "model") {
		raw, ok := options["model"].(string)
		if !ok {
			return upscaleRequestOptions{}, models.NewCanonicalJobError(UpscaleCodeInvalidOption, "options.model must be general or anime", nil)
		}
		model := strings.ToLower(strings.TrimSpace(raw))
		if model != toolchain.UpscaleModelGeneral && model != toolchain.UpscaleModelAnime {
			return upscaleRequestOptions{}, models.NewCanonicalJobError(
				"IMAGE_UPSCALE_MODEL_INVALID",
				"options.model must be general or anime",
				map[string]any{"model": raw},
			)
		}
		parsed.Model = model
	}

	if optionSupplied(options, "modelPath") {
		raw, ok := options["modelPath"].(string)
		if !ok {
			return upscaleRequestOptions{}, models.NewCanonicalJobError(UpscaleCodeInvalidOption, "options.modelPath must be a file path", nil)
		}
		parsed.ModelPath = strings.TrimSpace(raw)
	}

	if optionSupplied(options, "tileSize") {
		tile, ok := upscaleWholeNumber(options, "tileSize")
		if !ok || tile < upscaleMinTile || tile > upscaleMaxTile {
			return upscaleRequestOptions{}, models.NewCanonicalJobError(
				UpscaleCodeInvalidOption,
				fmt.Sprintf("options.tileSize must be between %d and %d", upscaleMinTile, upscaleMaxTile),
				map[string]any{"tileSize": options["tileSize"]},
			)
		}
		parsed.TileSize = tile
	}

	if optionSupplied(options, "format") {
		raw, ok := options["format"].(string)
		if !ok {
			return upscaleRequestOptions{}, models.NewCanonicalJobError("IMAGE_UPSCALE_FORMAT_INVALID", "options.format must be png, jpeg or webp", nil)
		}
		format := normalizeFormatName(raw)
		switch format {
		case "", "png":
			parsed.Format = "png"
		case "jpeg", "jpg":
			parsed.Format = "jpeg"
		case "webp":
			parsed.Format = "webp"
		default:
			return upscaleRequestOptions{}, models.NewCanonicalJobError(
				"IMAGE_UPSCALE_FORMAT_INVALID",
				"options.format must be png, jpeg or webp",
				map[string]any{"format": raw},
			)
		}
	}

	if optionSupplied(options, "quality") {
		quality, ok := upscaleWholeNumber(options, "quality")
		if !ok || quality < 1 || quality > 100 {
			return upscaleRequestOptions{}, models.NewCanonicalJobError(
				UpscaleCodeInvalidOption,
				"options.quality must be between 1 and 100",
				map[string]any{"quality": options["quality"]},
			)
		}
		parsed.Quality = quality
		parsed.QualitySet = true
	}

	normalized, normalizeErr := NormalizeUpscaleOptions(UpscaleOptions{
		Scale:    parsed.Scale,
		TileSize: parsed.TileSize,
		Format:   parsed.Format,
		Quality:  parsed.Quality,
	})
	if normalizeErr != nil {
		return upscaleRequestOptions{}, upscaleEngineJobError(normalizeErr, nil)
	}
	parsed.Scale = normalized.Scale
	parsed.TileSize = normalized.TileSize
	parsed.Format = normalized.Format
	parsed.Quality = normalized.Quality

	return parsed, nil
}

// resolveUpscaleRuntimeAt resolves the managed engine inside one data
// directory. It only stats files; it never starts Python.
func resolveUpscaleRuntimeAt(dataDir string) (upscaleRuntime, *UpscaleError) {
	pythonPath, pythonOK := toolchain.ManagedUpscalePythonPath(dataDir)
	if !pythonOK {
		return upscaleRuntime{}, &UpscaleError{
			Code:    UpscaleCodeRuntimeUnavailable,
			Message: UpscaleRuntimeMissingReason,
			Details: map[string]any{"pythonPath": pythonPath},
		}
	}

	if ready, reason := toolchain.ManagedUpscaleRuntimeState(dataDir); !ready {
		return upscaleRuntime{}, &UpscaleError{
			Code:    UpscaleCodeRuntimeUnavailable,
			Message: reason,
			Details: map[string]any{"pythonPath": pythonPath},
		}
	}

	return upscaleRuntime{PythonPath: pythonPath, DataDir: dataDir}, nil
}

// resolveUpscaleModel resolves the model path for one request. A custom path
// always wins; otherwise the anime choice requires a configured custom model
// and the general choice requires the managed pinned model.
func resolveUpscaleModel(runtime upscaleRuntime, options upscaleRequestOptions) (string, *UpscaleError) {
	if custom := strings.TrimSpace(options.ModelPath); custom != "" {
		if !strings.EqualFold(filepath.Ext(custom), ".onnx") {
			return "", &UpscaleError{
				Code:    UpscaleCodeModelUnavailable,
				Message: "the custom upscale model must be an .onnx file",
				Details: map[string]any{"modelPath": custom},
			}
		}
		if _, modelErr := validateUpscaleModelPath(custom); modelErr != nil {
			return "", modelErr
		}

		return custom, nil
	}

	if options.Model == toolchain.UpscaleModelAnime {
		return "", &UpscaleError{
			Code:    UpscaleCodeModelUnavailable,
			Message: UpscaleAnimeModelMissingReason,
			Details: map[string]any{"model": options.Model},
		}
	}

	return resolveManagedUpscaleModel(runtime)
}

func resolveManagedUpscaleModel(runtime upscaleRuntime) (string, *UpscaleError) {
	modelPath, modelOK := toolchain.ManagedUpscaleModelPath(runtime.DataDir)
	if !modelOK {
		spec := toolchain.DefaultUpscaleModelSpec()

		return "", &UpscaleError{
			Code:    UpscaleCodeModelUnavailable,
			Message: UpscaleModelMissingReason,
			Details: map[string]any{
				"model":      spec.Model,
				"modelFiles": spec.Files,
			},
		}
	}

	return modelPath, nil
}

// upscaleWholeNumber reads an integer option and rejects fractional values so a
// typo never silently truncates the scale, tile or quality.
func upscaleWholeNumber(options map[string]any, key string) (int, bool) {
	value, exists := options[key]
	if !exists || value == nil {
		return 0, false
	}

	switch typed := value.(type) {
	case int:
		return typed, true
	case int8:
		return int(typed), true
	case int16:
		return int(typed), true
	case int32:
		return int(typed), true
	case int64:
		return int(typed), true
	case uint:
		return int(typed), true
	case uint8:
		return int(typed), true
	case uint16:
		return int(typed), true
	case uint32:
		return int(typed), true
	case uint64:
		return int(typed), true
	case float32:
		return upscaleWholeFloat(float64(typed))
	case float64:
		return upscaleWholeFloat(typed)
	case json.Number:
		if integer, err := typed.Int64(); err == nil {
			return int(integer), true
		}
		parsed, err := typed.Float64()
		if err != nil {
			return 0, false
		}

		return upscaleWholeFloat(parsed)
	default:
		return 0, false
	}
}

func upscaleWholeFloat(value float64) (int, bool) {
	if math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) {
		return 0, false
	}
	if value < math.MinInt32 || value > math.MaxInt32 {
		return 0, false
	}

	return int(value), true
}

func upscaleOptionsFromEffective(effective map[string]any) UpscaleOptions {
	scale, _ := numericOption(effective, "scale")
	tile, _ := numericOption(effective, "tileSize")
	format := convertOptionString(effective, "format")
	quality, _ := numericOption(effective, "quality")

	return UpscaleOptions{Scale: scale, TileSize: tile, Format: format, Quality: quality}
}

func upscaleOutputBaseName(inputPath string, scale int) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		stem = "image"
	}

	return fmt.Sprintf("%s_%dx", stem, scale)
}

func upscalePlanMessage(count, scale int) string {
	if count == 1 {
		return fmt.Sprintf("Ready to upscale 1 image %dx.", scale)
	}

	return fmt.Sprintf("Ready to upscale %d images %dx.", count, scale)
}

func upscaleOptionWarnings(options upscaleRequestOptions) []string {
	warnings := make([]string, 0, 2)

	if options.Format == "png" && options.QualitySet {
		warnings = append(warnings, imageUpscalePNGQualityNote)
	}
	if options.ModelPath == "" && options.TileSize != 128 {
		warnings = append(warnings, imageUpscaleFixedTileNote)
	}

	return warnings
}

func upscaleSuccessItem(item preparedUpscale, outcome UpscaleOutcome) models.JobResultItem {
	outputPath := item.outputPath
	if strings.TrimSpace(outcome.OutputPath) != "" {
		outputPath = outcome.OutputPath
	}

	return models.JobResultItem{
		InputPath:   item.inputPath,
		OutputPath:  outputPath,
		Outputs:     []string{outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     fmt.Sprintf("%dx upscale, %d×%d", item.options.Scale, outcome.Width, outcome.Height),
	}
}

// upscaleEngineJobError maps a typed engine failure to a canonical job error.
func upscaleEngineJobError(upscaleErr *UpscaleError, extraDetails map[string]any) *models.JobError {
	details := make(map[string]any, len(extraDetails)+2)
	for key, value := range extraDetails {
		details[key] = value
	}
	for key, value := range upscaleErr.Details {
		details[key] = value
	}
	if upscaleErr.Cause != nil {
		details["error"] = upscaleErr.Cause.Error()
	}

	switch upscaleErr.Code {
	case UpscaleCodeRuntimeUnavailable, UpscaleCodeModelUnavailable:
		return models.NewJobError(models.ErrorCodeRuntimeDepMissing, upscaleErr.Code, upscaleErr.Message, details)
	case UpscaleCodeInputUnsupported:
		return models.NewJobError(models.ErrorCodeUnsupportedFormat, upscaleErr.Code, upscaleErr.Message, details)
	case UpscaleCodeCancelled:
		return models.NewJobError(models.ErrorCodeCancelledByUser, upscaleErr.Code, upscaleErr.Message, details)
	case UpscaleCodeInvalidInput,
		UpscaleCodeInputNotFound,
		UpscaleCodeInvalidOption,
		UpscaleCodeOutputInvalid,
		UpscaleCodeOutputExists,
		UpscaleCodeOutputTooLarge:
		return models.NewJobError(models.ErrorCodeValidationInvalidInput, upscaleErr.Code, upscaleErr.Message, details)
	default:
		return models.NewJobError(models.ErrorCodeExecIOTransient, upscaleErr.Code, upscaleErr.Message, details)
	}
}

func upscaleFailureJobError(failure UpscaleItemFailure, inputPath, outputPath string, extraDetails map[string]any) *models.JobError {
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

	return upscaleEngineJobError(&UpscaleError{
		Code:    failure.Code,
		Message: failure.Message,
	}, details)
}

// upscaleExecutionError maps a failed execution to a job error. Cancellation
// wins over the generic code, including the last batch item.
func upscaleExecutionError(ctx context.Context, detailCode string, item preparedUpscale, execErr error) *models.JobError {
	if upscaleIsCancellation(execErr) {
		return upscaleCancelledJobError(ctx, execErr)
	}

	var upscaleErr *UpscaleError
	if errors.As(execErr, &upscaleErr) {
		if len(upscaleErr.Failures) == 1 {
			return upscaleFailureJobError(upscaleErr.Failures[0], item.inputPath, item.outputPath, upscaleErr.Details)
		}

		return upscaleEngineJobError(upscaleErr, map[string]any{
			"inputPath":  item.inputPath,
			"outputPath": item.outputPath,
		})
	}

	details := map[string]any{"inputPath": item.inputPath, "outputPath": item.outputPath}
	if execErr != nil {
		details["error"] = execErr.Error()
	}

	return models.NewCanonicalJobError(detailCode, "Could not upscale the image.", details)
}

// upscaleIsCancellation classifies only an error returned by the engine. A nil
// error is a successful run even when ctx was cancelled late.
func upscaleIsCancellation(execErr error) bool {
	if execErr == nil {
		return false
	}

	var upscaleErr *UpscaleError
	if errors.As(execErr, &upscaleErr) && upscaleErr.Code == UpscaleCodeCancelled {
		return true
	}

	return errors.Is(execErr, context.Canceled) || errors.Is(execErr, context.DeadlineExceeded)
}

func upscaleCancelledJobError(ctx context.Context, execErr error) *models.JobError {
	var cause error = context.Canceled
	if ctx != nil && ctx.Err() != nil {
		cause = ctx.Err()
	}

	var details map[string]any
	var upscaleErr *UpscaleError
	if errors.As(execErr, &upscaleErr) {
		if len(upscaleErr.Details) > 0 {
			details = make(map[string]any, len(upscaleErr.Details))
			for key, value := range upscaleErr.Details {
				details[key] = value
			}
		}
		if cause == context.Canceled && upscaleErr.Cause != nil {
			cause = upscaleErr.Cause
		}
	}

	message := fmt.Errorf(OperationCancelledMsg, cause).Error()
	return models.NewCanonicalJobError(UpscaleCodeCancelled, message, details)
}
