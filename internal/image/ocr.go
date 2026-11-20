package image

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

	"fileforge-desktop/internal/pdf/engine"
	"fileforge-desktop/internal/platform/fsx"

	"github.com/h2non/bimg"
)

// Image OCR error codes. Every failure carries one of these stable codes so the
// tool can map it to a canonical job error without inspecting messages.
const (
	ImageOCRCodeRuntimeUnavailable  = "IMAGE_OCR_RUNTIME_UNAVAILABLE"
	ImageOCRCodeLanguageUnavailable = "IMAGE_OCR_LANGUAGE_UNAVAILABLE"
	ImageOCRCodeDPIInvalid          = "IMAGE_OCR_DPI_INVALID"
	ImageOCRCodeInputRequired       = "IMAGE_OCR_INPUT_REQUIRED"
	ImageOCRCodeInputNotFound       = "IMAGE_OCR_INPUT_NOT_FOUND"
	ImageOCRCodeInputUnsupported    = "IMAGE_OCR_INPUT_UNSUPPORTED"
	ImageOCRCodeOutputInvalid       = "IMAGE_OCR_OUTPUT_INVALID"
	ImageOCRCodeOutputExists        = "IMAGE_OCR_OUTPUT_ALREADY_EXISTS"
	ImageOCRCodeOutputDirInvalid    = "IMAGE_OCR_OUTPUT_DIR_INVALID"
	ImageOCRCodeNormalizeFailed     = "IMAGE_OCR_NORMALIZE_FAILED"
	ImageOCRCodeRecognizeFailed     = "IMAGE_OCR_RECOGNIZE_FAILED"
	ImageOCRCodeVerifyFailed        = "IMAGE_OCR_VERIFY_FAILED"
	ImageOCRCodePublishFailed       = "IMAGE_OCR_PUBLISH_FAILED"
	ImageOCRCodeCancelled           = "IMAGE_OCR_CANCELLED"
)

const (
	// ImageOCRWarningNoText is a warning-only result: Tesseract finished but
	// recognized no characters, so the text output is empty.
	ImageOCRWarningNoText = "No text was recognized."
)

const (
	imageOCRTempDirPattern   = "fileforge-image-ocr-*"
	imageOCRTempFilePattern  = ".fileforge-image-ocr-*.tmp"
	imageOCRNormalizedName   = "normalized.png"
	imageOCRRecognizeBase    = "recognized"
	imageOCRTesseractPSM     = "3"
	imageOCRPDFHeader        = "%PDF"
	imageOCRPDFHeaderSize    = len(imageOCRPDFHeader)
	imageOCRTextOutputPerm   = 0o644
	imageOCRNormalizedPerm   = 0o600
	imageOCRWriteProbePrefix = ".fileforge-image-ocr-writecheck-*.tmp"
)

// imageOCRDetectCapabilities is a package-level hook so tests can inject the
// runtime surface without a real Tesseract installation. Image OCR shares the
// PDF OCR detection (the single source of truth for Tesseract languages) but
// never requires pdftoppm.
var imageOCRDetectCapabilities = func(ctx context.Context, runner engine.CommandRunner) engine.OCRCapabilities {
	return engine.DetectCapabilities(ctx, runner)
}

// ImageOCROptions describes one image OCR pass. Language defaults to the
// detected recommendation and DPI defaults to engine.DefaultOCRDPI.
type ImageOCROptions struct {
	Language string
	DPI      int
}

// ImageOCRResult reports what the recognizer actually produced. Width and
// Height are the EXIF-normalized pixel dimensions. Warnings never fail the run.
type ImageOCRResult struct {
	TextPath       string
	PDFPath        string
	TextCharacters int
	Width          int
	Height         int
	Warnings       []string
}

// ImageOCRError is a typed OCR failure with a stable code the tool maps to a
// canonical job error.
type ImageOCRError struct {
	Code    string
	Message string
	Cause   error
}

