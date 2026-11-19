package engine

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"

	"fileforge-desktop/internal/platform/fsx"

	"github.com/h2non/bimg"
	"github.com/jung-kurt/gofpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// Images-to-PDF public values. Page sizes are the portrait dimensions in
// millimetres; the orientation swaps them at render time.
const (
	ImagesPageA4     = "a4"
	ImagesPageLetter = "letter"
	ImagesPageA5     = "a5"

	ImagesOrientationPortrait  = "portrait"
	ImagesOrientationLandscape = "landscape"

	ImagesFitContain = "contain"
	ImagesFitCover   = "cover"

	ImagesErrorInvalidInput      = "PDF_IMAGES_INVALID_INPUT"
	ImagesErrorUnsupportedFormat = "PDF_IMAGES_UNSUPPORTED_FORMAT"
	ImagesErrorDuplicateInput    = "PDF_IMAGES_DUPLICATE_INPUT"
	ImagesErrorInvalidLayout     = "PDF_IMAGES_INVALID_LAYOUT"
	ImagesErrorUnplacedImages    = "PDF_IMAGES_UNPLACED_IMAGES"
	ImagesErrorOutputExists      = "PDF_IMAGES_OUTPUT_EXISTS"
	ImagesErrorOutputDir         = "PDF_IMAGES_OUTPUT_DIR_INVALID"
	ImagesErrorDecodeFailed      = "PDF_IMAGES_DECODE_FAILED"
	ImagesErrorWriteFailed       = "PDF_IMAGES_WRITE_FAILED"
	ImagesErrorVerifyFailed      = "PDF_IMAGES_VERIFY_FAILED"
	ImagesErrorCancelled         = "PDF_IMAGES_CANCELLED"
)

// Images-to-PDF limits. They mirror the tool contract so the engine and the UI
// agree on what a valid request looks like.
const (
	imagesMaxInputs          = 100
	imagesMaxPageIndex       = 200
	imagesMaxGridDimension   = 4
	imagesMaxCells           = 16
	imagesMaxMarginMM        = 50.0
	imagesMaxGapMM           = 20.0
	imagesMaxEdgePixels      = 2400
	imagesJPEGQuality        = 92
	imagesPageDimTolerancePT = 1.0
	imagesPointsPerInch      = 72.0
	imagesMillimetresPerInch = 25.4
	imagesTempFilePattern    = ".fileforge-images-*.pdf"
	imagesWriteCheckPattern  = ".fileforge-images-writecheck-*.tmp"
	imagesResourceNamePrefix = "fileforge-image-"
)

// imagesPageSizesMM holds the portrait dimensions of every supported page size
// in millimetres.
var imagesPageSizesMM = map[string][2]float64{
	ImagesPageA4:     {210.0, 297.0},
	ImagesPageLetter: {215.9, 279.4},
	ImagesPageA5:     {148.0, 210.0},
}

// imagesSupportedExtensions lists the accepted input extensions. The extension
// is only a filter: the actual format is confirmed by the decoder.
var imagesSupportedExtensions = map[string]struct{}{
	".jpg":  {},
	".jpeg": {},
	".png":  {},
	".gif":  {},
	".webp": {},
	".bmp":  {},
	".tiff": {},
	".tif":  {},
}

// ImageSlot places one input image in one cell of one PDF page. Pages and
// cells are zero-based.
type ImageSlot struct {
	Page       int
	Cell       int
	InputIndex int
}

// ImagesLayout describes the grid an images-to-PDF run renders. An empty Slots
// slice means "fill the grid in input order"; a non-empty slice must place
// every input at least once.
type ImagesLayout struct {
	PageSize    string
	Orientation string
	Columns     int
	Rows        int
	MarginMM    float64
	GapMM       float64
	Fit         string
	Slots       []ImageSlot
}

// ImagesResult reports what the build produced.
type ImagesResult struct {
	OutputPath   string
	PageCount    int
	ImageCount   int
	PageWidthMM  float64
	PageHeightMM float64
}

// ImagesError is a typed images-to-PDF failure with a stable code the tool maps
// to a canonical job error.
type ImagesError struct {
	Code    string
	Message string
	Details map[string]any
	Cause   error
}

