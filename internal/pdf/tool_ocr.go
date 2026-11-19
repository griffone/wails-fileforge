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
	"fileforge-desktop/internal/utils"
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
		plan.EffectiveOptions = ocrEffectiveOptions(parsed, language, capabilities)

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
	plan.EffectiveOptions = ocrEffectiveOptions(parsed, language, capabilities)

	return plan, nil
}

// ExecuteSingle implements tools.SingleExecutor by delegating to the progress
// variant with a no-op callback.
func (t *OCRTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

// ExecuteSingleWithProgress resolves the plan once, executes exactly that plan
// and reports page-level progress for the one input.
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

	items, execErr := t.ExecutePlan(ctx, req, plan, onProgress)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("PDF_OCR_EXECUTION_MISSING_RESULT", "OCR execution returned no result", nil)
	}

	return ocrFailureItem(parsed.InputPaths[0], ocrPlannedOutputPath(plan), jobErr), jobErr
}

// ExecuteBatch plans the request once and then executes exactly that plan. It
// never re-resolves output paths from the request.
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

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan runs OCR for exactly the inputs resolved by Plan. The runtime is
// read from the plan, so the batch never probes Tesseract per input; inputs run
// with a small bounded pool and results keep plan order.
func (t *OCRTool) ExecutePlan(ctx context.Context, req models.JobRequestV1, plan models.PlanJobResponseV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, parseErr := ocrReqFields(req)
	if parseErr != nil {
		return nil, parseErr
	}

	textSidecar := parsed.TextSidecar
	if value, ok := planBoolOption(plan.EffectiveOptions, "textSidecar"); ok {
		textSidecar = value
	}
	language := parsed.Language
	if value := stringOption(plan.EffectiveOptions, "language"); value != "" {
		language = value
	}
	dpi := parsed.DPI
	if value := intOption(plan.EffectiveOptions, "dpi"); value > 0 {
		dpi = value
	}
	capabilities, plannedCapabilities := planOCRCapabilities(plan.EffectiveOptions)
	if !plannedCapabilities {
		// Plans built by an older caller carry no runtime snapshot; detect it
		// once for the whole execution instead of once per input.
		capabilities = t.detectCapabilities(ctx)
	}

	jobs, groupErr := groupOCROutputs(plan.Outputs, textSidecar)
	if groupErr != nil {
		return nil, groupErr
	}

	items := make([]models.JobResultItemV1, len(plan.Outputs))
	groupErrors := make([]*models.JobErrorV1, len(jobs))
	completed := make([]bool, len(plan.Outputs))
	singleInput := len(jobs) == 1

	// A single input keeps the legacy page-level progress only; the batch
	// completion counter would add a spurious 1/1 update.
	var progress *pdfBatchProgress
	if !singleInput {
		progress = newPDFBatchProgress(len(jobs), onProgress)
	}

	runErr := utils.ParallelForIndex(ctx, len(jobs), ocrInputWorkerCount(), func(ctx context.Context, index int) error {
		job := jobs[index]

		var pageProgress func(models.JobProgressV1)
		if singleInput {
			pageProgress = onProgress
		}

		outcome, ocrErr := t.runOCRWithCapabilities(ctx, job, language, dpi, capabilities, pageProgress)
		if ocrErr != nil {
			jobErr := mapOCRError(ocrErr)
			groupErrors[index] = jobErr
			for _, outputIndex := range job.outputIndices {
				items[outputIndex] = ocrFailureItem(job.InputPath, plan.Outputs[outputIndex].OutputPath, jobErr)
				completed[outputIndex] = true
			}
		} else {
			for _, outputIndex := range job.outputIndices {
				items[outputIndex] = ocrPlannedItem(job, plan.Outputs[outputIndex], outcome)
				completed[outputIndex] = true
			}
		}

		progress.itemCompleted()

		return nil
	})

	if runErr != nil {
		if ctx.Err() != nil {
			if singleInput {
				for _, item := range items {
					if item.Error != nil {
						return items, item.Error
					}
				}
			}

			return completedPDFBatchItems(items, completed), &models.JobErrorV1{Code: "CANCELED", Message: ctx.Err().Error()}
		}

		return completedPDFBatchItems(items, completed), models.NewCanonicalJobError(engine.OCRCodeRecognizeFailed, "batch OCR failed", map[string]any{"cause": runErr.Error()})
	}

	fileErrors := make([]map[string]any, 0)
	for index, jobErr := range groupErrors {
		if jobErr == nil {
			continue
		}
		fileErrors = append(fileErrors, map[string]any{
			"path":    jobs[index].InputPath,
			"code":    jobErr.DetailCode,
			"message": jobErr.Message,
		})
	}

	if len(fileErrors) > 0 {
		return items, models.NewCanonicalJobError(engine.OCRCodeRecognizeFailed, "one or more files failed in batch OCR", map[string]any{
			"fileErrors": fileErrors,
		})
	}

	return items, nil
}

