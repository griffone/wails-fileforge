package pdf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/pdf/engine"
)

const (
	// ToolIDPDFOCRV1 is the local OCR tool for scanned PDFs.
	ToolIDPDFOCRV1 = "tool.pdf.ocr"

	ocrOutputKindFile = "file"
	ocrOutputSuffix   = "_ocr"
	ocrTextExtension  = "txt"
)

// OCRTool turns scanned PDFs into searchable PDFs with the local pdftoppm +
// tesseract pipeline. The runner and the capability detector are injectable so
// tests can simulate missing dependencies.
type OCRTool struct {
	runner       engine.CommandRunner
	capabilities func(context.Context) engine.OCRCapabilities
}

func NewOCRTool() *OCRTool {
	return &OCRTool{}
}

func NewOCRToolWithDeps(runner engine.CommandRunner, capabilities func(context.Context) engine.OCRCapabilities) *OCRTool {
	return &OCRTool{runner: runner, capabilities: capabilities}
}

func (t *OCRTool) ID() string {
	return ToolIDPDFOCRV1
}

func (t *OCRTool) Capability() string {
	return ToolIDPDFOCRV1
}

func (t *OCRTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "PDF OCR",
		Description:      "Make scanned PDFs searchable with local OCR",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"pdf", "txt"},
		RuntimeDeps:      []string{"pdftoppm", "tesseract"},
		Tags:             []string{"pdf", "ocr", "scan", "text", "tesseract"},
	}
}

// OCRCapabilities exposes the detected runtime for the capability endpoint and
// the Clean scan recipe, which need the same single source of truth.
func (t *OCRTool) OCRCapabilities(ctx context.Context) engine.OCRCapabilities {
	return t.detectCapabilities(ctx)
}

func (t *OCRTool) RuntimeState(ctx context.Context) models.ToolRuntimeStateV1 {
	capabilities := t.detectCapabilities(ctx)
	if capabilities.Available() {
		return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
	}

	return models.ToolRuntimeStateV1{
		Status:  "degraded",
		Healthy: false,
		Reason:  OCRUnavailableReason(capabilities),
	}
}

func (t *OCRTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan validates the OCR runtime, language, inputs and resolves the PDF plus
// the optional text sidecar for every input without writing anything.
func (t *OCRTool) Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	parsed, parseErr := ocrReqFields(req)
	if parseErr != nil {
		return planFailure(plan, parseErr)
	}

	capabilities := t.detectCapabilities(ctx)
	if !capabilities.Available() {
		jobErr := models.NewJobError(
			models.ErrorCodeRuntimeDepMissing,
			engine.OCRCodeRuntimeUnavailable,
			OCRUnavailableReason(capabilities),
			map[string]any{
				"pdftoppm":  capabilities.PDFToPPM,
				"tesseract": capabilities.Tesseract,
				"languages": capabilities.Languages,
			},
		)
		return planFailure(plan, jobErr)
	}

	language := parsed.Language
	if language == "" {
		language = capabilities.RecommendedLanguage
	}
	if !capabilities.HasLanguage(language) {
		jobErr := models.NewJobError(
			models.ErrorCodeValidationInvalidInput,
			engine.OCRCodeLanguageUnavailable,
			fmt.Sprintf(
				"Tesseract language '%s' is not installed. Installed languages: %s.",
				language,
				strings.Join(capabilities.Languages, ", "),
			),
			map[string]any{"language": language, "languages": capabilities.Languages},
		)
		return planFailure(plan, jobErr)
	}

	for _, inputPath := range parsed.InputPaths {
		if validationErr := engine.ValidateOCRInput(inputPath); validationErr != nil {
			return planFailure(plan, mapOCRError(validationErr))
		}
	}

	if parsed.OutputPath != "" {
		planned, planErr := planExplicitOCROutputs(parsed)
		if planErr != nil {
			return planFailure(plan, planErr)
		}

		plan.Success = true
		plan.Message = ocrPlanMessage(1)
		plan.Outputs = planned
		plan.Warnings = ocrPlanWarnings(planned, language, capabilities)
		plan.EffectiveOptions = ocrEffectiveOptions(parsed, language)

		return plan, nil
	}

	if parsed.OutputDir != "" {
		if dirErr := engine.ValidateOCRDestination(parsed.OutputDir); dirErr != nil {
			return planFailure(plan, mapOCRError(dirErr))
		}
	}

	// Reserve every input path so an auto-derived output can never overwrite a
	// different input of the same batch.
	used := make(map[string]struct{}, len(parsed.InputPaths))
	for _, inputPath := range parsed.InputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	outputs := make([]models.PlannedOutputV1, 0, len(parsed.InputPaths)*2)
	for _, inputPath := range parsed.InputPaths {
		targetDir := parsed.OutputDir
		if targetDir == "" {
			targetDir = filepath.Dir(inputPath)
		}

		planned := planAutoOCROutputs(inputPath, filepath.Clean(targetDir), parsed.TextSidecar, used)
		outputs = append(outputs, planned...)
	}

	plan.Success = true
	plan.Message = ocrPlanMessage(len(parsed.InputPaths))
	plan.Outputs = outputs
	plan.Warnings = ocrPlanWarnings(outputs, language, capabilities)
	plan.EffectiveOptions = ocrEffectiveOptions(parsed, language)

	return plan, nil
}

