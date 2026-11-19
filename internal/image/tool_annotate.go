package image

import (
	"bytes"
	"context"
	"encoding/base64"
	"errors"
	"fmt"
	"image"
	"image/color"
	"image/draw"
	_ "image/gif"
	_ "image/jpeg"
	"image/png"
	"math"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/platform/fsx"
	"fileforge-desktop/internal/utils"

	"github.com/disintegration/imaging"
	"github.com/fogleman/gg"
	"github.com/h2non/bimg"
	"golang.org/x/image/font/gofont/goregular"
	"golang.org/x/image/font/opentype"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

const ToolIDImageAnnotateV1 = "tool.image.annotate"

const (
	annotateTypeText   = "text"
	annotateTypeArrow  = "arrow"
	annotateTypeRect   = "rect"
	annotateTypeBlur   = "blur"
	annotateTypeRedact = "redact"
)

type AnnotateTool struct{}

type annotateRequest struct {
	mode       string
	inputPaths []string
	outputPath string
	outputDir  string
	format     string
	operations []models.ImageAnnotateOperationV1
}

type preparedAnnotate struct {
	inputPath  string
	outputPath string
	outputFmt  string
	operations []models.ImageAnnotateOperationV1
}

func NewAnnotateTool() *AnnotateTool {
	return &AnnotateTool{}
}

func (t *AnnotateTool) ID() string {
	return ToolIDImageAnnotateV1
}

func (t *AnnotateTool) Capability() string {
	return ToolIDImageAnnotateV1
}

func (t *AnnotateTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Image Annotate",
		Description:      "Annotate images with text, arrows, rectangles, blur and redact",
		Domain:           "image",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff", "tif"},
		OutputExtensions: []string{"jpeg", "png", "webp", "gif", "tiff"},
		RuntimeDeps:      []string{"libvips"},
		Tags:             []string{"image", "annotate", "text", "arrow", "rect", "blur", "redact"},
	}
}

func (t *AnnotateTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *AnnotateTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe <stem>_annotated.<ext> destination per
// input and validates the annotation operations against every image without
// writing anything. An explicit options.outputPath keeps the single request
// contract: it must differ from the input and the target must not exist yet.
func (t *AnnotateTool) Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan, _, planErr := t.planRequest(ctx, req)
	return plan, planErr
}

// planRequest also returns the parsed request so execution can reuse the
// normalized operations without resolving any path again.
func (t *AnnotateTool) planRequest(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, annotateRequest, *models.JobErrorV1) {
	parsed, parseErr := parseAnnotateRequest(req)
	if parseErr != nil {
		plan, _ := planFailure(models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}, parseErr)
		return plan, annotateRequest{}, parseErr
	}

	plan, planErr := planParsedAnnotateRequest(parsed)
	return plan, parsed, planErr
}

func planParsedAnnotateRequest(parsed annotateRequest) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	formats := make([]string, 0, len(parsed.inputPaths))
	for _, inputPath := range parsed.inputPaths {
		if inputErr := validateInputImagePath(inputPath); inputErr != nil {
			return planFailure(plan, inputErr)
		}

		outputFmt, fmtErr := resolveOutputFormat(inputPath, parsed.format)
		if fmtErr != nil {
			return planFailure(plan, fmtErr)
		}
		formats = append(formats, outputFmt)

		width, height, dimErr := readPlanImageDimensions(inputPath)
		if dimErr != nil {
			readErr := models.NewCanonicalJobError("IMAGE_ANNOTATE_READ_FAILED", dimErr.Error(), map[string]any{"inputPath": inputPath})
			return planFailure(plan, readErr)
		}
		if opErr := validateOperationsForImage(parsed.operations, width, height); opErr != nil {
			return planFailure(plan, opErr)
		}
	}

	// Reserve every input path so an auto-derived output can never overwrite a
	// different input of the same batch.
	used := make(map[string]struct{}, len(parsed.inputPaths))
	for _, inputPath := range parsed.inputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	if parsed.mode == "single" {
		planned, planErr := planSingleAnnotateOutput(parsed, formats[0], used)
		if planErr != nil {
			if planned.OutputPath != "" {
				plan.Outputs = []models.PlannedOutputV1{planned}
			}
			return planFailure(plan, planErr)
		}

		plan.Success = true
		plan.Message = annotatePlanMessage(1)
		plan.Outputs = []models.PlannedOutputV1{planned}
		plan.Warnings = planned.Warnings
		plan.EffectiveOptions = annotateEffectiveOptions(parsed, formats)

		return plan, nil
	}

	if parsed.outputDir != "" {
		if dirErr := validateAnnotateOutputDir(parsed.outputDir); dirErr != nil {
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

		planned := planAutoAnnotateOutput(inputPath, filepath.Clean(targetDir), formats[index], used)
		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = annotatePlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = annotateEffectiveOptions(parsed, formats)

	return plan, nil
}

func (t *AnnotateTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	inputPath := firstPath(req.InputPaths)

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return imageFailureItem(inputPath, plannedOutputPath(plan), planErr), planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, nil)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("IMAGE_ANNOTATE_EXECUTION", "annotate returned no result", nil)
	}

	return imageFailureItem(inputPath, plannedOutputPath(plan), jobErr), jobErr
}

