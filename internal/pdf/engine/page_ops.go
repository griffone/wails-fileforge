package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"fileforge-desktop/internal/outputplan"
	"fileforge-desktop/internal/platform/fsx"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

// PDF page operations: rotate, remove pages, reorder and extract images.
// Every operation validates the page selection against the real page count,
// writes through pdfcpu's staged output where available, and reads the result
// back before reporting success.

const (
	// RotateAngleCW90 etc. are the supported clockwise rotation steps. pdfcpu
	// accepts any multiple of 90; the product only exposes these three.
	RotateAngleCW90  = 90
	RotateAngleCW180 = 180
	RotateAngleCW270 = 270

	ErrorCodeRotateFailed              = "PDF_ROTATE_FAILED"
	ErrorCodeRotateAngleInvalid        = "PDF_ROTATE_ANGLE_INVALID"
	ErrorCodeRotatePageSelectionBad    = "PDF_ROTATE_PAGE_SELECTION_INVALID"
	ErrorCodeRotatePageSelectionBounds = "PDF_ROTATE_PAGE_SELECTION_OUT_OF_BOUNDS"
	ErrorCodeRotateVerifyFailed        = "PDF_ROTATE_VERIFY_FAILED"

	ErrorCodeRemovePagesFailed            = "PDF_REMOVE_PAGES_FAILED"
	ErrorCodeRemovePagesSelectionRequired = "PDF_REMOVE_PAGES_SELECTION_REQUIRED"
	ErrorCodeRemovePagesSelectionBad      = "PDF_REMOVE_PAGES_SELECTION_INVALID"
	ErrorCodeRemovePagesSelectionBounds   = "PDF_REMOVE_PAGES_SELECTION_OUT_OF_BOUNDS"
	ErrorCodeRemovePagesAllPages          = "PDF_REMOVE_PAGES_ALL_PAGES"
	ErrorCodeRemovePagesVerifyFailed      = "PDF_REMOVE_PAGES_VERIFY_FAILED"

	ErrorCodeReorderFailed          = "PDF_REORDER_FAILED"
	ErrorCodeReorderOrderRequired   = "PDF_REORDER_ORDER_REQUIRED"
	ErrorCodeReorderOrderInvalid    = "PDF_REORDER_ORDER_INVALID"
	ErrorCodeReorderPageOutOfBounds = "PDF_REORDER_PAGE_OUT_OF_BOUNDS"
	ErrorCodeReorderPageMissing     = "PDF_REORDER_PAGE_MISSING"
	ErrorCodeReorderPageDuplicate   = "PDF_REORDER_PAGE_DUPLICATE"
	ErrorCodeReorderVerifyFailed    = "PDF_REORDER_VERIFY_FAILED"

	ErrorCodeExtractImagesFailed          = "PDF_EXTRACT_IMAGES_FAILED"
	ErrorCodeExtractImagesNone            = "PDF_EXTRACT_IMAGES_NONE"
	ErrorCodeExtractImagesSelectionBad    = "PDF_EXTRACT_IMAGES_PAGE_SELECTION_INVALID"
	ErrorCodeExtractImagesSelectionBounds = "PDF_EXTRACT_IMAGES_PAGE_SELECTION_OUT_OF_BOUNDS"
	ErrorCodeExtractImagesCountMismatch   = "PDF_EXTRACT_IMAGES_COUNT_MISMATCH"

	// ErrorCodeOutputAlreadyExists guards a destination that appeared between
	// planning and execution. Auto-derived names are suffix-adjusted at plan
	// time, so this is a race guard, not a normal user error.
	ErrorCodeOutputAlreadyExists = "PDF_OUTPUT_ALREADY_EXISTS"

	// pageOpCanceledCode is the shared cancellation detail code. The tool layer
	// and the retry policy treat any CANCELLED/CANCELED spelling as terminal.
	pageOpCanceledCode = "CANCELED"

	reorderReverseKeyword = "reverse"
)