func (e *ImagesError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *ImagesError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// ValidateImagesPDFRequest validates the input images, the output path and the
// grid layout. It auto-fills empty slots, normalizes the layout values and
// returns the resulting page count.
func ValidateImagesPDFRequest(inputPaths []string, outputPath string, layout ImagesLayout) (ImagesLayout, int, *ImagesError) {
	normalizedInputs, inputErr := validateImagesInputs(inputPaths)
	if inputErr != nil {
		return ImagesLayout{}, 0, inputErr
	}

	if _, outputErr := validateImagesOutputPath(normalizedInputs, outputPath); outputErr != nil {
		return ImagesLayout{}, 0, outputErr
	}

	normalizedLayout, layoutErr := normalizeImagesLayout(layout)
	if layoutErr != nil {
		return ImagesLayout{}, 0, layoutErr
	}

	slots, pageCount, slotsErr := resolveImagesSlots(normalizedLayout, len(normalizedInputs))
	if slotsErr != nil {
		return ImagesLayout{}, 0, slotsErr
	}
	normalizedLayout.Slots = slots

	return normalizedLayout, pageCount, nil
}

// validateImagesInputs checks the input list and returns the trimmed paths in
// the original order. Duplicate paths (compared by their normalized key) are
// rejected because each image is normalized exactly once.
func validateImagesInputs(inputPaths []string) ([]string, *ImagesError) {
	if len(inputPaths) == 0 {
		return nil, &ImagesError{Code: ImagesErrorInvalidInput, Message: "at least one input image is required"}
	}
	if len(inputPaths) > imagesMaxInputs {
		return nil, &ImagesError{
			Code:    ImagesErrorInvalidInput,
			Message: fmt.Sprintf("at most %d input images are supported", imagesMaxInputs),
			Details: map[string]any{"inputCount": len(inputPaths)},
		}
	}

	normalizedInputs := make([]string, 0, len(inputPaths))
	firstIndexByKey := make(map[string]int, len(inputPaths))

	for index, rawPath := range inputPaths {
		inputPath := strings.TrimSpace(rawPath)
		if inputPath == "" {
			return nil, &ImagesError{
				Code:    ImagesErrorInvalidInput,
				Message: "input path is required",
				Details: map[string]any{"inputIndex": index},
			}
		}

		if !isImagesSupportedPath(inputPath) {
			return nil, &ImagesError{
				Code:    ImagesErrorUnsupportedFormat,
				Message: fmt.Sprintf("unsupported image format: %s", filepath.Base(inputPath)),
				Details: map[string]any{
					"inputPath":  inputPath,
					"inputIndex": index,
					"extension":  strings.ToLower(filepath.Ext(inputPath)),
				},
			}
		}

		info, statErr := os.Stat(inputPath)
		if statErr != nil {
			if errors.Is(statErr, os.ErrNotExist) {
				return nil, &ImagesError{
					Code:    ImagesErrorInvalidInput,
					Message: fmt.Sprintf("input image was not found: %s", filepath.Base(inputPath)),
					Details: map[string]any{"inputPath": inputPath, "inputIndex": index},
					Cause:   statErr,
				}
			}

			return nil, &ImagesError{
				Code:    ImagesErrorInvalidInput,
				Message: fmt.Sprintf("input image is not accessible: %s", filepath.Base(inputPath)),
				Details: map[string]any{"inputPath": inputPath, "inputIndex": index},
				Cause:   statErr,
			}
		}
		if info.IsDir() {
			return nil, &ImagesError{
				Code:    ImagesErrorInvalidInput,
				Message: fmt.Sprintf("input path is a directory: %s", filepath.Base(inputPath)),
				Details: map[string]any{"inputPath": inputPath, "inputIndex": index},
			}
		}
		if !info.Mode().IsRegular() {
			return nil, &ImagesError{
				Code:    ImagesErrorInvalidInput,
				Message: fmt.Sprintf("input path is not a regular file: %s", filepath.Base(inputPath)),
				Details: map[string]any{"inputPath": inputPath, "inputIndex": index},
			}
		}

		inputKey := fsx.NormalizePathKey(inputPath)
		if firstIndex, exists := firstIndexByKey[inputKey]; exists {
			return nil, &ImagesError{
				Code:    ImagesErrorDuplicateInput,
				Message: fmt.Sprintf("duplicate input path: %s", inputPath),
				Details: map[string]any{
					"inputPath":       inputPath,
					"inputIndex":      index,
					"firstInputIndex": firstIndex,
				},
			}
		}
		firstIndexByKey[inputKey] = index

		normalizedInputs = append(normalizedInputs, inputPath)
	}

	return normalizedInputs, nil
}

// validateImagesOutputPath checks that the destination is a non-existing .pdf
// that does not overwrite one of the inputs and lives in a writable directory.
func validateImagesOutputPath(inputPaths []string, outputPath string) (string, *ImagesError) {
	trimmedOutput := strings.TrimSpace(outputPath)
	if trimmedOutput == "" {
		return "", &ImagesError{Code: ImagesErrorInvalidInput, Message: "outputPath is required"}
	}

	// The collision check runs before the extension check: an input image path
	// is never a .pdf, so this is the only place the "never overwrite an input"
	// rule can be enforced.
	outputKey := fsx.NormalizePathKey(trimmedOutput)
	for _, inputPath := range inputPaths {
		if fsx.NormalizePathKey(inputPath) == outputKey {
			// There is no dedicated collision code: overwriting an input is the
			// same user-facing problem as writing over an existing file.
			return "", &ImagesError{
				Code:    ImagesErrorOutputExists,
				Message: fmt.Sprintf("outputPath collides with an input image: %s", trimmedOutput),
				Details: map[string]any{"outputPath": trimmedOutput},
			}
		}
	}

	if !strings.EqualFold(filepath.Ext(trimmedOutput), ".pdf") {
		return "", &ImagesError{Code: ImagesErrorInvalidInput, Message: "outputPath must use .pdf extension"}
	}

	if _, statErr := os.Stat(trimmedOutput); statErr == nil {
		return "", &ImagesError{
			Code:    ImagesErrorOutputExists,
			Message: fmt.Sprintf("output file already exists: %s", trimmedOutput),
			Details: map[string]any{"outputPath": trimmedOutput},
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return "", &ImagesError{
			Code:    ImagesErrorOutputDir,
			Message: fmt.Sprintf("output path is not accessible: %s", trimmedOutput),
			Details: map[string]any{"outputPath": trimmedOutput},
			Cause:   statErr,
		}
	}

	if dirErr := validateImagesOutputDir(trimmedOutput); dirErr != nil {
		return "", dirErr
	}

	return trimmedOutput, nil
}

// validateImagesOutputDir checks that the destination directory exists, is a
// directory and accepts writes, so a successful run can be promised.
func validateImagesOutputDir(outputPath string) *ImagesError {
	dir := filepath.Dir(outputPath)
	if strings.TrimSpace(dir) == "" {
		dir = "."
	}

	info, statErr := os.Stat(dir)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return &ImagesError{
				Code:    ImagesErrorOutputDir,
				Message: fmt.Sprintf("output directory does not exist: %s", dir),
				Details: map[string]any{"outputDir": dir},
				Cause:   statErr,
			}
		}

		return &ImagesError{
			Code:    ImagesErrorOutputDir,
			Message: fmt.Sprintf("output directory is not accessible: %s", dir),
			Details: map[string]any{"outputDir": dir},
			Cause:   statErr,
		}
	}
	if !info.IsDir() {
		return &ImagesError{
			Code:    ImagesErrorOutputDir,
			Message: fmt.Sprintf("output path parent is not a directory: %s", dir),
			Details: map[string]any{"outputDir": dir},
		}
	}

	probe, probeErr := os.CreateTemp(dir, imagesWriteCheckPattern)
	if probeErr != nil {
		return &ImagesError{
			Code:    ImagesErrorOutputDir,
			Message: fmt.Sprintf("output directory is not writable: %s", dir),
			Details: map[string]any{"outputDir": dir},
			Cause:   probeErr,
		}
	}
	probePath := probe.Name()
	if closeErr := probe.Close(); closeErr != nil {
		_ = os.Remove(probePath)
		return &ImagesError{
			Code:    ImagesErrorOutputDir,
			Message: fmt.Sprintf("output directory is not writable: %s", dir),
			Details: map[string]any{"outputDir": dir},
			Cause:   closeErr,
		}
	}
	if removeErr := os.Remove(probePath); removeErr != nil {
		return &ImagesError{
			Code:    ImagesErrorOutputDir,
			Message: fmt.Sprintf("output directory is not writable: %s", dir),
			Details: map[string]any{"outputDir": dir},
			Cause:   removeErr,
		}
	}

	return nil
}