// ExecuteSingle implements tools.SingleExecutor by delegating to the progress
// variant with a no-op callback.
func (t *OCRTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

// ExecuteSingleWithProgress runs the OCR pipeline for one PDF and reports page
// level progress.
func (t *OCRTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
	parsed, parseErr := ocrReqFields(req)
	if parseErr != nil {
		return ocrFailureItem(firstInputPath(req.InputPaths), "", parseErr), parseErr
	}

	if parsed.Mode != "single" {
		jobErr := ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "mode must be single")
		return ocrFailureItem(parsed.InputPaths[0], "", jobErr), jobErr
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return ocrFailureItem(parsed.InputPaths[0], ocrPlannedOutputPath(plan), planErr), planErr
	}

	planned, jobErr := groupOCROutputs(plan.Outputs, parsed.TextSidecar)
	if jobErr != nil {
		return ocrFailureItem(parsed.InputPaths[0], ocrPlannedOutputPath(plan), jobErr), jobErr
	}

	outcome, ocrErr := t.runOCR(ctx, planned[0], stringOption(plan.EffectiveOptions, "language"), intOption(plan.EffectiveOptions, "dpi"), onProgress)
	if ocrErr != nil {
		jobErr = mapOCRError(ocrErr)
		return ocrFailureItem(planned[0].InputPath, planned[0].OutputPath, jobErr), jobErr
	}

	return ocrSuccessItem(planned[0], outcome), nil
}

// ExecuteBatch runs OCR for every input with per-input progress and per-item
// errors, mirroring the other PDF batch tools.
func (t *OCRTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, parseErr := ocrReqFields(req)
	if parseErr != nil {
		return nil, parseErr
	}

	if parsed.Mode != "batch" {
		return nil, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "mode must be batch")
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	planned, groupErr := groupOCROutputs(plan.Outputs, parsed.TextSidecar)
	if groupErr != nil {
		return nil, groupErr
	}

	language := stringOption(plan.EffectiveOptions, "language")
	dpi := intOption(plan.EffectiveOptions, "dpi")

	fileErrors := make([]map[string]any, 0)
	items := make([]models.JobResultItemV1, 0, len(planned))
	for i, job := range planned {
		select {
		case <-ctx.Done():
			return items, &models.JobErrorV1{Code: "CANCELED", Message: ctx.Err().Error()}
		default:
		}

		outcome, ocrErr := t.runOCR(ctx, job, language, dpi, nil)
		if ocrErr != nil {
			jobErr := mapOCRError(ocrErr)
			item := models.JobResultItemV1{
				InputPath:  job.InputPath,
				OutputPath: job.OutputPath,
				Success:    false,
				Message:    jobErr.Message,
				Error:      jobErr,
			}
			items = append(items, item)
			fileErrors = append(fileErrors, map[string]any{
				"path":    job.InputPath,
				"code":    jobErr.DetailCode,
				"message": jobErr.Message,
			})
		} else {
			items = append(items, ocrSuccessItem(job, outcome))
		}

		if onProgress != nil {
			onProgress(models.JobProgressV1{
				Current: i + 1,
				Total:   len(planned),
				Stage:   "running",
				Message: fmt.Sprintf("processed %d/%d", i+1, len(planned)),
			})
		}
	}

	if len(fileErrors) > 0 {
		return items, models.NewCanonicalJobError(engine.OCRCodeRecognizeFailed, "one or more files failed in batch OCR", map[string]any{
			"fileErrors": fileErrors,
		})
	}

	return items, nil
}

