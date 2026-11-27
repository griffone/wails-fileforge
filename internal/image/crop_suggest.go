package image

import (
	"bytes"
	"image"
	_ "image/gif"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"

	"fileforge-desktop/internal/models"

	"github.com/disintegration/imaging"
	_ "golang.org/x/image/bmp"
	_ "golang.org/x/image/tiff"
	_ "golang.org/x/image/webp"
)

// Border-suggestion heuristic constants. The heuristic is deliberately
// conservative: it is better to return no suggestion than a wrong rectangle.
const (
	// cropSuggestionMaxDimension is the longest edge used for analysis. Large
	// inputs are downscaled before scanning so the analysis stays cheap; the
	// detected insets are mapped back to the original pixel space.
	cropSuggestionMaxDimension = 512
	// cropSuggestionTolerance is the maximum per-channel (RGBA, 8-bit)
	// difference a pixel may have from the sampled edge colour and still count
	// as border. It absorbs JPEG/WebP compression noise without accepting a
	// visible gradient.
	cropSuggestionTolerance = 24
	// cropSuggestionMinBorderThickness is the minimum border width, in
	// analysis pixels, that every one of the four edges must show. A thinner
	// (or missing) edge is not a trustworthy border.
	cropSuggestionMinBorderThickness = 2
	// cropSuggestionMaxInsetFraction caps how far each edge is scanned. A
	// border reaching this cap is wider than a plausible frame and is rejected.
	cropSuggestionMaxInsetFraction = 0.35
	// cropSuggestionMinAreaFraction is the fraction of the image the suggested
	// crop must keep. Trimming more than half of the area is not a border.
	cropSuggestionMinAreaFraction = 0.5
	// cropSuggestionLineMatchFraction is the share of pixels in a border line
	// that must match the sampled edge colour. The small slack tolerates
	// isolated compression artifacts.
	cropSuggestionLineMatchFraction = 0.98
)

const (
	cropSuggestionReasonFound      = "Detected a uniform border on all four edges."
	cropSuggestionReasonNoBorder   = "No uniform border was found."
	cropSuggestionReasonPartial    = "No uniform border was found on all four edges."
	cropSuggestionReasonTooThin    = "The detected border is too thin to suggest a crop."
	cropSuggestionReasonScanLimit  = "The border reaches the scan limit; no safe crop was found."
	cropSuggestionReasonLittleArea = "The detected border leaves too little of the image."
	cropSuggestionConfidenceHigh   = "high"
	cropSuggestionConfidenceMedium = "medium"
)

// CropBorderSuggestion is the result of the pure border analysis. Coordinates
// are in the original image's pixel space, origin top-left.
type CropBorderSuggestion struct {
	Found      bool
	X          int
	Y          int
	Width      int
	Height     int
	Reason     string
	Confidence string
}

