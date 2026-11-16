package recipes

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"html"
	"io"
	"math"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"fileforge-desktop/internal/image"
	"fileforge-desktop/internal/inspect"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/verify"
)

const (
	stepIDBuildVariants = "build"
	stepIDCreateZip     = "zip"
	stepIDWriteSnippet  = "snippet"

	buildVariantsStepIndex = 0
	createZipStepIndex     = 1
	writeSnippetStepIndex  = 2
	webImageSetStepCount   = 3

	webImageSetFallbackStem       = "image"
	webImageSetSnippetEntry       = "srcset.html"
	webImageSetZipSuffix          = "-web-set"
	webImageSetSnippetSuffix      = "-srcset"
	webImageSetWebPQuality        = 80
	webImageSetJPEGQuality        = 85
	webImageSetZipTempPattern     = ".fileforge-web-set-*.tmp"
	webImageSetSnippetTempPattern = ".fileforge-srcset-*.tmp"
	webImageSetVariantWidthCheck  = "variant_width"
)

// webImageSetVariant is one planned variant: its destination, encoder and the
// width the source can actually support.
type webImageSetVariant struct {
	Path    string
	Format  string
	Quality int
	Width   int
	Height  int
}

// builtWebImageSetVariant records a variant that was written, using the actual
// output path and format so the snippet and the ZIP reference real files.
type builtWebImageSetVariant struct {
	Path   string
	Format string
	Width  int
}

// webImageSetResolution is the recipe-owned plan: every variant plus the ZIP
// and snippet destinations. It is resolved during Plan and reused by Run.
type webImageSetResolution struct {
	SourcePath  string
	OutputDir   string
	Variants    []webImageSetVariant
	ZipPath     string
	SnippetPath string
	Warnings    []string
}

func webImageSetRecipe() models.RecipeV1 {
	return models.RecipeV1{
		ID:               RecipeIDWebImageSet,
		Name:             "Web image set",
		Description:      "Export responsive WebP and JPEG variants with a ZIP bundle and a ready-to-paste srcset snippet.",
		Category:         "image",
		InputKind:        "image",
		AcceptExtensions: []string{"png", "jpg", "jpeg", "webp"},
		MaxInputs:        1,
		Highlights:       []string{"No upscaling", "WebP + JPEG", "ZIP + srcset"},
		Steps: []models.RecipeStepV1{
			{
				ID:     stepIDBuildVariants,
				Name:   "Build responsive variants",
				ToolID: image.ToolIDImageConvertV1,
				Detail: "WebP and JPEG at the widths your source can support",
			},
			{
				ID:     stepIDCreateZip,
				Name:   "Create ZIP bundle",
				Detail: "All variants plus the snippet in one download",
			},
			{
				ID:     stepIDWriteSnippet,
				Name:   "Write srcset snippet",
				Detail: "Ready-to-paste picture markup",
			},
		},
	}
}

