package pdf

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/pdf/engine"
)

const (
	// ToolIDPDFImagesV1 places several images on a page grid and exports one PDF.
	ToolIDPDFImagesV1 = "tool.pdf.images"

	imagesOutputKindFile = "file"
	imagesOutputSuffix   = "_images"

	// imagesOptionIntegerLimit bounds every integer option before it is
	// converted, so an out-of-range float can never wrap into a valid value.
	imagesOptionIntegerLimit = 1 << 20
	// imagesErrorReasonMaxRunes bounds the cause text copied into a job error.
	imagesErrorReasonMaxRunes = 200
)

// ImagesToPDFTool renders multiple images into a single PDF using a page grid.
type ImagesToPDFTool struct{}

func NewImagesToPDFTool() *ImagesToPDFTool {
	return &ImagesToPDFTool{}
}

func (t *ImagesToPDFTool) ID() string {
	return ToolIDPDFImagesV1
}

func (t *ImagesToPDFTool) Capability() string {
	return ToolIDPDFImagesV1
}

func (t *ImagesToPDFTool) Manifest() models.ToolManifestV1 {
	return models.ToolManifestV1{
		ToolID:           t.ID(),
		Name:             "Images to PDF",
		Description:      "Place several images on a page grid and export one PDF.",
		Domain:           "pdf",
		Capability:       t.Capability(),
		Version:          "v1",
		SupportsSingle:   true,
		SupportsBatch:    false,
		InputExtensions:  []string{"jpg", "jpeg", "png", "gif", "webp", "bmp", "tiff", "tif"},
		OutputExtensions: []string{"pdf"},
		RuntimeDeps:      []string{"libvips", "pdfcpu"},
		Tags:             []string{"pdf", "images", "layout", "grid", "album"},
	}
}

func (t *ImagesToPDFTool) RuntimeState(_ context.Context) models.ToolRuntimeStateV1 {
	return models.ToolRuntimeStateV1{Status: "enabled", Healthy: true}
}

func (t *ImagesToPDFTool) Validate(ctx context.Context, req models.JobRequestV1) *models.JobErrorV1 {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan validates the layout and resolves the destination PDF without writing
// anything. An explicit outputPath must not exist; otherwise the output defaults
// to <firstStem>_images.pdf in the chosen folder or next to the first input.
func (t *ImagesToPDFTool) Plan(_ context.Context, req models.JobRequestV1) (models.PlanJobResponseV1, *models.JobErrorV1) {
	plan := models.PlanJobResponseV1{Outputs: []models.PlannedOutputV1{}}

	// A single PDF is produced from every input, so "single" is the only valid
	// mode even when the request carries more than one image.
	if strings.TrimSpace(req.Mode) != "single" {
		return planFailure(plan, &models.JobErrorV1{Code: "VALIDATION_ERROR", Message: "mode must be single"})
	}

	layout, layoutErr := imagesLayoutFromOptions(req.Options)
	if layoutErr != nil {
		return planFailure(plan, layoutErr)
	}

	outputPath, naturalPath, adjusted := resolveImagesOutputPath(req.InputPaths, req.OutputDir, req.Options)

	normalizedLayout, pageCount, validationErr := engine.ValidateImagesPDFRequest(req.InputPaths, outputPath, layout)
	if validationErr != nil {
		return planFailure(plan, mapImagesPlanError(validationErr))
	}

	planned := models.PlannedOutputV1{
		InputPath:   strings.Join(req.InputPaths, ","),
		OutputPath:  outputPath,
		OutputKind:  imagesOutputKindFile,
		OutputCount: 1,
		Adjusted:    adjusted,
	}
	if adjusted {
		planned.Warnings = append(planned.Warnings, fmt.Sprintf("%s already exists. Saving as %s.", filepath.Base(naturalPath), filepath.Base(outputPath)))
	}

	plan.Success = true
	plan.Message = fmt.Sprintf("Ready to place %d images on %d page(s).", len(req.InputPaths), pageCount)
	plan.Outputs = []models.PlannedOutputV1{planned}
	plan.EffectiveOptions = imagesEffectiveOptions(normalizedLayout)

	return plan, nil
}

// ExecuteSingle implements tools.SingleExecutor by delegating to the progress
// variant with a no-op callback.
func (t *ImagesToPDFTool) ExecuteSingle(ctx context.Context, req models.JobRequestV1) (models.JobResultItemV1, *models.JobErrorV1) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

// ExecuteSingleWithProgress resolves the plan once, executes exactly that plan
// and reports progress once per normalized input image.
func (t *ImagesToPDFTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequestV1, onProgress func(models.JobProgressV1)) (models.JobResultItemV1, *models.JobErrorV1) {
	inputPath := strings.Join(req.InputPaths, ",")

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return imagesFailureItem(inputPath, imagesPlannedOutputPath(plan), planErr), planErr
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, onProgress)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("PDF_IMAGES_EXECUTION_MISSING_RESULT", "images-to-PDF execution returned no result", nil)
	}

	return imagesFailureItem(inputPath, imagesPlannedOutputPath(plan), jobErr), jobErr
}

