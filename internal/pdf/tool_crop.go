package pdf

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
)

const ToolIDPDFCropV1 = "tool.pdf.crop"

type CropTool struct{}

func NewCropTool() *CropTool {
	return &CropTool{}
}

func (t *CropTool) ID() string {
	return ToolIDPDFCropV1
}

func (t *CropTool) Capability() string {
	return ToolIDPDFCropV1
}

func (t *CropTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "PDF Crop",
		Description:      "Crop PDF pages using presets or custom margins",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{"pdfcpu"},
		Tags:             []string{"pdf", "crop", "margins"},
	}
}

func (t *CropTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *CropTool) Validate(_ context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	parsed, reqErr := cropReqFields(req)
	if reqErr != nil {
		return reqErr
	}

	if parsed.Mode == "batch" {
		if cropErr := engine.ValidateCropBatchRequest(parsed.InputPaths, parsed.OutputDir, parsed.PageSelection, parsed.CropPreset, parsed.Margins); cropErr != nil {
			return mapCropError(cropErr)
		}

		return nil
	}

	if cropErr := engine.ValidateCropRequest(parsed.InputPath, parsed.OutputPath, parsed.PageSelection, parsed.CropPreset, parsed.Margins); cropErr != nil {
		return mapCropError(cropErr)
	}

	return nil
}

func (t *CropTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	parsed, reqErr := cropReqFields(req)
	if reqErr != nil {
		return models.JobResultItemV1{InputPath: parsed.InputPath, OutputPath: parsed.OutputPath, Success: false, Message: reqErr.Message, Error: reqErr}, reqErr
	}

	if cropErr := engine.ValidateCropRequest(parsed.InputPath, parsed.OutputPath, parsed.PageSelection, parsed.CropPreset, parsed.Margins); cropErr != nil {
		jobErr := mapCropError(cropErr)
		return models.JobResultItemV1{InputPath: parsed.InputPath, OutputPath: parsed.OutputPath, Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	if err := engine.Crop(ctx, parsed.InputPath, parsed.OutputPath, parsed.PageSelection, parsed.CropPreset, parsed.Margins); err != nil {
		jobErr := mapCropError(err)
		return models.JobResultItemV1{InputPath: parsed.InputPath, OutputPath: parsed.OutputPath, Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	return models.JobResultItemV1{
		InputPath:   parsed.InputPath,
		OutputPath:  parsed.OutputPath,
		Outputs:     []string{parsed.OutputPath},
		OutputCount: 1,
		Success:     true,
		Message:     "PDF crop successful",
	}, nil
}

func (t *CropTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, reqErr := cropReqFields(req)
	if reqErr != nil {
		return nil, reqErr
	}

	if parsed.Mode != "batch" {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "mode must be batch"}
	}

	if cropErr := engine.ValidateCropBatchRequest(parsed.InputPaths, parsed.OutputDir, parsed.PageSelection, parsed.CropPreset, parsed.Margins); cropErr != nil {
		return nil, mapCropError(cropErr)
	}

	results, err := engine.CropBatch(ctx, parsed.InputPaths, parsed.OutputDir, parsed.PageSelection, parsed.CropPreset, parsed.Margins)
	if err != nil {
		return nil, mapCropError(err)
	}

	items := make([]models.JobResultItemV1, 0, len(results))
	for i, result := range results {
		items = append(items, models.JobResultItemV1{
			InputPath:   result.InputPath,
			OutputPath:  result.OutputPath,
			Outputs:     []string{result.OutputPath},
			OutputCount: 1,
			Success:     true,
			Message:     "PDF crop successful",
		})

		if onProgress != nil {
			onProgress(models.JobProgressV1{
				Current: i + 1,
				Total:   len(results),
				Stage:   "running",
				Message: fmt.Sprintf("processed %d/%d", i+1, len(results)),
			})
		}
	}

	return items, nil
}

type cropRequestFields struct {
	Mode          string
	InputPath     string
	InputPaths    []string
	OutputPath    string
	OutputDir     string
	PageSelection string
	CropPreset    string
	Margins       *engine.CropMargins
}

func cropReqFields(req models.JobRequestV1) (cropRequestFields, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return cropRequestFields{}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "mode must be single or batch"}
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return cropRequestFields{}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "exactly 1 input PDF is required"}
	}
	if mode == "batch" && len(req.InputPaths) < 1 {
		return cropRequestFields{}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "at least 1 input PDF is required"}
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawInput := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return cropRequestFields{}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "inputPath is required"}
		}
		inputPaths = append(inputPaths, inputPath)
	}

	inputPath := firstInputPath(inputPaths)

	outputDir := strings.TrimSpace(optionString(req.Options, "outputDir"))
	if mode == "batch" {
		if outputDir == "" {
			return cropRequestFields{Mode: mode, InputPath: inputPath, InputPaths: inputPaths}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "options.outputDir is required"}
		}
		outputDir = filepath.Clean(outputDir)
	}

	outputPath := strings.TrimSpace(optionString(req.Options, "outputPath"))
	if mode == "single" {
		if outputPath == "" {
			return cropRequestFields{Mode: mode, InputPath: inputPath, InputPaths: inputPaths}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "options.outputPath is required"}
		}
	}

	cropPreset := strings.TrimSpace(optionString(req.Options, "cropPreset"))
	if cropPreset == "" {
		return cropRequestFields{Mode: mode, InputPath: inputPath, InputPaths: inputPaths, OutputPath: outputPath, OutputDir: outputDir}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "options.cropPreset is required"}
	}

	pageSelection := strings.TrimSpace(optionString(req.Options, "pageSelection"))

	margins, marginsErr := optionMargins(req.Options, "margins")
	if marginsErr != nil {
		return cropRequestFields{Mode: mode, InputPath: inputPath, InputPaths: inputPaths, OutputPath: outputPath, OutputDir: outputDir, PageSelection: pageSelection, CropPreset: cropPreset}, marginsErr
	}

	if cropPreset == engine.CropPresetCustom && margins == nil {
		return cropRequestFields{Mode: mode, InputPath: inputPath, InputPaths: inputPaths, OutputPath: outputPath, OutputDir: outputDir, PageSelection: pageSelection, CropPreset: cropPreset}, &models.JobErrorV1{Code: engine.ErrorCodeCropMarginsRequired, Message: "options.margins is required when cropPreset=custom"}
	}

	return cropRequestFields{
		Mode:          mode,
		InputPath:     inputPath,
		InputPaths:    inputPaths,
		OutputPath:    outputPath,
		OutputDir:     outputDir,
		PageSelection: pageSelection,
		CropPreset:    cropPreset,
		Margins:       margins,
	}, nil
}

