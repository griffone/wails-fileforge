package image

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
)

const (
	ToolIDImageConvertV1 = "tool.image.convert"
)

type ImageToolAdapter struct {
	converter *ImageConverter
}

func NewImageToolAdapter(converter *ImageConverter) *ImageToolAdapter {
	if converter == nil {
		converter = NewImageConverter()
	}

	return &ImageToolAdapter{converter: converter}
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
		InputExtensions:  []string{"jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff", "tif"},
		OutputExtensions: t.converter.SupportedFormats(),
		RuntimeDeps:      []string{"libvips"},
		Tags:             []string{"image", "convert"},
	}
}

func (t *ImageToolAdapter) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *ImageToolAdapter) Validate(_ context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	if len(req.InputPaths) == 0 {
		return models.NewCanonicalJobError("IMAGE_INPUT_REQUIRED", "inputPaths cannot be empty", nil)
	}

	if req.Mode != "single" && req.Mode != "batch" {
		return models.NewCanonicalJobError("IMAGE_MODE_INVALID", "mode must be single or batch", nil)
	}

	format := t.resolveFormat(req.Options)
	if err := t.converter.validateOutputFormat(format); err != nil {
		return models.NewCanonicalJobError("IMAGE_FORMAT_INVALID", err.Error(), nil)
	}

	if req.Mode == "single" && len(req.InputPaths) != 1 {
		return models.NewCanonicalJobError("IMAGE_SINGLE_INPUT_COUNT", "single mode requires exactly one input path", nil)
	}

	if req.Mode == "batch" && strings.TrimSpace(req.OutputDir) == "" {
		return models.NewCanonicalJobError("IMAGE_BATCH_OUTPUT_DIR_REQUIRED", "outputDir is required in batch mode", nil)
	}

	return nil
}

func (t *ImageToolAdapter) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	inputPath := req.InputPaths[0]
	format := t.resolveFormat(req.Options)
	outputPath := t.resolveSingleOutputPath(inputPath, req.OutputDir, format, req.Options)

	err := t.converter.ConvertSingle(ctx, inputPath, outputPath, format, req.Options)
	if err != nil {
		jobErr := models.NewCanonicalJobError("IMAGE_SINGLE_EXECUTION", err.Error(), nil)
		return models.JobResultItemV1{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Success:    false,
			Message:    "conversion failed",
			Error:      jobErr,
		}, jobErr
	}

	return models.JobResultItemV1{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Success:    true,
		Message:    "conversion successful",
	}, nil
}

func (t *ImageToolAdapter) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	format := t.resolveFormat(req.Options)
	items := make([]models.JobResultItemV1, 0, len(req.InputPaths))
	total := len(req.InputPaths)
	usedOutputs := make(map[string]struct{}, len(req.InputPaths))

	for index, inputPath := range req.InputPaths {
		select {
		case <-ctx.Done():
			return items, models.NewCanonicalJobError("IMAGE_BATCH_CANCELLED", ctx.Err().Error(), nil)
		default:
		}

		outputPath := t.resolveBatchOutputPath(inputPath, req.OutputDir, format, usedOutputs)
		err := t.converter.ConvertSingle(ctx, inputPath, outputPath, format, req.Options)
		if err != nil {
			itemErr := models.NewCanonicalJobError("IMAGE_BATCH_ITEM", err.Error(), map[string]any{"inputPath": inputPath})
			items = append(items, models.JobResultItemV1{
				InputPath:  inputPath,
				OutputPath: outputPath,
				Success:    false,
				Message:    "conversion failed",
				Error:      itemErr,
			})
		} else {
			items = append(items, models.JobResultItemV1{
				InputPath:  inputPath,
				OutputPath: outputPath,
				Success:    true,
				Message:    "conversion successful",
			})
		}

		if onProgress != nil {
			onProgress(models.JobProgressV1{
				Current: index + 1,
				Total:   total,
				Stage:   models.JobStatusRunning,
				Message: fmt.Sprintf("processed %d/%d", index+1, total),
			})
		}
	}

	var firstErr *models.JobErrorV1
	for _, item := range items {
		if !item.Success && item.Error != nil {
			firstErr = item.Error
			break
		}
	}

	return items, firstErr
}

func (t *ImageToolAdapter) resolveFormat(options map[string]any) string {
	if options == nil {
		return "webp"
	}

	format, ok := options["format"].(string)
	if !ok || strings.TrimSpace(format) == "" {
		return "webp"
	}

	return strings.ToLower(strings.TrimSpace(format))
}

func (t *ImageToolAdapter) resolveSingleOutputPath(inputPath, outputDir, format string, options map[string]any) string {
	if options != nil {
		if outputPath, ok := options["outputPath"].(string); ok && strings.TrimSpace(outputPath) != "" {
			return outputPath
		}
	}

	if strings.TrimSpace(outputDir) == "" {
		outputDir = filepath.Dir(inputPath)
	}

	name := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	return filepath.Join(outputDir, fmt.Sprintf("%s.%s", name, format))
}

func (t *ImageToolAdapter) resolveBatchOutputPath(inputPath, outputDir, format string, used map[string]struct{}) string {
	name := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if strings.TrimSpace(name) == "" {
		name = "image"
	}

	candidate := filepath.Join(outputDir, fmt.Sprintf("%s.%s", name, format))
	if _, exists := used[candidate]; !exists {
		if _, err := os.Stat(candidate); os.IsNotExist(err) {
			used[candidate] = struct{}{}
			return candidate
		}
	}

	for suffix := 2; ; suffix++ {
		next := filepath.Join(outputDir, fmt.Sprintf("%s-%d.%s", name, suffix, format))
		if _, exists := used[next]; exists {
			continue
		}
		if _, err := os.Stat(next); os.IsNotExist(err) {
			used[next] = struct{}{}
			return next
		}
	}
}
