package pdf

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/pdf/engine"
	"fileforge-desktop/internal/toolchain"
)

const (
	// ToolIDPDFToDOCX converts a PDF into an editable DOCX.
	ToolIDPDFToDOCX = "tool.pdf.to_docx"

	pdfToDOCXOutputKindFile = "file"
	pdfToDOCXExtension      = "docx"

	// PDFToDOCXUnavailableReason is shown whenever the managed pdf2docx
	// virtual environment is missing.
	PDFToDOCXUnavailableReason = "The PDF to DOCX engine is not installed. Run Install / repair."

	// pdfToDOCXErrorReasonMaxRunes bounds the cause text copied into a job error.
	pdfToDOCXErrorReasonMaxRunes = 200
)

// pdf2docxPythonPath resolves the managed virtual environment interpreter. It
// is a package variable so tests can simulate a missing runtime.
var pdf2docxPythonPath = func() (string, bool) {
	dataDir, err := toolchain.DataDir()
	if err != nil {
		return "", false
	}

	return toolchain.ManagedPDF2DOCXPythonPath(dataDir)
}

// pdfToDOCXConvert runs the engine conversion. It is a package variable so
// tests can stub the converter without the real virtual environment.
var pdfToDOCXConvert = func(ctx context.Context, runner engine.DOCXCommandRunner, req engine.PDFToDOCXRequest, onProgress func(engine.PDFToDOCXProgressEvent)) (engine.PDFToDOCXResult, *engine.PDFToDOCXError) {
	return engine.ConvertPDFToDOCX(ctx, runner, req, onProgress)
}

// PDFToDOCXTool converts one PDF into a DOCX with pdf2docx.
type PDFToDOCXTool struct{}

func NewPDFToDOCXTool() *PDFToDOCXTool {
	return &PDFToDOCXTool{}
}

func (t *PDFToDOCXTool) ID() string {
	return ToolIDPDFToDOCX
}

func (t *PDFToDOCXTool) Capability() string {
	return ToolIDPDFToDOCX
}

func (t *PDFToDOCXTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "PDF to DOCX",
		Description:      "Convert a PDF into an editable DOCX while preserving fonts, weight, colors, images, tables and page layout.",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    false,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"docx"},
		RuntimeDeps:      []string{"python", "pdf2docx"},
		Tags:             []string{"pdf", "docx", "word", "layout", "fidelity"},
	}
}

func (t *PDFToDOCXTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	if _, available := pdf2docxPythonPath(); !available {
		return models.ToolRuntimeState{
			Status:  "degraded",
			Healthy: false,
			Reason:  PDFToDOCXUnavailableReason,
		}
	}

	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *PDFToDOCXTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan validates the single input, the page count and the runtime, then
// resolves the destination DOCX without writing anything.
func (t *PDFToDOCXTool) Plan(_ context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	parsed, parseErr := parsePDFToDOCXRequest(req)
	if parseErr != nil {
		return planFailure(plan, parseErr)
	}

	pages, pageErr := api.PageCountFile(context.Background(), parsed.InputPath)
	if pageErr != nil || pages <= 0 {
		jobErr := models.NewCanonicalJobError(
			engine.PDFToDOCXErrorInvalidInput,
			"The PDF could not be read. It may be encrypted or damaged.",
			map[string]any{"inputPath": parsed.InputPath},
		)

		return planFailure(plan, jobErr)
	}

	if _, available := pdf2docxPythonPath(); !available {
		jobErr := models.NewJobError(
			models.ErrorCodeRuntimeDepMissing,
			engine.PDFToDOCXErrorRuntimeUnavailable,
			PDFToDOCXUnavailableReason,
			nil,
		)

		return planFailure(plan, jobErr)
	}

	outputPath, naturalPath, adjusted, outputErr := resolvePDFToDOCXOutputPath(parsed)
	if outputErr != nil {
		return planFailure(plan, outputErr)
	}

	planned := models.PlannedOutput{
		InputPath:   parsed.InputPath,
		OutputPath:  outputPath,
		OutputKind:  pdfToDOCXOutputKindFile,
		OutputCount: 1,
		Adjusted:    adjusted,
	}
	if adjusted {
		planned.Warnings = append(planned.Warnings, fmt.Sprintf(
			"%s already exists. Saving as %s.",
			filepath.Base(naturalPath),
			filepath.Base(outputPath),
		))
	}

	plan.Success = true
	plan.Message = fmt.Sprintf("Ready to convert %d %s to DOCX.", pages, pdfToDOCXPlural(pages, "page", "pages"))
	plan.Outputs = []models.PlannedOutput{planned}

	return plan, nil
}

// ExecuteSingle implements tools.SingleExecutor by delegating to the progress
// variant with a no-op callback.
func (t *PDFToDOCXTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

// ExecuteSingleWithProgress resolves the plan once, executes exactly that plan
// and forwards the engine progress as job progress.
func (t *PDFToDOCXTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	inputPath := firstInputPath(req.InputPaths)

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return pdfToDOCXFailureItem(inputPath, pdfToDOCXPlannedOutputPath(plan), planErr), planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, onProgress)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("PDF_TO_DOCX_EXECUTION_MISSING_RESULT", "PDF to DOCX execution returned no result", nil)
	}

	return pdfToDOCXFailureItem(inputPath, pdfToDOCXPlannedOutputPath(plan), jobErr), jobErr
}