// PageOpError is the error type shared by the four page operations. Each
// operation keeps its own stable detail codes so the canonical table and the
// retry allowlist can classify them independently.
type PageOpError struct {
	Code    string
	Message string
	Details map[string]any
	Cause   error
}

func (e *PageOpError) Error() string {
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *PageOpError) Unwrap() error {
	return e.Cause
}

func pageOpCanceled(message string, cause error) *PageOpError {
	return &PageOpError{Code: pageOpCanceledCode, Message: message, Cause: cause}
}

// ValidatePageOpInput checks that inputPath is an existing .pdf file without
// parsing it. Planning uses it so one unreadable PDF cannot fail a whole batch
// before its per-item execution.
func ValidatePageOpInput(inputPath string) *PageOpError {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return &PageOpError{Code: ErrorCodeValidation, Message: "inputPath is required"}
	}

	if !isPDFPath(inputPath) {
		return &PageOpError{Code: ErrorCodeValidation, Message: fmt.Sprintf("input file must be .pdf: %s", inputPath)}
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &PageOpError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input PDF was not found: %s", filepath.Base(inputPath)), Cause: err}
		}
		return &PageOpError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input PDF is not accessible: %s", filepath.Base(inputPath)), Cause: err}
	}

	if info.IsDir() {
		return &PageOpError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input path is a directory: %s", filepath.Base(inputPath))}
	}

	return nil
}

// ValidatePageOpDestination checks that dir exists and accepts writes, so a
// plan never promises an output path that cannot be created.
func ValidatePageOpDestination(dir string) *PageOpError {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return &PageOpError{Code: ErrorCodeValidation, Message: "output directory is required"}
	}

	return pageOpProbeOutputDir(filepath.Join(filepath.Clean(dir), "_page_op_probe_.pdf"))
}

// PageOpPageCount returns the page count of inputPath, or a PDF_INVALID_INPUT
// error when the document cannot be read.
func PageOpPageCount(inputPath string) (int, *PageOpError) {
	count, err := api.PageCountFile(context.Background(), strings.TrimSpace(inputPath))
	if err != nil {
		return 0, &PageOpError{
			Code:    ErrorCodeInvalidInputPDF,
			Message: fmt.Sprintf("unable to determine the page count of %s", filepath.Base(strings.TrimSpace(inputPath))),
			Cause:   err,
		}
	}
	if count < 1 {
		return 0, &PageOpError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("the PDF has no pages: %s", filepath.Base(strings.TrimSpace(inputPath)))}
	}

	return count, nil
}

func pageOpProbeOutputDir(outputPath string) *PageOpError {
	if err := validateOutputDir(outputPath); err != nil {
		return &PageOpError{Code: err.Code, Message: err.Message, Cause: err.Cause}
	}

	return nil
}

// ValidateRotateOptions validates the rotation angle and the page selection
// syntax without touching the filesystem. An empty selection means every page.
func ValidateRotateOptions(pageSelection string, angle int) *PageOpError {
	if !isSupportedRotateAngle(angle) {
		return &PageOpError{
			Code:    ErrorCodeRotateAngleInvalid,
			Message: fmt.Sprintf("rotation angle must be 90, 180 or 270 degrees, got %d", angle),
		}
	}

	_, _, selectionErr := resolvePageSelection(pageSelection, 0, ErrorCodeRotatePageSelectionBad)
	return selectionErr
}

// ValidateRotatePageSelection validates the selection against a known page
// count. A non-positive pageCount means "unknown"; planning uses that to defer
// the check to execution for an unreadable PDF.
func ValidateRotatePageSelection(pageSelection string, pageCount int) *PageOpError {
	if pageCount < 1 {
		return nil
	}

	_, _, selectionErr := resolvePageSelection(pageSelection, pageCount, ErrorCodeRotatePageSelectionBad)
	if selectionErr != nil {
		return selectionErr
	}

	return nil
}

