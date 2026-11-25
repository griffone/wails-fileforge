package doc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fileforge-desktop/internal/doc/engine"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/platform/fsx"
	"fileforge-desktop/internal/utils"
)

const (
	ToolIDDocDOCXToPDF = "tool.doc.docx_to_pdf"

	// docxConvertWorkers bounds concurrent conversions. Rendering is in-process
	// and allocates a full document layout, so two conversions per job is the
	// ceiling.
	docxConvertWorkers = 2
)

type DOCXToPDFTool struct {
	renderer engine.DOCXRenderer
	runner   engine.CommandRunner
}

func NewDOCXToPDFTool() *DOCXToPDFTool {
	return &DOCXToPDFTool{}
}

func NewDOCXToPDFToolWithDeps(renderer engine.DOCXRenderer, runner engine.CommandRunner) *DOCXToPDFTool {
	return &DOCXToPDFTool{renderer: renderer, runner: runner}
}

func (t *DOCXToPDFTool) ID() string {
	return ToolIDDocDOCXToPDF
}

func (t *DOCXToPDFTool) Capability() string {
	return ToolIDDocDOCXToPDF
}

func (t *DOCXToPDFTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "DOCX to PDF",
		Description:      "Convert DOCX to PDF with standard fidelity using the bundled pure-Go renderer. Complex desktop publishing layouts, exact floating-shape wrapping and some SmartArt diagrams may differ; PDF/A export needs Ghostscript.",
		Domain:           "doc",
		Capability:       t.Capability(),
		Version:          "1.1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"docx"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{},
		ResourceClass:    "external",
		MaxWorkers:       2,
		Tags:             []string{"doc", "docx", "pdf", "standard-fidelity"},
	}
}

