package engine

import (
	"context"
	"errors"
	"fileforge-desktop/internal/platform/execx"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"unicode"

	"fileforge-desktop/internal/platform/fsx"
	"fileforge-desktop/internal/utils"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// OCR pipeline constants. The defaults match the plan shown to the user: every
// page is rasterized at 300 DPI before Tesseract runs.
const (
	DefaultOCRDPI = 300
	MinOCRDPI     = 150
	MaxOCRDPI     = 600
)

// OCR error codes. Validation reuses the PDF engine codes already understood by
// the UI (VALIDATION_ERROR, PDF_INVALID_INPUT) while runtime and pipeline
// failures use OCR-specific detail codes.
const (
	OCRCodeRuntimeUnavailable  = "OCR_RUNTIME_UNAVAILABLE"
	OCRCodeLanguageUnavailable = "OCR_LANGUAGE_UNAVAILABLE"
	OCRCodeRasterizeFailed     = "OCR_RASTERIZE_FAILED"
	OCRCodeRecognizeFailed     = "OCR_RECOGNIZE_FAILED"
	OCRCodeMergeFailed         = "OCR_MERGE_FAILED"
	OCRCodeVerifyFailed        = "OCR_VERIFY_FAILED"
	OCRCodePublishFailed       = "OCR_PUBLISH_FAILED"
	OCRCodeOutputExists        = "OCR_OUTPUT_ALREADY_EXISTS"
	OCRCodeOutputDirInvalid    = "OCR_OUTPUT_DIR_INVALID"
	// OCRCodeNoText is a warning-only code: it is never returned as a failure.
	OCRCodeNoText      = "OCR_NO_TEXT"
	OCRCodeCancelled   = "OCR_CANCELLED"
	ocrLanguageOSD     = "osd"
	ocrPageImagePrefix = "page-"
	ocrTempDirPattern  = "fileforge-ocr-*"
	ocrTempFilePattern = ".fileforge-ocr-*.tmp"

	// ocrPageWorkers caps concurrent Tesseract processes. Each one can use
	// hundreds of MB, so the batch stays well below the core count.
	ocrPageWorkers = 4
)

const (
	// OCRWarningNoText is added when pdftotext cannot extract any text from the
	// published PDF, which means the scan is not searchable.
	OCRWarningNoText = "No text was recognized."
	// OCRWarningOptimizeFailed keeps the merged OCR PDF when pdfcpu refuses to
	// optimize it.
	OCRWarningOptimizeFailed = "PDF optimization failed; the merged OCR file is used."
	// OCRWarningPDFToTextUnavailable falls back to the sidecar text when
	// pdftotext is not installed.
	OCRWarningPDFToTextUnavailable = "pdftotext is not available; the text layer was verified from the sidecar text."
	// OCRWarningOptimizeLostText keeps the merged OCR PDF when optimization
	// drops the searchable text layer.
	OCRWarningOptimizeLostText = "PDF optimization removed the text layer; the merged OCR file is used."
)

// ocrLookPath reports whether a runtime binary is available. It is a package
// variable so tests can simulate a machine without OCR dependencies.
var ocrLookPath = exec.LookPath

// CommandRunner runs an external command and returns its combined output.
// stdout and stderr are merged so a failed command can report the tool message.
type CommandRunner interface {
	Output(ctx context.Context, name string, args []string) ([]byte, error)
}

// ExecCommandRunner runs real binaries with exec.CommandContext.
type ExecCommandRunner struct{}

func (r *ExecCommandRunner) Output(ctx context.Context, name string, args []string) ([]byte, error) {
	command := execx.CommandContext(ctx, name, args...)

	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return output, fmt.Errorf("%s failed: %w", name, err)
		}

		return output, fmt.Errorf("%s failed: %w: %s", name, err, message)
	}

	return output, nil
}

func resolveCommandRunner(runner CommandRunner) CommandRunner {
	if runner == nil {
		return &ExecCommandRunner{}
	}

	return runner
}

// OCRCapabilities is the detected OCR runtime: required binaries, optional text
// extraction and the installed Tesseract languages (osd excluded).
type OCRCapabilities struct {
	PDFToPPM            bool
	Tesseract           bool
	PDFToText           bool
	Languages           []string
	RecommendedLanguage string
}

// Available reports whether the full rasterize + recognize pipeline can run.
func (c OCRCapabilities) Available() bool {
	return c.PDFToPPM && c.Tesseract && len(c.Languages) > 0
}