func resolveWebImageSet(recipe *models.RecipeV1, req models.RecipeRequestV1) (*resolvedRecipe, *models.JobErrorV1) {
	inputs, inputErr := validateWebImageSetInputs(req.InputPaths)
	if inputErr != nil {
		return nil, inputErr
	}
	sourcePath := inputs[0]

	outputDir, dirErr := resolveRecipeOutputDir(req.OutputDir, sourcePath)
	if dirErr != nil {
		return nil, dirErr
	}

	info := inspect.InspectFile(sourcePath)
	if !info.Exists {
		return nil, newRecipeError(
			"RECIPE_INPUT_UNREADABLE",
			fmt.Sprintf("The source image was not found: %s", filepath.Base(sourcePath)),
			map[string]any{"inputPath": sourcePath},
		)
	}

	stat, statErr := os.Stat(sourcePath)
	if statErr != nil {
		return nil, newRecipeError(
			"RECIPE_INPUT_UNREADABLE",
			fmt.Sprintf("The source image cannot be read: %s", filepath.Base(sourcePath)),
			map[string]any{"inputPath": sourcePath},
		)
	}
	if !stat.Mode().IsRegular() {
		return nil, newRecipeError(
			"RECIPE_INPUT_UNREADABLE",
			fmt.Sprintf("The source is not a regular file: %s", filepath.Base(sourcePath)),
			map[string]any{"inputPath": sourcePath},
		)
	}
	if info.Width <= 0 || info.Height <= 0 {
		return nil, newRecipeError(
			"RECIPE_INPUT_UNREADABLE",
			fmt.Sprintf("Could not read the image dimensions for %s.", filepath.Base(sourcePath)),
			map[string]any{"inputPath": sourcePath},
		)
	}

	stem := webImageSetSourceStem(sourcePath)
	widths := webImageSetWidths(info.Width)
	used := make(map[string]struct{}, len(widths)*2+2)

	variants := make([]webImageSetVariant, 0, len(widths)*2)
	planned := make([]models.PlannedOutputV1, 0, len(widths)*2+2)
	for _, width := range widths {
		height := int(math.Round(float64(info.Height) * float64(width) / float64(info.Width)))
		for _, format := range []string{"webp", "jpeg"} {
			extension := format
			quality := webImageSetWebPQuality
			if format == "jpeg" {
				extension = "jpg"
				quality = webImageSetJPEGQuality
			}

			outputPath, _ := outputplan.UniqueFilePath(outputDir, fmt.Sprintf("%s-%d", stem, width), extension, used)
			variants = append(variants, webImageSetVariant{
				Path:    outputPath,
				Format:  format,
				Quality: quality,
				Width:   width,
				Height:  height,
			})
			planned = append(planned, models.PlannedOutputV1{
				InputPath:   sourcePath,
				OutputPath:  outputPath,
				OutputKind:  "file",
				OutputCount: 1,
			})
		}
	}

	zipPath, _ := outputplan.UniqueFilePath(outputDir, stem+webImageSetZipSuffix, "zip", used)
	planned = append(planned, models.PlannedOutputV1{
		InputPath:   sourcePath,
		OutputPath:  zipPath,
		OutputKind:  "file",
		OutputCount: 1,
	})

	snippetPath, _ := outputplan.UniqueFilePath(outputDir, stem+webImageSetSnippetSuffix, "html", used)
	planned = append(planned, models.PlannedOutputV1{
		InputPath:   sourcePath,
		OutputPath:  snippetPath,
		OutputKind:  "file",
		OutputCount: 1,
	})

	warnings := webImageSetPlanWarnings(info.Width)

	return &resolvedRecipe{
		Recipe:         recipe,
		Inputs:         inputs,
		OutputDir:      outputDir,
		PlannedOutputs: planned,
		PlanMessage:    fmt.Sprintf("Ready to build %d files from %s.", len(planned), filepath.Base(sourcePath)),
		Web: &webImageSetResolution{
			SourcePath:  sourcePath,
			OutputDir:   outputDir,
			Variants:    variants,
			ZipPath:     zipPath,
			SnippetPath: snippetPath,
			Warnings:    warnings,
		},
		Warnings: warnings,
	}, nil
}

func validateWebImageSetInputs(rawInputs []string) ([]string, *models.JobErrorV1) {
	if len(rawInputs) == 0 {
		return nil, newRecipeError("RECIPE_INVALID_INPUT", "Choose one image for Web image set.", nil)
	}

	inputs := make([]string, 0, len(rawInputs))
	for _, rawInput := range rawInputs {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return nil, newRecipeError("RECIPE_INVALID_INPUT", "Every input must be an image file path.", nil)
		}
		inputs = append(inputs, inputPath)
	}

	if len(inputs) != 1 {
		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			"Web image set takes exactly one image. Remove the extra files.",
			nil,
		)
	}

	inputPath := inputs[0]
	if !supportsWebImageSetExtension(inputPath) {
		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			fmt.Sprintf("Only .png, .jpg, .jpeg and .webp images are supported: %s", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}

	return inputs, nil
}

func supportsWebImageSetExtension(inputPath string) bool {
	switch strings.ToLower(filepath.Ext(inputPath)) {
	case ".png", ".jpg", ".jpeg", ".webp":
		return true
	default:
		return false
	}
}

// webImageSetWidths requests the 1600 and 800 px variants, caps each at the
// source width and dedupes equal widths while preserving order. A source can
// therefore never produce an upscaled variant.
func webImageSetWidths(sourceWidth int) []int {
	requested := []int{1600, 800}
	widths := make([]int, 0, len(requested))
	seen := make(map[int]struct{}, len(requested))

	for _, width := range requested {
		capped := width
		if capped > sourceWidth {
			capped = sourceWidth
		}
		if capped <= 0 {
			continue
		}
		if _, exists := seen[capped]; exists {
			continue
		}
		seen[capped] = struct{}{}
		widths = append(widths, capped)
	}

	return widths
}

