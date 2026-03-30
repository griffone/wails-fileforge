package engine

import (
	"context"
	"errors"
	"fmt"
	"os"
	pathpkg "path"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const (
	ErrorCodeValidation            = "VALIDATION_ERROR"
	ErrorCodeInvalidInputPDF       = "PDF_INVALID_INPUT"
	ErrorCodeInvalidInputsPDF      = "PDF_INVALID_INPUTS"
	ErrorCodeProtectedInputPDF     = "PDF_PROTECTED_INPUT"
	ErrorCodeMergeFailed           = "PDF_MERGE_FAILED"
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

func Merge(ctx context.Context, inputPaths []string, outputPath string) error {
	if err := ctx.Err(); err != nil {
		return &MergeError{Code: "CANCELED", Message: "merge canceled", Cause: err}
	}

	if validationErr := ValidateMergePaths(inputPaths, outputPath); validationErr != nil {
		return validationErr
	}

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

		if err := api.ValidateFile(inputPath, nil); err != nil {
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

	if err := api.MergeCreateFile(inputPaths, outputPath, false, nil); err != nil {
		return &MergeError{Code: ErrorCodeMergeFailed, Message: "failed to merge PDF files", Cause: err}
	}

	return nil
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

	outputKey := normalizePathKey(outputPath)
	seen := make(map[string]struct{}, len(inputPaths))
	for _, inputPath := range inputPaths {
		normalizedInput := strings.TrimSpace(inputPath)
		if !isPDFPath(normalizedInput) {
			return &MergeError{Code: ErrorCodeValidation, Message: fmt.Sprintf("input file must be .pdf: %s", inputPath)}
		}

		inputKey := normalizePathKey(normalizedInput)
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

	return nil
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

func normalizePathKey(rawPath string) string {
	trimmed := strings.TrimSpace(rawPath)
	if trimmed == "" {
		return ""
	}

	normalizedSlashes := strings.ReplaceAll(trimmed, "\\", "/")

	if strings.HasPrefix(normalizedSlashes, "//") {
		uncTail := strings.TrimLeft(normalizedSlashes, "/")
		cleanedUNC := pathpkg.Clean("/" + uncTail)
		return "unc://" + strings.ToLower(strings.TrimPrefix(cleanedUNC, "/"))
	}

	if len(normalizedSlashes) >= 2 && normalizedSlashes[1] == ':' {
		drive := strings.ToLower(normalizedSlashes[:1])
		rest := normalizedSlashes[2:]
		cleanedRest := pathpkg.Clean("/" + strings.TrimPrefix(rest, "/"))
		return "win:" + drive + ":" + strings.ToLower(cleanedRest)
	}

	cleaned := pathpkg.Clean(normalizedSlashes)
	return "posix:" + strings.ToLower(cleaned)
}