func (e *ImageOCRError) Error() string {
	if e == nil {
		return ""
	}
	if e.Cause == nil {
		return e.Message
	}

	return fmt.Sprintf("%s: %v", e.Message, e.Cause)
}

func (e *ImageOCRError) Unwrap() error {
	if e == nil {
		return nil
	}

	return e.Cause
}

// ImageOCRAvailable reports whether image OCR can run. Only Tesseract and at
// least one installed language matter: images are normalized with bimg, so the
// PDF pipeline's pdftoppm rasterizer must never gate this tool.
func ImageOCRAvailable(capabilities engine.OCRCapabilities) bool {
	return capabilities.Tesseract && len(capabilities.Languages) > 0
}

// ImageOCRUnavailableReason explains why image OCR cannot run, in the order the
// user has to fix it: missing binary, then missing language data.
func ImageOCRUnavailableReason(capabilities engine.OCRCapabilities) string {
	if !capabilities.Tesseract {
		return "Local image OCR is unavailable: install Tesseract."
	}
	if len(capabilities.Languages) == 0 {
		return "Local image OCR is unavailable: Tesseract language data is unavailable or could not be listed; install eng or spa."
	}

	return "Local image OCR is unavailable."
}

// ValidateImageOCRInput checks that inputPath is an existing regular image
// without decoding pixels. The extension list is the shared image input list.
func ValidateImageOCRInput(inputPath string) *ImageOCRError {
	trimmed := strings.TrimSpace(inputPath)
	if trimmed == "" {
		return &ImageOCRError{Code: ImageOCRCodeInputRequired, Message: "inputPath is required"}
	}

	info, err := os.Stat(trimmed)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &ImageOCRError{
				Code:    ImageOCRCodeInputNotFound,
				Message: fmt.Sprintf("%s was not found.", filepath.Base(trimmed)),
				Cause:   err,
			}
		}

		return &ImageOCRError{
			Code:    ImageOCRCodeInputNotFound,
			Message: fmt.Sprintf("%s cannot be read.", filepath.Base(trimmed)),
			Cause:   err,
		}
	}
	if info.IsDir() {
		return &ImageOCRError{
			Code:    ImageOCRCodeInputUnsupported,
			Message: fmt.Sprintf("%s is a folder, not an image.", filepath.Base(trimmed)),
		}
	}

	extension := strings.ToLower(strings.TrimPrefix(filepath.Ext(trimmed), "."))
	if !compressSupportsInputExtension(extension) {
		return &ImageOCRError{
			Code:    ImageOCRCodeInputUnsupported,
			Message: fmt.Sprintf("%s is not a supported image. Use %s.", filepath.Base(trimmed), strings.Join(imageInputExtensions, ", ")),
		}
	}

	return nil
}

// ValidateImageOCRDestination checks that dir exists, is a directory and
// accepts writes, so an auto output can be promised safely.
func ValidateImageOCRDestination(dir string) *ImageOCRError {
	trimmed := strings.TrimSpace(dir)
	if trimmed == "" {
		return &ImageOCRError{Code: ImageOCRCodeOutputDirInvalid, Message: "output directory is required"}
	}

	return validateImageOCROutputDir(filepath.Clean(trimmed))
}

