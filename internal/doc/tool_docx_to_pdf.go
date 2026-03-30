package doc

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"

	"fileforge-desktop/internal/doc/engine"
	"fileforge-desktop/internal/models"
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
	probe := t.probe
	if probe == nil {
		probe = engine.NewHybridRuntimeProbe()
	}

	if err := probe.Check(ctx); err != nil {
		return mapDOCXEngineError(err)
	}

	parsed, jobErr := parseDOCXRequest(req)
	if jobErr != nil {
		return jobErr
	}

	for _, inputPath := range parsed.inputPaths {
		if inputErr := validateDOCXInputPath(inputPath); inputErr != nil {
			return inputErr
		}
		if protectedErr := engine.DetectProtectedDOCX(inputPath); protectedErr != nil {
			return mapDOCXEngineError(protectedErr)
		}
	}

	if parsed.mode == "single" {
		_, outErr := resolveSingleDOCXOutput(parsed.inputPaths[0], parsed.outputPath, parsed.outputDir)
		if outErr != nil {
			return outErr
		}
	}

	if parsed.mode == "batch" {
		if strings.TrimSpace(parsed.outputDir) == "" {
			return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_DIR_REQUIRED", "outputDir is required in batch mode", nil)
		}
		if outDirErr := validateDOCXOutputDir(parsed.outputDir); outDirErr != nil {
			return outDirErr
		}
	}

	return nil
}

