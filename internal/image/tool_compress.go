package image

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"

	"github.com/h2non/bimg"
)

const ToolIDImageCompressV1 = "tool.image.compress"

const compressCapability = "image.compress"

type CompressTool struct{}

type compressRequest struct {
	mode            string
	inputPaths      []string
	outputPath      string
	outputDir       string
	targetSizeBytes int64
	format          string
	allowDownscale  bool
	maxDimension    int
	quality         int
	stripMetadata   bool
	backgroundHex   string
}

type preparedCompress struct {
	inputPath       string
	outputPath      string
	outputFmt       string
	targetSizeBytes int64
	allowDownscale  bool
	maxDimension    int
	quality         int
	stripMetadata   bool
	background      *bimg.Color
}

// compressExecution carries the encoder result plus the original size needed
// for the user-facing size report.
type compressExecution struct {
	inputBytes int64
	outcome    CompressOutcome
}

func NewCompressTool() *CompressTool {
	return &CompressTool{}
}

func (t *CompressTool) ID() string {
	return ToolIDImageCompressV1
}

func (t *CompressTool) Capability() string {
	return compressCapability
}

func (t *CompressTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Image Compressor",
		Description:      "Compress images to a target file size with quality search and optional downscaling",
		Domain:           "image",
		Capability:       t.Capability(),
		Version:          "1.0.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  append([]string(nil), imageInputExtensions...),
		OutputExtensions: []string{"jpeg", "png", "webp"},
		RuntimeDeps:      []string{"libvips"},
		Tags:             []string{"image", "compress", "target-size"},
	}
}

func (t *CompressTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *CompressTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe <stem>_compressed.<ext> destination per
// input and validates the compression options without encoding anything. An
// explicit options.outputPath keeps the single request contract: it must differ
// from the input and the target must not exist yet.
func (t *CompressTool) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	parsed, parseErr := parseCompressRequest(req)
	if parseErr != nil {
		return planFailure(models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}, parseErr)
	}

	return planParsedCompressRequest(parsed)
}

func planParsedCompressRequest(parsed compressRequest) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	formats := make([]string, 0, len(parsed.inputPaths))
	for _, inputPath := range parsed.inputPaths {
		if inputErr := validateCompressInputPath(inputPath); inputErr != nil {
			return planFailure(plan, inputErr)
		}
		formats = append(formats, resolveCompressOutputFormat(inputPath, parsed.format))
	}

	// Reserve every input path so an auto-derived output can never overwrite a
	// different input of the same batch.
	used := make(map[string]struct{}, len(parsed.inputPaths))
	for _, inputPath := range parsed.inputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	if parsed.mode == "single" {
		planned, planErr := planSingleCompressOutput(parsed, formats[0], used)
		if planErr != nil {
			if planned.OutputPath != "" {
				plan.Outputs = []models.PlannedOutputV1{planned}
			}
			return planFailure(plan, planErr)
		}

		plan.Success = true
		plan.Message = compressPlanMessage(1)
		plan.Outputs = []models.PlannedOutputV1{planned}
		plan.Warnings = planned.Warnings
		plan.EffectiveOptions = compressEffectiveOptions(parsed, formats)

		return plan, nil
	}

	if parsed.outputDir != "" {
		if dirErr := validateCompressOutputDir(parsed.outputDir); dirErr != nil {
			return planFailure(plan, dirErr)
		}
	}

	outputs := make([]models.PlannedOutputV1, 0, len(parsed.inputPaths))
	warnings := make([]string, 0, len(parsed.inputPaths))
	for index, inputPath := range parsed.inputPaths {
		targetDir := parsed.outputDir
		if targetDir == "" {
			targetDir = filepath.Dir(inputPath)
		}

		planned := planAutoCompressOutput(inputPath, filepath.Clean(targetDir), formats[index], used)
		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = compressPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = compressEffectiveOptions(parsed, formats)

	return plan, nil
}