func webImageSetPlanWarnings(sourceWidth int) []string {
	warnings := make([]string, 0, 1)
	switch {
	case sourceWidth <= 800:
		warnings = append(warnings, fmt.Sprintf(
			"Source is %d px wide; only a %d px variant is generated (no upscaling).",
			sourceWidth,
			sourceWidth,
		))
	case sourceWidth < 1600:
		warnings = append(warnings, fmt.Sprintf(
			"Source is %d px wide; the 1600 px variant is capped at %d px (no upscaling).",
			sourceWidth,
			sourceWidth,
		))
	}

	return warnings
}

func webImageSetSourceStem(inputPath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		return webImageSetFallbackStem
	}

	return stem
}

func (s *Service) executeWebImageSet(ctx context.Context, tracked *recipeJob, resolved *resolvedRecipe) {
	web := resolved.Web
	if web == nil || len(resolved.Recipe.Steps) != webImageSetStepCount {
		s.failJob(tracked, newRecipeError("RECIPE_INVALID_DEFINITION", "The Web image set recipe definition is invalid.", nil))
		return
	}
	if ctx.Err() != nil {
		s.cancelJob(tracked)
		return
	}

	buildStep := resolved.Recipe.Steps[buildVariantsStepIndex]
	zipStep := resolved.Recipe.Steps[createZipStepIndex]
	snippetStep := resolved.Recipe.Steps[writeSnippetStepIndex]

	tracked.begin(1)
	s.emitProgress(tracked, models.JobStatusRunning, buildStep.Name, "starting", fmt.Sprintf("Starting %s.", resolved.Recipe.Name), 5)

	// created tracks every path this run may have written. It is only used to
	// clean up on failure, so a pre-existing file is never removed.
	created := make([]string, 0, len(web.Variants)+2)
	built, jobErr := s.runWebImageSetVariants(ctx, tracked, web, buildStep, &created)
	if jobErr != nil {
		removeCreatedFiles(created)
		if isCancellation(ctx, jobErr) {
			s.cancelJob(tracked)
			return
		}
		tracked.setStep(buildVariantsStepIndex, failedRecipeStep(buildStep, jobErr))
		s.failJob(tracked, jobErr)
		return
	}

	snippet := buildWebImageSetSnippet(built)

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}

	tracked.setCurrentStep(createZipStepIndex + 1)
	s.emitProgress(tracked, models.JobStatusRunning, zipStep.Name, "running", "Creating the ZIP bundle.", 70)
	if zipErr := createWebImageSetZip(web.ZipPath, built, snippet); zipErr != nil {
		removeCreatedFiles(created)
		tracked.setStep(createZipStepIndex, failedRecipeStep(zipStep, zipErr))
		s.failJob(tracked, zipErr)
		return
	}
	created = append(created, web.ZipPath)
	tracked.setStep(createZipStepIndex, models.RecipeStepResultV1{
		ID:      zipStep.ID,
		Name:    zipStep.Name,
		ToolID:  zipStep.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: "ZIP bundle created.",
		Outputs: []string{web.ZipPath},
	})
	s.emitProgress(tracked, models.JobStatusRunning, zipStep.Name, "running", "ZIP bundle created.", 80)

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}

	tracked.setCurrentStep(writeSnippetStepIndex + 1)
	s.emitProgress(tracked, models.JobStatusRunning, snippetStep.Name, "running", "Writing the srcset snippet.", 85)
	if snippetErr := writeWebImageSetSnippet(web.SnippetPath, snippet); snippetErr != nil {
		removeCreatedFiles(created)
		tracked.setStep(writeSnippetStepIndex, failedRecipeStep(snippetStep, snippetErr))
		s.failJob(tracked, snippetErr)
		return
	}
	created = append(created, web.SnippetPath)
	tracked.setStep(writeSnippetStepIndex, models.RecipeStepResultV1{
		ID:      snippetStep.ID,
		Name:    snippetStep.Name,
		ToolID:  snippetStep.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: "srcset snippet written.",
		Outputs: []string{web.SnippetPath},
	})
	s.emitProgress(tracked, models.JobStatusRunning, snippetStep.Name, "verifying", "Verifying the outputs.", 90)

	outputs := make([]string, 0, len(built)+2)
	for _, variant := range built {
		outputs = append(outputs, variant.Path)
	}
	outputs = append(outputs, web.ZipPath, web.SnippetPath)

	report := verify.VerifyPaths(outputs)
	report = addWebImageSetVariantWidthChecks(report, built)

	status := models.JobStatusSuccess
	message := fmt.Sprintf("Web image set ready: %d verified files.", len(outputs))
	if !report.Success {
		status = models.JobStatusPartialSuccess
		message = fmt.Sprintf("Web image set ready, but verification reported: %s.", report.Message)
	}

	if s.history != nil {
		s.history.RecordOutputs(tracked.jobID(), webImageSetHistoryToolID, outputs)
	}

	tracked.finish(status, message, outputs, &report, nil, true)
	s.emitProgress(tracked, status, snippetStep.Name, status, message, 100)
}

