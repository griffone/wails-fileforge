package capabilities

import (
	"context"
	"errors"
	"testing"

	"fileforge-desktop/internal/models"
)

var allChecks = []string{"pdftoppm", "tesseract", "pdftotext", "gs", "ffmpeg", "ffprobe", "libreoffice", "pandoc"}

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

func fakeOCR(response models.OCRCapabilitiesResponseV1) func(context.Context) models.OCRCapabilitiesResponseV1 {
	return func(context.Context) models.OCRCapabilitiesResponseV1 {
		return response
	}
}

func checksByID(t *testing.T, response models.LocalCapabilitiesResponseV1) map[string]models.LocalCapabilityV1 {
	t.Helper()

	byID := make(map[string]models.LocalCapabilityV1, len(response.Checks))
	for _, check := range response.Checks {
		if _, exists := byID[check.ID]; exists {
			t.Fatalf("duplicate capability id %q", check.ID)
		}
		byID[check.ID] = check
	}

	return byID
}

func TestDetectWithAllToolsAvailable(t *testing.T) {
	response := DetectWith(context.Background(), Options{
		LookPath: fakeLookPath(allChecks...),
		OCR: fakeOCR(models.OCRCapabilitiesResponseV1{
			Success:             true,
			Available:           true,
			Tesseract:           true,
			Languages:           []string{"afr", "eng"},
			RecommendedLanguage: "eng",
		}),
	})

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
		"pdftoppm":    true,
		"tesseract":   true,
		"pdftotext":   false,
		"gs":          false,
		"ffmpeg":      true,
		"ffprobe":     true,
		"libreoffice": false,
		"pandoc":      false,
	}
	for id, wantRequired := range required {
		check, found := byID[id]
		if !found {
			t.Fatalf("missing check %q", id)
		}
		if check.Label != id {
			t.Fatalf("check %q label = %q", id, check.Label)
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
	if len(response.OCRLanguages) != 2 || response.OCRLanguages[0] != "afr" || response.OCRLanguages[1] != "eng" {
		t.Fatalf("unexpected OCR languages: %+v", response.OCRLanguages)
	}
	if response.RecommendedOCRLanguage != "eng" {
		t.Fatalf("unexpected recommended language: %q", response.RecommendedOCRLanguage)
	}
}

func TestDetectWithMissingOptionalAndRequiredTools(t *testing.T) {
	response := DetectWith(context.Background(), Options{
		LookPath: fakeLookPath("pdftoppm", "tesseract", "pdftotext", "ffprobe", "libreoffice", "pandoc"),
		OCR: fakeOCR(models.OCRCapabilitiesResponseV1{
			Tesseract:           true,
			Languages:           []string{"eng"},
			RecommendedLanguage: "eng",
		}),
	})

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
	if ffmpegCheck.Suggestion != "Install ffmpeg for the video tools." {
		t.Fatalf("unexpected ffmpeg suggestion: %q", ffmpegCheck.Suggestion)
	}

	ffprobeCheck := byID["ffprobe"]
	if !ffprobeCheck.Available || !ffprobeCheck.Required {
		t.Fatalf("ffprobe should be available and required: %+v", ffprobeCheck)
	}
}

func TestDetectWithTesseractWithoutLanguageData(t *testing.T) {
	response := DetectWith(context.Background(), Options{
		LookPath: fakeLookPath("pdftoppm", "tesseract", "pdftotext", "ffmpeg", "ffprobe"),
		OCR: fakeOCR(models.OCRCapabilitiesResponseV1{
			Tesseract: true,
			Languages: []string{},
		}),
	})

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
	response := DetectWith(context.Background(), Options{
		LookPath: fakeLookPath("pdftoppm", "pdftotext", "ffmpeg", "ffprobe"),
		OCR: fakeOCR(models.OCRCapabilitiesResponseV1{
			Tesseract: true,
			Languages: []string{"eng"},
		}),
	})

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
	response := DetectWith(context.Background(), Options{
		LookPath: fakeLookPath(allChecks...),
	})

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
		if check.ID != id || check.Label != id {
			t.Fatalf("check %d = %+v, want id and label %q", index, check, id)
		}
		if check.Detail == "" {
			t.Fatalf("check %q must carry a detail line", id)
		}
		if !check.Available && check.Suggestion == "" {
			t.Fatalf("missing check %q must carry a suggestion", id)
		}
	}
}
