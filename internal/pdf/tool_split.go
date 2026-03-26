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

const ToolIDPDFSplitV1 = "tool.pdf.split"

type SplitTool struct{}

func NewSplitTool() *SplitTool {
	return &SplitTool{}
}

func (t *SplitTool) ID() string {
	return ToolIDPDFSplitV1
}

func (t *SplitTool) Capability() string {
	return ToolIDPDFSplitV1
}

func (t *SplitTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "PDF Split",
		Description:      "Split one PDF into individual page PDF files",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{"pdfcpu"},
		Tags:             []string{"pdf", "split", "pages"},
	}
}

func (t *SplitTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *SplitTool) Validate(_ context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	parsed, validationErr := splitReqFields(req)
	if validationErr != nil {
		return validationErr
	}

	if parsed.Mode == "single" {
		if splitErr := engine.ValidateSplitRequest(parsed.InputPath, parsed.OutputDir, parsed.Strategy, parsed.RangesExpr); splitErr != nil {
			return mapSplitError(splitErr)
		}

		return nil
	}

	if splitErr := engine.ValidateSplitBatchRequest(parsed.InputPaths, parsed.OutputDir, parsed.Strategy, parsed.RangesExpr, parsed.PerInputDir); splitErr != nil {
		return mapSplitError(splitErr)
	}

	return nil
}

func (t *SplitTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	parsed, validationErr := splitReqFields(req)
	if validationErr != nil {
		return models.JobResultItemV1{InputPath: parsed.InputPath, OutputPath: parsed.OutputDir, Success: false, Message: validationErr.Message, Error: validationErr}, validationErr
	}

	if parsed.Mode != "single" {
		jobErr := &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "mode must be single"}
		return models.JobResultItemV1{InputPath: parsed.InputPath, OutputPath: parsed.OutputDir, Success: false, Message: jobErr.Message, Error: jobErr}, jobErr
	}

	if splitErr := engine.ValidateSplitRequest(parsed.InputPath, parsed.OutputDir, parsed.Strategy, parsed.RangesExpr); splitErr != nil {
		jobErr := mapSplitError(splitErr)
		return models.JobResultItemV1{
			InputPath:  parsed.InputPath,
			OutputPath: parsed.OutputDir,
			Success:    false,
			Message:    jobErr.Message,
			Error:      jobErr,
		}, jobErr
	}

	outputs, err := engine.Split(ctx, parsed.InputPath, parsed.OutputDir, parsed.Strategy, parsed.RangesExpr)
	if err != nil {
		jobErr := mapSplitError(err)
		return models.JobResultItemV1{
			InputPath:  parsed.InputPath,
			OutputPath: parsed.OutputDir,
			Success:    false,
			Message:    jobErr.Message,
			Error:      jobErr,
		}, jobErr
	}

	return models.JobResultItemV1{
		InputPath:   parsed.InputPath,
		OutputPath:  parsed.OutputDir,
		Outputs:     append([]string(nil), outputs...),
		OutputCount: len(outputs),
		Success:     true,
		Message:     fmt.Sprintf("PDF split successful: generated %d files", len(outputs)),
	}, nil
}

func (t *SplitTool) ExecuteBatch(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	parsed, validationErr := splitReqFields(req)
	if validationErr != nil {
		return nil, validationErr
	}

	if parsed.Mode != "batch" {
		return nil, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "mode must be batch"}
	}

	if splitErr := engine.ValidateSplitBatchRequest(parsed.InputPaths, parsed.OutputDir, parsed.Strategy, parsed.RangesExpr, parsed.PerInputDir); splitErr != nil {
		return nil, mapSplitError(splitErr)
	}

	batchResults, err := engine.SplitBatch(ctx, parsed.InputPaths, parsed.OutputDir, parsed.Strategy, parsed.RangesExpr, parsed.PerInputDir)
	if err != nil {
		return nil, mapSplitError(err)
	}

	items := make([]models.JobResultItemV1, 0, len(batchResults))
	for i, result := range batchResults {
		items = append(items, models.JobResultItemV1{
			InputPath:   result.InputPath,
			OutputPath:  result.OutputDir,
			Outputs:     append([]string(nil), result.Outputs...),
			OutputCount: len(result.Outputs),
			Success:     true,
			Message:     fmt.Sprintf("PDF split successful: generated %d files", len(result.Outputs)),
		})

		if onProgress != nil {
			onProgress(models.JobProgressV1{
				Current: i + 1,
				Total:   len(batchResults),
				Stage:   "running",
				Message: fmt.Sprintf("processed %d/%d", i+1, len(batchResults)),
			})
		}
	}

	return items, nil
}

type splitRequestFields struct {
	Mode        string
	InputPath   string
	InputPaths  []string
	OutputDir   string
	Strategy    string
	RangesExpr  string
	PerInputDir bool
}

func splitReqFields(req models.JobRequestV1) (splitRequestFields, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return splitRequestFields{}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "mode must be single or batch"}
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return splitRequestFields{}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "exactly 1 input PDF is required"}
	}

	if mode == "batch" && len(req.InputPaths) < 1 {
		return splitRequestFields{}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "at least 1 input PDF is required"}
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawInput := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return splitRequestFields{}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "inputPath is required"}
		}
		inputPaths = append(inputPaths, inputPath)
	}

	outputDir := strings.TrimSpace(optionString(req.Options, "outputDir"))
	if outputDir == "" {
		return splitRequestFields{Mode: mode, InputPath: firstInputPath(inputPaths), InputPaths: inputPaths}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "options.outputDir is required"}
	}

	strategy := strings.TrimSpace(optionString(req.Options, "strategy"))
	if strategy == "" {
		strategy = engine.SplitStrategyEveryPage
	}

	rangesExpr := strings.TrimSpace(optionString(req.Options, "ranges"))
	if strategy == engine.SplitStrategyRanges && rangesExpr == "" {
		return splitRequestFields{Mode: mode, InputPath: firstInputPath(inputPaths), InputPaths: inputPaths, OutputDir: filepath.Clean(outputDir), Strategy: strategy}, &models.JobErrorV1{Code: engine.ErrorCodeSplitRangesRequired, Message: "options.ranges is required for strategy=ranges"}
	}

	return splitRequestFields{
		Mode:        mode,
		InputPath:   firstInputPath(inputPaths),
		InputPaths:  inputPaths,
		OutputDir:   filepath.Clean(outputDir),
		Strategy:    strategy,
		RangesExpr:  rangesExpr,
		PerInputDir: optionBool(req.Options, "perInputDir", mode == "batch"),
	}, nil
}

func optionString(options map[string]any, key string) string {
	if options == nil {
		return ""
	}

	v, ok := options[key].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(v)
}

func optionBool(options map[string]any, key string, fallback bool) bool {
	if options == nil {
		return fallback
	}

	v, ok := options[key].(bool)
	if !ok {
		return fallback
	}

	return v
}

func firstInputPath(inputPaths []string) string {
	if len(inputPaths) == 0 {
		return ""
	}

	return inputPaths[0]
}

func mapSplitError(err error) *models.JobErrorV1 {
	var splitErr *engine.SplitError
	if !errors.As(err, &splitErr) {
		return &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	return &models.JobErrorV1{Code: splitErr.Code, Message: splitErr.Message}
}