// runWebImageSetVariants converts every planned variant with the image tool and
// returns the files it actually wrote.
func (s *Service) runWebImageSetVariants(ctx context.Context, tracked *recipeJob, web *webImageSetResolution, step models.RecipeStepV1, created *[]string) ([]builtWebImageSetVariant, *models.JobErrorV1) {
	executor, planner, jobErr := s.singleExecutor(image.ToolIDImageConvertV1)
	if jobErr != nil {
		return nil, jobErr
	}

	total := len(web.Variants)
	if total == 0 {
		return nil, newRecipeError("RECIPE_INVALID_DEFINITION", "The Web image set recipe has no variants to build.", nil)
	}

	built := make([]builtWebImageSetVariant, 0, total)
	for index, variant := range web.Variants {
		if ctx.Err() != nil {
			return built, recipeCancelledError()
		}

		// The name was free during planning; confirm it still is, then claim it
		// for cleanup before the tool can write a partial file.
		if _, statErr := os.Stat(variant.Path); statErr == nil {
			return built, newRecipeError(
				"RECIPE_OUTPUT_EXISTS",
				fmt.Sprintf("%s already exists. Choose another output folder.", filepath.Base(variant.Path)),
				map[string]any{"outputPath": variant.Path},
			)
		} else if !errors.Is(statErr, os.ErrNotExist) {
			return built, newRecipeError(
				"RECIPE_STEP_FAILED",
				fmt.Sprintf("%s cannot be accessed.", filepath.Base(variant.Path)),
				map[string]any{"outputPath": variant.Path},
			)
		}
		*created = append(*created, variant.Path)

		percent := 15 + int(math.Round(float64(index+1)/float64(total)*50))
		s.emitProgress(
			tracked,
			models.JobStatusRunning,
			step.Name,
			"running",
			fmt.Sprintf("%d px %s", variant.Width, webImageSetFormatLabel(variant.Format)),
			percent,
		)

		req := webImageSetVariantRequest(web, variant)
		plan, planErr := planner.Plan(ctx, req)
		if planErr != nil {
			return built, planErr
		}

		item, execErr := executor.ExecuteSingle(ctx, req)
		if execErr != nil {
			return built, execErr
		}
		if !item.Success {
			return built, stepItemError(item, image.ToolIDImageConvertV1)
		}

		outputPath := strings.TrimSpace(item.OutputPath)
		if outputPath == "" && len(plan.Outputs) > 0 {
			outputPath = strings.TrimSpace(plan.Outputs[0].OutputPath)
		}
		if outputPath == "" {
			return built, newRecipeError("RECIPE_STEP_FAILED", "The image step did not report an output.", nil)
		}
		if outputPath != variant.Path {
			*created = append(*created, outputPath)
		}

		built = append(built, builtWebImageSetVariant{
			Path:   outputPath,
			Format: variant.Format,
			Width:  variant.Width,
		})
	}

	outputs := make([]string, 0, len(built))
	for _, variant := range built {
		outputs = append(outputs, variant.Path)
	}
	tracked.setStep(buildVariantsStepIndex, models.RecipeStepResultV1{
		ID:      step.ID,
		Name:    step.Name,
		ToolID:  step.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: fmt.Sprintf("%d variants created", len(built)),
		Outputs: outputs,
	})
	tracked.setCurrentStep(createZipStepIndex + 1)

	return built, nil
}