// normalizeImagesLayout canonicalizes the layout values, applies the documented
// defaults (a4, portrait, contain) and rejects layouts that cannot hold a
// visible cell.
func normalizeImagesLayout(layout ImagesLayout) (ImagesLayout, *ImagesError) {
	normalized := ImagesLayout{
		PageSize:    strings.ToLower(strings.TrimSpace(layout.PageSize)),
		Orientation: strings.ToLower(strings.TrimSpace(layout.Orientation)),
		Columns:     layout.Columns,
		Rows:        layout.Rows,
		MarginMM:    layout.MarginMM,
		GapMM:       layout.GapMM,
		Fit:         strings.ToLower(strings.TrimSpace(layout.Fit)),
		Slots:       append([]ImageSlot(nil), layout.Slots...),
	}

	if normalized.PageSize == "" {
		normalized.PageSize = ImagesPageA4
	}
	if _, supported := imagesPageSizesMM[normalized.PageSize]; !supported {
		return ImagesLayout{}, imagesLayoutFieldError("pageSize", layout.PageSize, "page size must be a4, letter or a5")
	}

	if normalized.Orientation == "" {
		normalized.Orientation = ImagesOrientationPortrait
	}
	switch normalized.Orientation {
	case ImagesOrientationPortrait, ImagesOrientationLandscape:
	default:
		return ImagesLayout{}, imagesLayoutFieldError("orientation", layout.Orientation, "orientation must be portrait or landscape")
	}

	if normalized.Fit == "" {
		normalized.Fit = ImagesFitContain
	}
	switch normalized.Fit {
	case ImagesFitContain, ImagesFitCover:
	default:
		return ImagesLayout{}, imagesLayoutFieldError("fit", layout.Fit, "fit must be contain or cover")
	}

	if normalized.Columns < 1 || normalized.Columns > imagesMaxGridDimension {
		return ImagesLayout{}, imagesLayoutFieldError("columns", normalized.Columns, fmt.Sprintf("columns must be between 1 and %d", imagesMaxGridDimension))
	}
	if normalized.Rows < 1 || normalized.Rows > imagesMaxGridDimension {
		return ImagesLayout{}, imagesLayoutFieldError("rows", normalized.Rows, fmt.Sprintf("rows must be between 1 and %d", imagesMaxGridDimension))
	}
	if normalized.Columns*normalized.Rows > imagesMaxCells {
		return ImagesLayout{}, imagesLayoutFieldError("columns", normalized.Columns, fmt.Sprintf("the grid must have at most %d cells", imagesMaxCells))
	}

	if err := validateImagesMillimetres("marginMM", normalized.MarginMM, imagesMaxMarginMM); err != nil {
		return ImagesLayout{}, err
	}
	if err := validateImagesMillimetres("gapMM", normalized.GapMM, imagesMaxGapMM); err != nil {
		return ImagesLayout{}, err
	}

	pageWidthMM, pageHeightMM := imagesPageDimensions(normalized)
	contentWidthMM := pageWidthMM - 2*normalized.MarginMM
	contentHeightMM := pageHeightMM - 2*normalized.MarginMM
	if contentWidthMM <= 0 || contentHeightMM <= 0 {
		return ImagesLayout{}, &ImagesError{
			Code:    ImagesErrorInvalidLayout,
			Message: "the margins leave no content area on the page",
			Details: map[string]any{
				"pageWidthMM":  pageWidthMM,
				"pageHeightMM": pageHeightMM,
				"marginMM":     normalized.MarginMM,
			},
		}
	}

	cellWidthMM := (contentWidthMM - normalized.GapMM*float64(normalized.Columns-1)) / float64(normalized.Columns)
	cellHeightMM := (contentHeightMM - normalized.GapMM*float64(normalized.Rows-1)) / float64(normalized.Rows)
	if cellWidthMM <= 0 || cellHeightMM <= 0 {
		return ImagesLayout{}, &ImagesError{
			Code:    ImagesErrorInvalidLayout,
			Message: "the margins and gaps leave no usable cell on the page",
			Details: map[string]any{
				"cellWidthMM":  cellWidthMM,
				"cellHeightMM": cellHeightMM,
				"columns":      normalized.Columns,
				"rows":         normalized.Rows,
				"gapMM":        normalized.GapMM,
			},
		}
	}

	return normalized, nil
}

