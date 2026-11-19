package image

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/utils"
)

const (
	ToolIDImageConvertV1 = "tool.image.convert"

	imageDefaultFormat  = "webp"
	imageFallbackFormat = "png"
	imageOutputKindFile = "file"
	// imageAutoFormat keeps each input's own format. The UI sends it as
	// options.format; "original" is accepted as an alias.
	imageAutoFormat = "auto"
)

var imageInputExtensions = []string{"jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff", "tif"}

type ImageToolAdapter struct {
	converter *ImageConverter
	// convertSingle is the conversion seam. Production always points at
	// converter.ConvertSingle; tests replace it to drive completion order.
	convertSingle func(ctx context.Context, inputPath, outputPath, format string, options map[string]any) error
}

func NewImageToolAdapter(converter *ImageConverter) *ImageToolAdapter {
	if converter == nil {
		converter = NewImageConverter()
	}

	return &ImageToolAdapter{converter: converter, convertSingle: converter.ConvertSingle}
}

func (t *ImageToolAdapter) ID() string {
	return ToolIDImageConvertV1
}

func (t *ImageToolAdapter) Capability() string {
	return "tool.image.convert"
}

func (t *ImageToolAdapter) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Image Converter",
		Description:      "Image conversion tool exposed as Tool V1 jobs",
		Domain:           "image",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  append([]string(nil), imageInputExtensions...),
		OutputExtensions: t.converter.SupportedFormats(),
		RuntimeDeps:      []string{"libvips"},
		Tags:             []string{"image", "convert"},
	}
}

func (t *ImageToolAdapter) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *ImageToolAdapter) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves every output path for the request without converting anything.
func (t *ImageToolAdapter) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	if len(req.InputPaths) == 0 {
		return planFailure(plan, models.NewCanonicalJobError("IMAGE_INPUT_REQUIRED", "Choose at least one image to convert.", nil))
	}

	if req.Mode != "single" && req.Mode != "batch" {
		return planFailure(plan, models.NewCanonicalJobError("IMAGE_MODE_INVALID", "mode must be single or batch", nil))
	}

	if req.Mode == "single" && len(req.InputPaths) != 1 {
		return planFailure(plan, models.NewCanonicalJobError("IMAGE_SINGLE_INPUT_COUNT", "single mode requires exactly one input path", nil))
	}

	if _, _, backgroundErr := backgroundOption(req.Options); backgroundErr != nil {
		return planFailure(plan, models.NewCanonicalJobError("IMAGE_BACKGROUND_INVALID", backgroundErr.Error(), nil))
	}

	requestedFormat, autoFormat := t.resolveRequestedFormat(req.Options)
	if !autoFormat {
		if err := t.converter.validateOutputFormat(requestedFormat); err != nil {
			return planFailure(plan, models.NewCanonicalJobError("IMAGE_FORMAT_INVALID", err.Error(), nil))
		}
	}

	outputDir, explicitOutput := t.planDestinations(req)

	// Reserve every input path so an automatically derived output can never
	// overwrite a different input of the same batch.
	used := make(map[string]struct{}, len(req.InputPaths))
	for _, inputPath := range req.InputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	outputs := make([]models.PlannedOutputV1, 0, len(req.InputPaths))
	warnings := make([]string, 0)
	for index, inputPath := range req.InputPaths {
		allowExplicit := req.Mode == "single" && index == 0
		outputFormat := t.inputOutputFormat(inputPath, requestedFormat, autoFormat)
		planned, planErr := t.planOutput(inputPath, outputDir, explicitOutput, outputFormat, allowExplicit, used)
		if planErr != nil {
			if planned.OutputPath != "" {
				outputs = append(outputs, planned)
				plan.Outputs = outputs
			}
			return planFailure(plan, planErr)
		}
		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = imagePlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = t.effectiveOptions(req.Options, requestedFormat, autoFormat)

	return plan, nil
}

func (t *ImageToolAdapter) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	inputPath := firstInputPath(req.InputPaths)

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return imageFailureItem(inputPath, plannedOutputPath(plan), planErr), planErr
	}

	inputPath = req.InputPaths[0]
	outputPath := plan.Outputs[0].OutputPath

	requestedFormat, autoFormat := t.resolveRequestedFormat(req.Options)
	format := t.inputOutputFormat(inputPath, requestedFormat, autoFormat)

	if err := t.convertSingle(ctx, inputPath, outputPath, format, req.Options); err != nil {
		jobErr := models.NewCanonicalJobError("IMAGE_SINGLE_EXECUTION", err.Error(), nil)
		return imageFailureItem(inputPath, outputPath, jobErr), jobErr
	}

	return models.JobResultItemV1{
		InputPath:   inputPath,
		OutputPath:  outputPath,
		Outputs:     []string{outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     "conversion successful",
	}, nil
}

