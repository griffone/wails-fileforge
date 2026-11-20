package image

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strconv"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/pdf/engine"
	"fileforge-desktop/internal/utils"
)

const ToolIDImageOCR = "tool.image.ocr"

const (
	imageOCRTextExtension   = "txt"
	imageOCRPDFExtension    = "pdf"
	imageOCRPDFSuffix       = "_ocr"
	imageOCRAnimatedWarning = "animated GIF input: only the first frame is read"
)

// ImageOCRTool turns images into text with local Tesseract and can also write a
// searchable PDF per image. The runner is injectable so tests can drive the
// exact Tesseract contract without a real installation.
type ImageOCRTool struct {
	runner engine.CommandRunner
}

// imageOCRRequest is the parsed, normalized request.
type imageOCRRequest struct {
	mode       string
	inputPaths []string
	outputDir  string
	language   string
	writePDF   bool
	dpi        int
}

// preparedImageOCRJob is one resolved input/output pair plus the runtime
// snapshot detected once for the whole job.
type preparedImageOCRJob struct {
	inputPath    string
	textPath     string
	pdfPath      string
	language     string
	dpi          int
	capabilities engine.OCRCapabilities
}

func NewImageOCRTool() *ImageOCRTool {
	return &ImageOCRTool{}
}

// NewImageOCRToolWithRunner injects the command runner. nil keeps the real
// exec-based runner used in production.
func NewImageOCRToolWithRunner(runner engine.CommandRunner) *ImageOCRTool {
	return &ImageOCRTool{runner: runner}
}

func (t *ImageOCRTool) ID() string {
	return ToolIDImageOCR
}

func (t *ImageOCRTool) Capability() string {
	return ToolIDImageOCR
}

func (t *ImageOCRTool) Manifest() models.ToolManifest {
	return models.ToolManifest{
		ToolID:           t.ID(),
		Name:             "Image OCR",
		Description:      "Extract text from images with local Tesseract and optionally write a searchable PDF",
		Domain:           "image",
		Capability:       t.Capability(),
		Version:          "1.0",
		SupportsSingle:   true,
		SupportsBatch:    true,
		InputExtensions:  append([]string(nil), imageInputExtensions...),
		OutputExtensions: []string{imageOCRTextExtension, imageOCRPDFExtension},
		RuntimeDeps:      []string{"tesseract", "libvips"},
		Tags:             []string{"image", "ocr", "text"},
	}
}

// detectCapabilities resolves the runtime once. Image OCR needs Tesseract and
// language data only; pdftoppm belongs to the PDF pipeline and is irrelevant.
func (t *ImageOCRTool) detectCapabilities(ctx context.Context) engine.OCRCapabilities {
	if ctx == nil {
		ctx = context.Background()
	}

	return imageOCRDetectCapabilities(ctx, t.runner)
}

func (t *ImageOCRTool) RuntimeState(ctx context.Context) models.ToolRuntimeState {
	capabilities := t.detectCapabilities(ctx)
	if ImageOCRAvailable(capabilities) {
		return models.ToolRuntimeState{Status: "enabled", Healthy: true}
	}

	return models.ToolRuntimeState{
		Status:  "degraded",
		Healthy: false,
		Reason:  ImageOCRUnavailableReason(capabilities),
	}
}

func (t *ImageOCRTool) Validate(ctx context.Context, req models.JobRequest) *models.JobError {
	_, planErr := t.Plan(ctx, req)
	return planErr
}

// Plan validates the OCR runtime, language, DPI and inputs and resolves one
// <stem>.txt plus an optional <stem>_ocr.pdf per input without writing anything.
func (t *ImageOCRTool) Plan(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, *models.JobError) {
	plan, _, planErr := t.planImageOCRRequest(ctx, req)
	return plan, planErr
}

