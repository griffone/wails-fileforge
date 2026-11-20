package image

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image"
	"image/color"
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

const ToolIDImageWatermark = "tool.image.watermark"

const (
	watermarkDefaultMarginPercent    = 3.0
	watermarkDefaultTextScalePercent = 5.0
	watermarkDefaultLogoScalePercent = 20.0
	watermarkDefaultOpacity          = 0.6
	watermarkDefaultTileRotation     = -30.0
	watermarkToolMinQuality          = 40
	watermarkAnimatedWarning         = "animated GIF input: only the first frame is watermarked"
	watermarkPNGQualityWarning       = "quality applies to JPEG and WebP; PNG is lossless"
	watermarkLogoExtensionsMessage   = "png, jpg, jpeg, webp"
	watermarkOutputSuffix            = "_watermarked"
)

// WatermarkTool implements the image watermark tool contract.
type WatermarkTool struct{}

// watermarkRequest is the parsed, normalized request. The logo bytes are read
// exactly once here so planning and execution never touch the logo file again.
type watermarkRequest struct {
	mode            string
	inputPaths      []string
	outputPath      string
	outputDir       string
	kind            string
	text            string
	bold            bool
	color           string
	logoPath        string
	logoBytes       []byte
	anchor          string
	marginPercent   float64
	scalePercent    float64
	opacity         float64
	tile            bool
	rotationDegrees float64
	format          string
	quality         int
	qualitySupplied bool
}

// preparedWatermark is one resolved input/output pair ready for execution.
type preparedWatermark struct {
	inputPath  string
	outputPath string
	options    WatermarkOptions
}

func NewWatermarkTool() *WatermarkTool {
	return &WatermarkTool{}
}

func (t *WatermarkTool) ID() string {
	return ToolIDImageWatermark
}

func (t *WatermarkTool) Capability() string {
	return ToolIDImageWatermark
}

func (t *WatermarkTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "Image Watermark",
		Description:      "Watermark images with text or a logo using nine anchors, opacity, scale, margin and tiling",
		Domain:           "image",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  append([]string(nil), imageInputExtensions...),
		OutputExtensions: []string{"jpeg", "png", "webp"},
		RuntimeDeps:      []string{"libvips"},
		Tags:             []string{"image", "watermark"},
	}
}

func (t *WatermarkTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *WatermarkTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe <stem>_watermarked.<ext> destination per
// input and validates every option without encoding anything. An explicit
// options.outputPath keeps the single request contract: it must differ from the
// input, agree with the resolved format, and not exist yet.
func (t *WatermarkTool) Plan(_ context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan, _, planErr := t.planRequest(req)
	return plan, planErr
}

// planRequest also returns the prepared items so execution reuses the logo
// bytes and background decisions resolved during planning.
func (t *WatermarkTool) planRequest(req models.JobRequest) (models.PlanJobResponse, []preparedWatermark, *models.JobError) {
	parsed, parseErr := parseWatermarkRequest(req)
	if parseErr != nil {
		plan, _ := planFailure(models.PlanJobResponse{Outputs: []models.PlannedOutput{}}, parseErr)
		return plan, nil, parseErr
	}

	return planParsedWatermarkRequest(parsed)
}

// ExecuteSingle implements tools.SingleExecutor by delegating to the progress
// variant with a no-op callback.
func (t *WatermarkTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

// ExecuteSingleWithProgress resolves the plan once, executes exactly that plan
// and reports stage level progress for the one-item job.
func (t *WatermarkTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	inputPath := firstInputPath(req.InputPaths)

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return imageFailureItem(inputPath, plannedOutputPath(plan), planErr), planErr
	}

	if onProgress != nil {
		onProgress(models.JobProgress{Current: 0, Total: 1, Stage: models.JobStatusRunning, Message: "watermarking 1/1"})
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, onProgress)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("IMAGE_WATERMARK_EXECUTION", "watermark returned no result", nil)
	}

	return imageFailureItem(inputPath, plannedOutputPath(plan), jobErr), jobErr
}