func (t *ImageToolAdapter) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	if len(req.InputPaths) == 0 {
		return nil, models.NewCanonicalJobError("IMAGE_INPUT_REQUIRED", "Choose at least one image to convert.", nil)
	}

	requestedFormat, autoFormat := t.resolveRequestedFormat(req.Options)
	if !autoFormat {
		if err := t.converter.validateOutputFormat(requestedFormat); err != nil {
			return nil, models.NewCanonicalJobError("IMAGE_FORMAT_INVALID", err.Error(), nil)
		}
	}

	outputDir, _ := t.planDestinations(req)
	used := make(map[string]struct{}, len(req.InputPaths))
	for _, inputPath := range req.InputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	// Planning is sequential because planOutput mutates the shared used set.
	// Every output is resolved before any worker starts, so the parallel
	// closure only executes already-planned work.
	type plannedBatchItem struct {
		inputPath string
		format    string
		planned   models.PlannedOutputV1
		planErr   *models.JobErrorV1
	}

	plannedItems := make([]plannedBatchItem, len(req.InputPaths))
	for index, inputPath := range req.InputPaths {
		format := t.inputOutputFormat(inputPath, requestedFormat, autoFormat)
		planned, planErr := t.planOutput(inputPath, outputDir, "", format, false, used)
		plannedItems[index] = plannedBatchItem{
			inputPath: inputPath,
			format:    format,
			planned:   planned,
			planErr:   planErr,
		}
	}

	total := len(plannedItems)
	items := make([]models.JobResultItemV1, total)
	completed := make([]bool, total)
	progress := newParallelProgress(total, onProgress)

	runErr := utils.ParallelForIndex(ctx, total, utils.ParallelWorkerCount(), func(ctx context.Context, index int) error {
		item := plannedItems[index]
		items[index] = t.executePlannedOutput(ctx, item.inputPath, item.planned, item.format, item.planErr, req.Options)
		completed[index] = true
		progress.itemCompleted()

		return nil
	})

	if batchRunCancelled(ctx, runErr) {
		return collectCompletedItems(items, completed), models.NewCanonicalJobError("IMAGE_BATCH_CANCELLED", batchCancellationCause(ctx, runErr).Error(), nil)
	}

	return items, firstFailedItemError(items)
}

// executePlannedOutput runs one already-planned conversion. It never touches
// the planning set, so it is safe to run concurrently.
func (t *ImageToolAdapter) executePlannedOutput(ctx context.Context, inputPath string, planned models.PlannedOutputV1, format string, planErr *models.JobErrorV1, options map[string]any) models.JobResultItemV1 {
	if planErr != nil {
		return imageFailureItem(inputPath, planned.OutputPath, planErr)
	}

	if err := t.convertSingle(ctx, inputPath, planned.OutputPath, format, options); err != nil {
		jobErr := models.NewCanonicalJobError("IMAGE_BATCH_ITEM", err.Error(), map[string]any{"inputPath": inputPath})
		return imageFailureItem(inputPath, planned.OutputPath, jobErr)
	}

	return models.JobResultItemV1{
		InputPath:   inputPath,
		OutputPath:  planned.OutputPath,
		Outputs:     []string{planned.OutputPath},
		OutputCount: 1,
		Success:     true,
		Message:     "conversion successful",
	}
}

// planDestinations returns the fallback output directory and the explicit
// output path from the request options.
func (t *ImageToolAdapter) planDestinations(req models.JobRequestV1) (string, string) {
	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = convertOptionString(req.Options, "outputDir")
	}

	explicitOutput := ""
	if req.Mode == "single" {
		explicitOutput = convertOptionString(req.Options, "outputPath")
	}

	return outputDir, explicitOutput
}

