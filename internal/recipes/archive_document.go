package recipes

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"

	"fileforge-desktop/internal/doc"
	docengine "fileforge-desktop/internal/doc/engine"
	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/platform/fsx"
	"fileforge-desktop/internal/toolchain"
	"fileforge-desktop/internal/tools"
)

const (
	// RecipeIDArchiveDocument is the curated outcome "Archive document".
	RecipeIDArchiveDocument = "recipe.archive_document"

	archiveStepRender = "render"
	archiveStepVerify = "verify"

	archiveStepIndexRender = 0
	archiveStepIndexVerify = 1
	archiveStepCount       = 2

	archiveOutputSuffix = "_archived"
	archiveFallbackStem = "document"

	// archiveSidecarOptionKey declares the only option: write the checksum
	// sidecar next to the PDF.
	archiveSidecarOptionKey = "writeSidecar"

	// archiveSidecarExtension follows the `<pdf>.sha256` convention so the file
	// is recognized by existing tooling.
	archiveSidecarExtension = "sha256"

	archivePDFFileCheck        = "recipe.pdf_file"
	archivePDFMagicCheck       = "recipe.pdf_magic"
	archivePDFAMarkerCheck     = "recipe.pdfa_marker"
	archivePDFAPageCountCheck  = "recipe.page_count"
	archiveSHA256Check         = "recipe.sha256"
	archiveSidecarCheck        = "recipe.sha256_sidecar"
	archivePDFHeaderReadBytes  = 16
	archiveProgressStart       = 5
	archiveProgressRendering   = 15
	archiveProgressVerifying   = 70
	archiveProgressEnd         = 100
	archiveProgressPublish     = 95
	archiveVerificationCode    = "RECIPE_ARCHIVE_VERIFICATION_FAILED"
	archiveGhostscriptGuidance = "Install Ghostscript (for example `sudo apt install ghostscript`, `brew install ghostscript` or the managed runtime in Settings) and try again."
)

// archiveComplianceCaveat is the honest label every archive result carries.
// Ghostscript writes PDF/A-2 metadata but the file is not independently
// validated, so the recipe never claims conformance.
const archiveComplianceCaveat = "PDF/A-2 is produced by Ghostscript and is not validated with veraPDF."

// archiveGhostscriptAvailable is the plan-time dependency detector. The PDF/A
// step in internal/doc/engine resolves the same candidate list before running,
// so plan and execution agree on what counts as an installed Ghostscript. It is
// a package variable so tests can simulate a machine without Ghostscript.
var archiveGhostscriptAvailable = func() bool {
	_, found := toolchain.ResolveGhostscript(nil)

	return found
}

// archivePDFAPartPattern and archivePDFAConformancePattern read the XMP PDF/A
// identification. Ghostscript writes the attributes in single quotes, but both
// quote styles are accepted.
var (
	archivePDFAPartPattern        = regexp.MustCompile(`(?i)pdfaid:part\s*=\s*["']([^"']+)["']`)
	archivePDFAConformancePattern = regexp.MustCompile(`(?i)pdfaid:conformance\s*=\s*["']([A-Za-z])["']`)
)

// archiveDocumentResolution is the recipe-owned plan: the source DOCX, the
// PDF/A destination, the optional sidecar and the honest caveat.
type archiveDocumentResolution struct {
	SourcePath   string
	OutputDir    string
	PDFPath      string
	SidecarPath  string
	WriteSidecar bool
}

// archiveDocumentFacts is what verification reads back from the produced PDF.
type archiveDocumentFacts struct {
	PDFAPart     string
	Conformance  string
	PageCount    int
	ReportedHash string
}