// ExecutePlan renders the images into the destination resolved by Plan. It
// re-normalizes the layout options but never re-resolves the output path.
func (t *ImagesToPDFTool) ExecutePlan(ctx context.Context, req models.JobRequestV1, plan models.PlanJobResponseV1, onProgress func(models.JobProgressV1)) ([]models.JobResultItemV1, *models.JobErrorV1) {
	inputPath := strings.Join(req.InputPaths, ",")

	layout, layoutErr := imagesLayoutFromOptions(req.Options)
	if layoutErr != nil {
		return []models.JobResultItemV1{imagesFailureItem(inputPath, imagesPlannedOutputPath(plan), layoutErr)}, layoutErr
	}

	outputPath := imagesPlannedOutputPath(plan)
	if outputPath == "" {
		jobErr := models.NewCanonicalJobError("PDF_IMAGES_EXECUTION_MISSING_RESULT", "the plan resolved no output", nil)
		return []models.JobResultItemV1{imagesFailureItem(inputPath, "", jobErr)}, jobErr
	}

	progress := func(processed, total int) {
		if onProgress == nil {
			return
		}
		onProgress(models.JobProgressV1{
			Current: processed,
			Total:   total,
			Stage:   "running",
			Message: fmt.Sprintf("Placed %d of %d images", processed, total),
		})
	}

	result, buildErr := engine.BuildImagesPDF(ctx, req.InputPaths, outputPath, layout, progress)
	if buildErr != nil {
		jobErr := mapImagesExecuteError(buildErr)
		return []models.JobResultItemV1{imagesFailureItem(inputPath, outputPath, jobErr)}, jobErr
	}

	return []models.JobResultItemV1{{
		InputPath:   inputPath,
		OutputPath:  result.OutputPath,
		Outputs:     []string{result.OutputPath},
		OutputCount: 1,
		Success:     true,
		Message:     fmt.Sprintf("%d images on %d page(s).", result.ImageCount, result.PageCount),
	}}, nil
}

// imagesLayoutFromOptions reads the optional grid options and applies the
// documented defaults. A value that is present but malformed is rejected
// instead of silently replaced by its default.
func imagesLayoutFromOptions(options map[string]any) (engine.ImagesLayout, *models.JobErrorV1) {
	layout := engine.ImagesLayout{
		PageSize:    engine.ImagesPageA4,
		Orientation: engine.ImagesOrientationPortrait,
		Columns:     2,
		Rows:        2,
		MarginMM:    10,
		GapMM:       4,
		Fit:         engine.ImagesFitContain,
	}

	if options == nil {
		return layout, nil
	}

	var optionErr *models.JobErrorV1
	if layout.PageSize, optionErr = imagesStringOption(options, "pageSize", layout.PageSize); optionErr != nil {
		return engine.ImagesLayout{}, optionErr
	}
	if layout.Orientation, optionErr = imagesStringOption(options, "orientation", layout.Orientation); optionErr != nil {
		return engine.ImagesLayout{}, optionErr
	}
	if layout.Columns, optionErr = imagesIntOption(options, "columns", layout.Columns); optionErr != nil {
		return engine.ImagesLayout{}, optionErr
	}
	if layout.Rows, optionErr = imagesIntOption(options, "rows", layout.Rows); optionErr != nil {
		return engine.ImagesLayout{}, optionErr
	}
	if layout.MarginMM, optionErr = imagesFloatOption(options, "marginMm", layout.MarginMM); optionErr != nil {
		return engine.ImagesLayout{}, optionErr
	}
	if layout.GapMM, optionErr = imagesFloatOption(options, "gapMm", layout.GapMM); optionErr != nil {
		return engine.ImagesLayout{}, optionErr
	}
	if layout.Fit, optionErr = imagesStringOption(options, "fit", layout.Fit); optionErr != nil {
		return engine.ImagesLayout{}, optionErr
	}

	slots, slotsErr := imagesSlotsFromOptions(options)
	if slotsErr != nil {
		return engine.ImagesLayout{}, slotsErr
	}
	layout.Slots = slots

	return layout, nil
}