// HasLanguage reports whether every part of a Tesseract language expression
// ("eng", "eng+spa") is installed.
func (c OCRCapabilities) HasLanguage(language string) bool {
	expressions := strings.Split(strings.TrimSpace(language), "+")
	for _, expression := range expressions {
		trimmed := strings.TrimSpace(expression)
		if trimmed == "" {
			return false
		}

		found := false
		for _, installed := range c.Languages {
			if installed == trimmed {
				found = true
				break
			}
		}
		if !found {
			return false
		}
	}

	return len(expressions) > 0
}

// DetectCapabilities probes the local OCR runtime. A runner may be nil, in
// which case real binaries are executed.
func DetectCapabilities(ctx context.Context, runner CommandRunner) OCRCapabilities {
	if ctx == nil {
		ctx = context.Background()
	}

	capabilities := OCRCapabilities{
		PDFToPPM:  ocrBinaryAvailable("pdftoppm"),
		Tesseract: ocrBinaryAvailable("tesseract"),
		PDFToText: ocrBinaryAvailable("pdftotext"),
		Languages: []string{},
	}
	if !capabilities.Tesseract {
		return capabilities
	}

	output, err := resolveCommandRunner(runner).Output(ctx, "tesseract", []string{"--list-langs"})
	if err != nil {
		return capabilities
	}

	capabilities.Languages = parseTesseractLanguages(string(output))
	capabilities.RecommendedLanguage = recommendOCRLanguage(capabilities.Languages)

	return capabilities
}

func ocrBinaryAvailable(name string) bool {
	_, err := ocrLookPath(name)
	return err == nil
}

// parseTesseractLanguages reads the `tesseract --list-langs` payload: the
// header line is skipped, osd is excluded (it is script detection, not a
// language), and the result is deduped and sorted.
func parseTesseractLanguages(payload string) []string {
	languages := make([]string, 0, 8)
	seen := make(map[string]struct{}, 8)

	for _, line := range strings.Split(payload, "\n") {
		trimmed := strings.TrimSpace(line)
		if trimmed == "" {
			continue
		}
		if strings.HasSuffix(trimmed, ":") || strings.Contains(strings.ToLower(trimmed), "available languages") {
			continue
		}
		if trimmed == ocrLanguageOSD {
			continue
		}
		if _, exists := seen[trimmed]; exists {
			continue
		}

		seen[trimmed] = struct{}{}
		languages = append(languages, trimmed)
	}

	sort.Strings(languages)

	return languages
}

// recommendOCRLanguage prefers eng, then spa, then the first installed
// language (the list is already sorted).
func recommendOCRLanguage(languages []string) string {
	for _, preferred := range []string{"eng", "spa"} {
		for _, installed := range languages {
			if installed == preferred {
				return preferred
			}
		}
	}
	if len(languages) > 0 {
		return languages[0]
	}

	return ""
}

// OCRRequest describes one PDF OCR run. DPI defaults to DefaultOCRDPI; the text
// sidecar is optional.
type OCRRequest struct {
	InputPath      string
	OutputPDFPath  string
	OutputTextPath string
	Language       string
	DPI            int
}

// OCRResult reports what the pipeline produced. Warnings never fail the run.
type OCRResult struct {
	PageCount      int
	TextCharacters int
	Optimized      bool
	Warnings       []string
}

// OCRError is a typed OCR failure with a stable code the tool maps to a
// canonical job error.
type OCRError struct {
	Code    string
	Message string
	Cause   error
}

func (e *OCRError) Error() string {
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *OCRError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// ValidateOCRInput checks that inputPath is an existing regular .pdf file
// without parsing the document, mirroring the other PDF tools' plan checks.
func ValidateOCRInput(inputPath string) *OCRError {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return &OCRError{Code: ErrorCodeValidation, Message: "inputPath is required"}
	}
	if !isPDFPath(inputPath) {
		return &OCRError{Code: ErrorCodeValidation, Message: fmt.Sprintf("input file must be .pdf: %s", inputPath)}
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &OCRError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input PDF was not found: %s", filepath.Base(inputPath)), Cause: err}
		}

		return &OCRError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input PDF is not accessible: %s", filepath.Base(inputPath)), Cause: err}
	}
	if info.IsDir() {
		return &OCRError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input path is a directory: %s", filepath.Base(inputPath))}
	}

	return nil
}