func archiveDocumentRecipe() models.Recipe {
	return models.Recipe{
		ID:               RecipeIDArchiveDocument,
		Name:             "Archive document",
		Description:      "Turn a DOCX into a PDF/A-2 file and report its SHA-256 for long-term storage.",
		Category:         "documents",
		InputKind:        "document",
		AcceptExtensions: []string{"docx"},
		MaxInputs:        1,
		Highlights:       []string{"PDF/A-2", "SHA-256 checksum", "Optional sidecar"},
		Steps: []models.RecipeStep{
			{
				ID:     archiveStepRender,
				Name:   "Render PDF/A-2",
				ToolID: doc.ToolIDDocDOCXToPDF,
				Detail: "Bundled DOCX renderer, then Ghostscript distils the PDF",
			},
			{
				ID:     archiveStepVerify,
				Name:   "Hash and verify",
				Detail: "PDF signature, PDF/A marker, page count and SHA-256",
			},
		},
		OptionSchema: archiveDocumentOptionSchema(),
		Duplicable:   true,
	}
}

// archiveDocumentOptionSchema declares the single knob: whether the checksum
// sidecar is written. It defaults to off so a plain archive run produces one
// file.
func archiveDocumentOptionSchema() []models.RecipeOptionField {
	return []models.RecipeOptionField{{
		Key:     archiveSidecarOptionKey,
		Label:   "SHA-256 sidecar",
		Kind:    models.RecipeOptionBoolean,
		Default: false,
		Help:    "Write <pdf>.sha256 next to the PDF with a `hash  filename` line.",
	}}
}

// resolveArchiveDocument validates the DOCX, requires Ghostscript and asks the
// renderer to plan the exact PDF/A destination so a document the tool rejects
// fails during planning instead of after the run starts.
func (s *Service) resolveArchiveDocument(recipe *models.Recipe, req models.RecipeRequest) (*resolvedRecipe, *models.JobError) {
	inputs, inputErr := validateArchiveDocumentInputs(req.InputPaths)
	if inputErr != nil {
		return nil, inputErr
	}
	sourcePath := inputs[0]

	writeSidecar, optionErr := archiveDocumentSidecarOption(req.Options)
	if optionErr != nil {
		return nil, optionErr
	}

	if !archiveGhostscriptAvailable() {
		candidates := strings.Join(toolchain.GhostscriptBinaryNames, ", ")
		return nil, newRecipeError(
			docengine.ErrorCodeDocxPDFAGhostscriptMissing,
			"Ghostscript ("+candidates+") is required for PDF/A-2 export but is not installed. "+archiveGhostscriptGuidance,
			map[string]any{"runtime": "gs", "candidates": candidates, "recipeId": recipe.ID},
		)
	}

	outputDir, dirErr := resolveRecipeOutputDir(req.OutputDir, sourcePath)
	if dirErr != nil {
		return nil, dirErr
	}

	base := archiveDocumentOutputBase(sourcePath)
	used := make(map[string]struct{}, 2)
	outputplan.MarkUsed(used, sourcePath)
	pdfPath, pdfAdjusted := outputplan.UniqueFilePath(outputDir, base, "pdf", used)

	// The tool plans the explicit destination with pdfa on: this validates the
	// DOCX package (corrupt or protected inputs fail here) and keeps the shown
	// path and the rendered path identical.
	toolPlan, toolErr := s.archiveToolPlan(sourcePath, pdfPath)
	if toolErr != nil {
		return nil, toolErr
	}
	if planned := strings.TrimSpace(toolPlan.Outputs[0].OutputPath); planned != "" {
		pdfPath = planned
	}

	var sidecarPath string
	sidecarAdjusted := false
	if writeSidecar {
		sidecarPath, sidecarAdjusted = outputplan.UniqueFilePath(outputDir, filepath.Base(pdfPath), archiveSidecarExtension, used)
	}

	warnings := make([]string, 0, 3)
	if pdfAdjusted {
		warnings = append(warnings, fmt.Sprintf("%s already exists. Saving as %s.", base+".pdf", filepath.Base(pdfPath)))
	}
	if sidecarAdjusted {
		warnings = append(warnings, fmt.Sprintf("%s already exists. Saving as %s.", filepath.Base(pdfPath)+"."+archiveSidecarExtension, filepath.Base(sidecarPath)))
	}
	warnings = append(warnings, archiveComplianceCaveat)
	warnings = append(warnings, toolPlan.Warnings...)

	planned := []models.PlannedOutput{
		{InputPath: sourcePath, OutputPath: pdfPath, OutputKind: "file", OutputCount: 1},
	}
	if sidecarPath != "" {
		planned = append(planned, models.PlannedOutput{InputPath: sourcePath, OutputPath: sidecarPath, OutputKind: "file", OutputCount: 1})
	}

	return &resolvedRecipe{
		Recipe:         recipe,
		Inputs:         inputs,
		OutputDir:      outputDir,
		PlannedOutputs: planned,
		PlanMessage:    fmt.Sprintf("Ready to archive %s as PDF/A-2.", filepath.Base(sourcePath)),
		Archive: &archiveDocumentResolution{
			SourcePath:   sourcePath,
			OutputDir:    outputDir,
			PDFPath:      pdfPath,
			SidecarPath:  sidecarPath,
			WriteSidecar: writeSidecar,
		},
		Warnings: warnings,
	}, nil
}