func (t *AnnotateTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, jobErr := parseAnnotateRequest(req)
	if jobErr != nil {
		return nil, jobErr
	}

	if parsed.mode != "batch" {
		return nil, models.NewCanonicalJobError("IMAGE_ANNOTATE_MODE_INVALID", "mode must be batch", nil)
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan annotates exactly the outputs resolved by Plan. The normalized
// operations travel in the effective options, so a reduced retry plan only
// processes the failed items without re-planning. Results keep plan order.
func (t *AnnotateTool) ExecutePlan(ctx context.Context, req models.JobRequestV1, plan models.PlanJobResponseV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	total := len(plan.Outputs)
	if total == 0 {
		return nil, models.NewCanonicalJobError("IMAGE_ANNOTATE_INPUT_REQUIRED", "Choose at least one image to annotate.", nil)
	}

	operations, opErr := annotatePlanOperations(plan.EffectiveOptions, req)
	if opErr != nil {
		return nil, opErr
	}

	// Resolve every prepared item sequentially; execution is what runs in
	// parallel.
	preparedItems := make([]preparedAnnotate, total)
	for index, planned := range plan.Outputs {
		preparedItems[index] = preparedAnnotateFromPlan(planned, operations, plan.EffectiveOptions)
	}

	detailCode := "IMAGE_ANNOTATE_BATCH_ITEM"
	if req.Mode == "single" {
		detailCode = "IMAGE_ANNOTATE_EXECUTION"
	}

	items := make([]models.JobResultItemV1, total)
	completed := make([]bool, total)
	progress := newParallelProgress(total, onProgress)

	runErr := utils.ParallelForIndex(ctx, total, utils.ParallelWorkerCount(), func(ctx context.Context, index int) error {
		prepared := preparedItems[index]
		if err := executeAnnotateToPath(ctx, prepared); err != nil {
			itemErr := models.NewCanonicalJobError(detailCode, err.Error(), map[string]any{"inputPath": prepared.inputPath})
			items[index] = models.JobResultItemV1{InputPath: prepared.inputPath, OutputPath: prepared.outputPath, Success: false, Message: "image annotate failed", Error: itemErr}
		} else {
			items[index] = models.JobResultItemV1{InputPath: prepared.inputPath, OutputPath: prepared.outputPath, Outputs: []string{prepared.outputPath}, OutputCount: 1, Success: true, Message: "image annotate successful"}
		}
		completed[index] = true
		progress.itemCompleted()

		return nil
	})

	if batchRunCancelled(ctx, runErr) {
		cause := batchCancellationCause(ctx, runErr)
		if req.Mode == "single" {
			return collectCompletedItems(items, completed), models.NewCanonicalJobError("IMAGE_ANNOTATE_EXECUTION", fmt.Sprintf("annotate cancelled: %v", cause), nil)
		}

		return collectCompletedItems(items, completed), models.NewCanonicalJobError("IMAGE_ANNOTATE_CANCELLED", cause.Error(), nil)
	}

	return items, firstFailedItemError(items)
}

// annotatePlanOperations reads the normalized operations stored by Plan and
// falls back to parsing the request options for legacy callers that never
// planned. It never resolves output paths.
func annotatePlanOperations(effective map[string]any, req models.JobRequestV1) ([]models.ImageAnnotateOperationV1, *models.JobErrorV1) {
	if effective != nil {
		if operations, ok := effective["operations"].([]models.ImageAnnotateOperationV1); ok && len(operations) > 0 {
			return operations, nil
		}
	}

	parsed, parseErr := parseAnnotateRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}

	return parsed.operations, nil
}

func (t *AnnotateTool) GetImageAnnotatePreviewV1(req models.ImageAnnotatePreviewRequestV1) models.ImageAnnotatePreviewResponseV1 {
	inputPath := strings.TrimSpace(req.InputPath)
	if inputPath == "" {
		return models.ImageAnnotatePreviewResponseV1{
			Success: false,
			Message: "Select a valid image path and retry.",
			Error:   models.NewCanonicalJobError("IMAGE_ANNOTATE_PREVIEW_INVALID_PATH", "inputPath is required", nil),
		}
	}

	if err := validateInputImagePath(inputPath); err != nil {
		return models.ImageAnnotatePreviewResponseV1{Success: false, Message: err.Message, Error: err}
	}

	bytes, fmtName, width, height, err := readAndNormalize(inputPath)
	if err != nil {
		jobErr := models.NewCanonicalJobError("IMAGE_ANNOTATE_PREVIEW_READ_FAILED", err.Error(), nil)
		return models.ImageAnnotatePreviewResponseV1{Success: false, Message: "Cannot load image preview source.", Error: jobErr}
	}

	outputFmt, fmtErr := resolveOutputFormat(inputPath, strings.TrimSpace(req.Format))
	if fmtErr != nil {
		return models.ImageAnnotatePreviewResponseV1{Success: false, Message: fmtErr.Message, Error: fmtErr}
	}

	ops, opErr := normalizeOperations(req.Operations)
	if opErr != nil {
		return models.ImageAnnotatePreviewResponseV1{Success: false, Message: opErr.Message, Error: opErr}
	}

	if validateErr := validateOperationsForImage(ops, width, height); validateErr != nil {
		return models.ImageAnnotatePreviewResponseV1{Success: false, Message: validateErr.Message, Error: validateErr}
	}

	annotatedBytes, err := applyOperations(bytes, outputFmt, ops)
	if err != nil {
		jobErr := models.NewCanonicalJobError("IMAGE_ANNOTATE_PREVIEW_EXECUTION", err.Error(), nil)
		return models.ImageAnnotatePreviewResponseV1{Success: false, Message: "Failed to generate annotate preview.", Error: jobErr}
	}

	mimeType := imageMimeByFormat[outputFmt]
	if mimeType == "" {
		mimeType = imageMimeByFormat[fmtName]
	}

	return models.ImageAnnotatePreviewResponseV1{
		Success:    true,
		Message:    "image annotate preview generated",
		DataBase64: base64.StdEncoding.EncodeToString(annotatedBytes),
		MimeType:   mimeType,
		Width:      width,
		Height:     height,
	}
}

func parseAnnotateRequest(req models.JobRequestV1) (annotateRequest, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return annotateRequest{}, models.NewCanonicalJobError("IMAGE_ANNOTATE_MODE_INVALID", "mode must be single or batch", nil)
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return annotateRequest{}, models.NewCanonicalJobError("IMAGE_ANNOTATE_SINGLE_INPUT_COUNT", "single mode requires exactly one input", nil)
	}

	if mode == "batch" && len(req.InputPaths) < 1 {
		return annotateRequest{}, models.NewCanonicalJobError("IMAGE_ANNOTATE_BATCH_INPUT_REQUIRED", "batch mode requires at least one input", nil)
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawPath := range req.InputPaths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			return annotateRequest{}, models.NewCanonicalJobError("IMAGE_ANNOTATE_INPUT_REQUIRED", "inputPaths contains empty item", nil)
		}
		inputPaths = append(inputPaths, trimmed)
	}

	if req.Options == nil {
		return annotateRequest{}, models.NewCanonicalJobError("IMAGE_ANNOTATE_OPTION_REQUIRED", "options.operations is required", nil)
	}

	rawOps, ok := req.Options["operations"]
	if !ok || rawOps == nil {
		return annotateRequest{}, models.NewCanonicalJobError("IMAGE_ANNOTATE_OPTION_REQUIRED", "options.operations is required", nil)
	}

	operations, opErr := operationsFromAny(rawOps)
	if opErr != nil {
		return annotateRequest{}, opErr
	}

	normalized, normalizeErr := normalizeOperations(operations)
	if normalizeErr != nil {
		return annotateRequest{}, normalizeErr
	}

	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = strings.TrimSpace(annotateOptionString(req.Options, "outputDir"))
	}

	return annotateRequest{
		mode:       mode,
		inputPaths: inputPaths,
		outputPath: strings.TrimSpace(annotateOptionString(req.Options, "outputPath")),
		outputDir:  outputDir,
		format:     strings.ToLower(strings.TrimSpace(annotateOptionString(req.Options, "format"))),
		operations: normalized,
	}, nil
}

