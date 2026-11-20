package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"fileforge-desktop/internal/platform/fsx"
	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
)

const (
	SplitStrategyEveryPage    = "every_page"
	SplitStrategyRanges       = "ranges"
	SplitStrategyPagesPerFile = "pages_per_file"
	SplitStrategyBookmarks    = "bookmarks"

	SplitMinPagesPerFile = 1
	SplitMaxPagesPerFile = 1000

	ErrorCodeSplitFailed                = "PDF_SPLIT_FAILED"
	ErrorCodeUnsupportedStrategy        = "PDF_SPLIT_STRATEGY_UNSUPPORTED"
	ErrorCodeSplitRangesRequired        = "PDF_SPLIT_RANGES_REQUIRED"
	ErrorCodeSplitRangesInvalid         = "PDF_SPLIT_RANGES_INVALID"
	ErrorCodeSplitRangeOutBounds        = "PDF_SPLIT_RANGE_OUT_OF_BOUNDS"
	ErrorCodeSplitOutputCollision       = "PDF_SPLIT_OUTPUT_COLLISION"
	ErrorCodeSplitOutputExists          = "PDF_SPLIT_OUTPUT_ALREADY_EXISTS"
	ErrorCodeSplitBatchOutputCollision  = "PDF_SPLIT_BATCH_OUTPUT_COLLISION"
	ErrorCodeSplitBatchInputDirConflict = "PDF_SPLIT_BATCH_INPUT_DIR_CONFLICT"
	ErrorCodeSplitPagesPerFileInvalid   = "PDF_SPLIT_PAGES_PER_FILE_INVALID"
	ErrorCodeSplitNoBookmarks           = "PDF_SPLIT_NO_BOOKMARKS"
	ErrorCodeSplitBookmarksInvalid      = "PDF_SPLIT_BOOKMARKS_INVALID"
)

type SplitPageRange struct {
	Start int
	End   int
}

// SplitBookmarkRange is one top-level bookmark resolved to a page span. End is
// inclusive and may be derived from the next bookmark or the last page.
type SplitBookmarkRange struct {
	Title string
	Start int
	End   int
}

type SplitError struct {
	Code    string
	Message string
	Cause   error
}

type splitPlannedOutput struct {
	Selection string
	Output    string
}

type SplitBatchResult struct {
	InputPath string
	OutputDir string
	Outputs   []string
	Error     *SplitError
}

type splitBatchItemPlan struct {
	InputPath string
	OutputDir string
	Planned   []splitPlannedOutput
	Error     *SplitError
}

type splitBatchInput struct {
	InputPath string
	OutputDir string
}

func (e *SplitError) Error() string {
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *SplitError) Unwrap() error {
	return e.Cause
}

// Split validates the request and splits inputPath into outputDir. The output
// directory is created at execution time when its parent is writable, so the
// planner can show a destination before anything exists.
func Split(ctx context.Context, inputPath, outputDir, strategy, rangesExpr string) ([]string, error) {
	return SplitAdvanced(ctx, inputPath, outputDir, strategy, rangesExpr, 0)
}

// SplitAdvanced is Split with the options needed by the pages_per_file
// strategy. pagesPerFile is ignored by every other strategy.
func SplitAdvanced(ctx context.Context, inputPath, outputDir, strategy, rangesExpr string, pagesPerFile int) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, &SplitError{Code: "CANCELED", Message: "split canceled", Cause: err}
	}

	planned, validationErr := buildSplitPlanWithAdvancedOptions(inputPath, outputDir, strategy, rangesExpr, pagesPerFile, true)
	if validationErr != nil {
		return nil, validationErr
	}

	return executeSplitPlan(ctx, inputPath, planned)
}

func ValidateSplitRequest(inputPath, outputDir, strategy, rangesExpr string) *SplitError {
	return ValidateSplitRequestAdvanced(inputPath, outputDir, strategy, rangesExpr, 0)
}

// ValidateSplitRequestAdvanced validates a request including pagesPerFile.
func ValidateSplitRequestAdvanced(inputPath, outputDir, strategy, rangesExpr string, pagesPerFile int) *SplitError {
	_, err := buildSplitPlanWithAdvancedOptions(inputPath, outputDir, strategy, rangesExpr, pagesPerFile, true)
	return err
}

