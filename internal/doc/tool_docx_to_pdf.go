package doc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"

	"fileforge-desktop/internal/doc/engine"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
)

const ToolIDDocDOCXToPDFV1 = "tool.doc.docx_to_pdf"

type DOCXToPDFTool struct {
	probe  engine.RuntimeProbe
	runner engine.CommandRunner
}

func NewDOCXToPDFTool() *DOCXToPDFTool {
	return &DOCXToPDFTool{}
}

func NewDOCXToPDFToolWithDeps(probe engine.RuntimeProbe, runner engine.CommandRunner) *DOCXToPDFTool {
	return &DOCXToPDFTool{probe: probe, runner: runner}
}

func (t *DOCXToPDFTool) ID() string {
	return ToolIDDocDOCXToPDFV1
}

func (t *DOCXToPDFTool) Capability() string {
	return ToolIDDocDOCXToPDFV1
}

func (t *DOCXToPDFTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "DOCX to PDF",
		Description:      "Convert DOCX to PDF with standard fidelity (LibreOffice + Pandoc fallback)",
		Domain:           "doc",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"docx"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{"libreoffice", "pandoc"},
		Tags:             []string{"doc", "docx", "pdf", "standard-fidelity", "fallback"},
	}
}

func (t *DOCXToPDFTool) RuntimeState(ctx context.Context) models.ToolRuntimeStateV1 {
	probe := t.probe
	if probe == nil {
		probe = engine.NewHybridRuntimeProbe()
	}

	if err := probe.Check(ctx); err != nil {
		return models.ToolRuntimeStateV1{Status: "degraded", Healthy: false, Reason: err.Error()}
	}

	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *DOCXToPDFTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan probes the conversion runtime, validates every input and resolves one
// collision-safe output per input without writing anything. Execution calls
// Plan again right before converting, so the shown destination and the written
// file always agree.
func (t *DOCXToPDFTool) Plan(ctx context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	parsed, parseErr := parseDOCXRequest(req)
	if parseErr != nil {
		return docPlanFailure(plan, parseErr)
	}

	probe := t.probe
	if probe == nil {
		probe = engine.NewHybridRuntimeProbe()
	}

	var probeErr error
	if parsed.pdfa {
		if pdfaProbe, ok := probe.(engine.PDFAProbe); ok {
			probeErr = pdfaProbe.CheckPDFA(ctx)
		} else {
			probeErr = probe.Check(ctx)
		}
	} else {
		probeErr = probe.Check(ctx)
	}
	if probeErr != nil {
		return docPlanFailure(plan, mapDOCXEngineError(probeErr))
	}

	for _, inputPath := range parsed.inputPaths {
		if inputErr := validateDOCXInputPath(inputPath); inputErr != nil {
			return docPlanFailure(plan, inputErr)
		}
		if protectedErr := engine.DetectProtectedDOCX(inputPath); protectedErr != nil {
			return docPlanFailure(plan, docxProtectedInputError(inputPath, protectedErr))
		}
	}

	if parsed.mode == "single" {
		planned, planErr := planSingleDOCXOutput(parsed)
		if planErr != nil {
			return docPlanFailure(plan, planErr)
		}

		plan.Success = true
		plan.Message = "Ready to convert 1 document."
		plan.Outputs = []models.PlannedOutputV1{planned}
		plan.Warnings = docxPlanWarnings(parsed, planned.Warnings)
		plan.EffectiveOptions = docxEffectiveOptions(parsed)

		return plan, nil
	}

	outputDir := strings.TrimSpace(parsed.outputDir)
	if outputDir != "" {
		if dirErr := validateDOCXOutputDir(outputDir); dirErr != nil {
			return docPlanFailure(plan, dirErr)
		}
	}

	// Reserve every input path so an auto-derived output can never overwrite a
	// different input of the same batch.
	used := make(map[string]struct{}, len(parsed.inputPaths))
	for _, inputPath := range parsed.inputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	outputs := make([]models.PlannedOutputV1, 0, len(parsed.inputPaths))
	warnings := make([]string, 0)
	for _, inputPath := range parsed.inputPaths {
		targetDir := outputDir
		if targetDir == "" {
			targetDir = filepath.Dir(inputPath)
		}

		planned := planAutoDOCXOutput(inputPath, targetDir, used)
		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = fmt.Sprintf("Ready to convert %d documents.", len(outputs))
	plan.Outputs = outputs
	plan.Warnings = docxPlanWarnings(parsed, warnings)
	plan.EffectiveOptions = docxEffectiveOptions(parsed)

	return plan, nil
}

func (t *DOCXToPDFTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return models.JobResultItemV1{InputPath: firstPath(req.InputPaths), Success: false, Message: planErr.Message, Error: planErr}, planErr
	}

	parsed, parseErr := parseDOCXRequest(req)
	if parseErr != nil {
		return models.JobResultItemV1{InputPath: firstPath(req.InputPaths), Success: false, Message: parseErr.Message, Error: parseErr}, parseErr
	}

	inputPath := plan.Outputs[0].InputPath
	outputPath := plan.Outputs[0].OutputPath

	result, err := engine.ConvertDOCX(ctx, t.probe, t.runner, engine.ConvertDOCXRequest{InputPath: inputPath, OutputPath: outputPath, PDFA: parsed.pdfa})
	if err != nil {
		jobErr := mapDOCXEngineError(err)
		return models.JobResultItemV1{InputPath: inputPath, OutputPath: outputPath, Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	if applyErr := applyDOCXPageSelection(inputPath, outputPath, parsed.pageSelection); applyErr != nil {
		return models.JobResultItemV1{InputPath: inputPath, OutputPath: outputPath, Success: false, Message: applyErr.Message, Error: applyErr}, applyErr
	}

	message := "docx to pdf successful"
	if result.FallbackUsed {
		message = "docx to pdf successful (fallback engine: pandoc)"
	}

	return models.JobResultItemV1{
		InputPath:   inputPath,
		OutputPath:  outputPath,
		Outputs:     []string{outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     message,
	}, nil
}

func (t *DOCXToPDFTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	parsed, parseErr := parseDOCXRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}

	items := make([]models.JobResultItemV1, 0, len(plan.Outputs))
	var firstErr *models.JobErrorV1

	for index, planned := range plan.Outputs {
		select {
		case <-ctx.Done():
			cancelErr := models.NewCanonicalJobError("DOC_DOCX_TO_PDF_CANCELLED", ctx.Err().Error(), nil)
			return items, cancelErr
		default:
		}

		result, execErr := engine.ConvertDOCX(ctx, t.probe, t.runner, engine.ConvertDOCXRequest{InputPath: planned.InputPath, OutputPath: planned.OutputPath, PDFA: parsed.pdfa})
		if execErr != nil {
			jobErr := mapDOCXEngineError(execErr)
			if firstErr == nil {
				firstErr = jobErr
			}
			items = append(items, models.JobResultItemV1{InputPath: planned.InputPath, OutputPath: planned.OutputPath, Success: false, Message: jobErr.Message, Error: jobErr})
		} else if applyErr := applyDOCXPageSelection(planned.InputPath, planned.OutputPath, parsed.pageSelection); applyErr != nil {
			if firstErr == nil {
				firstErr = applyErr
			}
			items = append(items, models.JobResultItemV1{InputPath: planned.InputPath, OutputPath: planned.OutputPath, Success: false, Message: applyErr.Message, Error: applyErr})
		} else {
			message := "docx to pdf successful"
			if result.FallbackUsed {
				message = "docx to pdf successful (fallback engine: pandoc)"
			}
			items = append(items, models.JobResultItemV1{InputPath: planned.InputPath, OutputPath: planned.OutputPath, Outputs: []string{planned.OutputPath}, OutputCount: 1, Success: true, Message: message})
		}

		emitDOCXBatchProgress(onProgress, index+1, len(plan.Outputs))
	}

	return items, firstErr
}

type docxRequest struct {
	mode          string
	inputPaths    []string
	outputDir     string
	outputPath    string
	pageSelection string
	pdfa          bool
}

func parseDOCXRequest(req models.JobRequestV1) (docxRequest, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return docxRequest{}, models.NewCanonicalJobError("DOC_DOCX_TO_PDF_MODE_INVALID", "mode must be single or batch", nil)
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return docxRequest{}, models.NewCanonicalJobError("DOC_DOCX_TO_PDF_INPUT_COUNT_INVALID", "single mode requires exactly one .docx input", nil)
	}
	if mode == "batch" && len(req.InputPaths) < 1 {
		return docxRequest{}, models.NewCanonicalJobError("DOC_DOCX_TO_PDF_INPUT_COUNT_INVALID", "batch mode requires at least one .docx input", nil)
	}

	inputs := make([]string, 0, len(req.InputPaths))
	for _, raw := range req.InputPaths {
		trimmed := strings.TrimSpace(raw)
		if trimmed == "" {
			return docxRequest{}, models.NewCanonicalJobError("DOC_DOCX_TO_PDF_INPUT_REQUIRED", "inputPaths cannot contain empty values", nil)
		}
		inputs = append(inputs, trimmed)
	}

	pdfa, pdfaErr := docxOptionBool(req.Options, "pdfa")
	if pdfaErr != nil {
		return docxRequest{}, pdfaErr
	}

	parsed := docxRequest{
		mode:          mode,
		inputPaths:    inputs,
		outputDir:     strings.TrimSpace(req.OutputDir),
		outputPath:    strings.TrimSpace(optionString(req.Options, "outputPath")),
		pageSelection: engine.NormalizePageSelection(optionString(req.Options, "pageSelection")),
		pdfa:          pdfa,
	}

	if parsed.pageSelection != "" {
		if selectionErr := engine.ValidatePageSelection(parsed.pageSelection); selectionErr != nil {
			return docxRequest{}, models.NewCanonicalJobError(
				"DOC_DOCX_TO_PDF_PAGE_SELECTION_INVALID",
				fmt.Sprintf("pageSelection %q is not valid. Use ranges like 1-3,5 or leave it empty for every page.", parsed.pageSelection),
				map[string]any{"pageSelection": parsed.pageSelection},
			)
		}
	}

	if mode == "single" && parsed.outputDir == "" {
		parsed.outputDir = strings.TrimSpace(optionString(req.Options, "outputDir"))
	}

	return parsed, nil
}

// docxOptionBool reads an optional boolean option. Absent options default to
// false; a supplied value must be a bool or a "true"/"false" string so a typo
// never silently disables the requested export flavor.
func docxOptionBool(options map[string]any, key string) (bool, *models.JobErrorV1) {
	raw, ok := options[key]
	if !ok || raw == nil {
		return false, nil
	}

	switch value := raw.(type) {
	case bool:
		return value, nil
	case string:
		if parsed, err := strconv.ParseBool(strings.TrimSpace(value)); err == nil {
			return parsed, nil
		}
	}

	return false, models.NewCanonicalJobError(
		"DOC_DOCX_TO_PDF_OPTION_INVALID",
		fmt.Sprintf("options.%s must be a boolean", key),
		map[string]any{"option": key},
	)
}

// docxPDFASelectionWarning documents that the archival guarantee is soft once
// the page selection step rewrites the exported PDF. The combination stays
// supported; only strict PDF/A conformance is no longer guaranteed.
const docxPDFASelectionWarning = "PDF/A compliance is not guaranteed when a page selection is applied: the page selection step rewrites the exported file and may drop its PDF/A metadata."

// docxPlanWarnings appends the option-specific warnings to the output warnings.
func docxPlanWarnings(parsed docxRequest, warnings []string) []string {
	if parsed.pdfa && parsed.pageSelection != "" {
		warnings = append(warnings, docxPDFASelectionWarning)
	}
	return warnings
}

// docxEffectiveOptions summarizes the options that change the conversion
// result. It stays nil when the request kept every default.
func docxEffectiveOptions(parsed docxRequest) map[string]any {
	options := make(map[string]any, 2)
	if parsed.pageSelection != "" {
		options["pageSelection"] = parsed.pageSelection
	}
	if parsed.pdfa {
		options["pdfa"] = true
	}

	if len(options) == 0 {
		return nil
	}

	return options
}

// applyDOCXPageSelection keeps only the requested pages of a converted PDF.
// An empty selection leaves the conversion untouched.
func applyDOCXPageSelection(inputPath, outputPath, selection string) *models.JobErrorV1 {
	if strings.TrimSpace(selection) == "" {
		return nil
	}

	if err := engine.ApplyPageSelection(inputPath, outputPath, selection); err != nil {
		return models.NewCanonicalJobError(
			"DOC_DOCX_TO_PDF_PAGE_SELECTION_INVALID",
			fmt.Sprintf("page selection %q cannot be applied to %s. The document may have fewer pages than the selection.", selection, filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath, "pageSelection": selection},
		)
	}

	return nil
}

func validateDOCXInputPath(inputPath string) *models.JobErrorV1 {
	name := filepath.Base(inputPath)

	info, err := os.Stat(inputPath)
	if err != nil {
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_INPUT_NOT_FOUND", fmt.Sprintf("%s was not found.", name), map[string]any{"inputPath": inputPath})
	}

	if info.IsDir() {
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_INPUT_IS_DIR", fmt.Sprintf("%s is a folder, not a .docx file.", name), map[string]any{"inputPath": inputPath})
	}

	if strings.ToLower(filepath.Ext(inputPath)) != ".docx" {
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_INPUT_UNSUPPORTED", fmt.Sprintf("%s is not a .docx file.", name), map[string]any{"inputPath": inputPath})
	}

	return nil
}

func validateDOCXOutputDir(dir string) *models.JobErrorV1 {
	if strings.TrimSpace(dir) == "" {
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_DIR_INVALID", "output directory is required", nil)
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_DIR_INVALID", fmt.Sprintf("The output folder does not exist: %s", dir), map[string]any{"outputDir": dir})
		}
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_DIR_INVALID", fmt.Sprintf("The output folder cannot be used: %s", dir), map[string]any{"outputDir": dir})
	}

	if !info.IsDir() {
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_DIR_INVALID", "output directory must be a directory", nil)
	}

	return nil
}

// planSingleDOCXOutput resolves the single-mode destination. An explicit
// outputPath keeps the established contract: .pdf extension, an existing
// parent directory and a collision suffix instead of an overwrite. Otherwise
// the output goes to req.OutputDir when provided, or next to the input.
func planSingleDOCXOutput(parsed docxRequest) (models.PlannedOutputV1, *models.JobErrorV1) {
	inputPath := parsed.inputPaths[0]

	if explicit := strings.TrimSpace(parsed.outputPath); explicit != "" {
		requestedPath := filepath.Clean(explicit)
		planned := models.PlannedOutputV1{
			InputPath:  inputPath,
			OutputPath: requestedPath,
			OutputKind: docPlannedOutputKindFile,
		}

		if strings.ToLower(filepath.Ext(requestedPath)) != ".pdf" {
			return planned, models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_INVALID", "outputPath must use .pdf extension", nil)
		}
		if dirErr := validateDOCXOutputDir(filepath.Dir(requestedPath)); dirErr != nil {
			return planned, dirErr
		}
		if sameDocFile(inputPath, requestedPath) {
			return planned, models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
		}

		stem := strings.TrimSuffix(filepath.Base(requestedPath), filepath.Ext(requestedPath))
		if strings.TrimSpace(stem) == "" {
			stem = "document"
		}

		outputPath, adjusted := outputplan.UniqueFilePath(filepath.Dir(requestedPath), stem, "pdf", nil)
		planned.OutputPath = outputPath
		if adjusted {
			planned.Adjusted = true
			planned.Warnings = append(planned.Warnings, fmt.Sprintf(
				"%s already exists. Saving as %s.",
				filepath.Base(requestedPath),
				filepath.Base(outputPath),
			))
		}

		return planned, nil
	}

	targetDir := strings.TrimSpace(parsed.outputDir)
	if targetDir == "" {
		targetDir = filepath.Dir(inputPath)
	}

	if dirErr := validateDOCXOutputDir(targetDir); dirErr != nil {
		return models.PlannedOutputV1{InputPath: inputPath, OutputKind: docPlannedOutputKindFile}, dirErr
	}

	return planAutoDOCXOutput(inputPath, targetDir, nil), nil
}

// planAutoDOCXOutput derives <stem>_docx2pdf.pdf inside outputDir, suffixing
// when the natural name is taken on disk or already used in the same plan.
func planAutoDOCXOutput(inputPath, outputDir string, used map[string]struct{}) models.PlannedOutputV1 {
	planned := models.PlannedOutputV1{
		InputPath:  inputPath,
		OutputKind: docPlannedOutputKindFile,
	}

	base := docxOutputBaseName(inputPath)
	outputPath, adjusted := outputplan.UniqueFilePath(outputDir, base, "pdf", used)
	planned.OutputPath = outputPath

	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf(
			"%s already exists. Saving as %s.",
			base+".pdf",
			filepath.Base(outputPath),
		))
	}

	return planned
}

