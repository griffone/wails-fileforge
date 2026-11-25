package recipes

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/platform/fsx"
	"fileforge-desktop/internal/tools"
	"fileforge-desktop/internal/verify"
)

const (
	stepIDMergePDFs      = "merge"
	stepIDOptimizeTarget = "optimize"

	mergeStepIndex    = 0
	optimizeStepIndex = 1
	emailPDFStepCount = 2

	emailPDFDefaultTargetBytes = 10 * 1024 * 1024

	// bytesPerMegabyte converts the declared MB unit to canonical bytes.
	bytesPerMegabyte = 1024 * 1024

	// emailPDFTargetMinBytes and emailPDFTargetMaxBytes bound the declared
	// target-size option. They are stored in bytes; the schema unit is MB.
	emailPDFTargetMinBytes = 1 * 1024 * 1024
	emailPDFTargetMaxBytes = 100 * 1024 * 1024

	recipeStepStatusPending  = "pending"
	recipeStepStatusSkipped  = "skipped"
	recipeStepStatusFailed   = models.JobStatusFailed
	recipeStepStatusSuccess  = models.JobStatusSuccess
	recipeTempDirPattern     = "fileforge-recipe-*"
	recipeWriteCheckPattern  = ".fileforge-recipe-writecheck-*.tmp"
	emailPDFOutputSuffix     = "_email"
	emailPDFFallbackStem     = "document"
	emailPDFTargetOptionKey  = "targetSizeBytes"
	recipePlanFailureMessage = "recipe plan failed"
)

// resolvedRecipe is a validated request: inputs, destination, planned outputs
// and the plan warnings. It contains no intermediate paths. Web is set only for
// the Web image set recipe, Social for the Social clip recipe, Scan for the
// Clean scan recipe and Burn for the Burned-in subtitles recipe, whose plans are
// fully resolved before execution.
type resolvedRecipe struct {
	// Recipe is the recipe shown to the caller: the built-in definition for a
	// curated recipe, or the user recipe (base metadata plus its own id and
	// name) for a stored duplicate.
	Recipe *models.Recipe
	// BaseRecipe is the built-in definition that owns the runnable chain and
	// decides which executor handles the run.
	BaseRecipe      *models.Recipe
	Inputs          []string
	OutputDir       string
	TargetSizeBytes int64
	FinalPath       string
	PlannedOutputs  []models.PlannedOutput
	PlanMessage     string
	Web             *webImageSetResolution
	Social          *socialClipResolution
	Scan            *cleanScanResolution
	Burn            *burnedSubtitlesResolution
	Warnings        []string
}

func emailPDFRecipe() models.Recipe {
	return models.Recipe{
		ID:          RecipeIDEmailPDF,
		Name:        "Email PDF",
		Description: "Merge one or more PDFs and compress the result to a size that is easy to send by email.",
		Category:    "documents",
		InputKind:   "pdf",
		AcceptExtensions: []string{
			"pdf",
		},
		MaxInputs:  0,
		Highlights: []string{"Merge when needed", "Target size", "Verified + undo"},
		Steps: []models.RecipeStep{
			{
				ID:     stepIDMergePDFs,
				Name:   "Merge PDFs",
				ToolID: pdf.ToolIDPDFMerge,
				Detail: "Only when there are two or more PDFs",
			},
			{
				ID:     stepIDOptimizeTarget,
				Name:   "Optimize to target",
				ToolID: pdf.ToolIDPDFOptimize,
				Detail: "Ghostscript when the target needs it",
			},
		},
		DefaultTargetSizeBytes: emailPDFDefaultTargetBytes,
		OptionSchema:           emailPDFOptionSchema(),
		Duplicable:             true,
	}
}

// emailPDFOptionSchema declares the only knob Email PDF can honour today: the
// best-effort target size. The canonical stored value stays in bytes, matching
// the optimize tool; the UI renders it in MB through Scale.
func emailPDFOptionSchema() []models.RecipeOptionField {
	minimum := float64(emailPDFTargetMinBytes)
	maximum := float64(emailPDFTargetMaxBytes)

	return []models.RecipeOptionField{{
		Key:     emailPDFTargetOptionKey,
		Label:   "Target size",
		Kind:    models.RecipeOptionInteger,
		Unit:    "MB",
		Min:     &minimum,
		Max:     &maximum,
		Scale:   bytesPerMegabyte,
		Default: emailPDFDefaultTargetBytes,
		Help:    "Best effort: the output may stay above the target.",
	}}
}