func operationsFromAny(raw any) ([]models.ImageAnnotateOperationV1, *models.JobErrorV1) {
	typed, ok := raw.([]models.ImageAnnotateOperationV1)
	if ok {
		return typed, nil
	}

	typedPtrs, ok := raw.([]*models.ImageAnnotateOperationV1)
	if ok {
		out := make([]models.ImageAnnotateOperationV1, 0, len(typedPtrs))
		for _, item := range typedPtrs {
			if item == nil {
				return nil, models.NewCanonicalJobError("IMAGE_ANNOTATE_OPERATION_INVALID", "each operation must be an object", nil)
			}
			out = append(out, *item)
		}
		return out, nil
	}

	list, ok := raw.([]any)
	if !ok {
		return nil, models.NewCanonicalJobError("IMAGE_ANNOTATE_OPERATIONS_INVALID", "options.operations must be an array", nil)
	}

	if len(list) == 0 {
		return nil, models.NewCanonicalJobError("IMAGE_ANNOTATE_OPERATIONS_EMPTY", "options.operations must include at least one operation", nil)
	}

	out := make([]models.ImageAnnotateOperationV1, 0, len(list))
	for _, item := range list {
		mapped, ok := item.(map[string]any)
		if !ok {
			return nil, models.NewCanonicalJobError("IMAGE_ANNOTATE_OPERATION_INVALID", "each operation must be an object", nil)
		}

		op, err := operationFromMap(mapped)
		if err != nil {
			return nil, err
		}
		out = append(out, op)
	}

	return out, nil
}

