package image

import (
	"context"
	"encoding/base64"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fileforge-desktop/internal/models"

	"github.com/h2non/bimg"
)

const ToolIDImageCropV1 = "tool.image.crop"

var imageMimeByFormat = map[string]string{
	"jpeg": "image/jpeg",
	"png":  "image/png",
	"webp": "image/webp",
	"gif":  "image/gif",
	"tiff": "image/tiff",
}

type CropTool struct{}

type cropRequest struct {
	mode        string
	inputPaths  []string
	outputPath  string
	outputDir   string
	x           int
	y           int
	width       int
	height      int
	ratioPreset string
	format      string
}

type preparedCrop struct {
	inputPath   string
	outputPath  string
	outputFmt   string
	x           int
	y           int
	width       int
	height      int
	ratioPreset string
}

func NewCropTool() *CropTool {
	return &CropTool{}
}

func (t *CropTool) ID() string {
	return ToolIDImageCropV1
}

func (t *CropTool) Capability() string {
	return ToolIDImageCropV1
}

func (t *CropTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Image Crop",
		Description:      "Crop images with ratio presets, batch mode and safe non-destructive output",
		Domain:           "image",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff", "tif"},
		OutputExtensions: []string{"jpeg", "png", "webp", "gif", "tiff"},
		RuntimeDeps:      []string{"libvips"},
		Tags:             []string{"image", "crop", "ratio", "batch"},
	}
}

func (t *CropTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *CropTool) Validate(_ context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	parsed, jobErr := parseCropRequest(req)
	if jobErr != nil {
		return jobErr
	}

	if parsed.mode == "single" {
		prepared, prepErr := t.prepareSingle(parsed)
		if prepErr != nil {
			return prepErr
		}

		if areaErr := validateCropBounds(prepared.inputPath, prepared.x, prepared.y, prepared.width, prepared.height); areaErr != nil {
			return areaErr
		}
	}

	if parsed.mode == "batch" {
		if strings.TrimSpace(parsed.outputDir) != "" {
			if _, statErr := os.Stat(parsed.outputDir); statErr != nil {
				return models.NewCanonicalJobError("IMAGE_CROP_OUTPUT_DIR_INVALID", fmt.Sprintf("outputDir is not accessible: %v", statErr), nil)
			}
		}

		// NOTE: Stage 1 batch mode applies the same crop rectangle to all files.
		// TODO(stage-2): support per-file crop rectangles in one batch request.
	}

	return nil
}