// ExecuteBatch plans the request once and then executes exactly that plan. It
// never re-resolves output paths from the request.
func (t *WatermarkTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := parseWatermarkRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}

	if parsed.mode != "batch" {
		return nil, models.NewCanonicalJobError("IMAGE_WATERMARK_MODE_INVALID", "mode must be batch", nil)
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan watermarks exactly the outputs resolved by Plan. The per-input
// formats and alphas travel in the effective options, so a reduced retry plan
// only processes the failed items. Results keep plan order.
func (t *WatermarkTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	// Parsing reads the logo bytes and normalizes the options; it never
	// resolves output paths, so execution cannot drift from the plan.
	parsed, parseErr := parseWatermarkRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}

	total := len(plan.Outputs)
	if total == 0 {
		return nil, models.NewCanonicalJobError("IMAGE_WATERMARK_INPUT_REQUIRED", "Choose at least one image to watermark.", nil)
	}

	formats := planStringSlice(plan.EffectiveOptions, "formats")
	alphas := planBoolSlice(plan.EffectiveOptions, "alphas")

	// Resolve every prepared item sequentially; execution is what runs in
	// parallel.
	preparedItems := make([]preparedWatermark, total)
	for index, planned := range plan.Outputs {
		format := ""
		if index < len(formats) {
			format = formats[index]
		}
		if format == "" {
			format = plannedOutputFormat(planned, plan.EffectiveOptions)
		}

		preparedItems[index] = preparedWatermark{
			inputPath:  planned.InputPath,
			outputPath: planned.OutputPath,
			options:    watermarkEngineOptions(parsed, format, index < len(alphas) && alphas[index]),
		}
	}

	detailCode := "IMAGE_WATERMARK_BATCH_ITEM"
	if req.Mode == "single" {
		detailCode = "IMAGE_WATERMARK_EXECUTION"
	}

	items := make([]models.JobResultItem, total)
	completed := make([]bool, total)
	progress := newParallelProgress(total, onProgress)

	runErr := utils.ParallelForIndex(ctx, total, utils.ParallelWorkerCount(), func(ctx context.Context, index int) error {
		item := preparedItems[index]
		outcome, execErr := executeWatermarkToPath(ctx, item)
		if execErr != nil {
			// A cancelled item must not be reported as a retryable batch
			// failure; the post-loop cancellation check returns the canonical
			// cancellation error instead.
			itemErr, _ := watermarkExecutionError(ctx, detailCode, item.inputPath, execErr)
			items[index] = imageFailureItem(item.inputPath, item.outputPath, itemErr)
		} else {
			items[index] = watermarkSuccessItem(item, outcome)
		}
		completed[index] = true
		progress.itemCompleted()

		return nil
	})

	if batchRunCancelled(ctx, runErr) {
		return collectCompletedItems(items, completed), watermarkCancelledError(ctx)
	}

	return items, firstFailedItemError(items)
}

func parseWatermarkRequest(req models.JobRequest) (watermarkRequest, *models.JobError) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return watermarkRequest{}, models.NewCanonicalJobError("IMAGE_WATERMARK_MODE_INVALID", "mode must be single or batch", nil)
	}

	if len(req.InputPaths) == 0 {
		return watermarkRequest{}, models.NewCanonicalJobError("IMAGE_WATERMARK_INPUT_REQUIRED", "Choose at least one image to watermark.", nil)
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return watermarkRequest{}, models.NewCanonicalJobError("IMAGE_WATERMARK_SINGLE_INPUT_COUNT", "single mode requires exactly one input", nil)
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawPath := range req.InputPaths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			return watermarkRequest{}, models.NewCanonicalJobError("IMAGE_WATERMARK_INPUT_REQUIRED", "inputPaths contains empty item", nil)
		}
		inputPaths = append(inputPaths, trimmed)
	}

	kind, kindErr := watermarkKindOption(req.Options)
	if kindErr != nil {
		return watermarkRequest{}, kindErr
	}

	text, textErr := watermarkTextOption(req.Options)
	if textErr != nil {
		return watermarkRequest{}, textErr
	}
	text = watermarkNormalizeText(text)
	if textErr := watermarkValidateTextOption(kind, text); textErr != nil {
		return watermarkRequest{}, textErr
	}

	var logoPath string
	var logoBytes []byte
	if kind == watermarkKindLogo {
		loadedPath, loadedBytes, logoErr := watermarkLogoOption(req.Options)
		if logoErr != nil {
			return watermarkRequest{}, logoErr
		}
		logoPath = loadedPath
		logoBytes = loadedBytes
	}

	anchor, anchorErr := watermarkAnchorOption(req.Options)
	if anchorErr != nil {
		return watermarkRequest{}, anchorErr
	}

	marginPercent, marginErr := watermarkBoundedFloatOption(req.Options, "marginPercent", watermarkDefaultMarginPercent, watermarkMinMarginPercent, watermarkMaxMarginPercent, "IMAGE_WATERMARK_MARGIN_INVALID")
	if marginErr != nil {
		return watermarkRequest{}, marginErr
	}

	scaleDefault := watermarkDefaultTextScalePercent
	scaleMin, scaleMax := watermarkMinTextScalePercent, watermarkMaxTextScalePercent
	if kind == watermarkKindLogo {
		scaleDefault = watermarkDefaultLogoScalePercent
		scaleMin, scaleMax = watermarkMinLogoScalePercent, watermarkMaxLogoScalePercent
	}
	scalePercent, scaleErr := watermarkBoundedFloatOption(req.Options, "scalePercent", scaleDefault, scaleMin, scaleMax, "IMAGE_WATERMARK_SCALE_INVALID")
	if scaleErr != nil {
		return watermarkRequest{}, scaleErr
	}

	opacity, opacityErr := watermarkBoundedFloatOption(req.Options, "opacity", watermarkDefaultOpacity, watermarkMinOpacity, watermarkMaxOpacity, "IMAGE_WATERMARK_OPACITY_INVALID")
	if opacityErr != nil {
		return watermarkRequest{}, opacityErr
	}

	rotationDegrees, rotationErr := watermarkBoundedFloatOption(req.Options, "rotationDegrees", watermarkDefaultTileRotation, watermarkMinRotationDegrees, watermarkMaxRotationDegrees, "IMAGE_WATERMARK_ROTATION_INVALID")
	if rotationErr != nil {
		return watermarkRequest{}, rotationErr
	}

	bold := true
	if optionSupplied(req.Options, "bold") {
		value, ok := boolOption(req.Options, "bold")
		if !ok {
			return watermarkRequest{}, models.NewCanonicalJobError("IMAGE_WATERMARK_OPTION_INVALID", "options.bold must be a boolean", nil)
		}
		bold = value
	}

	if kind == watermarkKindText {
		face, faceErr := watermarkFontFace(bold, watermarkMinFontSize)
		if faceErr != nil {
			return watermarkRequest{}, models.NewCanonicalJobError("IMAGE_WATERMARK_TEXT_UNSUPPORTED", faceErr.Error(), nil)
		}
		if unsupported, ok := watermarkUnsupportedRune(face, text); ok {
			return watermarkRequest{}, models.NewCanonicalJobError(
				"IMAGE_WATERMARK_TEXT_UNSUPPORTED",
				watermarkUnsupportedRuneError(unsupported).Error(),
				map[string]any{"rune": string(unsupported), "codepoint": fmt.Sprintf("U+%04X", unsupported)},
			)
		}
	}

	tile := false
	if optionSupplied(req.Options, "tile") {
		value, ok := boolOption(req.Options, "tile")
		if !ok {
			return watermarkRequest{}, models.NewCanonicalJobError("IMAGE_WATERMARK_OPTION_INVALID", "options.tile must be a boolean", nil)
		}
		tile = value
	}
	if tile && kind == watermarkKindLogo {
		return watermarkRequest{}, models.NewCanonicalJobError("IMAGE_WATERMARK_TILE_UNSUPPORTED", "options.tile is only supported for text watermarks", nil)
	}

	color, colorErr := watermarkColorOption(req.Options)
	if colorErr != nil {
		return watermarkRequest{}, colorErr
	}

	format, formatErr := watermarkRequestedFormat(req.Options)
	if formatErr != nil {
		return watermarkRequest{}, formatErr
	}

	quality := watermarkDefaultQuality
	qualitySupplied := optionSupplied(req.Options, "quality")
	if qualitySupplied {
		value, _, qualityErr := watermarkOptionalInt(req.Options, "quality")
		if qualityErr != nil {
			return watermarkRequest{}, qualityErr
		}
		if value < watermarkToolMinQuality || value > watermarkMaxQuality {
			return watermarkRequest{}, models.NewCanonicalJobError("IMAGE_WATERMARK_QUALITY_INVALID", "options.quality must be between 40 and 100", nil)
		}
		quality = value
	}

	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = convertOptionString(req.Options, "outputDir")
	}

	return watermarkRequest{
		mode:            mode,
		inputPaths:      inputPaths,
		outputPath:      convertOptionString(req.Options, "outputPath"),
		outputDir:       outputDir,
		kind:            kind,
		text:            text,
		bold:            bold,
		color:           color,
		logoPath:        logoPath,
		logoBytes:       logoBytes,
		anchor:          anchor,
		marginPercent:   marginPercent,
		scalePercent:    scalePercent,
		opacity:         opacity,
		tile:            tile,
		rotationDegrees: rotationDegrees,
		format:          format,
		quality:         quality,
		qualitySupplied: qualitySupplied,
	}, nil
}