// ValidateRemovePagesOptions validates that a selection is present and, when
// the page count is known, that it is in bounds and keeps at least one page.
func ValidateRemovePagesOptions(pageSelection string, pageCount int) *PageOpError {
	trimmed := strings.TrimSpace(pageSelection)
	if trimmed == "" {
		return &PageOpError{
			Code:    ErrorCodeRemovePagesSelectionRequired,
			Message: "options.pageSelection is required for remove_pages; specify the pages to delete",
		}
	}

	_, ranges, selectionErr := resolvePageSelection(pageSelection, pageCount, ErrorCodeRemovePagesSelectionBad)
	if selectionErr != nil {
		return selectionErr
	}
	if pageCount < 1 {
		return nil
	}

	removed := pageSelectionCount(ranges)
	if removed == pageCount {
		return &PageOpError{
			Code:    ErrorCodeRemovePagesAllPages,
			Message: fmt.Sprintf("refusing to remove every page: the selection covers all %d pages; keep at least one page", pageCount),
		}
	}

	return nil
}

// ValidateExtractImagesOptions validates the optional page selection. An empty
// selection means every page.
func ValidateExtractImagesOptions(pageSelection string, pageCount int) *PageOpError {
	_, _, selectionErr := resolvePageSelection(pageSelection, pageCount, ErrorCodeExtractImagesSelectionBad)
	if selectionErr != nil {
		return selectionErr
	}

	return nil
}

// ParseReorderOrder expands an explicit order spec such as "3,1,2,4-6" into
// the page sequence it requests and verifies that it covers every page of a
// pageCount-page document exactly once. "reverse" is accepted as a convenience
// for the descending sequence.
func ParseReorderOrder(order string, pageCount int) ([]int, *PageOpError) {
	pages, reverse, parseErr := parseReorderPages(order)
	if parseErr != nil {
		return nil, parseErr
	}
	if pageCount < 1 {
		return nil, &PageOpError{Code: ErrorCodeInvalidInputPDF, Message: "the PDF page count is unknown"}
	}

	if reverse {
		pages = make([]int, 0, pageCount)
		for page := pageCount; page >= 1; page-- {
			pages = append(pages, page)
		}
	}

	for _, page := range pages {
		if page > pageCount {
			return nil, &PageOpError{
				Code:    ErrorCodeReorderPageOutOfBounds,
				Message: fmt.Sprintf("order page %d does not exist; the PDF has %s", page, pluralizePageCount(pageCount)),
				Details: map[string]any{"page": page, "pageCount": pageCount},
			}
		}
	}

	seen := make(map[int]int, len(pages))
	for _, page := range pages {
		seen[page]++
	}

	duplicates := make([]int, 0)
	for page, count := range seen {
		if count > 1 {
			duplicates = append(duplicates, page)
		}
	}
	sort.Ints(duplicates)

	missing := make([]int, 0)
	for page := 1; page <= pageCount; page++ {
		if seen[page] == 0 {
			missing = append(missing, page)
		}
	}

	if len(duplicates) == 0 && len(missing) == 0 {
		return pages, nil
	}

	details := map[string]any{"pageCount": pageCount}
	problems := make([]string, 0, 2)
	if len(missing) > 0 {
		details["missing"] = missing
		problems = append(problems, "missing page(s) "+formatPageList(missing))
	}
	if len(duplicates) > 0 {
		details["duplicates"] = duplicates
		problems = append(problems, "repeated page(s) "+formatPageList(duplicates))
	}

	code := ErrorCodeReorderPageMissing
	if len(duplicates) > 0 {
		code = ErrorCodeReorderPageDuplicate
	}

	return nil, &PageOpError{
		Code:    code,
		Message: fmt.Sprintf("the order must list every page exactly once: %s; the PDF has %s", strings.Join(problems, "; "), pluralizePageCount(pageCount)),
		Details: details,
	}
}

