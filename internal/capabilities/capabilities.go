// Package capabilities detects the external tools the local FileForge tools can
// use. Detection is best-effort: a missing optional tool is reported with
// guidance, never as an app failure, and nothing is installed or downloaded.
package capabilities

import (
	"context"
	"fmt"
	"os/exec"
	"strings"

	"github.com/h2non/bimg"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/toolchain"
	"fileforge-desktop/internal/video/engine"
)

// Options injects the binary lookup, the OCR capability provider, the managed
// toolchain snapshot and the libvips build so tests can simulate a machine with
// a specific toolchain. A nil LookPath uses exec.LookPath; a nil OCR provider
// reports no language data, which keeps tests independent from the machine. A
// nil Managed runs the real toolchain detectors and a nil Vips reads bimg.
type Options struct {
	LookPath func(string) (string, error)
	OCR      func(context.Context) models.OCRCapabilitiesResponse
	Managed  func() ManagedRuntime
	Vips     func() (version string, formats int)
}

// ManagedRuntime is the managed-toolchain snapshot rendered by the capability
// panel. It is a value, not a live service, so tests can inject a machine state.
type ManagedRuntime struct {
	DataDir            string
	DataDirError       error
	PDF2DOCX           bool
	RembgReady         bool
	RembgReason        string
	RembgModelReady    bool
	RembgModelReason   string
	UpscaleReady       bool
	UpscaleReason      string
	UpscaleModelReady  bool
	UpscaleModelReason string
	Whisper            engine.TranscriptionCapabilities
}