// AnalyzeCropBorders scans the four edges of img inward and returns a crop
// rectangle that removes the uniform border. Transparent pixels (alpha 0) are
// treated as border, so logos and screenshots with alpha margins work too.
//
// The scan compares each line against the average colour of that edge's
// outermost line. An edge stops at the first line that does not match within
// cropSuggestionTolerance. All four edges must show at least
// cropSuggestionMinBorderThickness pixels of border, no edge may reach
// cropSuggestionMaxInsetFraction, and the remaining area must keep at least
// cropSuggestionMinAreaFraction of the image. When any check fails the result
// reports Found=false with a short reason.
func AnalyzeCropBorders(img image.Image) CropBorderSuggestion {
	if img == nil {
		return CropBorderSuggestion{Reason: cropSuggestionReasonNoBorder}
	}

	origWidth := img.Bounds().Dx()
	origHeight := img.Bounds().Dy()
	if origWidth < 1 || origHeight < 1 {
		return CropBorderSuggestion{Reason: cropSuggestionReasonNoBorder}
	}

	analysis := imaging.Clone(img)
	analysisWidth, analysisHeight := origWidth, origHeight
	if longest := max(origWidth, origHeight); longest > cropSuggestionMaxDimension {
		resizeWidth, resizeHeight := 0, 0
		if origWidth >= origHeight {
			resizeWidth = cropSuggestionMaxDimension
		} else {
			resizeHeight = cropSuggestionMaxDimension
		}
		analysis = imaging.Resize(analysis, resizeWidth, resizeHeight, imaging.NearestNeighbor)
		analysisWidth = analysis.Bounds().Dx()
		analysisHeight = analysis.Bounds().Dy()
	}

	analysisWidth = analysis.Bounds().Dx()
	analysisHeight = analysis.Bounds().Dy()

	maxInsetX := max(1, int(float64(analysisWidth)*cropSuggestionMaxInsetFraction))
	maxInsetY := max(1, int(float64(analysisHeight)*cropSuggestionMaxInsetFraction))

	left := scanEdge(analysis, maxInsetX, true, false)
	top := scanEdge(analysis, maxInsetY, false, false)
	right := scanEdge(analysis, maxInsetX, true, true)
	bottom := scanEdge(analysis, maxInsetY, false, true)

	reachedLimit := left == maxInsetX || right == maxInsetX ||
		top == maxInsetY || bottom == maxInsetY

	if left == 0 && top == 0 && right == 0 && bottom == 0 {
		return CropBorderSuggestion{Reason: cropSuggestionReasonNoBorder}
	}
	if left < cropSuggestionMinBorderThickness || top < cropSuggestionMinBorderThickness ||
		right < cropSuggestionMinBorderThickness || bottom < cropSuggestionMinBorderThickness {
		return CropBorderSuggestion{Reason: cropSuggestionReasonPartial}
	}
	if reachedLimit {
		return CropBorderSuggestion{Reason: cropSuggestionReasonScanLimit}
	}

	cropWidth := analysisWidth - left - right
	cropHeight := analysisHeight - top - bottom
	if cropWidth < 1 || cropHeight < 1 {
		return CropBorderSuggestion{Reason: cropSuggestionReasonLittleArea}
	}

	areaFraction := float64(cropWidth*cropHeight) / float64(analysisWidth*analysisHeight)
	if areaFraction < cropSuggestionMinAreaFraction {
		return CropBorderSuggestion{Reason: cropSuggestionReasonLittleArea}
	}

	x := scaleInset(left, analysisWidth, origWidth)
	y := scaleInset(top, analysisHeight, origHeight)
	rightInset := scaleInset(right, analysisWidth, origWidth)
	bottomInset := scaleInset(bottom, analysisHeight, origHeight)

	width := origWidth - x - rightInset
	height := origHeight - y - bottomInset
	if width < 1 || height < 1 {
		return CropBorderSuggestion{Reason: cropSuggestionReasonLittleArea}
	}

	confidence := cropSuggestionConfidenceMedium
	if min(left, top, right, bottom) >= cropSuggestionMinBorderThickness*2 {
		confidence = cropSuggestionConfidenceHigh
	}

	return CropBorderSuggestion{
		Found:      true,
		X:          x,
		Y:          y,
		Width:      width,
		Height:     height,
		Reason:     cropSuggestionReasonFound,
		Confidence: confidence,
	}
}

// scanEdge counts matching lines from one edge inward, up to limit lines. A
// line matches when at least cropSuggestionLineMatchFraction of its pixels are
// within tolerance of the average colour of the outermost line.
func scanEdge(img *image.NRGBA, limit int, horizontal, fromEnd bool) int {
	width := img.Bounds().Dx()
	height := img.Bounds().Dy()
	length := width
	if !horizontal {
		length = height
	}

	fixed := 0
	if horizontal {
		if fromEnd {
			fixed = height - 1
		}
	} else if fromEnd {
		fixed = width - 1
	}

	reference := averageLine(img, fixed, length, horizontal)
	inset := 0
	for step := 0; step < limit; step++ {
		line := step
		if fromEnd {
			if horizontal {
				line = height - 1 - step
			} else {
				line = width - 1 - step
			}
		}
		if !lineMatches(img, line, length, reference, horizontal) {
			break
		}
		inset++
	}

	return inset
}