// ocrPlannedJob is one resolved input plus its final PDF and sidecar paths.
type ocrPlannedJob struct {
	InputPath   string
	OutputPath  string
	TextPath    string
	OutputCount int
}

func (t *OCRTool) runOCR(ctx context.Context, job ocrPlannedJob, language string, dpi int, onProgress func(models.JobProgressV1)) (engine.OCRResult, *engine.OCRError) {
	progress := func(current, total int, message string) {
		if onProgress == nil {
			return
		}
		onProgress(models.JobProgressV1{
			Current: current,
			Total:   total,
			Stage:   "ocr",
			Message: message,
		})
	}

	return engine.Convert(ctx, t.runner, engine.OCRRequest{
		InputPath:      job.InputPath,
		OutputPDFPath:  job.OutputPath,
		OutputTextPath: job.TextPath,
		Language:       language,
		DPI:            dpi,
	}, progress)
}

func ocrSuccessItem(job ocrPlannedJob, outcome engine.OCRResult) models.JobResultItemV1 {
	outputs := []string{job.OutputPath}
	if job.TextPath != "" {
		outputs = append(outputs, job.TextPath)
	}

	message := fmt.Sprintf("OCR finished: %d pages, %d characters recognized.", outcome.PageCount, outcome.TextCharacters)
	if len(outcome.Warnings) > 0 {
		message += " " + strings.Join(outcome.Warnings, " ")
	}

	return models.JobResultItemV1{
		InputPath:   job.InputPath,
		OutputPath:  job.OutputPath,
		Outputs:     outputs,
		OutputCount: len(outputs),
		Success:     true,
		Message:     message,
	}
}

