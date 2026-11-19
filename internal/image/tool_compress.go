package image

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/platform/fsx"
	"fileforge-desktop/internal/utils"

	"github.com/h2non/bimg"
)

const ToolIDImageCompressV1 = "tool.image.compress"

const compressAnimatedWarning = "animated input: only the first frame is compressed"

const compressPNGQualityWarning = "quality applies to JPEG and WebP; PNG uses palette quantisation"

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
	return ToolIDImageCompressV1
}

func (t *CompressTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Image Compressor",
		Description:      "Compress images to a target file size with quality search and optional downscaling",
		Domain:           "image",
		Capability:       t.Capability(),
		Version:          "v1",
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
	inputWarnings := make([]string, 0)
	for _, inputPath := range parsed.inputPaths {
		if inputErr := validateCompressInputPath(inputPath); inputErr != nil {
			return planFailure(plan, inputErr)
		}
		formats = append(formats, resolveCompressOutputFormat(inputPath, parsed.format))

		if compressInputFrameCount(inputPath) > 1 {
			inputWarnings = append(inputWarnings, fmt.Sprintf("%s: %s", filepath.Base(inputPath), compressAnimatedWarning))
		}
	}

	optionWarnings := make([]string, 0, 1)
	if parsed.quality > 0 && compressFormatsUsePNG(formats) {
		optionWarnings = append(optionWarnings, compressPNGQualityWarning)
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
		plan.Warnings = append(append([]string{}, planned.Warnings...), inputWarnings...)
		plan.Warnings = append(plan.Warnings, optionWarnings...)
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
	plan.Warnings = append(warnings, inputWarnings...)
	plan.Warnings = append(plan.Warnings, optionWarnings...)
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
		jobErr, _ := compressExecutionError(ctx, "IMAGE_COMPRESS_EXECUTION", prepared.inputPath, execErr)
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

	// Resolve every prepared item sequentially; execution is what runs in
	// parallel.
	preparedItems := make([]preparedCompress, len(plan.Outputs))
	for index, planned := range plan.Outputs {
		preparedItems[index] = preparedCompressFromPlan(planned, plan.EffectiveOptions)
	}

	total := len(preparedItems)
	items := make([]models.JobResultItemV1, total)
	completed := make([]bool, total)
	progress := newParallelProgress(total, onProgress)

	runErr := utils.ParallelForIndex(ctx, total, utils.ParallelWorkerCount(), func(ctx context.Context, index int) error {
		prepared := preparedItems[index]
		execution, execErr := executeCompressToPath(ctx, prepared)
		if execErr != nil {
			// A cancelled item must not be reported as a retryable batch
			// failure; the post-loop cancellation check returns the canonical
			// cancellation error instead.
			itemErr, _ := compressExecutionError(ctx, "IMAGE_COMPRESS_BATCH_ITEM", prepared.inputPath, execErr)
			items[index] = imageFailureItem(prepared.inputPath, prepared.outputPath, itemErr)
		} else {
			items[index] = compressSuccessItem(prepared, execution)
		}
		completed[index] = true
		progress.itemCompleted()

		return nil
	})

	if batchRunCancelled(ctx, runErr) {
		return collectCompletedItems(items, completed), compressCancelledError(ctx)
	}

	return items, firstFailedItemError(items)
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

// compressTargetSizeOption reads the required positive byte target. The numeric
// surface mirrors internal/pdf/tool_optimize.go: every signed and unsigned
// integer width, json.Number, and float32/float64 only when whole and finite.
func compressTargetSizeOption(options map[string]any) (int64, *models.JobErrorV1) {
	if !optionSupplied(options, "targetSizeBytes") {
		return 0, models.NewCanonicalJobError("IMAGE_COMPRESS_TARGET_REQUIRED", "options.targetSizeBytes is required", nil)
	}

	invalid := func() (int64, *models.JobErrorV1) {
		return 0, models.NewCanonicalJobError("IMAGE_COMPRESS_TARGET_INVALID", "options.targetSizeBytes must be a whole number of bytes greater than zero", nil)
	}

	switch value := options["targetSizeBytes"].(type) {
	case int:
		if value <= 0 {
			return invalid()
		}
		return int64(value), nil
	case int8:
		if value <= 0 {
			return invalid()
		}
		return int64(value), nil
	case int16:
		if value <= 0 {
			return invalid()
		}
		return int64(value), nil
	case int32:
		if value <= 0 {
			return invalid()
		}
		return int64(value), nil
	case int64:
		if value <= 0 {
			return invalid()
		}
		return value, nil
	case uint:
		if value == 0 {
			return invalid()
		}
		return compressFloatTargetSize(float64(value))
	case uint8:
		if value == 0 {
			return invalid()
		}
		return int64(value), nil
	case uint16:
		if value == 0 {
			return invalid()
		}
		return int64(value), nil
	case uint32:
		if value == 0 {
			return invalid()
		}
		return int64(value), nil
	case uint64:
		return compressFloatTargetSize(float64(value))
	case float32:
		return compressFloatTargetSize(float64(value))
	case float64:
		return compressFloatTargetSize(value)
	case json.Number:
		if integer, err := value.Int64(); err == nil {
			if integer <= 0 {
				return invalid()
			}
			return integer, nil
		}

		parsed, err := value.Float64()
		if err != nil {
			return invalid()
		}
		return compressFloatTargetSize(parsed)
	default:
		return invalid()
	}
}

func compressFloatTargetSize(value float64) (int64, *models.JobErrorV1) {
	invalid := func() (int64, *models.JobErrorV1) {
		return 0, models.NewCanonicalJobError("IMAGE_COMPRESS_TARGET_INVALID", "options.targetSizeBytes must be a whole number of bytes greater than zero", nil)
	}

	if math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return invalid()
	}
	if value > float64(math.MaxInt64) {
		return invalid()
	}
	if math.Trunc(value) != value {
		return invalid()
	}

	converted := int64(value)
	if converted <= 0 {
		return invalid()
	}

	return converted, nil
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

// compressInputFrameCount reports the number of frames bimg would keep for the
// input. bimg v1.1.9 does not expose a page count, so animated GIFs are counted
// structurally without decoding pixels. Other containers report one frame.
func compressInputFrameCount(inputPath string) int {
	if normalizeFormatName(strings.TrimPrefix(filepath.Ext(inputPath), ".")) != "gif" {
		return 1
	}

	data, err := os.ReadFile(inputPath)
	if err != nil {
		return 1
	}

	frames := compressGIFFrameCount(data)
	if frames < 1 {
		return 1
	}

	return frames
}

// compressGIFFrameCount walks the GIF block structure and counts image
// descriptors. It never decodes pixels and returns 0 for an unparseable stream.
func compressGIFFrameCount(data []byte) int {
	if len(data) < 13 {
		return 0
	}
	header := string(data[:6])
	if header != "GIF87a" && header != "GIF89a" {
		return 0
	}

	pos := 13
	// The logical screen descriptor flags live in the low 3 bits of the global
	// color table size; a set high bit means the table is present.
	if data[10]&0x80 != 0 {
		pos += 3 * (1 << ((data[10] & 0x07) + 1))
	}

	frames := 0
	for pos < len(data) {
		switch data[pos] {
		case 0x3B: // trailer
			return frames
		case 0x21: // extension introducer plus label
			if pos+2 > len(data) {
				return frames
			}
			next, ok := compressGIFSkipSubBlocks(data, pos+2)
			if !ok {
				return frames
			}
			pos = next
		case 0x2C: // image descriptor
			if pos+10 > len(data) {
				return frames
			}
			frames++

			flags := data[pos+9]
			pos += 10
			if flags&0x80 != 0 {
				pos += 3 * (1 << ((flags & 0x07) + 1))
			}

			// LZW minimum code size, then the image data sub-blocks.
			next, ok := compressGIFSkipSubBlocks(data, pos+1)
			if !ok {
				return frames
			}
			pos = next
		default:
			return frames
		}
	}

	return frames
}

// compressGIFSkipSubBlocks advances past a chain of length-prefixed blocks and
// returns the position after the terminating zero-length block.
func compressGIFSkipSubBlocks(data []byte, pos int) (int, bool) {
	for pos < len(data) {
		size := int(data[pos])
		pos++
		if size == 0 {
			return pos, true
		}
		pos += size
	}

	return pos, false
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
		return planExplicitCompressOutput(inputPath, explicitOutput, outputFmt)
	}

	targetDir := strings.TrimSpace(parsed.outputDir)
	if targetDir == "" {
		targetDir = filepath.Dir(inputPath)
	}

	return planAutoCompressOutput(inputPath, filepath.Clean(targetDir), outputFmt, used), nil
}

func planExplicitCompressOutput(inputPath, explicitOutput, outputFormat string) (models.PlannedOutputV1, *models.JobErrorV1) {
	planned := models.PlannedOutputV1{
		InputPath:  inputPath,
		OutputPath: filepath.Clean(strings.TrimSpace(explicitOutput)),
		OutputKind: imageOutputKindFile,
	}

	if !compressOutputExtensionMatches(planned.OutputPath, outputFormat) {
		return planned, models.NewCanonicalJobError(
			"IMAGE_COMPRESS_OUTPUT_EXTENSION_MISMATCH",
			compressOutputExtensionMessage(planned.OutputPath, outputFormat),
			map[string]any{"outputPath": planned.OutputPath, "format": outputFormat},
		)
	}

	if fsx.SameFile(inputPath, planned.OutputPath) {
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

// compressOutputExtensionMatches reports whether the explicit output path
// extension agrees with the encoder format. The jpg alias folds into jpeg.
func compressOutputExtensionMatches(outputPath, outputFormat string) bool {
	extension := normalizeFormatName(strings.TrimPrefix(filepath.Ext(outputPath), "."))
	return extension != "" && extension == outputFormat
}

func compressOutputExtensionMessage(outputPath, outputFormat string) string {
	expected := "." + outputFormat
	if outputFormat == "jpeg" {
		expected = ".jpg or .jpeg"
	}

	return fmt.Sprintf("%s does not match the %s output format. Use a %s file.", filepath.Base(outputPath), outputFormat, expected)
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
		return models.NewCanonicalJobError(
			"IMAGE_COMPRESS_OUTPUT_DIR_INVALID",
			"The output folder cannot be used.",
			map[string]any{"outputDir": outputDir, "error": statErr.Error()},
		)
	}
	if !info.IsDir() {
		return models.NewCanonicalJobError(
			"IMAGE_COMPRESS_OUTPUT_DIR_INVALID",
			fmt.Sprintf("%s is not a folder.", outputDir),
			map[string]any{"outputDir": outputDir},
		)
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
	if parsed.backgroundHex != "" {
		effective["background"] = parsed.backgroundHex
	}
	if format, ok := commonPlannedFormat(formats); ok {
		effective["format"] = format
	}

	// Every output keeps the clamped ceiling so mixed batches execute each item
	// with the requested quality. PNG ignores it and quantises through the
	// palette ladder; the plan warning explains that difference.
	effective["quality"] = compressEffectiveQuality(parsed.quality)

	return effective
}

// compressEffectiveQuality reports the ceiling the encoder will actually use.
func compressEffectiveQuality(quality int) int {
	if quality <= 0 || quality > compressMaxQuality {
		return compressMaxQuality
	}

	return quality
}

func compressFormatsUsePNG(formats []string) bool {
	for _, format := range formats {
		if format == "png" {
			return true
		}
	}

	return false
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

// compressCancelledError builds the canonical cancellation error. It falls back
// to context.Canceled when the context has no recorded cause.
func compressCancelledError(ctx context.Context) *models.JobErrorV1 {
	var cause error = context.Canceled
	if ctx != nil && ctx.Err() != nil {
		cause = ctx.Err()
	}

	message := fmt.Errorf(OperationCancelledMsg, cause).Error()
	return models.NewCanonicalJobError("IMAGE_COMPRESS_CANCELLED", message, nil)
}

// compressExecutionError maps a failed execution to a job error. Cancellation
// wins over the generic execution/item code so the job engine reports
// CANCELLED_BY_USER, including when the last batch item is interrupted. The raw
// cause stays in Details while the message remains user-facing.
func compressExecutionError(ctx context.Context, detailCode, inputPath string, execErr error) (*models.JobErrorV1, bool) {
	if compressIsCancellation(ctx, execErr) {
		return compressCancelledError(ctx), true
	}

	details := map[string]any{"inputPath": inputPath}
	if execErr != nil {
		details["error"] = execErr.Error()
	}

	return models.NewCanonicalJobError(detailCode, "Could not compress the image.", details), false
}

func compressIsCancellation(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}

	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
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

	output, outcome, compressErr := CompressToTargetContext(ctx, input, CompressOptions{
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
		message += ", already under target"
	}
	if execution.outcome.MetadataPreserved {
		message += ", metadata kept (stripping would exceed the target or grow the file)"
	}
	if !execution.outcome.TargetMet {
		message += ", target not reached (best effort)"
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
