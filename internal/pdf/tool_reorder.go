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
	// ToolIDPDFReorder rewrites a PDF with an explicit page order.
	ToolIDPDFReorder = "tool.pdf.reorder"

	reorderOutputSuffix = "_reordered"

	reorderOptionOrder = "order"
)

// ReorderTool rewrites the page order from an explicit spec that must cover
// every page exactly once.
type ReorderTool struct{}

func NewReorderTool() *ReorderTool {
	return &ReorderTool{}
}

func (t *ReorderTool) ID() string {
	return ToolIDPDFReorder
}

func (t *ReorderTool) Capability() string {
	return ToolIDPDFReorder
}

func (t *ReorderTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "PDF Reorder Pages",
		Description:      "Rewrite the page order from an explicit spec, or reverse the document",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{"pdfcpu"},
		Tags:             []string{"pdf", "reorder", "pages", "order"},
		ResourceClass:    "cpu",
	}
}

func (t *ReorderTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *ReorderTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe <stem>_reordered.pdf per input without
// writing anything. The order must cover every page exactly once.
func (t *ReorderTool) Plan(_ context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	parsed, parseErr := reorderReqFields(req)
	if parseErr != nil {
		return planFailure(plan, parseErr)
	}

	if orderErr := engine.ValidateReorderOrderSyntax(parsed.Order); orderErr != nil {
		return planFailure(plan, mapPageOpError(orderErr))
	}

	for _, inputPath := range parsed.InputPaths {
		if inputErr := engine.ValidatePageOpInput(inputPath); inputErr != nil {
			return planFailure(plan, mapPageOpError(inputErr))
		}
	}

	// Coverage needs the real page count. An unreadable input keeps the plan
	// alive so its per-item execution reports the canonical failure in a batch.
	for _, inputPath := range parsed.InputPaths {
		pageCount, countErr := engine.PageOpPageCount(inputPath)
		if countErr != nil {
			continue
		}
		if _, orderErr := engine.ParseReorderOrder(parsed.Order, pageCount); orderErr != nil {
			return planFailure(plan, mapPageOpError(orderErr))
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

		planned := planPageOpFileOutput(inputPath, filepath.Clean(targetDir), reorderOutputSuffix, used)
		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = reorderPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = map[string]any{reorderOptionOrder: parsed.Order}

	return plan, nil
}

func (t *ReorderTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	parsed, parseErr := reorderReqFields(req)
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
		jobErr = models.NewCanonicalJobError("PDF_REORDER_EXECUTION_MISSING_RESULT", "reorder execution returned no result", nil)
	}

	return pageOpFailureItem(parsed.InputPath, pageOpPlannedOutputPath(plan), jobErr), jobErr
}

func (t *ReorderTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := reorderReqFields(req)
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

// ExecutePlan reorders exactly the outputs resolved by Plan.
func (t *ReorderTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := reorderReqFields(req)
	if parseErr != nil {
		return nil, parseErr
	}

	order := stringOption(plan.EffectiveOptions, reorderOptionOrder)
	if order == "" {
		order = parsed.Order
	}

	return executePageOpFilePlan(ctx, plan, onProgress, engine.ErrorCodeReorderFailed, "PDF pages reordered successfully", func(ctx context.Context, planned models.PlannedOutput) error {
		return engine.Reorder(ctx, planned.InputPath, planned.OutputPath, order)
	})
}

type reorderRequestFields struct {
	Mode       string
	InputPath  string
	InputPaths []string
	OutputDir  string
	Order      string
}

func reorderReqFields(req models.JobRequest) (reorderRequestFields, *models.JobError) {
	base, parseErr := pageOpReqFields(req)
	if parseErr != nil {
		return reorderRequestFields{}, parseErr
	}

	order := strings.TrimSpace(optionString(req.Options, reorderOptionOrder))
	if order == "" {
		return reorderRequestFields{Mode: base.Mode, InputPath: base.InputPath, InputPaths: base.InputPaths, OutputDir: base.OutputDir}, mapPageOpError(&engine.PageOpError{
			Code:    engine.ErrorCodeReorderOrderRequired,
			Message: "options.order is required; list every page once, for example 3,1,2,4-6",
		})
	}

	return reorderRequestFields{
		Mode:       base.Mode,
		InputPath:  base.InputPath,
		InputPaths: base.InputPaths,
		OutputDir:  base.OutputDir,
		Order:      order,
	}, nil
}

func reorderPlanMessage(count int) string {
	if count == 1 {
		return "Ready to reorder 1 PDF."
	}

	return fmt.Sprintf("Ready to reorder %d PDFs.", count)
}