func watermarkKindOption(options map[string]any) (string, *models.JobError) {
	kind := strings.ToLower(convertOptionString(options, "kind"))
	if kind == "" {
		return watermarkKindText, nil
	}
	if kind != watermarkKindText && kind != watermarkKindLogo {
		return "", models.NewCanonicalJobError("IMAGE_WATERMARK_KIND_INVALID", "options.kind must be text or logo", nil)
	}

	return kind, nil
}

func watermarkTextOption(options map[string]any) (string, *models.JobError) {
	if !optionSupplied(options, "text") {
		return "", nil
	}

	value, ok := options["text"].(string)
	if !ok {
		return "", models.NewCanonicalJobError("IMAGE_WATERMARK_OPTION_INVALID", "options.text must be a string", nil)
	}

	return value, nil
}

// watermarkValidateTextOption enforces the text surface limits at plan time so
// a bad request never reaches execution. The engine repeats the check as a
// final guard.
func watermarkValidateTextOption(kind, text string) *models.JobError {
	if kind != watermarkKindText {
		return nil
	}

	if strings.TrimSpace(text) == "" {
		return models.NewCanonicalJobError("IMAGE_WATERMARK_TEXT_REQUIRED", "options.text is required for text watermarks.", nil)
	}
	if len([]rune(text)) > watermarkMaxTextRunes {
		return models.NewCanonicalJobError("IMAGE_WATERMARK_TEXT_TOO_LONG", fmt.Sprintf("options.text must be at most %d characters", watermarkMaxTextRunes), nil)
	}
	if len(strings.Split(text, "\n")) > watermarkMaxTextLines {
		return models.NewCanonicalJobError("IMAGE_WATERMARK_TEXT_TOO_MANY_LINES", fmt.Sprintf("options.text must be at most %d lines", watermarkMaxTextLines), nil)
	}
	for _, character := range text {
		if character != '\n' && (character < 0x20 || character == 0x7f) {
			return models.NewCanonicalJobError("IMAGE_WATERMARK_TEXT_CONTROL_CHARS", "options.text must not contain control characters", nil)
		}
	}

	return nil
}