// ExecuteSingle implements tools.SingleExecutor by delegating to the progress
// variant with a no-op callback.
func (t *CompressTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

// ExecuteSingleWithProgress compresses the planned image and reports stage
// level progress for the one-item job.
func (t *CompressTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
	inputPath := firstInputPath(req.InputPaths)

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return imageFailureItem(inputPath, plannedOutputPath(plan), planErr), planErr
	}

	prepared := preparedCompressFromPlan(plan.Outputs[0], plan.EffectiveOptions)

	if onProgress != nil {
		onProgress(models.JobProgressV1{Current: 0, Total: 1, Stage: models.JobStatusRunning, Message: "compressing 1/1"})
	}

	execution, execErr := executeCompressToPath(ctx, prepared)
	if execErr != nil {
		if ctx.Err() != nil {
			cancelErr := compressCancelledError(ctx)
			return imageFailureItem(prepared.inputPath, prepared.outputPath, cancelErr), cancelErr
		}

		jobErr := models.NewCanonicalJobError("IMAGE_COMPRESS_EXECUTION", execErr.Error(), map[string]any{"inputPath": prepared.inputPath})
		return imageFailureItem(prepared.inputPath, prepared.outputPath, jobErr), jobErr
	}

	if onProgress != nil {
		onProgress(models.JobProgressV1{Current: 1, Total: 1, Stage: models.JobStatusRunning, Message: "processed 1/1"})
	}

	return compressSuccessItem(prepared, execution), nil
}

// ExecuteBatch compresses every planned input with per-item progress and
// per-item errors, mirroring the other image batch tools.
func (t *CompressTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, jobErr := parseCompressRequest(req)
	if jobErr != nil {
		return nil, jobErr
	}

	if parsed.mode != "batch" {
		return nil, models.NewCanonicalJobError("IMAGE_COMPRESS_MODE_INVALID", "mode must be batch", nil)
	}

	plan, planErr := planParsedCompressRequest(parsed)
	if planErr != nil {
		return nil, planErr
	}

	items := make([]models.JobResultItemV1, 0, len(plan.Outputs))
	var firstErr *models.JobErrorV1

	for idx, planned := range plan.Outputs {
		select {
		case <-ctx.Done():
			return items, compressCancelledError(ctx)
		default:
		}

		prepared := preparedCompressFromPlan(planned, plan.EffectiveOptions)
		execution, execErr := executeCompressToPath(ctx, prepared)
		if execErr != nil {
			itemErr := models.NewCanonicalJobError("IMAGE_COMPRESS_BATCH_ITEM", execErr.Error(), map[string]any{"inputPath": prepared.inputPath})
			if firstErr == nil {
				firstErr = itemErr
			}
			items = append(items, imageFailureItem(prepared.inputPath, prepared.outputPath, itemErr))
		} else {
			items = append(items, compressSuccessItem(prepared, execution))
		}

		if onProgress != nil {
			onProgress(models.JobProgressV1{Current: idx + 1, Total: len(plan.Outputs), Stage: models.JobStatusRunning, Message: fmt.Sprintf("processed %d/%d", idx+1, len(plan.Outputs))})
		}
	}

	return items, firstErr
}

