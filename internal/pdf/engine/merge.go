package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"fileforge-desktop/internal/platform/fsx"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const (
	ErrorCodeValidation            = "VALIDATION_ERROR"
	ErrorCodeInvalidInputPDF       = "PDF_INVALID_INPUT"
	ErrorCodeInvalidInputsPDF      = "PDF_INVALID_INPUTS"
	ErrorCodeProtectedInputPDF     = "PDF_PROTECTED_INPUT"
	ErrorCodeMergeFailed           = "PDF_MERGE_FAILED"
	ErrorCodeMergePageRangeInvalid = "PDF_MERGE_PAGE_RANGE_INVALID"
	ErrorCodeMergeOutputExists     = "PDF_MERGE_OUTPUT_ALREADY_EXISTS"
	ErrorCodeDuplicateInput        = "PDF_DUPLICATE_INPUT"
	ErrorCodeOutputCollidesInput   = "PDF_OUTPUT_COLLIDES_INPUT"
	ErrorCodeOutputDirNotFound     = "PDF_OUTPUT_DIR_NOT_FOUND"
	ErrorCodeOutputDirNotDirectory = "PDF_OUTPUT_DIR_NOT_DIRECTORY"
	ErrorCodeOutputDirNotWritable  = "PDF_OUTPUT_DIR_NOT_WRITABLE"
)

type MergeError struct {
	Code    string
	Message string
	Details map[string]any
	Cause   error
}

func (e *MergeError) Error() string {
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *MergeError) Unwrap() error {
	return e.Cause
}

// MergeWithPageRanges merges inputPaths into outputPath, optionally keeping a
// page selection per input. pageRanges is one entry per input; an empty entry
// keeps the whole file. A nil or short slice treats missing entries as empty.
func MergeWithPageRanges(ctx context.Context, inputPaths []string, outputPath string, pageRanges []string) error {
	if err := ctx.Err(); err != nil {
		return &MergeError{Code: "CANCELED", Message: "merge canceled", Cause: err}
	}

	if validationErr := ValidateMergePaths(inputPaths, outputPath); validationErr != nil {
		return validationErr
	}

	if validationErr := validateMergeInputs(ctx, inputPaths); validationErr != nil {
		return validationErr
	}

	ranges := normalizeMergePageRanges(pageRanges, len(inputPaths))
	if !hasMergePageRanges(ranges) {
		if err := api.MergeCreateFile(pdfCPUContext(ctx), inputPaths, outputPath, false, newPDFCPUConfig()); err != nil {
			return &MergeError{Code: ErrorCodeMergeFailed, Message: "failed to merge PDF files", Cause: err}
		}

		return nil
	}

	if rangeErr := ValidateMergePageRanges(inputPaths, ranges); rangeErr != nil {
		return rangeErr
	}

	return mergeSelectedPages(ctx, inputPaths, outputPath, ranges)
}

// ValidateMergePageRanges checks every non-empty page range against its input
// PDF. Ranges use pdfcpu page selection syntax ("1", "1-3", "1,3-5", "even").
// Empty entries mean "whole file" and are skipped.
func ValidateMergePageRanges(inputPaths []string, pageRanges []string) *MergeError {
	ranges := normalizeMergePageRanges(pageRanges, len(inputPaths))

	for i, inputPath := range inputPaths {
		if ranges[i] == "" {
			continue
		}

		pageCount, err := api.PageCountFile(context.Background(), inputPath)
		if err != nil {
			return &MergeError{
				Code:    ErrorCodeInvalidInputPDF,
				Message: fmt.Sprintf("unable to determine page count for: %s", filepath.Base(inputPath)),
				Details: mergePageRangeDetails(inputPath, ranges[i], 0),
				Cause:   err,
			}
		}

		if rangeErr := validateMergePageRange(inputPath, ranges[i], pageCount); rangeErr != nil {
			return rangeErr
		}
	}

	return nil
}

func validateMergeInputs(ctx context.Context, inputPaths []string) *MergeError {
	inputValidationErrors := make([]map[string]any, 0)
	for _, inputPath := range inputPaths {
		if err := ctx.Err(); err != nil {
			return &MergeError{Code: "CANCELED", Message: "merge canceled", Cause: err}
		}

		if !isPDFPath(inputPath) {
			inputValidationErrors = append(inputValidationErrors, map[string]any{
				"inputPath": inputPath,
				"fileName":  filepath.Base(inputPath),
				"code":      ErrorCodeValidation,
				"message":   fmt.Sprintf("input file must be .pdf: %s", inputPath),
			})
			continue
		}

		if err := api.ValidateFile(pdfCPUContext(ctx), inputPath, newPDFCPUConfig(), nil); err != nil {
			code := classifyInputValidationError(err)
			inputValidationErrors = append(inputValidationErrors, map[string]any{
				"inputPath": inputPath,
				"fileName":  filepath.Base(inputPath),
				"code":      code,
				"message":   fmt.Sprintf("%s: %s", codeMessagePrefix(code), filepath.Base(inputPath)),
			})
		}
	}

	if len(inputValidationErrors) > 0 {
		return aggregateInputValidationError(inputValidationErrors)
	}

	return nil
}

