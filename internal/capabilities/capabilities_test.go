package capabilities

import (
	"context"
	"errors"
	"testing"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf"
	"fileforge-desktop/internal/video/engine"
)

var allChecks = []string{
	"pdftoppm", "tesseract", "pdftotext", "gs", "ffmpeg", "ffprobe",
	"libvips", "cwebp", "pdf2docx", "rembg", "whisper",
}

var allBinaries = []string{
	"pdftoppm", "tesseract", "pdftotext", "gs", "ffmpeg", "ffprobe", "cwebp",
}

func fakeLookPath(available ...string) func(string) (string, error) {
	found := make(map[string]struct{}, len(available))
	for _, name := range available {
		found[name] = struct{}{}
	}

	return func(name string) (string, error) {
		if _, ok := found[name]; ok {
			return "/usr/bin/" + name, nil
		}

		return "", errors.New("executable file not found in $PATH")
	}
}

func fakeOCR(response models.OCRCapabilitiesResponse) func(context.Context) models.OCRCapabilitiesResponse {
	return func(context.Context) models.OCRCapabilitiesResponse {
		return response
	}
}

func fakeVips(version string, formats int) func() (string, int) {
	return func() (string, int) {
		return version, formats
	}
}

// managedReadyRuntime is a fully installed managed toolchain: pdf2docx
// environment, background removal pack and Whisper runtime/model.
func managedReadyRuntime() ManagedRuntime {
	return ManagedRuntime{
		DataDir:          "/data/fileforge",
		PDF2DOCX:         true,
		RembgReady:       true,
		RembgReason:      "Local background removal engine",
		RembgModelReady:  true,
		RembgModelReason: "Local background removal model",
		Whisper: engine.TranscriptionCapabilities{
			WhisperPath:    "/data/fileforge/toolchain/whisper/whisper-cli",
			WhisperPresent: true,
			ModelPath:      "/data/fileforge/toolchain/models/ggml-medium.bin",
			ModelPresent:   true,
			ModelName:      "ggml-medium.bin",
		},
	}
}

func fakeManaged(state ManagedRuntime) func() ManagedRuntime {
	return func() ManagedRuntime {
		return state
	}
}

// baseOptions builds deterministic machine options: every managed pack ready and
// a libvips build that exposes eight formats.
func baseOptions(lookPath func(string) (string, error), ocr func(context.Context) models.OCRCapabilitiesResponse) Options {
	return Options{
		LookPath: lookPath,
		OCR:      ocr,
		Managed:  fakeManaged(managedReadyRuntime()),
		Vips:     fakeVips("8.16.0", 8),
	}
}

func checksByID(t *testing.T, response models.LocalCapabilitiesResponse) map[string]models.LocalCapability {
	t.Helper()

	byID := make(map[string]models.LocalCapability, len(response.Checks))
	for _, check := range response.Checks {
		if _, exists := byID[check.ID]; exists {
			t.Fatalf("duplicate capability id %q", check.ID)
		}
		byID[check.ID] = check
	}

	return byID
}

