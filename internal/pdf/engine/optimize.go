package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
)

const (
	// OptimizeMetadataMaxLength bounds every metadata value accepted by the
	// optimize tool.
	OptimizeMetadataMaxLength = 512

	ErrorCodeOptimizeFailed          = "PDF_OPTIMIZE_FAILED"
	ErrorCodeOptimizeMetadataInvalid = "PDF_OPTIMIZE_METADATA_INVALID"
	ErrorCodeOptimizeOutputExists    = "PDF_OPTIMIZE_OUTPUT_ALREADY_EXISTS"
)

type OptimizeError struct {
	Code    string
	Message string
	Cause   error
}

func (e *OptimizeError) Error() string {
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *OptimizeError) Unwrap() error {
	return e.Cause
}

// ValidateOptimizeInput checks that inputPath is an existing .pdf file without
// parsing the document, mirroring the other PDF tools' plan-time checks.
func ValidateOptimizeInput(inputPath string) *OptimizeError {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return &OptimizeError{Code: ErrorCodeValidation, Message: "inputPath is required"}
	}

	if !isPDFPath(inputPath) {
		return &OptimizeError{Code: ErrorCodeValidation, Message: fmt.Sprintf("input file must be .pdf: %s", inputPath)}
	}

	info, err := os.Stat(inputPath)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &OptimizeError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input PDF was not found: %s", filepath.Base(inputPath)), Cause: err}
		}
		return &OptimizeError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input PDF is not accessible: %s", filepath.Base(inputPath)), Cause: err}
	}

	if info.IsDir() {
		return &OptimizeError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("input path is a directory: %s", filepath.Base(inputPath))}
	}

	return nil
}

// ValidateOptimizeDestination checks that dir exists, is a directory and
// accepts writes, so an auto output can be promised safely.
func ValidateOptimizeDestination(dir string) *OptimizeError {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		return &OptimizeError{Code: ErrorCodeValidation, Message: "output directory is required"}
	}

	if validationErr := validateOutputDirectory(filepath.Clean(dir), false); validationErr != nil {
		return &OptimizeError{Code: validationErr.Code, Message: validationErr.Message, Cause: validationErr.Cause}
	}

	return nil
}

// ValidateOptimizeRequest validates an explicit input/output pair. The output
// must be a non-existing .pdf inside a writable directory and must not collide
// with the input.
func ValidateOptimizeRequest(inputPath, outputPath string) *OptimizeError {
	if validationErr := ValidateOptimizeInput(inputPath); validationErr != nil {
		return validationErr
	}

	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return &OptimizeError{Code: ErrorCodeValidation, Message: "outputPath is required"}
	}

	if !isPDFPath(outputPath) {
		return &OptimizeError{Code: ErrorCodeValidation, Message: "outputPath must use .pdf extension"}
	}

	if normalizePathKey(inputPath) == normalizePathKey(outputPath) {
		return &OptimizeError{Code: ErrorCodeOutputCollidesInput, Message: fmt.Sprintf("outputPath collides with input path: %s", inputPath)}
	}

	if validationErr := validateOutputDirectory(filepath.Dir(outputPath), false); validationErr != nil {
		return &OptimizeError{Code: validationErr.Code, Message: validationErr.Message, Cause: validationErr.Cause}
	}

	if existsErr := ensureOptimizeOutputAbsent(outputPath); existsErr != nil {
		return existsErr
	}

	return nil
}

// Optimize writes a non-destructive optimized copy of inputPath to outputPath.
// Properties are applied to the same content; an empty properties map simply
// publishes the optimized (or copied) file. The output is staged in a unique
// temporary file inside the output directory and never overwrites a file.
func Optimize(ctx context.Context, inputPath, outputPath string, compress bool, properties map[string]string) error {
	if err := ctx.Err(); err != nil {
		return &OptimizeError{Code: "CANCELED", Message: "optimize canceled", Cause: err}
	}

	if validationErr := ValidateOptimizeRequest(inputPath, outputPath); validationErr != nil {
		return validationErr
	}

	outputDir := filepath.Dir(outputPath)
	staged, err := os.CreateTemp(outputDir, ".fileforge-optimize-*.tmp")
	if err != nil {
		return &OptimizeError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", outputDir), Cause: err}
	}

	stagedPath := staged.Name()
	if closeErr := staged.Close(); closeErr != nil {
		_ = os.Remove(stagedPath)
		return &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to prepare a temporary file", Cause: closeErr}
	}
	defer os.Remove(stagedPath)

	if compress {
		if optimizeErr := api.OptimizeFile(inputPath, stagedPath, nil); optimizeErr != nil {
			return &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to optimize PDF", Cause: optimizeErr}
		}
	} else if copyErr := copyFileContents(inputPath, stagedPath); copyErr != nil {
		return &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to copy PDF for metadata update", Cause: copyErr}
	}

	// CreateTemp uses 0600; publish a file with the input's permissions instead
	// so the output matches what a normal pdfcpu write would produce.
	if chmodErr := os.Chmod(stagedPath, outputFilePermissions(inputPath)); chmodErr != nil {
		return &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to prepare the optimized output", Cause: chmodErr}
	}

	if len(properties) > 0 {
		if existsErr := ensureOptimizeOutputAbsent(outputPath); existsErr != nil {
			return existsErr
		}
		if metadataErr := api.AddPropertiesFile(stagedPath, outputPath, properties, nil); metadataErr != nil {
			return &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to update PDF metadata", Cause: metadataErr}
		}

		return nil
	}

	if existsErr := ensureOptimizeOutputAbsent(outputPath); existsErr != nil {
		return existsErr
	}
	if renameErr := os.Rename(stagedPath, outputPath); renameErr != nil {
		return &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to write optimized PDF", Cause: renameErr}
	}

	return nil
}

func ensureOptimizeOutputAbsent(outputPath string) *OptimizeError {
	_, err := os.Stat(outputPath)
	if err == nil {
		return &OptimizeError{Code: ErrorCodeOptimizeOutputExists, Message: fmt.Sprintf("output already exists: %s", outputPath)}
	}
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}

	return &OptimizeError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: err}
}

func copyFileContents(srcPath, dstPath string) error {
	src, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer src.Close()

	dst, err := os.Create(dstPath)
	if err != nil {
		return err
	}

	if _, copyErr := io.Copy(dst, src); copyErr != nil {
		_ = dst.Close()
		return copyErr
	}

	return dst.Close()
}

// outputFilePermissions mirrors the input permissions so an optimized copy is
// as readable as the file it came from.
func outputFilePermissions(inputPath string) os.FileMode {
	info, err := os.Stat(inputPath)
	if err != nil {
		return 0o644
	}

	if permissions := info.Mode().Perm(); permissions != 0 {
		return permissions
	}

	return 0o644
}