// RecognizeImageWithCapabilities normalizes one image and runs Tesseract. The
// caller detects the runtime once per job and passes it here, so a long batch
// never reruns `tesseract --list-langs` for every image.
func RecognizeImageWithCapabilities(ctx context.Context, runner engine.CommandRunner, capabilities engine.OCRCapabilities, inputPath, textPath, pdfPath string, opts ImageOCROptions) (ImageOCRResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if checkErr := imageOCRContextError(ctx); checkErr != nil {
		return ImageOCRResult{}, checkErr
	}

	if !ImageOCRAvailable(capabilities) {
		return ImageOCRResult{}, &ImageOCRError{
			Code:    ImageOCRCodeRuntimeUnavailable,
			Message: ImageOCRUnavailableReason(capabilities),
		}
	}

	inputPath = strings.TrimSpace(inputPath)
	textPath = strings.TrimSpace(textPath)
	pdfPath = strings.TrimSpace(pdfPath)

	if validationErr := validateImageOCRRequest(inputPath, textPath, pdfPath); validationErr != nil {
		return ImageOCRResult{}, validationErr
	}

	language := strings.TrimSpace(opts.Language)
	if language == "" {
		language = strings.TrimSpace(capabilities.RecommendedLanguage)
	}
	if language == "" {
		return ImageOCRResult{}, &ImageOCRError{
			Code:    ImageOCRCodeLanguageUnavailable,
			Message: fmt.Sprintf("Choose a Tesseract language. Installed languages: %s.", strings.Join(capabilities.Languages, ", ")),
		}
	}
	if !capabilities.HasLanguage(language) {
		return ImageOCRResult{}, &ImageOCRError{
			Code: ImageOCRCodeLanguageUnavailable,
			Message: fmt.Sprintf(
				"Tesseract language '%s' is not installed. Installed languages: %s.",
				language,
				strings.Join(capabilities.Languages, ", "),
			),
		}
	}

	dpi := opts.DPI
	if dpi == 0 {
		dpi = engine.DefaultOCRDPI
	}
	if dpi < engine.MinOCRDPI || dpi > engine.MaxOCRDPI {
		return ImageOCRResult{}, &ImageOCRError{
			Code:    ImageOCRCodeDPIInvalid,
			Message: fmt.Sprintf("dpi must be between %d and %d", engine.MinOCRDPI, engine.MaxOCRDPI),
		}
	}

	tempDir, err := os.MkdirTemp("", imageOCRTempDirPattern)
	if err != nil {
		return ImageOCRResult{}, &ImageOCRError{
			Code:    ImageOCRCodeNormalizeFailed,
			Message: "Could not create the OCR workspace.",
			Cause:   err,
		}
	}
	defer func() {
		_ = os.RemoveAll(tempDir)
	}()

	// Tesseract ignores the EXIF orientation tag, so the pixels must be
	// physically rotated before recognition. A PNG round trip with
	// StripMetadata also drops the stale tag and flattens every input format.
	normalized, normalizeErr := imageOCRNormalizeToPNG(inputPath)
	if normalizeErr != nil {
		return ImageOCRResult{}, &ImageOCRError{
			Code:    ImageOCRCodeNormalizeFailed,
			Message: "The image could not be prepared for OCR.",
			Cause:   normalizeErr,
		}
	}

	normalizedPath := filepath.Join(tempDir, imageOCRNormalizedName)
	if writeErr := os.WriteFile(normalizedPath, normalized, imageOCRNormalizedPerm); writeErr != nil {
		return ImageOCRResult{}, &ImageOCRError{
			Code:    ImageOCRCodeNormalizeFailed,
			Message: "The normalized image could not be written.",
			Cause:   writeErr,
		}
	}

	size, sizeErr := bimg.NewImage(normalized).Size()
	if sizeErr != nil {
		return ImageOCRResult{}, &ImageOCRError{
			Code:    ImageOCRCodeNormalizeFailed,
			Message: "The normalized image size could not be read.",
			Cause:   sizeErr,
		}
	}

	if checkErr := imageOCRContextError(ctx); checkErr != nil {
		return ImageOCRResult{}, checkErr
	}

	outputBase := filepath.Join(tempDir, imageOCRRecognizeBase)
	if _, runErr := resolveImageOCRRunner(runner).Output(ctx, "tesseract", []string{
		normalizedPath,
		outputBase,
		"-l", language,
		"--dpi", strconv.Itoa(dpi),
		"--psm", imageOCRTesseractPSM,
		"txt", "pdf",
	}); runErr != nil {
		if checkErr := imageOCRContextError(ctx); checkErr != nil {
			return ImageOCRResult{}, checkErr
		}
		if imageOCRIsCancellation(ctx, runErr) {
			return ImageOCRResult{}, imageOCRCancelledError(context.Canceled)
		}

		return ImageOCRResult{}, &ImageOCRError{
			Code:    ImageOCRCodeRecognizeFailed,
			Message: "Tesseract could not recognize text in the image.",
			Cause:   runErr,
		}
	}
	if checkErr := imageOCRContextError(ctx); checkErr != nil {
		return ImageOCRResult{}, checkErr
	}

	recognizedTextPath := outputBase + ".txt"
	recognizedText, readErr := os.ReadFile(recognizedTextPath)
	if readErr != nil {
		return ImageOCRResult{}, &ImageOCRError{
			Code:    ImageOCRCodeRecognizeFailed,
			Message: "Tesseract did not produce the recognized text.",
			Cause:   readErr,
		}
	}

	recognizedPDFPath := outputBase + ".pdf"
	if pdfPath != "" {
		// A missing PDF config or pdf.ttf can make Tesseract exit successfully
		// while writing nothing. Validate before publishing either output.
		if pdfErr := imageOCRValidatePDF(recognizedPDFPath); pdfErr != nil {
			return ImageOCRResult{}, &ImageOCRError{
				Code:    ImageOCRCodeVerifyFailed,
				Message: "Tesseract did not produce a valid searchable PDF.",
				Cause:   pdfErr,
			}
		}
	}

	characters := engine.CountTextCharacters(string(recognizedText))
	warnings := make([]string, 0, 1)
	if characters == 0 {
		warnings = append(warnings, ImageOCRWarningNoText)
	}

	if checkErr := imageOCRContextError(ctx); checkErr != nil {
		return ImageOCRResult{}, checkErr
	}

	if publishErr := publishImageOCRText(textPath, string(recognizedText)); publishErr != nil {
		return ImageOCRResult{}, &ImageOCRError{
			Code:    ImageOCRCodePublishFailed,
			Message: "The recognized text could not be saved.",
			Cause:   publishErr,
		}
	}
	if pdfPath != "" {
		// A cancel can land after the TXT is published but before the PDF is.
		// Roll the TXT back so a cancelled run never leaves a partial result.
		if checkErr := imageOCRContextError(ctx); checkErr != nil {
			_ = os.Remove(textPath)
			return ImageOCRResult{}, checkErr
		}
		if publishErr := publishImageOCRPDF(recognizedPDFPath, pdfPath); publishErr != nil {
			// Publish the pair or nothing: a failed PDF must not leave a
			// seemingly complete result behind.
			_ = os.Remove(textPath)
			return ImageOCRResult{}, &ImageOCRError{
				Code:    ImageOCRCodePublishFailed,
				Message: "The searchable PDF could not be saved.",
				Cause:   publishErr,
			}
		}
	}

	return ImageOCRResult{
		TextPath:       textPath,
		PDFPath:        pdfPath,
		TextCharacters: characters,
		Width:          size.Width,
		Height:         size.Height,
		Warnings:       warnings,
	}, nil
}