func TestDetectWithAllToolsAvailable(t *testing.T) {
	response := DetectWith(context.Background(), baseOptions(
		fakeLookPath(allBinaries...),
		fakeOCR(models.OCRCapabilitiesResponse{
			Success:             true,
			Available:           true,
			Tesseract:           true,
			Languages:           []string{"afr", "eng"},
			RecommendedLanguage: "eng",
		}),
	))

	if !response.Success || response.Message != "Local capabilities checked." {
		t.Fatalf("unexpected response envelope: %+v", response)
	}
	if len(response.Checks) != len(allChecks) {
		t.Fatalf("expected %d checks, got %d", len(allChecks), len(response.Checks))
	}

	for index, id := range allChecks {
		if response.Checks[index].ID != id {
			t.Fatalf("check %d = %q, want %q", index, response.Checks[index].ID, id)
		}
	}

	byID := checksByID(t, response)
	required := map[string]bool{
		"pdftoppm":  true,
		"tesseract": true,
		"pdftotext": false,
		"gs":        false,
		"ffmpeg":    true,
		"ffprobe":   true,
		"libvips":   true,
		"cwebp":     false,
		"pdf2docx":  false,
		"rembg":     false,
		"whisper":   true,
	}
	for id, wantRequired := range required {
		check, found := byID[id]
		if !found {
			t.Fatalf("missing check %q", id)
		}
		if check.Label == "" {
			t.Fatalf("check %q has an empty label", id)
		}
		if !check.Available {
			t.Fatalf("check %q should be available", id)
		}
		if check.Required != wantRequired {
			t.Fatalf("check %q required = %t, want %t", id, check.Required, wantRequired)
		}
		if check.Suggestion != "" {
			t.Fatalf("available check %q must not suggest an install, got %q", id, check.Suggestion)
		}
	}

	if got := byID["tesseract"].Detail; got != "Languages: AFR, ENG" {
		t.Fatalf("unexpected tesseract detail: %q", got)
	}
	if got := byID["libvips"].Detail; got != "libvips 8.16.0, 8 formats" {
		t.Fatalf("unexpected libvips detail: %q", got)
	}
	if got := byID["pdf2docx"].Detail; got != "Managed pdf2docx environment" {
		t.Fatalf("unexpected pdf2docx detail: %q", got)
	}
	if got := byID["rembg"].Detail; got != "Background removal engine and model" {
		t.Fatalf("unexpected rembg detail: %q", got)
	}
	if got := byID["whisper"].Detail; got != "Model: ggml-medium.bin" {
		t.Fatalf("unexpected whisper detail: %q", got)
	}
	if byID["rembg"].Label != "Background removal pack" {
		t.Fatalf("unexpected rembg label: %q", byID["rembg"].Label)
	}
	if byID["whisper"].Label != "Whisper model" {
		t.Fatalf("unexpected whisper label: %q", byID["whisper"].Label)
	}
	if len(response.OCRLanguages) != 2 || response.OCRLanguages[0] != "afr" || response.OCRLanguages[1] != "eng" {
		t.Fatalf("unexpected OCR languages: %+v", response.OCRLanguages)
	}
	if response.RecommendedOCRLanguage != "eng" {
		t.Fatalf("unexpected recommended language: %q", response.RecommendedOCRLanguage)
	}
}

func TestDetectWithMissingOptionalAndRequiredTools(t *testing.T) {
	response := DetectWith(context.Background(), baseOptions(
		fakeLookPath("pdftoppm", "tesseract", "pdftotext", "ffprobe"),
		fakeOCR(models.OCRCapabilitiesResponse{
			Tesseract:           true,
			Languages:           []string{"eng"},
			RecommendedLanguage: "eng",
		}),
	))

	byID := checksByID(t, response)

	gsCheck := byID["gs"]
	if gsCheck.Available || gsCheck.Required {
		t.Fatalf("gs must be an unavailable optional tool: %+v", gsCheck)
	}
	if gsCheck.Suggestion != "Install Ghostscript to reach PDF target sizes." {
		t.Fatalf("unexpected gs suggestion: %q", gsCheck.Suggestion)
	}
	if gsCheck.Detail != "PDF compression for target sizes" {
		t.Fatalf("unexpected gs detail: %q", gsCheck.Detail)
	}

	ffmpegCheck := byID["ffmpeg"]
	if ffmpegCheck.Available || !ffmpegCheck.Required {
		t.Fatalf("ffmpeg must be an unavailable required tool: %+v", ffmpegCheck)
	}
	if ffmpegCheck.Suggestion != "Install ffmpeg for the video and audio tools." {
		t.Fatalf("unexpected ffmpeg suggestion: %q", ffmpegCheck.Suggestion)
	}

	ffprobeCheck := byID["ffprobe"]
	if !ffprobeCheck.Available || !ffprobeCheck.Required {
		t.Fatalf("ffprobe should be available and required: %+v", ffprobeCheck)
	}
}

func TestDetectCwebpIsOptionalAndDegrades(t *testing.T) {
	response := DetectWith(context.Background(), baseOptions(
		fakeLookPath("pdftoppm", "tesseract", "pdftotext", "gs", "ffmpeg", "ffprobe"),
		fakeOCR(models.OCRCapabilitiesResponse{Tesseract: true, Languages: []string{"eng"}}),
	))

	check := checksByID(t, response)["cwebp"]
	if check.Available {
		t.Fatal("cwebp must be unavailable when the binary is missing")
	}
	if check.Required {
		t.Fatal("cwebp must stay optional because previews fall back to PNG")
	}
	if check.Detail != "Optional WebP preview encoder" {
		t.Fatalf("unexpected cwebp detail: %q", check.Detail)
	}
	if check.Suggestion != "Install WebP tools (cwebp) for WebP previews; PNG previews are used otherwise." {
		t.Fatalf("unexpected cwebp suggestion: %q", check.Suggestion)
	}
}