// planOutput resolves one input into one planned output file. It never writes.
func (t *ImageToolAdapter) planOutput(inputPath, outputDir, explicitOutput, format string, allowExplicit bool, used map[string]struct{}) (models.PlannedOutputV1, *models.JobErrorV1) {
	planned := models.PlannedOutputV1{
		InputPath:  inputPath,
		OutputKind: imageOutputKindFile,
	}

	input := strings.TrimSpace(inputPath)
	if input == "" {
		return planned, models.NewCanonicalJobError("IMAGE_INPUT_NOT_FOUND", "Choose an image to convert.", nil)
	}

	info, err := os.Stat(input)
	if err != nil {
		return planned, imageInputAccessError(input, err)
	}

	if info.IsDir() {
		return planned, models.NewCanonicalJobError(
			"IMAGE_INPUT_UNSUPPORTED",
			fmt.Sprintf("%s is a folder, not an image.", filepath.Base(input)),
			map[string]any{"inputPath": input},
		)
	}

	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(input), "."))
	if !t.supportsInputExtension(extension) {
		return planned, models.NewCanonicalJobError(
			"IMAGE_INPUT_UNSUPPORTED",
			fmt.Sprintf("%s is not a supported image. Use %s.", filepath.Base(input), strings.Join(imageInputExtensions, ", ")),
			map[string]any{"inputPath": input, "extension": extension},
		)
	}

	if allowExplicit && explicitOutput != "" {
		return t.planExplicitOutput(input, explicitOutput, planned)
	}

	targetDir := strings.TrimSpace(outputDir)
	if targetDir == "" {
		targetDir = filepath.Dir(input)
	}
	targetDir = filepath.Clean(targetDir)

	if dirErr := ensureOutputDirectory(targetDir); dirErr != nil {
		return planned, dirErr
	}

	stem := strings.TrimSuffix(filepath.Base(input), filepath.Ext(input))
	if strings.TrimSpace(stem) == "" {
		stem = "image"
	}

	base := stem
	sameAsInput := sameFilePath(filepath.Join(targetDir, fmt.Sprintf("%s.%s", stem, format)), input)
	if sameAsInput {
		base = stem + "-converted"
	}

	outputPath, adjusted := outputplan.UniqueFilePath(targetDir, base, format, used)
	planned.OutputPath = outputPath

	if sameAsInput {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("Saving as %s to keep the original image.", filepath.Base(outputPath)))
	} else if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", fmt.Sprintf("%s.%s", stem, format), filepath.Base(outputPath)))
	}

	return planned, nil
}

func (t *ImageToolAdapter) planExplicitOutput(inputPath, explicitOutput string, planned models.PlannedOutputV1) (models.PlannedOutputV1, *models.JobErrorV1) {
	outputPath := filepath.Clean(strings.TrimSpace(explicitOutput))
	planned.OutputPath = outputPath

	if sameFilePath(inputPath, outputPath) {
		return planned, models.NewCanonicalJobError(
			"IMAGE_OUTPUT_SAME_AS_INPUT",
			fmt.Sprintf("%s is the input file. Choose a different name.", filepath.Base(outputPath)),
			map[string]any{"outputPath": outputPath},
		)
	}

	if info, err := os.Stat(outputPath); err == nil {
		planned.Exists = true
		if info.IsDir() {
			return planned, models.NewCanonicalJobError(
				"IMAGE_OUTPUT_DIR_INVALID",
				fmt.Sprintf("%s is a folder, not a file.", filepath.Base(outputPath)),
				map[string]any{"outputPath": outputPath},
			)
		}
		return planned, models.NewCanonicalJobError(
			"IMAGE_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another name.", filepath.Base(outputPath)),
			map[string]any{"outputPath": outputPath},
		)
	} else if !errors.Is(err, os.ErrNotExist) {
		return planned, models.NewCanonicalJobError(
			"IMAGE_OUTPUT_DIR_INVALID",
			fmt.Sprintf("%s cannot be accessed. Choose another location.", filepath.Base(outputPath)),
			map[string]any{"outputPath": outputPath},
		)
	}

	if dirErr := ensureOutputDirectory(filepath.Dir(outputPath)); dirErr != nil {
		return planned, dirErr
	}

	return planned, nil
}

// resolveRequestedFormat returns the normalized output format plus whether the
// request asks to keep every input's own format.
func (t *ImageToolAdapter) resolveRequestedFormat(options map[string]any) (string, bool) {
	format := normalizeFormatName(convertOptionString(options, "format"))
	if format == "" {
		return imageDefaultFormat, false
	}

	if format == imageAutoFormat || format == "original" {
		return imageAutoFormat, true
	}

	return format, false
}

