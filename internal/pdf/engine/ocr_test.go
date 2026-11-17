package engine

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jung-kurt/gofpdf"
	"github.com/pdfcpu/pdfcpu/pkg/api"
)

// ocrTestLanguage returns the recommended installed language and skips the
// test when the machine lacks a usable OCR runtime. The environment may only
// have `afr` and `osd` installed, so no test assumes eng or spa.
func ocrTestLanguage(t *testing.T) string {
	t.Helper()

	capabilities := DetectCapabilities(context.Background(), nil)
	if !capabilities.Available() {
		t.Skipf("local OCR is unavailable: %s", describeOCRCapabilities(capabilities))
	}

	return capabilities.RecommendedLanguage
}

func describeOCRCapabilities(capabilities OCRCapabilities) string {
	return fmt.Sprintf(
		"pdftoppm=%t tesseract=%t pdftotext=%t languages=%v",
		capabilities.PDFToPPM,
		capabilities.Tesseract,
		capabilities.PDFToText,
		capabilities.Languages,
	)
}

// writeOCRTextPDF creates a simple text PDF whose large glyphs survive
// rasterization and OCR.
func writeOCRTextPDF(t *testing.T, path string, pages int) {
	t.Helper()

	document := gofpdf.New("P", "mm", "A4", "")
	document.SetFont("Helvetica", "", 28)
	for page := 0; page < pages; page++ {
		document.AddPage()
		document.SetXY(20, 40)
		document.MultiCell(0, 16, fmt.Sprintf("Page %d", page+1), "", "L", false)
		document.SetXY(20, 80)
		document.MultiCell(0, 16, "FileForge OCR test document", "", "L", false)
	}
	if err := document.OutputFileAndClose(path); err != nil {
		t.Fatalf("write text pdf fixture %s: %v", path, err)
	}
}

// stubCommandRunner records commands and answers by command name. A blocked
// command waits for the context and then returns its error.
type stubCommandRunner struct {
	mu       sync.Mutex
	outputs  map[string][]byte
	failures map[string]error
	blockOn  string
	calls    []string
}