func TestDetectLibvipsUnavailable(t *testing.T) {
	opts := baseOptions(
		fakeLookPath(allBinaries...),
		fakeOCR(models.OCRCapabilitiesResponse{}),
	)
	opts.Vips = fakeVips("", 0)

	check := checksByID(t, DetectWith(context.Background(), opts))["libvips"]
	if check.Available {
		t.Fatal("an empty libvips version must be unavailable")
	}
	if !check.Required {
		t.Fatal("libvips must be required because every image tool depends on it")
	}
	if check.Detail != "Not available" {
		t.Fatalf("unexpected libvips detail: %q", check.Detail)
	}
	if check.Suggestion != "Rebuild FileForge against libvips (8.5 or newer) with its image loaders; every image tool needs it." {
		t.Fatalf("unexpected libvips suggestion: %q", check.Suggestion)
	}
}

func TestDetectPDFToDOCXEnvironmentMissing(t *testing.T) {
	state := managedReadyRuntime()
	state.PDF2DOCX = false
	opts := baseOptions(
		fakeLookPath(allBinaries...),
		fakeOCR(models.OCRCapabilitiesResponse{}),
	)
	opts.Managed = fakeManaged(state)

	check := checksByID(t, DetectWith(context.Background(), opts))["pdf2docx"]
	if check.Available {
		t.Fatal("pdf2docx must be unavailable when the managed environment is missing")
	}
	if check.Required {
		t.Fatal("a missing pdf2docx environment only degrades the PDF to DOCX tool")
	}
	if check.Detail != "Not installed" {
		t.Fatalf("unexpected pdf2docx detail: %q", check.Detail)
	}
	if check.Suggestion != pdf.PDFToDOCXUnavailableReason {
		t.Fatalf("unexpected pdf2docx suggestion: %q", check.Suggestion)
	}
}

func TestDetectBackgroundRemovalPackStates(t *testing.T) {
	tests := []struct {
		name           string
		engineReady    bool
		engineReason   string
		modelReady     bool
		modelReason    string
		wantAvailable  bool
		wantDetail     string
		wantSuggestion string
	}{
		{
			name:           "engine missing",
			engineReady:    false,
			engineReason:   "Background removal engine is not installed. Open Settings and run Install / repair.",
			modelReady:     false,
			wantDetail:     "Not installed",
			wantSuggestion: "Background removal engine is not installed. Open Settings and run Install / repair.",
		},
		{
			name:           "model missing",
			engineReady:    true,
			engineReason:   "Local background removal engine",
			modelReady:     false,
			modelReason:    "Background removal model is not installed. Run Install / repair.",
			wantAvailable:  false,
			wantDetail:     "Engine installed, but the model is not ready",
			wantSuggestion: "Background removal model is not installed. Run Install / repair.",
		},
		{
			name:          "pack ready",
			engineReady:   true,
			engineReason:  "Local background removal engine",
			modelReady:    true,
			modelReason:   "Local background removal model",
			wantAvailable: true,
			wantDetail:    "Background removal engine and model",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := managedReadyRuntime()
			state.RembgReady = test.engineReady
			state.RembgReason = test.engineReason
			state.RembgModelReady = test.modelReady
			state.RembgModelReason = test.modelReason

			opts := baseOptions(
				fakeLookPath(allBinaries...),
				fakeOCR(models.OCRCapabilitiesResponse{}),
			)
			opts.Managed = fakeManaged(state)

			check := checksByID(t, DetectWith(context.Background(), opts))["rembg"]
			if check.Available != test.wantAvailable {
				t.Fatalf("available = %t, want %t", check.Available, test.wantAvailable)
			}
			if check.Required {
				t.Fatal("the background removal pack must stay optional")
			}
			if check.Detail != test.wantDetail {
				t.Fatalf("detail = %q, want %q", check.Detail, test.wantDetail)
			}
			if check.Suggestion != test.wantSuggestion {
				t.Fatalf("suggestion = %q, want %q", check.Suggestion, test.wantSuggestion)
			}
		})
	}
}