func (t *CropTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	parsed, jobErr := parseCropRequest(req)
	if jobErr != nil {
		return models.JobResultItemV1{Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	prepared, prepErr := t.prepareSingle(parsed)
	if prepErr != nil {
		return models.JobResultItemV1{InputPath: firstPath(parsed.inputPaths), OutputPath: parsed.outputPath, Success: false, Message: prepErr.Message, Error: prepErr}, prepErr
	}

	if areaErr := validateCropBounds(prepared.inputPath, prepared.x, prepared.y, prepared.width, prepared.height); areaErr != nil {
		return models.JobResultItemV1{InputPath: prepared.inputPath, OutputPath: prepared.outputPath, Success: false, Message: areaErr.Message, Error: areaErr}, areaErr
	}

	if err := executeCropToPath(ctx, prepared); err != nil {
		jobErr = models.NewCanonicalJobError("IMAGE_CROP_EXECUTION", err.Error(), map[string]any{"inputPath": prepared.inputPath})
		return models.JobResultItemV1{InputPath: prepared.inputPath, OutputPath: prepared.outputPath, Success: false, Message: "image crop failed", Error: jobErr}, jobErr
	}

	return models.JobResultItemV1{
		InputPath:   prepared.inputPath,
		OutputPath:  prepared.outputPath,
		Outputs:     []string{prepared.outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     "image crop successful",
	}, nil
}

func (t *CropTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, jobErr := parseCropRequest(req)
	if jobErr != nil {
		return nil, jobErr
	}

	if parsed.mode != "batch" {
		return nil, models.NewCanonicalJobError("IMAGE_CROP_MODE_INVALID", "mode must be batch", nil)
	}

	usedOutputs := make(map[string]struct{}, len(parsed.inputPaths))
	items := make([]models.JobResultItemV1, 0, len(parsed.inputPaths))
	var firstErr *models.JobErrorV1

	for idx, inputPath := range parsed.inputPaths {
		select {
		case <-ctx.Done():
			cancelErr := models.NewCanonicalJobError("IMAGE_CROP_CANCELLED", ctx.Err().Error(), nil)
			return items, cancelErr
		default:
		}

		prepared, prepErr := t.prepareForInput(parsed, inputPath, usedOutputs)
		if prepErr != nil {
			if firstErr == nil {
				firstErr = prepErr
			}
			items = append(items, models.JobResultItemV1{
				InputPath:  inputPath,
				OutputPath: "",
				Success:    false,
				Message:    prepErr.Message,
				Error:      prepErr,
			})
			if onProgress != nil {
				onProgress(models.JobProgressV1{Current: idx + 1, Total: len(parsed.inputPaths), Stage: models.JobStatusRunning, Message: fmt.Sprintf("processed %d/%d", idx+1, len(parsed.inputPaths))})
			}
			continue
		}

		if areaErr := validateCropBounds(prepared.inputPath, prepared.x, prepared.y, prepared.width, prepared.height); areaErr != nil {
			if firstErr == nil {
				firstErr = areaErr
			}
			items = append(items, models.JobResultItemV1{
				InputPath:  prepared.inputPath,
				OutputPath: prepared.outputPath,
				Success:    false,
				Message:    areaErr.Message,
				Error:      areaErr,
			})
			if onProgress != nil {
				onProgress(models.JobProgressV1{Current: idx + 1, Total: len(parsed.inputPaths), Stage: models.JobStatusRunning, Message: fmt.Sprintf("processed %d/%d", idx+1, len(parsed.inputPaths))})
			}
			continue
		}

		if err := executeCropToPath(ctx, prepared); err != nil {
			itemErr := models.NewCanonicalJobError("IMAGE_CROP_BATCH_ITEM", err.Error(), map[string]any{"inputPath": prepared.inputPath})
			if firstErr == nil {
				firstErr = itemErr
			}
			items = append(items, models.JobResultItemV1{
				InputPath:  prepared.inputPath,
				OutputPath: prepared.outputPath,
				Success:    false,
				Message:    "image crop failed",
				Error:      itemErr,
			})
		} else {
			items = append(items, models.JobResultItemV1{
				InputPath:   prepared.inputPath,
				OutputPath:  prepared.outputPath,
				Outputs:     []string{prepared.outputPath},
				OutputCount: 1,
				Success:     true,
				Message:     "image crop successful",
			})
		}

		if onProgress != nil {
			onProgress(models.JobProgressV1{Current: idx + 1, Total: len(parsed.inputPaths), Stage: models.JobStatusRunning, Message: fmt.Sprintf("processed %d/%d", idx+1, len(parsed.inputPaths))})
		}
	}

	return items, firstErr
}

func (t *CropTool) GetImagePreviewSource(inputPath string) models.ImagePreviewSourceResponseV1 {
	path := strings.TrimSpace(inputPath)
	if path == "" {
		return models.ImagePreviewSourceResponseV1{
			Success: false,
			Message: "Select a valid image path and retry.",
			Error:   models.NewCanonicalJobError("IMAGE_PREVIEW_INVALID_PATH", "inputPath is required", nil),
		}
	}

	bytes, fmtName, width, height, err := readAndNormalize(path)
	if err != nil {
		jobErr := models.NewCanonicalJobError("IMAGE_PREVIEW_READ_FAILED", err.Error(), nil)
		return models.ImagePreviewSourceResponseV1{Success: false, Message: "Cannot load image preview source.", Error: jobErr}
	}

	mimeType := imageMimeByFormat[fmtName]
	if mimeType == "" {
		mimeType = "application/octet-stream"
	}

	return models.ImagePreviewSourceResponseV1{
		Success:    true,
		Message:    "image preview source loaded",
		DataBase64: base64.StdEncoding.EncodeToString(bytes),
		MimeType:   mimeType,
		Width:      width,
		Height:     height,
	}
}

func (t *CropTool) GetImageCropPreview(req models.ImageCropPreviewRequestV1) models.ImageCropPreviewResponseV1 {
	inputPath := strings.TrimSpace(req.InputPath)
	if inputPath == "" {
		return models.ImageCropPreviewResponseV1{
			Success: false,
			Message: "Select a valid image path and retry.",
			Error:   models.NewCanonicalJobError("IMAGE_CROP_PREVIEW_INVALID_PATH", "inputPath is required", nil),
		}
	}

	if req.Width < 1 || req.Height < 1 {
		return models.ImageCropPreviewResponseV1{
			Success: false,
			Message: "Crop area must be at least 1x1.",
			Error: models.NewCanonicalJobError(
				"IMAGE_CROP_DIMENSIONS_INVALID",
				"width and height must be >= 1",
				nil,
			),
		}
	}

	if ratioErr := validateRatioPreset(strings.TrimSpace(req.RatioPreset), req.Width, req.Height); ratioErr != nil {
		return models.ImageCropPreviewResponseV1{Success: false, Message: ratioErr.Message, Error: ratioErr}
	}

	bytes, fmtName, _, _, err := readAndNormalize(inputPath)
	if err != nil {
		jobErr := models.NewCanonicalJobError("IMAGE_CROP_PREVIEW_READ_FAILED", err.Error(), nil)
		return models.ImageCropPreviewResponseV1{Success: false, Message: "Cannot load image preview source.", Error: jobErr}
	}

	img := bimg.NewImage(bytes)
	size, sizeErr := img.Size()
	if sizeErr != nil {
		jobErr := models.NewCanonicalJobError("IMAGE_CROP_DIMENSIONS_READ_FAILED", sizeErr.Error(), nil)
		return models.ImageCropPreviewResponseV1{Success: false, Message: "Cannot inspect image dimensions.", Error: jobErr}
	}

	if boundsErr := validateBoundsAgainstSize(req.X, req.Y, req.Width, req.Height, size.Width, size.Height); boundsErr != nil {
		return models.ImageCropPreviewResponseV1{Success: false, Message: boundsErr.Message, Error: boundsErr}
	}

	outputFmt, fmtErr := resolveOutputFormat(inputPath, strings.TrimSpace(req.Format))
	if fmtErr != nil {
		return models.ImageCropPreviewResponseV1{Success: false, Message: fmtErr.Message, Error: fmtErr}
	}

	croppedBytes, err := cropInMemory(bytes, req.X, req.Y, req.Width, req.Height, outputFmt)
	if err != nil {
		jobErr := models.NewCanonicalJobError("IMAGE_CROP_PREVIEW_EXECUTION", err.Error(), nil)
		return models.ImageCropPreviewResponseV1{Success: false, Message: "Failed to generate preview.", Error: jobErr}
	}

	mimeType := imageMimeByFormat[outputFmt]
	if mimeType == "" {
		mimeType = imageMimeByFormat[fmtName]
	}

	return models.ImageCropPreviewResponseV1{
		Success:    true,
		Message:    "image crop preview generated",
		DataBase64: base64.StdEncoding.EncodeToString(croppedBytes),
		MimeType:   mimeType,
		Width:      req.Width,
		Height:     req.Height,
	}
}

func parseCropRequest(req models.JobRequestV1) (cropRequest, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return cropRequest{}, models.NewCanonicalJobError("IMAGE_CROP_MODE_INVALID", "mode must be single or batch", nil)
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return cropRequest{}, models.NewCanonicalJobError("IMAGE_CROP_SINGLE_INPUT_COUNT", "single mode requires exactly one input", nil)
	}

	if mode == "batch" && len(req.InputPaths) < 1 {
		return cropRequest{}, models.NewCanonicalJobError("IMAGE_CROP_BATCH_INPUT_REQUIRED", "batch mode requires at least one input", nil)
	}

	inputs := make([]string, 0, len(req.InputPaths))
	for _, rawPath := range req.InputPaths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			return cropRequest{}, models.NewCanonicalJobError("IMAGE_CROP_INPUT_REQUIRED", "inputPaths contains empty item", nil)
		}
		inputs = append(inputs, trimmed)
	}

	x, xErr := optionInt(req.Options, "x")
	if xErr != nil {
		return cropRequest{}, xErr
	}
	y, yErr := optionInt(req.Options, "y")
	if yErr != nil {
		return cropRequest{}, yErr
	}
	width, wErr := optionInt(req.Options, "width")
	if wErr != nil {
		return cropRequest{}, wErr
	}
	height, hErr := optionInt(req.Options, "height")
	if hErr != nil {
		return cropRequest{}, hErr
	}

	if x < 0 || y < 0 {
		return cropRequest{}, models.NewCanonicalJobError("IMAGE_CROP_COORDINATES_INVALID", "x and y must be >= 0 (origin top-left)", nil)
	}

	if width < 1 || height < 1 {
		return cropRequest{}, models.NewCanonicalJobError("IMAGE_CROP_DIMENSIONS_INVALID", "width and height must be >= 1", nil)
	}

	ratioPreset := strings.TrimSpace(cropOptionString(req.Options, "ratioPreset"))
	if ratioPreset == "" {
		ratioPreset = "free"
	}

	if ratioErr := validateRatioPreset(ratioPreset, width, height); ratioErr != nil {
		return cropRequest{}, ratioErr
	}

	parsed := cropRequest{
		mode:        mode,
		inputPaths:  inputs,
		outputPath:  strings.TrimSpace(cropOptionString(req.Options, "outputPath")),
		outputDir:   strings.TrimSpace(req.OutputDir),
		x:           x,
		y:           y,
		width:       width,
		height:      height,
		ratioPreset: ratioPreset,
		format:      strings.ToLower(strings.TrimSpace(cropOptionString(req.Options, "format"))),
	}

	if parsed.mode == "single" {
		if reqOutDir := strings.TrimSpace(cropOptionString(req.Options, "outputDir")); reqOutDir != "" {
			parsed.outputDir = reqOutDir
		}
	}

	if parsed.mode == "batch" && strings.TrimSpace(cropOptionString(req.Options, "outputDir")) != "" {
		parsed.outputDir = strings.TrimSpace(cropOptionString(req.Options, "outputDir"))
	}

	return parsed, nil
}