func (t *ImageOCRTool) planImageOCRRequest(ctx context.Context, req models.JobRequest) (models.PlanJobResponse, []preparedImageOCRJob, *models.JobError) {
	parsed, parseErr := parseImageOCRRequest(req)
	if parseErr != nil {
		plan, _ := planFailure(models.PlanJobResponse{Outputs: []models.PlannedOutput{}}, parseErr)
		return plan, nil, parseErr
	}

	return t.planParsedImageOCRRequest(ctx, parsed)
}

// planParsedImageOCRRequest detects capabilities once per job and builds every
// planned output plus the executor input. It never runs Tesseract.
func (t *ImageOCRTool) planParsedImageOCRRequest(ctx context.Context, parsed imageOCRRequest) (models.PlanJobResponse, []preparedImageOCRJob, *models.JobError) {
	plan := models.PlanJobResponse{Outputs: []models.PlannedOutput{}}

	capabilities := t.detectCapabilities(ctx)
	if !ImageOCRAvailable(capabilities) {
		jobErr := models.NewJobError(
			models.ErrorCodeRuntimeDepMissing,
			ImageOCRCodeRuntimeUnavailable,
			ImageOCRUnavailableReason(capabilities),
			map[string]any{
				"tesseract": capabilities.Tesseract,
				"languages": capabilities.Languages,
			},
		)
		failedPlan, _ := planFailure(plan, jobErr)
		return failedPlan, nil, jobErr
	}

	language := strings.TrimSpace(parsed.language)
	if language == "" {
		language = strings.TrimSpace(capabilities.RecommendedLanguage)
	}
	if language == "" || !capabilities.HasLanguage(language) {
		message := fmt.Sprintf("Choose an installed Tesseract language. Installed languages: %s.", strings.Join(capabilities.Languages, ", "))
		if language != "" {
			message = fmt.Sprintf("Tesseract language '%s' is not installed. Installed languages: %s.", language, strings.Join(capabilities.Languages, ", "))
		}
		jobErr := models.NewJobError(
			models.ErrorCodeValidationInvalidInput,
			ImageOCRCodeLanguageUnavailable,
			message,
			map[string]any{"language": language, "languages": capabilities.Languages},
		)
		failedPlan, _ := planFailure(plan, jobErr)
		return failedPlan, nil, jobErr
	}

	inputWarnings := make([]string, 0)
	for _, inputPath := range parsed.inputPaths {
		if inputErr := ValidateImageOCRInput(inputPath); inputErr != nil {
			jobErr := imageOCREngineJobError(inputErr, map[string]any{"inputPath": inputPath})
			failedPlan, _ := planFailure(plan, jobErr)
			return failedPlan, nil, jobErr
		}
		if compressInputFrameCount(inputPath) > 1 {
			inputWarnings = append(inputWarnings, fmt.Sprintf("%s: %s", filepath.Base(inputPath), imageOCRAnimatedWarning))
		}
	}

	// Resolve the effective destination for every input. Explicit and implicit
	// output folders are write-probed exactly like execution, so the plan never
	// promises a destination the user cannot write.
	targetDirs := make([]string, len(parsed.inputPaths))
	validatedDirs := make(map[string]struct{}, len(parsed.inputPaths))
	for index, inputPath := range parsed.inputPaths {
		targetDir := parsed.outputDir
		if targetDir == "" {
			targetDir = filepath.Dir(inputPath)
		}
		targetDir = filepath.Clean(targetDir)
		targetDirs[index] = targetDir

		if _, checked := validatedDirs[targetDir]; checked {
			continue
		}
		validatedDirs[targetDir] = struct{}{}

		if dirErr := ValidateImageOCRDestination(targetDir); dirErr != nil {
			jobErr := imageOCREngineJobError(dirErr, map[string]any{"outputDir": targetDir, "inputPath": inputPath})
			failedPlan, _ := planFailure(plan, jobErr)
			return failedPlan, nil, jobErr
		}
	}

	// Reserve every input path so an auto-derived output can never overwrite a
	// different input of the same batch.
	used := make(map[string]struct{}, len(parsed.inputPaths))
	for _, inputPath := range parsed.inputPaths {
		outputplan.MarkUsed(used, inputPath)
	}

	outputs := make([]models.PlannedOutput, 0, len(parsed.inputPaths)*2)
	prepared := make([]preparedImageOCRJob, 0, len(parsed.inputPaths))
	warnings := make([]string, 0, len(parsed.inputPaths)*2)

	for index, inputPath := range parsed.inputPaths {
		targetDir := targetDirs[index]

		textBase, pdfBase := imageOCROutputBaseNames(inputPath)

		textPath, textAdjusted := outputplan.UniqueFilePath(targetDir, textBase, imageOCRTextExtension, used)
		textOutput := models.PlannedOutput{
			InputPath:   inputPath,
			OutputPath:  textPath,
			OutputKind:  imageOutputKindFile,
			OutputCount: 1,
		}
		if textAdjusted {
			textOutput.Adjusted = true
			textOutput.Warnings = append(textOutput.Warnings, fmt.Sprintf(
				"%s already exists. Saving as %s.",
				textBase+"."+imageOCRTextExtension,
				filepath.Base(textPath),
			))
		}

		outputs = append(outputs, textOutput)
		warnings = append(warnings, textOutput.Warnings...)

		pdfPath := ""
		if parsed.writePDF {
			adjustedPDFPath, pdfAdjusted := outputplan.UniqueFilePath(targetDir, pdfBase, imageOCRPDFExtension, used)
			pdfPath = adjustedPDFPath

			pdfOutput := models.PlannedOutput{
				InputPath:   inputPath,
				OutputPath:  pdfPath,
				OutputKind:  imageOutputKindFile,
				OutputCount: 1,
			}
			if pdfAdjusted {
				pdfOutput.Adjusted = true
				pdfOutput.Warnings = append(pdfOutput.Warnings, fmt.Sprintf(
					"%s already exists. Saving as %s.",
					pdfBase+"."+imageOCRPDFExtension,
					filepath.Base(pdfPath),
				))
			}

			outputs = append(outputs, pdfOutput)
			warnings = append(warnings, pdfOutput.Warnings...)
		}

		prepared = append(prepared, preparedImageOCRJob{
			inputPath:    inputPath,
			textPath:     textPath,
			pdfPath:      pdfPath,
			language:     language,
			dpi:          parsed.dpi,
			capabilities: capabilities,
		})
	}

	plan.Success = true
	plan.Message = imageOCRPlanMessage(len(prepared))
	plan.Outputs = outputs
	plan.Warnings = append(warnings, inputWarnings...)
	plan.EffectiveOptions = map[string]any{
		"language": language,
		"writePdf": parsed.writePDF,
		"dpi":      parsed.dpi,
	}

	return plan, prepared, nil
}