// ExecutePlan converts the PDF into the destination resolved by Plan. The plan
// already validated the input and the runtime; execution only re-resolves the
// interpreter path it needs to launch the converter.
func (t *PDFToDOCXTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := parsePDFToDOCXRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}

	if len(plan.Outputs) == 0 {
		jobErr := models.NewCanonicalJobError("PDF_TO_DOCX_EXECUTION_MISSING_RESULT", "the plan resolved no output", nil)
		return []models.JobResultItem{pdfToDOCXFailureItem(parsed.InputPath, "", jobErr)}, jobErr
	}

	planned := plan.Outputs[0]
	if strings.TrimSpace(planned.InputPath) == "" {
		planned.InputPath = parsed.InputPath
	}

	pythonPath, available := pdf2docxPythonPath()
	if !available {
		jobErr := models.NewJobError(
			models.ErrorCodeRuntimeDepMissing,
			engine.PDFToDOCXErrorRuntimeUnavailable,
			PDFToDOCXUnavailableReason,
			nil,
		)

		return []models.JobResultItem{pdfToDOCXFailureItem(planned.InputPath, planned.OutputPath, jobErr)}, jobErr
	}

	progress := func(event engine.PDFToDOCXProgressEvent) {
		if onProgress == nil {
			return
		}

		onProgress(models.JobProgress{
			Current: event.Percent,
			Total:   100,
			Stage:   "processing",
			Message: event.Message,
		})
	}

	result, conversionErr := pdfToDOCXConvert(ctx, &engine.ExecDOCXCommandRunner{}, engine.PDFToDOCXRequest{
		InputPath:  planned.InputPath,
		OutputPath: planned.OutputPath,
		PythonPath: pythonPath,
	}, progress)
	if conversionErr != nil {
		jobErr := mapPDFToDOCXExecutionError(ctx, conversionErr)

		return []models.JobResultItem{pdfToDOCXFailureItem(planned.InputPath, planned.OutputPath, jobErr)}, jobErr
	}

	return []models.JobResultItem{pdfToDOCXSuccessItem(planned, result)}, nil
}

type pdfToDOCXRequestFields struct {
	Mode       string
	InputPath  string
	OutputPath string
	OutputDir  string
}

