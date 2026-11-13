package pdf

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/pdf/engine"
)

const (
	ToolIDPDFCropV1 = "tool.pdf.crop"

	cropOutputKindFile = "file"
	cropOutputSuffix   = "_cropped"
)

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

func (t *CropTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe <stem>_cropped.pdf destination per input
// without writing anything. An explicit options.outputPath keeps the existing
// contract: it must be a .pdf whose parent exists and is writable, and the
// target must not exist yet. Auto-derived names are suffixed (-2, -3, ...)
// instead of overwriting anything.
func (t *CropTool) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	parsed, parseErr := cropReqFields(req)
	if parseErr != nil {
		return planFailure(plan, parseErr)
	}

	for _, inputPath := range parsed.InputPaths {
		if cropErr := engine.ValidateCropInput(inputPath); cropErr != nil {
			return planFailure(plan, mapCropError(cropErr))
		}
	}

	if cropErr := engine.ValidateCropOptions(parsed.PageSelection, parsed.CropPreset, parsed.Margins); cropErr != nil {
		return planFailure(plan, mapCropError(cropErr))
	}

	if parsed.Mode == "single" {
		planned, planErr := planSingleCropOutput(parsed)
		if planErr != nil {
			if planned.OutputPath != "" {
				plan.Outputs = []models.PlannedOutputV1{planned}
			}
			return planFailure(plan, planErr)
		}

		plan.Success = true
		plan.Message = cropPlanMessage(1)
		plan.Outputs = []models.PlannedOutputV1{planned}
		plan.Warnings = planned.Warnings
		plan.EffectiveOptions = cropEffectiveOptions(parsed)

		return plan, nil
	}

	if parsed.OutputDir != "" {
		if cropErr := engine.ValidateCropDestination(parsed.OutputDir); cropErr != nil {
			return planFailure(plan, mapCropError(cropErr))
		}
	}

	// Reserve every input path so an auto-derived output can never overwrite a
	// different input of the same batch.
	used := make(map[string]struct{}, len(parsed.InputPaths))
	for _, inputPath := range parsed.InputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	outputs := make([]models.PlannedOutputV1, 0, len(parsed.InputPaths))
	warnings := make([]string, 0)
	for _, inputPath := range parsed.InputPaths {
		targetDir := parsed.OutputDir
		if targetDir == "" {
			targetDir = filepath.Dir(inputPath)
		}

		planned := planAutoCropOutput(inputPath, filepath.Clean(targetDir), used)
		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = cropPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = cropEffectiveOptions(parsed)

	return plan, nil
}

func (t *CropTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	parsed, parseErr := cropReqFields(req)
	if parseErr != nil {
		return cropFailureItem(firstInputPath(req.InputPaths), "", parseErr), parseErr
	}

	if parsed.Mode != "single" {
		jobErr := cropRequestError(engine.ErrorCodeValidation, "mode must be single")
		return cropFailureItem(parsed.InputPath, "", jobErr), jobErr
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return cropFailureItem(parsed.InputPath, cropPlannedOutputPath(plan), planErr), planErr
	}

	planned := plan.Outputs[0]
	if err := engine.Crop(ctx, planned.InputPath, planned.OutputPath, parsed.PageSelection, parsed.CropPreset, parsed.Margins); err != nil {
		jobErr := mapCropError(err)
		return cropFailureItem(planned.InputPath, planned.OutputPath, jobErr), jobErr
	}

	return models.JobResultItemV1{
		InputPath:   planned.InputPath,
		OutputPath:  planned.OutputPath,
		Outputs:     []string{planned.OutputPath},
		OutputCount: 1,
		Success:     true,
		Message:     "PDF crop successful",
	}, nil
}

func (t *CropTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, parseErr := cropReqFields(req)
	if parseErr != nil {
		return nil, parseErr
	}

	if parsed.Mode != "batch" {
		return nil, cropRequestError(engine.ErrorCodeValidation, "mode must be batch")
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	fileErrors := make([]map[string]any, 0)
	items := make([]models.JobResultItemV1, 0, len(plan.Outputs))
	for i, planned := range plan.Outputs {
		select {
		case <-ctx.Done():
			cancelErr := mapCropError(&engine.CropError{Code: "CANCELED", Message: "crop canceled", Cause: ctx.Err()})
			return items, cancelErr
		default:
		}

		item := models.JobResultItemV1{
			InputPath:  planned.InputPath,
			OutputPath: planned.OutputPath,
		}

		if err := engine.Crop(ctx, planned.InputPath, planned.OutputPath, parsed.PageSelection, parsed.CropPreset, parsed.Margins); err != nil {
			jobErr := mapCropError(err)
			item.Success = false
			item.Message = jobErr.Message
			item.Error = jobErr
			fileErrors = append(fileErrors, map[string]any{
				"path":    planned.InputPath,
				"code":    jobErr.DetailCode,
				"message": jobErr.Message,
			})
		} else {
			item.Success = true
			item.Message = "PDF crop successful"
			item.Outputs = []string{planned.OutputPath}
			item.OutputCount = 1
		}

		items = append(items, item)

		if onProgress != nil {
			onProgress(models.JobProgressV1{
				Current: i + 1,
				Total:   len(plan.Outputs),
				Stage:   "running",
				Message: fmt.Sprintf("processed %d/%d", i+1, len(plan.Outputs)),
			})
		}
	}

	if len(fileErrors) > 0 {
		return items, models.NewCanonicalJobError(engine.ErrorCodeCropFailed, "one or more files failed in batch crop", map[string]any{
			"fileErrors": fileErrors,
		})
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
		return cropRequestFields{}, cropRequestError(engine.ErrorCodeValidation, "mode must be single or batch")
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return cropRequestFields{}, cropRequestError(engine.ErrorCodeValidation, "exactly 1 input PDF is required")
	}
	if mode == "batch" && len(req.InputPaths) < 1 {
		return cropRequestFields{}, cropRequestError(engine.ErrorCodeValidation, "at least 1 input PDF is required")
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawInput := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return cropRequestFields{}, cropRequestError(engine.ErrorCodeValidation, "inputPath is required")
		}
		inputPaths = append(inputPaths, inputPath)
	}

	inputPath := firstInputPath(inputPaths)

	// The chosen folder is optional: without it every output is written next
	// to its input. req.OutputDir is the current field; options.outputDir stays
	// supported for older clients.
	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = strings.TrimSpace(optionString(req.Options, "outputDir"))
	}
	if outputDir != "" {
		outputDir = filepath.Clean(outputDir)
	}

	outputPath := strings.TrimSpace(optionString(req.Options, "outputPath"))

	cropPreset := strings.TrimSpace(optionString(req.Options, "cropPreset"))
	if cropPreset == "" {
		return cropRequestFields{Mode: mode, InputPath: inputPath, InputPaths: inputPaths, OutputPath: outputPath, OutputDir: outputDir}, cropRequestError(engine.ErrorCodeValidation, "options.cropPreset is required")
	}

	pageSelection := strings.TrimSpace(optionString(req.Options, "pageSelection"))

	margins, marginsErr := optionMargins(req.Options, "margins")
	if marginsErr != nil {
		return cropRequestFields{Mode: mode, InputPath: inputPath, InputPaths: inputPaths, OutputPath: outputPath, OutputDir: outputDir, PageSelection: pageSelection, CropPreset: cropPreset}, marginsErr
	}

	if cropPreset == engine.CropPresetCustom && margins == nil {
		return cropRequestFields{Mode: mode, InputPath: inputPath, InputPaths: inputPaths, OutputPath: outputPath, OutputDir: outputDir, PageSelection: pageSelection, CropPreset: cropPreset}, cropRequestError(engine.ErrorCodeCropMarginsRequired, "options.margins is required when cropPreset=custom")
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

// planSingleCropOutput resolves the one output of a single request. An
// explicit outputPath keeps the old validation contract; otherwise the output
// is auto-named next to the input or inside the chosen folder.
func planSingleCropOutput(parsed cropRequestFields) (models.PlannedOutputV1, *models.JobErrorV1) {
	planned := models.PlannedOutputV1{
		InputPath:   parsed.InputPath,
		OutputKind:  cropOutputKindFile,
		OutputCount: 1,
	}

	if parsed.OutputPath != "" {
		planned.OutputPath = parsed.OutputPath
		if cropErr := engine.ValidateCropRequest(parsed.InputPath, parsed.OutputPath, parsed.PageSelection, parsed.CropPreset, parsed.Margins); cropErr != nil {
			return planned, mapCropError(cropErr)
		}

		return planned, nil
	}

	targetDir := parsed.OutputDir
	if targetDir == "" {
		targetDir = filepath.Dir(parsed.InputPath)
	}
	targetDir = filepath.Clean(targetDir)

	if cropErr := engine.ValidateCropDestination(targetDir); cropErr != nil {
		return planned, mapCropError(cropErr)
	}

	return planAutoCropOutput(parsed.InputPath, targetDir, map[string]struct{}{}), nil
}

func planAutoCropOutput(inputPath, dir string, used map[string]struct{}) models.PlannedOutputV1 {
	planned := models.PlannedOutputV1{
		InputPath:   inputPath,
		OutputKind:  cropOutputKindFile,
		OutputCount: 1,
	}

	base := cropOutputBaseName(inputPath)
	outputPath, adjusted := outputplan.UniqueFilePath(dir, base, "pdf", used)
	planned.OutputPath = outputPath

	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", base+".pdf", filepath.Base(outputPath)))
	}

	return planned
}

func cropOutputBaseName(inputPath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		stem = "document"
	}

	return stem + cropOutputSuffix
}

