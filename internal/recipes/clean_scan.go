package recipes

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/pdf/engine"
	"fileforge-desktop/internal/platform/fsx"
	"fileforge-desktop/internal/tools"
	"fileforge-desktop/internal/verify"
)

const (
	stepIDCleanScanOCR    = "ocr"
	stepIDCleanScanVerify = "verify"

	cleanScanOCRStepIndex    = 0
	cleanScanVerifyStepIndex = 1
	cleanScanStepCount       = 2

	cleanScanOutputSuffix = "_clean"
	cleanScanFallbackStem = "document"

	cleanScanSourcePageCountCheck = "source_page_count"
	cleanScanTextLayerCheck       = "text_layer"
	cleanScanTextSidecarCheck     = "text_sidecar"

	cleanScanOCRLanguageCheck = "ocr_language"
)

// cleanScanResolution is the recipe-owned plan: the source PDF, the searchable
// PDF and its text sidecar, the OCR language and the plan warnings. It is
// resolved during Plan and reused by Run.
type cleanScanResolution struct {
	SourcePath      string
	OutputDir       string
	PDFPath         string
	TextPath        string
	Language        string
	SourcePageCount int
	Warnings        []string
}

// ocrCapabilityProvider is implemented by the PDF OCR tool so the recipe reads
// the same capability detection as the tool page.
type ocrCapabilityProvider interface {
	OCRCapabilities(ctx context.Context) engine.OCRCapabilities
}

func cleanScanRecipe() models.RecipeV1 {
	return models.RecipeV1{
		ID:               RecipeIDCleanScan,
		Name:             "Clean scan",
		Description:      "Turn a scanned PDF into a searchable PDF with a local text sidecar.",
		Category:         "documents",
		InputKind:        "pdf",
		AcceptExtensions: []string{"pdf"},
		MaxInputs:        1,
		Highlights:       []string{"Local OCR", "Searchable PDF", "Text sidecar"},
		Steps: []models.RecipeStepV1{
			{
				ID:     stepIDCleanScanOCR,
				Name:   "OCR every page",
				ToolID: pdf.ToolIDPDFOCRV1,
				Detail: "Rasterize at 300 DPI and recognize text locally",
			},
			{
				ID:     stepIDCleanScanVerify,
				Name:   "Verify text layer",
				Detail: "Page count and extracted text",
			},
		},
	}
}

// resolveCleanScan validates the single PDF, the OCR runtime and language and
// resolves the collision-safe searchable PDF plus text sidecar.
func (s *Service) resolveCleanScan(recipe *models.RecipeV1, req models.RecipeRequestV1) (*resolvedRecipe, *models.JobErrorV1) {
	inputs, inputErr := validateCleanScanInputs(req.InputPaths)
	if inputErr != nil {
		return nil, inputErr
	}
	sourcePath := inputs[0]

	outputDir, dirErr := resolveRecipeOutputDir(req.OutputDir, sourcePath)
	if dirErr != nil {
		return nil, dirErr
	}

	capabilities, capsErr := s.cleanScanCapabilities()
	if capsErr != nil {
		return nil, capsErr
	}

	language, languageErr := cleanScanLanguageOption(req.Options, capabilities)
	if languageErr != nil {
		return nil, languageErr
	}

	sourcePages, pagesErr := api.PageCountFile(sourcePath)
	if pagesErr != nil || sourcePages <= 0 {
		return nil, newRecipeError(
			"RECIPE_INPUT_UNREADABLE",
			fmt.Sprintf("Could not read the PDF page count for %s.", filepath.Base(sourcePath)),
			map[string]any{"inputPath": sourcePath},
		)
	}

	base := cleanScanOutputBase(sourcePath)
	used := make(map[string]struct{}, 3)
	outputplan.MarkUsed(used, sourcePath)

	pdfPath, pdfAdjusted := outputplan.UniqueFilePath(outputDir, base, "pdf", used)
	textBase := strings.TrimSuffix(filepath.Base(pdfPath), filepath.Ext(pdfPath))
	textPath, textAdjusted := outputplan.UniqueFilePath(outputDir, textBase, "txt", used)

	warnings := make([]string, 0, 4)
	if pdfAdjusted {
		warnings = append(warnings, fmt.Sprintf("%s already exists. Saving as %s.", base+".pdf", filepath.Base(pdfPath)))
	}
	if textAdjusted {
		warnings = append(warnings, fmt.Sprintf("%s already exists. Saving as %s.", textBase+".txt", filepath.Base(textPath)))
	}
	warnings = append(warnings, "Pages are rasterized; vector text is replaced by an image with an invisible text layer.")
	if language != "eng" && language != "spa" {
		warnings = append(warnings, fmt.Sprintf(
			"Installed OCR language is %s; install eng/spa language data for better accuracy.",
			language,
		))
	}
	if !capabilities.PDFToText {
		warnings = append(warnings, engine.OCRWarningPDFToTextUnavailable)
	}

	planned := []models.PlannedOutputV1{
		{InputPath: sourcePath, OutputPath: pdfPath, OutputKind: "file", OutputCount: 1},
		{InputPath: sourcePath, OutputPath: textPath, OutputKind: "file", OutputCount: 1},
	}

	return &resolvedRecipe{
		Recipe:         recipe,
		Inputs:         inputs,
		OutputDir:      outputDir,
		PlannedOutputs: planned,
		PlanMessage:    fmt.Sprintf("Ready to make %s searchable.", filepath.Base(sourcePath)),
		Scan: &cleanScanResolution{
			SourcePath:      sourcePath,
			OutputDir:       outputDir,
			PDFPath:         pdfPath,
			TextPath:        textPath,
			Language:        language,
			SourcePageCount: sourcePages,
			Warnings:        warnings,
		},
		Warnings: warnings,
	}, nil
}