// ValidateOCRRequest checks the request paths without touching the files the
// pipeline will write. The output PDF must be a non-existing .pdf and the
// optional sidecar must be a non-existing .txt in a writable directory.
func ValidateOCRRequest(inputPath, outputPDFPath, outputTextPath string) *OCRError {
	inputPath = strings.TrimSpace(inputPath)
	if validationErr := ValidateOCRInput(inputPath); validationErr != nil {
		return validationErr
	}

	outputPDFPath = strings.TrimSpace(outputPDFPath)
	if outputPDFPath == "" {
		return &OCRError{Code: ErrorCodeValidation, Message: "outputPath is required"}
	}
	if !isPDFPath(outputPDFPath) {
		return &OCRError{Code: ErrorCodeValidation, Message: "outputPath must use .pdf extension"}
	}

	outputTextPath = strings.TrimSpace(outputTextPath)
	if outputTextPath != "" && !strings.EqualFold(filepath.Ext(outputTextPath), ".txt") {
		return &OCRError{Code: ErrorCodeValidation, Message: "textOutputPath must use .txt extension"}
	}

	if fsx.NormalizePathKey(inputPath) == fsx.NormalizePathKey(outputPDFPath) {
		return &OCRError{Code: ErrorCodeOutputCollidesInput, Message: fmt.Sprintf("outputPath collides with input path: %s", inputPath)}
	}
	if outputTextPath != "" && fsx.NormalizePathKey(outputTextPath) == fsx.NormalizePathKey(inputPath) {
		return &OCRError{Code: ErrorCodeOutputCollidesInput, Message: fmt.Sprintf("textOutputPath collides with input path: %s", inputPath)}
	}
	if outputTextPath != "" && fsx.NormalizePathKey(outputTextPath) == fsx.NormalizePathKey(outputPDFPath) {
		return &OCRError{Code: ErrorCodeValidation, Message: "textOutputPath must differ from outputPath"}
	}

	if existsErr := ensureOCROutputAbsent(outputPDFPath); existsErr != nil {
		return existsErr
	}
	if outputTextPath != "" {
		if existsErr := ensureOCROutputAbsent(outputTextPath); existsErr != nil {
			return existsErr
		}
	}

	if dirErr := validateOCROutputDir(filepath.Dir(outputPDFPath)); dirErr != nil {
		return dirErr
	}
	if outputTextPath != "" {
		if dirErr := validateOCROutputDir(filepath.Dir(outputTextPath)); dirErr != nil {
			return dirErr
		}
	}

	return nil
}

func ensureOCROutputAbsent(outputPath string) *OCRError {
	_, err := os.Stat(outputPath)
	if err == nil {
		return &OCRError{Code: OCRCodeOutputExists, Message: fmt.Sprintf("output already exists: %s", outputPath)}
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return &OCRError{Code: OCRCodeOutputDirInvalid, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: err}
}

// ValidateOCRDestination checks that dir exists, is a directory and accepts
// writes, so an auto output can be promised safely.
func ValidateOCRDestination(dir string) *OCRError {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return &OCRError{Code: ErrorCodeValidation, Message: "output directory is required"}
	}

	return validateOCROutputDir(filepath.Clean(dir))
}

func validateOCROutputDir(dir string) *OCRError {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "."
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &OCRError{Code: OCRCodeOutputDirInvalid, Message: fmt.Sprintf("output directory does not exist: %s", dir), Cause: err}
		}

		return &OCRError{Code: OCRCodeOutputDirInvalid, Message: fmt.Sprintf("output directory is not accessible: %s", dir), Cause: err}
	}
	if !info.IsDir() {
		return &OCRError{Code: OCRCodeOutputDirInvalid, Message: fmt.Sprintf("output path parent is not a directory: %s", dir)}
	}

	probe, probeErr := os.CreateTemp(dir, ".fileforge-ocr-writecheck-*.tmp")
	if probeErr != nil {
		return &OCRError{Code: OCRCodeOutputDirInvalid, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: probeErr}
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)

	return nil
}