func parseCompressRequest(req models.JobRequestV1) (compressRequest, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return compressRequest{}, models.NewCanonicalJobError("IMAGE_COMPRESS_MODE_INVALID", "mode must be single or batch", nil)
	}

	if len(req.InputPaths) == 0 {
		return compressRequest{}, models.NewCanonicalJobError("IMAGE_COMPRESS_INPUT_REQUIRED", "Choose at least one image to compress.", nil)
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return compressRequest{}, models.NewCanonicalJobError("IMAGE_COMPRESS_SINGLE_INPUT_COUNT", "single mode requires exactly one input", nil)
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawPath := range req.InputPaths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			return compressRequest{}, models.NewCanonicalJobError("IMAGE_COMPRESS_INPUT_REQUIRED", "inputPaths contains empty item", nil)
		}
		inputPaths = append(inputPaths, trimmed)
	}

	targetSizeBytes, targetErr := compressTargetSizeOption(req.Options)
	if targetErr != nil {
		return compressRequest{}, targetErr
	}

	format, formatErr := compressRequestedFormat(req.Options)
	if formatErr != nil {
		return compressRequest{}, formatErr
	}

	allowDownscale := true
	if optionSupplied(req.Options, "allowDownscale") {
		value, ok := boolOption(req.Options, "allowDownscale")
		if !ok {
			return compressRequest{}, models.NewCanonicalJobError("IMAGE_COMPRESS_OPTION_INVALID", "options.allowDownscale must be a boolean", nil)
		}
		allowDownscale = value
	}

	maxDimension, hasMaxDimension, maxErr := compressOptionalInt(req.Options, "maxDimension")
	if maxErr != nil {
		return compressRequest{}, maxErr
	}
	if hasMaxDimension && maxDimension <= 0 {
		return compressRequest{}, models.NewCanonicalJobError("IMAGE_COMPRESS_OPTION_INVALID", "options.maxDimension must be greater than zero", nil)
	}

	quality, hasQuality, qualityErr := compressOptionalInt(req.Options, "quality")
	if qualityErr != nil {
		return compressRequest{}, qualityErr
	}
	if hasQuality && (quality < 40 || quality > 100) {
		return compressRequest{}, models.NewCanonicalJobError("IMAGE_COMPRESS_OPTION_INVALID", "options.quality must be between 40 and 100", nil)
	}

	stripMetadata := true
	if optionSupplied(req.Options, "stripMetadata") {
		value, ok := boolOption(req.Options, "stripMetadata")
		if !ok {
			return compressRequest{}, models.NewCanonicalJobError("IMAGE_COMPRESS_OPTION_INVALID", "options.stripMetadata must be a boolean", nil)
		}
		stripMetadata = value
	}

	background, hasBackground, backgroundErr := backgroundOption(req.Options)
	if backgroundErr != nil {
		return compressRequest{}, models.NewCanonicalJobError("IMAGE_COMPRESS_BACKGROUND_INVALID", backgroundErr.Error(), nil)
	}

	var backgroundHex string
	if hasBackground {
		backgroundHex = fmt.Sprintf("#%02x%02x%02x", background.R, background.G, background.B)
	}

	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = convertOptionString(req.Options, "outputDir")
	}

	return compressRequest{
		mode:            mode,
		inputPaths:      inputPaths,
		outputPath:      convertOptionString(req.Options, "outputPath"),
		outputDir:       outputDir,
		targetSizeBytes: targetSizeBytes,
		format:          format,
		allowDownscale:  allowDownscale,
		maxDimension:    maxDimension,
		quality:         quality,
		stripMetadata:   stripMetadata,
		backgroundHex:   backgroundHex,
	}, nil
}

// compressTargetSizeOption reads the required positive byte target. Whole
// numeric values are accepted from JSON numbers and from textual callers.
func compressTargetSizeOption(options map[string]any) (int64, *models.JobErrorV1) {
	if !optionSupplied(options, "targetSizeBytes") {
		return 0, models.NewCanonicalJobError("IMAGE_COMPRESS_TARGET_REQUIRED", "options.targetSizeBytes is required", nil)
	}

	invalid := func() (int64, *models.JobErrorV1) {
		return 0, models.NewCanonicalJobError("IMAGE_COMPRESS_TARGET_INVALID", "options.targetSizeBytes must be a whole number of bytes greater than zero", nil)
	}

	var value int64
	switch typed := options["targetSizeBytes"].(type) {
	case int:
		value = int64(typed)
	case int32:
		value = int64(typed)
	case int64:
		value = typed
	case float32:
		if float64(typed) != math.Trunc(float64(typed)) || float64(typed) > math.MaxInt64 {
			return invalid()
		}
		value = int64(typed)
	case float64:
		if typed != math.Trunc(typed) || typed > math.MaxInt64 {
			return invalid()
		}
		value = int64(typed)
	case string:
		parsed, err := strconv.ParseInt(strings.TrimSpace(typed), 10, 64)
		if err != nil {
			return invalid()
		}
		value = parsed
	default:
		return invalid()
	}

	if value <= 0 {
		return invalid()
	}

	return value, nil
}

func compressRequestedFormat(options map[string]any) (string, *models.JobErrorV1) {
	raw, exists := options["format"]
	if !exists || raw == nil {
		return imageAutoFormat, nil
	}

	text, ok := raw.(string)
	if !ok {
		return "", models.NewCanonicalJobError("IMAGE_COMPRESS_FORMAT_INVALID", "format must be auto, jpeg, webp or png", nil)
	}

	format := normalizeFormatName(text)
	switch format {
	case "":
		return imageAutoFormat, nil
	case imageAutoFormat, "original":
		return imageAutoFormat, nil
	case "jpeg", "webp", "png":
		return format, nil
	default:
		return "", models.NewCanonicalJobError("IMAGE_COMPRESS_FORMAT_INVALID", fmt.Sprintf("format must be auto, jpeg, webp or png (got %q)", strings.TrimSpace(text)), nil)
	}
}