func webImageSetVariantRequest(web *webImageSetResolution, variant webImageSetVariant) models.JobRequestV1 {
	options := map[string]any{
		"format":        variant.Format,
		"width":         variant.Width,
		"quality":       variant.Quality,
		"stripMetadata": true,
		"outputPath":    variant.Path,
	}
	if variant.Format == "jpeg" {
		options["background"] = "#ffffff"
	}

	return models.JobRequestV1{
		ToolID:     image.ToolIDImageConvertV1,
		Mode:       "single",
		InputPaths: []string{web.SourcePath},
		OutputDir:  web.OutputDir,
		Options:    options,
	}
}

func webImageSetFormatLabel(format string) string {
	switch format {
	case "webp":
		return "WebP"
	case "jpeg":
		return "JPEG"
	default:
		return strings.ToUpper(format)
	}
}

// buildWebImageSetSnippet renders the ready-to-paste picture markup using the
// actual variant file names. Width descriptors ascend so the smallest candidate
// comes first.
func buildWebImageSetSnippet(variants []builtWebImageSetVariant) string {
	webp := make([]builtWebImageSetVariant, 0, len(variants))
	jpeg := make([]builtWebImageSetVariant, 0, len(variants))
	for _, variant := range variants {
		switch variant.Format {
		case "webp":
			webp = append(webp, variant)
		case "jpeg":
			jpeg = append(jpeg, variant)
		}
	}
	sort.SliceStable(webp, func(left, right int) bool { return webp[left].Width < webp[right].Width })
	sort.SliceStable(jpeg, func(left, right int) bool { return jpeg[left].Width < jpeg[right].Width })

	var builder strings.Builder
	builder.WriteString("<!-- Generated by FileForge. Paste this into your page. -->\n")
	builder.WriteString("<picture>\n")
	if len(webp) > 0 {
		builder.WriteString(fmt.Sprintf(
			"  <source type=\"image/webp\" srcset=\"%s\" sizes=\"(max-width: 800px) 100vw, 800px\" />\n",
			webImageSetSrcset(webp),
		))
	}
	if len(jpeg) > 0 {
		builder.WriteString(fmt.Sprintf(
			"  <img src=\"%s\" srcset=\"%s\" sizes=\"(max-width: 800px) 100vw, 800px\" alt=\"\" loading=\"lazy\" decoding=\"async\" />\n",
			html.EscapeString(filepath.Base(jpeg[0].Path)),
			webImageSetSrcset(jpeg),
		))
	}
	builder.WriteString("</picture>\n")

	return builder.String()
}

func webImageSetSrcset(variants []builtWebImageSetVariant) string {
	entries := make([]string, 0, len(variants))
	for _, variant := range variants {
		entries = append(entries, fmt.Sprintf("%s %dw", html.EscapeString(filepath.Base(variant.Path)), variant.Width))
	}

	return strings.Join(entries, ", ")
}

// createWebImageSetZip writes the bundle to a temporary file in the output
// folder and renames it into place. An existing destination is never
// overwritten.
func createWebImageSetZip(zipPath string, variants []builtWebImageSetVariant, snippet string) *models.JobErrorV1 {
	tempFile, err := os.CreateTemp(filepath.Dir(zipPath), webImageSetZipTempPattern)
	if err != nil {
		return newRecipeError("RECIPE_ZIP_FAILED", fmt.Sprintf("Could not create the ZIP bundle: %v", err), nil)
	}
	tempPath := tempFile.Name()

	writer := zip.NewWriter(tempFile)
	writeErr := writeWebImageSetZipEntries(writer, variants, snippet)
	if writeErr == nil {
		writeErr = writer.Close()
	} else {
		_ = writer.Close()
	}
	if writeErr == nil {
		writeErr = tempFile.Close()
	} else {
		_ = tempFile.Close()
	}
	if writeErr != nil {
		_ = os.Remove(tempPath)
		return newRecipeError("RECIPE_ZIP_FAILED", fmt.Sprintf("Could not write the ZIP bundle: %v", writeErr), nil)
	}

	if _, statErr := os.Stat(zipPath); statErr == nil {
		_ = os.Remove(tempPath)
		return newRecipeError(
			"RECIPE_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another output folder.", filepath.Base(zipPath)),
			map[string]any{"outputPath": zipPath},
		)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = os.Remove(tempPath)
		return newRecipeError("RECIPE_ZIP_FAILED", fmt.Sprintf("%s cannot be accessed.", filepath.Base(zipPath)), map[string]any{"outputPath": zipPath})
	}

	if renameErr := os.Rename(tempPath, zipPath); renameErr != nil {
		_ = os.Remove(tempPath)
		return newRecipeError("RECIPE_ZIP_FAILED", fmt.Sprintf("Could not save the ZIP bundle: %v", renameErr), nil)
	}

	return nil
}