// watermarkLogoOption loads and validates the logo exactly once per request.
func watermarkLogoOption(options map[string]any) (string, []byte, *models.JobError) {
	logoPath := convertOptionString(options, "logoPath")
	if logoPath == "" {
		return "", nil, models.NewCanonicalJobError("IMAGE_WATERMARK_LOGO_REQUIRED", "options.logoPath is required for logo watermarks.", nil)
	}

	info, statErr := os.Stat(logoPath)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return "", nil, models.NewCanonicalJobError(
				"IMAGE_WATERMARK_LOGO_NOT_FOUND",
				fmt.Sprintf("%s was not found.", filepath.Base(logoPath)),
				map[string]any{"logoPath": logoPath},
			)
		}

		return "", nil, models.NewCanonicalJobError(
			"IMAGE_WATERMARK_LOGO_NOT_FOUND",
			fmt.Sprintf("%s cannot be read.", filepath.Base(logoPath)),
			map[string]any{"logoPath": logoPath},
		)
	}

	if info.IsDir() {
		return "", nil, models.NewCanonicalJobError(
			"IMAGE_WATERMARK_LOGO_UNSUPPORTED",
			fmt.Sprintf("%s is a folder, not an image.", filepath.Base(logoPath)),
			map[string]any{"logoPath": logoPath},
		)
	}

	extension := normalizeFormatName(strings.TrimPrefix(filepath.Ext(logoPath), "."))
	if extension != "png" && extension != "jpeg" && extension != "webp" {
		return "", nil, models.NewCanonicalJobError(
			"IMAGE_WATERMARK_LOGO_UNSUPPORTED",
			fmt.Sprintf("%s is not a supported logo. Use %s.", filepath.Base(logoPath), watermarkLogoExtensionsMessage),
			map[string]any{"logoPath": logoPath, "extension": extension},
		)
	}

	logoBytes, readErr := os.ReadFile(logoPath)
	if readErr != nil {
		return "", nil, models.NewCanonicalJobError(
			"IMAGE_WATERMARK_LOGO_NOT_FOUND",
			fmt.Sprintf("%s cannot be read.", filepath.Base(logoPath)),
			map[string]any{"logoPath": logoPath, "error": readErr.Error()},
		)
	}

	if _, _, decodeErr := image.Decode(bytes.NewReader(logoBytes)); decodeErr != nil {
		return "", nil, models.NewCanonicalJobError(
			"IMAGE_WATERMARK_LOGO_INVALID",
			fmt.Sprintf("%s is not a readable image.", filepath.Base(logoPath)),
			map[string]any{"logoPath": logoPath, "error": decodeErr.Error()},
		)
	}

	return logoPath, logoBytes, nil
}

func watermarkAnchorOption(options map[string]any) (string, *models.JobError) {
	anchor := strings.ToLower(convertOptionString(options, "anchor"))
	if anchor == "" {
		return watermarkDefaultAnchor, nil
	}
	if _, _, ok := watermarkAnchorFactors(anchor); !ok {
		return "", models.NewCanonicalJobError(
			"IMAGE_WATERMARK_ANCHOR_INVALID",
			fmt.Sprintf("options.anchor must be one of: %s", strings.Join(watermarkAnchorNames, ", ")),
			nil,
		)
	}

	return anchor, nil
}

// watermarkBoundedFloatOption reads one bounded float option. Supplied values
// of the wrong type, NaN or infinity are rejected so a typo never silently
// changes the drawn watermark.
func watermarkBoundedFloatOption(options map[string]any, key string, defaultValue, min, max float64, invalidCode string) (float64, *models.JobError) {
	value := defaultValue
	if optionSupplied(options, key) {
		parsed, ok := watermarkFloatValue(options[key])
		if !ok {
			return 0, models.NewCanonicalJobError(invalidCode, fmt.Sprintf("options.%s must be numeric", key), nil)
		}
		value = parsed
	}

	if value < min || value > max {
		return 0, models.NewCanonicalJobError(
			invalidCode,
			fmt.Sprintf("options.%s must be between %v and %v", key, min, max),
			nil,
		)
	}

	return value, nil
}

func watermarkFloatValue(raw any) (float64, bool) {
	var value float64
	switch casted := raw.(type) {
	case float64:
		value = casted
	case float32:
		value = float64(casted)
	case int:
		value = float64(casted)
	case int8:
		value = float64(casted)
	case int16:
		value = float64(casted)
	case int32:
		value = float64(casted)
	case int64:
		value = float64(casted)
	case uint:
		value = float64(casted)
	case uint8:
		value = float64(casted)
	case uint16:
		value = float64(casted)
	case uint32:
		value = float64(casted)
	case uint64:
		value = float64(casted)
	case json.Number:
		parsed, err := casted.Float64()
		if err != nil {
			return 0, false
		}
		value = parsed
	default:
		return 0, false
	}

	if math.IsNaN(value) || math.IsInf(value, 0) {
		return 0, false
	}

	return value, true
}

// watermarkOptionalInt reads an optional integer option and rejects fractions.
func watermarkOptionalInt(options map[string]any, key string) (int, bool, *models.JobError) {
	if !optionSupplied(options, key) {
		return 0, false, nil
	}

	if floatValue, isFloat := options[key].(float64); isFloat && floatValue != math.Trunc(floatValue) {
		return 0, true, models.NewCanonicalJobError("IMAGE_WATERMARK_OPTION_INVALID", fmt.Sprintf("options.%s must be an integer", key), nil)
	}

	value, ok := numericOption(options, key)
	if !ok {
		return 0, true, models.NewCanonicalJobError("IMAGE_WATERMARK_OPTION_INVALID", fmt.Sprintf("options.%s must be numeric", key), nil)
	}

	return value, true, nil
}