func validateCleanScanInputs(rawInputs []string) ([]string, *models.JobErrorV1) {
	if len(rawInputs) == 0 {
		return nil, newRecipeError("RECIPE_INVALID_INPUT", "Choose one PDF for Clean scan.", nil)
	}

	inputs := make([]string, 0, len(rawInputs))
	for _, rawInput := range rawInputs {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return nil, newRecipeError("RECIPE_INVALID_INPUT", "Every input must be a PDF file path.", nil)
		}
		inputs = append(inputs, inputPath)
	}

	if len(inputs) != 1 {
		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			"Clean scan takes exactly one PDF. Remove the extra files.",
			nil,
		)
	}

	inputPath := inputs[0]
	if !strings.EqualFold(filepath.Ext(inputPath), ".pdf") {
		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			fmt.Sprintf("Only .pdf files are supported: %s", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, newRecipeError(
				"RECIPE_INPUT_MISSING",
				fmt.Sprintf("Input PDF was not found: %s", filepath.Base(inputPath)),
				map[string]any{"inputPath": inputPath},
			)
		}

		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			fmt.Sprintf("Input PDF is not accessible: %s", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}
	if !info.Mode().IsRegular() {
		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			fmt.Sprintf("Input is not a regular file: %s", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}

	return inputs, nil
}

// cleanScanCapabilities asks the OCR tool for its runtime state so the plan and
// the tool page always agree on installed languages.
func (s *Service) cleanScanCapabilities() (engine.OCRCapabilities, *models.JobErrorV1) {
	tool, jobErr := s.toolFor(pdf.ToolIDPDFOCRV1)
	if jobErr != nil {
		return engine.OCRCapabilities{}, jobErr
	}

	provider, ok := tool.(ocrCapabilityProvider)
	if !ok {
		return engine.OCRCapabilities{}, newRecipeError(
			"RECIPE_TOOL_DEPENDENCY_MISSING",
			fmt.Sprintf("Tool '%s' cannot report its OCR capabilities.", pdf.ToolIDPDFOCRV1),
			map[string]any{"toolId": pdf.ToolIDPDFOCRV1},
		)
	}

	capabilities := provider.OCRCapabilities(context.Background())
	if !capabilities.Available() {
		return engine.OCRCapabilities{}, newRecipeError(
			"RECIPE_OCR_UNAVAILABLE",
			pdf.OCRUnavailableReason(capabilities),
			map[string]any{
				"pdftoppm":  capabilities.PDFToPPM,
				"tesseract": capabilities.Tesseract,
				"languages": capabilities.Languages,
			},
		)
	}

	return capabilities, nil
}

// cleanScanLanguageOption reads options.language, defaulting to the recommended
// installed language and rejecting an uninstalled one.
func cleanScanLanguageOption(options map[string]any, capabilities engine.OCRCapabilities) (string, *models.JobErrorV1) {
	if options != nil {
		if raw, exists := options["language"]; exists && raw != nil {
			value, ok := raw.(string)
			if !ok {
				return "", newRecipeError("RECIPE_INVALID_LANGUAGE", "options.language must be a string.", nil)
			}
			language := strings.TrimSpace(value)
			if language != "" && !capabilities.HasLanguage(language) {
				return "", newRecipeError(
					cleanScanOCRLanguageCheck,
					fmt.Sprintf(
						"Tesseract language '%s' is not installed. Installed languages: %s.",
						language,
						strings.Join(capabilities.Languages, ", "),
					),
					map[string]any{"language": language, "languages": capabilities.Languages},
				)
			}
			if language != "" {
				return language, nil
			}
		}
	}

	return capabilities.RecommendedLanguage, nil
}

func cleanScanOutputBase(sourcePath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath)))
	if stem == "" {
		stem = cleanScanFallbackStem
	}

	return stem + cleanScanOutputSuffix
}