func (r *stubCommandRunner) Output(ctx context.Context, name string, args []string) ([]byte, error) {
	call := name + " " + strings.Join(args, " ")
	r.mu.Lock()
	r.calls = append(r.calls, call)
	blockOn := r.blockOn
	output, hasOutput := r.outputs[call]
	failure, hasFailure := r.failures[call]
	r.mu.Unlock()

	if blockOn != "" && name == blockOn {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if hasFailure {
		return output, failure
	}
	if hasOutput {
		return output, nil
	}

	return nil, fmt.Errorf("unexpected command: %s", call)
}

func (r *stubCommandRunner) snapshotCalls() []string {
	r.mu.Lock()
	defer r.mu.Unlock()

	return append([]string{}, r.calls...)
}

func TestDetectCapabilitiesExcludesOSDAndRecommends(t *testing.T) {
	if !ocrBinaryAvailable("tesseract") {
		t.Skip("tesseract is not installed")
	}

	payload := "List of available languages in \"/usr/share/tessdata/\" (4):\n\nafr\neng\nosd\nspa\n"
	runner := &stubCommandRunner{outputs: map[string][]byte{"tesseract --list-langs": []byte(payload)}}

	capabilities := DetectCapabilities(context.Background(), runner)
	if !capabilities.Tesseract || !capabilities.PDFToPPM || !capabilities.PDFToText {
		t.Fatalf("expected every binary to be detected, got %+v", capabilities)
	}
	wantLanguages := []string{"afr", "eng", "spa"}
	if strings.Join(capabilities.Languages, ",") != strings.Join(wantLanguages, ",") {
		t.Fatalf("expected languages %v, got %v", wantLanguages, capabilities.Languages)
	}
	if capabilities.RecommendedLanguage != "eng" {
		t.Fatalf("expected eng as the recommendation, got %q", capabilities.RecommendedLanguage)
	}
}

func TestDetectCapabilitiesRecommendsSpaThenFirst(t *testing.T) {
	if !ocrBinaryAvailable("tesseract") {
		t.Skip("tesseract is not installed")
	}

	cases := []struct {
		payload string
		want    string
	}{
		{payload: "List of available languages (3):\nafr\nosd\nspa\n", want: "spa"},
		{payload: "List of available languages (3):\nafr\ndeu\nosd\n", want: "afr"},
	}

	for _, testCase := range cases {
		runner := &stubCommandRunner{outputs: map[string][]byte{"tesseract --list-langs": []byte(testCase.payload)}}
		capabilities := DetectCapabilities(context.Background(), runner)
		if capabilities.RecommendedLanguage != testCase.want {
			t.Fatalf("payload %q: expected %q, got %q", testCase.payload, testCase.want, capabilities.RecommendedLanguage)
		}
	}
}

func TestDetectCapabilitiesWithoutLanguages(t *testing.T) {
	if !ocrBinaryAvailable("tesseract") {
		t.Skip("tesseract is not installed")
	}

	runner := &stubCommandRunner{outputs: map[string][]byte{"tesseract --list-langs": []byte("List of available languages (1):\nosd\n")}}
	capabilities := DetectCapabilities(context.Background(), runner)
	if len(capabilities.Languages) != 0 {
		t.Fatalf("expected osd to be excluded, got %v", capabilities.Languages)
	}
	if capabilities.Available() {
		t.Fatal("expected OCR to be unavailable without a real language")
	}
}

func TestDetectCapabilitiesMissingBinaries(t *testing.T) {
	original := ocrLookPath
	ocrLookPath = func(string) (string, error) { return "", exec.ErrNotFound }
	t.Cleanup(func() { ocrLookPath = original })

	capabilities := DetectCapabilities(context.Background(), nil)
	if capabilities.PDFToPPM || capabilities.Tesseract || capabilities.PDFToText {
		t.Fatalf("expected every binary to be missing, got %+v", capabilities)
	}
	if capabilities.Available() {
		t.Fatal("expected OCR to be unavailable")
	}
}

func TestOCRCapabilitiesHasLanguage(t *testing.T) {
	capabilities := OCRCapabilities{Languages: []string{"afr", "eng"}}

	for _, installed := range []string{"afr", "eng", "eng+afr", " afr "} {
		if !capabilities.HasLanguage(installed) {
			t.Fatalf("expected %q to be installed", installed)
		}
	}
	for _, missing := range []string{"spa", "eng+spa", ""} {
		if capabilities.HasLanguage(missing) {
			t.Fatalf("expected %q to be missing", missing)
		}
	}
}

func TestCountTextCharacters(t *testing.T) {
	cases := map[string]int{
		"":              0,
		"a b\nc\f\td":   4,
		"  \n\t\f\r\n ": 0,
		"héllo":         5,
	}

	for text, want := range cases {
		if got := CountTextCharacters(text); got != want {
			t.Fatalf("CountTextCharacters(%q) = %d, want %d", text, got, want)
		}
	}
}

func TestOCRRequestValidation(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeOCRTextPDF(t, input, 1)
	output := filepath.Join(dir, "scan_ocr.pdf")

	if err := ValidateOCRRequest(input, output, filepath.Join(dir, "scan_ocr.txt")); err != nil {
		t.Fatalf("expected the request to validate, got %v", err)
	}

	cases := []struct {
		name   string
		input  string
		output string
		text   string
		code   string
	}{
		{name: "missing input", input: "", output: output, code: ErrorCodeValidation},
		{name: "non pdf input", input: filepath.Join(dir, "scan.txt"), output: output, code: ErrorCodeValidation},
		{name: "unreadable input", input: filepath.Join(dir, "missing.pdf"), output: output, code: ErrorCodeInvalidInputPDF},
		{name: "non pdf output", input: input, output: filepath.Join(dir, "scan_ocr.txt"), code: ErrorCodeValidation},
		{name: "bad sidecar extension", input: input, output: output, text: filepath.Join(dir, "scan_ocr.md"), code: ErrorCodeValidation},
		{name: "missing output dir", input: input, output: filepath.Join(dir, "nope", "scan.pdf"), code: OCRCodeOutputDirInvalid},
		{name: "collides with input", input: input, output: input, code: ErrorCodeOutputCollidesInput},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidateOCRRequest(testCase.input, testCase.output, testCase.text)
			if err == nil {
				t.Fatal("expected validation to fail")
			}
			if err.Code != testCase.code {
				t.Fatalf("expected code %q, got %q (%s)", testCase.code, err.Code, err.Message)
			}
		})
	}

	if writeErr := os.WriteFile(output, []byte("taken"), 0o644); writeErr != nil {
		t.Fatalf("write existing output: %v", writeErr)
	}
	existsErr := ValidateOCRRequest(input, output, "")
	if existsErr == nil || existsErr.Code != OCRCodeOutputExists {
		t.Fatalf("expected OCR_OUTPUT_ALREADY_EXISTS, got %v", existsErr)
	}
}