func recipeByID(recipeID string) *models.Recipe {
	trimmed := strings.TrimSpace(recipeID)
	for _, recipe := range []models.Recipe{emailPDFRecipe(), webImageSetRecipe(), socialClipRecipe(), cleanScanRecipe(), burnSubtitlesRecipe()} {
		if recipe.ID == trimmed {
			candidate := recipe
			return &candidate
		}
	}

	return nil
}

func resolveEmailPDF(recipe *models.Recipe, req models.RecipeRequest) (*resolvedRecipe, *models.JobError) {
	inputs, inputErr := validateEmailPDFInputs(req.InputPaths)
	if inputErr != nil {
		return nil, inputErr
	}

	targetSizeBytes, targetErr := emailPDFTargetSize(req.Options, recipe.DefaultTargetSizeBytes)
	if targetErr != nil {
		return nil, targetErr
	}

	outputDir, dirErr := resolveRecipeOutputDir(req.OutputDir, inputs[0])
	if dirErr != nil {
		return nil, dirErr
	}

	base := emailPDFOutputBase(inputs[0])
	finalPath, adjusted := outputplan.UniqueFilePath(outputDir, base, "pdf", nil)

	warnings := make([]string, 0, 2)
	if adjusted {
		warnings = append(warnings, fmt.Sprintf("%s already exists. Saving as %s.", base+".pdf", filepath.Base(finalPath)))
	}
	warnings = append(warnings, fmt.Sprintf(
		"Target compression is best effort: the output may stay above %s.",
		formatByteSize(targetSizeBytes),
	))

	return &resolvedRecipe{
		Recipe:          recipe,
		Inputs:          inputs,
		OutputDir:       outputDir,
		TargetSizeBytes: targetSizeBytes,
		FinalPath:       finalPath,
		PlannedOutputs: []models.PlannedOutput{{
			InputPath:   strings.Join(inputs, ","),
			OutputPath:  finalPath,
			OutputKind:  "file",
			OutputCount: 1,
		}},
		PlanMessage: emailPDFPlanMessage(len(inputs)),
		Warnings:    warnings,
	}, nil
}

func validateEmailPDFInputs(rawInputs []string) ([]string, *models.JobError) {
	if len(rawInputs) == 0 {
		return nil, newRecipeError("RECIPE_INVALID_INPUT", "Choose at least one PDF for Email PDF.", nil)
	}

	inputs := make([]string, 0, len(rawInputs))
	seen := make(map[string]struct{}, len(rawInputs))
	for _, rawInput := range rawInputs {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return nil, newRecipeError("RECIPE_INVALID_INPUT", "Every input must be a PDF file path.", nil)
		}
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

		key := canonicalInputKey(inputPath)
		if _, exists := seen[key]; exists {
			return nil, newRecipeError(
				"RECIPE_INVALID_INPUT",
				fmt.Sprintf("Duplicate input: %s", filepath.Base(inputPath)),
				map[string]any{"inputPath": inputPath},
			)
		}
		seen[key] = struct{}{}
		inputs = append(inputs, inputPath)
	}

	return inputs, nil
}

// resolveRecipeOutputDir validates the destination folder shared by every
// recipe: an empty value defaults to the first input's folder and the folder
// must exist and accept a write probe.
func resolveRecipeOutputDir(rawDir, firstInput string) (string, *models.JobError) {
	dir := strings.TrimSpace(rawDir)
	if dir == "" {
		dir = filepath.Dir(firstInput)
	}
	dir = filepath.Clean(dir)

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", newRecipeError("RECIPE_INVALID_OUTPUT_DIR", fmt.Sprintf("Output folder does not exist: %s", dir), nil)
		}
		return "", newRecipeError("RECIPE_INVALID_OUTPUT_DIR", fmt.Sprintf("Output folder is not accessible: %s", dir), nil)
	}
	if !info.IsDir() {
		return "", newRecipeError("RECIPE_INVALID_OUTPUT_DIR", fmt.Sprintf("Output path is not a folder: %s", dir), nil)
	}

	probe, probeErr := os.CreateTemp(dir, recipeWriteCheckPattern)
	if probeErr != nil {
		return "", newRecipeError("RECIPE_INVALID_OUTPUT_DIR", fmt.Sprintf("Output folder is not writable: %s", dir), nil)
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)

	return dir, nil
}

