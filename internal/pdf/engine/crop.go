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
	Cause   error
}

type CropBatchResult struct {
	InputPath  string
	OutputPath string
}

type cropBatchPlanItem struct {
	InputPath     string
	OutputPath    string
	SelectedPages []string
	Box           *model.Box
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
	_, err := buildCropBatchPlan(inputPaths, outputDir, pageSelection, preset, margins)
	return err
}

func CropBatch(ctx context.Context, inputPaths []string, outputDir, pageSelection, preset string, margins *CropMargins) ([]CropBatchResult, error) {
	if err := ctx.Err(); err != nil {
		return nil, &CropError{Code: "CANCELED", Message: "crop canceled", Cause: err}
	}

	plans, validationErr := buildCropBatchPlan(inputPaths, outputDir, pageSelection, preset, margins)
	if validationErr != nil {
		return nil, validationErr
	}

	results := make([]CropBatchResult, 0, len(plans))
	for _, plan := range plans {
		if err := ctx.Err(); err != nil {
			return results, &CropError{Code: "CANCELED", Message: "crop canceled", Cause: err}
		}

		if err := api.CropFile(plan.InputPath, plan.OutputPath, plan.SelectedPages, plan.Box, nil); err != nil {
			return results, &CropError{Code: ErrorCodeCropFailed, Message: "failed to crop PDF", Cause: err}
		}

		results = append(results, CropBatchResult{InputPath: plan.InputPath, OutputPath: plan.OutputPath})
	}

	return results, nil
}

func buildCropPlan(inputPath, outputPath, pageSelection, preset string, margins *CropMargins) ([]string, *model.Box, *CropError) {
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

	if _, statErr := os.Stat(outputPath); statErr == nil {
		return nil, nil, &CropError{Code: ErrorCodeCropOutputExists, Message: fmt.Sprintf("output already exists: %s", outputPath)}
	} else if !os.IsNotExist(statErr) {
		return nil, nil, &CropError{Code: ErrorCodeOutputDirNotWritable, Message: fmt.Sprintf("output path is not accessible: %s", outputPath), Cause: statErr}
	}

	if err := api.ValidateFile(inputPath, nil); err != nil {
		return nil, nil, &CropError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("invalid PDF input: %s", filepath.Base(inputPath)), Cause: err}
	}

	pageCount, pageErr := api.PageCountFile(inputPath)
	if pageErr != nil {
		return nil, nil, &CropError{Code: ErrorCodeInvalidInputPDF, Message: fmt.Sprintf("unable to determine page count for: %s", filepath.Base(inputPath)), Cause: pageErr}
	}

	selectedPages, selectionErr := parseCropPageSelection(pageSelection, pageCount)
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

func buildCropBatchPlan(inputPaths []string, outputDir, pageSelection, preset string, margins *CropMargins) ([]cropBatchPlanItem, *CropError) {
	outputDir = strings.TrimSpace(outputDir)
	if outputDir == "" {
		return nil, &CropError{Code: ErrorCodeValidation, Message: "outputDir is required"}
	}

	if len(inputPaths) < 1 {
		return nil, &CropError{Code: ErrorCodeValidation, Message: "at least 1 input PDF is required"}
	}

	outputDirProbe := filepath.Join(outputDir, "_crop_probe_.pdf")
	if validationErr := validateOutputDir(outputDirProbe); validationErr != nil {
		return nil, &CropError{Code: validationErr.Code, Message: validationErr.Message, Cause: validationErr.Cause}
	}

	plans := make([]cropBatchPlanItem, 0, len(inputPaths))
	seenOutputs := make(map[string]string, len(inputPaths))

	for _, rawInputPath := range inputPaths {
		inputPath := strings.TrimSpace(rawInputPath)
		if inputPath == "" {
			return nil, &CropError{Code: ErrorCodeValidation, Message: "inputPath is required"}
		}

		outputPath := filepath.Join(outputDir, buildBatchCropOutputName(inputPath))
		outputKey := normalizePathKey(outputPath)
		if previous, exists := seenOutputs[outputKey]; exists {
			return nil, &CropError{Code: ErrorCodeCropBatchOutputCollision, Message: fmt.Sprintf("batch planned outputs collide: %s and %s", previous, outputPath)}
		}
		seenOutputs[outputKey] = outputPath

		selectedPages, box, validationErr := buildCropPlan(inputPath, outputPath, pageSelection, preset, margins)
		if validationErr != nil {
			return nil, validationErr
		}

		plans = append(plans, cropBatchPlanItem{
			InputPath:     inputPath,
			OutputPath:    outputPath,
			SelectedPages: selectedPages,
			Box:           box,
		})
	}

	return plans, nil
}

func buildBatchCropOutputName(inputPath string) string {
	base := strings.TrimSpace(strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath)))
	if base == "" {
		base = "input"
	}

	return fmt.Sprintf("%s_cropped.pdf", base)
}

func parseCropPageSelection(pageSelection string, pageCount int) ([]string, *CropError) {
	trimmed := strings.TrimSpace(pageSelection)
	if trimmed == "" {
		return nil, nil
	}

	ranges, rangeErr := ParseSplitRanges(trimmed)
	if rangeErr != nil {
		if rangeErr.Code == ErrorCodeSplitRangesRequired || rangeErr.Code == ErrorCodeSplitRangesInvalid {
			return nil, &CropError{Code: ErrorCodeCropPageSelectionBad, Message: rangeErr.Message, Cause: rangeErr.Cause}
		}
		if rangeErr.Code == ErrorCodeSplitRangeOutBounds {
			return nil, &CropError{Code: ErrorCodeCropPageSelectionBounds, Message: rangeErr.Message, Cause: rangeErr.Cause}
		}
		return nil, &CropError{Code: ErrorCodeCropPageSelectionBad, Message: rangeErr.Message, Cause: rangeErr.Cause}
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