// ValidateSplitInput checks that inputPath is an existing .pdf file without
// parsing the document. Planning uses it to stay cheap, so one unreadable PDF
// cannot make an entire batch plan fail before its per-item execution.
func ValidateSplitInput(inputPath string) *SplitError {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return &SplitError{Code: ErrorCodeValidation, Message: "inputPath is required"}
	}

	if !isPDFPath(inputPath) {
		return &SplitError{Code: ErrorCodeValidation, Message: fmt.Sprintf("input file must be .pdf: %s", inputPath)}
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &SplitError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input PDF was not found: %s", filepath.Base(inputPath)), Cause: err}
		}
		return &SplitError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input PDF is not accessible: %s", filepath.Base(inputPath)), Cause: err}
	}

	if info.IsDir() {
		return &SplitError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input path is a directory: %s", filepath.Base(inputPath))}
	}

	return nil
}

// ValidateSplitDestination validates the folder that will contain the planned
// split directory. The planned directory itself must not exist yet.
func ValidateSplitDestination(dir string) *SplitError {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return &SplitError{Code: ErrorCodeValidation, Message: "output directory is required"}
	}

	return validateOutputDirectory(dir, false)
}

// SplitPageCount returns the number of pages of a PDF, or 0 when the document
// cannot be read. Planning uses 0 to mean "unknown count".
func SplitPageCount(inputPath string) int {
	count, err := api.PageCountFile(context.Background(), strings.TrimSpace(inputPath))
	if err != nil {
		return 0
	}

	return count
}

// SplitPagesPerFileChunkCount returns the number of outputs a pages_per_file
// split produces for pageCount pages. It returns 0 when either input is
// unusable, which planning reports as an unknown count.
func SplitPagesPerFileChunkCount(pageCount, pagesPerFile int) int {
	if pageCount <= 0 || pagesPerFile <= 0 {
		return 0
	}

	return (pageCount + pagesPerFile - 1) / pagesPerFile
}

// SplitBookmarkPageRanges resolves the top-level bookmarks of inputPath to
// inclusive page ranges. It returns PDF_SPLIT_NO_BOOKMARKS when the document
// has no outline.
func SplitBookmarkPageRanges(inputPath string) ([]SplitBookmarkRange, *SplitError) {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return nil, &SplitError{Code: ErrorCodeValidation, Message: "inputPath is required"}
	}

	if !isPDFPath(inputPath) {
		return nil, &SplitError{Code: ErrorCodeValidation, Message: fmt.Sprintf("input file must be .pdf: %s", inputPath)}
	}

	pageCount, pageCountErr := api.PageCountFile(context.Background(), inputPath)
	if pageCountErr != nil {
		return nil, &SplitError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("unable to determine page count for: %s", filepath.Base(inputPath)), Cause: pageCountErr}
	}

	file, openErr := os.Open(inputPath)
	if openErr != nil {
		return nil, &SplitError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input PDF is not accessible: %s", filepath.Base(inputPath)), Cause: openErr}
	}
	defer file.Close()

	bookmarks, bookmarksErr := api.Bookmarks(context.Background(), file, newPDFCPUConfig())
	if bookmarksErr != nil {
		return nil, &SplitError{Code: ErrorCodeSplitFailed, Message: fmt.Sprintf("failed to read PDF bookmarks: %s", filepath.Base(inputPath)), Cause: bookmarksErr}
	}

	return deriveBookmarkRanges(bookmarks, pageCount)
}

// deriveBookmarkRanges converts top-level bookmarks to page spans. A bookmark
// without PageThru ends before the next usable bookmark, or on the last page.
// Bookmarks whose page is unusable or whose span is empty are skipped; a
// document left without any usable span reports PDF_SPLIT_NO_BOOKMARKS.
func deriveBookmarkRanges(bookmarks []pdfcpu.Bookmark, pageCount int) ([]SplitBookmarkRange, *SplitError) {
	if len(bookmarks) == 0 {
		return nil, &SplitError{Code: ErrorCodeSplitNoBookmarks, Message: "PDF has no bookmarks to split by"}
	}

	ranges := make([]SplitBookmarkRange, 0, len(bookmarks))
	for i, bookmark := range bookmarks {
		start := bookmark.PageFrom
		if start <= 0 {
			continue
		}
		if start > pageCount {
			return nil, &SplitError{
				Code:    ErrorCodeSplitBookmarksInvalid,
				Message: fmt.Sprintf("bookmark %q starts on page %d beyond the PDF page count %d", bookmark.Title, start, pageCount),
			}
		}

		end := bookmark.PageThru
		if end < start {
			end = bookmarkRangeEnd(bookmarks[i+1:], start, pageCount)
		}
		if end > pageCount {
			return nil, &SplitError{
				Code:    ErrorCodeSplitBookmarksInvalid,
				Message: fmt.Sprintf("bookmark %q ends on page %d beyond the PDF page count %d", bookmark.Title, end, pageCount),
			}
		}
		if end < start {
			continue
		}

		ranges = append(ranges, SplitBookmarkRange{Title: bookmark.Title, Start: start, End: end})
	}

	if len(ranges) == 0 {
		return nil, &SplitError{Code: ErrorCodeSplitNoBookmarks, Message: "PDF bookmarks do not define any usable page ranges"}
	}

	return ranges, nil
}