func validateArchiveDocumentInputs(rawInputs []string) ([]string, *models.JobError) {
	if len(rawInputs) == 0 {
		return nil, newRecipeError("RECIPE_INVALID_INPUT", "Choose one DOCX for Archive document.", nil)
	}

	inputs := make([]string, 0, len(rawInputs))
	for _, rawInput := range rawInputs {
		inputPath := strings.TrimSpace(rawInput)
		if inputPath == "" {
			return nil, newRecipeError("RECIPE_INVALID_INPUT", "Every input must be a DOCX file path.", nil)
		}
		inputs = append(inputs, inputPath)
	}

	if len(inputs) != 1 {
		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			"Archive document takes exactly one DOCX. Remove the extra files.",
			nil,
		)
	}

	inputPath := inputs[0]
	if !strings.EqualFold(filepath.Ext(inputPath), ".docx") {
		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			fmt.Sprintf("Only .docx files are supported: %s", filepath.Base(inputPath)),
			map[string]any{"inputPath": inputPath},
		)
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, newRecipeError(
				"RECIPE_INPUT_MISSING",
				fmt.Sprintf("Input DOCX was not found: %s", filepath.Base(inputPath)),
				map[string]any{"inputPath": inputPath},
			)
		}
		return nil, newRecipeError(
			"RECIPE_INVALID_INPUT",
			fmt.Sprintf("Input DOCX is not accessible: %s", filepath.Base(inputPath)),
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

// archiveDocumentSidecarOption reads options.writeSidecar. Absent or null keeps
// the default (off); any other value must be a boolean.
func archiveDocumentSidecarOption(options map[string]any) (bool, *models.JobError) {
	if options == nil {
		return false, nil
	}

	raw, exists := options[archiveSidecarOptionKey]
	if !exists || raw == nil {
		return false, nil
	}
	value, ok := raw.(bool)
	if !ok {
		return false, newRecipeError(
			"RECIPE_OPTION_INVALID_TYPE",
			fmt.Sprintf("Option '%s' must be true or false.", archiveSidecarOptionKey),
			map[string]any{"option": archiveSidecarOptionKey},
		)
	}

	return value, nil
}

func archiveDocumentOutputBase(sourcePath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(sourcePath), filepath.Ext(sourcePath)))
	if stem == "" {
		stem = archiveFallbackStem
	}

	return stem + archiveOutputSuffix
}

// archiveToolPlan plans the render step with PDF/A on and the explicit
// destination, returning the tool's own plan so the recipe reports the
// validated path and any tool warning.
func (s *Service) archiveToolPlan(sourcePath, outputPath string) (models.PlanJobResponse, *models.JobError) {
	tool, jobErr := s.toolFor(doc.ToolIDDocDOCXToPDF)
	if jobErr != nil {
		return models.PlanJobResponse{}, jobErr
	}

	planner, ok := tool.(tools.Planner)
	if !ok {
		return models.PlanJobResponse{}, newRecipeError(
			"RECIPE_TOOL_DEPENDENCY_MISSING",
			fmt.Sprintf("Tool '%s' cannot plan its output.", doc.ToolIDDocDOCXToPDF),
			map[string]any{"toolId": doc.ToolIDDocDOCXToPDF},
		)
	}

	plan, planErr := planner.Plan(context.Background(), archiveRenderRequest(sourcePath, outputPath))
	if planErr != nil {
		return models.PlanJobResponse{}, planErr
	}
	if !plan.Success || len(plan.Outputs) != 1 || strings.TrimSpace(plan.Outputs[0].OutputPath) == "" {
		return models.PlanJobResponse{}, newRecipeError(
			"RECIPE_STEP_FAILED",
			"The DOCX renderer did not plan an output.",
			map[string]any{"toolId": doc.ToolIDDocDOCXToPDF},
		)
	}

	return plan, nil
}