func cropEffectiveOptions(parsed cropRequestFields) map[string]any {
	effective := map[string]any{"cropPreset": parsed.CropPreset}
	if parsed.PageSelection != "" {
		effective["pageSelection"] = parsed.PageSelection
	}
	if parsed.CropPreset == engine.CropPresetCustom && parsed.Margins != nil {
		effective["margins"] = map[string]any{
			"top":    parsed.Margins.Top,
			"right":  parsed.Margins.Right,
			"bottom": parsed.Margins.Bottom,
			"left":   parsed.Margins.Left,
		}
	}

	return effective
}

func cropPlanMessage(count int) string {
	if count == 1 {
		return "Ready to crop 1 PDF."
	}

	return fmt.Sprintf("Ready to crop %d PDFs.", count)
}

func cropFailureItem(inputPath, outputPath string, jobErr *models.JobErrorV1) models.JobResultItemV1 {
	return models.JobResultItemV1{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Success:    false,
		Message:    jobErr.Message,
		Error:      jobErr,
	}
}

func cropPlannedOutputPath(plan models.PlanJobResponseV1) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
}

func cropRequestError(code, message string) *models.JobErrorV1 {
	return &models.JobErrorV1{Code: code, DetailCode: code, Message: message}
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
		return nil, cropRequestError(engine.ErrorCodeCropMarginsInvalid, "options.margins must be an object")
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
		return 0, cropRequestError(engine.ErrorCodeCropMarginsInvalid, fmt.Sprintf("options.margins.%s is required", key))
	}

	n, ok := v.(float64)
	if !ok {
		return 0, cropRequestError(engine.ErrorCodeCropMarginsInvalid, fmt.Sprintf("options.margins.%s must be a number", key))
	}

	return n, nil
}

func mapCropError(err error) *models.JobErrorV1 {
	var cropErr *engine.CropError
	if !errors.As(err, &cropErr) {
		return &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	return &models.JobErrorV1{Code: cropErr.Code, DetailCode: cropErr.Code, Message: cropErr.Message, Details: cropErr.Details}
}