// averageLine returns the rounded average RGBA of a full row (horizontal) or
// column.
func averageLine(img *image.NRGBA, fixed, length int, horizontal bool) [4]uint8 {
	var sumR, sumG, sumB, sumA int
	for index := 0; index < length; index++ {
		offset := img.PixOffset(index, fixed)
		if !horizontal {
			offset = img.PixOffset(fixed, index)
		}
		sumR += int(img.Pix[offset])
		sumG += int(img.Pix[offset+1])
		sumB += int(img.Pix[offset+2])
		sumA += int(img.Pix[offset+3])
	}

	if length < 1 {
		return [4]uint8{}
	}

	return [4]uint8{
		uint8(sumR / length),
		uint8(sumG / length),
		uint8(sumB / length),
		uint8(sumA / length),
	}
}

// lineMatches reports whether enough pixels of the line are border pixels
// relative to reference.
func lineMatches(img *image.NRGBA, fixed, length int, reference [4]uint8, horizontal bool) bool {
	allowed := int(math.Floor(float64(length) * (1 - cropSuggestionLineMatchFraction)))
	mismatches := 0
	for index := 0; index < length; index++ {
		offset := img.PixOffset(index, fixed)
		if !horizontal {
			offset = img.PixOffset(fixed, index)
		}
		if !borderPixel(img.Pix[offset:offset+4], reference) {
			mismatches++
			if mismatches > allowed {
				return false
			}
		}
	}

	return true
}

// borderPixel treats fully transparent pixels as border and otherwise compares
// each channel against the sampled edge colour.
func borderPixel(pixel []uint8, reference [4]uint8) bool {
	if pixel[3] == 0 {
		return true
	}

	return channelDelta(pixel[0], reference[0]) <= cropSuggestionTolerance &&
		channelDelta(pixel[1], reference[1]) <= cropSuggestionTolerance &&
		channelDelta(pixel[2], reference[2]) <= cropSuggestionTolerance &&
		channelDelta(pixel[3], reference[3]) <= cropSuggestionTolerance
}

func channelDelta(a, b uint8) int {
	if a > b {
		return int(a - b)
	}
	return int(b - a)
}

// scaleInset maps an analysis-space inset back to the original dimensions.
func scaleInset(inset, analysisDim, originalDim int) int {
	if analysisDim == originalDim || analysisDim < 1 {
		return inset
	}

	scaled := int(math.Round(float64(inset) * float64(originalDim) / float64(analysisDim)))
	if scaled < 0 {
		return 0
	}
	if scaled > originalDim-1 {
		return originalDim - 1
	}

	return scaled
}

// GetImageCropSuggestion analyzes inputPath and returns a suggested crop that
// removes a uniform border. Nothing is cropped; the caller decides whether to
// accept the suggestion.
func (t *CropTool) GetImageCropSuggestion(inputPath string) models.ImageCropSuggestionResponse {
	path := strings.TrimSpace(inputPath)
	if path == "" {
		return models.ImageCropSuggestionResponse{
			Success: false,
			Message: "Select a valid image path and retry.",
			Error:   models.NewCanonicalJobError("IMAGE_CROP_SUGGESTION_INVALID_PATH", "inputPath is required", nil),
		}
	}

	data, _, _, _, err := readAndNormalize(path)
	if err != nil {
		return models.ImageCropSuggestionResponse{
			Success: false,
			Message: "Cannot analyze the image.",
			Error:   models.NewCanonicalJobError("IMAGE_CROP_SUGGESTION_READ_FAILED", err.Error(), map[string]any{"inputPath": path}),
		}
	}

	decoded, _, decodeErr := image.Decode(bytes.NewReader(data))
	if decodeErr != nil {
		return models.ImageCropSuggestionResponse{
			Success: false,
			Message: "Cannot analyze the image.",
			Error:   models.NewCanonicalJobError("IMAGE_CROP_SUGGESTION_DECODE_FAILED", decodeErr.Error(), map[string]any{"inputPath": path}),
		}
	}

	suggestion := AnalyzeCropBorders(decoded)
	if !suggestion.Found {
		return models.ImageCropSuggestionResponse{
			Success: true,
			Message: suggestion.Reason,
			Found:   false,
			Reason:  suggestion.Reason,
		}
	}

	return models.ImageCropSuggestionResponse{
		Success:    true,
		Message:    suggestion.Reason,
		Found:      true,
		X:          suggestion.X,
		Y:          suggestion.Y,
		Width:      suggestion.Width,
		Height:     suggestion.Height,
		Reason:     suggestion.Reason,
		Confidence: suggestion.Confidence,
	}
}