// archiveRenderRequest builds the render step: one DOCX in, the explicit PDF/A
// destination out.
func archiveRenderRequest(sourcePath, outputPath string) models.JobRequest {
	return models.JobRequest{
		ToolID:     doc.ToolIDDocDOCXToPDF,
		Mode:       "single",
		InputPaths: []string{sourcePath},
		OutputDir:  filepath.Dir(outputPath),
		Options: map[string]any{
			"pdfa":       true,
			"outputPath": outputPath,
		},
	}
}

func (s *Service) executeArchiveDocument(ctx context.Context, tracked *recipeJob, resolved *resolvedRecipe) {
	archive := resolved.Archive
	if archive == nil || len(resolved.Recipe.Steps) != archiveStepCount {
		s.failJob(tracked, newRecipeError("RECIPE_INVALID_DEFINITION", "The Archive document recipe definition is invalid.", nil))
		return
	}
	if ctx.Err() != nil {
		s.cancelJob(tracked)
		return
	}

	renderStep := resolved.Recipe.Steps[archiveStepIndexRender]
	verifyStep := resolved.Recipe.Steps[archiveStepIndexVerify]

	tracked.begin(1)
	s.emitProgress(tracked, models.JobStatusRunning, renderStep.Name, "starting", fmt.Sprintf("Starting %s.", resolved.Recipe.Name), archiveProgressStart)

	// created tracks every path this run may have written. It is only used to
	// clean up on failure or cancel, so a pre-existing file is never removed.
	created := make([]string, 0, 2)

	pdfPath, renderMessage, jobErr := s.runArchiveRenderStep(ctx, tracked, archive, renderStep, &created)
	if jobErr != nil {
		removeCreatedFiles(created)
		s.finishArchiveFailure(ctx, tracked, renderStep, archiveStepIndexRender, jobErr, nil)
		return
	}

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}

	tracked.setStep(archiveStepIndexRender, models.RecipeStepResult{
		ID:      renderStep.ID,
		Name:    renderStep.Name,
		ToolID:  renderStep.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: renderMessage,
		Outputs: []string{pdfPath},
	})
	tracked.setCurrentStep(archiveStepIndexVerify + 1)

	// The reported checksum is computed here, in the recipe, and verification
	// re-reads the file independently to compare.
	reportedHash, hashErr := archiveSHA256File(pdfPath)
	if hashErr != nil {
		jobErr = newRecipeError(
			archiveVerificationCode,
			fmt.Sprintf("Could not compute the SHA-256 of %s: %v", filepath.Base(pdfPath), hashErr),
			map[string]any{"outputPath": pdfPath},
		)
		removeCreatedFiles(created)
		s.finishArchiveFailure(ctx, tracked, verifyStep, archiveStepIndexVerify, jobErr, nil)
		return
	}

	s.emitProgress(tracked, models.JobStatusRunning, verifyStep.Name, "verifying", "Verifying PDF/A marker, page count and SHA-256.", archiveProgressVerifying)

	report, facts, verifyErr := verifyArchiveDocument(pdfPath, reportedHash)
	if verifyErr != nil {
		removeCreatedFiles(created)
		s.finishArchiveFailure(ctx, tracked, verifyStep, archiveStepIndexVerify, verifyErr, &report)
		return
	}

	if ctx.Err() != nil {
		removeCreatedFiles(created)
		s.cancelJob(tracked)
		return
	}

	outputs := []string{pdfPath}
	if archive.WriteSidecar {
		s.emitProgress(tracked, models.JobStatusRunning, verifyStep.Name, "verifying", "Writing the SHA-256 sidecar.", archiveProgressPublish)

		// A late collision may have renamed the PDF after planning; the sidecar
		// must always describe the file that was actually written.
		sidecarPath := archive.SidecarPath
		if filepath.Base(pdfPath) != filepath.Base(archive.PDFPath) {
			if unique, _ := outputplan.UniqueFilePath(archive.OutputDir, filepath.Base(pdfPath), archiveSidecarExtension, nil); unique != "" {
				sidecarPath = unique
			}
		}

		written, sidecarErr := publishArchiveSidecar(sidecarPath, pdfPath, reportedHash, &created)
		if sidecarErr != nil {
			removeCreatedFiles(created)
			s.finishArchiveFailure(ctx, tracked, verifyStep, archiveStepIndexVerify, sidecarErr, &report)
			return
		}
		outputs = append(outputs, written)
		report.Outputs = append(report.Outputs, archiveSidecarOutput(written, reportedHash))
	}

	verifyMessage := archiveVerifyMessage(facts)
	if s.history != nil {
		s.history.RecordOutputs(tracked.jobID(), archiveDocumentHistoryToolID, outputs)
	}

	tracked.setStep(archiveStepIndexVerify, models.RecipeStepResult{
		ID:      verifyStep.ID,
		Name:    verifyStep.Name,
		ToolID:  verifyStep.ToolID,
		Status:  recipeStepStatusSuccess,
		Message: verifyMessage,
		Outputs: outputs,
	})

	message := archiveResultMessage(filepath.Base(pdfPath), reportedHash)
	tracked.finish(models.JobStatusSuccess, message, outputs, &report, nil, true)
	s.emitProgress(tracked, models.JobStatusSuccess, verifyStep.Name, models.JobStatusSuccess, message, archiveProgressEnd)
}

