package engine

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
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
	ErrorCodeOptimizeTargetInvalid   = "PDF_OPTIMIZE_TARGET_INVALID"

	// Optimize strategy identifiers reported in OptimizeOutcome.Strategy.
	OptimizeStrategyCopy        = "copy"
	OptimizeStrategyPDFCPU      = "pdfcpu"
	OptimizeStrategyGhostscript = "ghostscript"
	OptimizeStrategyNone        = ""
)

const (
	ghostscriptPresetEbook  = "ebook"
	ghostscriptPresetScreen = "screen"
)

// GhostscriptRunner runs Ghostscript PDF compression with one PDFSETTINGS
// preset. It is a package-level variable so tests can replace the external
// binary with a deterministic stub.
var GhostscriptRunner = runGhostscript

// GhostscriptLookPath reports whether the gs binary is available. It is a
// package-level variable so tests can simulate a machine without Ghostscript.
var GhostscriptLookPath = func() bool {
	_, err := exec.LookPath("gs")
	return err == nil
}

// OptimizeOptions describe one optimize run. TargetSizeBytes enables the
// best-effort pdfcpu → Ghostscript pipeline; zero means no target.
type OptimizeOptions struct {
	Compress        bool
	Properties      map[string]string
	TargetSizeBytes int64
}

// OptimizeOutcome reports what the best-effort pipeline actually produced.
type OptimizeOutcome struct {
	SizeBytes            int64
	TargetSizeBytes      int64
	TargetAttempted      bool
	TargetMet            bool
	GhostscriptAvailable bool
	UsedGhostscript      bool
	GhostscriptError     string
	Strategy             string
}

func runGhostscript(ctx context.Context, inputPath, outputPath, preset string) error {
	command := exec.CommandContext(
		ctx,
		"gs",
		"-sDEVICE=pdfwrite",
		"-dCompatibilityLevel=1.7",
		"-dPDFSETTINGS=/"+preset,
		"-dNOPAUSE",
		"-dQUIET",
		"-dBATCH",
		"-sOutputFile="+outputPath,
		inputPath,
	)
	output, err := command.CombinedOutput()
	if err != nil {
		message := strings.TrimSpace(string(output))
		if message == "" {
			return fmt.Errorf("ghostscript /%s failed: %w", preset, err)
		}
		return fmt.Errorf("ghostscript /%s failed: %w: %s", preset, err, message)
	}

	return nil
}

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
	_, err := OptimizeWithOptions(ctx, inputPath, outputPath, OptimizeOptions{
		Compress:   compress,
		Properties: properties,
	})
	return err
}