func operationFromMap(m map[string]any) (models.ImageAnnotateOperationV1, *models.JobErrorV1) {
	typeName := strings.ToLower(strings.TrimSpace(anyString(m["type"])))
	if typeName == "" {
		return models.ImageAnnotateOperationV1{}, models.NewCanonicalJobError("IMAGE_ANNOTATE_OPERATION_TYPE_REQUIRED", "operation.type is required", nil)
	}

	op := models.ImageAnnotateOperationV1{Type: typeName}

	op.X = anyInt(m["x"])
	op.Y = anyInt(m["y"])
	op.Width = anyInt(m["width"])
	op.Height = anyInt(m["height"])
	op.X2 = anyInt(m["x2"])
	op.Y2 = anyInt(m["y2"])
	op.Text = anyString(m["text"])
	op.Color = anyString(m["color"])
	op.Opacity = anyFloat(m["opacity"])
	op.StrokeWidth = anyInt(m["strokeWidth"])
	op.FontSize = anyInt(m["fontSize"])
	op.BlurIntensity = anyInt(m["blurIntensity"])

	return op, nil
}

func normalizeOperations(ops []models.ImageAnnotateOperationV1) ([]models.ImageAnnotateOperationV1, *models.JobErrorV1) {
	if len(ops) == 0 {
		return nil, models.NewCanonicalJobError("IMAGE_ANNOTATE_OPERATIONS_EMPTY", "operations must include at least one operation", nil)
	}

	normalized := make([]models.ImageAnnotateOperationV1, 0, len(ops))
	for idx, raw := range ops {
		op := raw
		op.Type = strings.ToLower(strings.TrimSpace(op.Type))
		op.Text = strings.TrimSpace(op.Text)
		op.Color = normalizeColor(op.Color)

		if op.StrokeWidth == 0 {
			op.StrokeWidth = 2
		}
		if op.FontSize == 0 {
			op.FontSize = 18
		}

		switch op.Type {
		case annotateTypeRect, annotateTypeArrow:
			if op.Opacity <= 0 {
				op.Opacity = 1
			}
		case annotateTypeRedact:
			if op.Color == "" {
				op.Color = "#000000"
			}
		}

		if op.Type == "" {
			return nil, models.NewCanonicalJobError("IMAGE_ANNOTATE_OPERATION_TYPE_REQUIRED", fmt.Sprintf("operation[%d].type is required", idx), nil)
		}

		normalized = append(normalized, op)
	}

	return normalized, nil
}