func mergeSelectedPages(ctx context.Context, inputPaths []string, outputPath string, ranges []string) error {
	trimmedOutput := strings.TrimSpace(outputPath)

	outputFile, err := os.OpenFile(trimmedOutput, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		if errors.Is(err, os.ErrExist) {
			return &MergeError{Code: ErrorCodeMergeOutputExists, Message: fmt.Sprintf("output file already exists: %s", trimmedOutput)}
		}

		return &MergeError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output path is not writable: %s", trimmedOutput), Cause: err}
	}

	readers := make([]io.ReadSeeker, 0, len(inputPaths))
	openInputs := make([]*os.File, 0, len(inputPaths))
	completed := false

	defer func() {
		for _, inputFile := range openInputs {
			_ = inputFile.Close()
		}

		if completed {
			return
		}

		_ = outputFile.Close()
		_ = os.Remove(trimmedOutput)
	}()

	for i, inputPath := range inputPaths {
		if err := ctx.Err(); err != nil {
			return &MergeError{Code: "CANCELED", Message: "merge canceled", Cause: err}
		}

		inputFile, openErr := os.Open(inputPath)
		if openErr != nil {
			return &MergeError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input PDF is not accessible: %s", filepath.Base(inputPath)), Cause: openErr}
		}

		rangeExpr := strings.TrimSpace(ranges[i])
		if rangeExpr == "" {
			openInputs = append(openInputs, inputFile)
			readers = append(readers, inputFile)
			continue
		}

		tokens, parseErr := api.ParsePageSelection(rangeExpr)
		if parseErr != nil || len(tokens) == 0 {
			_ = inputFile.Close()
			return &MergeError{
				Code:    ErrorCodeMergePageRangeInvalid,
				Message: fmt.Sprintf("invalid page range %q for %s", rangeExpr, filepath.Base(inputPath)),
				Cause:   parseErr,
			}
		}

		var collected bytes.Buffer
		collectErr := api.Collect(pdfCPUContext(ctx), inputFile, &collected, tokens, newPDFCPUConfig())
		_ = inputFile.Close()
		if collectErr != nil {
			return &MergeError{
				Code:    ErrorCodeMergePageRangeInvalid,
				Message: fmt.Sprintf("failed to select pages %q from %s", rangeExpr, filepath.Base(inputPath)),
				Cause:   collectErr,
			}
		}

		readers = append(readers, bytes.NewReader(collected.Bytes()))
	}

	if mergeErr := api.MergeRaw(pdfCPUContext(ctx), readers, outputFile, false, newPDFCPUConfig()); mergeErr != nil {
		return &MergeError{Code: ErrorCodeMergeFailed, Message: "failed to merge PDF files", Cause: mergeErr}
	}

	if closeErr := outputFile.Close(); closeErr != nil {
		return &MergeError{Code: ErrorCodeMergeFailed, Message: "failed to finalize merged PDF", Cause: closeErr}
	}

	completed = true
	return nil
}

func normalizeMergePageRanges(pageRanges []string, inputCount int) []string {
	ranges := make([]string, inputCount)
	for i := 0; i < inputCount && i < len(pageRanges); i++ {
		ranges[i] = strings.TrimSpace(pageRanges[i])
	}

	return ranges
}

func hasMergePageRanges(ranges []string) bool {
	for _, pageRange := range ranges {
		if pageRange != "" {
			return true
		}
	}

	return false
}