// ExecuteSingle implements tools.SingleExecutor by delegating to the progress
// variant with a no-op callback.
func (t *ImageOCRTool) ExecuteSingle(ctx context.Context, req models.JobRequest) (models.JobResultItem, *models.JobError) {
	return t.ExecuteSingleWithProgress(ctx, req, nil)
}

// ExecuteSingleWithProgress resolves the plan once, executes exactly that plan
// and reports stage level progress for the one-item job.
func (t *ImageOCRTool) ExecuteSingleWithProgress(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) (models.JobResultItem, *models.JobError) {
	inputPath := firstInputPath(req.InputPaths)

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return imageFailureItem(inputPath, plannedOutputPath(plan), planErr), planErr
	}

	if onProgress != nil {
		onProgress(models.JobProgress{Current: 0, Total: 1, Stage: models.JobStatusRunning, Message: "recognizing 1/1"})
	}

	items, execErr := t.ExecutePlan(ctx, req, plan, onProgress)
	if len(items) > 0 {
		return items[0], execErr
	}

	jobErr := execErr
	if jobErr == nil {
		jobErr = models.NewCanonicalJobError("IMAGE_OCR_EXECUTION", "OCR returned no result", nil)
	}

	return imageFailureItem(inputPath, plannedOutputPath(plan), jobErr), jobErr
}