// OptimizeWithOptions runs the best-effort compression pipeline. With a
// target, pdfcpu output is compared against Ghostscript's /ebook and /screen
// presets and the smallest candidate is published. Metadata is applied after
// the chosen compression so Ghostscript cannot drop it, and the final file is
// moved into place atomically.
func OptimizeWithOptions(ctx context.Context, inputPath, outputPath string, options OptimizeOptions) (OptimizeOutcome, error) {
	outcome := OptimizeOutcome{
		TargetSizeBytes: options.TargetSizeBytes,
		Strategy:        OptimizeStrategyNone,
	}

	if err := ctx.Err(); err != nil {
		return outcome, &OptimizeError{Code: "CANCELED", Message: "optimize canceled", Cause: err}
	}

	if options.TargetSizeBytes < 0 {
		return outcome, &OptimizeError{Code: ErrorCodeOptimizeTargetInvalid, Message: "target size must be greater than zero"}
	}
	if options.TargetSizeBytes > 0 && !options.Compress {
		return outcome, &OptimizeError{Code: ErrorCodeOptimizeTargetInvalid, Message: "target size requires compression"}
	}

	if validationErr := ValidateOptimizeRequest(inputPath, outputPath); validationErr != nil {
		return outcome, validationErr
	}

	outputDir := filepath.Dir(outputPath)
	temps := newOptimizeTemps(outputDir)
	defer temps.cleanup()

	stagedPath, err := temps.create()
	if err != nil {
		return outcome, &OptimizeError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", outputDir), Cause: err}
	}

	if options.Compress {
		if optimizeErr := api.OptimizeFile(inputPath, stagedPath, nil); optimizeErr != nil {
			return outcome, &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to optimize PDF", Cause: optimizeErr}
		}
		outcome.Strategy = OptimizeStrategyPDFCPU
	} else {
		if copyErr := copyFileContents(inputPath, stagedPath); copyErr != nil {
			return outcome, &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to copy PDF for metadata update", Cause: copyErr}
		}
		outcome.Strategy = OptimizeStrategyCopy
	}

	chosenPath := stagedPath
	chosenSize, err := fileSize(stagedPath)
	if err != nil {
		return outcome, &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to inspect optimized PDF", Cause: err}
	}

	if options.TargetSizeBytes > 0 {
		outcome.TargetAttempted = true
		outcome.GhostscriptAvailable = GhostscriptLookPath()

		if chosenSize > options.TargetSizeBytes && outcome.GhostscriptAvailable {
			for _, preset := range []string{ghostscriptPresetEbook, ghostscriptPresetScreen} {
				candidatePath, createErr := temps.create()
				if createErr != nil {
					return outcome, &OptimizeError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", outputDir), Cause: createErr}
				}

				if runErr := GhostscriptRunner(ctx, inputPath, candidatePath, preset); runErr != nil {
					outcome.GhostscriptError = runErr.Error()
					continue
				}

				candidateSize, sizeErr := fileSize(candidatePath)
				if sizeErr != nil || candidateSize <= 0 {
					continue
				}
				if candidateSize < chosenSize {
					chosenPath = candidatePath
					chosenSize = candidateSize
					outcome.UsedGhostscript = true
					outcome.Strategy = OptimizeStrategyGhostscript
				}

				if chosenSize <= options.TargetSizeBytes {
					break
				}
			}
		}
	}

	publishPath := chosenPath
	if len(options.Properties) > 0 {
		metadataPath, createErr := temps.create()
		if createErr != nil {
			return outcome, &OptimizeError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output directory is not writable: %s", outputDir), Cause: createErr}
		}

		if metadataErr := api.AddPropertiesFile(chosenPath, metadataPath, options.Properties, nil); metadataErr != nil {
			return outcome, &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to update PDF metadata", Cause: metadataErr}
		}
		publishPath = metadataPath
	}

	// CreateTemp uses 0600; publish a file with the input's permissions instead
	// so the output matches what a normal pdfcpu write would produce.
	if chmodErr := os.Chmod(publishPath, outputFilePermissions(inputPath)); chmodErr != nil {
		return outcome, &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to prepare the optimized output", Cause: chmodErr}
	}

	if existsErr := ensureOptimizeOutputAbsent(outputPath); existsErr != nil {
		return outcome, existsErr
	}
	if renameErr := os.Rename(publishPath, outputPath); renameErr != nil {
		return outcome, &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to write optimized PDF", Cause: renameErr}
	}

	finalSize, statErr := fileSize(outputPath)
	if statErr != nil {
		return outcome, &OptimizeError{Code: ErrorCodeOptimizeFailed, Message: "failed to inspect optimized PDF", Cause: statErr}
	}
	outcome.SizeBytes = finalSize
	outcome.TargetMet = options.TargetSizeBytes > 0 && finalSize <= options.TargetSizeBytes

	return outcome, nil
}

// optimizeTemps tracks staged files so every path is removed on return, even
// when a late pipeline step fails.
type optimizeTemps struct {
	dir   string
	paths []string
}

func newOptimizeTemps(dir string) *optimizeTemps {
	return &optimizeTemps{dir: dir}
}

func (t *optimizeTemps) create() (string, error) {
	file, err := os.CreateTemp(t.dir, ".fileforge-optimize-*.tmp")
	if err != nil {
		return "", err
	}
	path := file.Name()
	t.paths = append(t.paths, path)
	if closeErr := file.Close(); closeErr != nil {
		return "", closeErr
	}

	return path, nil
}

func (t *optimizeTemps) cleanup() {
	for _, path := range t.paths {
		_ = os.Remove(path)
	}
}

func fileSize(path string) (int64, error) {
	info, err := os.Stat(path)
	if err != nil {
		return 0, err
	}

	return info.Size(), nil
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
