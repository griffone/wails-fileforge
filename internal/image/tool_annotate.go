package image

import (
	"bytes"
	"context"
	"encoding/base64"
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
	inputPath    string
	outputPath   string
	outputFmt    string
	operations   []models.ImageAnnotateOperationV1
	canvasWidth  int
	canvasHeight int
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

func (t *AnnotateTool) Validate(_ context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	parsed, jobErr := parseAnnotateRequest(req)
	if jobErr != nil {
		return jobErr
	}

	if parsed.mode == "single" {
		prepared, prepErr := t.prepareSingle(parsed)
		if prepErr != nil {
			return prepErr
		}

		if opErr := validateOperationsForImage(prepared.operations, prepared.canvasWidth, prepared.canvasHeight); opErr != nil {
			return opErr
		}
	}

	if parsed.mode == "batch" {
		if strings.TrimSpace(parsed.outputDir) != "" {
			if _, statErr := os.Stat(parsed.outputDir); statErr != nil {
				return models.NewCanonicalJobError("IMAGE_ANNOTATE_OUTPUT_DIR_INVALID", fmt.Sprintf("outputDir is not accessible: %v", statErr), nil)
			}
		}

		for _, inputPath := range parsed.inputPaths {
			bytes, _, width, height, err := readAndNormalize(inputPath)
			if err != nil {
				return models.NewCanonicalJobError("IMAGE_ANNOTATE_READ_FAILED", err.Error(), map[string]any{"inputPath": inputPath})
			}
			_ = bytes
			if opErr := validateOperationsForImage(parsed.operations, width, height); opErr != nil {
				return opErr
			}
		}

		// NOTE: Stage 2 batch mode applies the same annotation operations to all files.
		// TODO(stage-3): support per-file annotation operation sets in one batch request.
	}

	return nil
}

func (t *AnnotateTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	parsed, jobErr := parseAnnotateRequest(req)
	if jobErr != nil {
		return models.JobResultItemV1{Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	prepared, prepErr := t.prepareSingle(parsed)
	if prepErr != nil {
		return models.JobResultItemV1{InputPath: firstPath(parsed.inputPaths), OutputPath: parsed.outputPath, Success: false, Message: prepErr.Message, Error: prepErr}, prepErr
	}

	if opErr := validateOperationsForImage(prepared.operations, prepared.canvasWidth, prepared.canvasHeight); opErr != nil {
		return models.JobResultItemV1{InputPath: prepared.inputPath, OutputPath: prepared.outputPath, Success: false, Message: opErr.Message, Error: opErr}, opErr
	}

	if err := executeAnnotateToPath(ctx, prepared); err != nil {
		jobErr = models.NewCanonicalJobError("IMAGE_ANNOTATE_EXECUTION", err.Error(), map[string]any{"inputPath": prepared.inputPath})
		return models.JobResultItemV1{InputPath: prepared.inputPath, OutputPath: prepared.outputPath, Success: false, Message: "image annotate failed", Error: jobErr}, jobErr
	}

	return models.JobResultItemV1{
		InputPath:   prepared.inputPath,
		OutputPath:  prepared.outputPath,
		Outputs:     []string{prepared.outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     "image annotate successful",
	}, nil
}

func (t *AnnotateTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, jobErr := parseAnnotateRequest(req)
	if jobErr != nil {
		return nil, jobErr
	}

	if parsed.mode != "batch" {
		return nil, models.NewCanonicalJobError("IMAGE_ANNOTATE_MODE_INVALID", "mode must be batch", nil)
	}

	usedOutputs := make(map[string]struct{}, len(parsed.inputPaths))
	items := make([]models.JobResultItemV1, 0, len(parsed.inputPaths))
	var firstErr *models.JobErrorV1

	for idx, inputPath := range parsed.inputPaths {
		select {
		case <-ctx.Done():
			cancelErr := models.NewCanonicalJobError("IMAGE_ANNOTATE_CANCELLED", ctx.Err().Error(), nil)
			return items, cancelErr
		default:
		}

		prepared, prepErr := t.prepareForInput(parsed, inputPath, usedOutputs)
		if prepErr != nil {
			if firstErr == nil {
				firstErr = prepErr
			}
			items = append(items, models.JobResultItemV1{InputPath: inputPath, OutputPath: "", Success: false, Message: prepErr.Message, Error: prepErr})
			if onProgress != nil {
				onProgress(models.JobProgressV1{Current: idx + 1, Total: len(parsed.inputPaths), Stage: models.JobStatusRunning, Message: fmt.Sprintf("processed %d/%d", idx+1, len(parsed.inputPaths))})
			}
			continue
		}

		if opErr := validateOperationsForImage(prepared.operations, prepared.canvasWidth, prepared.canvasHeight); opErr != nil {
			if firstErr == nil {
				firstErr = opErr
			}
			items = append(items, models.JobResultItemV1{InputPath: prepared.inputPath, OutputPath: prepared.outputPath, Success: false, Message: opErr.Message, Error: opErr})
			if onProgress != nil {
				onProgress(models.JobProgressV1{Current: idx + 1, Total: len(parsed.inputPaths), Stage: models.JobStatusRunning, Message: fmt.Sprintf("processed %d/%d", idx+1, len(parsed.inputPaths))})
			}
			continue
		}

		if err := executeAnnotateToPath(ctx, prepared); err != nil {
			itemErr := models.NewCanonicalJobError("IMAGE_ANNOTATE_BATCH_ITEM", err.Error(), map[string]any{"inputPath": prepared.inputPath})
			if firstErr == nil {
				firstErr = itemErr
			}
			items = append(items, models.JobResultItemV1{InputPath: prepared.inputPath, OutputPath: prepared.outputPath, Success: false, Message: "image annotate failed", Error: itemErr})
		} else {
			items = append(items, models.JobResultItemV1{InputPath: prepared.inputPath, OutputPath: prepared.outputPath, Outputs: []string{prepared.outputPath}, OutputCount: 1, Success: true, Message: "image annotate successful"})
		}

		if onProgress != nil {
			onProgress(models.JobProgressV1{Current: idx + 1, Total: len(parsed.inputPaths), Stage: models.JobStatusRunning, Message: fmt.Sprintf("processed %d/%d", idx+1, len(parsed.inputPaths))})
		}
	}

	return items, firstErr
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

	parsed := annotateRequest{
		mode:       mode,
		inputPaths: inputPaths,
		outputPath: strings.TrimSpace(annotateOptionString(req.Options, "outputPath")),
		outputDir:  strings.TrimSpace(req.OutputDir),
		format:     strings.ToLower(strings.TrimSpace(annotateOptionString(req.Options, "format"))),
		operations: normalized,
	}

	if mode == "single" {
		if reqOutDir := strings.TrimSpace(annotateOptionString(req.Options, "outputDir")); reqOutDir != "" {
			parsed.outputDir = reqOutDir
		}
	}

	if mode == "batch" && strings.TrimSpace(annotateOptionString(req.Options, "outputDir")) != "" {
		parsed.outputDir = strings.TrimSpace(annotateOptionString(req.Options, "outputDir"))
	}

	return parsed, nil
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

func (t *AnnotateTool) prepareSingle(parsed annotateRequest) (preparedAnnotate, *models.JobErrorV1) {
	inputPath := firstPath(parsed.inputPaths)
	if err := validateInputImagePath(inputPath); err != nil {
		return preparedAnnotate{}, err
	}

	outputFmt, fmtErr := resolveOutputFormat(inputPath, parsed.format)
	if fmtErr != nil {
		return preparedAnnotate{}, fmtErr
	}

	outputPath := strings.TrimSpace(parsed.outputPath)
	if outputPath == "" {
		outputDir := strings.TrimSpace(parsed.outputDir)
		if outputDir == "" {
			outputDir = filepath.Dir(inputPath)
		}
		outputPath = nextAvailableAnnotatedOutputPath(outputDir, inputPath, outputFmt, nil)
	}

	if sameFile(inputPath, outputPath) {
		return preparedAnnotate{}, models.NewCanonicalJobError("IMAGE_ANNOTATE_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
	}

	_, _, width, height, err := readAndNormalize(inputPath)
	if err != nil {
		return preparedAnnotate{}, models.NewCanonicalJobError("IMAGE_ANNOTATE_READ_FAILED", err.Error(), map[string]any{"inputPath": inputPath})
	}

	return preparedAnnotate{
		inputPath:    inputPath,
		outputPath:   outputPath,
		outputFmt:    outputFmt,
		operations:   parsed.operations,
		canvasWidth:  width,
		canvasHeight: height,
	}, nil
}

func (t *AnnotateTool) prepareForInput(parsed annotateRequest, inputPath string, usedOutputs map[string]struct{}) (preparedAnnotate, *models.JobErrorV1) {
	if err := validateInputImagePath(inputPath); err != nil {
		return preparedAnnotate{}, err
	}

	outputFmt, fmtErr := resolveOutputFormat(inputPath, parsed.format)
	if fmtErr != nil {
		return preparedAnnotate{}, fmtErr
	}

	outputPath := nextAvailableAnnotatedOutputPath(parsed.outputDir, inputPath, outputFmt, usedOutputs)
	if sameFile(inputPath, outputPath) {
		return preparedAnnotate{}, models.NewCanonicalJobError("IMAGE_ANNOTATE_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
	}

	_, _, width, height, err := readAndNormalize(inputPath)
	if err != nil {
		return preparedAnnotate{}, models.NewCanonicalJobError("IMAGE_ANNOTATE_READ_FAILED", err.Error(), map[string]any{"inputPath": inputPath})
	}

	return preparedAnnotate{
		inputPath:    inputPath,
		outputPath:   outputPath,
		outputFmt:    outputFmt,
		operations:   parsed.operations,
		canvasWidth:  width,
		canvasHeight: height,
	}, nil
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

func nextAvailableAnnotatedOutputPath(outputDir, inputPath, format string, used map[string]struct{}) string {
	baseName := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if strings.TrimSpace(baseName) == "" {
		baseName = "image"
	}

	if strings.TrimSpace(outputDir) == "" {
		outputDir = filepath.Dir(inputPath)
	}

	candidateBase := filepath.Join(outputDir, fmt.Sprintf("%s_annotated", baseName))
	first := fmt.Sprintf("%s.%s", candidateBase, format)
	if isOutputAvailable(first, used) {
		markUsed(first, used)
		return first
	}

	for suffix := 2; ; suffix++ {
		candidate := fmt.Sprintf("%s-%d.%s", candidateBase, suffix, format)
		if isOutputAvailable(candidate, used) {
			markUsed(candidate, used)
			return candidate
		}
	}
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