func (s *Service) executeCleanScan(ctx context.Context, tracked *recipeJob, resolved *resolvedRecipe) {
	scan := resolved.Scan
	if scan == nil || len(resolved.Recipe.Steps) != cleanScanStepCount {
		s.failJob(tracked, newRecipeError("RECIPE_INVALID_DEFINITION", "The Clean scan recipe definition is invalid.", nil))
		return
	}
	if ctx.Err() != nil {
		s.cancelJob(tracked)
		return
	}

	ocrStep := resolved.Recipe.Steps[cleanScanOCRStepIndex]
	verifyStep := resolved.Recipe.Steps[cleanScanVerifyStepIndex]

	tracked.begin(1)
	s.emitProgress(tracked, models.JobStatusRunning, ocrStep.Name, "starting", fmt.Sprintf("Starting %s.", resolved.Recipe.Name), 5)

	// created tracks every path this run may have written. It is only used to
	// clean up on failure or cancel, so a pre-existing file is never removed.
	created := make([]string, 0, 3)

	pdfPath, textPath, ocrMessage, jobErr := s.runCleanScanOCRStep(ctx, tracked, scan, ocrStep, &created)
	if jobErr != nil {
		removeCreatedFiles(created)
		if isCancellation(ctx, jobErr) {
			s.cancelJob(tracked)
			return
		}
		tracked.setStep(cleanScanOCRStepIndex, failedRecipeStep(ocrStep, jobErr))
		s.failJob(tracked, jobErr)
		return
	}

	s.emitProgress(tracked, models.JobStatusRunning, ocrStep.Name, "ocr", ocrMessage, 80)

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}

	tracked.setCurrentStep(cleanScanVerifyStepIndex + 1)
	s.emitProgress(tracked, models.JobStatusRunning, verifyStep.Name, "verifying", "Verifying page count and text layer.", 90)

	outputs := []string{pdfPath}
	if textPath != "" {
		outputs = append(outputs, textPath)
	}

	report := buildCleanScanReport(ctx, scan, pdfPath, textPath)
	verifyMessage := cleanScanVerifyMessage(report)

	status := models.JobStatusSuccess
	message := fmt.Sprintf("Clean scan ready: %s and %s.", filepath.Base(pdfPath), filepath.Base(textPath))
	if !report.Success {
		status = models.JobStatusPartialSuccess
		message = cleanScanPartialMessage(report, scan, pdfPath)
	}

	if s.history != nil {
		s.history.RecordOutputs(tracked.jobID(), cleanScanHistoryToolID, outputs)
	}

	tracked.setStep(cleanScanVerifyStepIndex, models.RecipeStepResultV1{
		ID:      verifyStep.ID,
		Name:    verifyStep.Name,
		ToolID:  verifyStep.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: verifyMessage,
		Outputs: outputs,
	})

	tracked.finish(status, message, outputs, &report, nil, true)
	s.emitProgress(tracked, status, verifyStep.Name, status, message, 100)
}