// Convert runs the local OCR pipeline for one PDF: rasterize with pdftoppm,
// recognize every page with tesseract, merge the page PDFs, optimize, verify the
// text layer and publish the searchable PDF plus an optional text sidecar.
func Convert(ctx context.Context, runner CommandRunner, req OCRRequest, onProgress func(current, total int, message string)) (OCRResult, *OCRError) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return OCRResult{}, cancelledOCRError("OCR canceled", ctx.Err())
	}

	runner = resolveCommandRunner(runner)
	capabilities := DetectCapabilities(ctx, runner)
	if ctx.Err() != nil {
		return OCRResult{}, cancelledOCRError("OCR canceled", ctx.Err())
	}

	return ConvertWithCapabilities(ctx, runner, capabilities, req, onProgress)
}

// ConvertWithCapabilities runs the OCR pipeline with a runtime already detected
// by the caller. Batch executors detect the runtime once per job and reuse it
// for every input instead of probing Tesseract again per file.
func ConvertWithCapabilities(ctx context.Context, runner CommandRunner, capabilities OCRCapabilities, req OCRRequest, onProgress func(current, total int, message string)) (OCRResult, *OCRError) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctx.Err() != nil {
		return OCRResult{}, cancelledOCRError("OCR canceled", ctx.Err())
	}

	return convertWithCapabilities(ctx, resolveCommandRunner(runner), capabilities, req, onProgress)
}

