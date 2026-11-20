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
	ToolIDPDFMerge = "tool.pdf.merge"

	mergeInputPageRangesOption = "inputPageRanges"
)

type MergeTool struct{}

func NewMergeTool() *MergeTool {
	return &MergeTool{}
}

func (t *MergeTool) ID() string {
	return ToolIDPDFMerge
}

func (t *MergeTool) Capability() string {
	return "tool.pdf.merge"
}

func (t *MergeTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "PDF Merge",
		Description:      "Merge multiple PDF files into a single output PDF",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    false,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{"pdfcpu"},
		Tags:             []string{"pdf", "merge", "documents"},
		ResourceClass:    "cpu",
		MaxWorkers:       1,
	}
}

func (t *MergeTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *MergeTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves the merge destination without writing anything. An explicit
// outputPath keeps the existing contract (it must not exist); otherwise the
// output defaults to <firstStem>_merged.pdf next to the first input.
func (t *MergeTool) Plan(_ context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	if req.Mode != "single" {
		return planFailure(plan, &models.JobError{Code: "VALIDATION_ERROR", Message: "mode must be single"})
	}

	pageRanges, rangesErr := parseMergeInputPageRanges(req)
	if rangesErr != nil {
		return planFailure(plan, rangesErr)
	}

	outputPath, naturalPath, adjusted := resolveMergeOutputPath(req.InputPaths, req.OutputDir, req.Options)

	if mergeErr := engine.ValidateMergePaths(req.InputPaths, outputPath); mergeErr != nil {
		return planFailure(plan, mapMergeError(mergeErr))
	}

	if pageRanges != nil {
		if rangeErr := engine.ValidateMergePageRanges(req.InputPaths, pageRanges); rangeErr != nil {
			return planFailure(plan, mapMergeError(rangeErr))
		}

		plan.EffectiveOptions = map[string]any{mergeInputPageRangesOption: pageRanges}
	}

	planned := models.PlannedOutput{
		InputPath:  strings.Join(req.InputPaths, ","),
		OutputPath: outputPath,
		OutputKind: "file",
		Adjusted:   adjusted,
	}
	if adjusted {
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", filepath.Base(naturalPath), filepath.Base(outputPath)))
	}

	plan.Success = true
	plan.Message = fmt.Sprintf("Ready to merge %d PDFs.", len(req.InputPaths))
	plan.Outputs = []models.PlannedOutput{planned}

	return plan, nil
}

// ExecuteSingle keeps the legacy single-executor contract by resolving the plan
// first and then running that exact plan, so it cannot drift from ExecutePlan.
func (t *MergeTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	inputPath := strings.Join(req.InputPaths, ",")

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return mergeFailureItem(inputPath, plannedOutputPath(plan), planErr), planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, nil)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("PDF_MERGE_EXECUTION_MISSING_RESULT", "merge execution returned no result", nil)
	}

	return mergeFailureItem(inputPath, plannedOutputPath(plan), jobErr), jobErr
}

// ExecutePlan merges exactly the resolved output and page ranges. It never
// re-plans or re-resolves paths from req.Options: the engine owns planning.
func (t *MergeTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, _ func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	inputPath := strings.Join(req.InputPaths, ",")
	outputPath := plannedOutputPath(plan)
	pageRanges := plannedMergePageRanges(plan)

	err := engine.MergeWithPageRanges(ctx, req.InputPaths, outputPath, pageRanges)
	if err != nil {
		jobErr := mapMergeError(err)
		return []models.JobResultItem{mergeFailureItem(inputPath, outputPath, jobErr)}, jobErr
	}

	return []models.JobResultItem{{
		InputPath:   inputPath,
		OutputPath:  outputPath,
		Outputs:     mergeOutputs(outputPath),
		OutputCount: mergeOutputCount(outputPath),
		Success:     true,
		Message:     "PDF merge successful",
	}}, nil
}

// plannedOutputPath returns the destination resolved by Plan.
func plannedOutputPath(plan models.PlanJobResponse) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
}

// plannedMergePageRanges returns the page ranges stored in the plan. A missing
// or mistyped value keeps the whole file for every input.
func plannedMergePageRanges(plan models.PlanJobResponse) []string {
	if plan.EffectiveOptions == nil {
		return nil
	}

	ranges, ok := plan.EffectiveOptions[mergeInputPageRangesOption].([]string)
	if !ok {
		return nil
	}

	return ranges
}