func TestConvertProducesSearchablePDFAndSidecar(t *testing.T) {
	language := ocrTestLanguage(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeOCRTextPDF(t, input, 2)
	outputPDF := filepath.Join(dir, "scan_ocr.pdf")
	outputText := filepath.Join(dir, "scan_ocr.txt")

	var mu sync.Mutex
	lastCurrent, lastTotal := 0, 0
	progressCalls := 0

	result, ocrErr := Convert(context.Background(), nil, OCRRequest{
		InputPath:      input,
		OutputPDFPath:  outputPDF,
		OutputTextPath: outputText,
		Language:       language,
		DPI:            300,
	}, func(current, total int, _ string) {
		mu.Lock()
		defer mu.Unlock()
		progressCalls++
		lastCurrent = current
		lastTotal = total
	})
	if ocrErr != nil {
		t.Fatalf("expected OCR to succeed, got %v", ocrErr)
	}

	if result.PageCount != 2 {
		t.Fatalf("expected 2 pages, got %d", result.PageCount)
	}
	if result.TextCharacters <= 0 {
		t.Fatalf("expected recognized characters, got %d (%v)", result.TextCharacters, result.Warnings)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings on a clean run, got %v", result.Warnings)
	}

	pages, pageErr := api.PageCountFile(outputPDF)
	if pageErr != nil {
		t.Fatalf("read output page count: %v", pageErr)
	}
	if pages != 2 {
		t.Fatalf("expected the output to keep 2 pages, got %d", pages)
	}

	text, readErr := os.ReadFile(outputText)
	if readErr != nil {
		t.Fatalf("read text sidecar: %v", readErr)
	}
	if CountTextCharacters(string(text)) == 0 {
		t.Fatalf("expected a non-empty sidecar, got %q", string(text))
	}
	if !strings.Contains(string(text), "\f") {
		t.Fatalf("expected a form feed between pages, got %q", string(text))
	}

	extracted, ok := ExtractPDFText(context.Background(), nil, outputPDF)
	if !ok {
		t.Fatal("expected pdftotext to read the output")
	}
	if CountTextCharacters(extracted) == 0 {
		t.Fatal("expected the output PDF to be searchable")
	}

	mu.Lock()
	defer mu.Unlock()
	if progressCalls != 2 || lastCurrent != 2 || lastTotal != 2 {
		t.Fatalf("expected progress to reach 2/2, got calls=%d %d/%d", progressCalls, lastCurrent, lastTotal)
	}
}

func TestConvertRejectsUnknownLanguage(t *testing.T) {
	language := ocrTestLanguage(t)
	if language == "" {
		t.Fatal("expected a recommended language")
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeOCRTextPDF(t, input, 1)

	_, ocrErr := Convert(context.Background(), nil, OCRRequest{
		InputPath:     input,
		OutputPDFPath: filepath.Join(dir, "scan_ocr.pdf"),
		Language:      "not-a-language",
	}, nil)
	if ocrErr == nil {
		t.Fatal("expected an unknown language to fail")
	}
	if ocrErr.Code != OCRCodeLanguageUnavailable {
		t.Fatalf("expected OCR_LANGUAGE_UNAVAILABLE, got %q", ocrErr.Code)
	}
	if !strings.Contains(ocrErr.Message, "Installed languages") {
		t.Fatalf("expected the installed languages in the message, got %q", ocrErr.Message)
	}
}

func TestConvertRejectsOutOfRangeDPI(t *testing.T) {
	language := ocrTestLanguage(t)

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeOCRTextPDF(t, input, 1)

	_, ocrErr := Convert(context.Background(), nil, OCRRequest{
		InputPath:     input,
		OutputPDFPath: filepath.Join(dir, "scan_ocr.pdf"),
		Language:      language,
		DPI:           900,
	}, nil)
	if ocrErr == nil || ocrErr.Code != ErrorCodeValidation {
		t.Fatalf("expected a validation error, got %v", ocrErr)
	}
}

func TestConvertCancellationLeavesNoOutputsAndCleansTemp(t *testing.T) {
	language := ocrTestLanguage(t)

	tempRoot := t.TempDir()
	t.Setenv("TMPDIR", tempRoot)

	dir := t.TempDir()
	input := filepath.Join(dir, "scan.pdf")
	writeOCRTextPDF(t, input, 3)
	outputPDF := filepath.Join(dir, "scan_ocr.pdf")
	outputText := filepath.Join(dir, "scan_ocr.txt")

	runner := &stubCommandRunner{
		outputs: map[string][]byte{
			"tesseract --list-langs": []byte("List of available languages (2):\n" + language + "\nosd\n"),
		},
		blockOn: "pdftoppm",
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, ocrErr := Convert(ctx, runner, OCRRequest{
		InputPath:      input,
		OutputPDFPath:  outputPDF,
		OutputTextPath: outputText,
		Language:       language,
		DPI:            300,
	}, nil)
	if ocrErr == nil {
		t.Fatal("expected cancellation to fail the run")
	}
	if ocrErr.Code != OCRCodeCancelled {
		t.Fatalf("expected OCR_CANCELLED, got %q (%s)", ocrErr.Code, ocrErr.Message)
	}

	for _, path := range []string{outputPDF, outputText} {
		if _, err := os.Stat(path); !os.IsNotExist(err) {
			t.Fatalf("expected %s to be absent, stat err = %v", path, err)
		}
	}

	entries, err := os.ReadDir(tempRoot)
	if err != nil {
		t.Fatalf("read temp root: %v", err)
	}
	if len(entries) != 0 {
		t.Fatalf("expected the OCR workspace to be removed, found %d entries", len(entries))
	}
}