func validateMergePageRange(inputPath, rangeExpr string, pageCount int) *MergeError {
	fileName := filepath.Base(inputPath)

	tokens, err := api.ParsePageSelection(rangeExpr)
	if err != nil || len(tokens) == 0 {
		return &MergeError{
			Code:    ErrorCodeMergePageRangeInvalid,
			Message: fmt.Sprintf("invalid page range %q for %s", rangeExpr, fileName),
			Details: mergePageRangeDetails(inputPath, rangeExpr, pageCount),
			Cause:   err,
		}
	}

	for _, token := range tokens {
		if tokenErr := validateMergePageRangeToken(token, pageCount); tokenErr != nil {
			return &MergeError{
				Code:    ErrorCodeMergePageRangeInvalid,
				Message: fmt.Sprintf("invalid page range %q for %s: %v", rangeExpr, fileName, tokenErr),
				Details: mergePageRangeDetails(inputPath, rangeExpr, pageCount),
			}
		}
	}

	collected, err := api.PagesForPageCollection(pageCount, tokens)
	if err != nil {
		return &MergeError{
			Code:    ErrorCodeMergePageRangeInvalid,
			Message: fmt.Sprintf("invalid page range %q for %s", rangeExpr, fileName),
			Details: mergePageRangeDetails(inputPath, rangeExpr, pageCount),
			Cause:   err,
		}
	}

	selected, err := api.PagesForPageSelection(pageCount, tokens, false, false)
	if err != nil {
		return &MergeError{
			Code:    ErrorCodeMergePageRangeInvalid,
			Message: fmt.Sprintf("invalid page range %q for %s", rangeExpr, fileName),
			Details: mergePageRangeDetails(inputPath, rangeExpr, pageCount),
			Cause:   err,
		}
	}

	if len(collected) != len(selected) {
		return &MergeError{
			Code:    ErrorCodeMergePageRangeInvalid,
			Message: fmt.Sprintf("page range %q for %s selects overlapping pages", rangeExpr, fileName),
			Details: mergePageRangeDetails(inputPath, rangeExpr, pageCount),
		}
	}

	return nil
}

func validateMergePageRangeToken(token string, pageCount int) error {
	normalized := token
	if len(normalized) > 0 && (normalized[0] == '!' || normalized[0] == 'n') {
		normalized = normalized[1:]
	}

	switch normalized {
	case "even", "odd", "l":
		return nil
	}

	if strings.HasPrefix(normalized, "-") {
		lastCount, err := strconv.Atoi(strings.TrimPrefix(normalized, "-"))
		if err != nil {
			return fmt.Errorf("invalid page token %q", token)
		}

		return validateMergePageNumber(lastCount, pageCount)
	}

	if strings.HasPrefix(normalized, "l-") {
		lastCount, err := strconv.Atoi(strings.TrimSuffix(normalized[2:], "-"))
		if err != nil {
			return fmt.Errorf("invalid page token %q", token)
		}

		if lastCount < 1 || lastCount > pageCount {
			return fmt.Errorf("last-page count %d is outside 1-%d", lastCount, pageCount)
		}

		return nil
	}

	parts := strings.Split(normalized, "-")
	start, err := strconv.Atoi(parts[0])
	if err != nil {
		return fmt.Errorf("invalid page token %q", token)
	}

	if err := validateMergePageNumber(start, pageCount); err != nil {
		return err
	}

	if len(parts) == 1 {
		return nil
	}

	if len(parts) == 2 && (parts[1] == "" || parts[1] == "l") {
		return nil
	}

	if len(parts) == 2 {
		end, err := strconv.Atoi(parts[1])
		if err != nil {
			return fmt.Errorf("invalid page token %q", token)
		}

		if end < start {
			return fmt.Errorf("page range %q is descending", token)
		}

		return validateMergePageNumber(end, pageCount)
	}

	if len(parts) == 3 && parts[1] == "l" {
		offset, err := strconv.Atoi(parts[2])
		if err != nil {
			return fmt.Errorf("invalid page token %q", token)
		}

		end := pageCount - offset
		if offset < 0 || end < start {
			return fmt.Errorf("page range %q is outside 1-%d", token, pageCount)
		}

		return nil
	}

	return fmt.Errorf("invalid page token %q", token)
}

func validateMergePageNumber(page, pageCount int) error {
	if page < 1 || page > pageCount {
		return fmt.Errorf("page %d is outside 1-%d", page, pageCount)
	}

	return nil
}

func mergePageRangeDetails(inputPath, rangeExpr string, pageCount int) map[string]any {
	return map[string]any{
		"inputPath": inputPath,
		"fileName":  filepath.Base(inputPath),
		"pageRange": rangeExpr,
		"pageCount": pageCount,
	}
}

func classifyInputValidationError(err error) string {
	msg := strings.ToLower(err.Error())
	if strings.Contains(msg, "password") || strings.Contains(msg, "encrypt") || strings.Contains(msg, "decrypt") || strings.Contains(msg, "protected") {
		return ErrorCodeProtectedInputPDF
	}

	return ErrorCodeInvalidInputPDF
}