func watermarkColorOption(options map[string]any) (string, *models.JobError) {
	color := convertOptionString(options, "color")
	if color == "" {
		return watermarkDefaultColor, nil
	}
	if !watermarkValidHexColor(color) {
		return "", models.NewCanonicalJobError("IMAGE_WATERMARK_COLOR_INVALID", "options.color must be a #RRGGBB hex value", nil)
	}

	return color, nil
}

func watermarkRequestedFormat(options map[string]any) (string, *models.JobError) {
	raw, exists := options["format"]
	if !exists || raw == nil {
		return imageAutoFormat, nil
	}

	text, ok := raw.(string)
	if !ok {
		return "", models.NewCanonicalJobError("IMAGE_WATERMARK_FORMAT_INVALID", "format must be auto, jpeg, png or webp", nil)
	}

	format := normalizeFormatName(text)
	switch format {
	case "":
		return imageAutoFormat, nil
	case imageAutoFormat, "original":
		return imageAutoFormat, nil
	case "jpeg", "png", "webp":
		return format, nil
	default:
		return "", models.NewCanonicalJobError("IMAGE_WATERMARK_FORMAT_INVALID", fmt.Sprintf("format must be auto, jpeg, png or webp (got %q)", strings.TrimSpace(text)), nil)
	}
}

func validateWatermarkInputPath(inputPath string) *models.JobError {
	trimmed := strings.TrimSpace(inputPath)
	if trimmed == "" {
		return models.NewCanonicalJobError("IMAGE_WATERMARK_INPUT_REQUIRED", "inputPath is required", nil)
	}

	info, statErr := os.Stat(trimmed)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return models.NewCanonicalJobError(
				"IMAGE_WATERMARK_INPUT_NOT_FOUND",
				fmt.Sprintf("%s was not found.", filepath.Base(trimmed)),
				map[string]any{"inputPath": trimmed},
			)
		}

		return models.NewCanonicalJobError(
			"IMAGE_WATERMARK_INPUT_NOT_FOUND",
			fmt.Sprintf("%s cannot be read.", filepath.Base(trimmed)),
			map[string]any{"inputPath": trimmed},
		)
	}

	if info.IsDir() {
		return models.NewCanonicalJobError(
			"IMAGE_WATERMARK_INPUT_UNSUPPORTED",
			fmt.Sprintf("%s is a folder, not an image.", filepath.Base(trimmed)),
			map[string]any{"inputPath": trimmed},
		)
	}

	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(trimmed), "."))
	if !compressSupportsInputExtension(extension) {
		return models.NewCanonicalJobError(
			"IMAGE_WATERMARK_INPUT_UNSUPPORTED",
			fmt.Sprintf("%s is not a supported image. Use %s.", filepath.Base(trimmed), strings.Join(imageInputExtensions, ", ")),
			map[string]any{"inputPath": trimmed, "extension": extension},
		)
	}

	return nil
}

// resolveWatermarkOutputFormat keeps an explicit format untouched and resolves
// auto per input: JPEG, PNG and WebP keep their family, everything else falls to
// JPEG unless the source carries transparency. The metadata comes from the one
// plan-time read of the input.
func resolveWatermarkOutputFormat(inputPath, requested string, meta planImageMeta, metaOK bool) string {
	if requested != imageAutoFormat {
		return requested
	}

	switch format := normalizeFormatName(strings.TrimPrefix(filepath.Ext(inputPath), ".")); format {
	case "jpeg", "webp", "png":
		return format
	default:
		if metaOK && meta.Alpha {
			return "png"
		}
		return "jpeg"
	}
}

