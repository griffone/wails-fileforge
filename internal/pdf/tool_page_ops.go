package pdf

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/pdf/engine"
	"fileforge-desktop/internal/utils"
)

// Shared plumbing for the PDF page operations (rotate, remove pages, reorder
// and extract images). They follow the crop/split contract: plan first, resolve
// collision-safe destinations, and execute exactly the plan.

const (
	pageOpOutputKindFile      = "file"
	pageOpOutputKindDirectory = "directory"

	// pageOpOptionIntegerLimit bounds an integer option before it is converted.
	pageOpOptionIntegerLimit = 1 << 20
)

type pageOpRequestFields struct {
	Mode       string
	InputPath  string
	InputPaths []string
	OutputDir  string
}

func pageOpReqFields(req models.JobRequest) (pageOpRequestFields, *models.JobError) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return pageOpRequestFields{}, pageOpRequestError(engine.ErrorCodeValidation, "mode must be single or batch")
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return pageOpRequestFields{Mode: mode}, pageOpRequestError(engine.ErrorCodeValidation, "exactly 1 input PDF is required")
	}
	if mode == "batch" && len(req.InputPaths) < 1 {
		return pageOpRequestFields{Mode: mode}, pageOpRequestError(engine.ErrorCodeValidation, "at least 1 input PDF is required")
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawInput := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return pageOpRequestFields{Mode: mode}, pageOpRequestError(engine.ErrorCodeValidation, "inputPath is required")
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

	return pageOpRequestFields{
		Mode:       mode,
		InputPath:  firstInputPath(inputPaths),
		InputPaths: inputPaths,
		OutputDir:  outputDir,
	}, nil
}

// planPageOpFileOutput resolves one collision-safe <stem><suffix>.pdf.
func planPageOpFileOutput(inputPath, dir, suffix string, used map[string]struct{}) models.PlannedOutput {
	planned := models.PlannedOutput{
		InputPath:   inputPath,
		OutputKind:  pageOpOutputKindFile,
		OutputCount: 1,
	}

	base := pageOpOutputBaseName(inputPath, suffix)
	outputPath, adjusted := outputplan.UniqueFilePath(dir, base, "pdf", used)
	planned.OutputPath = outputPath

	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", base+".pdf", filepath.Base(outputPath)))
	}

	return planned
}

// planPageOpDirectoryOutput resolves one collision-safe <stem><suffix> folder.
// The folder is created at execution time; the count stays unknown.
func planPageOpDirectoryOutput(inputPath, dir, suffix string, used map[string]struct{}) models.PlannedOutput {
	planned := models.PlannedOutput{
		InputPath:  inputPath,
		OutputKind: pageOpOutputKindDirectory,
	}

	base := pageOpOutputBaseName(inputPath, suffix)
	outputPath, adjusted := outputplan.UniqueDirPath(dir, base, used)
	planned.OutputPath = outputPath

	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving to %s.", base, filepath.Base(outputPath)))
	}

	return planned
}

func pageOpOutputBaseName(inputPath, suffix string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		stem = "document"
	}

	return stem + suffix
}

func pageOpPlannedOutputPath(plan models.PlanJobResponse) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
}

func pageOpFailureItem(inputPath, outputPath string, jobErr *models.JobError) models.JobResultItem {
	return models.JobResultItem{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Success:    false,
		Message:    jobErr.Message,
		Error:      jobErr,
	}
}

func pageOpRequestError(code, message string) *models.JobError {
	return &models.JobError{Code: code, DetailCode: code, Message: message}
}