// runCleanScanOCRStep plans and runs the OCR tool and returns the paths it
// actually wrote. Both final paths are claimed for cleanup before the tool can
// write a partial file.
func (s *Service) runCleanScanOCRStep(ctx context.Context, tracked *recipeJob, scan *cleanScanResolution, step models.RecipeStepV1, created *[]string) (string, string, string, *models.JobErrorV1) {
	executor, planner, jobErr := s.singleExecutor(pdf.ToolIDPDFOCRV1)
	if jobErr != nil {
		return "", "", "", jobErr
	}

	for _, path := range []string{scan.PDFPath, scan.TextPath} {
		if strings.TrimSpace(path) == "" {
			continue
		}
		if _, statErr := os.Stat(path); statErr == nil {
			return "", "", "", newRecipeError(
				"RECIPE_OUTPUT_EXISTS",
				fmt.Sprintf("%s already exists. Choose another output folder.", filepath.Base(path)),
				map[string]any{"outputPath": path},
			)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return "", "", "", newRecipeError(
				"RECIPE_STEP_FAILED",
				fmt.Sprintf("%s cannot be accessed.", filepath.Base(path)),
				map[string]any{"outputPath": path},
			)
		}
		*created = append(*created, path)
	}

	req := cleanScanOCRRequest(scan)
	if _, planErr := planner.Plan(ctx, req); planErr != nil {
		return "", "", "", planErr
	}

	progress := func(progress models.JobProgressV1) {
		percent := 10
		if progress.Total > 0 {
			percent = 10 + int(math.Round(float64(progress.Current)/float64(progress.Total)*70))
			if percent > 80 {
				percent = 80
			}
		}
		s.emitProgress(tracked, models.JobStatusRunning, step.Name, "ocr", progress.Message, percent)
	}

	var item models.JobResultItemV1
	if progressExecutor, ok := executor.(tools.SingleExecutorWithProgress); ok {
		item, jobErr = progressExecutor.ExecuteSingleWithProgress(ctx, req, progress)
	} else {
		s.emitProgress(tracked, models.JobStatusRunning, step.Name, "ocr", "Recognizing text locally.", 15)
		item, jobErr = executor.ExecuteSingle(ctx, req)
	}
	if jobErr != nil {
		return "", "", "", jobErr
	}
	if !item.Success {
		return "", "", "", stepItemError(item, pdf.ToolIDPDFOCRV1)
	}

	pdfPath := strings.TrimSpace(item.OutputPath)
	if pdfPath == "" {
		pdfPath = strings.TrimSpace(scan.PDFPath)
	}
	if pdfPath == "" {
		return "", "", "", newRecipeError("RECIPE_STEP_FAILED", "The OCR step did not report an output.", nil)
	}
	if pdfPath != scan.PDFPath {
		*created = append(*created, pdfPath)
	}

	textPath := cleanScanItemTextPath(item)
	if textPath == "" {
		textPath = scan.TextPath
	}
	if textPath != scan.TextPath {
		*created = append(*created, textPath)
	}

	if !fsx.IsRegularFile(pdfPath) {
		return "", "", "", newRecipeError(
			"RECIPE_OUTPUT_MISSING",
			fmt.Sprintf("The OCR step finished but the output is missing: %s", filepath.Base(pdfPath)),
			map[string]any{"outputPath": pdfPath},
		)
	}

	tracked.setStep(cleanScanOCRStepIndex, models.RecipeStepResultV1{
		ID:      step.ID,
		Name:    step.Name,
		ToolID:  step.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: cleanScanOCRStepMessage(item.Message),
		Outputs: cleanScanStepOutputs(pdfPath, textPath),
	})

	return pdfPath, textPath, item.Message, nil
}

func cleanScanOCRRequest(scan *cleanScanResolution) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     pdf.ToolIDPDFOCRV1,
		Mode:       "single",
		InputPaths: []string{scan.SourcePath},
		OutputDir:  scan.OutputDir,
		Options: map[string]any{
			"language":    scan.Language,
			"textSidecar": true,
			"outputPath":  scan.PDFPath,
		},
	}
}

// cleanScanItemTextPath picks the text sidecar path from the OCR result.
func cleanScanItemTextPath(item models.JobResultItemV1) string {
	for _, output := range item.Outputs {
		trimmed := strings.TrimSpace(output)
		if trimmed != "" && strings.EqualFold(filepath.Ext(trimmed), ".txt") {
			return trimmed
		}
	}

	return ""
}

func cleanScanStepOutputs(pdfPath, textPath string) []string {
	outputs := []string{pdfPath}
	if strings.TrimSpace(textPath) != "" {
		outputs = append(outputs, textPath)
	}

	return outputs
}

func cleanScanOCRStepMessage(toolMessage string) string {
	trimmed := strings.TrimSpace(toolMessage)
	if trimmed == "" {
		return "OCR finished."
	}

	return trimmed
}