// planParsedWatermarkRequest builds the public plan plus one prepared item per
// output. Engine options (logo bytes, background flattening) are resolved here
// so execution performs no extra reads or metadata probes.
func planParsedWatermarkRequest(parsed watermarkRequest) (models.PlanJobResponse, []preparedWatermark, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	formats := make([]string, 0, len(parsed.inputPaths))
	alphas := make([]bool, 0, len(parsed.inputPaths))
	inputWarnings := make([]string, 0)

	for _, inputPath := range parsed.inputPaths {
		if inputErr := validateWatermarkInputPath(inputPath); inputErr != nil {
			failedPlan, _ := planFailure(plan, inputErr)
			return failedPlan, nil, inputErr
		}

		// One metadata read per input drives the auto format, the alpha
		// flattening decision, the animation warning and the text-fit check.
		// An unreadable header leaves the previous behavior intact: skip the
		// fit check and let execution report the real decode error.
		meta, metaErr := readPlanImageMeta(inputPath)
		metaOK := metaErr == nil

		formats = append(formats, resolveWatermarkOutputFormat(inputPath, parsed.format, meta, metaOK))
		alphas = append(alphas, metaOK && meta.Alpha)

		if metaOK && meta.Frames > 1 {
			inputWarnings = append(inputWarnings, fmt.Sprintf("%s: %s", filepath.Base(inputPath), watermarkAnimatedWarning))
		}

		// Reject text that cannot fit even at the 12 px floor before execution.
		// Undecodable inputs are skipped here; the executor reports the real
		// decode error with a per-item failure.
		if parsed.kind == watermarkKindText && metaOK {
			if fitErr := watermarkPlanTextFitError(parsed, inputPath, meta.Width, meta.Height); fitErr != nil {
				failedPlan, _ := planFailure(plan, fitErr)
				return failedPlan, nil, fitErr
			}
		}
	}

	// Reserve every input path so an auto-derived output can never overwrite a
	// different input of the same batch.
	used := make(map[string]struct{}, len(parsed.inputPaths))
	for _, inputPath := range parsed.inputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	optionWarnings := make([]string, 0, 1)
	if parsed.qualitySupplied && watermarkFormatsUsePNG(formats) {
		optionWarnings = append(optionWarnings, watermarkPNGQualityWarning)
	}

	if parsed.mode == "single" {
		if parsed.outputDir != "" && strings.TrimSpace(parsed.outputPath) == "" {
			if dirErr := validateWatermarkOutputDir(parsed.outputDir); dirErr != nil {
				failedPlan, _ := planFailure(plan, dirErr)
				return failedPlan, nil, dirErr
			}
		}

		planned, planErr := planSingleWatermarkOutput(parsed, formats[0], used)
		if planErr != nil {
			if planned.OutputPath != "" {
				plan.Outputs = []models.PlannedOutput{planned}
			}
			failedPlan, _ := planFailure(plan, planErr)
			return failedPlan, nil, planErr
		}

		plan.Success = true
		plan.Message = watermarkPlanMessage(1)
		plan.Outputs = []models.PlannedOutput{planned}
		plan.Warnings = append(append([]string{}, planned.Warnings...), inputWarnings...)
		plan.Warnings = append(plan.Warnings, optionWarnings...)
		plan.EffectiveOptions = watermarkEffectiveOptions(parsed, formats, alphas)

		prepared := []preparedWatermark{{
			inputPath:  planned.InputPath,
			outputPath: planned.OutputPath,
			options:    watermarkEngineOptions(parsed, formats[0], alphas[0]),
		}}

		return plan, prepared, nil
	}

	if parsed.outputDir != "" {
		if dirErr := validateWatermarkOutputDir(parsed.outputDir); dirErr != nil {
			failedPlan, _ := planFailure(plan, dirErr)
			return failedPlan, nil, dirErr
		}
	}

	outputs := make([]models.PlannedOutput, 0, len(parsed.inputPaths))
	preparedItems := make([]preparedWatermark, 0, len(parsed.inputPaths))
	outputWarnings := make([]string, 0, len(parsed.inputPaths))

	for index, inputPath := range parsed.inputPaths {
		targetDir := parsed.outputDir
		if targetDir == "" {
			targetDir = filepath.Dir(inputPath)
		}

		planned := planAutoWatermarkOutput(inputPath, filepath.Clean(targetDir), formats[index], used)
		outputs = append(outputs, planned)
		outputWarnings = append(outputWarnings, planned.Warnings...)
		preparedItems = append(preparedItems, preparedWatermark{
			inputPath:  planned.InputPath,
			outputPath: planned.OutputPath,
			options:    watermarkEngineOptions(parsed, formats[index], alphas[index]),
		})
	}

	plan.Success = true
	plan.Message = watermarkPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = append(outputWarnings, inputWarnings...)
	plan.Warnings = append(plan.Warnings, optionWarnings...)
	plan.EffectiveOptions = watermarkEffectiveOptions(parsed, formats, alphas)

	return plan, preparedItems, nil
}

// watermarkFormatsUsePNG reports whether any planned output is PNG.
func watermarkFormatsUsePNG(formats []string) bool {
	for _, format := range formats {
		if format == "png" {
			return true
		}
	}

	return false
}

// watermarkEngineOptions turns the parsed request into engine input for one
// output. JPEG output with an alpha source is flattened on white because bimg
// skips flattening when Background is its zero value (black).
func watermarkEngineOptions(parsed watermarkRequest, outputFmt string, sourceHasAlpha bool) WatermarkOptions {
	options := WatermarkOptions{
		Kind:            parsed.kind,
		Text:            parsed.text,
		Bold:            parsed.bold,
		Color:           parsed.color,
		Logo:            parsed.logoBytes,
		Anchor:          parsed.anchor,
		MarginPercent:   parsed.marginPercent,
		ScalePercent:    parsed.scalePercent,
		Opacity:         parsed.opacity,
		Tile:            parsed.tile,
		RotationDegrees: parsed.rotationDegrees,
		Format:          outputFmt,
		Quality:         parsed.quality,
	}

	if outputFmt == "jpeg" && sourceHasAlpha {
		white := bimg.Color{R: 255, G: 255, B: 255}
		options.Background = &white
	}

	return options
}

// planSingleWatermarkOutput resolves the one output of a single request.
func planSingleWatermarkOutput(parsed watermarkRequest, outputFmt string, used map[string]struct{}) (models.PlannedOutput, *models.JobError) {
	inputPath := parsed.inputPaths[0]

	if explicitOutput := strings.TrimSpace(parsed.outputPath); explicitOutput != "" {
		return planExplicitWatermarkOutput(inputPath, explicitOutput, outputFmt)
	}

	targetDir := strings.TrimSpace(parsed.outputDir)
	if targetDir == "" {
		targetDir = filepath.Dir(inputPath)
	}

	return planAutoWatermarkOutput(inputPath, filepath.Clean(targetDir), outputFmt, used), nil
}