// runArchiveRenderStep plans the exact PDF/A output and runs the bundled DOCX
// renderer with Ghostscript. The destination is claimed before the call so a
// failed run is cleaned up.
func (s *Service) runArchiveRenderStep(ctx context.Context, tracked *recipeJob, archive *archiveDocumentResolution, step models.RecipeStep, created *[]string) (string, string, *models.JobError) {
	if ctx.Err() != nil {
		return "", "", recipeCancelledError()
	}

	executor, planner, jobErr := s.singleExecutor(doc.ToolIDDocDOCXToPDF)
	if jobErr != nil {
		return "", "", jobErr
	}

	plannedPath := archive.PDFPath
	if _, statErr := os.Stat(plannedPath); statErr == nil {
		// The planned name was taken after planning; claim the next free name
		// instead of failing the whole run.
		if unique, _ := outputplan.UniqueFilePath(archive.OutputDir, archiveDocumentOutputBase(archive.SourcePath), "pdf", nil); unique != "" {
			plannedPath = unique
		}
	}
	*created = append(*created, plannedPath)

	req := archiveRenderRequest(archive.SourcePath, plannedPath)
	toolPlan, planErr := planner.Plan(ctx, req)
	if planErr != nil {
		return "", "", planErr
	}

	s.emitProgress(tracked, models.JobStatusRunning, step.Name, "rendering", "Rendering the document to PDF/A-2.", archiveProgressRendering)

	var item models.JobResultItem
	if progressExecutor, ok := executor.(tools.SingleExecutorWithProgress); ok {
		progress := func(event models.JobProgress) {
			s.emitProgress(tracked, models.JobStatusRunning, step.Name, "rendering", event.Message, archiveProgressRendering)
		}
		item, jobErr = progressExecutor.ExecuteSingleWithProgress(ctx, req, progress)
	} else {
		item, jobErr = executor.ExecuteSingle(ctx, req)
	}
	if jobErr != nil {
		return "", "", jobErr
	}
	if !item.Success {
		return "", "", stepItemError(item, doc.ToolIDDocDOCXToPDF)
	}

	outputPath := strings.TrimSpace(item.OutputPath)
	if outputPath == "" && len(toolPlan.Outputs) > 0 {
		outputPath = strings.TrimSpace(toolPlan.Outputs[0].OutputPath)
	}
	if outputPath == "" {
		return "", "", newRecipeError("RECIPE_STEP_FAILED", "The DOCX renderer did not report an output.", nil)
	}
	if outputPath != plannedPath {
		*created = append(*created, outputPath)
	}
	if !fsx.IsRegularFile(outputPath) {
		return "", "", newRecipeError(
			"RECIPE_OUTPUT_MISSING",
			fmt.Sprintf("The render step finished but the output is missing: %s", filepath.Base(outputPath)),
			map[string]any{"outputPath": outputPath},
		)
	}

	return outputPath, fmt.Sprintf("Rendered %s as PDF/A-2.", filepath.Base(outputPath)), nil
}