func validateOperationsForImage(ops []models.ImageAnnotateOperationV1, imageWidth, imageHeight int) *models.JobErrorV1 {
	if imageWidth < 1 || imageHeight < 1 {
		return models.NewCanonicalJobError("IMAGE_ANNOTATE_DIMENSIONS_INVALID", "image dimensions are invalid", nil)
	}

	for idx, op := range ops {
		suffix := fmt.Sprintf("operation[%d]", idx)
		switch op.Type {
		case annotateTypeText:
			if strings.TrimSpace(op.Text) == "" {
				return models.NewCanonicalJobError("IMAGE_ANNOTATE_TEXT_REQUIRED", suffix+".text is required", nil)
			}
			if op.FontSize < 8 || op.FontSize > 256 {
				return models.NewCanonicalJobError("IMAGE_ANNOTATE_FONT_SIZE_INVALID", suffix+".fontSize must be between 8 and 256", nil)
			}
			if op.X < 0 || op.Y < 0 || op.X >= imageWidth || op.Y >= imageHeight {
				return models.NewCanonicalJobError("IMAGE_ANNOTATE_COORDINATES_INVALID", suffix+" coordinates out of bounds", nil)
			}
		case annotateTypeRect, annotateTypeBlur, annotateTypeRedact:
			if err := validateRectOp(op, imageWidth, imageHeight, suffix); err != nil {
				return err
			}
			if op.Type == annotateTypeBlur {
				if op.BlurIntensity < 0 || op.BlurIntensity > 100 {
					return models.NewCanonicalJobError("IMAGE_ANNOTATE_BLUR_INTENSITY_INVALID", suffix+".blurIntensity must be between 0 and 100", nil)
				}
			}
			if op.Type == annotateTypeRedact {
				if normalizeColor(op.Color) == "" {
					return models.NewCanonicalJobError("IMAGE_ANNOTATE_REDACT_COLOR_REQUIRED", suffix+".color is required for redact", nil)
				}
			}
			if op.Type == annotateTypeRect {
				if op.StrokeWidth < 1 || op.StrokeWidth > 64 {
					return models.NewCanonicalJobError("IMAGE_ANNOTATE_STROKE_INVALID", suffix+".strokeWidth must be between 1 and 64", nil)
				}
				if op.Opacity < 0 || op.Opacity > 1 {
					return models.NewCanonicalJobError("IMAGE_ANNOTATE_OPACITY_INVALID", suffix+".opacity must be between 0 and 1", nil)
				}
			}
		case annotateTypeArrow:
			if op.StrokeWidth < 1 || op.StrokeWidth > 64 {
				return models.NewCanonicalJobError("IMAGE_ANNOTATE_STROKE_INVALID", suffix+".strokeWidth must be between 1 and 64", nil)
			}
			if op.Opacity < 0 || op.Opacity > 1 {
				return models.NewCanonicalJobError("IMAGE_ANNOTATE_OPACITY_INVALID", suffix+".opacity must be between 0 and 1", nil)
			}
			if !pointInBounds(op.X, op.Y, imageWidth, imageHeight) || !pointInBounds(op.X2, op.Y2, imageWidth, imageHeight) {
				return models.NewCanonicalJobError("IMAGE_ANNOTATE_COORDINATES_INVALID", suffix+" arrow coordinates out of bounds", nil)
			}
			if op.X == op.X2 && op.Y == op.Y2 {
				return models.NewCanonicalJobError("IMAGE_ANNOTATE_ARROW_INVALID", suffix+" arrow requires two distinct points", nil)
			}
		default:
			return models.NewCanonicalJobError("IMAGE_ANNOTATE_OPERATION_UNSUPPORTED", fmt.Sprintf("%s has unsupported type: %s", suffix, op.Type), nil)
		}
	}

	return nil
}

func validateRectOp(op models.ImageAnnotateOperationV1, imageWidth, imageHeight int, suffix string) *models.JobErrorV1 {
	if op.Width < 1 || op.Height < 1 {
		return models.NewCanonicalJobError("IMAGE_ANNOTATE_DIMENSIONS_INVALID", suffix+" width/height must be >= 1", nil)
	}
	if op.X < 0 || op.Y < 0 {
		return models.NewCanonicalJobError("IMAGE_ANNOTATE_COORDINATES_INVALID", suffix+" x/y must be >= 0", nil)
	}
	if op.X+op.Width > imageWidth || op.Y+op.Height > imageHeight {
		return models.NewCanonicalJobError("IMAGE_ANNOTATE_OUT_OF_BOUNDS", suffix+" rectangle is outside image bounds", nil)
	}
	return nil
}

