// Package capabilities detects the external tools the local FileForge tools can
// use. Detection is best-effort: a missing optional tool is reported with
// guidance, never as an app failure, and nothing is installed or downloaded.
package capabilities

import (
	"context"
	"os/exec"
	"strings"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
)

// Options injects the binary lookup and the OCR capability provider so tests
// can simulate a machine with a specific toolchain. A nil LookPath uses
// exec.LookPath; a nil OCR provider reports no language data, which keeps tests
// independent from the machine. Detect wires the real PDF OCR detector.
type Options struct {
	LookPath func(string) (string, error)
	OCR      func(context.Context) models.OCRCapabilitiesResponse
}

// Detect probes the real machine: binaries through exec.LookPath and OCR
// languages through the PDF OCR detector.
func Detect(ctx context.Context) models.LocalCapabilitiesResponse {
	return DetectWith(ctx, Options{OCR: pdf.OCRCapabilities})
}

// DetectWith probes the requested runtime with injectable dependencies.
func DetectWith(ctx context.Context, opts Options) models.LocalCapabilitiesResponse {
	if ctx == nil {
		ctx = context.Background()
	}

	lookPath := opts.LookPath
	if lookPath == nil {
		lookPath = exec.LookPath
	}
	ocr := opts.OCR
	if ocr == nil {
		ocr = func(context.Context) models.OCRCapabilitiesResponse {
			return models.OCRCapabilitiesResponse{}
		}
	}

	ocrCapabilities := ocr(ctx)
	languages := ocrCapabilities.Languages
	if languages == nil {
		languages = []string{}
	}

	binaryAvailable := func(name string) bool {
		_, err := lookPath(name)

		return err == nil
	}

	pdftoppm := binaryAvailable("pdftoppm")
	pdftotext := binaryAvailable("pdftotext")
	gs := binaryAvailable("gs")
	ffmpeg := binaryAvailable("ffmpeg")
	ffprobe := binaryAvailable("ffprobe")

	tesseract := binaryAvailable("tesseract")
	tesseractDetail := "Not installed"
	tesseractSuggestion := ""
	tesseractAvailable := false
	switch {
	case !tesseract:
		tesseractSuggestion = "Install tesseract and at least one language pack (eng, spa, ...)."
	case len(languages) == 0:
		tesseractDetail = "Installed, but no language data was found"
		tesseractSuggestion = "Install tesseract and at least one language pack (eng, spa, ...)."
	default:
		tesseractAvailable = true
		tesseractDetail = "Languages: " + strings.ToUpper(strings.Join(languages, ", "))
	}

	checks := []models.LocalCapability{
		{
			ID:         "pdftoppm",
			Label:      "pdftoppm",
			Available:  pdftoppm,
			Required:   true,
			Detail:     "OCR page rasterizer",
			Suggestion: suggestion(pdftoppm, "Install poppler-utils (pdftoppm) for local OCR."),
		},
		{
			ID:         "tesseract",
			Label:      "tesseract",
			Available:  tesseractAvailable,
			Required:   true,
			Detail:     tesseractDetail,
			Suggestion: tesseractSuggestion,
		},
		{
			ID:         "pdftotext",
			Label:      "pdftotext",
			Available:  pdftotext,
			Required:   false,
			Detail:     "OCR text-layer verification",
			Suggestion: suggestion(pdftotext, "Install poppler-utils for text verification; the OCR sidecar is used otherwise."),
		},
		{
			ID:         "gs",
			Label:      "gs",
			Available:  gs,
			Required:   false,
			Detail:     "PDF compression for target sizes",
			Suggestion: suggestion(gs, "Install Ghostscript to reach PDF target sizes."),
		},
		{
			ID:         "ffmpeg",
			Label:      "ffmpeg",
			Available:  ffmpeg,
			Required:   true,
			Detail:     "Video conversion, trimming and merging",
			Suggestion: suggestion(ffmpeg, "Install ffmpeg for the video tools."),
		},
		{
			ID:         "ffprobe",
			Label:      "ffprobe",
			Available:  ffprobe,
			Required:   true,
			Detail:     "Video metadata and durations",
			Suggestion: suggestion(ffprobe, "Install ffprobe (usually with ffmpeg)."),
		},
	}

	return models.LocalCapabilitiesResponse{
		Success:                true,
		Message:                "Local capabilities checked.",
		Checks:                 checks,
		OCRLanguages:           languages,
		RecommendedOCRLanguage: ocrCapabilities.RecommendedLanguage,
	}
}

// suggestion returns guidance only when a tool is missing, so available rows
// never carry install instructions.
func suggestion(available bool, text string) string {
	if available {
		return ""
	}

	return text
}
