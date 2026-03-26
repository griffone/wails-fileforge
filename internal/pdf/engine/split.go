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

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const (
	SplitStrategyEveryPage              = "every_page"
	SplitStrategyRanges                 = "ranges"
	ErrorCodeSplitFailed                = "PDF_SPLIT_FAILED"
	ErrorCodeUnsupportedStrategy        = "PDF_SPLIT_STRATEGY_UNSUPPORTED"
	ErrorCodeSplitRangesRequired        = "PDF_SPLIT_RANGES_REQUIRED"
	ErrorCodeSplitRangesInvalid         = "PDF_SPLIT_RANGES_INVALID"
	ErrorCodeSplitRangeOutBounds        = "PDF_SPLIT_RANGE_OUT_OF_BOUNDS"
	ErrorCodeSplitOutputCollision       = "PDF_SPLIT_OUTPUT_COLLISION"
	ErrorCodeSplitOutputExists          = "PDF_SPLIT_OUTPUT_ALREADY_EXISTS"
	ErrorCodeSplitBatchOutputCollision  = "PDF_SPLIT_BATCH_OUTPUT_COLLISION"
	ErrorCodeSplitBatchInputDirConflict = "PDF_SPLIT_BATCH_INPUT_DIR_CONFLICT"
)

type SplitPageRange struct {
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
}

type splitBatchItemPlan struct {
	InputPath string
	OutputDir string
	Planned   []splitPlannedOutput
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

func Split(ctx context.Context, inputPath, outputDir, strategy, rangesExpr string) ([]string, error) {
	if err := ctx.Err(); err != nil {
		return nil, &SplitError{Code: "CANCELED", Message: "split canceled", Cause: err}
	}

	planned, validationErr := buildSplitPlan(inputPath, outputDir, strategy, rangesExpr)
	if validationErr != nil {
		return nil, validationErr
	}

	return executeSplitPlan(ctx, inputPath, planned)
}

func ValidateSplitRequest(inputPath, outputDir, strategy, rangesExpr string) *SplitError {
	_, err := buildSplitPlan(inputPath, outputDir, strategy, rangesExpr)
	return err
}

func ValidateSplitBatchRequest(inputPaths []string, outputDir, strategy, rangesExpr string, perInputDir bool) *SplitError {
	_, err := buildSplitBatchPlan(inputPaths, outputDir, strategy, rangesExpr, perInputDir)
	return err
}

func SplitBatch(ctx context.Context, inputPaths []string, outputDir, strategy, rangesExpr string, perInputDir bool) ([]SplitBatchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, &SplitError{Code: "CANCELED", Message: "split canceled", Cause: err}
	}

	plans, validationErr := buildSplitBatchPlan(inputPaths, outputDir, strategy, rangesExpr, perInputDir)
	if validationErr != nil {
		return nil, validationErr
	}

	results := make([]SplitBatchResult, 0, len(plans))
	for _, plan := range plans {
		outputs, err := executeSplitPlan(ctx, plan.InputPath, plan.Planned)
		if err != nil {
			return results, err
		}

		results = append(results, SplitBatchResult{
			InputPath: plan.InputPath,
			OutputDir: plan.OutputDir,
			Outputs:   append([]string(nil), outputs...),
		})
	}

	return results, nil
}

func buildSplitPlan(inputPath, outputDir, strategy, rangesExpr string) ([]splitPlannedOutput, *SplitError) {
	return buildSplitPlanWithOptions(inputPath, outputDir, strategy, rangesExpr, false)
}