// bookmarkRangeEnd derives the last page of a bookmark from the next usable
// top-level bookmark, falling back to the last page of the document.
func bookmarkRangeEnd(remaining []pdfcpu.Bookmark, start, pageCount int) int {
	for _, next := range remaining {
		if next.PageFrom >= start {
			return next.PageFrom - 1
		}
	}

	return pageCount
}

func ValidateSplitBatchRequest(inputPaths []string, outputDir, strategy, rangesExpr string, perInputDir bool) *SplitError {
	return ValidateSplitBatchRequestAdvanced(inputPaths, outputDir, strategy, rangesExpr, 0, perInputDir)
}

func ValidateSplitBatchRequestAdvanced(inputPaths []string, outputDir, strategy, rangesExpr string, pagesPerFile int, perInputDir bool) *SplitError {
	return validateSplitBatchShape(inputPaths, outputDir, strategy, rangesExpr, pagesPerFile, perInputDir)
}

func SplitBatch(ctx context.Context, inputPaths []string, outputDir, strategy, rangesExpr string, perInputDir bool) ([]SplitBatchResult, error) {
	return SplitBatchAdvanced(ctx, inputPaths, outputDir, strategy, rangesExpr, 0, perInputDir)
}

// SplitBatchAdvanced is SplitBatch with the pages_per_file option.
func SplitBatchAdvanced(ctx context.Context, inputPaths []string, outputDir, strategy, rangesExpr string, pagesPerFile int, perInputDir bool) ([]SplitBatchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, &SplitError{Code: "CANCELED", Message: "split canceled", Cause: err}
	}

	plans, validationErr := planSplitBatch(inputPaths, outputDir, strategy, rangesExpr, pagesPerFile, perInputDir)
	if validationErr != nil {
		return nil, validationErr
	}

	results := make([]SplitBatchResult, 0, len(plans))
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return results, &SplitError{Code: "CANCELED", Message: "split canceled", Cause: err}
		}

		if plan.Error != nil {
			results = append(results, SplitBatchResult{
				InputPath: plan.InputPath,
				OutputDir: plan.OutputDir,
				Error:     plan.Error,
			})
			continue
		}

		outputs, err := executeSplitPlan(ctx, plan.InputPath, plan.Planned)
		if err != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return results, &SplitError{Code: "CANCELED", Message: "split canceled", Cause: ctxErr}
			}

			var splitErr *SplitError
			if !errors.As(err, &splitErr) {
				splitErr = &SplitError{Code: ErrorCodeSplitFailed, Message: "failed to split PDF", Cause: err}
			}
			if splitErr.Code == "CANCELED" {
				return results, splitErr
			}

			results = append(results, SplitBatchResult{
				InputPath: plan.InputPath,
				OutputDir: plan.OutputDir,
				Error:     splitErr,
			})
			continue
		}

		results = append(results, SplitBatchResult{
			InputPath: plan.InputPath,
			OutputDir: plan.OutputDir,
			Outputs:   append([]string(nil), outputs...),
		})
	}

	return results, nil
}

func buildSplitPlanWithOptions(inputPath, outputDir, strategy, rangesExpr string, allowCreateOutputDir bool) ([]splitPlannedOutput, *SplitError) {
	return buildSplitPlanWithAdvancedOptions(inputPath, outputDir, strategy, rangesExpr, 0, allowCreateOutputDir)
}