func validateImagesMillimetres(field string, value, maximum float64) *ImagesError {
	if math.IsNaN(value) || math.IsInf(value, 0) || value < 0 || value > maximum {
		return imagesLayoutFieldError(field, value, fmt.Sprintf("%s must be a finite value between 0 and %g mm", field, maximum))
	}

	return nil
}

func imagesLayoutFieldError(field string, value any, message string) *ImagesError {
	return &ImagesError{
		Code:    ImagesErrorInvalidLayout,
		Message: message,
		Details: map[string]any{"field": field, "value": value},
	}
}

// resolveImagesSlots auto-fills the grid in input order when no explicit slots
// were given, validates every slot and requires full input coverage. It returns
// the effective slots and the resulting page count.
func resolveImagesSlots(layout ImagesLayout, inputCount int) ([]ImageSlot, int, *ImagesError) {
	cellCount := layout.Columns * layout.Rows

	slots := layout.Slots
	if len(slots) == 0 {
		slots = make([]ImageSlot, 0, inputCount)
		for index := 0; index < inputCount; index++ {
			slots = append(slots, ImageSlot{
				Page:       index / cellCount,
				Cell:       index % cellCount,
				InputIndex: index,
			})
		}
	}

	placed := make([]bool, inputCount)
	pageCount := 0

	for index, slot := range slots {
		if slot.Page < 0 || slot.Page >= imagesMaxPageIndex {
			return nil, 0, imagesSlotError(index, "page", slot.Page, fmt.Sprintf("slot page must be between 0 and %d", imagesMaxPageIndex-1))
		}
		if slot.Cell < 0 || slot.Cell >= cellCount {
			return nil, 0, imagesSlotError(index, "cell", slot.Cell, fmt.Sprintf("slot cell must be between 0 and %d", cellCount-1))
		}
		if slot.InputIndex < 0 || slot.InputIndex >= inputCount {
			return nil, 0, imagesSlotError(index, "inputIndex", slot.InputIndex, fmt.Sprintf("slot inputIndex must be between 0 and %d", inputCount-1))
		}

		placed[slot.InputIndex] = true
		if slot.Page+1 > pageCount {
			pageCount = slot.Page + 1
		}
	}

	missing := 0
	for _, isPlaced := range placed {
		if !isPlaced {
			missing++
		}
	}
	if missing > 0 {
		return nil, 0, &ImagesError{
			Code:    ImagesErrorUnplacedImages,
			Message: fmt.Sprintf("%d input image(s) are not placed in the layout", missing),
			Details: map[string]any{
				"missing": missing,
				"total":   inputCount,
			},
		}
	}

	return slots, pageCount, nil
}