// ocrBatchInputWorkers caps concurrent OCR inputs. Each pipeline already runs a
// bounded pool of Tesseract processes, so two inputs saturate a desktop.
const ocrBatchInputWorkers = 2

func ocrInputWorkerCount() int {
	workers := utils.ParallelWorkerCount()
	if workers > ocrBatchInputWorkers {
		workers = ocrBatchInputWorkers
	}
	if workers < 1 {
		workers = 1
	}

	return workers
}

// ocrPlannedJob is one resolved input plus its final PDF and sidecar paths and
// the plan output positions it satisfies.
type ocrPlannedJob struct {
	InputPath     string
	OutputPath    string
	TextPath      string
	OutputCount   int
	outputIndices []int
}

func (t *OCRTool) runOCRWithCapabilities(ctx context.Context, job ocrPlannedJob, language string, dpi int, capabilities engine.OCRCapabilities, onProgress func(models.JobProgressV1)) (engine.OCRResult, *engine.OCRError) {
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

	return engine.ConvertWithCapabilities(ctx, t.runner, capabilities, engine.OCRRequest{
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

	return models.JobResultItemV1{
		InputPath:   job.InputPath,
		OutputPath:  job.OutputPath,
		Outputs:     outputs,
		OutputCount: len(outputs),
		Success:     true,
		Message:     ocrSuccessMessage(outcome),
	}
}

// ocrPlannedItem reports one planned output. The PDF output keeps the legacy
// item shape with both declared outputs; the text sidecar reports its own file
// so plan alignment stays 1:1.
func ocrPlannedItem(job ocrPlannedJob, planned models.PlannedOutputV1, outcome engine.OCRResult) models.JobResultItemV1 {
	if job.TextPath != "" && strings.EqualFold(planned.OutputPath, job.TextPath) {
		return models.JobResultItemV1{
			InputPath:   job.InputPath,
			OutputPath:  planned.OutputPath,
			Outputs:     []string{planned.OutputPath},
			OutputCount: 1,
			Success:     true,
			Message:     ocrSuccessMessage(outcome),
		}
	}

	return ocrSuccessItem(job, outcome)
}

func ocrSuccessMessage(outcome engine.OCRResult) string {
	message := fmt.Sprintf("OCR finished: %d pages, %d characters recognized.", outcome.PageCount, outcome.TextCharacters)
	if len(outcome.Warnings) > 0 {
		message += " " + strings.Join(outcome.Warnings, " ")
	}

	return message
}

// groupOCROutputs pairs the flat planned outputs (PDF then TXT per input) with
// their source paths. A reduced retry plan may contain only one side of a pair;
// every planned output is still kept so it gets its own result or failure.
func groupOCROutputs(outputs []models.PlannedOutputV1, textSidecar bool) ([]ocrPlannedJob, *models.JobErrorV1) {
	if len(outputs) == 0 {
		return nil, ocrRequestError(models.ErrorCodeValidationInvalidInput, engine.ErrorCodeValidation, "the plan resolved no outputs")
	}

	jobs := make([]ocrPlannedJob, 0, len(outputs))
	for index := 0; index < len(outputs); index++ {
		planned := outputs[index]

		if ocrIsTextOutput(planned.OutputPath) {
			jobs = append(jobs, ocrPlannedJob{
				InputPath:     planned.InputPath,
				TextPath:      planned.OutputPath,
				OutputCount:   1,
				outputIndices: []int{index},
			})
			continue
		}

		job := ocrPlannedJob{
			InputPath:     planned.InputPath,
			OutputPath:    planned.OutputPath,
			OutputCount:   1,
			outputIndices: []int{index},
		}
		if textSidecar && index+1 < len(outputs) {
			sidecar := outputs[index+1]
			if sidecar.InputPath == planned.InputPath && ocrIsTextOutput(sidecar.OutputPath) {
				job.TextPath = sidecar.OutputPath
				job.OutputCount = 2
				job.outputIndices = append(job.outputIndices, index+1)
				index++
			}
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

func ocrIsTextOutput(outputPath string) bool {
	return strings.EqualFold(filepath.Ext(outputPath), "."+ocrTextExtension)
}

// planOCRCapabilities reads the runtime snapshot stored in the plan. A missing
// or mistyped value asks the caller to detect it once.
func planOCRCapabilities(options map[string]any) (engine.OCRCapabilities, bool) {
	if options == nil {
		return engine.OCRCapabilities{}, false
	}

	capabilities, ok := options["capabilities"].(engine.OCRCapabilities)
	if !ok {
		return engine.OCRCapabilities{}, false
	}

	return capabilities, true
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

func ocrEffectiveOptions(parsed ocrRequestFields, language string, capabilities engine.OCRCapabilities) map[string]any {
	return map[string]any{
		"language":     language,
		"textSidecar":  parsed.TextSidecar,
		"dpi":          parsed.DPI,
		"capabilities": capabilities,
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
