package engine

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/pdfcpu/pdfcpu/pkg/api"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/model"
	"github.com/pdfcpu/pdfcpu/pkg/pdfcpu/types"
)

const (
	CropPresetNone   = "none"
	CropPresetSmall  = "small"
	CropPresetMedium = "medium"
	CropPresetLarge  = "large"
	CropPresetCustom = "custom"

	ErrorCodeCropFailed               = "PDF_CROP_FAILED"
	ErrorCodeCropPresetInvalid        = "PDF_CROP_PRESET_INVALID"
	ErrorCodeCropMarginsRequired      = "PDF_CROP_MARGINS_REQUIRED"
	ErrorCodeCropMarginsInvalid       = "PDF_CROP_MARGINS_INVALID"
	ErrorCodeCropPageSelectionBad     = "PDF_CROP_PAGE_SELECTION_INVALID"
	ErrorCodeCropPageSelectionBounds  = "PDF_CROP_PAGE_SELECTION_OUT_OF_BOUNDS"
	ErrorCodeCropOutputExists         = "PDF_CROP_OUTPUT_ALREADY_EXISTS"
	ErrorCodeCropBatchOutputCollision = "PDF_CROP_BATCH_OUTPUT_COLLISION"
)

type CropMargins struct {
	Top    float64
	Right  float64
	Bottom float64
	Left   float64
}

type CropError struct {
	Code    string
	Message string
	Details map[string]any
	Cause   error
}

type CropBatchResult struct {
	InputPath  string
	OutputPath string
	Success    bool
	Error      *CropError
}

func (e *CropError) Error() string {
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *CropError) Unwrap() error {
	return e.Cause
}

func ValidateCropRequest(inputPath, outputPath, pageSelection, preset string, margins *CropMargins) *CropError {
	_, _, err := buildCropPlan(inputPath, outputPath, pageSelection, preset, margins)
	return err
}

func Crop(ctx context.Context, inputPath, outputPath, pageSelection, preset string, margins *CropMargins) error {
	if err := ctx.Err(); err != nil {
		return &CropError{Code: "CANCELED", Message: "crop canceled", Cause: err}
	}

	selectedPages, box, validationErr := buildCropPlan(inputPath, outputPath, pageSelection, preset, margins)
	if validationErr != nil {
		return validationErr
	}

	if err := api.CropFile(inputPath, outputPath, selectedPages, box, nil); err != nil {
		return &CropError{Code: ErrorCodeCropFailed, Message: "failed to crop PDF", Cause: err}
	}

	return nil
}

func ValidateCropBatchRequest(inputPaths []string, outputDir, pageSelection, preset string, margins *CropMargins) *CropError {
	outputDir = strings.TrimSpace(outputDir)
	if outputDir == "" {
		return &CropError{Code: ErrorCodeValidation, Message: "outputDir is required"}
	}

	if len(inputPaths) < 1 {
		return &CropError{Code: ErrorCodeValidation, Message: "at least 1 input PDF is required"}
	}

	outputDirProbe := filepath.Join(outputDir, "_crop_probe_.pdf")
	if validationErr := validateOutputDir(outputDirProbe); validationErr != nil {
		return &CropError{Code: validationErr.Code, Message: validationErr.Message, Cause: validationErr.Cause}
	}

	for _, rawInputPath := range inputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return &CropError{Code: ErrorCodeValidation, Message: "inputPath is required"}
		}
		if !isPDFPath(inputPath) {
			return &CropError{Code: ErrorCodeValidation, Message: fmt.Sprintf("input file must be .pdf: %s", inputPath)}
		}
	}

	if _, selectionErr := parseCropPageSelectionSyntax(pageSelection); selectionErr != nil {
		return selectionErr
	}

	if _, marginsErr := resolveCropMargins(strings.TrimSpace(preset), margins); marginsErr != nil {
		return marginsErr
	}

	return nil
}