func imagesSlotError(slotIndex int, field string, value int, message string) *ImagesError {
	return &ImagesError{
		Code:    ImagesErrorInvalidLayout,
		Message: message,
		Details: map[string]any{
			"slotIndex": slotIndex,
			"field":     field,
			"value":     value,
		},
	}
}

// imagesPageDimensions returns the page size in millimetres for the requested
// orientation. The size table always holds portrait dimensions.
func imagesPageDimensions(layout ImagesLayout) (float64, float64) {
	dimensions := imagesPageSizesMM[layout.PageSize]
	widthMM, heightMM := dimensions[0], dimensions[1]
	if layout.Orientation == ImagesOrientationLandscape {
		widthMM, heightMM = heightMM, widthMM
	}

	return widthMM, heightMM
}

func isImagesSupportedPath(path string) bool {
	_, supported := imagesSupportedExtensions[strings.ToLower(filepath.Ext(strings.TrimSpace(path)))]

	return supported
}

// BuildImagesPDF validates the request, renders one PDF and publishes it
// atomically. onProgress is called once per normalized input image and may be
// nil.
func BuildImagesPDF(ctx context.Context, inputPaths []string, outputPath string, layout ImagesLayout, onProgress func(processed, total int)) (ImagesResult, *ImagesError) {
	if ctx == nil {
		ctx = context.Background()
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ImagesResult{}, cancelledImagesError("images to PDF canceled", ctxErr)
	}

	normalizedLayout, pageCount, validationErr := ValidateImagesPDFRequest(inputPaths, outputPath, layout)
	if validationErr != nil {
		return ImagesResult{}, validationErr
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return ImagesResult{}, cancelledImagesError("images to PDF canceled", ctxErr)
	}

	normalizedInputs := make([]string, 0, len(inputPaths))
	for _, inputPath := range inputPaths {
		normalizedInputs = append(normalizedInputs, strings.TrimSpace(inputPath))
	}
	trimmedOutput := strings.TrimSpace(outputPath)
	pageWidthMM, pageHeightMM := imagesPageDimensions(normalizedLayout)

	pdf, documentErr := newImagesPDFDocument(normalizedLayout)
	if documentErr != nil {
		return ImagesResult{}, documentErr
	}

	prepared, prepareErr := prepareImagesPDFResources(ctx, pdf, normalizedInputs, onProgress)
	if prepareErr != nil {
		return ImagesResult{}, prepareErr
	}

	if drawErr := drawImagesPDFPages(ctx, pdf, normalizedLayout, prepared, pageCount, pageWidthMM, pageHeightMM); drawErr != nil {
		return ImagesResult{}, drawErr
	}
	if pdfErr := pdf.Error(); pdfErr != nil {
		return ImagesResult{}, &ImagesError{Code: ImagesErrorWriteFailed, Message: "failed to render the PDF pages", Cause: pdfErr}
	}

	return publishImagesPDF(ctx, pdf, trimmedOutput, pageCount, len(normalizedInputs), pageWidthMM, pageHeightMM)
}

// imagesPDFResource is a normalized input image registered with gofpdf.
type imagesPDFResource struct {
	Name        string
	ImageType   string
	PixelWidth  int
	PixelHeight int
}

// newImagesPDFDocument creates the gofpdf document with the exact millimetre
// page size. gofpdf's built-in "A5" entry is 148.5 mm wide, which would not
// match the 148 mm contract, so every page size is passed explicitly.
func newImagesPDFDocument(layout ImagesLayout) (*gofpdf.Fpdf, *ImagesError) {
	orientation := "P"
	if layout.Orientation == ImagesOrientationLandscape {
		orientation = "L"
	}

	portraitDimensions := imagesPageSizesMM[layout.PageSize]
	document := gofpdf.NewCustom(&gofpdf.InitType{
		OrientationStr: orientation,
		UnitStr:        "mm",
		Size:           gofpdf.SizeType{Wd: portraitDimensions[0], Ht: portraitDimensions[1]},
	})
	if documentErr := document.Error(); documentErr != nil {
		return nil, &ImagesError{Code: ImagesErrorWriteFailed, Message: "failed to create the PDF document", Cause: documentErr}
	}

	document.SetMargins(0, 0, 0)
	document.SetAutoPageBreak(false, 0)
	document.SetCompression(true)

	return document, nil
}