// parsePDFToDOCXRequest validates the request shape and the input file. The
// output is resolved separately because it depends on the runtime state.
func parsePDFToDOCXRequest(req models.JobRequest) (pdfToDOCXRequestFields, *models.JobError) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" {
		return pdfToDOCXRequestFields{}, pdfToDOCXRequestError("mode must be single")
	}
	if len(req.InputPaths) != 1 {
		return pdfToDOCXRequestFields{}, pdfToDOCXRequestError("exactly 1 input PDF is required")
	}

	inputPath := strings.TrimSpace(req.InputPaths[0])
	if inputPath == "" {
		return pdfToDOCXRequestFields{}, pdfToDOCXRequestError("inputPath is required")
	}
	if !strings.EqualFold(filepath.Ext(inputPath), ".pdf") {
		return pdfToDOCXRequestFields{}, models.NewCanonicalJobError(
			engine.PDFToDOCXErrorInvalidInput,
			fmt.Sprintf("input file must be .pdf: %s", inputPath),
			map[string]any{"inputPath": inputPath},
		)
	}

	info, statErr := os.Stat(inputPath)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return pdfToDOCXRequestFields{}, models.NewCanonicalJobError(
				engine.PDFToDOCXErrorInvalidInput,
				fmt.Sprintf("Input PDF was not found: %s", filepath.Base(inputPath)),
				map[string]any{"inputPath": inputPath},
			)
		}

		return pdfToDOCXRequestFields{}, models.NewCanonicalJobError(
			engine.PDFToDOCXErrorInvalidInput,
			fmt.Sprintf("Input PDF is not accessible: %s", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}
	if info.IsDir() {
		return pdfToDOCXRequestFields{}, models.NewCanonicalJobError(
			engine.PDFToDOCXErrorInvalidInput,
			fmt.Sprintf("input path is a directory: %s", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}
	if !info.Mode().IsRegular() {
		return pdfToDOCXRequestFields{}, models.NewCanonicalJobError(
			engine.PDFToDOCXErrorInvalidInput,
			fmt.Sprintf("input path is not a regular file: %s", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}

	outputPath := strings.TrimSpace(optionString(req.Options, "outputPath"))

	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = optionString(req.Options, "outputDir")
	}
	if outputDir != "" {
		outputDir = filepath.Clean(outputDir)
	}

	return pdfToDOCXRequestFields{
		Mode:       mode,
		InputPath:  inputPath,
		OutputPath: outputPath,
		OutputDir:  outputDir,
	}, nil
}

// resolvePDFToDOCXOutputPath honors an explicit outputPath or derives a
// collision-safe <stem>.docx in the chosen folder. It returns the resolved
// path, the natural (unsuffixed) path and whether a suffix was added.
func resolvePDFToDOCXOutputPath(parsed pdfToDOCXRequestFields) (string, string, bool, *models.JobError) {
	if parsed.OutputPath != "" {
		if !strings.EqualFold(filepath.Ext(parsed.OutputPath), ".docx") {
			return "", "", false, models.NewCanonicalJobError(
				engine.PDFToDOCXErrorInvalidInput,
				"options.outputPath must use .docx extension",
				map[string]any{"outputPath": parsed.OutputPath},
			)
		}

		if _, statErr := os.Stat(parsed.OutputPath); statErr == nil {
			return "", "", false, models.NewCanonicalJobError(
				engine.PDFToDOCXErrorOutputFailed,
				fmt.Sprintf("Output file already exists: %s", parsed.OutputPath),
				map[string]any{"outputPath": parsed.OutputPath},
			)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", "", false, models.NewCanonicalJobError(
				engine.PDFToDOCXErrorOutputFailed,
				fmt.Sprintf("Output path is not accessible: %s", parsed.OutputPath),
				map[string]any{"outputPath": parsed.OutputPath},
			)
		}

		return parsed.OutputPath, parsed.OutputPath, false, nil
	}

	dir := parsed.OutputDir
	if dir == "" {
		dir = filepath.Dir(parsed.InputPath)
	}
	dir = filepath.Clean(dir)

	stem := strings.TrimSuffix(filepath.Base(parsed.InputPath), filepath.Ext(parsed.InputPath))
	if strings.TrimSpace(stem) == "" {
		stem = "document"
	}

	naturalPath := filepath.Join(dir, stem+"."+pdfToDOCXExtension)
	used := make(map[string]struct{}, 1)
	outputplan.MarkUsed(used, parsed.InputPath)
	outputPath, adjusted := outputplan.UniqueFilePath(dir, stem, pdfToDOCXExtension, used)

	return outputPath, naturalPath, adjusted, nil
}

func pdfToDOCXSuccessItem(planned models.PlannedOutput, result engine.PDFToDOCXResult) models.JobResultItem {
	outputPath := result.OutputPath
	if strings.TrimSpace(outputPath) == "" {
		outputPath = planned.OutputPath
	}

	message := fmt.Sprintf(
		"DOCX created: %d %s, %d bold %s, %d %s, %d %s.",
		result.Stats.Paragraphs, pdfToDOCXPlural(result.Stats.Paragraphs, "paragraph", "paragraphs"),
		result.Stats.BoldRuns, pdfToDOCXPlural(result.Stats.BoldRuns, "run", "runs"),
		result.Stats.Images, pdfToDOCXPlural(result.Stats.Images, "image", "images"),
		result.Stats.Tables, pdfToDOCXPlural(result.Stats.Tables, "table", "tables"),
	)
	if result.Stats.TextCharacters == 0 {
		message += " No text was found; the PDF may be scanned. Run PDF OCR first."
	}

	return models.JobResultItem{
		InputPath:   planned.InputPath,
		OutputPath:  outputPath,
		Outputs:     []string{outputPath},
		OutputCount: 1,
		Success:     true,
		Message:     message,
	}
}

func pdfToDOCXFailureItem(inputPath, outputPath string, jobErr *models.JobError) models.JobResultItem {
	return models.JobResultItem{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Success:    false,
		Message:    jobErr.Message,
		Error:      jobErr,
	}
}

func pdfToDOCXPlannedOutputPath(plan models.PlanJobResponse) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
}

func pdfToDOCXPlural(count int, singular, plural string) string {
	if count == 1 {
		return singular
	}

	return plural
}

func pdfToDOCXRequestError(message string) *models.JobError {
	return models.NewCanonicalJobError(engine.PDFToDOCXErrorInvalidInput, message, nil)
}

// mapPDFToDOCXExecutionError turns a conversion failure into a job error.
// Cancellation is reported as CANCELLED_BY_USER so the job is cancelled instead
// of failed; every other failure keeps the stable engine code in both fields.
func mapPDFToDOCXExecutionError(ctx context.Context, err error) *models.JobError {
	var docxErr *engine.PDFToDOCXError
	if !errors.As(err, &docxErr) {
		return &models.JobError{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	if docxErr.Code == engine.PDFToDOCXErrorCancelled || (ctx != nil && ctx.Err() != nil) {
		return models.NewJobError(
			models.ErrorCodeCancelledByUser,
			engine.PDFToDOCXErrorCancelled,
			"PDF to DOCX was canceled.",
			docxErr.Details,
		)
	}

	return &models.JobError{
		Code:       docxErr.Code,
		DetailCode: docxErr.Code,
		Message:    docxErr.Message,
		Details:    pdfToDOCXDetailsWithReason(docxErr.Details, docxErr.Cause),
	}
}

// pdfToDOCXDetailsWithReason copies the engine details and adds a bounded
// reason taken from the cause, so raw library errors never flood the response.
func pdfToDOCXDetailsWithReason(details map[string]any, cause error) map[string]any {
	reason := ""
	if cause != nil {
		reason = strings.TrimSpace(cause.Error())
	}
	if reason == "" {
		return details
	}

	if runes := []rune(reason); len(runes) > pdfToDOCXErrorReasonMaxRunes {
		reason = string(runes[:pdfToDOCXErrorReasonMaxRunes]) + "..."
	}

	merged := make(map[string]any, len(details)+1)
	for key, value := range details {
		merged[key] = value
	}
	merged["reason"] = reason

	return merged
}