// inputOutputFormat resolves the encoder for one input. Explicit formats pass
// through untouched; auto keeps the source format when the converter can write
// it and falls back to PNG otherwise (BMP has no bimg encoder, for example).
func (t *ImageToolAdapter) inputOutputFormat(inputPath, requestedFormat string, autoFormat bool) string {
	if !autoFormat {
		return requestedFormat
	}

	if format, ok := t.converter.sourceFormat(inputPath); ok {
		return format
	}

	return imageFallbackFormat
}

func (t *ImageToolAdapter) supportsInputExtension(extension string) bool {
	normalized := strings.ToLower(strings.TrimSpace(extension))
	for _, supported := range imageInputExtensions {
		if supported == normalized {
			return true
		}
	}
	return false
}

// effectiveOptions reports the normalized request. In auto mode the effective
// format stays "auto" because each output carries its own per-input format.
func (t *ImageToolAdapter) effectiveOptions(options map[string]any, requestedFormat string, autoFormat bool) map[string]any {
	effective := map[string]any{"format": requestedFormat}
	if autoFormat {
		effective["format"] = imageAutoFormat
	}

	// longestEdge constrains only the larger side, so width/height are left
	// out of the effective options when it is also supplied.
	if longestEdge, ok := numericOption(options, "longestEdge"); ok && longestEdge > 0 {
		effective["longestEdge"] = longestEdge
	} else {
		for _, key := range []string{"width", "height"} {
			if value, ok := numericOption(options, key); ok && value > 0 {
				effective[key] = value
			}
		}
	}

	if value, ok := numericOption(options, "quality"); ok {
		effective["quality"] = value
	}

	if value, ok := numericOption(options, "rotate"); ok {
		effective["rotate"] = value
	}

	for _, key := range []string{"stripMetadata", "flipH", "flipV"} {
		if value, ok := boolOption(options, key); ok {
			effective[key] = value
		}
	}

	// Report the normalized #rrggbb form so callers see exactly what the
	// converter applies when flattening transparency.
	if background, ok, err := backgroundOption(options); err == nil && ok {
		effective["background"] = fmt.Sprintf("#%02x%02x%02x", background.R, background.G, background.B)
	}

	return effective
}

func planFailure(plan models.PlanJobResponseV1, jobErr *models.JobErrorV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan.Success = false
	plan.Error = jobErr
	if jobErr != nil && jobErr.Message != "" {
		plan.Message = jobErr.Message
	}
	return plan, jobErr
}

func imagePlanMessage(count int) string {
	if count == 1 {
		return "Ready to convert 1 image."
	}
	return fmt.Sprintf("Ready to convert %d images.", count)
}

func imageInputAccessError(inputPath string, err error) *models.JobErrorV1 {
	if errors.Is(err, os.ErrNotExist) {
		return models.NewCanonicalJobError(
			"IMAGE_INPUT_NOT_FOUND",
			fmt.Sprintf("%s was not found.", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}

	return models.NewCanonicalJobError(
		"IMAGE_INPUT_NOT_FOUND",
		fmt.Sprintf("%s cannot be read.", filepath.Base(inputPath)),
		map[string]any{"inputPath": inputPath},
	)
}

func ensureOutputDirectory(dir string) *models.JobErrorV1 {
	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return models.NewCanonicalJobError(
				"IMAGE_OUTPUT_DIR_INVALID",
				fmt.Sprintf("The output folder does not exist: %s", dir),
				map[string]any{"outputDir": dir},
			)
		}
		return models.NewCanonicalJobError(
			"IMAGE_OUTPUT_DIR_INVALID",
			fmt.Sprintf("The output folder cannot be used: %s", dir),
			map[string]any{"outputDir": dir},
		)
	}

	if !info.IsDir() {
		return models.NewCanonicalJobError(
			"IMAGE_OUTPUT_DIR_INVALID",
			fmt.Sprintf("%s is not a folder.", dir),
			map[string]any{"outputDir": dir},
		)
	}

	return nil
}