// ValidateReorderOrderSyntax validates an order without a page count. It still
// requires a non-empty spec and rejects malformed tokens, so an obviously
// broken request fails at plan time even for an unreadable PDF.
func ValidateReorderOrderSyntax(order string) *PageOpError {
	_, _, parseErr := parseReorderPages(order)
	return parseErr
}

// parseReorderPages expands the spec into pages, or reports reverse. It does
// not know the page count, so out-of-range coverage is validated separately.
func parseReorderPages(order string) ([]int, bool, *PageOpError) {
	trimmed := strings.TrimSpace(order)
	if trimmed == "" {
		return nil, false, &PageOpError{
			Code:    ErrorCodeReorderOrderRequired,
			Message: "options.order is required; list every page once, for example 3,1,2,4-6",
		}
	}

	if strings.EqualFold(trimmed, reorderReverseKeyword) {
		return nil, true, nil
	}

	pages := make([]int, 0)
	for _, rawToken := range strings.Split(trimmed, ",") {
		token := strings.TrimSpace(rawToken)
		if token == "" {
			return nil, false, reorderSyntaxError(fmt.Sprintf("the order %q contains an empty entry", trimmed))
		}

		if !strings.Contains(token, "-") {
			page, err := strconv.Atoi(token)
			if err != nil || page < 1 {
				return nil, false, reorderSyntaxError(fmt.Sprintf("invalid order entry %q; pages start at 1", token))
			}
			pages = append(pages, page)
			continue
		}

		parts := strings.Split(token, "-")
		if len(parts) != 2 {
			return nil, false, reorderSyntaxError(fmt.Sprintf("invalid order range %q; use start-end such as 4-6", token))
		}

		start, startErr := strconv.Atoi(strings.TrimSpace(parts[0]))
		end, endErr := strconv.Atoi(strings.TrimSpace(parts[1]))
		if startErr != nil || endErr != nil || start < 1 || end < 1 {
			return nil, false, reorderSyntaxError(fmt.Sprintf("invalid order range %q; use ascending start-end such as 4-6", token))
		}
		if start > end {
			return nil, false, reorderSyntaxError(fmt.Sprintf("invalid order range %q; write the range in ascending order such as %d-%d", token, end, start))
		}

		for page := start; page <= end; page++ {
			pages = append(pages, page)
		}
	}

	return pages, false, nil
}

func reorderSyntaxError(message string) *PageOpError {
	return &PageOpError{Code: ErrorCodeReorderOrderInvalid, Message: message}
}

// Rotate rotates the selected pages (every page when pageSelection is empty) of
// inputPath clockwise by angle and writes the result to outputPath.
func Rotate(ctx context.Context, inputPath, outputPath, pageSelection string, angle int) error {
	if err := ctx.Err(); err != nil {
		return pageOpCanceled("rotate canceled", err)
	}

	if optionErr := ValidateRotateOptions(pageSelection, angle); optionErr != nil {
		return optionErr
	}

	pageCount, countErr := PageOpPageCount(inputPath)
	if countErr != nil {
		return countErr
	}

	selection, _, selectionErr := resolvePageSelection(pageSelection, pageCount, ErrorCodeRotatePageSelectionBad)
	if selectionErr != nil {
		return selectionErr
	}

	if outputErr := validatePageOpFileOutput(inputPath, outputPath); outputErr != nil {
		return outputErr
	}

	if err := api.RotateFile(pdfCPUContext(ctx), inputPath, outputPath, angle, selection, newPDFCPUConfig()); err != nil {
		return &PageOpError{Code: ErrorCodeRotateFailed, Message: fmt.Sprintf("failed to rotate %s", filepath.Base(inputPath)), Cause: err}
	}

	if verifyErr := verifyPageOpPageCount(ctx, outputPath, pageCount, ErrorCodeRotateVerifyFailed); verifyErr != nil {
		_ = os.Remove(outputPath)
		return verifyErr
	}

	return nil
}