func (t *DOCXToPDFTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	parsed, parseErr := parseDOCXRequest(req)
	if parseErr != nil {
		return models.JobResultItemV1{InputPath: firstPath(req.InputPaths), Success: false, Message: parseErr.Message, Error: parseErr}, parseErr
	}

	inputPath := parsed.inputPaths[0]
	if inputErr := validateDOCXInputPath(inputPath); inputErr != nil {
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: inputErr.Message, Error: inputErr}, inputErr
	}

	if protectedErr := engine.DetectProtectedDOCX(inputPath); protectedErr != nil {
		jobErr := mapDOCXEngineError(protectedErr)
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	outputPath, outErr := resolveSingleDOCXOutput(inputPath, parsed.outputPath, parsed.outputDir)
	if outErr != nil {
		return models.JobResultItemV1{InputPath: inputPath, Success: false, Message: outErr.Message, Error: outErr}, outErr
	}

	result, err := engine.ConvertDOCX(ctx, t.probe, t.runner, engine.ConvertDOCXRequest{InputPath: inputPath, OutputPath: outputPath})
	if err != nil {
		jobErr := mapDOCXEngineError(err)
		return models.JobResultItemV1{InputPath: inputPath, OutputPath: outputPath, Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
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
	parsed, parseErr := parseDOCXRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}

	if strings.TrimSpace(parsed.outputDir) == "" {
		return nil, models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_DIR_REQUIRED", "outputDir is required in batch mode", nil)
	}

	if outDirErr := validateDOCXOutputDir(parsed.outputDir); outDirErr != nil {
		return nil, outDirErr
	}

	usedOutputs := make(map[string]struct{}, len(parsed.inputPaths))
	items := make([]models.JobResultItemV1, 0, len(parsed.inputPaths))
	var firstErr *models.JobErrorV1

	for index, inputPath := range parsed.inputPaths {
		select {
		case <-ctx.Done():
			cancelErr := models.NewCanonicalJobError("DOC_DOCX_TO_PDF_CANCELLED", ctx.Err().Error(), nil)
			return items, cancelErr
		default:
		}

		if inputErr := validateDOCXInputPath(inputPath); inputErr != nil {
			if firstErr == nil {
				firstErr = inputErr
			}
			items = append(items, models.JobResultItemV1{InputPath: inputPath, Success: false, Message: inputErr.Message, Error: inputErr})
			emitDOCXBatchProgress(onProgress, index+1, len(parsed.inputPaths))
			continue
		}

		if protectedErr := engine.DetectProtectedDOCX(inputPath); protectedErr != nil {
			jobErr := mapDOCXEngineError(protectedErr)
			if firstErr == nil {
				firstErr = jobErr
			}
			items = append(items, models.JobResultItemV1{InputPath: inputPath, Success: false, Message: jobErr.Message, Error: jobErr})
			emitDOCXBatchProgress(onProgress, index+1, len(parsed.inputPaths))
			continue
		}

		outputPath := nextAvailableDOCXOutput(parsed.outputDir, inputPath, usedOutputs)
		result, execErr := engine.ConvertDOCX(ctx, t.probe, t.runner, engine.ConvertDOCXRequest{InputPath: inputPath, OutputPath: outputPath})
		if execErr != nil {
			jobErr := mapDOCXEngineError(execErr)
			if firstErr == nil {
				firstErr = jobErr
			}
			items = append(items, models.JobResultItemV1{InputPath: inputPath, OutputPath: outputPath, Success: false, Message: jobErr.Message, Error: jobErr})
		} else {
			message := "docx to pdf successful"
			if result.FallbackUsed {
				message = "docx to pdf successful (fallback engine: pandoc)"
			}
			items = append(items, models.JobResultItemV1{InputPath: inputPath, OutputPath: outputPath, Outputs: []string{outputPath}, OutputCount: 1, Success: true, Message: message})
		}

		emitDOCXBatchProgress(onProgress, index+1, len(parsed.inputPaths))
	}

	return items, firstErr
}

type docxRequest struct {
	mode       string
	inputPaths []string
	outputDir  string
	outputPath string
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

	parsed := docxRequest{
		mode:       mode,
		inputPaths: inputs,
		outputDir:  strings.TrimSpace(req.OutputDir),
		outputPath: strings.TrimSpace(optionString(req.Options, "outputPath")),
	}

	if mode == "single" && parsed.outputDir == "" {
		parsed.outputDir = strings.TrimSpace(optionString(req.Options, "outputDir"))
	}

	return parsed, nil
}

func validateDOCXInputPath(inputPath string) *models.JobErrorV1 {
	info, err := os.Stat(inputPath)
	if err != nil {
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_INPUT_NOT_FOUND", err.Error(), nil)
	}

	if info.IsDir() {
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_INPUT_IS_DIR", "input path must be a .docx file", nil)
	}

	if strings.ToLower(filepath.Ext(inputPath)) != ".docx" {
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_INPUT_UNSUPPORTED", "v1 supports only .docx inputs", nil)
	}

	return nil
}

func validateDOCXOutputDir(dir string) *models.JobErrorV1 {
	if strings.TrimSpace(dir) == "" {
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_DIR_INVALID", "output directory is required", nil)
	}

	info, err := os.Stat(dir)
	if err != nil {
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_DIR_INVALID", err.Error(), nil)
	}

	if !info.IsDir() {
		return models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_DIR_INVALID", "output directory must be a directory", nil)
	}

	return nil
}

func resolveSingleDOCXOutput(inputPath, rawOutputPath, rawOutputDir string) (string, *models.JobErrorV1) {
	outputPath := strings.TrimSpace(rawOutputPath)
	if outputPath != "" {
		if strings.ToLower(filepath.Ext(outputPath)) != ".pdf" {
			return "", models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_INVALID", "outputPath must use .pdf extension", nil)
		}
		if outDirErr := validateDOCXOutputDir(filepath.Dir(outputPath)); outDirErr != nil {
			return "", outDirErr
		}

		if sameDocFile(inputPath, outputPath) {
			return "", models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
		}

		if isDocOutputAvailable(outputPath, nil) {
			return outputPath, nil
		}

		base := strings.TrimSuffix(outputPath, filepath.Ext(outputPath))
		for i := 2; ; i++ {
			candidate := fmt.Sprintf("%s-%d.pdf", base, i)
			if isDocOutputAvailable(candidate, nil) {
				return candidate, nil
			}
		}
	}

	outputDir := strings.TrimSpace(rawOutputDir)
	if outputDir == "" {
		outputDir = filepath.Dir(inputPath)
	}

	if outDirErr := validateDOCXOutputDir(outputDir); outDirErr != nil {
		return "", outDirErr
	}

	candidate := nextAvailableDOCXOutput(outputDir, inputPath, nil)
	if sameDocFile(inputPath, candidate) {
		return "", models.NewCanonicalJobError("DOC_DOCX_TO_PDF_OUTPUT_COLLIDES_INPUT", "output path cannot match input path", nil)
	}

	return candidate, nil
}

func nextAvailableDOCXOutput(outputDir, inputPath string, used map[string]struct{}) string {
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if strings.TrimSpace(base) == "" {
		base = "document"
	}

	prefix := filepath.Join(outputDir, fmt.Sprintf("%s_docx2pdf", base))
	first := prefix + ".pdf"
	if isDocOutputAvailable(first, used) {
		markDocOutputUsed(first, used)
		return first
	}

	for i := 2; ; i++ {
		candidate := fmt.Sprintf("%s-%d.pdf", prefix, i)
		if isDocOutputAvailable(candidate, used) {
			markDocOutputUsed(candidate, used)
			return candidate
		}
	}
}

func isDocOutputAvailable(outputPath string, used map[string]struct{}) bool {
	cleaned := strings.ToLower(filepath.Clean(outputPath))
	if used != nil {
		if _, exists := used[cleaned]; exists {
			return false
		}
	}

	_, err := os.Stat(outputPath)
	return err != nil
}

func markDocOutputUsed(outputPath string, used map[string]struct{}) {
	if used == nil {
		return
	}
	used[strings.ToLower(filepath.Clean(outputPath))] = struct{}{}
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