// ExecuteBatch plans the request once and then executes exactly that plan. It
// never re-resolves output paths from the request.
func (t *ImageOCRTool) ExecuteBatch(ctx context.Context, req models.JobRequest, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := parseImageOCRRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}

	if parsed.mode != "batch" {
		return nil, models.NewCanonicalJobError("IMAGE_OCR_MODE_INVALID", "mode must be batch", nil)
	}

	plan, planErr := t.Plan(ctx, req)
	if planErr != nil {
		return nil, planErr
	}

	return t.ExecutePlan(ctx, req, plan, onProgress)
}

// ExecutePlan recognizes exactly the outputs resolved by Plan. OCR writes a
// text file and, when writePdf is set, a PDF sidecar for the same input; the
// plan emits them adjacently, so they are grouped into one Tesseract run per
// input. Capabilities are detected once for the whole plan, never per item.
func (t *ImageOCRTool) ExecutePlan(ctx context.Context, req models.JobRequest, plan models.PlanJobResponse, onProgress func(models.JobProgress)) ([]models.JobResultItem, *models.JobError) {
	parsed, parseErr := parseImageOCRRequest(req)
	if parseErr != nil {
		return nil, parseErr
	}

	total := len(plan.Outputs)
	if total == 0 {
		return nil, models.NewCanonicalJobError(ImageOCRCodeInputRequired, "Choose at least one image to OCR.", nil)
	}

	language := parsed.language
	if value := convertOptionString(plan.EffectiveOptions, "language"); value != "" {
		language = value
	}
	dpi := parsed.dpi
	if value, ok := numericOption(plan.EffectiveOptions, "dpi"); ok && value > 0 {
		dpi = value
	}
	writePDF := parsed.writePDF
	if value, ok := boolOption(plan.EffectiveOptions, "writePdf"); ok {
		writePDF = value
	}

	capabilities := t.detectCapabilities(ctx)
	groups := imageOCRPlanGroups(plan.Outputs, writePDF, preparedImageOCRJob{
		language:     language,
		dpi:          dpi,
		capabilities: capabilities,
	})

	detailCode := "IMAGE_OCR_BATCH_ITEM"
	if req.Mode == "single" {
		detailCode = "IMAGE_OCR_EXECUTION"
	}

	items := make([]models.JobResultItem, total)
	completed := make([]bool, total)
	progress := newParallelProgress(len(groups), onProgress)

	runErr := utils.ParallelForIndex(ctx, len(groups), utils.ParallelWorkerCount(), func(ctx context.Context, index int) error {
		group := groups[index]
		outcome, execErr := t.recognize(ctx, group.job)
		if execErr != nil {
			// A cancelled item must not be reported as a retryable batch
			// failure; the post-loop cancellation check returns the canonical
			// cancellation error instead.
			itemErr, _ := imageOCRExecutionError(ctx, detailCode, group.job.inputPath, execErr)
			for _, outputIndex := range group.outputIndices {
				items[outputIndex] = imageFailureItem(group.job.inputPath, plan.Outputs[outputIndex].OutputPath, itemErr)
				completed[outputIndex] = true
			}
		} else {
			for _, outputIndex := range group.outputIndices {
				items[outputIndex] = imageOCRPlannedSuccessItem(group.job, plan.Outputs[outputIndex], outcome)
				completed[outputIndex] = true
			}
		}
		progress.itemCompleted()

		return nil
	})

	if batchRunCancelled(ctx, runErr) {
		return collectCompletedItems(items, completed), imageOCRCancelledJobError(ctx)
	}

	return items, firstFailedItemError(items)
}