func imagesStringOption(options map[string]any, key, fallback string) (string, *models.JobErrorV1) {
	raw, exists := options[key]
	if !exists || raw == nil {
		return fallback, nil
	}

	value, ok := raw.(string)
	if !ok {
		return "", imagesInvalidLayoutError(fmt.Sprintf("options.%s must be a string", key), map[string]any{"field": key})
	}

	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fallback, nil
	}

	return trimmed, nil
}

func imagesIntOption(options map[string]any, key string, fallback int) (int, *models.JobErrorV1) {
	raw, exists := options[key]
	if !exists || raw == nil {
		return fallback, nil
	}

	value, ok := ocrNumericOption(raw)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) {
		return 0, imagesInvalidLayoutError(fmt.Sprintf("options.%s must be a whole number", key), map[string]any{"field": key})
	}
	if value < -imagesOptionIntegerLimit || value > imagesOptionIntegerLimit {
		return 0, imagesInvalidLayoutError(fmt.Sprintf("options.%s is out of range", key), map[string]any{"field": key})
	}

	return int(value), nil
}

func imagesFloatOption(options map[string]any, key string, fallback float64) (float64, *models.JobErrorV1) {
	raw, exists := options[key]
	if !exists || raw == nil {
		return fallback, nil
	}

	value, ok := ocrNumericOption(raw)
	if !ok {
		return 0, imagesInvalidLayoutError(fmt.Sprintf("options.%s must be a number", key), map[string]any{"field": key})
	}

	return value, nil
}

// imagesSlotsFromOptions accepts the JSON shape ([]any of map[string]any) and
// the programmatic shape ([]engine.ImageSlot or []map[string]any). A nil result
// lets the engine auto-fill the grid in input order.
func imagesSlotsFromOptions(options map[string]any) ([]engine.ImageSlot, *models.JobErrorV1) {
	raw, exists := options["slots"]
	if !exists || raw == nil {
		return nil, nil
	}

	switch typedSlots := raw.(type) {
	case []engine.ImageSlot:
		return append([]engine.ImageSlot(nil), typedSlots...), nil
	case []map[string]any:
		return imagesSlotsFromMaps(typedSlots)
	case []any:
		return imagesSlotsFromAny(typedSlots)
	default:
		return nil, imagesInvalidLayoutError("options.slots must be an array of slot objects", nil)
	}
}

func imagesSlotsFromMaps(entries []map[string]any) ([]engine.ImageSlot, *models.JobErrorV1) {
	slots := make([]engine.ImageSlot, 0, len(entries))
	for index, entry := range entries {
		slot, slotErr := imagesSlotFromMap(entry, index)
		if slotErr != nil {
			return nil, slotErr
		}
		slots = append(slots, slot)
	}

	return slots, nil
}

func imagesSlotsFromAny(entries []any) ([]engine.ImageSlot, *models.JobErrorV1) {
	slots := make([]engine.ImageSlot, 0, len(entries))
	for index, entry := range entries {
		values, ok := entry.(map[string]any)
		if !ok {
			return nil, imagesInvalidLayoutError(
				fmt.Sprintf("options.slots[%d] must be an object with page, cell and inputIndex", index),
				map[string]any{"slotIndex": index},
			)
		}

		slot, slotErr := imagesSlotFromMap(values, index)
		if slotErr != nil {
			return nil, slotErr
		}
		slots = append(slots, slot)
	}

	return slots, nil
}

func imagesSlotFromMap(values map[string]any, index int) (engine.ImageSlot, *models.JobErrorV1) {
	page, pageErr := imagesSlotField(values, index, "page")
	if pageErr != nil {
		return engine.ImageSlot{}, pageErr
	}
	cell, cellErr := imagesSlotField(values, index, "cell")
	if cellErr != nil {
		return engine.ImageSlot{}, cellErr
	}
	inputIndex, inputErr := imagesSlotField(values, index, "inputIndex")
	if inputErr != nil {
		return engine.ImageSlot{}, inputErr
	}

	return engine.ImageSlot{Page: page, Cell: cell, InputIndex: inputIndex}, nil
}

func imagesSlotField(values map[string]any, index int, field string) (int, *models.JobErrorV1) {
	raw, exists := values[field]
	if !exists || raw == nil {
		return 0, imagesSlotError(index, fmt.Sprintf("options.slots[%d].%s is required", index, field), field)
	}

	value, ok := ocrNumericOption(raw)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value != math.Trunc(value) || value < -imagesOptionIntegerLimit || value > imagesOptionIntegerLimit {
		return 0, imagesSlotError(index, fmt.Sprintf("options.slots[%d].%s must be a whole number", index, field), field)
	}

	return int(value), nil
}