// groupOCROutputs pairs the flat planned outputs (PDF then TXT per input) with
// their source paths.
func groupOCROutputs(outputs []models.PlannedOutputV1, textSidecar bool) ([]ocrPlannedJob, *models.JobErrorV1) {
	if len(outputs) == 0 {
		return nil, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "the plan resolved no outputs")
	}

	stride := 1
	if textSidecar {
		stride = 2
	}
	if len(outputs)%stride != 0 {
		return nil, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "the plan resolved an unexpected number of outputs")
	}

	jobs := make([]ocrPlannedJob, 0, len(outputs)/stride)
	for index := 0; index < len(outputs); index += stride {
		job := ocrPlannedJob{
			InputPath:   outputs[index].InputPath,
			OutputPath:  outputs[index].OutputPath,
			OutputCount: 1,
		}
		if stride == 2 {
			job.TextPath = outputs[index+1].OutputPath
			job.OutputCount = 2
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

type ocrRequestFields struct {
	Mode        string
	InputPaths  []string
	OutputPath  string
	OutputDir   string
	Language    string
	TextSidecar bool
	DPI         int
}

func ocrReqFields(req models.JobRequestV1) (ocrRequestFields, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return ocrRequestFields{}, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "mode must be single or batch")
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return ocrRequestFields{}, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "exactly 1 input PDF is required")
	}
	if mode == "batch" && len(req.InputPaths) < 1 {
		return ocrRequestFields{}, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "at least 1 input PDF is required")
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawInput := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return ocrRequestFields{}, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "inputPath is required")
		}
		inputPaths = append(inputPaths, inputPath)
	}

	// The chosen folder is optional: without it every output is written next
	// to its input. req.OutputDir is the current field; options.outputDir stays
	// supported for older clients.
	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = optionString(req.Options, "outputDir")
	}
	if outputDir != "" {
		outputDir = filepath.Clean(outputDir)
	}

	outputPath := strings.TrimSpace(optionString(req.Options, "outputPath"))
	if outputPath != "" && mode == "batch" {
		return ocrRequestFields{}, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "options.outputPath is only supported for single mode")
	}

	language := strings.TrimSpace(optionString(req.Options, "language"))

	textSidecar, sidecarErr := ocrOptionBool(req.Options, "textSidecar")
	if sidecarErr != nil {
		return ocrRequestFields{}, sidecarErr
	}

	dpi, dpiErr := ocrOptionDPI(req.Options)
	if dpiErr != nil {
		return ocrRequestFields{}, dpiErr
	}

	return ocrRequestFields{
		Mode:        mode,
		InputPaths:  inputPaths,
		OutputPath:  outputPath,
		OutputDir:   outputDir,
		Language:    language,
		TextSidecar: textSidecar,
		DPI:         dpi,
	}, nil
}

// ocrOptionBool defaults textSidecar to true when the option is absent.
func ocrOptionBool(options map[string]any, key string) (bool, *models.JobErrorV1) {
	if options == nil {
		return true, nil
	}

	raw, ok := options[key]
	if !ok || raw == nil {
		return true, nil
	}

	switch value := raw.(type) {
	case bool:
		return value, nil
	case string:
		if strings.EqualFold(strings.TrimSpace(value), "true") {
			return true, nil
		}
		if strings.EqualFold(strings.TrimSpace(value), "false") {
			return false, nil
		}
	}

	return false, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "options.textSidecar must be a boolean")
}

// ocrOptionDPI defaults dpi to engine.DefaultOCRDPI and accepts the
// engine.MinOCRDPI–engine.MaxOCRDPI range.
func ocrOptionDPI(options map[string]any) (int, *models.JobErrorV1) {
	dpi := engine.DefaultOCRDPI
	if options != nil {
		if raw, ok := options["dpi"]; ok && raw != nil {
			value, parsed := ocrNumericOption(raw)
			if !parsed || value != float64(int(value)) {
				return 0, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "options.dpi must be a whole number")
			}
			dpi = int(value)
		}
	}

	if dpi < engine.MinOCRDPI || dpi > engine.MaxOCRDPI {
		return 0, ocrRequestError(
			models.ErrorCodeValidationInvalidInput,
			engine.ErrorCodeValidation,
			fmt.Sprintf("options.dpi must be between %d and %d", engine.MinOCRDPI, engine.MaxOCRDPI),
		)
	}

	return dpi, nil
}