// imageOCRPlanGroup is one Tesseract run plus the plan outputs it satisfies: the
// text file and, when requested, its adjacent PDF sidecar.
type imageOCRPlanGroup struct {
	job           preparedImageOCRJob
	outputIndices []int
}

// imageOCRPlanGroups rebuilds the executor input from the planned outputs in
// plan order. A missing sidecar leaves the text output as a standalone job; an
// orphan sidecar keeps its own output so it fails gracefully instead of being
// dropped.
func imageOCRPlanGroups(outputs []models.PlannedOutput, writePDF bool, template preparedImageOCRJob) []imageOCRPlanGroup {
	groups := make([]imageOCRPlanGroup, 0, len(outputs))

	for index := 0; index < len(outputs); index++ {
		planned := outputs[index]
		job := template
		job.inputPath = planned.InputPath
		job.textPath = ""
		job.pdfPath = ""

		if imageOCRIsPDFOutput(planned.OutputPath) {
			// Sidecar without its text output (a reduced plan): keep it so the
			// failure is reported per planned output.
			job.pdfPath = planned.OutputPath
			groups = append(groups, imageOCRPlanGroup{job: job, outputIndices: []int{index}})
			continue
		}

		job.textPath = planned.OutputPath
		indices := []int{index}

		if writePDF && index+1 < len(outputs) {
			sidecar := outputs[index+1]
			if sidecar.InputPath == planned.InputPath && imageOCRIsPDFOutput(sidecar.OutputPath) {
				job.pdfPath = sidecar.OutputPath
				indices = append(indices, index+1)
				index++
			}
		}

		groups = append(groups, imageOCRPlanGroup{job: job, outputIndices: indices})
	}

	return groups
}

func imageOCRIsPDFOutput(outputPath string) bool {
	return strings.EqualFold(filepath.Ext(outputPath), "."+imageOCRPDFExtension)
}

// imageOCRPlannedSuccessItem reports one planned output. The text output keeps
// the existing result shape with both declared outputs; the PDF sidecar
// reports its own file.
func imageOCRPlannedSuccessItem(job preparedImageOCRJob, planned models.PlannedOutput, outcome ImageOCRResult) models.JobResultItem {
	if strings.EqualFold(planned.OutputPath, job.textPath) {
		return imageOCRSuccessItem(job, outcome)
	}

	return models.JobResultItem{
		InputPath:   job.inputPath,
		OutputPath:  planned.OutputPath,
		Outputs:     []string{planned.OutputPath},
		OutputCount: 1,
		Success:     true,
		Message:     imageOCRResultMessage(outcome),
	}
}

func parseImageOCRRequest(req models.JobRequest) (imageOCRRequest, *models.JobError) {
	mode := strings.TrimSpace(req.Mode)
	if mode != "single" && mode != "batch" {
		return imageOCRRequest{}, models.NewCanonicalJobError("IMAGE_OCR_MODE_INVALID", "mode must be single or batch", nil)
	}

	if len(req.InputPaths) == 0 {
		return imageOCRRequest{}, models.NewCanonicalJobError(ImageOCRCodeInputRequired, "Choose at least one image to OCR.", nil)
	}

	if mode == "single" && len(req.InputPaths) != 1 {
		return imageOCRRequest{}, models.NewCanonicalJobError("IMAGE_OCR_SINGLE_INPUT_COUNT", "single mode requires exactly one input", nil)
	}

	inputPaths := make([]string, 0, len(req.InputPaths))
	for _, rawPath := range req.InputPaths {
		trimmed := strings.TrimSpace(rawPath)
		if trimmed == "" {
			return imageOCRRequest{}, models.NewCanonicalJobError(ImageOCRCodeInputRequired, "inputPaths contains empty item", nil)
		}
		inputPaths = append(inputPaths, trimmed)
	}

	language, languageErr := imageOCRLanguageOption(req.Options)
	if languageErr != nil {
		return imageOCRRequest{}, languageErr
	}

	writePDF := false
	if optionSupplied(req.Options, "writePdf") {
		value, ok := boolOption(req.Options, "writePdf")
		if !ok {
			return imageOCRRequest{}, models.NewCanonicalJobError("IMAGE_OCR_OPTION_INVALID", "options.writePdf must be a boolean", nil)
		}
		writePDF = value
	}

	dpi, dpiErr := imageOCRDPIOption(req.Options)
	if dpiErr != nil {
		return imageOCRRequest{}, dpiErr
	}

	outputDir := strings.TrimSpace(req.OutputDir)
	if outputDir == "" {
		outputDir = convertOptionString(req.Options, "outputDir")
	}

	return imageOCRRequest{
		mode:       mode,
		inputPaths: inputPaths,
		outputDir:  outputDir,
		language:   language,
		writePDF:   writePDF,
		dpi:        dpi,
	}, nil
}