func imagesSlotError(index int, message, field string) *models.JobErrorV1 {
	return imagesInvalidLayoutError(message, map[string]any{"slotIndex": index, "field": field})
}

func imagesInvalidLayoutError(message string, details map[string]any) *models.JobErrorV1 {
	return models.NewCanonicalJobError(engine.ImagesErrorInvalidLayout, message, details)
}

// resolveImagesOutputPath returns the resolved output path, the natural
// (unsuffixed) auto path and whether the auto path had to be adjusted.
func resolveImagesOutputPath(inputPaths []string, outputDir string, options map[string]any) (string, string, bool) {
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

	base := stem + imagesOutputSuffix
	naturalPath := filepath.Join(dir, base+".pdf")
	outputPath, adjusted := outputplan.UniqueFilePath(dir, base, "pdf", map[string]struct{}{})

	return outputPath, naturalPath, adjusted
}

// imagesEffectiveOptions returns the normalized layout the engine will render,
// including the resolved slots.
func imagesEffectiveOptions(layout engine.ImagesLayout) map[string]any {
	slots := make([]map[string]any, 0, len(layout.Slots))
	for _, slot := range layout.Slots {
		slots = append(slots, map[string]any{
			"page":       slot.Page,
			"cell":       slot.Cell,
			"inputIndex": slot.InputIndex,
		})
	}

	return map[string]any{
		"pageSize":    layout.PageSize,
		"orientation": layout.Orientation,
		"columns":     layout.Columns,
		"rows":        layout.Rows,
		"marginMm":    layout.MarginMM,
		"gapMm":       layout.GapMM,
		"fit":         layout.Fit,
		"slots":       slots,
	}
}

func imagesFailureItem(inputPath, outputPath string, jobErr *models.JobErrorV1) models.JobResultItemV1 {
	return models.JobResultItemV1{
		InputPath:  inputPath,
		OutputPath: outputPath,
		Success:    false,
		Message:    jobErr.Message,
		Error:      jobErr,
	}
}

func imagesPlannedOutputPath(plan models.PlanJobResponseV1) string {
	if len(plan.Outputs) == 0 {
		return ""
	}

	return plan.Outputs[0].OutputPath
}

// mapImagesPlanError keeps the engine detail code and applies the canonical
// mapping for the top-level code, which is what planning failures need.
func mapImagesPlanError(err error) *models.JobErrorV1 {
	var imagesErr *engine.ImagesError
	if !errors.As(err, &imagesErr) {
		return &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	return models.NewCanonicalJobError(imagesErr.Code, imagesErr.Message, imagesErr.Details)
}

// mapImagesExecuteError turns a build failure into a job error. Cancellation is
// reported as CANCELLED_BY_USER so the job is cancelled instead of failed;
// every other failure keeps the stable engine code in both code fields.
func mapImagesExecuteError(err error) *models.JobErrorV1 {
	var imagesErr *engine.ImagesError
	if !errors.As(err, &imagesErr) {
		return &models.JobErrorV1{Code: "EXECUTION_ERROR", Message: err.Error()}
	}

	if imagesErr.Code == engine.ImagesErrorCancelled {
		return models.NewJobError(models.ErrorCodeCancelledByUser, engine.ImagesErrorCancelled, "Images to PDF was canceled.", imagesErr.Details)
	}

	return &models.JobErrorV1{
		Code:       imagesErr.Code,
		DetailCode: imagesErr.Code,
		Message:    imagesErr.Message,
		Details:    imagesDetailsWithReason(imagesErr.Details, imagesErr.Cause),
	}
}

// imagesDetailsWithReason copies the engine details and adds a bounded reason
// taken from the cause, so raw library errors never flood the response.
func imagesDetailsWithReason(details map[string]any, cause error) map[string]any {
	reason := ""
	if cause != nil {
		reason = strings.TrimSpace(cause.Error())
	}
	if reason == "" {
		return details
	}

	if runes := []rune(reason); len(runes) > imagesErrorReasonMaxRunes {
		reason = string(runes[:imagesErrorReasonMaxRunes]) + "..."
	}

	merged := make(map[string]any, len(details)+1)
	for key, value := range details {
		merged[key] = value
	}
	merged["reason"] = reason

	return merged
}