func aggregateInputValidationError(fileErrors []map[string]any) *MergeError {
	codes := make([]string, 0, len(fileErrors))
	for _, fileError := range fileErrors {
		code, _ := fileError["code"].(string)
		if code != "" {
			codes = append(codes, code)
		}
	}

	errorCode := ErrorCodeInvalidInputsPDF
	if len(codes) == 1 {
		errorCode = codes[0]
	} else if len(codes) > 1 {
		firstCode := codes[0]
		allSame := true
		for _, code := range codes[1:] {
			if code != firstCode {
				allSame = false
				break
			}
		}
		if allSame {
			errorCode = firstCode
		}
	}

	return &MergeError{
		Code:    errorCode,
		Message: fmt.Sprintf("%d input file(s) failed PDF validation", len(fileErrors)),
		Details: map[string]any{
			"fileErrors": fileErrors,
		},
	}
}

func codeMessagePrefix(code string) string {
	switch code {
	case ErrorCodeProtectedInputPDF:
		return "password-protected PDF input"
	default:
		return "invalid PDF input"
	}
}

func ValidateMergePaths(inputPaths []string, outputPath string) *MergeError {
	if len(inputPaths) < 2 {
		return &MergeError{Code: ErrorCodeValidation, Message: "at least 2 input PDFs are required"}
	}

	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return &MergeError{Code: ErrorCodeValidation, Message: "outputPath is required"}
	}

	if !isPDFPath(outputPath) {
		return &MergeError{Code: ErrorCodeValidation, Message: "outputPath must use .pdf extension"}
	}

	outputKey := fsx.NormalizePathKey(outputPath)
	seen := make(map[string]struct{}, len(inputPaths))
	for _, inputPath := range inputPaths {
		normalizedInput := strings.TrimSpace(inputPath)
		if !isPDFPath(normalizedInput) {
			return &MergeError{Code: ErrorCodeValidation, Message: fmt.Sprintf("input file must be .pdf: %s", inputPath)}
		}

		inputKey := fsx.NormalizePathKey(normalizedInput)
		if _, exists := seen[inputKey]; exists {
			return &MergeError{Code: ErrorCodeDuplicateInput, Message: fmt.Sprintf("duplicate input path: %s", normalizedInput)}
		}
		seen[inputKey] = struct{}{}

		if inputKey == outputKey {
			return &MergeError{Code: ErrorCodeOutputCollidesInput, Message: fmt.Sprintf("outputPath collides with input path: %s", normalizedInput)}
		}
	}

	if validationErr := validateOutputDir(outputPath); validationErr != nil {
		return validationErr
	}

	if existsErr := ensureMergeOutputDoesNotExist(outputPath); existsErr != nil {
		return existsErr
	}

	return nil
}

func ensureMergeOutputDoesNotExist(outputPath string) *MergeError {
	outputPath = strings.TrimSpace(outputPath)

	_, err := os.Stat(outputPath)
	if err == nil {
		return &MergeError{Code: ErrorCodeMergeOutputExists, Message: fmt.Sprintf("output file already exists: %s", outputPath)}
	}

	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return &MergeError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: err}
}

func IsMergeErrorCode(err error, code string) bool {
	var mergeErr *MergeError
	if !errors.As(err, &mergeErr) {
		return false
	}

	return mergeErr.Code == code
}

func isPDFPath(path string) bool {
	return strings.EqualFold(filepath.Ext(strings.TrimSpace(path)), ".pdf")
}

func validateOutputDir(outputPath string) *MergeError {
	dir := filepath.Dir(outputPath)
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}

	info, err := os.Stat(dir)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &MergeError{Code: ErrorCodeOutputDirNotFound, Message: fmt.Sprintf("output directory does not exist: %s", dir), Cause: err}
		}
		return &MergeError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not accessible: %s", dir), Cause: err}
	}

	if !info.IsDir() {
		return &MergeError{Code: ErrorCodeOutputDirNotDirectory, Message: fmt.Sprintf("output path parent is not a directory: %s", dir)}
	}

	tmp, err := os.CreateTemp(dir, ".fileforge-writecheck-*.tmp")
	if err != nil {
		return &MergeError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: err}
	}
	if closeErr := tmp.Close(); closeErr != nil {
		_ = os.Remove(tmp.Name())
		return &MergeError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: closeErr}
	}
	if removeErr := os.Remove(tmp.Name()); removeErr != nil {
		return &MergeError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", dir), Cause: removeErr}
	}

	return nil
}