func TestDetectWhisperModelStates(t *testing.T) {
	tests := []struct {
		name           string
		whisperPresent bool
		modelPresent   bool
		wantAvailable  bool
		wantDetail     string
		wantSuggestion string
	}{
		{
			name:           "runtime and model missing",
			wantDetail:     "Not installed",
			wantSuggestion: "Install whisper.cpp and a GGML model, or run Install / repair.",
		},
		{
			name:           "model missing",
			whisperPresent: true,
			wantDetail:     "Installed, but no model was found",
			wantSuggestion: "Download a GGML model in Settings or run Install / repair.",
		},
		{
			name:           "runtime missing",
			modelPresent:   true,
			wantDetail:     "Model found, but whisper-cli was not found",
			wantSuggestion: "Install whisper.cpp or run Install / repair.",
		},
		{
			name:           "ready",
			whisperPresent: true,
			modelPresent:   true,
			wantAvailable:  true,
			wantDetail:     "Model: ggml-medium.bin",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			state := managedReadyRuntime()
			state.Whisper = engine.TranscriptionCapabilities{
				WhisperPath:    "/data/fileforge/toolchain/whisper/whisper-cli",
				WhisperPresent: test.whisperPresent,
				ModelPath:      "/data/fileforge/toolchain/models/ggml-medium.bin",
				ModelPresent:   test.modelPresent,
				ModelName:      "ggml-medium.bin",
			}

			opts := baseOptions(
				fakeLookPath(allBinaries...),
				fakeOCR(models.OCRCapabilitiesResponse{}),
			)
			opts.Managed = fakeManaged(state)

			check := checksByID(t, DetectWith(context.Background(), opts))["whisper"]
			if check.Available != test.wantAvailable {
				t.Fatalf("available = %t, want %t", check.Available, test.wantAvailable)
			}
			if !check.Required {
				t.Fatal("the Whisper model must be required for the transcription tool")
			}
			if check.Detail != test.wantDetail {
				t.Fatalf("detail = %q, want %q", check.Detail, test.wantDetail)
			}
			if check.Suggestion != test.wantSuggestion {
				t.Fatalf("suggestion = %q, want %q", check.Suggestion, test.wantSuggestion)
			}
		})
	}
}

func TestDetectWithTesseractWithoutLanguageData(t *testing.T) {
	response := DetectWith(context.Background(), baseOptions(
		fakeLookPath("pdftoppm", "tesseract", "pdftotext", "ffmpeg", "ffprobe"),
		fakeOCR(models.OCRCapabilitiesResponse{
			Tesseract: true,
			Languages: []string{},
		}),
	))

	check := checksByID(t, response)["tesseract"]
	if check.Available {
		t.Fatal("tesseract without language data must be unavailable")
	}
	if check.Detail != "Installed, but no language data was found" {
		t.Fatalf("unexpected tesseract detail: %q", check.Detail)
	}
	if check.Suggestion == "" {
		t.Fatal("tesseract without language data must suggest installing a language pack")
	}
}

func TestDetectWithMissingTesseractBinary(t *testing.T) {
	response := DetectWith(context.Background(), baseOptions(
		fakeLookPath("pdftoppm", "pdftotext", "ffmpeg", "ffprobe"),
		fakeOCR(models.OCRCapabilitiesResponse{
			Tesseract: true,
			Languages: []string{"eng"},
		}),
	))

	check := checksByID(t, response)["tesseract"]
	if check.Available {
		t.Fatal("a missing tesseract binary must be unavailable even when languages are reported")
	}
	if check.Detail != "Not installed" {
		t.Fatalf("unexpected tesseract detail: %q", check.Detail)
	}
	if check.Suggestion != "Install tesseract and at least one language pack (eng, spa, ...)." {
		t.Fatalf("unexpected tesseract suggestion: %q", check.Suggestion)
	}
}

func TestDetectWithNoOCRProvider(t *testing.T) {
	opts := baseOptions(fakeLookPath(allBinaries...), nil)

	response := DetectWith(context.Background(), opts)
	if response.OCRLanguages == nil {
		t.Fatal("OCRLanguages must be an empty slice, never nil")
	}
	if len(response.OCRLanguages) != 0 {
		t.Fatalf("expected no OCR languages, got %+v", response.OCRLanguages)
	}
	if response.RecommendedOCRLanguage != "" {
		t.Fatalf("expected no recommended language, got %q", response.RecommendedOCRLanguage)
	}
	if checksByID(t, response)["tesseract"].Available {
		t.Fatal("tesseract without OCR data must be unavailable")
	}
}

func TestDetectReportsEveryCheckOnThisMachine(t *testing.T) {
	response := Detect(context.Background())

	if !response.Success || response.Message != "Local capabilities checked." {
		t.Fatalf("unexpected response envelope: %+v", response)
	}
	if len(response.Checks) != len(allChecks) {
		t.Fatalf("expected %d checks, got %d", len(allChecks), len(response.Checks))
	}
	if response.OCRLanguages == nil {
		t.Fatal("OCRLanguages must be an empty slice, never nil")
	}
	for index, id := range allChecks {
		check := response.Checks[index]
		if check.ID != id {
			t.Fatalf("check %d = %+v, want id %q", index, check, id)
		}
		if check.Label == "" {
			t.Fatalf("check %q must carry a label", id)
		}
		if check.Detail == "" {
			t.Fatalf("check %q must carry a detail line", id)
		}
		if !check.Available && check.Suggestion == "" {
			t.Fatalf("missing check %q must carry a suggestion", id)
		}
	}
}