func pointInBounds(x, y, width, height int) bool {
	if x < 0 || y < 0 {
		return false
	}
	if x >= width || y >= height {
		return false
	}
	return true
}

// planSingleAnnotateOutput resolves the one output of a single request. An
// explicit outputPath keeps the old validation contract; otherwise the output
// is auto-named next to the input or inside the chosen folder.
func planSingleAnnotateOutput(parsed annotateRequest, outputFmt string, used map[string]struct{}) (models.PlannedOutputV1, *models.JobErrorV1) {
	inputPath := parsed.inputPaths[0]

	if explicitOutput := strings.TrimSpace(parsed.outputPath); explicitOutput != "" {
		return planExplicitAnnotateOutput(inputPath, explicitOutput)
	}

	targetDir := strings.TrimSpace(parsed.outputDir)
	if targetDir == "" {
		targetDir = filepath.Dir(inputPath)
	}

	return planAutoAnnotateOutput(inputPath, filepath.Clean(targetDir), outputFmt, used), nil
}

func planExplicitAnnotateOutput(inputPath, explicitOutput string) (models.PlannedOutputV1, *models.JobErrorV1) {
	planned := models.PlannedOutputV1{
		InputPath:  inputPath,
		OutputPath: filepath.Clean(strings.TrimSpace(explicitOutput)),
		OutputKind: imageOutputKindFile,
	}

	if fsx.SameFile(inputPath, planned.OutputPath) {
		return planned, models.NewCanonicalJobError("IMAGE_ANNOTATE_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
	}

	info, statErr := os.Stat(planned.OutputPath)
	switch {
	case statErr == nil:
		planned.Exists = true
		if info.IsDir() {
			return planned, models.NewCanonicalJobError(
				"IMAGE_ANNOTATE_OUTPUT_DIR_INVALID",
				fmt.Sprintf("%s is a folder, not a file.", filepath.Base(planned.OutputPath)),
				map[string]any{"outputPath": planned.OutputPath},
			)
		}
		return planned, models.NewCanonicalJobError(
			"IMAGE_ANNOTATE_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another name.", filepath.Base(planned.OutputPath)),
			map[string]any{"outputPath": planned.OutputPath},
		)
	case !errors.Is(statErr, os.ErrNotExist):
		return planned, models.NewCanonicalJobError(
			"IMAGE_ANNOTATE_OUTPUT_DIR_INVALID",
			fmt.Sprintf("%s cannot be accessed. Choose another location.", filepath.Base(planned.OutputPath)),
			map[string]any{"outputPath": planned.OutputPath},
		)
	}

	return planned, nil
}

// planAutoAnnotateOutput derives a collision-safe name inside dir. It never writes.
func planAutoAnnotateOutput(inputPath, dir, outputFmt string, used map[string]struct{}) models.PlannedOutputV1 {
	planned := models.PlannedOutputV1{
		InputPath:  inputPath,
		OutputKind: imageOutputKindFile,
	}

	base := annotateOutputBaseName(inputPath)
	outputPath, adjusted := outputplan.UniqueFilePath(dir, base, outputFmt, used)
	planned.OutputPath = outputPath

	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", base+"."+outputFmt, filepath.Base(outputPath)))
	}

	return planned
}

func annotateOutputBaseName(inputPath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		stem = "image"
	}

	return stem + "_annotated"
}

// preparedAnnotateFromPlan turns a planned output into executor input without
// resolving any path again.
func preparedAnnotateFromPlan(planned models.PlannedOutputV1, operations []models.ImageAnnotateOperationV1, effective map[string]any) preparedAnnotate {
	return preparedAnnotate{
		inputPath:  planned.InputPath,
		outputPath: planned.OutputPath,
		outputFmt:  plannedOutputFormat(planned, effective),
		operations: operations,
	}
}

func annotateEffectiveOptions(parsed annotateRequest, formats []string) map[string]any {
	effective := map[string]any{
		"operationCount": len(parsed.operations),
		"operations":     append([]models.ImageAnnotateOperationV1(nil), parsed.operations...),
	}
	if format, ok := commonPlannedFormat(formats); ok {
		effective["format"] = format
	}

	return effective
}