// imageOCRNormalizeToPNG physically applies EXIF orientation, decodes every
// supported input through libvips and returns PNG bytes without metadata.
func imageOCRNormalizeToPNG(inputPath string) ([]byte, error) {
	input, readErr := os.ReadFile(inputPath)
	if readErr != nil {
		return nil, fmt.Errorf("read input: %w", readErr)
	}

	normalized, normalizeErr := bimg.NewImage(input).Process(bimg.Options{
		Type:          bimg.PNG,
		StripMetadata: true,
	})
	if normalizeErr != nil {
		return nil, fmt.Errorf("normalize to png: %w", normalizeErr)
	}

	return normalized, nil
}

func validateImageOCRRequest(inputPath, textPath, pdfPath string) *ImageOCRError {
	if inputErr := ValidateImageOCRInput(inputPath); inputErr != nil {
		return inputErr
	}

	if textPath == "" {
		return &ImageOCRError{Code: ImageOCRCodeOutputInvalid, Message: "The text output path is required."}
	}
	if !strings.EqualFold(filepath.Ext(textPath), ".txt") {
		return &ImageOCRError{Code: ImageOCRCodeOutputInvalid, Message: "The text output must use a .txt extension."}
	}
	if pdfPath != "" && !strings.EqualFold(filepath.Ext(pdfPath), ".pdf") {
		return &ImageOCRError{Code: ImageOCRCodeOutputInvalid, Message: "The PDF output must use a .pdf extension."}
	}

	if fsx.SameFile(inputPath, textPath) || (pdfPath != "" && fsx.SameFile(inputPath, pdfPath)) {
		return &ImageOCRError{Code: ImageOCRCodeOutputInvalid, Message: "The OCR outputs cannot overwrite the input image."}
	}
	if pdfPath != "" && fsx.SameFile(textPath, pdfPath) {
		return &ImageOCRError{Code: ImageOCRCodeOutputInvalid, Message: "The text and PDF outputs must differ."}
	}

	if existsErr := ensureImageOCROutputAbsent(textPath); existsErr != nil {
		return existsErr
	}
	if pdfPath != "" {
		if existsErr := ensureImageOCROutputAbsent(pdfPath); existsErr != nil {
			return existsErr
		}
	}

	if dirErr := validateImageOCROutputDir(filepath.Dir(textPath)); dirErr != nil {
		return dirErr
	}
	if pdfPath != "" {
		if dirErr := validateImageOCROutputDir(filepath.Dir(pdfPath)); dirErr != nil {
			return dirErr
		}
	}

	return nil
}