func imageFailureItem(inputPath, outputPath string, jobErr *models.JobErrorV1) models.JobResultItemV1 {
	item := models.JobResultItemV1{
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

// parallelProgress serializes completion progress for a parallel batch. The
// atomic counter numbers finished items; the mutex keeps the callback order
// monotonic too, so callers never observe "processed 2/3" before "processed
// 1/3" even when two workers finish at the same time.
type parallelProgress struct {
	total      int
	onProgress func(models.JobProgressV1)
	completed  atomic.Int64
	mu         sync.Mutex
}

func newParallelProgress(total int, onProgress func(models.JobProgressV1)) *parallelProgress {
	if onProgress == nil {
		return nil
	}

	return &parallelProgress{total: total, onProgress: onProgress}
}

// itemCompleted counts one finished item and emits its progress event.
func (p *parallelProgress) itemCompleted() {
	if p == nil {
		return
	}

	p.mu.Lock()
	defer p.mu.Unlock()

	current := int(p.completed.Add(1))
	p.onProgress(models.JobProgressV1{
		Current: current,
		Total:   p.total,
		Stage:   models.JobStatusRunning,
		Message: fmt.Sprintf("processed %d/%d", current, p.total),
	})
}

// collectCompletedItems returns the results written by index in input order,
// dropping entries whose worker never ran (for example after cancellation).
// Each completed flag is written by the goroutine that owns its index, so the
// read after the workers join is race-free.
func collectCompletedItems(items []models.JobResultItemV1, completed []bool) []models.JobResultItemV1 {
	collected := make([]models.JobResultItemV1, 0, len(items))
	for index, item := range items {
		if completed[index] {
			collected = append(collected, item)
		}
	}

	return collected
}

// firstFailedItemError returns the error of the first failed item in input
// order, matching the sequential loop semantics.
func firstFailedItemError(items []models.JobResultItemV1) *models.JobErrorV1 {
	for _, item := range items {
		if !item.Success && item.Error != nil {
			return item.Error
		}
	}

	return nil
}

// batchCancellationCause returns the context error when the batch context was
// cancelled, otherwise the helper error.
func batchCancellationCause(ctx context.Context, runErr error) error {
	if ctx != nil && ctx.Err() != nil {
		return ctx.Err()
	}

	return runErr
}

// batchRunCancelled reports whether a parallel batch must return its
// cancellation error after the workers join.
func batchRunCancelled(ctx context.Context, runErr error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}

	return errors.Is(runErr, context.Canceled) || errors.Is(runErr, context.DeadlineExceeded)
}

func plannedOutputPath(plan models.PlanJobResponseV1) string {
	if len(plan.Outputs) == 0 {
		return ""
	}
	return plan.Outputs[0].OutputPath
}

func firstInputPath(inputPaths []string) string {
	if len(inputPaths) == 0 {
		return ""
	}
	return inputPaths[0]
}

func convertOptionString(options map[string]any, key string) string {
	if options == nil {
		return ""
	}

	value, ok := options[key].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(value)
}

func numericOption(options map[string]any, key string) (int, bool) {
	if options == nil {
		return 0, false
	}

	switch value := options[key].(type) {
	case int:
		return value, true
	case int32:
		return int(value), true
	case int64:
		return int(value), true
	case float32:
		return int(value), true
	case float64:
		return int(value), true
	default:
		return 0, false
	}
}

// boolOption reads a boolean option that may arrive as a JSON bool or, from
// older callers, as a textual representation.
func boolOption(options map[string]any, key string) (bool, bool) {
	if options == nil {
		return false, false
	}

	switch value := options[key].(type) {
	case bool:
		return value, true
	case string:
		parsed, err := strconv.ParseBool(strings.TrimSpace(value))
		if err != nil {
			return false, false
		}
		return parsed, true
	default:
		return false, false
	}
}

// boolOptionValue is boolOption for callers that do not need to know whether
// the option was supplied.
func boolOptionValue(options map[string]any, key string) bool {
	value, _ := boolOption(options, key)
	return value
}

// optionSupplied reports whether a request option was provided with a value.
func optionSupplied(options map[string]any, key string) bool {
	if options == nil {
		return false
	}

	value, ok := options[key]
	return ok && value != nil
}

func sameFilePath(left, right string) bool {
	if left == "" || right == "" {
		return false
	}
	leftClean := filepath.Clean(left)
	rightClean := filepath.Clean(right)
	if leftClean == rightClean {
		return true
	}
	if abs, err := filepath.Abs(leftClean); err == nil {
		leftClean = abs
	}
	if abs, err := filepath.Abs(rightClean); err == nil {
		rightClean = abs
	}
	if leftClean == rightClean {
		return true
	}
	if eval, err := filepath.EvalSymlinks(leftClean); err == nil {
		leftClean = eval
	}
	if eval, err := filepath.EvalSymlinks(rightClean); err == nil {
		rightClean = eval
	}
	return leftClean == rightClean
}