// emailPDFTargetSize reads options.targetSizeBytes, accepting integer and
// floating point values (JSON numbers arrive as float64). Absent or null keeps
// the recipe default.
func emailPDFTargetSize(options map[string]any, defaultTargetBytes int64) (int64, *models.JobError) {
	if options == nil {
		return defaultRecipeTarget(defaultTargetBytes)
	}

	raw, exists := options[emailPDFTargetOptionKey]
	if !exists || raw == nil {
		return defaultRecipeTarget(defaultTargetBytes)
	}

	value, ok := numericOption(raw)
	if !ok || math.IsNaN(value) || math.IsInf(value, 0) || value <= 0 {
		return 0, newRecipeError("RECIPE_INVALID_TARGET", "options.targetSizeBytes must be a positive number of bytes.", nil)
	}
	if math.Trunc(value) != value {
		return 0, newRecipeError("RECIPE_INVALID_TARGET", "options.targetSizeBytes must be a whole number of bytes.", nil)
	}
	if value > float64(math.MaxInt64) {
		return 0, newRecipeError("RECIPE_INVALID_TARGET", "options.targetSizeBytes is too large.", nil)
	}

	return int64(value), nil
}

func defaultRecipeTarget(defaultTargetBytes int64) (int64, *models.JobError) {
	if defaultTargetBytes <= 0 {
		return 0, newRecipeError("RECIPE_INVALID_TARGET", "The recipe has no valid default target size.", nil)
	}

	return defaultTargetBytes, nil
}

func numericOption(raw any) (float64, bool) {
	switch value := raw.(type) {
	case int:
		return float64(value), true
	case int8:
		return float64(value), true
	case int16:
		return float64(value), true
	case int32:
		return float64(value), true
	case int64:
		return float64(value), true
	case uint:
		return float64(value), true
	case uint8:
		return float64(value), true
	case uint16:
		return float64(value), true
	case uint32:
		return float64(value), true
	case uint64:
		return float64(value), true
	case float32:
		return float64(value), true
	case float64:
		return value, true
	case json.Number:
		parsed, err := value.Float64()
		if err != nil {
			return 0, false
		}
		return parsed, true
	default:
		return 0, false
	}
}

func emailPDFOutputBase(firstInput string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(firstInput), filepath.Ext(firstInput)))
	if stem == "" {
		stem = emailPDFFallbackStem
	}

	return stem + emailPDFOutputSuffix
}

func emailPDFPlanMessage(inputCount int) string {
	if inputCount == 1 {
		return "Ready to email 1 PDF."
	}

	return fmt.Sprintf("Ready to email %d PDFs.", inputCount)
}

// optimizePlanWarnings asks the optimize tool to plan the final step. Plan is
// side-effect free apart from a temporary write probe, and it contributes its
// best-effort and Ghostscript warnings to the recipe plan.
func (s *Service) optimizePlanWarnings(resolved *resolvedRecipe) ([]string, *models.JobError) {
	tool, jobErr := s.toolFor(pdf.ToolIDPDFOptimize)
	if jobErr != nil {
		return nil, jobErr
	}

	planner, ok := tool.(tools.Planner)
	if !ok {
		return nil, newRecipeError(
			"RECIPE_TOOL_DEPENDENCY_MISSING",
			fmt.Sprintf("Tool '%s' cannot plan its output.", pdf.ToolIDPDFOptimize),
			map[string]any{"toolId": pdf.ToolIDPDFOptimize},
		)
	}

	plan, planErr := planner.Plan(context.Background(), optimizeEmailPDFRequest(resolved, resolved.Inputs[0], resolved.FinalPath))
	if planErr != nil {
		return nil, planErr
	}

	// The recipe owns the headline best-effort target warning; keep the
	// remaining tool warnings (for example a missing Ghostscript) and drop the
	// duplicate target note.
	warnings := make([]string, 0, len(plan.Warnings))
	for _, warning := range plan.Warnings {
		if strings.HasPrefix(strings.ToLower(warning), "compression to") {
			continue
		}
		warnings = append(warnings, warning)
	}

	return warnings, nil
}