func convertWithCapabilities(ctx context.Context, runner CommandRunner, capabilities OCRCapabilities, req OCRRequest, onProgress func(current, total int, message string)) (OCRResult, *OCRError) {
	if !capabilities.PDFToPPM || !capabilities.Tesseract {
		return OCRResult{}, &OCRError{
			Code:    OCRCodeRuntimeUnavailable,
			Message: "local OCR requires pdftoppm and tesseract",
		}
	}
	if len(capabilities.Languages) == 0 {
		return OCRResult{}, &OCRError{
			Code:    OCRCodeLanguageUnavailable,
			Message: "no Tesseract language data is installed; install eng or spa language data",
		}
	}

	if validationErr := ValidateOCRRequest(req.InputPath, req.OutputPDFPath, req.OutputTextPath); validationErr != nil {
		return OCRResult{}, validationErr
	}

	language := strings.TrimSpace(req.Language)
	if language == "" {
		language = capabilities.RecommendedLanguage
	}
	if !capabilities.HasLanguage(language) {
		return OCRResult{}, &OCRError{
			Code: OCRCodeLanguageUnavailable,
			Message: fmt.Sprintf(
				"Tesseract language '%s' is not installed. Installed languages: %s.",
				language,
				strings.Join(capabilities.Languages, ", "),
			),
		}
	}

	dpi := req.DPI
	if dpi == 0 {
		dpi = DefaultOCRDPI
	}
	if dpi < MinOCRDPI || dpi > MaxOCRDPI {
		return OCRResult{}, &OCRError{
			Code:    ErrorCodeValidation,
			Message: fmt.Sprintf("dpi must be between %d and %d", MinOCRDPI, MaxOCRDPI),
		}
	}

	inputPath := strings.TrimSpace(req.InputPath)
	sourcePages, err := api.PageCountFile(pdfCPUContext(ctx), inputPath)
	if err != nil {
		return OCRResult{}, &OCRError{
			Code:    ErrorCodeInvalidInputPDF,
			Message: fmt.Sprintf("failed to read the PDF page count: %s", filepath.Base(inputPath)),
			Cause:   err,
		}
	}
	if sourcePages <= 0 {
		return OCRResult{}, &OCRError{Code: ErrorCodeValidation, Message: "the PDF has no pages"}
	}

	tempDir, err := os.MkdirTemp("", ocrTempDirPattern)
	if err != nil {
		return OCRResult{}, &OCRError{Code: OCRCodeRasterizeFailed, Message: "could not create the OCR workspace", Cause: err}
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	pageImages, rasterizeErr := rasterizeOCRPages(ctx, runner, inputPath, tempDir, dpi, sourcePages)
	if rasterizeErr != nil {
		return OCRResult{}, rasterizeErr
	}

	// Pages are independent: recognize them with a small bounded pool, write
	// results by page index, then assemble the sidecar and the PDF list in
	// document order.
	type ocrPageResult struct {
		pdfPath string
		text    string
		chars   int
		err     *OCRError
	}

	pageResults := make([]ocrPageResult, len(pageImages))
	var progressMu sync.Mutex
	completed := 0

	runErr := utils.ParallelForIndex(ctx, len(pageImages), ocrPageWorkerCount(), func(ctx context.Context, index int) error {
		pageNumber := index + 1
		pageBase := filepath.Join(tempDir, fmt.Sprintf("ocr-%d", pageNumber))

		if _, recognizeErr := runner.Output(ctx, "tesseract", []string{
			pageImages[index],
			pageBase,
			"-l", language,
			"--dpi", strconv.Itoa(dpi),
			"pdf", "txt",
		}); recognizeErr != nil {
			if ctx.Err() != nil {
				return ctx.Err()
			}

			pageResults[index].err = &OCRError{
				Code:    OCRCodeRecognizeFailed,
				Message: fmt.Sprintf("failed to recognize page %d of %d", pageNumber, len(pageImages)),
				Cause:   recognizeErr,
			}
			return nil
		}

		if outputErr := validateTesseractPageOutputs(pageBase, pageNumber); outputErr != nil {
			pageResults[index].err = outputErr
			return nil
		}

		pageText, readErr := os.ReadFile(pageBase + ".txt")
		if readErr != nil {
			pageResults[index].err = &OCRError{
				Code:    OCRCodeRecognizeFailed,
				Message: fmt.Sprintf("failed to read the recognized text of page %d", pageNumber),
				Cause:   readErr,
			}
			return nil
		}

		pageResults[index] = ocrPageResult{
			pdfPath: pageBase + ".pdf",
			text:    string(pageText),
			chars:   CountTextCharacters(string(pageText)),
		}

		if onProgress != nil {
			progressMu.Lock()
			completed++
			current := completed
			onProgress(current, len(pageImages), fmt.Sprintf("Recognized %d of %d pages", current, len(pageImages)))
			progressMu.Unlock()
		}

		return nil
	})

	if runErr != nil {
		if ctx.Err() != nil {
			return OCRResult{}, cancelledOCRError("OCR canceled", ctx.Err())
		}

		return OCRResult{}, &OCRError{
			Code:    OCRCodeRecognizeFailed,
			Message: "failed to recognize the PDF pages",
			Cause:   runErr,
		}
	}

	pagePDFs := make([]string, 0, len(pageResults))
	var sidecar strings.Builder
	sidecarCharacters := 0
	for index, result := range pageResults {
		if result.err != nil {
			return OCRResult{}, result.err
		}
		if index > 0 {
			sidecar.WriteString("\f")
		}
		sidecar.WriteString(result.text)
		sidecarCharacters += result.chars
		pagePDFs = append(pagePDFs, result.pdfPath)
	}

	if ctx.Err() != nil {
		return OCRResult{}, cancelledOCRError("OCR canceled", ctx.Err())
	}

	mergedPath := filepath.Join(tempDir, "merged.pdf")
	if mergeErr := api.MergeCreateFile(pdfCPUContext(ctx), pagePDFs, mergedPath, false, newPDFCPUConfig()); mergeErr != nil {
		return OCRResult{}, &OCRError{Code: OCRCodeMergeFailed, Message: "failed to merge the recognized pages", Cause: mergeErr}
	}

	warnings := make([]string, 0, 3)
	chosenPath := mergedPath
	optimized := false
	optimizedPath := filepath.Join(tempDir, "optimized.pdf")
	if optimizeErr := api.OptimizeFile(pdfCPUContext(ctx), mergedPath, optimizedPath, newPDFCPUConfig(), nil); optimizeErr != nil {
		warnings = append(warnings, OCRWarningOptimizeFailed)
	} else {
		chosenPath = optimizedPath
		optimized = true

		// pdfcpu normally preserves the invisible text layer. When it does not,
		// prefer the merged Tesseract PDF: a searchable result matters more than
		// the size win.
		if capabilities.PDFToText && sidecarCharacters > 0 {
			if optimizedText, ok := ExtractPDFText(ctx, runner, optimizedPath); ok &&
				CountTextCharacters(optimizedText) == 0 {
				chosenPath = mergedPath
				optimized = false
				warnings = append(warnings, OCRWarningOptimizeLostText)
			}
		}
	}

	textCharacters := sidecarCharacters
	if capabilities.PDFToText {
		extractedText, ok := ExtractPDFText(ctx, runner, chosenPath)
		if !ok {
			warnings = append(warnings, OCRWarningPDFToTextUnavailable)
		} else {
			extractedCharacters := CountTextCharacters(extractedText)
			if extractedCharacters == 0 {
				warnings = append(warnings, OCRWarningNoText)
			} else {
				textCharacters = extractedCharacters
			}
		}
	} else {
		warnings = append(warnings, OCRWarningPDFToTextUnavailable)
	}

	verifyErr := verifyOCRPageCount(ctx, chosenPath, sourcePages)
	if verifyErr != nil {
		return OCRResult{}, verifyErr
	}

	if ctx.Err() != nil {
		return OCRResult{}, cancelledOCRError("OCR canceled", ctx.Err())
	}

	if publishErr := publishOCRFile(chosenPath, strings.TrimSpace(req.OutputPDFPath)); publishErr != nil {
		return OCRResult{}, &OCRError{Code: OCRCodePublishFailed, Message: "failed to write the searchable PDF", Cause: publishErr}
	}

	outputTextPath := strings.TrimSpace(req.OutputTextPath)
	if outputTextPath != "" {
		if writeErr := writeOCRTextFile(outputTextPath, sidecar.String()); writeErr != nil {
			_ = os.Remove(strings.TrimSpace(req.OutputPDFPath))
			return OCRResult{}, &OCRError{Code: OCRCodePublishFailed, Message: "failed to write the text sidecar", Cause: writeErr}
		}
	}

	if _, statErr := os.Stat(strings.TrimSpace(req.OutputPDFPath)); statErr != nil {
		return OCRResult{}, &OCRError{Code: OCRCodeVerifyFailed, Message: "the searchable PDF is missing after publishing", Cause: statErr}
	}

	return OCRResult{
		PageCount:      sourcePages,
		TextCharacters: textCharacters,
		Optimized:      optimized,
		Warnings:       warnings,
	}, nil
}

// ocrPageWorkerCount bounds concurrent Tesseract processes.
func ocrPageWorkerCount() int {
	workers := utils.ParallelWorkerCount()
	if workers > ocrPageWorkers {
		workers = ocrPageWorkers
	}
	if workers < 1 {
		workers = 1
	}

	return workers
}

// validateTesseractPageOutputs catches a silent Tesseract failure. When the PDF
// config or pdf.ttf is missing, tesseract exits successfully but writes no PDF,
// and the pipeline would later fail while merging files that do not exist.
func validateTesseractPageOutputs(pageBase string, pageNumber int) *OCRError {
	pdfPath := pageBase + ".pdf"
	if info, err := os.Stat(pdfPath); err != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		return &OCRError{
			Code:    OCRCodeRecognizeFailed,
			Message: fmt.Sprintf("Tesseract did not produce the searchable PDF for page %d. Install or repair the OCR data.", pageNumber),
			Cause:   err,
		}
	}

	// A blank page legitimately produces an empty text file, so only its
	// presence is required.
	textPath := pageBase + ".txt"
	if info, err := os.Stat(textPath); err != nil || !info.Mode().IsRegular() {
		return &OCRError{
			Code:    OCRCodeRecognizeFailed,
			Message: fmt.Sprintf("Tesseract did not produce the text for page %d.", pageNumber),
			Cause:   err,
		}
	}

	return nil
}