func planExplicitWatermarkOutput(inputPath, explicitOutput, outputFormat string) (models.PlannedOutput, *models.JobError) {
	planned := models.PlannedOutput{
		InputPath:  inputPath,
		OutputPath: filepath.Clean(strings.TrimSpace(explicitOutput)),
		OutputKind: imageOutputKindFile,
	}

	if !compressOutputExtensionMatches(planned.OutputPath, outputFormat) {
		return planned, models.NewCanonicalJobError(
			"IMAGE_WATERMARK_OUTPUT_EXTENSION_MISMATCH",
			compressOutputExtensionMessage(planned.OutputPath, outputFormat),
			map[string]any{"outputPath": planned.OutputPath, "format": outputFormat},
		)
	}

	if fsx.SameFile(inputPath, planned.OutputPath) {
		return planned, models.NewCanonicalJobError("IMAGE_WATERMARK_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
	}

	info, statErr := os.Stat(planned.OutputPath)
	switch {
	case statErr == nil:
		planned.Exists = true
		if info.IsDir() {
			return planned, models.NewCanonicalJobError(
				"IMAGE_WATERMARK_OUTPUT_DIR_INVALID",
				fmt.Sprintf("%s is a folder, not a file.", filepath.Base(planned.OutputPath)),
				map[string]any{"outputPath": planned.OutputPath},
			)
		}
		return planned, models.NewCanonicalJobError(
			"IMAGE_WATERMARK_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another name.", filepath.Base(planned.OutputPath)),
			map[string]any{"outputPath": planned.OutputPath},
		)
	case !errors.Is(statErr, os.ErrNotExist):
		return planned, models.NewCanonicalJobError(
			"IMAGE_WATERMARK_OUTPUT_DIR_INVALID",
			fmt.Sprintf("%s cannot be accessed. Choose another location.", filepath.Base(planned.OutputPath)),
			map[string]any{"outputPath": planned.OutputPath},
		)
	}

	return planned, nil
}

// planAutoWatermarkOutput derives a collision-safe name inside dir. It never
// writes.
func planAutoWatermarkOutput(inputPath, dir, outputFmt string, used map[string]struct{}) models.PlannedOutput {
	planned := models.PlannedOutput{
		InputPath:  inputPath,
		OutputKind: imageOutputKindFile,
	}

	base := watermarkOutputBaseName(inputPath)
	outputPath, adjusted := outputplan.UniqueFilePath(dir, base, outputFmt, used)
	planned.OutputPath = outputPath

	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", base+"."+outputFmt, filepath.Base(outputPath)))
	}

	return planned
}

func watermarkOutputBaseName(inputPath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		stem = "image"
	}

	return stem + watermarkOutputSuffix
}

func validateWatermarkOutputDir(outputDir string) *models.JobError {
	info, statErr := os.Stat(outputDir)
	if statErr != nil {
		return models.NewCanonicalJobError(
			"IMAGE_WATERMARK_OUTPUT_DIR_INVALID",
			"The output folder cannot be used.",
			map[string]any{"outputDir": outputDir, "error": statErr.Error()},
		)
	}
	if !info.IsDir() {
		return models.NewCanonicalJobError(
			"IMAGE_WATERMARK_OUTPUT_DIR_INVALID",
			fmt.Sprintf("%s is not a folder.", outputDir),
			map[string]any{"outputDir": outputDir},
		)
	}

	return nil
}

func watermarkPlanMessage(count int) string {
	if count == 1 {
		return "Ready to watermark 1 image."
	}

	return fmt.Sprintf("Ready to watermark %d images.", count)
}

// watermarkPlanTextFitError rejects text that cannot fit even at the 12 px
// floor, so the UI fails before execution. The execution path keeps the same
// checks as a final defense.
func watermarkPlanTextFitError(parsed watermarkRequest, inputPath string, width, height int) *models.JobError {
	if width < 1 || height < 1 {
		return nil
	}

	shortEdge := width
	if height < shortEdge {
		shortEdge = height
	}
	margin := int(math.Max(1, math.Round(float64(shortEdge)*parsed.marginPercent/100)))
	details := map[string]any{"inputPath": inputPath}

	if parsed.tile {
		stamp, stampErr := watermarkRenderTileStamp(parsed.text, parsed.bold, watermarkMinFontSize, color.NRGBA{R: 255, G: 255, B: 255, A: 255})
		if stampErr != nil {
			return models.NewCanonicalJobError("IMAGE_WATERMARK_TEXT_DOES_NOT_FIT", stampErr.Error(), details)
		}
		if stamp.Bounds().Dx() > width {
			return models.NewCanonicalJobError(
				"IMAGE_WATERMARK_TEXT_DOES_NOT_FIT",
				fmt.Sprintf("text is too wide to tile at the minimum font size (%d px); shorten the text or use a larger image", watermarkMinFontSize),
				details,
			)
		}

		return nil
	}

	message := fmt.Sprintf("text does not fit the image at the minimum font size (%d px); use fewer or shorter lines, or a larger image", watermarkMinFontSize)
	insetWidth := width - 2*margin
	insetHeight := height - 2*margin
	if insetWidth <= 0 || insetHeight <= 0 {
		return models.NewCanonicalJobError("IMAGE_WATERMARK_TEXT_DOES_NOT_FIT", message, details)
	}

	face, faceErr := watermarkFontFace(parsed.bold, watermarkMinFontSize)
	if faceErr != nil {
		return models.NewCanonicalJobError("IMAGE_WATERMARK_TEXT_DOES_NOT_FIT", faceErr.Error(), details)
	}
	block := watermarkMeasureText(parsed.text, face, watermarkMinFontSize)
	if block.width <= float64(insetWidth) && block.height <= float64(insetHeight) {
		return nil
	}

	return models.NewCanonicalJobError("IMAGE_WATERMARK_TEXT_DOES_NOT_FIT", message, details)
}