func validateAnnotateOutputDir(outputDir string) *models.JobErrorV1 {
	info, statErr := os.Stat(outputDir)
	if statErr != nil {
		return models.NewCanonicalJobError("IMAGE_ANNOTATE_OUTPUT_DIR_INVALID", fmt.Sprintf("outputDir is not accessible: %v", statErr), nil)
	}
	if !info.IsDir() {
		return models.NewCanonicalJobError("IMAGE_ANNOTATE_OUTPUT_DIR_INVALID", fmt.Sprintf("outputDir is not a folder: %s", outputDir), nil)
	}

	return nil
}

func annotatePlanMessage(count int) string {
	if count == 1 {
		return "Ready to annotate 1 image."
	}

	return fmt.Sprintf("Ready to annotate %d images.", count)
}

func executeAnnotateToPath(ctx context.Context, prepared preparedAnnotate) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("annotate cancelled: %w", ctx.Err())
	default:
	}

	bytes, _, _, _, err := readAndNormalize(prepared.inputPath)
	if err != nil {
		return fmt.Errorf("read input failed: %w", err)
	}

	annotated, err := applyOperations(bytes, prepared.outputFmt, prepared.operations)
	if err != nil {
		return fmt.Errorf("annotate failed: %w", err)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("annotate cancelled: %w", ctx.Err())
	default:
	}

	if mkErr := os.MkdirAll(filepath.Dir(prepared.outputPath), 0o755); mkErr != nil {
		return fmt.Errorf("create output dir failed: %w", mkErr)
	}

	if writeErr := os.WriteFile(prepared.outputPath, annotated, DefaultFilePermissions); writeErr != nil {
		return fmt.Errorf("write output failed: %w", writeErr)
	}

	return nil
}

func applyOperations(input []byte, outputFmt string, operations []models.ImageAnnotateOperationV1) ([]byte, error) {
	decoded, _, err := image.Decode(bytes.NewReader(input))
	if err != nil {
		return nil, fmt.Errorf("decode image: %w", err)
	}

	canvas := imaging.Clone(decoded)
	gc := gg.NewContextForImage(canvas)

	for _, op := range operations {
		switch op.Type {
		case annotateTypeRect:
			col, err := parseColorWithOpacity(op.Color, op.Opacity)
			if err != nil {
				return nil, err
			}
			gc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(col.A))
			gc.SetLineWidth(float64(op.StrokeWidth))
			gc.DrawRectangle(float64(op.X), float64(op.Y), float64(op.Width), float64(op.Height))
			gc.Stroke()
		case annotateTypeArrow:
			col, err := parseColorWithOpacity(op.Color, op.Opacity)
			if err != nil {
				return nil, err
			}
			gc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(col.A))
			gc.SetLineWidth(float64(op.StrokeWidth))
			gc.DrawLine(float64(op.X), float64(op.Y), float64(op.X2), float64(op.Y2))
			gc.Stroke()

			headPts := arrowHead(op.X, op.Y, op.X2, op.Y2, float64(maxInt(10, op.StrokeWidth*4)), float64(maxInt(5, op.StrokeWidth*2)))
			gc.DrawLine(headPts[0].x, headPts[0].y, headPts[1].x, headPts[1].y)
			gc.Stroke()
			gc.DrawLine(headPts[0].x, headPts[0].y, headPts[2].x, headPts[2].y)
			gc.Stroke()
		case annotateTypeText:
			col, err := parseColorWithOpacity(op.Color, 1)
			if err != nil {
				return nil, err
			}
			font, err := opentype.Parse(goregular.TTF)
			if err != nil {
				return nil, fmt.Errorf("parse font: %w", err)
			}
			face, err := opentype.NewFace(font, &opentype.FaceOptions{Size: float64(op.FontSize), DPI: 72})
			if err != nil {
				return nil, fmt.Errorf("create font face: %w", err)
			}
			gc.SetFontFace(face)
			gc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(col.A))
			lines := strings.Split(op.Text, "\n")
			lineHeight := float64(op.FontSize) * 1.3
			for i, line := range lines {
				gc.DrawString(line, float64(op.X), float64(op.Y)+lineHeight*float64(i+1))
			}
		case annotateTypeBlur:
			region := imaging.Crop(canvas, image.Rect(op.X, op.Y, op.X+op.Width, op.Y+op.Height))
			blurred := imaging.Blur(region, float64(blurIntensityToSigma(op.BlurIntensity)))
			draw.Draw(canvas, image.Rect(op.X, op.Y, op.X+op.Width, op.Y+op.Height), blurred, image.Point{}, draw.Over)
			gc = gg.NewContextForImage(canvas)
		case annotateTypeRedact:
			col, err := parseColorWithOpacity(op.Color, 1)
			if err != nil {
				return nil, err
			}
			gc.SetRGBA255(int(col.R), int(col.G), int(col.B), int(col.A))
			gc.DrawRectangle(float64(op.X), float64(op.Y), float64(op.Width), float64(op.Height))
			gc.Fill()
		default:
			return nil, fmt.Errorf("unsupported operation: %s", op.Type)
		}
	}

	result := gc.Image()
	var buf bytes.Buffer
	if err := png.Encode(&buf, result); err != nil {
		return nil, fmt.Errorf("encode intermediate png: %w", err)
	}

	return bimg.NewImage(buf.Bytes()).Convert(mapFormatToImageType(outputFmt))
}