func (t *CropTool) prepareSingle(parsed cropRequest) (preparedCrop, *models.JobErrorV1) {
	inputPath := firstPath(parsed.inputPaths)
	if err := validateInputImagePath(inputPath); err != nil {
		return preparedCrop{}, err
	}

	outputFmt, fmtErr := resolveOutputFormat(inputPath, parsed.format)
	if fmtErr != nil {
		return preparedCrop{}, fmtErr
	}

	outputPath := strings.TrimSpace(parsed.outputPath)
	if outputPath == "" {
		outputDir := strings.TrimSpace(parsed.outputDir)
		if outputDir == "" {
			outputDir = filepath.Dir(inputPath)
		}
		outputPath = nextAvailableOutputPath(outputDir, inputPath, outputFmt, nil)
	}

	if sameFile(inputPath, outputPath) {
		return preparedCrop{}, models.NewCanonicalJobError("IMAGE_CROP_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
	}

	return preparedCrop{
		inputPath:   inputPath,
		outputPath:  outputPath,
		outputFmt:   outputFmt,
		x:           parsed.x,
		y:           parsed.y,
		width:       parsed.width,
		height:      parsed.height,
		ratioPreset: parsed.ratioPreset,
	}, nil
}

func (t *CropTool) prepareForInput(parsed cropRequest, inputPath string, usedOutputs map[string]struct{}) (preparedCrop, *models.JobErrorV1) {
	if err := validateInputImagePath(inputPath); err != nil {
		return preparedCrop{}, err
	}

	outputFmt, fmtErr := resolveOutputFormat(inputPath, parsed.format)
	if fmtErr != nil {
		return preparedCrop{}, fmtErr
	}

	outputPath := nextAvailableOutputPath(parsed.outputDir, inputPath, outputFmt, usedOutputs)
	if sameFile(inputPath, outputPath) {
		return preparedCrop{}, models.NewCanonicalJobError("IMAGE_CROP_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
	}

	return preparedCrop{
		inputPath:   inputPath,
		outputPath:  outputPath,
		outputFmt:   outputFmt,
		x:           parsed.x,
		y:           parsed.y,
		width:       parsed.width,
		height:      parsed.height,
		ratioPreset: parsed.ratioPreset,
	}, nil
}

func executeCropToPath(ctx context.Context, prepared preparedCrop) error {
	select {
	case <-ctx.Done():
		return fmt.Errorf("crop cancelled: %w", ctx.Err())
	default:
	}

	bytes, _, _, _, err := readAndNormalize(prepared.inputPath)
	if err != nil {
		return fmt.Errorf("read input failed: %w", err)
	}

	if err := validateCropBoundsFromBytes(bytes, prepared.x, prepared.y, prepared.width, prepared.height); err != nil {
		return fmt.Errorf("bounds validation failed: %s", err.Message)
	}

	cropped, err := cropInMemory(bytes, prepared.x, prepared.y, prepared.width, prepared.height, prepared.outputFmt)
	if err != nil {
		return fmt.Errorf("crop failed: %w", err)
	}

	select {
	case <-ctx.Done():
		return fmt.Errorf("crop cancelled: %w", ctx.Err())
	default:
	}

	if mkErr := os.MkdirAll(filepath.Dir(prepared.outputPath), 0o755); mkErr != nil {
		return fmt.Errorf("create output dir failed: %w", mkErr)
	}

	if writeErr := os.WriteFile(prepared.outputPath, cropped, DefaultFilePermissions); writeErr != nil {
		return fmt.Errorf("write output failed: %w", writeErr)
	}

	return nil
}

func readAndNormalize(inputPath string) ([]byte, string, int, int, error) {
	if strings.TrimSpace(inputPath) == "" {
		return nil, "", 0, 0, fmt.Errorf("inputPath is required")
	}

	inputBytes, err := os.ReadFile(inputPath)
	if err != nil {
		return nil, "", 0, 0, fmt.Errorf("read input file: %w", err)
	}

	img := bimg.NewImage(inputBytes)
	oriented, rotateErr := img.AutoRotate()
	if rotateErr != nil {
		return nil, "", 0, 0, fmt.Errorf("normalize exif orientation: %w", rotateErr)
	}

	fmtName := strings.ToLower(strings.TrimSpace(bimg.DetermineImageTypeName(oriented)))
	if fmtName == "jpg" {
		fmtName = "jpeg"
	}

	size, sizeErr := bimg.NewImage(oriented).Size()
	if sizeErr != nil {
		return nil, "", 0, 0, fmt.Errorf("read image size: %w", sizeErr)
	}

	return oriented, fmtName, size.Width, size.Height, nil
}

func cropInMemory(input []byte, x, y, width, height int, outputFmt string) ([]byte, error) {
	img := bimg.NewImage(input)
	extracted, err := img.Extract(y, x, width, height)
	if err != nil {
		return nil, err
	}

	return bimg.NewImage(extracted).Convert(mapFormatToImageType(outputFmt))
}

func validateCropBounds(inputPath string, x, y, width, height int) *models.JobErrorV1 {
	bytes, _, _, _, err := readAndNormalize(inputPath)
	if err != nil {
		return models.NewCanonicalJobError("IMAGE_CROP_READ_FAILED", err.Error(), map[string]any{"inputPath": inputPath})
	}

	return validateCropBoundsFromBytes(bytes, x, y, width, height)
}

func validateCropBoundsFromBytes(input []byte, x, y, width, height int) *models.JobErrorV1 {
	size, sizeErr := bimg.NewImage(input).Size()
	if sizeErr != nil {
		return models.NewCanonicalJobError("IMAGE_CROP_DIMENSIONS_READ_FAILED", sizeErr.Error(), nil)
	}

	return validateBoundsAgainstSize(x, y, width, height, size.Width, size.Height)
}

func validateBoundsAgainstSize(x, y, width, height, imageWidth, imageHeight int) *models.JobErrorV1 {
	if x < 0 || y < 0 {
		return models.NewCanonicalJobError("IMAGE_CROP_COORDINATES_INVALID", "x and y must be >= 0", nil)
	}

	if width < 1 || height < 1 {
		return models.NewCanonicalJobError("IMAGE_CROP_DIMENSIONS_INVALID", "width and height must be >= 1", nil)
	}

	if x+width > imageWidth || y+height > imageHeight {
		return models.NewCanonicalJobError(
			"IMAGE_CROP_OUT_OF_BOUNDS",
			fmt.Sprintf("crop area (%d,%d,%d,%d) is outside image bounds (%d,%d)", x, y, width, height, imageWidth, imageHeight),
			nil,
		)
	}

	return nil
}

func validateRatioPreset(preset string, width, height int) *models.JobErrorV1 {
	normalized := strings.ToLower(strings.TrimSpace(preset))
	if normalized == "" {
		normalized = "free"
	}

	if width < 1 || height < 1 {
		return models.NewCanonicalJobError("IMAGE_CROP_DIMENSIONS_INVALID", "width and height must be >= 1", nil)
	}

	if normalized == "free" {
		return nil
	}

	parts := strings.Split(normalized, ":")
	if len(parts) != 2 {
		return models.NewCanonicalJobError("IMAGE_CROP_RATIO_INVALID", "ratioPreset must be one of: free, 1:1, 4:3, 16:9", nil)
	}

	numerator, nErr := strconv.Atoi(strings.TrimSpace(parts[0]))
	denominator, dErr := strconv.Atoi(strings.TrimSpace(parts[1]))
	if nErr != nil || dErr != nil || numerator < 1 || denominator < 1 {
		return models.NewCanonicalJobError("IMAGE_CROP_RATIO_INVALID", "ratioPreset must be one of: free, 1:1, 4:3, 16:9", nil)
	}

	if normalized != "1:1" && normalized != "4:3" && normalized != "16:9" {
		return models.NewCanonicalJobError("IMAGE_CROP_RATIO_INVALID", "ratioPreset must be one of: free, 1:1, 4:3, 16:9", nil)
	}

	if width*denominator != height*numerator {
		return models.NewCanonicalJobError(
			"IMAGE_CROP_RATIO_INVALID",
			fmt.Sprintf("ratioPreset %s requires width:height to match %s", normalized, normalized),
			nil,
		)
	}

	return nil
}

func validateInputImagePath(inputPath string) *models.JobErrorV1 {
	if strings.TrimSpace(inputPath) == "" {
		return models.NewCanonicalJobError("IMAGE_CROP_INPUT_REQUIRED", "inputPath is required", nil)
	}

	if _, statErr := os.Stat(inputPath); statErr != nil {
		return models.NewCanonicalJobError("IMAGE_CROP_INPUT_NOT_FOUND", statErr.Error(), nil)
	}

	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(inputPath), "."))
	supported := map[string]bool{
		"jpg": true, "jpeg": true, "png": true, "gif": true, "webp": true, "bmp": true, "tiff": true, "tif": true,
	}
	if !supported[ext] {
		return models.NewCanonicalJobError("IMAGE_CROP_INPUT_UNSUPPORTED", fmt.Sprintf("unsupported input extension: %s", ext), nil)
	}

	return nil
}