// buildCleanScanReport verifies the generic output checks plus the recipe
// guarantees: the page count matches the source, the PDF has a non-empty text
// layer and the sidecar contains text.
func buildCleanScanReport(ctx context.Context, scan *cleanScanResolution, pdfPath, textPath string) models.VerificationReportV1 {
	report := verify.VerifyPaths(cleanScanStepOutputs(pdfPath, textPath))
	failed := false

	finalPages, pageErr := api.PageCountFile(pdfPath)
	pagePassed := pageErr == nil && finalPages == scan.SourcePageCount
	pageDetail := ""
	if !pagePassed {
		failed = true
		pageDetail = fmt.Sprintf("expected %d pages", scan.SourcePageCount)
		report.Warnings = append(report.Warnings, fmt.Sprintf(
			"%s: expected %d pages, got %d",
			filepath.Base(pdfPath),
			scan.SourcePageCount,
			finalPages,
		))
	}
	appendCleanScanCheck(&report, pdfPath, models.VerificationCheckV1{
		Key:    cleanScanSourcePageCountCheck,
		Label:  "Source page count",
		Passed: pagePassed,
		Value:  fmt.Sprintf("%d pages", finalPages),
		Detail: pageDetail,
	})

	layerPassed := false
	layerValue := ""
	extractedText, pdfToTextAvailable := engine.ExtractPDFText(ctx, nil, pdfPath)
	if pdfToTextAvailable {
		layerCharacters := engine.CountTextCharacters(extractedText)
		layerPassed = layerCharacters > 0
		layerValue = fmt.Sprintf("%d characters", layerCharacters)
	} else {
		report.Warnings = append(report.Warnings, "pdftotext is not available; the text layer was verified from the text sidecar.")
		if sidecar, readErr := os.ReadFile(textPath); readErr == nil {
			layerCharacters := engine.CountTextCharacters(string(sidecar))
			layerPassed = layerCharacters > 0
			layerValue = fmt.Sprintf("%d characters", layerCharacters)
		}
	}
	if !layerPassed {
		failed = true
		report.Warnings = append(report.Warnings, engine.OCRWarningNoText)
	}
	appendCleanScanCheck(&report, pdfPath, models.VerificationCheckV1{
		Key:    cleanScanTextLayerCheck,
		Label:  "Text layer",
		Passed: layerPassed,
		Value:  layerValue,
	})

	sidecarPassed := false
	sidecarValue := ""
	if sidecar, readErr := os.ReadFile(textPath); readErr == nil {
		sidecarCharacters := engine.CountTextCharacters(string(sidecar))
		sidecarPassed = sidecarCharacters > 0
		sidecarValue = fmt.Sprintf("%d characters", sidecarCharacters)
	}
	if !sidecarPassed {
		failed = true
		report.Warnings = append(report.Warnings, fmt.Sprintf("%s: no text was recognized in the sidecar", filepath.Base(textPath)))
	}
	appendCleanScanCheck(&report, textPath, models.VerificationCheckV1{
		Key:    cleanScanTextSidecarCheck,
		Label:  "Text sidecar",
		Passed: sidecarPassed,
		Value:  sidecarValue,
	})

	if failed {
		report.Success = false
		report.Message = "verification failed: the text layer does not match the plan"
	}

	return report
}

// appendCleanScanCheck attaches a check to the verified output with the same
// path. A missing entry falls back to the first output so a check is never
// silently dropped.
func appendCleanScanCheck(report *models.VerificationReportV1, path string, check models.VerificationCheckV1) {
	key := canonicalInputKey(path)
	for index := range report.Outputs {
		if canonicalInputKey(report.Outputs[index].Path) == key {
			report.Outputs[index].Checks = append(report.Outputs[index].Checks, check)
			return
		}
	}

	if len(report.Outputs) > 0 {
		report.Outputs[0].Checks = append(report.Outputs[0].Checks, check)
	}
}

func cleanScanVerifyMessage(report models.VerificationReportV1) string {
	if report.Success {
		return "Page count and text layer verified."
	}

	return "Verification found: " + report.Message + "."
}

func cleanScanPartialMessage(report models.VerificationReportV1, scan *cleanScanResolution, pdfPath string) string {
	base := filepath.Base(pdfPath)
	for _, output := range report.Outputs {
		for _, check := range output.Checks {
			if check.Passed {
				continue
			}

			switch check.Key {
			case cleanScanTextLayerCheck:
				return fmt.Sprintf("Clean scan finished: %s was saved, but no searchable text was detected.", base)
			case cleanScanSourcePageCountCheck:
				return fmt.Sprintf("Clean scan finished: %s was saved, but the page count does not match the %d-page source.", base, scan.SourcePageCount)
			case cleanScanTextSidecarCheck:
				return fmt.Sprintf("Clean scan finished: %s was saved, but the text sidecar is empty.", base)
			}
		}
	}

	return fmt.Sprintf("Clean scan finished, but verification reported: %s. The outputs were kept.", report.Message)
}