// compressOptionalInt reads an optional integer option. Supplied values of the
// wrong type or with a fraction are rejected so a typo never silently changes
// the encoder.
func compressOptionalInt(options map[string]any, key string) (int, bool, *models.JobErrorV1) {
	if !optionSupplied(options, key) {
		return 0, false, nil
	}

	if floatValue, isFloat := options[key].(float64); isFloat && floatValue != math.Trunc(floatValue) {
		return 0, true, models.NewCanonicalJobError("IMAGE_COMPRESS_OPTION_INVALID", fmt.Sprintf("options.%s must be an integer", key), nil)
	}

	value, ok := numericOption(options, key)
	if !ok {
		return 0, true, models.NewCanonicalJobError("IMAGE_COMPRESS_OPTION_INVALID", fmt.Sprintf("options.%s must be numeric", key), nil)
	}

	return value, true, nil
}

func validateCompressInputPath(inputPath string) *models.JobErrorV1 {
	trimmed := strings.TrimSpace(inputPath)
	if trimmed == "" {
		return models.NewCanonicalJobError("IMAGE_COMPRESS_INPUT_REQUIRED", "inputPath is required", nil)
	}

	info, statErr := os.Stat(trimmed)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return models.NewCanonicalJobError(
				"IMAGE_COMPRESS_INPUT_NOT_FOUND",
				fmt.Sprintf("%s was not found.", filepath.Base(trimmed)),
				map[string]any{"inputPath": trimmed},
			)
		}

		return models.NewCanonicalJobError(
			"IMAGE_COMPRESS_INPUT_NOT_FOUND",
			fmt.Sprintf("%s cannot be read.", filepath.Base(trimmed)),
			map[string]any{"inputPath": trimmed},
		)
	}

	if info.IsDir() {
		return models.NewCanonicalJobError(
			"IMAGE_COMPRESS_INPUT_UNSUPPORTED",
			fmt.Sprintf("%s is a folder, not an image.", filepath.Base(trimmed)),
			map[string]any{"inputPath": trimmed},
		)
	}

	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(trimmed), "."))
	if !compressSupportsInputExtension(extension) {
		return models.NewCanonicalJobError(
			"IMAGE_COMPRESS_INPUT_UNSUPPORTED",
			fmt.Sprintf("%s is not a supported image. Use %s.", filepath.Base(trimmed), strings.Join(imageInputExtensions, ", ")),
			map[string]any{"inputPath": trimmed, "extension": extension},
		)
	}

	return nil
}

func compressSupportsInputExtension(extension string) bool {
	for _, supported := range imageInputExtensions {
		if supported == extension {
			return true
		}
	}

	return false
}

// resolveCompressOutputFormat keeps an explicit format untouched and resolves
// the auto mode per input.
func resolveCompressOutputFormat(inputPath, requested string) string {
	if requested != imageAutoFormat {
		return requested
	}

	return compressAutoFormat(inputPath)
}

// compressAutoFormat keeps JPEG, WebP and PNG as-is. Formats the tool refuses to
// write go to JPEG, or to PNG when the source carries transparency.
func compressAutoFormat(inputPath string) string {
	switch format := normalizeFormatName(strings.TrimPrefix(filepath.Ext(inputPath), ".")); format {
	case "jpeg", "webp", "png":
		return format
	default:
		if compressInputHasAlpha(inputPath) {
			return "png"
		}
		return "jpeg"
	}
}

// compressInputHasAlpha reads just enough metadata to know whether the source
// has an alpha channel. A read failure leaves the format on the fallback path;
// execution reports the real error later.
func compressInputHasAlpha(inputPath string) bool {
	data, err := os.ReadFile(inputPath)
	if err != nil {
		return false
	}

	metadata, metaErr := bimg.NewImage(data).Metadata()
	if metaErr != nil {
		return false
	}

	return metadata.Alpha
}

// planSingleCompressOutput resolves the one output of a single request.
func planSingleCompressOutput(parsed compressRequest, outputFmt string, used map[string]struct{}) (models.PlannedOutputV1, *models.JobErrorV1) {
	inputPath := parsed.inputPaths[0]

	if explicitOutput := strings.TrimSpace(parsed.outputPath); explicitOutput != "" {
		return planExplicitCompressOutput(inputPath, explicitOutput)
	}

	targetDir := strings.TrimSpace(parsed.outputDir)
	if targetDir == "" {
		targetDir = filepath.Dir(inputPath)
	}

	return planAutoCompressOutput(inputPath, filepath.Clean(targetDir), outputFmt, used), nil
}