// optimizeEmailPDFRequest builds the final step request: the merge output (or
// the original PDF) is optimized into the explicit collision-safe destination.
func optimizeEmailPDFRequest(resolved *resolvedRecipe, sourcePath, outputPath string) models.JobRequest {
	return models.JobRequest{
		ToolID:     pdf.ToolIDPDFOptimize,
		Mode:       "single",
		InputPaths: []string{sourcePath},
		OutputDir:  resolved.OutputDir,
		Options: map[string]any{
			"compress":        true,
			"targetSizeBytes": resolved.TargetSizeBytes,
			"outputPath":      outputPath,
		},
	}
}

func (s *Service) executeEmailPDF(ctx context.Context, tracked *recipeJob, resolved *resolvedRecipe) {
	if len(resolved.Recipe.Steps) != emailPDFStepCount {
		s.failJob(tracked, newRecipeError("RECIPE_INVALID_DEFINITION", "The Email PDF recipe definition is invalid.", nil))
		return
	}
	if ctx.Err() != nil {
		s.cancelJob(tracked)
		return
	}

	mergeStep := resolved.Recipe.Steps[mergeStepIndex]
	optimizeStep := resolved.Recipe.Steps[optimizeStepIndex]

	tracked.begin(1)
	s.emitProgress(tracked, models.JobStatusRunning, mergeStep.Name, "starting", fmt.Sprintf("Starting %s.", resolved.Recipe.Name), 5)

	tempDir, tempErr := s.tempDir()
	if tempErr != nil {
		s.failJob(tracked, newRecipeError(
			"RECIPE_TEMP_WORKSPACE_FAILED",
			fmt.Sprintf("Could not create the recipe workspace: %v", tempErr),
			nil,
		))
		return
	}
	s.setWorkspace(tracked, tempDir)
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	sourcePath := resolved.Inputs[0]
	if len(resolved.Inputs) > 1 {
		s.emitProgress(tracked, models.JobStatusRunning, mergeStep.Name, "running", fmt.Sprintf("Merging %d PDFs.", len(resolved.Inputs)), 15)

		mergedPath, mergeErr := s.runEmailPDFMergeStep(ctx, resolved.Inputs, tempDir)
		if mergeErr != nil {
			if isCancellation(ctx, mergeErr) {
				s.cancelJob(tracked)
				return
			}
			tracked.setStep(mergeStepIndex, failedRecipeStep(mergeStep, mergeErr))
			s.failJob(tracked, mergeErr)
			return
		}

		sourcePath = mergedPath
		tracked.setStep(mergeStepIndex, models.RecipeStepResult{
			ID:      mergeStep.ID,
			Name:    mergeStep.Name,
			ToolID:  mergeStep.ToolID,
			Status:  recipeStepStatusSuccess,
			Message: fmt.Sprintf("Merged %d PDFs.", len(resolved.Inputs)),
			Outputs: []string{mergedPath},
		})
		tracked.setCurrentStep(optimizeStepIndex + 1)
		s.emitProgress(tracked, models.JobStatusRunning, optimizeStep.Name, "merged", fmt.Sprintf("Merged %d PDFs.", len(resolved.Inputs)), 40)
	} else {
		tracked.setStep(mergeStepIndex, models.RecipeStepResult{
			ID:      mergeStep.ID,
			Name:    mergeStep.Name,
			ToolID:  mergeStep.ToolID,
			Status:  recipeStepStatusSkipped,
			Message: "Using the original PDF",
		})
		tracked.setCurrentStep(optimizeStepIndex + 1)
		s.emitProgress(tracked, models.JobStatusRunning, optimizeStep.Name, "prepared", "Using the original PDF.", 40)
	}

	if ctx.Err() != nil {
		s.cancelJob(tracked)
		return
	}

	s.emitProgress(
		tracked,
		models.JobStatusRunning,
		optimizeStep.Name,
		"running",
		fmt.Sprintf("Optimizing to %s.", formatByteSize(resolved.TargetSizeBytes)),
		45,
	)

	finalPath, optimizeMessage, optimizeErr := s.runEmailPDFOptimizeStep(ctx, sourcePath, resolved)
	if optimizeErr != nil {
		if isCancellation(ctx, optimizeErr) {
			s.cancelJob(tracked)
			return
		}
		tracked.setStep(optimizeStepIndex, failedRecipeStep(optimizeStep, optimizeErr))
		s.failJob(tracked, optimizeErr)
		return
	}

	tracked.setStep(optimizeStepIndex, models.RecipeStepResult{
		ID:      optimizeStep.ID,
		Name:    optimizeStep.Name,
		ToolID:  optimizeStep.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: optimizeMessage,
		Outputs: []string{finalPath},
	})

	if ctx.Err() != nil {
		s.cancelJob(tracked)
		return
	}

	s.emitProgress(tracked, models.JobStatusRunning, optimizeStep.Name, "verifying", "Verifying the output.", 90)

	if !fsx.IsRegularFile(finalPath) {
		missingErr := newRecipeError(
			"RECIPE_OUTPUT_MISSING",
			fmt.Sprintf("The recipe finished but the output is missing: %s", filepath.Base(finalPath)),
			map[string]any{"outputPath": finalPath},
		)
		tracked.setStep(optimizeStepIndex, failedRecipeStep(optimizeStep, missingErr))
		s.failJob(tracked, missingErr)
		return
	}

	report := verify.VerifyPathsWithTarget([]string{finalPath}, resolved.TargetSizeBytes)

	if s.history != nil {
		s.history.RecordOutputs(tracked.jobID(), emailPDFHistoryToolID, []string{finalPath})
	}

	if report.Success && !targetNotMet(report, resolved.TargetSizeBytes) {
		message := fmt.Sprintf("Email PDF ready: %s.", filepath.Base(finalPath))
		tracked.finish(models.JobStatusSuccess, message, []string{finalPath}, &report, nil, true)
		s.emitProgress(tracked, models.JobStatusSuccess, optimizeStep.Name, models.JobStatusSuccess, message, 100)
		return
	}

	message := emailPDFResultMessage(finalPath, resolved.TargetSizeBytes, report)
	tracked.finish(models.JobStatusPartialSuccess, message, []string{finalPath}, &report, nil, true)
	s.emitProgress(tracked, models.JobStatusPartialSuccess, optimizeStep.Name, models.JobStatusPartialSuccess, message, 100)
}