// ocrNumericOption reads a JSON or programmatic numeric option.
func ocrNumericOption(raw any) (float64, bool) {
	switch value := raw.(type) {
	case int:
		return float64(value), true
	case int8:
		return float64(value), true
	case int16:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint8:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	case float32:
		return float64(value), true
	case float64:
		return value, true
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func planExplicitOCROutputs(parsed ocrRequestFields) ([]models.PlannedOutputV1, *models.JobErrorV1) {
	outputPath := strings.TrimSpace(parsed.OutputPath)
	if !strings.EqualFold(filepath.Ext(outputPath), ".pdf") {
		return nil, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "options.outputPath must use .pdf extension")
	}

	if validationErr := engine.ValidateOCRRequest(parsed.InputPaths[0], outputPath, ""); validationErr != nil {
		return nil, mapOCRError(validationErr)
	}

	used := make(map[string]struct{}, 2)
	outputplan.MarkUsed(used, parsed.InputPaths[0])
	outputplan.MarkUsed(used, outputPath)

	outputs := []models.PlannedOutputV1{{
		InputPath:   parsed.InputPaths[0],
		OutputPath:  outputPath,
		OutputKind:  ocrOutputKindFile,
		OutputCount: 1,
	}}

	if parsed.TextSidecar {
		textPath, adjusted := outputplan.UniqueFilePath(filepath.Dir(outputPath), strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath)), ocrTextExtension, used)

		textOutput := models.PlannedOutputV1{
			InputPath:   parsed.InputPaths[0],
			OutputPath:  textPath,
			OutputKind:  ocrOutputKindFile,
			OutputCount: 1,
		}
		if adjusted {
			textOutput.Adjusted = true
			textOutput.Warnings = append(textOutput.Warnings, fmt.Sprintf("The text sidecar is saved as %s.", filepath.Base(textPath)))
		}

		outputs = append(outputs, textOutput)
	}

	return outputs, nil
}

func planAutoOCROutputs(inputPath, dir string, textSidecar bool, used map[string]struct{}) []models.PlannedOutputV1 {
	base := ocrOutputBaseName(inputPath)
	outputPath, adjusted := outputplan.UniqueFilePath(dir, base, "pdf", used)

	pdfOutput := models.PlannedOutputV1{
		InputPath:   inputPath,
		OutputPath:  outputPath,
		OutputKind:  ocrOutputKindFile,
		OutputCount: 1,
	}
	if adjusted {
		pdfOutput.Adjusted = true
		pdfOutput.Warnings = append(pdfOutput.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", base+".pdf", filepath.Base(outputPath)))
	}

	outputs := []models.PlannedOutputV1{pdfOutput}
	if !textSidecar {
		return outputs
	}

	textBase := strings.TrimSuffix(filepath.Base(outputPath), filepath.Ext(outputPath))
	textPath, textAdjusted := outputplan.UniqueFilePath(dir, textBase, ocrTextExtension, used)

	textOutput := models.PlannedOutputV1{
		InputPath:   inputPath,
		OutputPath:  textPath,
		OutputKind:  ocrOutputKindFile,
		OutputCount: 1,
	}
	if textAdjusted {
		textOutput.Adjusted = true
		textOutput.Warnings = append(textOutput.Warnings, fmt.Sprintf("The text sidecar is saved as %s.", filepath.Base(textPath)))
	}

	outputs = append(outputs, textOutput)

	return outputs
}

func ocrOutputBaseName(inputPath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		stem = "document"
	}

	return stem + ocrOutputSuffix
}

func ocrEffectiveOptions(parsed ocrRequestFields, language string) map[string]any {
	return map[string]any{
		"language":    language,
		"textSidecar": parsed.TextSidecar,
		"dpi":         parsed.DPI,
	}
}

func ocrPlanMessage(inputCount int) string {
	if inputCount == 1 {
		return "Ready to OCR 1 PDF."
	}

	return fmt.Sprintf("Ready to OCR %d PDFs.", inputCount)
}

// ocrPlanWarnings repeats the explicit tradeoff of the pipeline and explains
// the installed-language state.
func ocrPlanWarnings(outputs []models.PlannedOutputV1, language string, capabilities engine.OCRCapabilities) []string {
	warnings := make([]string, 0, len(outputs)+2)
	for _, output := range outputs {
		warnings = append(warnings, output.Warnings...)
	}

	warnings = append(warnings, "Pages are rasterized; vector text is replaced by an image with an invisible text layer.")
	if language != "" && language != "eng" && language != "spa" {
		warnings = append(warnings, fmt.Sprintf(
			"Installed OCR language is %s; install eng/spa language data for better accuracy.",
			language,
		))
	}
	if !capabilities.PDFToText {
		warnings = append(warnings, engine.OCRWarningPDFToTextUnavailable)
	}

	return warnings
}