// buildSplitPlanWithAdvancedOptions resolves every output of a split without
// writing anything. Bookmark and pages_per_file plans read the PDF because
// their output names depend on the document structure.
func buildSplitPlanWithAdvancedOptions(inputPath, outputDir, strategy, rangesExpr string, pagesPerFile int, allowCreateOutputDir bool) ([]splitPlannedOutput, *SplitError) {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return nil, &SplitError{Code: ErrorCodeValidation, Message: "inputPath is required"}
	}

	if !isPDFPath(inputPath) {
		return nil, &SplitError{Code: ErrorCodeValidation, Message: fmt.Sprintf("input file must be .pdf: %s", inputPath)}
	}

	outputDir = strings.TrimSpace(outputDir)
	if outputDir == "" {
		return nil, &SplitError{Code: ErrorCodeValidation, Message: "outputDir is required"}
	}

	if !isSupportedSplitStrategy(strategy) {
		return nil, &SplitError{Code: ErrorCodeUnsupportedStrategy, Message: fmt.Sprintf("unsupported split strategy: %s", strategy)}
	}

	if strategy == SplitStrategyPagesPerFile {
		if pagesErr := validatePagesPerFile(pagesPerFile); pagesErr != nil {
			return nil, pagesErr
		}
	}

	if validationErr := validateOutputDirectory(outputDir, allowCreateOutputDir); validationErr != nil {
		return nil, validationErr
	}

	if err := api.ValidateFile(context.Background(), inputPath, newPDFCPUConfig(), nil); err != nil {
		return nil, &SplitError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("invalid PDF input: %s", filepath.Base(inputPath)), Cause: err}
	}

	pageCount, pageCountErr := api.PageCountFile(context.Background(), inputPath)
	if pageCountErr != nil {
		return nil, &SplitError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("unable to determine page count for: %s", filepath.Base(inputPath)), Cause: pageCountErr}
	}

	planned := make([]splitPlannedOutput, 0)
	switch strategy {
	case SplitStrategyEveryPage:
		planned = buildEveryPagePlan(inputPath, outputDir, pageCount)
	case SplitStrategyRanges:
		ranges, rangesErr := resolveRangesExpr(rangesExpr)
		if rangesErr != nil {
			return nil, rangesErr
		}

		if boundsErr := validateRangesWithinPageCount(ranges, pageCount); boundsErr != nil {
			return nil, boundsErr
		}

		planned = buildRangesPlan(inputPath, outputDir, ranges)
	case SplitStrategyPagesPerFile:
		planned = buildPagesPerFilePlan(inputPath, outputDir, pageCount, pagesPerFile)
	case SplitStrategyBookmarks:
		bookmarkRanges, bookmarksErr := SplitBookmarkPageRanges(inputPath)
		if bookmarksErr != nil {
			return nil, bookmarksErr
		}

		planned = buildBookmarksPlan(inputPath, outputDir, bookmarkRanges)
	}

	if err := ensureNoOutputCollisions(planned); err != nil {
		return nil, err
	}

	if err := ensureOutputsDoNotExist(planned); err != nil {
		return nil, err
	}

	return planned, nil
}

func isSupportedSplitStrategy(strategy string) bool {
	switch strategy {
	case SplitStrategyEveryPage, SplitStrategyRanges, SplitStrategyPagesPerFile, SplitStrategyBookmarks:
		return true
	default:
		return false
	}
}

func validatePagesPerFile(pagesPerFile int) *SplitError {
	if pagesPerFile < SplitMinPagesPerFile || pagesPerFile > SplitMaxPagesPerFile {
		return &SplitError{
			Code:    ErrorCodeSplitPagesPerFileInvalid,
			Message: fmt.Sprintf("pagesPerFile must be between %d and %d", SplitMinPagesPerFile, SplitMaxPagesPerFile),
		}
	}

	return nil
}

func resolveRangesExpr(rangesExpr string) ([]SplitPageRange, *SplitError) {
	rangesExpr = strings.TrimSpace(rangesExpr)
	if rangesExpr == "" {
		return nil, &SplitError{Code: ErrorCodeSplitRangesRequired, Message: "options.ranges is required for strategy=ranges"}
	}

	return ParseSplitRanges(rangesExpr)
}

func validateSplitBatchShape(inputPaths []string, outputDir, strategy, rangesExpr string, pagesPerFile int, perInputDir bool) *SplitError {
	outputDir = strings.TrimSpace(outputDir)
	if outputDir == "" {
		return &SplitError{Code: ErrorCodeValidation, Message: "outputDir is required"}
	}

	if len(inputPaths) < 1 {
		return &SplitError{Code: ErrorCodeValidation, Message: "at least 1 input PDF is required"}
	}

	if validationErr := validateOutputDirectory(outputDir, false); validationErr != nil {
		return validationErr
	}

	if !isSupportedSplitStrategy(strategy) {
		return &SplitError{Code: ErrorCodeUnsupportedStrategy, Message: fmt.Sprintf("unsupported split strategy: %s", strategy)}
	}

	if strategy == SplitStrategyPagesPerFile {
		if pagesErr := validatePagesPerFile(pagesPerFile); pagesErr != nil {
			return pagesErr
		}
	}

	if strategy == SplitStrategyRanges {
		if _, rangesErr := resolveRangesExpr(rangesExpr); rangesErr != nil {
			return rangesErr
		}
	}

	_, conflictsErr := resolveSplitBatchInputs(inputPaths, outputDir, perInputDir)
	return conflictsErr
}