// prepareImagesPDFResources normalizes every unique input once and registers it
// with gofpdf. Progress is reported after each normalized image.
func prepareImagesPDFResources(ctx context.Context, pdf *gofpdf.Fpdf, inputPaths []string, onProgress func(processed, total int)) ([]imagesPDFResource, *ImagesError) {
	total := len(inputPaths)
	resources := make([]imagesPDFResource, 0, total)

	for index, inputPath := range inputPaths {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return nil, cancelledImagesError("images to PDF canceled", ctxErr)
		}

		normalized, imageType, pixelWidth, pixelHeight, normalizeErr := normalizeImagesPDFImage(inputPath)
		if normalizeErr != nil {
			return nil, normalizeErr
		}

		resourceName := fmt.Sprintf("%s%d", imagesResourceNamePrefix, index)
		pdf.RegisterImageOptionsReader(resourceName, gofpdf.ImageOptions{ImageType: imageType, ReadDpi: false}, bytes.NewReader(normalized))
		if registerErr := pdf.Error(); registerErr != nil {
			return nil, &ImagesError{
				Code:    ImagesErrorDecodeFailed,
				Message: fmt.Sprintf("failed to decode image: %s", filepath.Base(inputPath)),
				Details: map[string]any{"inputPath": inputPath, "fileName": filepath.Base(inputPath)},
				Cause:   registerErr,
			}
		}

		resources = append(resources, imagesPDFResource{
			Name:        resourceName,
			ImageType:   imageType,
			PixelWidth:  pixelWidth,
			PixelHeight: pixelHeight,
		})

		if onProgress != nil {
			onProgress(index+1, total)
		}
	}

	return resources, nil
}

// drawImagesPDFPages renders every page with a white background and draws the
// images that belong to it. Cells without a slot stay white.
func drawImagesPDFPages(ctx context.Context, pdf *gofpdf.Fpdf, layout ImagesLayout, resources []imagesPDFResource, pageCount int, pageWidthMM, pageHeightMM float64) *ImagesError {
	contentWidthMM := pageWidthMM - 2*layout.MarginMM
	contentHeightMM := pageHeightMM - 2*layout.MarginMM
	cellWidthMM := (contentWidthMM - layout.GapMM*float64(layout.Columns-1)) / float64(layout.Columns)
	cellHeightMM := (contentHeightMM - layout.GapMM*float64(layout.Rows-1)) / float64(layout.Rows)

	slotsByPage := make([][]ImageSlot, pageCount)
	for _, slot := range layout.Slots {
		slotsByPage[slot.Page] = append(slotsByPage[slot.Page], slot)
	}

	for page := 0; page < pageCount; page++ {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return cancelledImagesError("images to PDF canceled", ctxErr)
		}

		pdf.AddPage()
		pdf.SetFillColor(255, 255, 255)
		pdf.Rect(0, 0, pageWidthMM, pageHeightMM, "F")

		for _, slot := range slotsByPage[page] {
			column := slot.Cell % layout.Columns
			row := slot.Cell / layout.Columns
			cellX := layout.MarginMM + float64(column)*(cellWidthMM+layout.GapMM)
			cellY := layout.MarginMM + float64(row)*(cellHeightMM+layout.GapMM)
			drawImagesPDFCell(pdf, resources[slot.InputIndex], layout.Fit, cellX, cellY, cellWidthMM, cellHeightMM)
		}
	}

	return nil
}

// drawImagesPDFCell centers one image inside a cell. Contain keeps the whole
// image visible; cover scales it up and clips the overflow to the cell.
func drawImagesPDFCell(pdf *gofpdf.Fpdf, resource imagesPDFResource, fit string, cellX, cellY, cellWidthMM, cellHeightMM float64) {
	pixelWidth := float64(resource.PixelWidth)
	pixelHeight := float64(resource.PixelHeight)

	scale := math.Min(cellWidthMM/pixelWidth, cellHeightMM/pixelHeight)
	if fit == ImagesFitCover {
		scale = math.Max(cellWidthMM/pixelWidth, cellHeightMM/pixelHeight)
	}

	drawWidth := pixelWidth * scale
	drawHeight := pixelHeight * scale
	drawX := cellX + (cellWidthMM-drawWidth)/2
	drawY := cellY + (cellHeightMM-drawHeight)/2

	options := gofpdf.ImageOptions{ImageType: resource.ImageType, ReadDpi: false}
	clipped := fit == ImagesFitCover
	if clipped {
		options.AllowNegativePosition = true
		pdf.ClipRect(cellX, cellY, cellWidthMM, cellHeightMM, false)
	}

	pdf.ImageOptions(resource.Name, drawX, drawY, drawWidth, drawHeight, false, options, 0, "")

	if clipped {
		pdf.ClipEnd()
	}
}