// RemovePages deletes the selected pages from inputPath and writes the result
// to outputPath. Removing every page is refused.
func RemovePages(ctx context.Context, inputPath, outputPath, pageSelection string) error {
	if err := ctx.Err(); err != nil {
		return pageOpCanceled("remove pages canceled", err)
	}

	pageCount, countErr := PageOpPageCount(inputPath)
	if countErr != nil {
		return countErr
	}

	if optionErr := ValidateRemovePagesOptions(pageSelection, pageCount); optionErr != nil {
		return optionErr
	}

	selection, ranges, selectionErr := resolvePageSelection(pageSelection, pageCount, ErrorCodeRemovePagesSelectionBad)
	if selectionErr != nil {
		return selectionErr
	}

	if outputErr := validatePageOpFileOutput(inputPath, outputPath); outputErr != nil {
		return outputErr
	}

	if err := api.RemovePagesFile(pdfCPUContext(ctx), inputPath, outputPath, selection, newPDFCPUConfig()); err != nil {
		return &PageOpError{Code: ErrorCodeRemovePagesFailed, Message: fmt.Sprintf("failed to remove pages from %s", filepath.Base(inputPath)), Cause: err}
	}

	expected := pageCount - pageSelectionCount(ranges)
	if verifyErr := verifyPageOpPageCount(ctx, outputPath, expected, ErrorCodeRemovePagesVerifyFailed); verifyErr != nil {
		_ = os.Remove(outputPath)
		return verifyErr
	}

	return nil
}

// Reorder rewrites inputPath with the pages in the explicit order requested by
// order, which must list every page exactly once. "reverse" writes the pages in
// descending order.
func Reorder(ctx context.Context, inputPath, outputPath, order string) error {
	if err := ctx.Err(); err != nil {
		return pageOpCanceled("reorder canceled", err)
	}

	pageCount, countErr := PageOpPageCount(inputPath)
	if countErr != nil {
		return countErr
	}

	pages, orderErr := ParseReorderOrder(order, pageCount)
	if orderErr != nil {
		return orderErr
	}

	if outputErr := validatePageOpFileOutput(inputPath, outputPath); outputErr != nil {
		return outputErr
	}

	selection := make([]string, 0, len(pages))
	for _, page := range pages {
		selection = append(selection, strconv.Itoa(page))
	}

	if err := api.CollectFile(pdfCPUContext(ctx), inputPath, outputPath, selection, newPDFCPUConfig()); err != nil {
		return &PageOpError{Code: ErrorCodeReorderFailed, Message: fmt.Sprintf("failed to reorder %s", filepath.Base(inputPath)), Cause: err}
	}

	if verifyErr := verifyPageOpPageCount(ctx, outputPath, pageCount, ErrorCodeReorderVerifyFailed); verifyErr != nil {
		_ = os.Remove(outputPath)
		return verifyErr
	}

	return nil
}

// ExtractImagesResult reports what an extraction wrote. Count is the number of
// files already verified on disk.
type ExtractImagesResult struct {
	OutputDir string
	Files     []string
	Count     int
}