func buildSplitPlanWithOptions(inputPath, outputDir, strategy, rangesExpr string, allowCreateOutputDir bool) ([]splitPlannedOutput, *SplitError) {
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

	if strategy != SplitStrategyEveryPage && strategy != SplitStrategyRanges {
		return nil, &SplitError{Code: ErrorCodeUnsupportedStrategy, Message: fmt.Sprintf("unsupported split strategy: %s", strategy)}
	}

	if validationErr := validateOutputDirectory(outputDir, allowCreateOutputDir); validationErr != nil {
		return nil, validationErr
	}

	if err := api.ValidateFile(inputPath, nil); err != nil {
		return nil, &SplitError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("invalid PDF input: %s", filepath.Base(inputPath)), Cause: err}
	}

	pageCount, pageCountErr := api.PageCountFile(inputPath)
	if pageCountErr != nil {
		return nil, &SplitError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("unable to determine page count for: %s", filepath.Base(inputPath)), Cause: pageCountErr}
	}

	planned := make([]splitPlannedOutput, 0)
	if strategy == SplitStrategyEveryPage {
		planned = buildEveryPagePlan(inputPath, outputDir, pageCount)
	} else {
		rangesExpr = strings.TrimSpace(rangesExpr)
		if rangesExpr == "" {
			return nil, &SplitError{Code: ErrorCodeSplitRangesRequired, Message: "options.ranges is required for strategy=ranges"}
		}

		ranges, err := ParseSplitRanges(rangesExpr)
		if err != nil {
			return nil, err
		}

		if boundsErr := validateRangesWithinPageCount(ranges, pageCount); boundsErr != nil {
			return nil, boundsErr
		}

		planned = buildRangesPlan(inputPath, outputDir, ranges)
	}

	if err := ensureNoOutputCollisions(planned); err != nil {
		return nil, err
	}

	if err := ensureOutputsDoNotExist(planned); err != nil {
		return nil, err
	}

	return planned, nil
}

func buildSplitBatchPlan(inputPaths []string, outputDir, strategy, rangesExpr string, perInputDir bool) ([]splitBatchItemPlan, *SplitError) {
	outputDir = strings.TrimSpace(outputDir)
	if outputDir == "" {
		return nil, &SplitError{Code: ErrorCodeValidation, Message: "outputDir is required"}
	}

	if len(inputPaths) < 1 {
		return nil, &SplitError{Code: ErrorCodeValidation, Message: "at least 1 input PDF is required"}
	}

	if validationErr := validateOutputDirectory(outputDir, false); validationErr != nil {
		return nil, validationErr
	}

	plans := make([]splitBatchItemPlan, 0, len(inputPaths))
	seenInputDirs := make(map[string]string, len(inputPaths))
	seenOutputs := make(map[string]string)

	for _, rawInputPath := range inputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return nil, &SplitError{Code: ErrorCodeValidation, Message: "inputPath is required"}
		}

		effectiveOutputDir := outputDir
		allowCreateOutputDir := false
		if perInputDir {
			effectiveOutputDir = filepath.Join(outputDir, perInputSplitDirName(inputPath))
			allowCreateOutputDir = true

			dirKey := normalizePathKey(effectiveOutputDir)
			if previous, exists := seenInputDirs[dirKey]; exists {
				return nil, &SplitError{Code: ErrorCodeSplitBatchInputDirConflict, Message: fmt.Sprintf("batch perInputDir conflict for %s and %s", previous, inputPath)}
			}
			seenInputDirs[dirKey] = inputPath
		}

		planned, err := buildSplitPlanWithOptions(inputPath, effectiveOutputDir, strategy, rangesExpr, allowCreateOutputDir)
		if err != nil {
			return nil, err
		}

		for _, entry := range planned {
			outputKey := normalizePathKey(entry.Output)
			if previous, exists := seenOutputs[outputKey]; exists {
				return nil, &SplitError{Code: ErrorCodeSplitBatchOutputCollision, Message: fmt.Sprintf("batch planned outputs collide: %s and %s", previous, entry.Output)}
			}
			seenOutputs[outputKey] = entry.Output
		}

		plans = append(plans, splitBatchItemPlan{
			InputPath: inputPath,
			OutputDir: effectiveOutputDir,
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
	outputs := make([]string, 0, len(planned))
	for _, entry := range planned {
		if err := ctx.Err(); err != nil {
			return nil, &SplitError{Code: "CANCELED", Message: "split canceled", Cause: err}
		}

		if err := os.MkdirAll(filepath.Dir(entry.Output), 0o755); err != nil {
			return nil, &SplitError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", filepath.Dir(entry.Output)), Cause: err}
		}

		if err := api.TrimFile(inputPath, entry.Output, []string{entry.Selection}, nil); err != nil {
			return nil, &SplitError{Code: ErrorCodeSplitFailed, Message: fmt.Sprintf("failed to split PDF for selection %s", entry.Selection), Cause: err}
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
		key := normalizePathKey(entry.Output)
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