func mergeFailureItem(inputPath, outputPath string, jobErr *models.JobError) models.JobResultItem {
	return models.JobResultItem{
		InputPath:   inputPath,
		OutputPath:  outputPath,
		Outputs:     mergeOutputs(outputPath),
		OutputCount: mergeOutputCount(outputPath),
		Success:     false,
		Message:     jobErr.Message,
		Error:       jobErr,
	}
}

// parseMergeInputPageRanges reads the optional per-input page ranges. A nil
// result means the option was not supplied. When supplied, the list must have
// exactly one entry per input; empty entries keep the whole file.
func parseMergeInputPageRanges(req models.JobRequest) ([]string, *models.JobError) {
	if req.Options == nil {
		return nil, nil
	}

	rawRanges, exists := req.Options[mergeInputPageRangesOption]
	if !exists || rawRanges == nil {
		return nil, nil
	}

	var ranges []string
	switch typedRanges := rawRanges.(type) {
	case []string:
		ranges = append([]string(nil), typedRanges...)
	case []any:
		ranges = make([]string, 0, len(typedRanges))
		for index, rawRange := range typedRanges {
			value, ok := rawRange.(string)
			if !ok {
				return nil, invalidMergePageRangesError(fmt.Sprintf("options.%s[%d] must be a string", mergeInputPageRangesOption, index))
			}
			ranges = append(ranges, value)
		}
	default:
		return nil, invalidMergePageRangesError(fmt.Sprintf("options.%s must be an array of strings", mergeInputPageRangesOption))
	}

	if len(ranges) != len(req.InputPaths) {
		return nil, invalidMergePageRangesError(fmt.Sprintf("options.%s must contain one entry per input file (%d), got %d", mergeInputPageRangesOption, len(req.InputPaths), len(ranges)))
	}

	for index := range ranges {
		ranges[index] = strings.TrimSpace(ranges[index])
	}

	return ranges, nil
}

func invalidMergePageRangesError(message string) *models.JobError {
	return &models.JobError{
		Code:       engine.ErrorCodeMergePageRangeInvalid,
		DetailCode: engine.ErrorCodeMergePageRangeInvalid,
		Message:    message,
	}
}

// resolveMergeOutputPath returns the resolved output path, the natural
// (unsuffixed) auto path and whether the auto path had to be adjusted.
func resolveMergeOutputPath(inputPaths []string, outputDir string, options map[string]any) (string, string, bool) {
	if explicit := outputPathFromOptions(options); explicit != "" {
		return explicit, explicit, false
	}

	if len(inputPaths) == 0 {
		return "", "", false
	}

	first := strings.TrimSpace(inputPaths[0])
	if first == "" {
		return "", "", false
	}

	dir := strings.TrimSpace(outputDir)
	if dir == "" {
		dir = filepath.Dir(first)
	}
	stem := strings.TrimSuffix(filepath.Base(first), filepath.Ext(first))
	if strings.TrimSpace(stem) == "" {
		stem = "document"
	}

	base := stem + "_merged"
	naturalPath := filepath.Join(dir, base+".pdf")
	outputPath, adjusted := outputplan.UniqueFilePath(dir, base, "pdf", map[string]struct{}{})

	return outputPath, naturalPath, adjusted
}

func outputPathFromOptions(options map[string]any) string {
	if options == nil {
		return ""
	}

	outputPath, ok := options["outputPath"].(string)
	if !ok {
		return ""
	}

	return strings.TrimSpace(outputPath)
}

func mergeOutputs(outputPath string) []string {
	if outputPath == "" {
		return nil
	}
	return []string{outputPath}
}

func mergeOutputCount(outputPath string) int {
	if outputPath == "" {
		return 0
	}
	return 1
}

func planFailure(plan models.PlanJobResponse, jobErr *models.JobError) (models.PlanJobResponse, *models.JobError) {
	plan.Success = false
	plan.Error = jobErr
	if jobErr != nil && jobErr.Message != "" {
		plan.Message = jobErr.Message
	}
	return plan, jobErr
}

func mapMergeError(err error) *models.JobError {
	var mergeErr *engine.MergeError
	if !errors.As(err, &mergeErr) {
		return &models.JobError{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	return &models.JobError{
		Code:       mergeErr.Code,
		DetailCode: mergeErr.Code,
		Message:    mergeErr.Message,
		Details:    mergeErr.Details,
	}
}