func resolveSplitBatchInputs(inputPaths []string, outputDir string, perInputDir bool) ([]splitBatchInput, *SplitError) {
	inputs := make([]splitBatchInput, 0, len(inputPaths))
	seenInputDirs := make(map[string]string, len(inputPaths))

	for _, rawInputPath := range inputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return nil, &SplitError{Code: ErrorCodeValidation, Message: "inputPath is required"}
		}

		effectiveOutputDir := outputDir
		if perInputDir {
			effectiveOutputDir = filepath.Join(outputDir, perInputSplitDirName(inputPath))

			dirKey := fsx.NormalizePathKey(effectiveOutputDir)
			if previous, exists := seenInputDirs[dirKey]; exists {
				return nil, &SplitError{Code: ErrorCodeSplitBatchInputDirConflict, Message: fmt.Sprintf("batch perInputDir conflict for %s and %s", previous, inputPath)}
			}
			seenInputDirs[dirKey] = inputPath
		}

		inputs = append(inputs, splitBatchInput{InputPath: inputPath, OutputDir: effectiveOutputDir})
	}

	return inputs, nil
}

func planSplitBatch(inputPaths []string, outputDir, strategy, rangesExpr string, pagesPerFile int, perInputDir bool) ([]splitBatchItemPlan, *SplitError) {
	if validationErr := validateSplitBatchShape(inputPaths, outputDir, strategy, rangesExpr, pagesPerFile, perInputDir); validationErr != nil {
		return nil, validationErr
	}

	inputs, conflictsErr := resolveSplitBatchInputs(inputPaths, strings.TrimSpace(outputDir), perInputDir)
	if conflictsErr != nil {
		return nil, conflictsErr
	}

	plans := make([]splitBatchItemPlan, 0, len(inputs))
	seenOutputs := make(map[string]string)

	for _, input := range inputs {
		allowCreateOutputDir := perInputDir
		planned, planErr := buildSplitPlanWithAdvancedOptions(input.InputPath, input.OutputDir, strategy, rangesExpr, pagesPerFile, allowCreateOutputDir)
		if planErr != nil {
			plans = append(plans, splitBatchItemPlan{
				InputPath: input.InputPath,
				OutputDir: input.OutputDir,
				Error:     planErr,
			})
			continue
		}

		for _, entry := range planned {
			outputKey := fsx.NormalizePathKey(entry.Output)
			if previous, exists := seenOutputs[outputKey]; exists {
				return nil, &SplitError{Code: ErrorCodeSplitBatchOutputCollision, Message: fmt.Sprintf("batch planned outputs collide: %s and %s", previous, entry.Output)}
			}
			seenOutputs[outputKey] = entry.Output
		}

		plans = append(plans, splitBatchItemPlan{
			InputPath: input.InputPath,
			OutputDir: input.OutputDir,
			Planned:   planned,
		})
	}

	return plans, nil
}