// finishArchiveFailure removes ambiguity between a cancelled run and a real
// failure, never reporting both.
func (s *Service) finishArchiveFailure(ctx context.Context, tracked *recipeJob, step models.RecipeStep, stepIndex int, jobErr *models.JobError, report *models.VerificationReport) {
	if isCancellation(ctx, jobErr) {
		s.cancelJob(tracked)
		return
	}

	tracked.setStep(stepIndex, failedRecipeStep(step, jobErr))

	message := "The recipe failed."
	if jobErr != nil && strings.TrimSpace(jobErr.Message) != "" {
		message = jobErr.Message
	}
	tracked.finish(models.JobStatusFailed, message, []string{}, report, jobErr, false)
	s.emitTerminal(tracked, models.JobStatusFailed, step.Name, message)
}

// verifyArchiveDocument runs the archive checks in their declared order and
// stops at the first failure: magic and size, PDF/A marker, page count, then
// the independent SHA-256 comparison.
func verifyArchiveDocument(pdfPath, reportedHash string) (models.VerificationReport, archiveDocumentFacts, *models.JobError) {
	facts := archiveDocumentFacts{ReportedHash: reportedHash}
	report := models.VerificationReport{
		Success:  true,
		Outputs:  []models.VerifiedOutput{{Path: pdfPath, Kind: "pdf", Checks: []models.VerificationCheck{}}},
		Warnings: []string{archiveComplianceCaveat},
	}
	checks := make([]models.VerificationCheck, 0, 5)

	size, header, headerErr := archiveReadHeader(pdfPath)
	filePassed := headerErr == nil && size > 0
	checks = append(checks, models.VerificationCheck{
		Key:    archivePDFFileCheck,
		Label:  "PDF file",
		Passed: filePassed,
		Value:  formatByteSize(size),
		Detail: checkDetail(!filePassed, "the output is missing or empty"),
	})
	if !filePassed {
		return archiveFailedVerification(report, checks, archivePDFFileCheck, "the output is missing or empty")
	}
	report.Outputs[0].Exists = true
	report.Outputs[0].SizeBytes = size

	version := archivePDFVersion(header)
	magicPassed := strings.HasPrefix(version, "%PDF")
	checks = append(checks, models.VerificationCheck{
		Key:    archivePDFMagicCheck,
		Label:  "PDF signature",
		Passed: magicPassed,
		Value:  version,
		Detail: checkDetail(!magicPassed, "the file does not start with %PDF"),
	})
	if !magicPassed {
		return archiveFailedVerification(report, checks, archivePDFMagicCheck, "the file does not start with %PDF")
	}

	part, conformance, markerErr := archivePDFAIdentification(pdfPath)
	facts.PDFAPart = part
	facts.Conformance = conformance
	markerPassed := markerErr == nil && part == "2" && conformance != ""
	markerValue := archivePDFAMarkerValue(part, conformance)
	checks = append(checks, models.VerificationCheck{
		Key:    archivePDFAMarkerCheck,
		Label:  "PDF/A marker",
		Passed: markerPassed,
		Value:  markerValue,
		Detail: checkDetail(!markerPassed, archivePDFAMarkerFailure(part, conformance, markerErr)),
	})
	if !markerPassed {
		return archiveFailedVerification(report, checks, archivePDFAMarkerCheck, archivePDFAMarkerFailure(part, conformance, markerErr))
	}

	pages, pageErr := api.PageCountFile(context.Background(), pdfPath)
	facts.PageCount = pages
	pagesPassed := pageErr == nil && pages > 0
	checks = append(checks, models.VerificationCheck{
		Key:    archivePDFAPageCountCheck,
		Label:  "Page count",
		Passed: pagesPassed,
		Value:  fmt.Sprintf("%d %s", pages, pluralizePage(pages)),
		Detail: checkDetail(!pagesPassed, "the PDF page count could not be read or is zero"),
	})
	if !pagesPassed {
		return archiveFailedVerification(report, checks, archivePDFAPageCountCheck, "the PDF page count could not be read or is zero")
	}

	secondHash, secondErr := archiveSHA256File(pdfPath)
	hashPassed := secondErr == nil && secondHash == reportedHash
	checks = append(checks, models.VerificationCheck{
		Key:    archiveSHA256Check,
		Label:  "SHA-256",
		Passed: hashPassed,
		Value:  reportedHash,
		Detail: checkDetail(!hashPassed, archiveHashMismatchDetail(reportedHash, secondHash, secondErr)),
	})
	if !hashPassed {
		return archiveFailedVerification(report, checks, archiveSHA256Check, archiveHashMismatchDetail(reportedHash, secondHash, secondErr))
	}

	report.Outputs[0].Checks = checks
	report.Message = "verified PDF/A-2 output with a matching SHA-256"

	return report, facts, nil
}