// ExtractImages writes every embedded image of the selected pages into
// outputDir. The directory must not exist yet: the caller plans a collision-safe
// name. The count returned by pdfcpu is compared with the files actually
// written, and a mismatch fails instead of reporting a partial extraction.
func ExtractImages(ctx context.Context, inputPath, outputDir, pageSelection string) (ExtractImagesResult, error) {
	result := ExtractImagesResult{}
	if err := ctx.Err(); err != nil {
		return result, pageOpCanceled("extract images canceled", err)
	}

	pageCount, countErr := PageOpPageCount(inputPath)
	if countErr != nil {
		return result, countErr
	}

	if optionErr := ValidateExtractImagesOptions(pageSelection, pageCount); optionErr != nil {
		return result, optionErr
	}

	selection, _, selectionErr := resolvePageSelection(pageSelection, pageCount, ErrorCodeExtractImagesSelectionBad)
	if selectionErr != nil {
		return result, selectionErr
	}

	outputDir = strings.TrimSpace(outputDir)
	if outputDir == "" {
		return result, &PageOpError{Code: ErrorCodeValidation, Message: "outputDir is required"}
	}
	outputDir = filepath.Clean(outputDir)

	if _, statErr := os.Stat(outputDir); statErr == nil {
		return result, &PageOpError{Code: ErrorCodeOutputAlreadyExists, Message: fmt.Sprintf("output directory already exists: %s", outputDir)}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return result, &PageOpError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not accessible: %s", outputDir), Cause: statErr}
	}

	if dirErr := validatePageOpCreatableDir(outputDir); dirErr != nil {
		return result, dirErr
	}

	source, openErr := os.Open(inputPath)
	if openErr != nil {
		return result, &PageOpError{Code: ErrorCodeExtractImagesFailed, Message: fmt.Sprintf("failed to open the PDF: %s", filepath.Base(inputPath)), Cause: openErr}
	}
	defer source.Close()

	images, extractErr := api.ExtractImagesRaw(pdfCPUContext(ctx), source, selection, newPDFCPUConfig())
	if extractErr != nil {
		return result, &PageOpError{Code: ErrorCodeExtractImagesFailed, Message: fmt.Sprintf("failed to extract images from %s", filepath.Base(inputPath)), Cause: extractErr}
	}

	expected := countExtractedImages(images)
	if expected == 0 {
		return result, &PageOpError{
			Code:    ErrorCodeExtractImagesNone,
			Message: fmt.Sprintf("%s has no embedded images to extract", filepath.Base(inputPath)),
		}
	}

	if err := os.Mkdir(outputDir, 0o755); err != nil {
		return result, &PageOpError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory could not be created: %s", outputDir), Cause: err}
	}

	stem := imageStem(inputPath)
	used := make(map[string]struct{})
	files := make([]string, 0, expected)
	cleanup := func() {
		for _, file := range files {
			_ = os.Remove(file)
		}
		_ = os.Remove(outputDir)
	}

	for _, pageImages := range images {
		objNrs := make([]int, 0, len(pageImages))
		for objNr := range pageImages {
			objNrs = append(objNrs, objNr)
		}
		sort.Ints(objNrs)

		for _, objNr := range objNrs {
			if ctxErr := ctx.Err(); ctxErr != nil {
				cleanup()
				return result, pageOpCanceled("extract images canceled", ctxErr)
			}

			img := pageImages[objNr]
			if img.Reader == nil {
				cleanup()
				return result, &PageOpError{Code: ErrorCodeExtractImagesFailed, Message: fmt.Sprintf("page %d image %d has no data", img.PageNr, objNr)}
			}

			base := fmt.Sprintf("%s_p%d_%s", stem, img.PageNr, imageNameSlug(img.Name))
			outputPath, _ := outputplan.UniqueFilePath(outputDir, base, imageFileType(img.FileType), used)

			if writeErr := writeExtractedImage(outputPath, img.Reader); writeErr != nil {
				cleanup()
				return result, &PageOpError{Code: ErrorCodeExtractImagesFailed, Message: fmt.Sprintf("failed to write an extracted image from %s", filepath.Base(inputPath)), Cause: writeErr}
			}

			files = append(files, outputPath)
		}
	}

	if countErr := verifyExtractedImages(outputDir, files, expected); countErr != nil {
		cleanup()
		return result, countErr
	}

	return ExtractImagesResult{OutputDir: outputDir, Files: files, Count: len(files)}, nil
}

func countExtractedImages(images []map[int]model.Image) int {
	total := 0
	for _, pageImages := range images {
		total += len(pageImages)
	}

	return total
}