// RuntimeState is always healthy: rendering is bundled in the binary. PDF/A is
// the only optional external step and is checked when the export is requested.
func (t *DOCXToPDFTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *DOCXToPDFTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan validates every input and resolves one collision-safe output per input
// without writing anything. ExecutePlan reuses this plan, so the shown
// destination and the written file always agree.
func (t *DOCXToPDFTool) Plan(_ context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	parsed, parseErr := parseDOCXRequest(req)
	if parseErr != nil {
		return docPlanFailure(plan, parseErr)
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
		plan.Outputs = []models.PlannedOutput{planned}
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

	outputs := make([]models.PlannedOutput, 0, len(parsed.inputPaths))
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

// ExecuteSingle resolves the plan first and then runs that exact plan, so it
// cannot drift from ExecutePlan.
func (t *DOCXToPDFTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return models.JobResultItem{InputPath: firstPath(req.InputPaths), Success: false, Message: planErr.Message, Error: planErr}, planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, nil)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("DOC_DOCX_TO_PDF_EXECUTION_FAILED", "docx conversion returned no result", nil)
	}

	return models.JobResultItem{InputPath: firstPath(req.InputPaths), Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
}

// ExecuteBatch plans the whole request once and then executes exactly that
// plan. It never re-resolves output paths from the request.
func (t *DOCXToPDFTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan converts exactly the outputs resolved by Plan. It reuses the
// plan's effective options and skips the ZIP protection scan that Plan already
// performed, so a reduced retry plan only converts its own outputs. Results keep
// plan order and item failures stay isolated.
func (t *DOCXToPDFTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	total := len(plan.Outputs)
	if total == 0 {
		return nil, models.NewCanonicalJobError("DOC_DOCX_TO_PDF_INPUT_COUNT_INVALID", "at least one .docx input is required", nil)
	}

	parsed, parseErr := parseDOCXRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}
	parsed = plannedDOCXOptions(plan, parsed)

	items := make([]models.JobResultItem, total)
	completed := make([]bool, total)
	progress := newDocItemProgress(total, onProgress)

	runErr := utils.ParallelForIndex(ctx, total, docxConvertWorkers, func(ctx context.Context, index int) error {
		planned := plan.Outputs[index]
		items[index] = t.convertPlannedDOCX(ctx, planned, parsed.pdfa, parsed.pageSelection)
		completed[index] = true
		progress.itemCompleted()

		return nil
	})

	if docBatchCancelled(ctx, runErr) {
		return collectCompletedDocItems(items, completed), models.NewCanonicalJobError("DOC_DOCX_TO_PDF_CANCELLED", docBatchCancellationCause(ctx, runErr).Error(), nil)
	}
	if runErr != nil {
		return collectCompletedDocItems(items, completed), models.NewCanonicalJobError("DOC_DOCX_TO_PDF_EXECUTION_FAILED", runErr.Error(), nil)
	}

	return items, firstFailedDocItemError(items)
}

// convertPlannedDOCX runs one already-planned conversion. It never re-plans, so
// it is safe to run concurrently.
func (t *DOCXToPDFTool) convertPlannedDOCX(ctx context.Context, planned models.PlannedOutput, pdfa bool, pageSelection string) models.JobResultItem {
	_, execErr := engine.ConvertDOCX(ctx, t.renderer, t.runner, engine.ConvertDOCXRequest{
		InputPath:          planned.InputPath,
		OutputPath:         planned.OutputPath,
		PDFA:               pdfa,
		PageSelection:      pageSelection,
		SkipProtectedCheck: true,
	})
	if execErr != nil {
		jobErr := mapDOCXEngineError(execErr)
		return models.JobResultItem{InputPath: planned.InputPath, OutputPath: planned.OutputPath, Success: false, Message: jobErr.Message, Error: jobErr}
	}

	return models.JobResultItem{
		InputPath:   planned.InputPath,
		OutputPath:  planned.OutputPath,
		Outputs:     []string{planned.OutputPath},
		OutputCount: 1,
		Success:     true,
		Message:     "docx to pdf successful",
	}
}

// plannedDOCXOptions returns the conversion options resolved by Plan, falling
// back to the re-parsed request when a plan was built by an older caller.
func plannedDOCXOptions(plan models.PlanJobResponse, parsed docxRequest) docxRequest {
	if plan.EffectiveOptions == nil {
		return parsed
	}

	if pdfa, ok := plan.EffectiveOptions["pdfa"].(bool); ok {
		parsed.pdfa = pdfa
	}
	if selection, ok := plan.EffectiveOptions["pageSelection"].(string); ok {
		parsed.pageSelection = engine.NormalizePageSelection(selection)
	}

	return parsed
}

type docxRequest struct {
	mode          string
	inputPaths    []string
	outputDir     string
	outputPath    string
	pageSelection string
	pdfa          bool
}

func parseDOCXRequest(req models.JobRequest) (docxRequest, *models.JobError) {
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
func docxOptionBool(options map[string]any, key string) (bool, *models.JobError) {
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

func validateDOCXInputPath(inputPath string) *models.JobError {
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

func validateDOCXOutputDir(dir string) *models.JobError {
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
func planSingleDOCXOutput(parsed docxRequest) (models.PlannedOutput, *models.JobError) {
	inputPath := parsed.inputPaths[0]

	if explicit := strings.TrimSpace(parsed.outputPath); explicit != "" {
		requestedPath := filepath.Clean(explicit)
		planned := models.PlannedOutput{
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
		if fsx.SameFile(inputPath, requestedPath) {
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
		return models.PlannedOutput{InputPath: inputPath, OutputKind: docPlannedOutputKindFile}, dirErr
	}

	return planAutoDOCXOutput(inputPath, targetDir, nil), nil
}

// planAutoDOCXOutput derives <stem>_docx2pdf.pdf inside outputDir, suffixing
// when the natural name is taken on disk or already used in the same plan.
func planAutoDOCXOutput(inputPath, outputDir string, used map[string]struct{}) models.PlannedOutput {
	planned := models.PlannedOutput{
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
func docxProtectedInputError(inputPath string, err error) *models.JobError {
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

func mapDOCXEngineError(err error) *models.JobError {
	if err == nil {
		return nil
	}

	var docxErr *engine.DOCXError
	if errors.As(err, &docxErr) {
		return models.NewCanonicalJobError(docxErr.Code, docxErr.Message, docxErr.Details)
	}

	return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_EXECUTION_FAILED", err.Error(), nil)
}