// normalizeImagesPDFImage decodes one input with bimg, applies the EXIF
// orientation and caps the longest edge. PNG sources and images with an alpha
// channel stay PNG; everything else becomes a quality-92 JPEG with the metadata
// stripped. It returns the normalized bytes, the gofpdf image type and the
// pixel dimensions of the normalized image.
func normalizeImagesPDFImage(inputPath string) ([]byte, string, int, int, *ImagesError) {
	fileName := filepath.Base(inputPath)

	source, readErr := os.ReadFile(inputPath)
	if readErr != nil {
		return nil, "", 0, 0, &ImagesError{
			Code:    ImagesErrorInvalidInput,
			Message: fmt.Sprintf("failed to read image: %s", fileName),
			Details: map[string]any{"inputPath": inputPath, "fileName": fileName},
			Cause:   readErr,
		}
	}

	metadata, metadataErr := bimg.NewImage(source).Metadata()
	if metadataErr != nil || metadata.Size.Width <= 0 || metadata.Size.Height <= 0 {
		return nil, "", 0, 0, imagesDecodeError(inputPath, metadataErr)
	}

	sourceType := strings.ToLower(strings.TrimSpace(bimg.DetermineImageTypeName(source)))
	outputType := bimg.JPEG
	if metadata.Alpha || sourceType == "png" {
		outputType = bimg.PNG
	}

	// A JPEG is auto-rotated by Process itself; every other format with an
	// orientation tag is rotated explicitly so the size read below is upright.
	working := source
	rotatedWidth, rotatedHeight := metadata.Size.Width, metadata.Size.Height
	if sourceType != "jpeg" && metadata.Orientation > 1 {
		rotated, rotateErr := bimg.NewImage(source).AutoRotate()
		if rotateErr != nil {
			return nil, "", 0, 0, imagesDecodeError(inputPath, rotateErr)
		}

		rotatedSize, sizeErr := bimg.NewImage(rotated).Size()
		if sizeErr != nil || rotatedSize.Width <= 0 || rotatedSize.Height <= 0 {
			return nil, "", 0, 0, imagesDecodeError(inputPath, sizeErr)
		}

		working = rotated
		rotatedWidth, rotatedHeight = rotatedSize.Width, rotatedSize.Height
	} else if metadata.Orientation >= 5 && metadata.Orientation <= 8 {
		// Orientations 5-8 rotate by 90 degrees, which swaps the axes.
		rotatedWidth, rotatedHeight = rotatedHeight, rotatedWidth
	}

	options := bimg.Options{
		Type:          outputType,
		Quality:       imagesJPEGQuality,
		StripMetadata: true,
	}
	switch {
	case rotatedWidth >= rotatedHeight && rotatedWidth > imagesMaxEdgePixels:
		options.Width = imagesMaxEdgePixels
	case rotatedHeight > imagesMaxEdgePixels:
		options.Height = imagesMaxEdgePixels
	}

	normalized, processErr := bimg.NewImage(working).Process(options)
	if processErr != nil {
		return nil, "", 0, 0, imagesDecodeError(inputPath, processErr)
	}

	normalizedSize, sizeErr := bimg.NewImage(normalized).Size()
	if sizeErr != nil || normalizedSize.Width <= 0 || normalizedSize.Height <= 0 {
		return nil, "", 0, 0, imagesDecodeError(inputPath, sizeErr)
	}

	imageType := "jpeg"
	if outputType == bimg.PNG {
		imageType = "png"
	}

	return normalized, imageType, normalizedSize.Width, normalizedSize.Height, nil
}

func imagesDecodeError(inputPath string, cause error) *ImagesError {
	fileName := filepath.Base(inputPath)

	return &ImagesError{
		Code:    ImagesErrorDecodeFailed,
		Message: fmt.Sprintf("failed to decode image: %s", fileName),
		Details: map[string]any{"inputPath": inputPath, "fileName": fileName},
		Cause:   cause,
	}
}