func docxOutputBaseName(inputPath string) string {
	stem := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if strings.TrimSpace(stem) == "" {
		stem = "document"
	}

	return stem + "_docx2pdf"
}

// docxProtectedInputError keeps the engine's protected/encrypted detail code
// while naming the offending file for the UI.
func docxProtectedInputError(inputPath string, err error) *models.JobErrorV1 {
	jobErr := mapDOCXEngineError(err)
	if jobErr == nil {
		return nil
	}

	if jobErr.DetailCode != engine.ErrorCodeDocxInputProtected && jobErr.DetailCode != engine.ErrorCodeDocxInputEncrypted {
		return jobErr
	}

	details := map[string]any{"inputPath": inputPath}
	for key, value := range jobErr.Details {
		details[key] = value
	}

	return models.NewCanonicalJobError(
		jobErr.DetailCode,
		fmt.Sprintf("%s is protected, encrypted or not a valid .docx file.", filepath.Base(inputPath)),
		details,
	)
}

func sameDocFile(inputPath, outputPath string) bool {
	if strings.TrimSpace(inputPath) == "" || strings.TrimSpace(outputPath) == "" {
		return false
	}

	left := filepath.Clean(inputPath)
	right := filepath.Clean(outputPath)
	if runtime.GOOS == "windows" {
		return strings.EqualFold(left, right)
	}

	return left == right
}

func emitDOCXBatchProgress(onProgress func(models.JobProgressV1), current, total int) {
	if onProgress == nil {
		return
	}
	onProgress(models.JobProgressV1{Current: current, Total: total, Stage: models.JobStatusRunning, Message: fmt.Sprintf("processed %d/%d", current, total)})
}

func mapDOCXEngineError(err error) *models.JobErrorV1 {
	if err == nil {
		return nil
	}

	var docxErr *engine.DOCXError
	if errors.As(err, &docxErr) {
		return models.NewCanonicalJobError(docxErr.Code, docxErr.Message, docxErr.Details)
	}

	return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_EXECUTION_FAILED", err.Error(), nil)
}