// rasterizeOCRPages renders every page to PNG and returns the page images in
// document order. pdftoppm pads the page number to the document width, so the
// suffix is parsed numerically instead of relying on the file name order.
func rasterizeOCRPages(ctx context.Context, runner CommandRunner, inputPath, tempDir string, dpi, expectedPages int) ([]string, *OCRError) {
	prefix := filepath.Join(tempDir, "page")
	if _, err := runner.Output(ctx, "pdftoppm", []string{
		"-r", strconv.Itoa(dpi),
		"-png",
		inputPath,
		prefix,
	}); err != nil {
		if ctx.Err() != nil {
			return nil, cancelledOCRError("OCR canceled", ctx.Err())
		}

		return nil, &OCRError{Code: OCRCodeRasterizeFailed, Message: "failed to rasterize the PDF pages", Cause: err}
	}

	pageImages, err := collectOCRPageImages(tempDir)
	if err != nil {
		return nil, &OCRError{Code: OCRCodeRasterizeFailed, Message: "failed to read the rasterized pages", Cause: err}
	}
	if len(pageImages) != expectedPages {
		return nil, &OCRError{
			Code:    OCRCodeRasterizeFailed,
			Message: fmt.Sprintf("expected %d rasterized pages, got %d", expectedPages, len(pageImages)),
		}
	}

	return pageImages, nil
}

