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
	ToolIDPDFSplitV1 = "tool.pdf.split"

	splitOutputKindDirectory = "directory"
	splitDirSuffix           = "_pages"
)

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

func (t *SplitTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe <stem>_pages directory per input without
// creating or writing anything. The directory is created when the job runs.
func (t *SplitTool) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	parsed, validationErr := splitReqFields(req)
	if validationErr != nil {
		return planFailure(plan, validationErr)
	}

	used := make(map[string]struct{}, len(parsed.InputPaths))
	outputs := make([]models.PlannedOutputV1, 0, len(parsed.InputPaths))
	warnings := make([]string, 0)
	for _, inputPath := range parsed.InputPaths {
		planned, planErr := planSplitDirectory(inputPath, parsed.BaseDir, parsed.Strategy, parsed.RangesExpr, used)
		if planErr != nil {
			if planned.OutputPath != "" {
				outputs = append(outputs, planned)
				plan.Outputs = outputs
			}
			return planFailure(plan, planErr)
		}

		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = splitPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = splitEffectiveOptions(parsed)

	return plan, nil
}

func (t *SplitTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	parsed, validationErr := splitReqFields(req)
	if validationErr != nil {
		return splitFailureItem(parsed.InputPath, "", validationErr), validationErr
	}

	if parsed.Mode != "single" {
		jobErr := &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "mode must be single"}
		return splitFailureItem(parsed.InputPath, "", jobErr), jobErr
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return splitFailureItem(parsed.InputPath, splitPlannedOutputPath(plan), planErr), planErr
	}

	planned := plan.Outputs[0]
	outputs, err := engine.Split(ctx, planned.InputPath, planned.OutputPath, parsed.Strategy, parsed.RangesExpr)
	if err != nil {
		jobErr := mapSplitError(err)
		return splitFailureItem(planned.InputPath, planned.OutputPath, jobErr), jobErr
	}

	return models.JobResultItemV1{
		InputPath:   planned.InputPath,
		OutputPath:  planned.OutputPath,
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

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	fileErrors := make([]map[string]any, 0)
	items := make([]models.JobResultItemV1, 0, len(plan.Outputs))
	for i, planned := range plan.Outputs {
		select {
		case <-ctx.Done():
			return items, &models.JobErrorV1{Code: "CANCELED", Message: ctx.Err().Error()}
		default:
		}

		item := models.JobResultItemV1{
			InputPath:  planned.InputPath,
			OutputPath: planned.OutputPath,
		}

		outputs, err := engine.Split(ctx, planned.InputPath, planned.OutputPath, parsed.Strategy, parsed.RangesExpr)
		if err != nil {
			jobErr := mapSplitError(err)
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
			item.Outputs = append([]string(nil), outputs...)
			item.OutputCount = len(outputs)
			item.Message = fmt.Sprintf("PDF split successful: generated %d files", len(outputs))
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
		return items, models.NewCanonicalJobError(engine.ErrorCodeSplitFailed, "one or more files failed in batch split", map[string]any{
			"fileErrors": fileErrors,
		})
	}

	return items, nil
}

type splitRequestFields struct {
	Mode       string
	InputPath  string
	InputPaths []string
	BaseDir    string
	Strategy   string
	RangesExpr string
}

func splitReqFields(req models.JobRequestV1) (splitRequestFields, *models.JobErrorV1) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return splitRequestFields{}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "mode must be single or batch"}
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return splitRequestFields{Mode: mode}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "exactly 1 input PDF is required"}
	}

	if mode == "batch" && len(req.InputPaths) < 1 {
		return splitRequestFields{Mode: mode}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "at least 1 input PDF is required"}
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawInput := range req.InputPaths {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return splitRequestFields{Mode: mode}, &models.JobErrorV1{Code: engine.ErrorCodeValidation, Message: "inputPath is required"}
		}
		inputPaths = append(inputPaths, inputPath)
	}

	// The chosen folder is the base for <stem>_pages directories. It is
	// optional: without it every input is split next to itself.
	baseDir := strings.TrimSpace(req.OutputDir)
	if baseDir == "" {
		baseDir = optionString(req.Options, "outputDir")
	}
	if baseDir != "" {
		baseDir = filepath.Clean(baseDir)
	}

	strategy := strings.TrimSpace(optionString(req.Options, "strategy"))
	if strategy == "" {
		strategy = engine.SplitStrategyEveryPage
	}
	if strategy != engine.SplitStrategyEveryPage && strategy != engine.SplitStrategyRanges {
		return splitRequestFields{Mode: mode, InputPath: firstInputPath(inputPaths), InputPaths: inputPaths, BaseDir: baseDir, Strategy: strategy}, &models.JobErrorV1{
			Code:       engine.ErrorCodeUnsupportedStrategy,
			DetailCode: engine.ErrorCodeUnsupportedStrategy,
			Message:    fmt.Sprintf("unsupported split strategy: %s", strategy),
		}
	}

	rangesExpr := strings.TrimSpace(optionString(req.Options, "ranges"))
	if strategy == engine.SplitStrategyRanges {
		if rangesExpr == "" {
			return splitRequestFields{Mode: mode, InputPath: firstInputPath(inputPaths), InputPaths: inputPaths, BaseDir: baseDir, Strategy: strategy}, &models.JobErrorV1{Code: engine.ErrorCodeSplitRangesRequired, Message: "options.ranges is required for strategy=ranges"}
		}

		if _, rangesErr := engine.ParseSplitRanges(rangesExpr); rangesErr != nil {
			return splitRequestFields{Mode: mode, InputPath: firstInputPath(inputPaths), InputPaths: inputPaths, BaseDir: baseDir, Strategy: strategy}, mapSplitError(rangesErr)
		}
	}

	return splitRequestFields{
		Mode:       mode,
		InputPath:  firstInputPath(inputPaths),
		InputPaths: inputPaths,
		BaseDir:    baseDir,
		Strategy:   strategy,
		RangesExpr: rangesExpr,
	}, nil
}