func writeExtractedImage(outputPath string, reader io.Reader) error {
	file, err := os.Create(outputPath)
	if err != nil {
		return err
	}

	if _, copyErr := io.Copy(file, reader); copyErr != nil {
		_ = file.Close()
		_ = os.Remove(outputPath)
		return copyErr
	}

	if closeErr := file.Close(); closeErr != nil {
		_ = os.Remove(outputPath)
		return closeErr
	}

	return nil
}

// verifyExtractedImages reads the written files back: the count must match the
// images pdfcpu reported and every file must be non-empty.
func verifyExtractedImages(outputDir string, files []string, expected int) *PageOpError {
	if len(files) != expected {
		return &PageOpError{
			Code:    ErrorCodeExtractImagesCountMismatch,
			Message: fmt.Sprintf("expected %d extracted images, wrote %d", expected, len(files)),
			Details: map[string]any{"expected": expected, "actual": len(files)},
		}
	}

	for _, file := range files {
		info, err := os.Stat(file)
		if err != nil {
			return &PageOpError{Code: ErrorCodeExtractImagesCountMismatch, Message: fmt.Sprintf("an extracted image is missing: %s", filepath.Base(file)), Cause: err}
		}
		if info.Size() <= 0 {
			return &PageOpError{Code: ErrorCodeExtractImagesCountMismatch, Message: fmt.Sprintf("an extracted image is empty: %s", filepath.Base(file))}
		}
	}

	entries, err := os.ReadDir(outputDir)
	if err != nil {
		return &PageOpError{Code: ErrorCodeExtractImagesCountMismatch, Message: fmt.Sprintf("the output directory could not be read back: %s", outputDir), Cause: err}
	}
	if len(entries) != expected {
		return &PageOpError{
			Code:    ErrorCodeExtractImagesCountMismatch,
			Message: fmt.Sprintf("expected %d extracted images, found %d in the output directory", expected, len(entries)),
			Details: map[string]any{"expected": expected, "actual": len(entries)},
		}
	}

	return nil
}

func imageStem(inputPath string) string {
	stem := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if stem == "" {
		return "document"
	}

	return stem
}

// imageNameSlug turns a PDF resource name into a filesystem-friendly token.
func imageNameSlug(name string) string {
	var builder strings.Builder
	previousSeparator := true

	for _, r := range strings.ToLower(strings.TrimSpace(name)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			previousSeparator = false
			continue
		}
		if !previousSeparator {
			builder.WriteRune('-')
			previousSeparator = true
		}
	}

	slug := strings.Trim(builder.String(), "-")
	if slug == "" {
		return "image"
	}

	return slug
}

func imageFileType(fileType string) string {
	normalized := strings.TrimSpace(strings.ToLower(strings.TrimPrefix(fileType, ".")))
	if normalized == "" {
		return "img"
	}

	var builder strings.Builder
	for _, r := range normalized {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
		}
	}
	if builder.Len() == 0 {
		return "img"
	}

	return builder.String()
}

func validatePageOpCreatableDir(dir string) *PageOpError {
	if _, statErr := os.Stat(dir); statErr == nil {
		return &PageOpError{Code: ErrorCodeOutputAlreadyExists, Message: fmt.Sprintf("output directory already exists: %s", dir)}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return &PageOpError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not accessible: %s", dir), Cause: statErr}
	}

	if err := validateOutputDirectory(dir, true); err != nil {
		return &PageOpError{Code: err.Code, Message: err.Message, Cause: err.Cause}
	}

	return nil
}

func validatePageOpFileOutput(inputPath, outputPath string) *PageOpError {
	inputPath = strings.TrimSpace(inputPath)
	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return &PageOpError{Code: ErrorCodeValidation, Message: "outputPath is required"}
	}
	if !isPDFPath(outputPath) {
		return &PageOpError{Code: ErrorCodeValidation, Message: "outputPath must use .pdf extension"}
	}
	if fsx.NormalizePathKey(inputPath) == fsx.NormalizePathKey(outputPath) {
		return &PageOpError{Code: ErrorCodeOutputCollidesInput, Message: fmt.Sprintf("outputPath collides with input path: %s", inputPath)}
	}

	if _, statErr := os.Stat(outputPath); statErr == nil {
		return &PageOpError{Code: ErrorCodeOutputAlreadyExists, Message: fmt.Sprintf("output already exists: %s", outputPath)}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return &PageOpError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: statErr}
	}

	return pageOpProbeOutputDir(outputPath)
}