func ParseSplitRanges(expr string) ([]SplitPageRange, *SplitError) {
	trimmed := strings.TrimSpace(expr)
	if trimmed == "" {
		return nil, &SplitError{Code: ErrorCodeSplitRangesRequired, Message: "options.ranges is required for strategy=ranges"}
	}

	tokens := strings.Split(trimmed, ",")
	ranges := make([]SplitPageRange, 0, len(tokens))

	for _, rawToken := range tokens {
		token := strings.TrimSpace(rawToken)
		if token == "" {
			return nil, &SplitError{Code: ErrorCodeSplitRangesInvalid, Message: "ranges contains empty token"}
		}

		if strings.Count(token, "-") == 0 {
			page, err := strconv.Atoi(token)
			if err != nil {
				return nil, &SplitError{Code: ErrorCodeSplitRangesInvalid, Message: fmt.Sprintf("invalid page token: %s", token), Cause: err}
			}
			if page <= 0 {
				return nil, &SplitError{Code: ErrorCodeSplitRangesInvalid, Message: fmt.Sprintf("page must be > 0: %d", page)}
			}

			ranges = append(ranges, SplitPageRange{Start: page, End: page})
			continue
		}

		parts := strings.Split(token, "-")
		if len(parts) != 2 {
			return nil, &SplitError{Code: ErrorCodeSplitRangesInvalid, Message: fmt.Sprintf("invalid range token: %s", token)}
		}

		startText := strings.TrimSpace(parts[0])
		endText := strings.TrimSpace(parts[1])
		if startText == "" || endText == "" {
			return nil, &SplitError{Code: ErrorCodeSplitRangesInvalid, Message: fmt.Sprintf("invalid range token: %s", token)}
		}

		start, err := strconv.Atoi(startText)
		if err != nil {
			return nil, &SplitError{Code: ErrorCodeSplitRangesInvalid, Message: fmt.Sprintf("invalid range start: %s", token), Cause: err}
		}
		end, err := strconv.Atoi(endText)
		if err != nil {
			return nil, &SplitError{Code: ErrorCodeSplitRangesInvalid, Message: fmt.Sprintf("invalid range end: %s", token), Cause: err}
		}

		if start <= 0 || end <= 0 {
			return nil, &SplitError{Code: ErrorCodeSplitRangesInvalid, Message: fmt.Sprintf("range must be > 0: %s", token)}
		}

		if start > end {
			return nil, &SplitError{Code: ErrorCodeSplitRangesInvalid, Message: fmt.Sprintf("range start must be <= end: %s", token)}
		}

		ranges = append(ranges, SplitPageRange{Start: start, End: end})
	}

	if overlapErr := validateRangeOverlaps(ranges); overlapErr != nil {
		return nil, overlapErr
	}

	return ranges, nil
}

func validateRangeOverlaps(ranges []SplitPageRange) *SplitError {
	if len(ranges) <= 1 {
		return nil
	}

	sorted := append([]SplitPageRange(nil), ranges...)
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Start == sorted[j].Start {
			return sorted[i].End < sorted[j].End
		}
		return sorted[i].Start < sorted[j].Start
	})

	prev := sorted[0]
	for i := 1; i < len(sorted); i++ {
		curr := sorted[i]
		if curr.Start <= prev.End {
			return &SplitError{Code: ErrorCodeSplitRangesInvalid, Message: fmt.Sprintf("ranges contain duplicate/overlapping pages: %d-%d overlaps %d-%d", prev.Start, prev.End, curr.Start, curr.End)}
		}
		prev = curr
	}

	return nil
}

func validateRangesWithinPageCount(ranges []SplitPageRange, pageCount int) *SplitError {
	for _, r := range ranges {
		if r.End > pageCount {
			return &SplitError{Code: ErrorCodeSplitRangeOutBounds, Message: fmt.Sprintf("range %d-%d exceeds PDF page count %d", r.Start, r.End, pageCount)}
		}
	}

	return nil
}

func executeSplitPlan(ctx context.Context, inputPath string, planned []splitPlannedOutput) ([]string, error) {
	// Read and validate the source once. api.TrimFile would re-parse the whole
	// document for every selection, which turns an every-page split into an
	// O(pages²) operation.
	source, openErr := os.Open(inputPath)
	if openErr != nil {
		return nil, &SplitError{Code: ErrorCodeSplitFailed, Message: fmt.Sprintf("failed to open the PDF: %s", filepath.Base(inputPath)), Cause: openErr}
	}
	defer source.Close()

	conf := newPDFCPUConfig()
	conf.Cmd = model.TRIM
	sourceCtx, readErr := api.ReadValidateAndOptimize(pdfCPUContext(ctx), source, conf, nil)
	if readErr != nil {
		return nil, &SplitError{Code: ErrorCodeSplitFailed, Message: fmt.Sprintf("failed to read the PDF: %s", filepath.Base(inputPath)), Cause: readErr}
	}

	outputs := make([]string, 0, len(planned))
	for _, entry := range planned {
		if err := ctx.Err(); err != nil {
			return nil, &SplitError{Code: "CANCELED", Message: "split canceled", Cause: err}
		}

		if err := os.MkdirAll(filepath.Dir(entry.Output), 0o755); err != nil {
			return nil, &SplitError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", filepath.Dir(entry.Output)), Cause: err}
		}

		selected, selectionErr := api.PagesForPageSelection(sourceCtx.PageCount, []string{entry.Selection}, false, true)
		if selectionErr != nil {
			return nil, &SplitError{Code: ErrorCodeSplitFailed, Message: fmt.Sprintf("failed to resolve selection %s", entry.Selection), Cause: selectionErr}
		}
		pageNumbers := make([]int, 0, len(selected))
		for page := range selected {
			pageNumbers = append(pageNumbers, page)
		}
		sort.Ints(pageNumbers)

		destCtx, extractErr := pdfcpu.ExtractPages(pdfCPUContext(ctx), sourceCtx, pageNumbers, false)
		if extractErr != nil {
			return nil, &SplitError{Code: ErrorCodeSplitFailed, Message: fmt.Sprintf("failed to split PDF for selection %s", entry.Selection), Cause: extractErr}
		}
		if writeErr := api.WriteContextFile(pdfCPUContext(ctx), destCtx, entry.Output); writeErr != nil {
			return nil, &SplitError{Code: ErrorCodeSplitFailed, Message: fmt.Sprintf("failed to split PDF for selection %s", entry.Selection), Cause: writeErr}
		}

		outputs = append(outputs, entry.Output)
	}

	return outputs, nil
}