func (s *Service) runEmailPDFMergeStep(ctx context.Context, inputPaths []string, tempDir string) (string, *models.JobError) {
	executor, planner, jobErr := s.singleExecutor(pdf.ToolIDPDFMerge)
	if jobErr != nil {
		return "", jobErr
	}

	req := models.JobRequest{
		ToolID:     pdf.ToolIDPDFMerge,
		Mode:       "single",
		InputPaths: inputPaths,
		OutputDir:  tempDir,
		Options:    map[string]any{},
	}

	plan, planErr := planner.Plan(ctx, req)
	if planErr != nil {
		return "", planErr
	}

	item, execErr := executor.ExecuteSingle(ctx, req)
	if execErr != nil {
		return "", execErr
	}
	if !item.Success {
		return "", stepItemError(item, pdf.ToolIDPDFMerge)
	}

	outputPath := strings.TrimSpace(item.OutputPath)
	if outputPath == "" && len(plan.Outputs) > 0 {
		outputPath = strings.TrimSpace(plan.Outputs[0].OutputPath)
	}
	if outputPath == "" {
		return "", newRecipeError("RECIPE_STEP_FAILED", "The PDF merge step did not report an output.", nil)
	}

	return outputPath, nil
}

func (s *Service) runEmailPDFOptimizeStep(ctx context.Context, sourcePath string, resolved *resolvedRecipe) (string, string, *models.JobError) {
	executor, planner, jobErr := s.singleExecutor(pdf.ToolIDPDFOptimize)
	if jobErr != nil {
		return "", "", jobErr
	}

	finalPath := resolved.FinalPath
	if _, statErr := os.Stat(finalPath); statErr == nil {
		// The planned name was taken after planning; claim the next free name
		// instead of failing the whole run.
		if unique, _ := outputplan.UniqueFilePath(resolved.OutputDir, emailPDFOutputBase(resolved.Inputs[0]), "pdf", nil); unique != "" {
			finalPath = unique
		}
	}

	req := optimizeEmailPDFRequest(resolved, sourcePath, finalPath)
	plan, planErr := planner.Plan(ctx, req)
	if planErr != nil {
		return "", "", planErr
	}

	item, execErr := executor.ExecuteSingle(ctx, req)
	if execErr != nil {
		return "", "", execErr
	}
	if !item.Success {
		return "", "", stepItemError(item, pdf.ToolIDPDFOptimize)
	}

	outputPath := strings.TrimSpace(item.OutputPath)
	if outputPath == "" && len(plan.Outputs) > 0 {
		outputPath = strings.TrimSpace(plan.Outputs[0].OutputPath)
	}
	if outputPath == "" {
		return "", "", newRecipeError("RECIPE_STEP_FAILED", "The PDF optimize step did not report an output.", nil)
	}

	return outputPath, item.Message, nil
}