func resolveOutputFormat(inputPath, requested string) (string, *models.JobErrorV1) {
	format := strings.ToLower(strings.TrimSpace(requested))
	if format == "jpg" {
		format = "jpeg"
	}

	if format == "" {
		ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(inputPath), "."))
		if ext == "jpg" {
			ext = "jpeg"
		}
		if ext == "tif" {
			ext = "tiff"
		}
		format = ext
	}

	if mapFormatToImageType(format) == bimg.UNKNOWN {
		return "", models.NewCanonicalJobError("IMAGE_CROP_FORMAT_UNSUPPORTED", fmt.Sprintf("unsupported output format: %s", format), nil)
	}

	return format, nil
}

func mapFormatToImageType(format string) bimg.ImageType {
	switch strings.ToLower(strings.TrimSpace(format)) {
	case "jpeg", "jpg":
		return bimg.JPEG
	case "png":
		return bimg.PNG
	case "webp":
		return bimg.WEBP
	case "gif":
		return bimg.GIF
	case "tiff", "tif":
		return bimg.TIFF
	default:
		return bimg.UNKNOWN
	}
}

func nextAvailableOutputPath(outputDir, inputPath, format string, used map[string]struct{}) string {
	baseName := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if strings.TrimSpace(baseName) == "" {
		baseName = "image"
	}

	if strings.TrimSpace(outputDir) == "" {
		outputDir = filepath.Dir(inputPath)
	}

	candidateBase := filepath.Join(outputDir, fmt.Sprintf("%s_cropped", baseName))
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

func isOutputAvailable(outputPath string, used map[string]struct{}) bool {
	cleaned := strings.ToLower(filepath.Clean(outputPath))
	if used != nil {
		if _, exists := used[cleaned]; exists {
			return false
		}
	}

	if _, err := os.Stat(outputPath); err == nil {
		return false
	}

	return true
}

func markUsed(outputPath string, used map[string]struct{}) {
	if used == nil {
		return
	}
	used[strings.ToLower(filepath.Clean(outputPath))] = struct{}{}
}

func sameFile(inputPath, outputPath string) bool {
	if strings.TrimSpace(inputPath) == "" || strings.TrimSpace(outputPath) == "" {
		return false
	}
	return strings.EqualFold(filepath.Clean(inputPath), filepath.Clean(outputPath))
}

func firstPath(paths []string) string {
	if len(paths) == 0 {
		return ""
	}
	return strings.TrimSpace(paths[0])
}

func optionInt(options map[string]any, key string) (int, *models.JobErrorV1) {
	if options == nil {
		return 0, models.NewCanonicalJobError("IMAGE_CROP_OPTION_REQUIRED", fmt.Sprintf("options.%s is required", key), nil)
	}

	value, ok := options[key]
	if !ok || value == nil {
		return 0, models.NewCanonicalJobError("IMAGE_CROP_OPTION_REQUIRED", fmt.Sprintf("options.%s is required", key), nil)
	}

	switch casted := value.(type) {
	case float64:
		if casted != float64(int(casted)) {
			return 0, models.NewCanonicalJobError("IMAGE_CROP_OPTION_INVALID", fmt.Sprintf("options.%s must be an integer", key), nil)
		}
		return int(casted), nil
	case int:
		return casted, nil
	case int32:
		return int(casted), nil
	case int64:
		return int(casted), nil
	default:
		return 0, models.NewCanonicalJobError("IMAGE_CROP_OPTION_INVALID", fmt.Sprintf("options.%s must be numeric", key), nil)
	}
}

func cropOptionString(options map[string]any, key string) string {
	if options == nil {
		return ""
	}
	value, _ := options[key].(string)
	return strings.TrimSpace(value)
}