func watermarkEffectiveOptions(parsed watermarkRequest, formats []string, alphas []bool) map[string]any {
	effective := map[string]any{
		"kind":          parsed.kind,
		"anchor":        parsed.anchor,
		"marginPercent": parsed.marginPercent,
		"scalePercent":  parsed.scalePercent,
		"opacity":       parsed.opacity,
		"tile":          parsed.tile,
		"quality":       parsed.quality,
		// Per-input values let execution rebuild the exact engine options of
		// each planned output, including reduced retry plans.
		"formats": append([]string(nil), formats...),
		"alphas":  append([]bool(nil), alphas...),
	}
	if parsed.kind == watermarkKindText {
		effective["text"] = parsed.text
		effective["color"] = parsed.color
		effective["bold"] = parsed.bold
	}
	if parsed.kind == watermarkKindLogo {
		effective["logoPath"] = parsed.logoPath
	}
	// Rotation only affects tiled text, so echoing it for every watermark would
	// overstate what the request does.
	if parsed.tile {
		effective["rotationDegrees"] = parsed.rotationDegrees
	}
	if format, ok := commonPlannedFormat(formats); ok {
		effective["format"] = format
	}

	return effective
}

// watermarkCancelledError builds the canonical cancellation error. It falls back
// to context.Canceled when the context has no recorded cause.
func watermarkCancelledError(ctx context.Context) *models.JobError {
	var cause error = context.Canceled
	if ctx != nil && ctx.Err() != nil {
		cause = ctx.Err()
	}

	message := fmt.Errorf(OperationCancelledMsg, cause).Error()
	return models.NewCanonicalJobError("IMAGE_WATERMARK_CANCELLED", message, nil)
}

// watermarkExecutionError maps a failed execution to a job error. Cancellation
// wins over the generic execution/item code so the job engine reports
// CANCELLED_BY_USER, including when the last batch item is interrupted. The raw
// cause stays in Details while the message remains user-facing.
func watermarkExecutionError(ctx context.Context, detailCode, inputPath string, execErr error) (*models.JobError, bool) {
	if watermarkIsCancellation(ctx, execErr) {
		return watermarkCancelledError(ctx), true
	}

	details := map[string]any{"inputPath": inputPath}
	if execErr != nil {
		details["error"] = execErr.Error()
	}

	return models.NewCanonicalJobError(detailCode, "Could not watermark the image.", details), false
}

func watermarkIsCancellation(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}

	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

func watermarkCheckContext(ctx context.Context) error {
	if ctx == nil {
		return nil
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf(OperationCancelledMsg, ctx.Err())
	default:
		return nil
	}
}

func executeWatermarkToPath(ctx context.Context, prepared preparedWatermark) (WatermarkOutcome, error) {
	if checkErr := watermarkCheckContext(ctx); checkErr != nil {
		return WatermarkOutcome{}, checkErr
	}

	input, readErr := os.ReadFile(prepared.inputPath)
	if readErr != nil {
		return WatermarkOutcome{}, fmt.Errorf("read input failed: %w", readErr)
	}

	if checkErr := watermarkCheckContext(ctx); checkErr != nil {
		return WatermarkOutcome{}, checkErr
	}

	output, outcome, applyErr := ApplyWatermark(input, prepared.options)
	if applyErr != nil {
		return WatermarkOutcome{}, fmt.Errorf("watermark failed: %w", applyErr)
	}

	if checkErr := watermarkCheckContext(ctx); checkErr != nil {
		return WatermarkOutcome{}, checkErr
	}

	if mkErr := os.MkdirAll(filepath.Dir(prepared.outputPath), 0o755); mkErr != nil {
		return WatermarkOutcome{}, fmt.Errorf("create output dir failed: %w", mkErr)
	}

	if writeErr := os.WriteFile(prepared.outputPath, output, DefaultFilePermissions); writeErr != nil {
		return WatermarkOutcome{}, fmt.Errorf("write output failed: %w", writeErr)
	}

	return outcome, nil
}

func watermarkSuccessItem(prepared preparedWatermark, outcome WatermarkOutcome) models.JobResultItem {
	return models.JobResultItem{
		InputPath:   prepared.inputPath,
		OutputPath:  prepared.outputPath,
		Outputs:     []string{prepared.outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     watermarkResultMessage(prepared, outcome),
	}
}

// watermarkResultMessage reports the drawn watermark honestly: its mode, anchor,
// resolved size and the encoded dimensions.
func watermarkResultMessage(prepared preparedWatermark, outcome WatermarkOutcome) string {
	if outcome.FontSize > 0 {
		if prepared.options.Tile {
			return fmt.Sprintf("tiled text watermark (font %d px, %d×%d)", outcome.FontSize, outcome.Width, outcome.Height)
		}
		return fmt.Sprintf("text watermark at %s (font %d px, %d×%d)", outcome.Anchor, outcome.FontSize, outcome.Width, outcome.Height)
	}

	return fmt.Sprintf("logo watermark at %s (%d px wide, %d×%d)", outcome.Anchor, outcome.LogoWidth, outcome.Width, outcome.Height)
}