func planExplicitCompressOutput(inputPath, explicitOutput string) (models.PlannedOutputV1, *models.JobErrorV1) {
	planned := models.PlannedOutputV1{
		InputPath:  inputPath,
		OutputPath: filepath.Clean(strings.TrimSpace(explicitOutput)),
		OutputKind: imageOutputKindFile,
	}

	if sameFilePath(inputPath, planned.OutputPath) {
		return planned, models.NewCanonicalJobError("IMAGE_COMPRESS_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
	}

	info, statErr := os.Stat(planned.OutputPath)
	switch {
	case statErr == nil:
		planned.Exists = true
		if info.IsDir() {
			return planned, models.NewCanonicalJobError(
				"IMAGE_COMPRESS_OUTPUT_DIR_INVALID",
				fmt.Sprintf("%s is a folder, not a file.", filepath.Base(planned.OutputPath)),
				map[string]any{"outputPath": planned.OutputPath},
			)
		}
		return planned, models.NewCanonicalJobError(
			"IMAGE_COMPRESS_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another name.", filepath.Base(planned.OutputPath)),
			map[string]any{"outputPath": planned.OutputPath},
		)
	case !errors.Is(statErr, os.ErrNotExist):
		return planned, models.NewCanonicalJobError(
			"IMAGE_COMPRESS_OUTPUT_DIR_INVALID",
			fmt.Sprintf("%s cannot be accessed. Choose another location.", filepath.Base(planned.OutputPath)),
			map[string]any{"outputPath": planned.OutputPath},
		)
	}

	return planned, nil
}

// planAutoCompressOutput derives a collision-safe name inside dir. It never
// writes.
func planAutoCompressOutput(inputPath, dir, outputFmt string, used map[string]struct{}) models.PlannedOutputV1 {
	planned := models.PlannedOutputV1{
		InputPath:  inputPath,
		OutputKind: imageOutputKindFile,
	}

	base := compressOutputBaseName(inputPath)
	outputPath, adjusted := outputplan.UniqueFilePath(dir, base, outputFmt, used)
	planned.OutputPath = outputPath

	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", base+"."+outputFmt, filepath.Base(outputPath)))
	}

	return planned
}

func compressOutputBaseName(inputPath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		stem = "image"
	}

	return stem + "_compressed"
}

func validateCompressOutputDir(outputDir string) *models.JobErrorV1 {
	info, statErr := os.Stat(outputDir)
	if statErr != nil {
		return models.NewCanonicalJobError("IMAGE_COMPRESS_OUTPUT_DIR_INVALID", fmt.Sprintf("outputDir is not accessible: %v", statErr), nil)
	}
	if !info.IsDir() {
		return models.NewCanonicalJobError("IMAGE_COMPRESS_OUTPUT_DIR_INVALID", fmt.Sprintf("outputDir is not a folder: %s", outputDir), nil)
	}

	return nil
}

func compressPlanMessage(count int) string {
	if count == 1 {
		return "Ready to compress 1 image."
	}

	return fmt.Sprintf("Ready to compress %d images.", count)
}

func compressEffectiveOptions(parsed compressRequest, formats []string) map[string]any {
	effective := map[string]any{
		"targetSizeBytes": parsed.targetSizeBytes,
		"allowDownscale":  parsed.allowDownscale,
		"stripMetadata":   parsed.stripMetadata,
	}

	if parsed.maxDimension > 0 {
		effective["maxDimension"] = parsed.maxDimension
	}
	if parsed.quality > 0 {
		effective["quality"] = parsed.quality
	}
	if parsed.backgroundHex != "" {
		effective["background"] = parsed.backgroundHex
	}
	if format, ok := commonPlannedFormat(formats); ok {
		effective["format"] = format
	}

	return effective
}

// preparedCompressFromPlan turns a planned output into executor input without
// resolving any path again.
func preparedCompressFromPlan(planned models.PlannedOutputV1, effective map[string]any) preparedCompress {
	targetSizeBytes := compressInt64Option(effective, "targetSizeBytes")

	allowDownscale := true
	if value, ok := boolOption(effective, "allowDownscale"); ok {
		allowDownscale = value
	}

	stripMetadata := true
	if value, ok := boolOption(effective, "stripMetadata"); ok {
		stripMetadata = value
	}

	maxDimension, _ := numericOption(effective, "maxDimension")
	quality, _ := numericOption(effective, "quality")

	background, hasBackground, _ := backgroundOption(effective)
	var backgroundPtr *bimg.Color
	if hasBackground {
		color := background
		backgroundPtr = &color
	}

	return preparedCompress{
		inputPath:       planned.InputPath,
		outputPath:      planned.OutputPath,
		outputFmt:       plannedOutputFormat(planned, effective),
		targetSizeBytes: targetSizeBytes,
		allowDownscale:  allowDownscale,
		maxDimension:    maxDimension,
		quality:         quality,
		stripMetadata:   stripMetadata,
		background:      backgroundPtr,
	}
}