func optionMargins(options map[string]any, key string) (*engine.CropMargins, *models.JobErrorV1) {
	if options == nil {
		return nil, nil
	}

	raw, ok := options[key]
	if !ok || raw == nil {
		return nil, nil
	}

	m, ok := raw.(map[string]any)
	if !ok {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeCropMarginsInvalid, Message: "options.margins must be an object"}
	}

	top, err := optionNumberField(m, "top")
	if err != nil {
		return nil, err
	}
	right, err := optionNumberField(m, "right")
	if err != nil {
		return nil, err
	}
	bottom, err := optionNumberField(m, "bottom")
	if err != nil {
		return nil, err
	}
	left, err := optionNumberField(m, "left")
	if err != nil {
		return nil, err
	}

	return &engine.CropMargins{Top: top, Right: right, Bottom: bottom, Left: left}, nil
}

func optionNumberField(m map[string]any, key string) (float64, *models.JobErrorV1) {
	v, ok := m[key]
	if !ok {
		return 0, &models.JobErrorV1{Code: engine.ErrorCodeCropMarginsInvalid, Message: fmt.Sprintf("options.margins.%s is required", key)}
	}

	n, ok := v.(float64)
	if !ok {
		return 0, &models.JobErrorV1{Code: engine.ErrorCodeCropMarginsInvalid, Message: fmt.Sprintf("options.margins.%s must be a number", key)}
	}

	return n, nil
}

func mapCropError(err error) *models.JobErrorV1 {
	var cropErr *engine.CropError
	if !errors.As(err, &cropErr) {
		return &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	return &models.JobErrorV1{Code: cropErr.Code, Message: cropErr.Message}
}