func CropBatch(ctx context.Context, inputPaths []string, outputDir, pageSelection, preset string, margins *CropMargins) ([]CropBatchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, &CropError{Code: "CANCELED", Message: "crop canceled", Cause: err}
	}

	validationErr := ValidateCropBatchRequest(inputPaths, outputDir, pageSelection, preset, margins)
	if validationErr != nil {
		return nil, validationErr
	}

	results := make([]CropBatchResult, 0, len(inputPaths))
	reservedOutputs := make(map[string]struct{}, len(inputPaths))
	for _, rawInputPath := range inputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		outputPath := resolveUniqueBatchCropOutputPath(outputDir, inputPath, reservedOutputs)

		if err := ctx.Err(); err != nil {
			return results, &CropError{Code: "CANCELED", Message: "crop canceled", Cause: err}
		}

		selectedPages, box, buildErr := buildCropPlanWithoutOutputExistence(inputPath, outputPath, pageSelection, preset, margins)
		if buildErr != nil {
			results = append(results, CropBatchResult{
				InputPath:  inputPath,
				OutputPath: outputPath,
				Success:    false,
				Error:      buildErr,
			})
			continue
		}

		if err := api.CropFile(inputPath, outputPath, selectedPages, box, nil); err != nil {
			results = append(results, CropBatchResult{
				InputPath:  inputPath,
				OutputPath: outputPath,
				Success:    false,
				Error:      &CropError{Code: ErrorCodeCropFailed, Message: "failed to crop PDF", Cause: err},
			})
			continue
		}

		results = append(results, CropBatchResult{InputPath: inputPath, OutputPath: outputPath, Success: true})
	}

	return results, nil
}

func buildCropPlan(inputPath, outputPath, pageSelection, preset string, margins *CropMargins) ([]string, *model.Box, *CropError) {
	return buildCropPlanWithOptions(inputPath, outputPath, pageSelection, preset, margins, true)
}

func buildCropPlanWithoutOutputExistence(inputPath, outputPath, pageSelection, preset string, margins *CropMargins) ([]string, *model.Box, *CropError) {
	return buildCropPlanWithOptions(inputPath, outputPath, pageSelection, preset, margins, false)
}

func buildCropPlanWithOptions(inputPath, outputPath, pageSelection, preset string, margins *CropMargins, requireOutputNotExists bool) ([]string, *model.Box, *CropError) {
	inputPath = strings.TrimSpace(inputPath)
	if inputPath == "" {
		return nil, nil, &CropError{Code: ErrorCodeValidation, Message: "inputPath is required"}
	}

	if !isPDFPath(inputPath) {
		return nil, nil, &CropError{Code: ErrorCodeValidation, Message: fmt.Sprintf("input file must be .pdf: %s", inputPath)}
	}

	outputPath = strings.TrimSpace(outputPath)
	if outputPath == "" {
		return nil, nil, &CropError{Code: ErrorCodeValidation, Message: "outputPath is required"}
	}

	if !isPDFPath(outputPath) {
		return nil, nil, &CropError{Code: ErrorCodeValidation, Message: "outputPath must use .pdf extension"}
	}

	if normalizePathKey(inputPath) == normalizePathKey(outputPath) {
		return nil, nil, &CropError{Code: ErrorCodeOutputCollidesInput, Message: fmt.Sprintf("outputPath collides with input path: %s", inputPath)}
	}

	if validationErr := validateOutputDir(outputPath); validationErr != nil {
		return nil, nil, &CropError{Code: validationErr.Code, Message: validationErr.Message, Cause: validationErr.Cause}
	}

	if requireOutputNotExists {
		if _, statErr := os.Stat(outputPath); statErr == nil {
			return nil, nil, &CropError{Code: ErrorCodeCropOutputExists, Message: fmt.Sprintf("output already exists: %s", outputPath)}
		} else if !os.IsNotExist(statErr) {
			return nil, nil, &CropError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: statErr}
		}
	}

	if err := api.ValidateFile(inputPath, nil); err != nil {
		return nil, nil, &CropError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("invalid PDF input: %s", filepath.Base(inputPath)), Cause: err}
	}

	pageCount, pageErr := api.PageCountFile(inputPath)
	if pageErr != nil {
		return nil, nil, &CropError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("unable to determine page count for: %s", filepath.Base(inputPath)), Cause: pageErr}
	}

	selectedPages, selectionErr := parseCropPageSelectionWithinPageCount(pageSelection, pageCount)
	if selectionErr != nil {
		return nil, nil, selectionErr
	}

	resolvedMargins, marginsErr := resolveCropMargins(strings.TrimSpace(preset), margins)
	if marginsErr != nil {
		return nil, nil, marginsErr
	}

	box, parseErr := model.ParseBox(formatMarginsAsBoxString(resolvedMargins), types.POINTS)
	if parseErr != nil {
		return nil, nil, &CropError{Code: ErrorCodeCropMarginsInvalid, Message: "invalid crop margins", Cause: parseErr}
	}

	return selectedPages, box, nil
}
func buildBatchCropOutputName(inputPath string) string {
	base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if base == "" {
		base = "input"
	}

	return fmt.Sprintf("%s_cropped.pdf", base)
}