func emailPDFResultMessage(finalPath string, targetSizeBytes int64, report models.VerificationReport) string {
	base := filepath.Base(finalPath)
	if targetNotMet(report, targetSizeBytes) {
		return fmt.Sprintf(
			"Email PDF ready: %s, but the %s target could not be met (best effort).",
			base,
			formatByteSize(targetSizeBytes),
		)
	}

	return fmt.Sprintf("Email PDF ready: %s, but verification reported: %s.", base, report.Message)
}

func targetNotMet(report models.VerificationReport, targetSizeBytes int64) bool {
	if targetSizeBytes <= 0 {
		return false
	}
	for _, output := range report.Outputs {
		if output.Exists && output.SizeBytes > targetSizeBytes {
			return true
		}
	}

	return false
}

func failedRecipeStep(step models.RecipeStep, jobErr *models.JobError) models.RecipeStepResult {
	result := models.RecipeStepResult{
		ID:     step.ID,
		Name:   step.Name,
		ToolID: step.ToolID,
		Status: recipeStepStatusFailed,
		Error:  jobErr,
	}
	if jobErr != nil {
		result.Message = jobErr.Message
	}

	return result
}

func stepItemError(item models.JobResultItem, toolID string) *models.JobError {
	if item.Error != nil {
		return item.Error
	}

	message := strings.TrimSpace(item.Message)
	if message == "" {
		message = fmt.Sprintf("The '%s' step failed.", toolID)
	}

	return newRecipeError("RECIPE_STEP_FAILED", message, map[string]any{"toolId": toolID})
}

func isCancellation(ctx context.Context, jobErr *models.JobError) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}
	if jobErr == nil {
		return false
	}

	codes := strings.ToUpper(jobErr.Code + " " + jobErr.DetailCode)
	return strings.Contains(codes, "CANCEL")
}

func recipePlanFailure(jobErr *models.JobError) models.RecipePlanResponse {
	message := recipePlanFailureMessage
	if jobErr != nil && jobErr.Message != "" {
		message = jobErr.Message
	}

	return models.RecipePlanResponse{
		Success:    false,
		Message:    message,
		InputPaths: []string{},
		Outputs:    []models.PlannedOutput{},
		Error:      jobErr,
	}
}

func canonicalInputKey(path string) string {
	cleaned := filepath.Clean(strings.TrimSpace(path))
	if absolute, err := filepath.Abs(cleaned); err == nil {
		return absolute
	}

	return cleaned
}

func newRecipeTempDir() (string, error) {
	return os.MkdirTemp("", recipeTempDirPattern)
}

// formatByteSize renders a byte count with a unit the UI can show as-is.
func formatByteSize(bytes int64) string {
	if bytes < 0 {
		bytes = 0
	}

	const (
		kib = 1024.0
		mib = kib * 1024.0
		gib = mib * 1024.0
	)

	switch {
	case float64(bytes) >= gib:
		return fmt.Sprintf("%.1f GB", float64(bytes)/gib)
	case float64(bytes) >= mib:
		return fmt.Sprintf("%.1f MB", float64(bytes)/mib)
	case float64(bytes) >= kib:
		return fmt.Sprintf("%.0f KB", float64(bytes)/kib)
	default:
		return fmt.Sprintf("%d B", bytes)
	}
}