func buildEveryPagePlan(inputPath, outputDir string, pageCount int) []splitPlannedOutput {
	planned := make([]splitPlannedOutput, 0, pageCount)
	for page := 1; page <= pageCount; page++ {
		selection := strconv.Itoa(page)
		outputName := buildEveryPageOutputName(inputPath, page)
		planned = append(planned, splitPlannedOutput{
			Selection: selection,
			Output:    filepath.Join(outputDir, outputName),
		})
	}

	return planned
}

func buildRangesPlan(inputPath, outputDir string, ranges []SplitPageRange) []splitPlannedOutput {
	planned := make([]splitPlannedOutput, 0, len(ranges))
	for i, r := range ranges {
		selection := fmt.Sprintf("%d-%d", r.Start, r.End)
		if r.Start == r.End {
			selection = strconv.Itoa(r.Start)
		}

		planned = append(planned, splitPlannedOutput{
			Selection: selection,
			Output:    filepath.Join(outputDir, buildRangeOutputName(inputPath, i+1, r)),
		})
	}

	return planned
}

func buildPagesPerFilePlan(inputPath, outputDir string, pageCount, pagesPerFile int) []splitPlannedOutput {
	planned := make([]splitPlannedOutput, 0, SplitPagesPerFileChunkCount(pageCount, pagesPerFile))
	part := 1
	for start := 1; start <= pageCount; start += pagesPerFile {
		end := start + pagesPerFile - 1
		if end > pageCount {
			end = pageCount
		}

		selection := fmt.Sprintf("%d-%d", start, end)
		if start == end {
			selection = strconv.Itoa(start)
		}

		planned = append(planned, splitPlannedOutput{
			Selection: selection,
			Output:    filepath.Join(outputDir, buildPagesPerFileOutputName(inputPath, part, start, end)),
		})
		part++
	}

	return planned
}

func buildBookmarksPlan(inputPath, outputDir string, ranges []SplitBookmarkRange) []splitPlannedOutput {
	planned := make([]splitPlannedOutput, 0, len(ranges))
	for i, r := range ranges {
		selection := fmt.Sprintf("%d-%d", r.Start, r.End)
		if r.Start == r.End {
			selection = strconv.Itoa(r.Start)
		}

		planned = append(planned, splitPlannedOutput{
			Selection: selection,
			Output:    filepath.Join(outputDir, buildBookmarkOutputName(inputPath, i+1, r.Title)),
		})
	}

	return planned
}

func buildEveryPageOutputName(inputPath string, page int) string {
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	return fmt.Sprintf("%s_page_%03d.pdf", base, page)
}

func buildRangeOutputName(inputPath string, index int, r SplitPageRange) string {
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if r.Start == r.End {
		return fmt.Sprintf("%s_range_%03d_p%d.pdf", base, index, r.Start)
	}

	return fmt.Sprintf("%s_range_%03d_p%d-%d.pdf", base, index, r.Start, r.End)
}

func buildPagesPerFileOutputName(inputPath string, part, start, end int) string {
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	if start == end {
		return fmt.Sprintf("%s_part_%03d_p%d.pdf", base, part, start)
	}

	return fmt.Sprintf("%s_part_%03d_p%d-%d.pdf", base, part, start, end)
}

func buildBookmarkOutputName(inputPath string, index int, title string) string {
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	return fmt.Sprintf("%s_bookmark_%03d_%s.pdf", base, index, bookmarkSlug(title))
}