func verifyPageOpPageCount(ctx context.Context, outputPath string, want int, code string) *PageOpError {
	got, err := api.PageCountFile(pdfCPUContext(ctx), outputPath)
	if err != nil {
		return &PageOpError{Code: code, Message: fmt.Sprintf("the output PDF could not be read back: %s", filepath.Base(outputPath)), Cause: err}
	}
	if got != want {
		return &PageOpError{
			Code:    code,
			Message: fmt.Sprintf("the output has %d page(s); expected %d", got, want),
			Details: map[string]any{"expected": want, "actual": got},
		}
	}

	return nil
}

// resolvePageSelection parses a selection and returns pdfcpu-ready tokens plus
// the resolved ranges. An empty selection returns nil tokens, meaning "all
// pages" for the operations that allow it. A positive pageCount rejects ranges
// beyond the document; a zero pageCount means the count is unknown and the
// bounds check is deferred to execution.
func resolvePageSelection(pageSelection string, pageCount int, invalidCode string) ([]string, []SplitPageRange, *PageOpError) {
	trimmed := strings.TrimSpace(pageSelection)
	if trimmed == "" {
		return nil, nil, nil
	}

	ranges, rangeErr := ParseSplitRanges(trimmed)
	if rangeErr != nil {
		return nil, nil, &PageOpError{Code: invalidCode, Message: rangeErr.Message, Cause: rangeErr.Cause}
	}

	if pageCount > 0 {
		boundsCode := pageOpSelectionBoundsCode(invalidCode)
		for _, r := range ranges {
			if r.End > pageCount {
				return nil, nil, &PageOpError{
					Code:    boundsCode,
					Message: fmt.Sprintf("page selection %q exceeds the PDF page count %d", trimmed, pageCount),
					Details: map[string]any{"selection": trimmed, "pageCount": pageCount},
				}
			}
		}
	}

	tokens := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r.Start == r.End {
			tokens = append(tokens, strconv.Itoa(r.Start))
			continue
		}
		tokens = append(tokens, fmt.Sprintf("%d-%d", r.Start, r.End))
	}

	return tokens, ranges, nil
}

// pageOpSelectionBoundsCode maps a tool's selection-syntax code to the matching
// out-of-bounds code.
func pageOpSelectionBoundsCode(invalidCode string) string {
	switch invalidCode {
	case ErrorCodeRotatePageSelectionBad:
		return ErrorCodeRotatePageSelectionBounds
	case ErrorCodeRemovePagesSelectionBad:
		return ErrorCodeRemovePagesSelectionBounds
	case ErrorCodeExtractImagesSelectionBad:
		return ErrorCodeExtractImagesSelectionBounds
	default:
		return invalidCode
	}
}

func pageSelectionCount(ranges []SplitPageRange) int {
	total := 0
	for _, r := range ranges {
		total += r.End - r.Start + 1
	}

	return total
}

func isSupportedRotateAngle(angle int) bool {
	switch angle {
	case RotateAngleCW90, RotateAngleCW180, RotateAngleCW270:
		return true
	default:
		return false
	}
}

func formatPageList(pages []int) string {
	parts := make([]string, 0, len(pages))
	for _, page := range pages {
		parts = append(parts, strconv.Itoa(page))
	}

	return strings.Join(parts, ", ")
}

func pluralizePageCount(count int) string {
	if count == 1 {
		return "1 page"
	}

	return fmt.Sprintf("%d pages", count)
}