func writeWebImageSetZipEntries(writer *zip.Writer, variants []builtWebImageSetVariant, snippet string) error {
	for _, variant := range variants {
		entry, err := writer.Create(filepath.Base(variant.Path))
		if err != nil {
			return err
		}

		file, openErr := os.Open(variant.Path)
		if openErr != nil {
			return openErr
		}
		_, copyErr := io.Copy(entry, file)
		closeErr := file.Close()
		if copyErr != nil {
			return copyErr
		}
		if closeErr != nil {
			return closeErr
		}
	}

	entry, err := writer.Create(webImageSetSnippetEntry)
	if err != nil {
		return err
	}
	_, err = entry.Write([]byte(snippet))

	return err
}

// writeWebImageSetSnippet writes the standalone HTML atomically. An existing
// destination is never overwritten.
func writeWebImageSetSnippet(snippetPath, snippet string) *models.JobErrorV1 {
	tempFile, err := os.CreateTemp(filepath.Dir(snippetPath), webImageSetSnippetTempPattern)
	if err != nil {
		return newRecipeError("RECIPE_SNIPPET_FAILED", fmt.Sprintf("Could not create the srcset snippet: %v", err), nil)
	}
	tempPath := tempFile.Name()

	if _, writeErr := tempFile.WriteString(snippet); writeErr != nil {
		_ = tempFile.Close()
		_ = os.Remove(tempPath)
		return newRecipeError("RECIPE_SNIPPET_FAILED", fmt.Sprintf("Could not write the srcset snippet: %v", writeErr), nil)
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(tempPath)
		return newRecipeError("RECIPE_SNIPPET_FAILED", fmt.Sprintf("Could not write the srcset snippet: %v", closeErr), nil)
	}

	if _, statErr := os.Stat(snippetPath); statErr == nil {
		_ = os.Remove(tempPath)
		return newRecipeError(
			"RECIPE_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another output folder.", filepath.Base(snippetPath)),
			map[string]any{"outputPath": snippetPath},
		)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		_ = os.Remove(tempPath)
		return newRecipeError("RECIPE_SNIPPET_FAILED", fmt.Sprintf("%s cannot be accessed.", filepath.Base(snippetPath)), map[string]any{"outputPath": snippetPath})
	}

	if renameErr := os.Rename(tempPath, snippetPath); renameErr != nil {
		_ = os.Remove(tempPath)
		return newRecipeError("RECIPE_SNIPPET_FAILED", fmt.Sprintf("Could not save the srcset snippet: %v", renameErr), nil)
	}

	return nil
}

// addWebImageSetVariantWidthChecks appends one exact-width check per image
// output. A mismatch marks the whole report as failed instead of silently
// passing.
func addWebImageSetVariantWidthChecks(report models.VerificationReportV1, variants []builtWebImageSetVariant) models.VerificationReportV1 {
	widthFailed := false
	for index, variant := range variants {
		if index >= len(report.Outputs) {
			break
		}

		actual := inspect.InspectFile(variant.Path)
		passed := actual.Width == variant.Width
		detail := ""
		if !passed {
			widthFailed = true
			detail = fmt.Sprintf("expected %d px", variant.Width)
			report.Warnings = append(report.Warnings, fmt.Sprintf(
				"%s: expected %d px, got %d px",
				filepath.Base(variant.Path),
				variant.Width,
				actual.Width,
			))
		}

		report.Outputs[index].Checks = append(report.Outputs[index].Checks, models.VerificationCheckV1{
			Key:    webImageSetVariantWidthCheck,
			Label:  "Variant width",
			Passed: passed,
			Value:  fmt.Sprintf("%d px", actual.Width),
			Detail: detail,
		})
	}

	if widthFailed {
		report.Success = false
		report.Message = "verification failed: variant widths do not match the plan"
	}

	return report
}

func removeCreatedFiles(paths []string) {
	for _, path := range paths {
		if strings.TrimSpace(path) == "" {
			continue
		}
		_ = os.Remove(path)
	}
}

func recipeCancelledError() *models.JobErrorV1 {
	return models.NewJobError(models.ErrorCodeCancelledByUser, "RECIPE_CANCELLED", "Recipe cancelled.", nil)
}