// Detect probes the real machine: binaries through exec.LookPath, OCR
// languages through the PDF OCR detector, managed packs through the toolchain
// layout and libvips through the linked bimg build.
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
	managed := opts.Managed
	if managed == nil {
		managed = detectManagedRuntime
	}
	vips := opts.Vips
	if vips == nil {
		vips = linkedVipsRuntime
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
	_, gs := toolchain.ResolveGhostscript(lookPath)
	ffmpeg := binaryAvailable("ffmpeg")
	ffprobe := binaryAvailable("ffprobe")
	cwebp := binaryAvailable("cwebp")

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

	vipsVersion, vipsFormats := vips()
	vipsAvailable := strings.TrimSpace(vipsVersion) != "" && vipsFormats > 0
	vipsDetail := "Not available"
	if vipsAvailable {
		vipsDetail = fmt.Sprintf("libvips %s, %d formats", strings.TrimSpace(vipsVersion), vipsFormats)
	}

	managedState := managed()

	pdf2docxAvailable := managedState.PDF2DOCX
	pdf2docxDetail := "Not installed"
	pdf2docxSuggestion := ""
	if pdf2docxAvailable {
		pdf2docxDetail = "Managed pdf2docx environment"
	} else {
		pdf2docxSuggestion = pdf.PDFToDOCXUnavailableReason
	}

	rembgAvailable := managedState.RembgReady && managedState.RembgModelReady
	rembgDetail := "Not installed"
	rembgSuggestion := ""
	switch {
	case rembgAvailable:
		rembgDetail = "Background removal engine and model"
	case !managedState.RembgReady:
		rembgSuggestion = managedReason(managedState.RembgReason,
			"Open Settings and run Install / repair to install the background removal pack.")
	case !managedState.RembgModelReady:
		rembgDetail = "Engine installed, but the model is not ready"
		rembgSuggestion = managedReason(managedState.RembgModelReason,
			"Open Settings and run Install / repair to download the background removal model.")
	}

	upscaleAvailable := managedState.UpscaleReady && managedState.UpscaleModelReady
	upscaleDetail := "Not installed"
	upscaleSuggestion := ""
	switch {
	case upscaleAvailable:
		upscaleDetail = "Upscale engine and general model"
	case !managedState.UpscaleReady:
		upscaleSuggestion = managedReason(managedState.UpscaleReason,
			"Open Settings and run Install / repair to install the upscale pack.")
	case !managedState.UpscaleModelReady:
		upscaleDetail = "Engine installed, but the model is not ready"
		upscaleSuggestion = managedReason(managedState.UpscaleModelReason,
			"Open Settings and run Install / repair to download the upscale model.")
	}

	whisper := managedState.Whisper
	whisperAvailable := whisper.Available()
	whisperDetail := "Not installed"
	whisperSuggestion := "Install whisper.cpp and a GGML model, or run Install / repair."
	switch {
	case whisperAvailable:
		whisperDetail = "Model: " + whisper.ModelName
		whisperSuggestion = ""
	case whisper.WhisperPresent && !whisper.ModelPresent:
		whisperDetail = "Installed, but no model was found"
		whisperSuggestion = "Download a GGML model in Settings or run Install / repair."
	case !whisper.WhisperPresent && whisper.ModelPresent:
		whisperDetail = "Model found, but whisper-cli was not found"
		whisperSuggestion = "Install whisper.cpp or run Install / repair."
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
			Suggestion: suggestion(gs, "Install Ghostscript (gswin64c on Windows) to reach PDF target sizes."),
		},
		{
			ID:         "ffmpeg",
			Label:      "ffmpeg",
			Available:  ffmpeg,
			Required:   true,
			Detail:     "Video and audio conversion, trimming, merging and normalization",
			Suggestion: suggestion(ffmpeg, "Install ffmpeg for the video and audio tools."),
		},
		{
			ID:         "ffprobe",
			Label:      "ffprobe",
			Available:  ffprobe,
			Required:   true,
			Detail:     "Video and audio metadata, durations and output verification",
			Suggestion: suggestion(ffprobe, "Install ffprobe (usually with ffmpeg)."),
		},
		{
			ID:         "libvips",
			Label:      "libvips",
			Available:  vipsAvailable,
			Required:   true,
			Detail:     vipsDetail,
			Suggestion: suggestion(vipsAvailable, "Rebuild FileForge against libvips (8.5 or newer) with its image loaders; every image tool needs it."),
		},
		{
			ID:         "cwebp",
			Label:      "cwebp",
			Available:  cwebp,
			Required:   false,
			Detail:     "Optional WebP preview encoder",
			Suggestion: suggestion(cwebp, "Install WebP tools (cwebp) for WebP previews; PNG previews are used otherwise."),
		},
		{
			ID:         "pdf2docx",
			Label:      "pdf2docx",
			Available:  pdf2docxAvailable,
			Required:   false,
			Detail:     pdf2docxDetail,
			Suggestion: pdf2docxSuggestion,
		},
		{
			ID:         "rembg",
			Label:      "Background removal pack",
			Available:  rembgAvailable,
			Required:   false,
			Detail:     rembgDetail,
			Suggestion: rembgSuggestion,
		},
		{
			ID:         "upscale",
			Label:      "Local upscale pack",
			Available:  upscaleAvailable,
			Required:   false,
			Detail:     upscaleDetail,
			Suggestion: upscaleSuggestion,
		},
		{
			ID:         "whisper",
			Label:      "Whisper model",
			Available:  whisperAvailable,
			Required:   true,
			Detail:     whisperDetail,
			Suggestion: whisperSuggestion,
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

// detectManagedRuntime reads the real managed toolchain using the same detectors
// the tools use, so the panel never drifts from the actual runtime.
func detectManagedRuntime() ManagedRuntime {
	state := ManagedRuntime{}

	dataDir, err := toolchain.DataDir()
	if err != nil {
		state.DataDirError = err
		return state
	}
	state.DataDir = dataDir

	_, state.PDF2DOCX = toolchain.ManagedPDF2DOCXPythonPath(dataDir)
	state.RembgReady, state.RembgReason = toolchain.ManagedRembgRuntimeState(dataDir)
	state.RembgModelReady, state.RembgModelReason = toolchain.ManagedRembgModelState(dataDir)
	state.UpscaleReady, state.UpscaleReason = toolchain.ManagedUpscaleRuntimeState(dataDir)
	state.UpscaleModelReady, state.UpscaleModelReason = toolchain.ManagedUpscaleModelState(dataDir)
	state.Whisper = engine.DetectTranscriptionCapabilities("", "")

	return state
}

// linkedVipsRuntime reports the libvips version bimg was built against and how
// many image formats it exposes for loading.
func linkedVipsRuntime() (string, int) {
	formats := 0
	for imageType := range bimg.ImageTypes {
		if bimg.VipsIsTypeSupported(imageType) {
			formats++
		}
	}

	return bimg.VipsVersion, formats
}

// managedReason keeps the detector guidance when it exists and falls back to
// the panel copy otherwise.
func managedReason(reason, fallback string) string {
	if trimmed := strings.TrimSpace(reason); trimmed != "" {
		return trimmed
	}

	return fallback
}

// suggestion returns guidance only when a tool is missing, so available rows
// never carry install instructions.
func suggestion(available bool, text string) string {
	if available {
		return ""
	}

	return text
}
