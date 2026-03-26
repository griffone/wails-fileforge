package image

import (
	"context"
	"fmt"
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
		Description:      "Legacy image converter adapter exposed as Tool V1",
		Domain:           "image",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff", "tif"},
		OutputExtensions: t.converter.SupportedFormats(),
		RuntimeDeps:      []string{"libvips"},
		Tags:             []string{"legacy", "adapter", "image"},
	}
}

func (t *ImageToolAdapter) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *ImageToolAdapter) Validate(_ context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	if len(req.InputPaths) == 0 {
		return &models.JobErrorV1{Code: "VALIDATION_ERROR", Message: "inputPaths cannot be empty"}
	}

	if req.Mode != "single" && req.Mode != "batch" {
		return &models.JobErrorV1{Code: "VALIDATION_ERROR", Message: "mode must be single or batch"}
	}

	format := t.resolveFormat(req.Options)
	if err := t.converter.validateOutputFormat(format); err != nil {
		return &models.JobErrorV1{Code: "VALIDATION_ERROR", Message: err.Error()}
	}

	if req.Mode == "single" && len(req.InputPaths) != 1 {
		return &models.JobErrorV1{Code: "VALIDATION_ERROR", Message: "single mode requires exactly one input path"}
	}

	if req.Mode == "batch" && strings.TrimSpace(req.OutputDir) == "" {
		return &models.JobErrorV1{Code: "VALIDATION_ERROR", Message: "outputDir is required in batch mode"}
	}

	return nil
}

func (t *ImageToolAdapter) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	inputPath := req.InputPaths[0]
	format := t.resolveFormat(req.Options)
	outputPath := t.resolveSingleOutputPath(inputPath, req.OutputDir, format, req.Options)

	err := t.converter.ConvertSingle(ctx, inputPath, outputPath, format, req.Options)
	if err != nil {
		return models.JobResultItemV1{
			InputPath:  inputPath,
			OutputPath: outputPath,
			Success:    false,
			Message:    "conversion failed",
		}, &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
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
	workers := req.Workers
	if workers <= 0 {
		workers = 4
	}

	total := len(req.InputPaths)
	current := 0
	results, err := t.converter.ConvertBatch(ctx, req.InputPaths, req.OutputDir, format, false, workers, req.Options, func(item models.ConversionResult) {
		current++
		if onProgress != nil {
			onProgress(models.JobProgressV1{
				Current: current,
				Total:   total,
				Stage:   "running",
				Message: item.Message,
			})
		}
	})
	if err != nil {
		return nil, &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	items := make([]models.JobResultItemV1, 0, len(results))
	for _, r := range results {
		var itemErr *models.JobErrorV1
		if !r.Success && r.Error != "" {
			itemErr = &models.JobErrorV1{Code: "ITEM_ERROR", Message: r.Error}
		}

		items = append(items, models.JobResultItemV1{
			InputPath:  r.InputPath,
			OutputPath: r.OutputPath,
			Success:    r.Success,
			Message:    r.Message,
			Error:      itemErr,
		})
	}

	return items, nil
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