func ensureImageOCROutputAbsent(outputPath string) *ImageOCRError {
	_, statErr := os.Stat(outputPath)
	if statErr == nil {
		return &ImageOCRError{
			Code:    ImageOCRCodeOutputExists,
			Message: fmt.Sprintf("%s already exists.", filepath.Base(outputPath)),
		}
	}
	if errors.Is(statErr, os.ErrNotExist) {
		return nil
	}

	return &ImageOCRError{
		Code:    ImageOCRCodeOutputInvalid,
		Message: fmt.Sprintf("%s cannot be accessed.", filepath.Base(outputPath)),
		Cause:   statErr,
	}
}

func validateImageOCROutputDir(dir string) *ImageOCRError {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = "."
	}

	info, statErr := os.Stat(dir)
	if statErr != nil {
		if errors.Is(statErr, os.ErrNotExist) {
			return &ImageOCRError{
				Code:    ImageOCRCodeOutputDirInvalid,
				Message: fmt.Sprintf("The output folder does not exist: %s", dir),
				Cause:   statErr,
			}
		}

		return &ImageOCRError{
			Code:    ImageOCRCodeOutputDirInvalid,
			Message: fmt.Sprintf("The output folder cannot be used: %s", dir),
			Cause:   statErr,
		}
	}
	if !info.IsDir() {
		return &ImageOCRError{
			Code:    ImageOCRCodeOutputDirInvalid,
			Message: fmt.Sprintf("%s is not a folder.", dir),
		}
	}

	probe, probeErr := os.CreateTemp(dir, imageOCRWriteProbePrefix)
	if probeErr != nil {
		return &ImageOCRError{
			Code:    ImageOCRCodeOutputDirInvalid,
			Message: fmt.Sprintf("The output folder is not writable: %s", dir),
			Cause:   probeErr,
		}
	}
	probePath := probe.Name()
	_ = probe.Close()
	_ = os.Remove(probePath)

	return nil
}