func resolveUniqueBatchCropOutputPath(outputDir, inputPath string, reserved map[string]struct{}) string {
	baseOutputName := buildBatchCropOutputName(inputPath)
	baseWithoutExt := strings.TrimSuffix(baseOutputName, filepath.Ext(baseOutputName))
	ext := filepath.Ext(baseOutputName)
	if ext == "" {
		ext = ".pdf"
	}

	for candidateIndex := 1; ; candidateIndex++ {
		candidateName := baseOutputName
		if candidateIndex > 1 {
			candidateName = fmt.Sprintf("%s-%d%s", baseWithoutExt, candidateIndex, ext)
		}

		candidatePath := filepath.Join(outputDir, candidateName)
		candidateKey := normalizePathKey(candidatePath)
		if _, exists := reserved[candidateKey]; exists {
			continue
		}

		if _, err := os.Stat(candidatePath); err == nil {
			continue
		} else if !os.IsNotExist(err) {
			continue
		}

		reserved[candidateKey] = struct{}{}
		return candidatePath
	}
}

func parseCropPageSelectionSyntax(pageSelection string) ([]SplitPageRange, *CropError) {
	trimmed := strings.TrimSpace(pageSelection)
	if trimmed == "" {
		return nil, nil
	}

	ranges, rangeErr := ParseSplitRanges(trimmed)
	if rangeErr != nil {
		if rangeErr.Code == ErrorCodeSplitRangesRequired || rangeErr.Code == ErrorCodeSplitRangesInvalid {
			return nil, &CropError{Code: ErrorCodeCropPageSelectionBad, Message: rangeErr.Message, Cause: rangeErr.Cause}
		}
		return nil, &CropError{Code: ErrorCodeCropPageSelectionBad, Message: rangeErr.Message, Cause: rangeErr.Cause}
	}

	return ranges, nil
}

func parseCropPageSelectionWithinPageCount(pageSelection string, pageCount int) ([]string, *CropError) {
	ranges, selectionErr := parseCropPageSelectionSyntax(pageSelection)
	if selectionErr != nil {
		return nil, selectionErr
	}
	if len(ranges) == 0 {
		return nil, nil
	}

	for _, r := range ranges {
		if r.End > pageCount {
			return nil, &CropError{Code: ErrorCodeCropPageSelectionBounds, Message: fmt.Sprintf("range %d-%d exceeds PDF page count %d", r.Start, r.End, pageCount)}
		}
	}

	result := make([]string, 0, len(ranges))
	for _, r := range ranges {
		if r.Start == r.End {
			result = append(result, strconv.Itoa(r.Start))
			continue
		}
		result = append(result, fmt.Sprintf("%d-%d", r.Start, r.End))
	}

	return result, nil
}

func resolveCropMargins(preset string, margins *CropMargins) (CropMargins, *CropError) {
	switch preset {
	case CropPresetNone:
		return CropMargins{}, nil
	case CropPresetSmall:
		return CropMargins{Top: 10, Right: 10, Bottom: 10, Left: 10}, nil
	case CropPresetMedium:
		return CropMargins{Top: 20, Right: 20, Bottom: 20, Left: 20}, nil
	case CropPresetLarge:
		return CropMargins{Top: 40, Right: 40, Bottom: 40, Left: 40}, nil
	case CropPresetCustom:
		if margins == nil {
			return CropMargins{}, &CropError{Code: ErrorCodeCropMarginsRequired, Message: "options.margins is required when cropPreset=custom"}
		}

		if invalid := validateMargins(*margins); invalid != "" {
			return CropMargins{}, &CropError{Code: ErrorCodeCropMarginsInvalid, Message: invalid}
		}

		return *margins, nil
	default:
		return CropMargins{}, &CropError{Code: ErrorCodeCropPresetInvalid, Message: fmt.Sprintf("unsupported crop preset: %s", preset)}
	}
}

func validateMargins(m CropMargins) string {
	if m.Top < 0 || m.Right < 0 || m.Bottom < 0 || m.Left < 0 {
		return "crop margins must be >= 0"
	}

	const maxMargin = 720.0
	if m.Top > maxMargin || m.Right > maxMargin || m.Bottom > maxMargin || m.Left > maxMargin {
		return "crop margins must be <= 720"
	}

	return ""
}

func formatMarginsAsBoxString(m CropMargins) string {
	return fmt.Sprintf("%.2f %.2f %.2f %.2f", m.Top, m.Right, m.Bottom, m.Left)
}