// OCRUnavailableReason explains why the local OCR pipeline cannot run, in the
// order the user has to fix it: missing binaries, then missing languages.
func OCRUnavailableReason(capabilities engine.OCRCapabilities) string {
	missing := make([]string, 0, 2)
	if !capabilities.PDFToPPM {
		missing = append(missing, "pdftoppm")
	}
	if !capabilities.Tesseract {
		missing = append(missing, "tesseract")
	}

	if len(missing) > 0 {
		return fmt.Sprintf("Local OCR is unavailable: install %s.", strings.Join(missing, " and "))
	}
	if len(capabilities.Languages) == 0 {
		return "Local OCR is unavailable: no Tesseract language data is installed (install eng or spa)."
	}

	return "Local OCR is unavailable."
}

func ocrFailureItem(inputPath, outputPath string, jobErr *models.JobErrorV1) models.JobResultItemV1 {
	return models.JobResultItemV1{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Success:    false,
		Message:    jobErr.Message,
		Error:      jobErr,
	}
}

func ocrPlannedOutputPath(plan models.PlanJobResponseV1) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
}

func ocrRequestError(code, detailCode, message string) *models.JobErrorV1 {
	return models.NewJobError(code, detailCode, message, nil)
}

func mapOCRError(err error) *models.JobErrorV1 {
	var ocrErr *engine.OCRError
	if !errors.As(err, &ocrErr) {
		return &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	switch ocrErr.Code {
	case engine.OCRCodeRuntimeUnavailable:
		return models.NewJobError(models.ErrorCodeRuntimeDepMissing, ocrErr.Code, ocrErr.Message, nil)
	case engine.OCRCodeLanguageUnavailable, engine.ErrorCodeValidation, engine.ErrorCodeInvalidInputPDF:
		return models.NewJobError(models.ErrorCodeValidationInvalidInput, ocrErr.Code, ocrErr.Message, nil)
	default:
		return models.NewCanonicalJobError(ocrErr.Code, ocrErr.Message, nil)
	}
}

// stringOption reads a string option from the plan's effective options.
func stringOption(options map[string]any, key string) string {
	value, ok := options[key].(string)
	if !ok {
		return ""
	}

	return value
}

// intOption reads an int option from the plan's effective options.
func intOption(options map[string]any, key string) int {
	switch value := options[key].(type) {
	case int:
		return value
	case float64:
		return int(value)
	default:
		return 0
	}
}

// OCRCapabilitiesV1 reports the local OCR state for the app capability
// endpoint.
func OCRCapabilitiesV1(ctx context.Context) models.OCRCapabilitiesResponseV1 {
	capabilities := NewOCRTool().detectCapabilities(ctx)

	message := "Local OCR is unavailable."
	if capabilities.Available() {
		message = fmt.Sprintf("Local OCR is available with %d installed %s.", len(capabilities.Languages), pluralizeLanguage(len(capabilities.Languages)))
	} else {
		message = OCRUnavailableReason(capabilities)
	}

	languages := capabilities.Languages
	if languages == nil {
		languages = []string{}
	}

	return models.OCRCapabilitiesResponseV1{
		Success:             true,
		Message:             message,
		Available:           capabilities.Available(),
		PDFToPPM:            capabilities.PDFToPPM,
		Tesseract:           capabilities.Tesseract,
		PDFToText:           capabilities.PDFToText,
		Languages:           languages,
		RecommendedLanguage: capabilities.RecommendedLanguage,
	}
}

func pluralizeLanguage(count int) string {
	if count == 1 {
		return "language"
	}

	return "languages"
}

func (t *OCRTool) detectCapabilities(ctx context.Context) engine.OCRCapabilities {
	if t.capabilities != nil {
		return t.capabilities(ctx)
	}

	return engine.DetectCapabilities(ctx, t.runner)
}