func collectOCRPageImages(dir string) ([]string, error) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		return nil, err
	}

	type pageImage struct {
		path   string
		number int
	}

	images := make([]pageImage, 0, len(entries))
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}

		name := entry.Name()
		if !strings.HasPrefix(name, ocrPageImagePrefix) || !strings.EqualFold(filepath.Ext(name), ".png") {
			continue
		}

		rawNumber := strings.TrimSuffix(strings.TrimPrefix(name, ocrPageImagePrefix), filepath.Ext(name))
		number, parseErr := strconv.Atoi(rawNumber)
		if parseErr != nil {
			continue
		}

		images = append(images, pageImage{path: filepath.Join(dir, name), number: number})
	}

	sort.Slice(images, func(left, right int) bool { return images[left].number < images[right].number })

	paths := make([]string, 0, len(images))
	for _, image := range images {
		paths = append(paths, image.path)
	}

	return paths, nil
}

func verifyOCRPageCount(ctx context.Context, pdfPath string, expectedPages int) *OCRError {
	pages, err := api.PageCountFile(pdfCPUContext(ctx), pdfPath)
	if err != nil {
		return &OCRError{Code: OCRCodeVerifyFailed, Message: "failed to verify the searchable PDF page count", Cause: err}
	}
	if pages != expectedPages {
		return &OCRError{
			Code:    OCRCodeVerifyFailed,
			Message: fmt.Sprintf("the searchable PDF has %d pages but the source has %d", pages, expectedPages),
		}
	}

	return nil
}

// ExtractPDFText extracts the text layer of a PDF with pdftotext. The bool is
// false when pdftotext is unavailable or the extraction failed; callers fall
// back to the sidecar text in that case.
func ExtractPDFText(ctx context.Context, runner CommandRunner, pdfPath string) (string, bool) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !ocrBinaryAvailable("pdftotext") {
		return "", false
	}

	trimmed := strings.TrimSpace(pdfPath)
	if trimmed == "" {
		return "", false
	}

	output, err := resolveCommandRunner(runner).Output(ctx, "pdftotext", []string{"-q", trimmed, "-"})
	if err != nil {
		return "", false
	}

	return string(output), true
}

// CountTextCharacters counts non-whitespace characters, ignoring spaces, tabs,
// newlines and form feeds.
func CountTextCharacters(text string) int {
	count := 0
	for _, char := range text {
		if !unicode.IsSpace(char) {
			count++
		}
	}

	return count
}

// publishOCRFile copies sourcePath into outputPath atomically: a temp file in
// the destination folder, then a rename. An existing destination is never
// overwritten.
func publishOCRFile(sourcePath, outputPath string) error {
	destinationDir := filepath.Dir(outputPath)
	tempFile, err := os.CreateTemp(destinationDir, ocrTempFilePattern)
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	source, openErr := os.Open(sourcePath)
	if openErr != nil {
		_ = tempFile.Close()
		return openErr
	}

	_, copyErr := io.Copy(tempFile, source)
	closeSourceErr := source.Close()
	closeTempErr := tempFile.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeSourceErr != nil {
		return closeSourceErr
	}
	if closeTempErr != nil {
		return closeTempErr
	}

	// CreateTemp uses 0600; publish a readable file like the other PDF tools.
	if chmodErr := os.Chmod(tempPath, 0o644); chmodErr != nil {
		return chmodErr
	}

	if _, statErr := os.Stat(outputPath); statErr == nil {
		return fmt.Errorf("output already exists: %s", outputPath)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	return os.Rename(tempPath, outputPath)
}

// writeOCRTextFile writes the sidecar atomically; an existing destination is
// never overwritten.
func writeOCRTextFile(outputPath, content string) error {
	tempFile, err := os.CreateTemp(filepath.Dir(outputPath), ocrTempFilePattern)
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, writeErr := tempFile.WriteString(content); writeErr != nil {
		_ = tempFile.Close()
		return writeErr
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		return closeErr
	}

	if chmodErr := os.Chmod(tempPath, 0o644); chmodErr != nil {
		return chmodErr
	}

	if _, statErr := os.Stat(outputPath); statErr == nil {
		return fmt.Errorf("output already exists: %s", outputPath)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	return os.Rename(tempPath, outputPath)
}

func cancelledOCRError(message string, cause error) *OCRError {
	return &OCRError{Code: OCRCodeCancelled, Message: message, Cause: cause}
}