func mapPageOpError(err error) *models.JobError {
	var pageErr *engine.PageOpError
	if !errors.As(err, &pageErr) {
		return &models.JobError{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	return &models.JobError{
		Code:       pageErr.Code,
		DetailCode: pageErr.Code,
		Message:    pageErr.Message,
		Details:    pageErr.Details,
	}
}

// pageOpIntOption reads a whole-number option. Absent or null returns
// (0, false, nil); a malformed value is rejected instead of being coerced.
func pageOpIntOption(options map[string]any, key string) (int, bool, *models.JobError) {
	if options == nil {
		return 0, false, nil
	}

	raw, ok := options[key]
	if !ok || raw == nil {
		return 0, false, nil
	}

	var value float64
	switch typed := raw.(type) {
	case int:
		value = float64(typed)
	case int8:
		value = float64(typed)
	case int16:
		value = float64(typed)
	case int32:
		value = float64(typed)
	case int64:
		value = float64(typed)
	case uint:
		value = float64(typed)
	case uint8:
		value = float64(typed)
	case uint16:
		value = float64(typed)
	case uint32:
		value = float64(typed)
	case uint64:
		value = float64(typed)
	case float32:
		value = float64(typed)
	case float64:
		value = typed
	case json.Number:
		parsed, err := typed.Float64()
		if err != nil {
			return 0, true, pageOpRequestError(engine.ErrorCodeValidation, fmt.Sprintf("options.%s must be a number", key))
		}
		value = parsed
	default:
		return 0, true, pageOpRequestError(engine.ErrorCodeValidation, fmt.Sprintf("options.%s must be a number", key))
	}

	if math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) || value < -pageOpOptionIntegerLimit || value > pageOpOptionIntegerLimit {
		return 0, true, pageOpRequestError(engine.ErrorCodeValidation, fmt.Sprintf("options.%s must be a whole number", key))
	}

	return int(value), true, nil
}

// executePageOpFilePlan runs a file-producing page operation over exactly the
// planned outputs. Results keep plan order and per-item failures never stop the
// remaining items.
func executePageOpFilePlan(
	ctx context.Context,
	plan models.PlanJobResponse,
	onProgress func(models.JobProgress),
	aggregateCode string,
	successMessage string,
	run func(context.Context, models.PlannedOutput) error,
) ([]models.JobResultItem, *models.JobError) {
	items := make([]models.JobResultItem, len(plan.Outputs))
	itemErrors := make([]*models.JobError, len(plan.Outputs))
	completed := make([]bool, len(plan.Outputs))
	progress := newPDFBatchProgress(len(plan.Outputs), onProgress)

	runErr := utils.ParallelForIndex(ctx, len(plan.Outputs), pdfBatchWorkerCount(), func(ctx context.Context, i int) error {
		planned := plan.Outputs[i]
		item := models.JobResultItem{
			InputPath:  planned.InputPath,
			OutputPath: planned.OutputPath,
		}

		if err := run(ctx, planned); err != nil {
			jobErr := mapPageOpError(err)
			item.Success = false
			item.Message = jobErr.Message
			item.Error = jobErr
			itemErrors[i] = jobErr
		} else {
			item.Success = true
			item.Message = successMessage
			item.Outputs = []string{planned.OutputPath}
			item.OutputCount = 1
		}

		items[i] = item
		completed[i] = true
		progress.itemCompleted()

		return nil
	})

	if runErr != nil {
		if ctx.Err() != nil {
			return completedPDFBatchItems(items, completed), &models.JobError{Code: "CANCELED", Message: ctx.Err().Error()}
		}

		return completedPDFBatchItems(items, completed), models.NewCanonicalJobError(aggregateCode, "batch page operation failed", map[string]any{"cause": runErr.Error()})
	}

	fileErrors := make([]map[string]any, 0)
	for i, jobErr := range itemErrors {
		if jobErr == nil {
			continue
		}
		fileErrors = append(fileErrors, map[string]any{
			"path":    plan.Outputs[i].InputPath,
			"code":    jobErr.DetailCode,
			"message": jobErr.Message,
		})
	}

	if len(fileErrors) > 0 {
		return items, models.NewCanonicalJobError(aggregateCode, "one or more files failed in the batch", map[string]any{
			"fileErrors": fileErrors,
		})
	}

	return items, nil
}