const bookmarkSlugMaxLength = 40

// bookmarkSlug turns a bookmark title into a filesystem-friendly token made of
// letters, digits, '-' and '_'. Empty or fully filtered titles become "part".
func bookmarkSlug(title string) string {
	var builder strings.Builder
	previousSeparator := true

	for _, r := range strings.ToLower(strings.TrimSpace(title)) {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			builder.WriteRune(r)
			previousSeparator = false
			continue
		}

		if r == '-' || r == '_' {
			if !previousSeparator {
				builder.WriteRune(r)
				previousSeparator = true
			}
			continue
		}

		if !previousSeparator {
			builder.WriteRune('-')
			previousSeparator = true
		}
	}

	slug := strings.Trim(builder.String(), "-_")
	if runes := []rune(slug); len(runes) > bookmarkSlugMaxLength {
		slug = strings.TrimRight(string(runes[:bookmarkSlugMaxLength]), "-_")
	}

	if slug == "" {
		return "part"
	}

	return slug
}

func IsSplitErrorCode(err error, code string) bool {
	var splitErr *SplitError
	if !errors.As(err, &splitErr) {
		return false
	}

	return splitErr.Code == code
}

func ensureNoOutputCollisions(planned []splitPlannedOutput) *SplitError {
	seen := make(map[string]string, len(planned))
	for _, entry := range planned {
		key := fsx.NormalizePathKey(entry.Output)
		if previous, exists := seen[key]; exists {
			return &SplitError{Code: ErrorCodeSplitOutputCollision, Message: fmt.Sprintf("planned outputs collide: %s and %s", previous, entry.Output)}
		}
		seen[key] = entry.Output
	}

	return nil
}

func ensureOutputsDoNotExist(planned []splitPlannedOutput) *SplitError {
	for _, entry := range planned {
		_, err := os.Stat(entry.Output)
		if err == nil {
			return &SplitError{Code: ErrorCodeSplitOutputExists, Message: fmt.Sprintf("output already exists: %s", entry.Output)}
		}

		if errors.Is(err, os.ErrNotExist) {
			continue
		}

		return &SplitError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not accessible: %s", filepath.Dir(entry.Output)), Cause: err}
	}

	return nil
}

func validateOutputDirectory(outputDir string, allowCreate bool) *SplitError {
	info, err := os.Stat(outputDir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			if !allowCreate {
				return &SplitError{Code: ErrorCodeOutputDirNotFound, Message: fmt.Sprintf("output directory does not exist: %s", outputDir), Cause: err}
			}

			parentDir := filepath.Dir(outputDir)
			parentInfo, parentErr := os.Stat(parentDir)
			if parentErr != nil {
				if errors.Is(parentErr, os.ErrNotExist) {
					return &SplitError{Code: ErrorCodeOutputDirNotFound, Message: fmt.Sprintf("output directory parent does not exist: %s", parentDir), Cause: parentErr}
				}
				return &SplitError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory parent is not accessible: %s", parentDir), Cause: parentErr}
			}
			if !parentInfo.IsDir() {
				return &SplitError{Code: ErrorCodeOutputDirNotDirectory, Message: fmt.Sprintf("output directory parent is not a directory: %s", parentDir)}
			}

			if writeErr := validateDirectoryWritable(parentDir, ".fileforge-split-parent-writecheck-*.tmp"); writeErr != nil {
				return writeErr
			}

			return nil
		}
		return &SplitError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not accessible: %s", outputDir), Cause: err}
	}

	if !info.IsDir() {
		return &SplitError{Code: ErrorCodeOutputDirNotDirectory, Message: fmt.Sprintf("output directory is not a directory: %s", outputDir)}
	}

	if writeErr := validateDirectoryWritable(outputDir, ".fileforge-split-writecheck-*.tmp"); writeErr != nil {
		return writeErr
	}

	return nil
}

func validateDirectoryWritable(dir string, pattern string) *SplitError {
	tmp, err := os.CreateTemp(dir, pattern)
	if err != nil {
		return &SplitError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: err}
	}
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmp.Name())
		return &SplitError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: closeErr}
	}
	if removeErr := os.Remove(tmp.Name()); removeErr != nil {
		return &SplitError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: removeErr}
	}

	return nil
}

func perInputSplitDirName(inputPath string) string {
	base := strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))
	trimmed := strings.TrimSpace(base)
	if trimmed == "" {
		return "input"
	}

	return trimmed
}