func blurIntensityToSigma(intensity int) float64 {
	if intensity < 0 {
		intensity = 0
	}
	if intensity > 100 {
		intensity = 100
	}
	return 0.5 + (float64(intensity) * 0.2)
}

func annotateOptionString(options map[string]any, key string) string {
	if options == nil {
		return ""
	}
	v, _ := options[key].(string)
	return strings.TrimSpace(v)
}

func anyString(v any) string {
	s, _ := v.(string)
	return strings.TrimSpace(s)
}

func anyInt(v any) int {
	switch casted := v.(type) {
	case float64:
		return int(casted)
	case float32:
		return int(casted)
	case int:
		return casted
	case int64:
		return int(casted)
	case int32:
		return int(casted)
	case string:
		parsed, err := strconv.Atoi(strings.TrimSpace(casted))
		if err == nil {
			return parsed
		}
	}
	return 0
}

func anyFloat(v any) float64 {
	switch casted := v.(type) {
	case float64:
		return casted
	case float32:
		return float64(casted)
	case int:
		return float64(casted)
	case int64:
		return float64(casted)
	case string:
		parsed, err := strconv.ParseFloat(strings.TrimSpace(casted), 64)
		if err == nil {
			return parsed
		}
	}
	return 0
}

func normalizeColor(raw string) string {
	trimmed := strings.TrimSpace(strings.ToLower(raw))
	if trimmed == "" {
		return ""
	}
	if strings.HasPrefix(trimmed, "#") {
		trimmed = strings.TrimPrefix(trimmed, "#")
	}
	if len(trimmed) == 3 {
		trimmed = fmt.Sprintf("%c%c%c%c%c%c", trimmed[0], trimmed[0], trimmed[1], trimmed[1], trimmed[2], trimmed[2])
	}
	if len(trimmed) != 6 {
		return ""
	}
	for _, c := range trimmed {
		if (c < '0' || c > '9') && (c < 'a' || c > 'f') {
			return ""
		}
	}
	return "#" + trimmed
}

func parseColorWithOpacity(hex string, opacity float64) (color.NRGBA, error) {
	normalized := normalizeColor(hex)
	if normalized == "" {
		normalized = "#ff0000"
	}
	if opacity < 0 || opacity > 1 {
		return color.NRGBA{}, fmt.Errorf("opacity must be between 0 and 1")
	}

	r, err := strconv.ParseUint(normalized[1:3], 16, 8)
	if err != nil {
		return color.NRGBA{}, err
	}
	g, err := strconv.ParseUint(normalized[3:5], 16, 8)
	if err != nil {
		return color.NRGBA{}, err
	}
	b, err := strconv.ParseUint(normalized[5:7], 16, 8)
	if err != nil {
		return color.NRGBA{}, err
	}
	return color.NRGBA{R: uint8(r), G: uint8(g), B: uint8(b), A: uint8(math.Round(opacity * 255))}, nil
}

type point struct {
	x float64
	y float64
}

func arrowHead(x1, y1, x2, y2 int, length, width float64) [3]point {
	dx := float64(x2 - x1)
	dy := float64(y2 - y1)
	mag := math.Hypot(dx, dy)
	if mag == 0 {
		return [3]point{{x: float64(x2), y: float64(y2)}, {x: float64(x2), y: float64(y2)}, {x: float64(x2), y: float64(y2)}}
	}
	ux := dx / mag
	uy := dy / mag
	bx := float64(x2) - ux*length
	by := float64(y2) - uy*length
	px := -uy
	py := ux
	return [3]point{
		{x: float64(x2), y: float64(y2)},
		{x: bx + px*width, y: by + py*width},
		{x: bx - px*width, y: by - py*width},
	}
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