// imageOCRValidatePDF checks the Tesseract PDF before publication: it must
// exist, be non-empty and start with the %PDF signature.
func imageOCRValidatePDF(pdfPath string) error {
	info, statErr := os.Stat(pdfPath)
	if statErr != nil {
		return statErr
	}
	if !info.Mode().IsRegular() {
		return fmt.Errorf("not a regular file: %s", filepath.Base(pdfPath))
	}
	if info.Size() == 0 {
		return fmt.Errorf("empty file: %s", filepath.Base(pdfPath))
	}

	file, openErr := os.Open(pdfPath)
	if openErr != nil {
		return openErr
	}
	defer file.Close()

	header := make([]byte, imageOCRPDFHeaderSize)
	if _, readErr := io.ReadFull(file, header); readErr != nil {
		return readErr
	}
	if !bytes.Equal(header, []byte(imageOCRPDFHeader)) {
		return fmt.Errorf("missing %%PDF header: %s", filepath.Base(pdfPath))
	}

	return nil
}

// publishImageOCRText writes the recognized text atomically: a temp file in the
// destination folder, chmod 0644, refuse an existing destination, then rename.
func publishImageOCRText(outputPath, content string) error {
	tempFile, err := os.CreateTemp(filepath.Dir(outputPath), imageOCRTempFilePattern)
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	if _, writeErr := tempFile.WriteString(content); writeErr != nil {
		_ = tempFile.Close()
		return writeErr
	}
	if closeErr := tempFile.Close(); closeErr != nil {
		return closeErr
	}

	return imageOCRCommitTempFile(tempPath, outputPath)
}

// publishImageOCRPDF copies the Tesseract PDF into a temp file beside the
// destination and renames it, so a partial file is never visible.
func publishImageOCRPDF(sourcePath, outputPath string) error {
	tempFile, err := os.CreateTemp(filepath.Dir(outputPath), imageOCRTempFilePattern)
	if err != nil {
		return err
	}
	tempPath := tempFile.Name()
	defer func() {
		_ = os.Remove(tempPath)
	}()

	source, openErr := os.Open(sourcePath)
	if openErr != nil {
		_ = tempFile.Close()
		return openErr
	}

	_, copyErr := io.Copy(tempFile, source)
	closeSourceErr := source.Close()
	closeTempErr := tempFile.Close()
	if copyErr != nil {
		return copyErr
	}
	if closeSourceErr != nil {
		return closeSourceErr
	}
	if closeTempErr != nil {
		return closeTempErr
	}

	return imageOCRCommitTempFile(tempPath, outputPath)
}

func imageOCRCommitTempFile(tempPath, outputPath string) error {
	// os.CreateTemp uses 0600; publish a readable file like the other tools.
	if chmodErr := os.Chmod(tempPath, imageOCRTextOutputPerm); chmodErr != nil {
		return chmodErr
	}

	if _, statErr := os.Stat(outputPath); statErr == nil {
		return fmt.Errorf("output already exists: %s", outputPath)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return statErr
	}

	return os.Rename(tempPath, outputPath)
}

func resolveImageOCRRunner(runner engine.CommandRunner) engine.CommandRunner {
	if runner == nil {
		return &engine.ExecCommandRunner{}
	}

	return runner
}

func imageOCRContextError(ctx context.Context) *ImageOCRError {
	if ctx == nil || ctx.Err() == nil {
		return nil
	}

	return imageOCRCancelledError(ctx.Err())
}

func imageOCRCancelledError(cause error) *ImageOCRError {
	if cause == nil {
		cause = context.Canceled
	}

	return &ImageOCRError{
		Code:    ImageOCRCodeCancelled,
		Message: "Image OCR was cancelled.",
		Cause:   cause,
	}
}

func imageOCRIsCancellation(ctx context.Context, err error) bool {
	if ctx != nil && ctx.Err() != nil {
		return true
	}

	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}
