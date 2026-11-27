package pdf

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/pdf/engine"
)

const (
	// ToolIDPDFRemovePages deletes selected pages from a PDF.
	ToolIDPDFRemovePages = "tool.pdf.remove_pages"

	removePagesOutputSuffix = "_pages_removed"

	removePagesOptionPageSelection = "pageSelection"
)

// RemovePagesTool deletes the selected pages and keeps the rest.
type RemovePagesTool struct{}

func NewRemovePagesTool() *RemovePagesTool {
	return &RemovePagesTool{}
}

func (t *RemovePagesTool) ID() string {
	return ToolIDPDFRemovePages
}

func (t *RemovePagesTool) Capability() string {
	return ToolIDPDFRemovePages
}

func (t *RemovePagesTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "PDF Remove Pages",
		Description:      "Delete selected pages and keep the rest of the document",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{"pdfcpu"},
		Tags:             []string{"pdf", "pages", "delete", "remove"},
		ResourceClass:    "cpu",
	}
}

func (t *RemovePagesTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *RemovePagesTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe <stem>_pages_removed.pdf per input without
// writing anything. Removing every page is refused before execution.
func (t *RemovePagesTool) Plan(_ context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	parsed, parseErr := removePagesReqFields(req)
	if parseErr != nil {
		return planFailure(plan, parseErr)
	}

	if optionErr := engine.ValidateRemovePagesOptions(parsed.PageSelection, 0); optionErr != nil {
		return planFailure(plan, mapPageOpError(optionErr))
	}

	for _, inputPath := range parsed.InputPaths {
		if inputErr := engine.ValidatePageOpInput(inputPath); inputErr != nil {
			return planFailure(plan, mapPageOpError(inputErr))
		}
	}

	// Validate the selection against the real page count and refuse a removal
	// that would empty the document. An unreadable input keeps the plan alive so
	// its per-item execution reports the canonical failure inside a batch.
	for _, inputPath := range parsed.InputPaths {
		pageCount, countErr := engine.PageOpPageCount(inputPath)
		if countErr != nil {
			continue
		}
		if optionErr := engine.ValidateRemovePagesOptions(parsed.PageSelection, pageCount); optionErr != nil {
			return planFailure(plan, mapPageOpError(optionErr))
		}
	}

	if parsed.OutputDir != "" {
		if destErr := engine.ValidatePageOpDestination(parsed.OutputDir); destErr != nil {
			return planFailure(plan, mapPageOpError(destErr))
		}
	}

	used := make(map[string]struct{}, len(parsed.InputPaths))
	for _, inputPath := range parsed.InputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	outputs := make([]models.PlannedOutput, 0, len(parsed.InputPaths))
	warnings := make([]string, 0)
	for _, inputPath := range parsed.InputPaths {
		targetDir := parsed.OutputDir
		if targetDir == "" {
			targetDir = filepath.Dir(inputPath)
		}

		planned := planPageOpFileOutput(inputPath, filepath.Clean(targetDir), removePagesOutputSuffix, used)
		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = removePagesPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = map[string]any{removePagesOptionPageSelection: parsed.PageSelection}

	return plan, nil
}

func (t *RemovePagesTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	parsed, parseErr := removePagesReqFields(req)
	if parseErr != nil {
		return pageOpFailureItem(firstInputPath(req.InputPaths), "", parseErr), parseErr
	}

	if parsed.Mode != "single" {
		jobErr := pageOpRequestError(engine.ErrorCodeValidation, "mode must be single")
		return pageOpFailureItem(parsed.InputPath, "", jobErr), jobErr
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return pageOpFailureItem(parsed.InputPath, pageOpPlannedOutputPath(plan), planErr), planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, nil)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("PDF_REMOVE_PAGES_EXECUTION_MISSING_RESULT", "remove pages execution returned no result", nil)
	}

	return pageOpFailureItem(parsed.InputPath, pageOpPlannedOutputPath(plan), jobErr), jobErr
}

func (t *RemovePagesTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := removePagesReqFields(req)
	if parseErr != nil {
		return nil, parseErr
	}

	if parsed.Mode != "batch" {
		return nil, pageOpRequestError(engine.ErrorCodeValidation, "mode must be batch")
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan removes pages from exactly the outputs resolved by Plan.
func (t *RemovePagesTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := removePagesReqFields(req)
	if parseErr != nil {
		return nil, parseErr
	}

	pageSelection := stringOption(plan.EffectiveOptions, removePagesOptionPageSelection)
	if pageSelection == "" {
		pageSelection = parsed.PageSelection
	}

	return executePageOpFilePlan(ctx, plan, onProgress, engine.ErrorCodeRemovePagesFailed, "PDF pages removed successfully", func(ctx context.Context, planned models.PlannedOutput) error {
		return engine.RemovePages(ctx, planned.InputPath, planned.OutputPath, pageSelection)
	})
}

type removePagesRequestFields struct {
	Mode          string
	InputPath     string
	InputPaths    []string
	OutputDir     string
	PageSelection string
}

func removePagesReqFields(req models.JobRequest) (removePagesRequestFields, *models.JobError) {
	base, parseErr := pageOpReqFields(req)
	if parseErr != nil {
		return removePagesRequestFields{}, parseErr
	}

	pageSelection := strings.TrimSpace(optionString(req.Options, removePagesOptionPageSelection))

	return removePagesRequestFields{
		Mode:          base.Mode,
		InputPath:     base.InputPath,
		InputPaths:    base.InputPaths,
		OutputDir:     base.OutputDir,
		PageSelection: pageSelection,
	}, nil
}

func removePagesPlanMessage(count int) string {
	if count == 1 {
		return "Ready to remove pages from 1 PDF."
	}

	return fmt.Sprintf("Ready to remove pages from %d PDFs.", count)
}