// archiveFailedVerification finalizes a failed report with the checks evaluated
// so far and a canonical error naming the first failing check.
func archiveFailedVerification(report models.VerificationReport, checks []models.VerificationCheck, checkKey, reason string) (models.VerificationReport, archiveDocumentFacts, *models.JobError) {
	if len(report.Outputs) > 0 {
		report.Outputs[0].Checks = checks
	}
	report.Success = false
	report.Message = fmt.Sprintf("verification failed: %s", reason)

	jobErr := newRecipeError(
		archiveVerificationCode,
		fmt.Sprintf("Archive verification failed: %s.", reason),
		map[string]any{"check": checkKey},
	)

	return report, archiveDocumentFacts{}, jobErr
}

// archiveReadHeader returns the size and the first bytes of the file. The
// header is enough to prove the PDF signature without reading the document.
func archiveReadHeader(path string) (int64, []byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return 0, nil, err
	}
	defer func() {
		_ = file.Close()
	}()

	info, err := file.Stat()
	if err != nil {
		return 0, nil, err
	}

	header := make([]byte, archivePDFHeaderReadBytes)
	read, err := io.ReadFull(file, header)
	if err != nil && !errors.Is(err, io.ErrUnexpectedEOF) && !errors.Is(err, io.EOF) {
		return info.Size(), nil, err
	}

	return info.Size(), header[:read], nil
}

// archivePDFVersion returns the version line, for example "%PDF-1.7".
func archivePDFVersion(header []byte) string {
	line := string(header)
	if index := strings.IndexAny(line, "\r\n"); index >= 0 {
		line = line[:index]
	}

	return strings.TrimSpace(line)
}

// archivePDFAIdentification reads the XMP metadata stream and extracts the
// PDF/A part and conformance. pdfcpu decompresses the metadata stream, so the
// check does not depend on Ghostscript's stream layout.
func archivePDFAIdentification(path string) (string, string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", "", err
	}
	defer func() {
		_ = file.Close()
	}()

	var builder strings.Builder
	if err := api.ExtractMetadata(context.Background(), file, func(metadata pdfcpu.Metadata) error {
		_, copyErr := io.Copy(&builder, metadata)
		return copyErr
	}, nil); err != nil {
		return "", "", err
	}

	xmp := builder.String()
	part := ""
	if match := archivePDFAPartPattern.FindStringSubmatch(xmp); len(match) == 2 {
		part = strings.TrimSpace(match[1])
	}
	conformance := ""
	if match := archivePDFAConformancePattern.FindStringSubmatch(xmp); len(match) == 2 {
		conformance = strings.ToUpper(strings.TrimSpace(match[1]))
	}

	return part, conformance, nil
}

func archivePDFAMarkerValue(part, conformance string) string {
	if part == "" && conformance == "" {
		return "none"
	}

	return fmt.Sprintf("PDF/A-%s%s", part, conformance)
}