// publishImagesPDF writes the document to a temp file in the destination
// directory, verifies it with pdfcpu, makes it world-readable and renames it
// into place. The temp file is removed on every failure path.
func publishImagesPDF(ctx context.Context, pdf *gofpdf.Fpdf, outputPath string, pageCount, imageCount int, pageWidthMM, pageHeightMM float64) (ImagesResult, *ImagesError) {
	outputDir := filepath.Dir(outputPath)

	tempFile, tempErr := os.CreateTemp(outputDir, imagesTempFilePattern)
	if tempErr != nil {
		return ImagesResult{}, &ImagesError{
			Code:    ImagesErrorWriteFailed,
			Message: fmt.Sprintf("failed to create the output file in: %s", outputDir),
			Details: map[string]any{"outputPath": outputPath, "outputDir": outputDir},
			Cause:   tempErr,
		}
	}
	tempPath := tempFile.Name()
	if closeErr := tempFile.Close(); closeErr != nil {
		_ = os.Remove(tempPath)
		return ImagesResult{}, &ImagesError{Code: ImagesErrorWriteFailed, Message: "failed to prepare the output file", Cause: closeErr}
	}

	published := false
	defer func() {
		if !published {
			_ = os.Remove(tempPath)
		}
	}()

	if outputErr := pdf.OutputFileAndClose(tempPath); outputErr != nil {
		return ImagesResult{}, &ImagesError{Code: ImagesErrorWriteFailed, Message: "failed to write the PDF file", Cause: outputErr}
	}
	if pdfErr := pdf.Error(); pdfErr != nil {
		return ImagesResult{}, &ImagesError{Code: ImagesErrorWriteFailed, Message: "failed to write the PDF file", Cause: pdfErr}
	}

	if ctxErr := ctx.Err(); ctxErr != nil {
		return ImagesResult{}, cancelledImagesError("images to PDF canceled", ctxErr)
	}

	if verifyErr := verifyImagesPDF(tempPath, pageCount, pageWidthMM, pageHeightMM); verifyErr != nil {
		return ImagesResult{}, verifyErr
	}

	if chmodErr := os.Chmod(tempPath, 0o644); chmodErr != nil {
		return ImagesResult{}, &ImagesError{Code: ImagesErrorWriteFailed, Message: "failed to publish the PDF file", Cause: chmodErr}
	}

	if _, statErr := os.Stat(outputPath); statErr == nil {
		return ImagesResult{}, &ImagesError{
			Code:    ImagesErrorOutputExists,
			Message: fmt.Sprintf("output file already exists: %s", outputPath),
			Details: map[string]any{"outputPath": outputPath},
		}
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return ImagesResult{}, &ImagesError{
			Code:    ImagesErrorWriteFailed,
			Message: fmt.Sprintf("output path is not accessible: %s", outputPath),
			Details: map[string]any{"outputPath": outputPath},
			Cause:   statErr,
		}
	}

	if renameErr := os.Rename(tempPath, outputPath); renameErr != nil {
		return ImagesResult{}, &ImagesError{
			Code:    ImagesErrorWriteFailed,
			Message: fmt.Sprintf("failed to publish the PDF file: %s", outputPath),
			Details: map[string]any{"outputPath": outputPath},
			Cause:   renameErr,
		}
	}
	published = true

	return ImagesResult{
		OutputPath:   outputPath,
		PageCount:    pageCount,
		ImageCount:   imageCount,
		PageWidthMM:  pageWidthMM,
		PageHeightMM: pageHeightMM,
	}, nil
}

// verifyImagesPDF checks the page count and the first page dimensions before
// the temp file replaces the destination.
func verifyImagesPDF(pdfPath string, expectedPages int, expectedWidthMM, expectedHeightMM float64) *ImagesError {
	dimensions, dimsErr := api.PageDimsFile(pdfPath)
	if dimsErr != nil {
		return &ImagesError{Code: ImagesErrorVerifyFailed, Message: "failed to read the generated PDF", Cause: dimsErr}
	}
	if len(dimensions) != expectedPages {
		return &ImagesError{
			Code:    ImagesErrorVerifyFailed,
			Message: fmt.Sprintf("the generated PDF has %d pages but %d were expected", len(dimensions), expectedPages),
			Details: map[string]any{"expectedPages": expectedPages, "actualPages": len(dimensions)},
		}
	}

	expectedWidthPT := expectedWidthMM * imagesPointsPerInch / imagesMillimetresPerInch
	expectedHeightPT := expectedHeightMM * imagesPointsPerInch / imagesMillimetresPerInch
	if math.Abs(dimensions[0].Width-expectedWidthPT) > imagesPageDimTolerancePT ||
		math.Abs(dimensions[0].Height-expectedHeightPT) > imagesPageDimTolerancePT {
		return &ImagesError{
			Code:    ImagesErrorVerifyFailed,
			Message: fmt.Sprintf("the generated PDF page is %.2fx%.2f pt but %.2fx%.2f pt were expected", dimensions[0].Width, dimensions[0].Height, expectedWidthPT, expectedHeightPT),
			Details: map[string]any{
				"expectedWidthPT":  expectedWidthPT,
				"expectedHeightPT": expectedHeightPT,
				"actualWidthPT":    dimensions[0].Width,
				"actualHeightPT":   dimensions[0].Height,
			},
		}
	}

	return nil
}

func cancelledImagesError(message string, cause error) *ImagesError {
	return &ImagesError{Code: ImagesErrorCancelled, Message: message, Cause: cause}
}