func compressInt64Option(options map[string]any, key string) int64 {
	if options == nil {
		return 0
	}

	switch value := options[key].(type) {
	case int:
		return int64(value)
	case int32:
		return int64(value)
	case int64:
		return value
	case float32:
		return int64(value)
	case float64:
		return int64(value)
	default:
		return 0
	}
}

func compressCancelledError(ctx context.Context) *models.JobErrorV1 {
	message := fmt.Errorf(OperationCancelledMsg, ctx.Err()).Error()
	return models.NewCanonicalJobError("IMAGE_COMPRESS_CANCELLED", message, nil)
}

func executeCompressToPath(ctx context.Context, prepared preparedCompress) (compressExecution, error) {
	select {
	case <-ctx.Done():
		return compressExecution{}, fmt.Errorf(OperationCancelledMsg, ctx.Err())
	default:
	}

	input, err := os.ReadFile(prepared.inputPath)
	if err != nil {
		return compressExecution{}, fmt.Errorf("read input failed: %w", err)
	}

	select {
	case <-ctx.Done():
		return compressExecution{}, fmt.Errorf(OperationCancelledMsg, ctx.Err())
	default:
	}

	output, outcome, compressErr := CompressToTarget(input, CompressOptions{
		TargetSizeBytes: prepared.targetSizeBytes,
		Format:          prepared.outputFmt,
		AllowDownscale:  prepared.allowDownscale,
		MaxDimension:    prepared.maxDimension,
		Quality:         prepared.quality,
		StripMetadata:   prepared.stripMetadata,
		Background:      prepared.background,
	})
	if compressErr != nil {
		return compressExecution{}, fmt.Errorf("compress failed: %w", compressErr)
	}

	select {
	case <-ctx.Done():
		return compressExecution{}, fmt.Errorf(OperationCancelledMsg, ctx.Err())
	default:
	}

	if mkErr := os.MkdirAll(filepath.Dir(prepared.outputPath), 0o755); mkErr != nil {
		return compressExecution{}, fmt.Errorf("create output dir failed: %w", mkErr)
	}

	if writeErr := os.WriteFile(prepared.outputPath, output, DefaultFilePermissions); writeErr != nil {
		return compressExecution{}, fmt.Errorf("write output failed: %w", writeErr)
	}

	return compressExecution{inputBytes: int64(len(input)), outcome: outcome}, nil
}

func compressSuccessItem(prepared preparedCompress, execution compressExecution) models.JobResultItemV1 {
	return models.JobResultItemV1{
		InputPath:   prepared.inputPath,
		OutputPath:  prepared.outputPath,
		Outputs:     []string{prepared.outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     compressResultMessage(execution, prepared.targetSizeBytes),
	}
}

// compressResultMessage reports the honest size result: the original and final
// size, the target, and whether the goal was already met, reached or missed.
func compressResultMessage(execution compressExecution, targetSizeBytes int64) string {
	message := fmt.Sprintf(
		"%s → %s (target %s)",
		compressFormatBytes(execution.inputBytes),
		compressFormatBytes(execution.outcome.SizeBytes),
		compressFormatBytes(targetSizeBytes),
	)

	if execution.outcome.AlreadyUnderTarget {
		return message + ", already under target"
	}
	if !execution.outcome.TargetMet {
		return message + ", target not reached (best effort)"
	}

	return message
}

// compressFormatBytes renders a byte count with the largest unit that keeps the
// value below 1024, trimming a trailing zero decimal.
func compressFormatBytes(size int64) string {
	if size < 1024 {
		return fmt.Sprintf("%d B", size)
	}

	value := float64(size)
	unit := "KB"
	for _, candidate := range []string{"KB", "MB", "GB", "TB"} {
		value /= 1024
		unit = candidate
		if value < 1024 || candidate == "TB" {
			break
		}
	}

	text := fmt.Sprintf("%.1f %s", value, unit)
	return strings.Replace(text, ".0 ", " ", 1)
}