func archivePDFAMarkerFailure(part, conformance string, err error) string {
	if err != nil {
		return fmt.Sprintf("the XMP metadata could not be read: %v", err)
	}
	if part == "" || conformance == "" {
		return fmt.Sprintf("the XMP metadata does not declare pdfaid:part and pdfaid:conformance (found pdfaid:part=%q pdfaid:conformance=%q)", part, conformance)
	}

	return fmt.Sprintf("the XMP metadata declares PDF/A-%s%s instead of PDF/A-2", part, conformance)
}

// archiveSHA256File streams the whole file through SHA-256. Each call opens the
// file again, so the verification read is independent from the reported one.
func archiveSHA256File(path string) (string, error) {
	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer func() {
		_ = file.Close()
	}()

	digest := sha256.New()
	if _, err := io.Copy(digest, file); err != nil {
		return "", err
	}

	return hex.EncodeToString(digest.Sum(nil)), nil
}

func archiveHashMismatchDetail(reported, second string, err error) string {
	if err != nil {
		return fmt.Sprintf("the second read failed: %v", err)
	}

	return fmt.Sprintf("the second read reported %s instead of %s", second, reported)
}

// publishArchiveSidecar writes the conventional `hash  filename` line next to
// the PDF. The destination is claimed before writing and never overwritten.
func publishArchiveSidecar(plannedPath, pdfPath, hash string, created *[]string) (string, *models.JobError) {
	sidecarPath := strings.TrimSpace(plannedPath)
	if sidecarPath == "" {
		sidecarPath = pdfPath + "." + archiveSidecarExtension
	}

	if _, statErr := os.Stat(sidecarPath); statErr == nil {
		return "", newRecipeError(
			"RECIPE_OUTPUT_EXISTS",
			fmt.Sprintf("%s already exists. Choose another output folder.", filepath.Base(sidecarPath)),
			map[string]any{"outputPath": sidecarPath},
		)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", newRecipeError(
			"RECIPE_SIDECAR_FAILED",
			fmt.Sprintf("%s cannot be accessed.", filepath.Base(sidecarPath)),
			map[string]any{"outputPath": sidecarPath},
		)
	}

	*created = append(*created, sidecarPath)
	if err := fsx.WriteAtomic(sidecarPath, archiveSidecarContent(hash, pdfPath), 0o644); err != nil {
		return "", newRecipeError(
			"RECIPE_SIDECAR_FAILED",
			fmt.Sprintf("Could not write the SHA-256 sidecar: %v", err),
			map[string]any{"outputPath": sidecarPath},
		)
	}

	return sidecarPath, nil
}

func archiveSidecarContent(hash, pdfPath string) []byte {
	return []byte(fmt.Sprintf("%s  %s\n", hash, filepath.Base(pdfPath)))
}

func archiveSidecarOutput(path, hash string) models.VerifiedOutput {
	info, err := os.Stat(path)
	size := int64(0)
	exists := err == nil
	if exists {
		size = info.Size()
	}

	return models.VerifiedOutput{
		Path:      path,
		Exists:    exists,
		SizeBytes: size,
		Kind:      "text",
		Checks: []models.VerificationCheck{{
			Key:    archiveSidecarCheck,
			Label:  "SHA-256 sidecar",
			Passed: exists && size > 0,
			Value:  hash,
			Detail: checkDetail(!exists || size == 0, "the sidecar is missing or empty"),
		}},
	}
}

func archiveVerifyMessage(facts archiveDocumentFacts) string {
	return fmt.Sprintf(
		"%s, %d %s, SHA-256 %s. %s",
		archivePDFAMarkerValue(facts.PDFAPart, facts.Conformance),
		facts.PageCount,
		pluralizePage(facts.PageCount),
		facts.ReportedHash,
		archiveComplianceCaveat,
	)
}

func archiveResultMessage(name, hash string) string {
	return fmt.Sprintf(
		"Archive document ready: %s (PDF/A-2 produced by Ghostscript, not validated with veraPDF). SHA-256: %s.",
		name,
		hash,
	)
}

func checkDetail(failed bool, detail string) string {
	if failed {
		return detail
	}

	return ""
}

func pluralizePage(count int) string {
	if count == 1 {
		return "page"
	}

	return "pages"
}
