package pdf

import (
	"context"
	"fmt"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/pdf/engine"
	"fileforge-desktop/internal/utils"
)

const (
	// ToolIDPDFExtractImages writes embedded images to a folder.
	ToolIDPDFExtractImages = "tool.pdf.extract_images"

	extractImagesOutputSuffix = "_images"

	extractImagesOptionPageSelection = "pageSelection"
)

// ExtractImagesTool extracts embedded images from the selected pages into a
// folder next to the input or inside the chosen output directory.
type ExtractImagesTool struct{}

func NewExtractImagesTool() *ExtractImagesTool {
	return &ExtractImagesTool{}
}

func (t *ExtractImagesTool) ID() string {
	return ToolIDPDFExtractImages
}

func (t *ExtractImagesTool) Capability() string {
	return ToolIDPDFExtractImages
}

func (t *ExtractImagesTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "PDF Extract Images",
		Description:      "Extract embedded images into a folder with collision-safe names",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  []string{"pdf"},
		OutputExtensions: []string{"png", "jpg", "jpeg", "tif", "tiff", "jp2"},
		RuntimeDeps:      []string{"pdfcpu"},
		Tags:             []string{"pdf", "images", "extract"},
		ResourceClass:    "cpu",
	}
}

func (t *ExtractImagesTool) RuntimeState(_ context.Context) models.ToolRuntimeState {
	return models.ToolRuntimeState{Status: "enabled", Healthy: true}
}

func (t *ExtractImagesTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan resolves one collision-safe <stem>_images folder per input without
// creating it. The image count is only known after extraction.
func (t *ExtractImagesTool) Plan(_ context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	parsed, parseErr := extractImagesReqFields(req)
	if parseErr != nil {
		return planFailure(plan, parseErr)
	}

	if optionErr := engine.ValidateExtractImagesOptions(parsed.PageSelection, 0); optionErr != nil {
		return planFailure(plan, mapPageOpError(optionErr))
	}

	for _, inputPath := range parsed.InputPaths {
		if inputErr := engine.ValidatePageOpInput(inputPath); inputErr != nil {
			return planFailure(plan, mapPageOpError(inputErr))
		}
	}

	// Bound the selection when the PDF can be read; an unreadable input fails
	// per item at execution so a batch still reports the rest.
	for _, inputPath := range parsed.InputPaths {
		pageCount, countErr := engine.PageOpPageCount(inputPath)
		if countErr != nil {
			continue
		}
		if optionErr := engine.ValidateExtractImagesOptions(parsed.PageSelection, pageCount); optionErr != nil {
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

		planned := planPageOpDirectoryOutput(inputPath, filepath.Clean(targetDir), extractImagesOutputSuffix, used)
		outputs = append(outputs, planned)
		warnings = append(warnings, planned.Warnings...)
	}

	if parsed.PageSelection != "" {
		plan.EffectiveOptions = map[string]any{extractImagesOptionPageSelection: parsed.PageSelection}
	}

	plan.Success = true
	plan.Message = extractImagesPlanMessage(len(outputs))
	plan.Outputs = outputs
	plan.Warnings = warnings

	return plan, nil
}

func (t *ExtractImagesTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	parsed, parseErr := extractImagesReqFields(req)
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
		jobErr = models.NewCanonicalJobError("PDF_EXTRACT_IMAGES_EXECUTION_MISSING_RESULT", "image extraction returned no result", nil)
	}

	return pageOpFailureItem(parsed.InputPath, pageOpPlannedOutputPath(plan), jobErr), jobErr
}

func (t *ExtractImagesTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := extractImagesReqFields(req)
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

// ExecutePlan extracts into exactly the folders resolved by Plan. Each item
// reports every written image file, so job verification checks the images
// themselves instead of only the folder.
func (t *ExtractImagesTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := extractImagesReqFields(req)
	if parseErr != nil {
		return nil, parseErr
	}

	pageSelection := stringOption(plan.EffectiveOptions, extractImagesOptionPageSelection)
	if pageSelection == "" {
		pageSelection = parsed.PageSelection
	}

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

		result, err := engine.ExtractImages(ctx, planned.InputPath, planned.OutputPath, pageSelection)
		if err != nil {
			jobErr := mapPageOpError(err)
			item.Success = false
			item.Message = jobErr.Message
			item.Error = jobErr
			itemErrors[i] = jobErr
		} else {
			item.Success = true
			item.Outputs = append([]string(nil), result.Files...)
			item.OutputCount = result.Count
			item.Details = map[string]any{"imageCount": result.Count}
			item.Message = fmt.Sprintf("Extracted %d %s to %s", result.Count, pluralizeWord("image", result.Count), filepath.Base(result.OutputDir))
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

		return completedPDFBatchItems(items, completed), models.NewCanonicalJobError(engine.ErrorCodeExtractImagesFailed, "batch image extraction failed", map[string]any{"cause": runErr.Error()})
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
		return items, models.NewCanonicalJobError(engine.ErrorCodeExtractImagesFailed, "one or more files failed in batch image extraction", map[string]any{
			"fileErrors": fileErrors,
		})
	}

	return items, nil
}

type extractImagesRequestFields struct {
	Mode          string
	InputPath     string
	InputPaths    []string
	OutputDir     string
	PageSelection string
}

func extractImagesReqFields(req models.JobRequest) (extractImagesRequestFields, *models.JobError) {
	base, parseErr := pageOpReqFields(req)
	if parseErr != nil {
		return extractImagesRequestFields{}, parseErr
	}

	pageSelection := strings.TrimSpace(optionString(req.Options, extractImagesOptionPageSelection))

	return extractImagesRequestFields{
		Mode:          base.Mode,
		InputPath:     base.InputPath,
		InputPaths:    base.InputPaths,
		OutputDir:     base.OutputDir,
		PageSelection: pageSelection,
	}, nil
}

func extractImagesPlanMessage(count int) string {
	if count == 1 {
		return "Ready to extract images from 1 PDF."
	}

	return fmt.Sprintf("Ready to extract images from %d PDFs.", count)
}

func pluralizeWord(word string, count int) string {
	if count == 1 {
		return word
	}

	return word + "s"
}