func imageOCRLanguageOption(options map[string]any) (string, *models.JobError) {
	if !optionSupplied(options, "language") {
		return "", nil
	}

	value, ok := options["language"].(string)
	if !ok {
		return "", models.NewCanonicalJobError("IMAGE_OCR_LANGUAGE_INVALID", "options.language must be a string", nil)
	}

	return strings.TrimSpace(value), nil
}

// imageOCRDPIOption defaults to the engine's OCR DPI and accepts the shared
// 150-600 range. It rejects fractions instead of silently truncating them.
func imageOCRDPIOption(options map[string]any) (int, *models.JobError) {
	dpi := engine.DefaultOCRDPI
	if optionSupplied(options, "dpi") {
		value, ok := watermarkFloatValue(options["dpi"])
		if !ok || value != math.Trunc(value) {
			return 0, models.NewCanonicalJobError("IMAGE_OCR_DPI_INVALID", "options.dpi must be a whole number", nil)
		}
		if value < float64(engine.MinOCRDPI) || value > float64(engine.MaxOCRDPI) {
			return 0, models.NewCanonicalJobError(
				"IMAGE_OCR_DPI_INVALID",
				fmt.Sprintf("options.dpi must be between %d and %d", engine.MinOCRDPI, engine.MaxOCRDPI),
				nil,
			)
		}
		dpi = int(value)
	}

	return dpi, nil
}

// recognize runs one prepared item with the capabilities detected during
// planning, so a batch never re-detects the runtime per image.
func (t *ImageOCRTool) recognize(ctx context.Context, item preparedImageOCRJob) (ImageOCRResult, error) {
	return RecognizeImageWithCapabilities(
		ctx,
		t.runner,
		item.capabilities,
		item.inputPath,
		item.textPath,
		item.pdfPath,
		ImageOCROptions{Language: item.language, DPI: item.dpi},
	)
}

func imageOCROutputBaseNames(inputPath string) (string, string) {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		stem = "image"
	}

	return stem, stem + imageOCRPDFSuffix
}

func imageOCRPlanMessage(count int) string {
	if count == 1 {
		return "Ready to OCR 1 image."
	}

	return fmt.Sprintf("Ready to OCR %d images.", count)
}

// imageOCRSuccessItem reports the primary text output first and the optional
// searchable PDF second.
func imageOCRSuccessItem(item preparedImageOCRJob, outcome ImageOCRResult) models.JobResultItem {
	outputs := []string{item.textPath}
	if item.pdfPath != "" {
		outputs = append(outputs, item.pdfPath)
	}

	return models.JobResultItem{
		InputPath:   item.inputPath,
		OutputPath:  item.textPath,
		Outputs:     outputs,
		OutputCount: len(outputs),
		Success:     true,
		Message:     imageOCRResultMessage(outcome),
	}
}