// planSplitDirectory resolves one collision-safe output directory. It never
// creates anything: UniqueDirPath only probes the filesystem.
func planSplitDirectory(inputPath, baseDir, strategy, rangesExpr string, used map[string]struct{}) (models.PlannedOutputV1, *models.JobErrorV1) {
	planned := models.PlannedOutputV1{
		InputPath:  inputPath,
		OutputKind: splitOutputKindDirectory,
	}

	if splitErr := engine.ValidateSplitInput(inputPath); splitErr != nil {
		return planned, mapSplitError(splitErr)
	}

	dir := strings.TrimSpace(baseDir)
	if dir == "" {
		dir = filepath.Dir(inputPath)
	}
	dir = filepath.Clean(dir)

	if splitErr := engine.ValidateSplitDestination(dir); splitErr != nil {
		return planned, mapSplitError(splitErr)
	}

	name := splitDirectoryName(inputPath)
	outputPath, adjusted := outputplan.UniqueDirPath(dir, name, used)
	planned.OutputPath = outputPath

	if adjusted {
		planned.Adjusted = true
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving to %s.", name, filepath.Base(outputPath)))
	}

	// A directory plan cannot always know its file count (for example when the
	// PDF is unreadable or page counts are unavailable). Leave 0 in that case.
	planned.OutputCount = splitPlannedOutputCount(inputPath, strategy, rangesExpr)

	return planned, nil
}

func splitDirectoryName(inputPath string) string {
	stem := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	stem = strings.TrimSpace(stem)
	if stem == "" {
		stem = "document"
	}

	return stem + splitDirSuffix
}

func splitPlannedOutputCount(inputPath, strategy, rangesExpr string) int {
	switch strategy {
	case engine.SplitStrategyEveryPage:
		return engine.SplitPageCount(inputPath)
	case engine.SplitStrategyRanges:
		ranges, rangesErr := engine.ParseSplitRanges(rangesExpr)
		if rangesErr != nil {
			return 0
		}
		return len(ranges)
	default:
		return 0
	}
}

func splitEffectiveOptions(parsed splitRequestFields) map[string]any {
	options := map[string]any{"strategy": parsed.Strategy}
	if parsed.Strategy == engine.SplitStrategyRanges {
		options["ranges"] = parsed.RangesExpr
	}

	return options
}

func splitPlanMessage(count int) string {
	if count == 1 {
		return "Ready to split 1 PDF."
	}

	return fmt.Sprintf("Ready to split %d PDFs.", count)
}

func splitFailureItem(inputPath, outputPath string, jobErr *models.JobErrorV1) models.JobResultItemV1 {
	return models.JobResultItemV1{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Success:    false,
		Message:    jobErr.Message,
		Error:      jobErr,
	}
}

func splitPlannedOutputPath(plan models.PlanJobResponseV1) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
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

	return &models.JobErrorV1{Code: splitErr.Code, DetailCode: splitErr.Code, Message: splitErr.Message}
}
