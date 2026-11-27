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
	// ToolIDPDFRotate rotates all or selected pages of a PDF.
	ToolIDPDFRotate = "tool.pdf.rotate"

	rotateOutputSuffix = "_rotated"

	rotateOptionPageSelection = "pageSelection"
	rotateOptionAngle         = "angle"
)

// RotateTool rotates pages clockwise by 90, 180 or 270 degrees.
type RotateTool struct{}

func NewRotateTool() *RotateTool {
	return &RotateTool{}
}

func (t *RotateTool) ID() string {
	return ToolIDPDFRotate
}

func (t *RotateTool) Capability() string {
	return ToolIDPDFRotate
}

func (t *RotateTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "PDF Rotate",
		Description:      "Rotate all pages or a page selection by 90, 180 or 270 degrees",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{"pdfcpu"},
		Tags:             []string{"pdf", "rotate", "pages"},
		ResourceClass:    "cpu",
	}
}

func (t *RotateTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *RotateTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe <stem>_rotated.pdf per input without writing
// anything.
func (t *RotateTool) Plan(_ context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	parsed, parseErr := rotateReqFields(req)
	if parseErr != nil {
		return planFailure(plan, parseErr)
	}

	if optionErr := engine.ValidateRotateOptions(parsed.PageSelection, parsed.Angle); optionErr != nil {
		return planFailure(plan, mapPageOpError(optionErr))
	}

	for _, inputPath := range parsed.InputPaths {
		if inputErr := engine.ValidatePageOpInput(inputPath); inputErr != nil {
			return planFailure(plan, mapPageOpError(inputErr))
		}
	}

	// Validate the selection against the real page count when the PDF can be
	// read. An unreadable input keeps the plan alive so its per-item execution
	// reports the canonical failure inside a batch.
	for _, inputPath := range parsed.InputPaths {
		pageCount, countErr := engine.PageOpPageCount(inputPath)
		if countErr != nil {
			continue
		}
		if selectionErr := engine.ValidateRotatePageSelection(parsed.PageSelection, pageCount); selectionErr != nil {
			return planFailure(plan, mapPageOpError(selectionErr))
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

		planned := planPageOpFileOutput(inputPath, filepath.Clean(targetDir), rotateOutputSuffix, used)
		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	plan.Success = true
	plan.Message = rotatePlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings
	plan.EffectiveOptions = rotateEffectiveOptions(parsed)

	return plan, nil
}

func (t *RotateTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	parsed, parseErr := rotateReqFields(req)
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
		jobErr = models.NewCanonicalJobError("PDF_ROTATE_EXECUTION_MISSING_RESULT", "rotate execution returned no result", nil)
	}

	return pageOpFailureItem(parsed.InputPath, pageOpPlannedOutputPath(plan), jobErr), jobErr
}

func (t *RotateTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := rotateReqFields(req)
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

// ExecutePlan rotates exactly the outputs resolved by Plan, using the options
// stored on the plan so a retry subset only processes its own outputs.
func (t *RotateTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := rotateReqFields(req)
	if parseErr != nil {
		return nil, parseErr
	}

	pageSelection := stringOption(plan.EffectiveOptions, rotateOptionPageSelection)
	if pageSelection == "" {
		pageSelection = parsed.PageSelection
	}
	angle := intOption(plan.EffectiveOptions, rotateOptionAngle)
	if angle == 0 {
		angle = parsed.Angle
	}

	return executePageOpFilePlan(ctx, plan, onProgress, engine.ErrorCodeRotateFailed, "PDF rotate successful", func(ctx context.Context, planned models.PlannedOutput) error {
		return engine.Rotate(ctx, planned.InputPath, planned.OutputPath, pageSelection, angle)
	})
}

type rotateRequestFields struct {
	Mode          string
	InputPath     string
	InputPaths    []string
	OutputDir     string
	PageSelection string
	Angle         int
}

func rotateReqFields(req models.JobRequest) (rotateRequestFields, *models.JobError) {
	base, parseErr := pageOpReqFields(req)
	if parseErr != nil {
		return rotateRequestFields{}, parseErr
	}

	angle, present, angleErr := pageOpIntOption(req.Options, rotateOptionAngle)
	if angleErr != nil {
		return rotateRequestFields{Mode: base.Mode, InputPath: base.InputPath, InputPaths: base.InputPaths, OutputDir: base.OutputDir}, &models.JobError{
			Code:       engine.ErrorCodeRotateAngleInvalid,
			DetailCode: engine.ErrorCodeRotateAngleInvalid,
			Message:    angleErr.Message,
		}
	}
	if !present {
		return rotateRequestFields{Mode: base.Mode, InputPath: base.InputPath, InputPaths: base.InputPaths, OutputDir: base.OutputDir}, pageOpRequestError(engine.ErrorCodeRotateAngleInvalid, "options.angle is required (90, 180 or 270)")
	}

	pageSelection := strings.TrimSpace(optionString(req.Options, rotateOptionPageSelection))

	return rotateRequestFields{
		Mode:          base.Mode,
		InputPath:     base.InputPath,
		InputPaths:    base.InputPaths,
		OutputDir:     base.OutputDir,
		PageSelection: pageSelection,
		Angle:         angle,
	}, nil
}

func rotateEffectiveOptions(parsed rotateRequestFields) map[string]any {
	effective := map[string]any{rotateOptionAngle: parsed.Angle}
	if parsed.PageSelection != "" {
		effective[rotateOptionPageSelection] = parsed.PageSelection
	}

	return effective
}

func rotatePlanMessage(count int) string {
	if count == 1 {
		return "Ready to rotate 1 PDF."
	}

	return fmt.Sprintf("Ready to rotate %d PDFs.", count)
}