// imageOCRResultMessage reports the honest character count. A zero count is the
// no-text warning, never a fake success message with "0 characters".
func imageOCRResultMessage(outcome ImageOCRResult) string {
	message := fmt.Sprintf("%s characters recognized", imageOCRFormatCharacterCount(outcome.TextCharacters))
	if outcome.TextCharacters == 0 {
		message = ImageOCRWarningNoText
	}

	for _, warning := range outcome.Warnings {
		if warning == "" || strings.Contains(message, warning) {
			continue
		}
		message += " " + warning
	}

	return message
}

// imageOCRFormatCharacterCount groups digits with commas ("1234" -> "1,234").
func imageOCRFormatCharacterCount(value int) string {
	text := strconv.Itoa(value)
	if len(text) <= 3 {
		return text
	}

	sign := ""
	if strings.HasPrefix(text, "-") {
		sign = "-"
		text = text[1:]
	}

	leader := len(text) % 3
	if leader == 0 {
		leader = 3
	}

	var builder strings.Builder
	builder.WriteString(text[:leader])
	for index := leader; index < len(text); index += 3 {
		builder.WriteByte(',')
		builder.WriteString(text[index : index+3])
	}

	return sign + builder.String()
}

// imageOCREngineJobError maps a typed engine failure to a canonical job error.
// The engine messages are user-facing; the raw cause stays in Details["error"].
func imageOCREngineJobError(ocrErr *ImageOCRError, extraDetails map[string]any) *models.JobError {
	details := make(map[string]any, len(extraDetails)+1)
	for key, value := range extraDetails {
		details[key] = value
	}
	if ocrErr.Cause != nil {
		details["error"] = ocrErr.Cause.Error()
	} else if ocrErr.Message != "" {
		details["error"] = ocrErr.Message
	}

	switch ocrErr.Code {
	case ImageOCRCodeRuntimeUnavailable:
		return models.NewJobError(models.ErrorCodeRuntimeDepMissing, ocrErr.Code, ocrErr.Message, details)
	case ImageOCRCodeInputUnsupported:
		return models.NewJobError(models.ErrorCodeUnsupportedFormat, ocrErr.Code, ocrErr.Message, details)
	case ImageOCRCodeCancelled:
		return models.NewJobError(models.ErrorCodeCancelledByUser, ocrErr.Code, ocrErr.Message, details)
	case ImageOCRCodeLanguageUnavailable,
		ImageOCRCodeDPIInvalid,
		ImageOCRCodeInputRequired,
		ImageOCRCodeInputNotFound,
		ImageOCRCodeOutputInvalid,
		ImageOCRCodeOutputExists,
		ImageOCRCodeOutputDirInvalid:
		return models.NewJobError(models.ErrorCodeValidationInvalidInput, ocrErr.Code, ocrErr.Message, details)
	default:
		return models.NewJobError(models.ErrorCodeExecIOTransient, ocrErr.Code, ocrErr.Message, details)
	}
}

// imageOCRExecutionError maps a failed execution to a job error. Cancellation
// wins over the generic execution/item code so the job engine reports
// CANCELLED_BY_USER, including when the last batch item is interrupted.
func imageOCRExecutionError(ctx context.Context, detailCode, inputPath string, execErr error) (*models.JobError, bool) {
	if imageOCRIsCancellation(ctx, execErr) {
		return imageOCRCancelledJobError(ctx), true
	}

	details := map[string]any{"inputPath": inputPath}
	if execErr != nil {
		details["error"] = execErr.Error()
	}

	var ocrErr *ImageOCRError
	if errors.As(execErr, &ocrErr) {
		return imageOCREngineJobError(ocrErr, map[string]any{"inputPath": inputPath}), false
	}

	return models.NewCanonicalJobError(detailCode, "Could not OCR the image.", details), false
}

func imageOCRCancelledJobError(ctx context.Context) *models.JobError {
	var cause error = context.Canceled
	if ctx != nil && ctx.Err() != nil {
		cause = ctx.Err()
	}

	message := fmt.Errorf(OperationCancelledMsg, cause).Error()
	return models.NewCanonicalJobError(ImageOCRCodeCancelled, message, nil)
}
