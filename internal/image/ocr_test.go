package image

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"fileforge-desktop/internal/pdf/engine"

	"github.com/fogleman/gg"
	"github.com/h2non/bimg"
)

// imageOCRStubRunner records every command and lets each test answer it. The
// production code only talks to Tesseract, so no other command is expected.
type imageOCRStubRunner struct {
	mu    sync.Mutex
	calls [][]string
	onRun func(ctx context.Context, name string, args []string) ([]byte, error)
}

func (r *imageOCRStubRunner) Output(ctx context.Context, name string, args []string) ([]byte, error) {
	r.mu.Lock()
	r.calls = append(r.calls, append([]string{name}, args...))
	onRun := r.onRun
	r.mu.Unlock()

	if onRun == nil {
		return nil, fmt.Errorf("unexpected command: %s %s", name, strings.Join(args, " "))
	}

	return onRun(ctx, name, args)
}

func (r *imageOCRStubRunner) snapshotCalls() [][]string {
	r.mu.Lock()
	defer r.mu.Unlock()

	calls := make([][]string, len(r.calls))
	for index, call := range r.calls {
		calls[index] = append([]string{}, call...)
	}

	return calls
}

// stubImageOCRCapabilities replaces the package detector and returns a counter
// so tests can prove the runtime is detected once per job, not once per image.
func stubImageOCRCapabilities(t *testing.T, capabilities engine.OCRCapabilities) *int {
	t.Helper()

	calls := 0
	previous := imageOCRDetectCapabilities
	imageOCRDetectCapabilities = func(context.Context, engine.CommandRunner) engine.OCRCapabilities {
		calls++
		return capabilities
	}
	t.Cleanup(func() {
		imageOCRDetectCapabilities = previous
	})

	return &calls
}

// imageOCRTestCapabilities builds a runtime snapshot where Tesseract is
// installed but the PDF rasterizer is not, which is the normal image OCR case.
func imageOCRTestCapabilities(languages ...string) engine.OCRCapabilities {
	if len(languages) == 0 {
		languages = []string{"eng"}
	}

	return engine.OCRCapabilities{
		Tesseract:           true,
		Languages:           append([]string{}, languages...),
		RecommendedLanguage: languages[0],
	}
}

// imageOCRStubOutputs writes the TXT and PDF that Tesseract would produce for
// a stub command.
func imageOCRStubOutputs(args []string, text, pdf string) error {
	if err := os.WriteFile(args[1]+".txt", []byte(text), 0o644); err != nil {
		return err
	}

	return os.WriteFile(args[1]+".pdf", []byte(pdf), 0o644)
}

func TestImageOCRAvailableIgnoresPDFRasterizer(t *testing.T) {
	cases := []struct {
		name         string
		capabilities engine.OCRCapabilities
		want         bool
	}{
		{
			name:         "tesseract and a language are enough",
			capabilities: engine.OCRCapabilities{Tesseract: true, Languages: []string{"eng"}},
			want:         true,
		},
		{
			name: "pdftoppm and pdftotext are irrelevant",
			capabilities: engine.OCRCapabilities{
				Tesseract: true,
				PDFToPPM:  false,
				PDFToText: false,
				Languages: []string{"eng", "spa"},
			},
			want: true,
		},
		{
			name:         "missing tesseract",
			capabilities: engine.OCRCapabilities{PDFToPPM: true, Languages: []string{"eng"}},
			want:         false,
		},
		{
			name:         "no installed languages",
			capabilities: engine.OCRCapabilities{Tesseract: true, PDFToPPM: true},
			want:         false,
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := ImageOCRAvailable(testCase.capabilities); got != testCase.want {
				t.Fatalf("ImageOCRAvailable(%+v) = %t, want %t", testCase.capabilities, got, testCase.want)
			}
		})
	}
}

func TestImageOCRRecognizePublishesTextAndPDF(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 64, 48)
	textPath := filepath.Join(dir, "photo.txt")
	pdfPath := filepath.Join(dir, "photo_ocr.pdf")

	var normalizedPath string
	runner := &imageOCRStubRunner{onRun: func(_ context.Context, name string, args []string) ([]byte, error) {
		if name != "tesseract" {
			return nil, fmt.Errorf("unexpected command %q", name)
		}
		if len(args) != 10 {
			return nil, fmt.Errorf("unexpected argv length: %v", args)
		}

		// The first argument is the normalized PNG in the private temp dir.
		normalizedPath = args[0]
		normalized, readErr := os.ReadFile(normalizedPath)
		if readErr != nil {
			return nil, fmt.Errorf("normalized png missing: %w", readErr)
		}
		if !bytes.HasPrefix(normalized, []byte("\x89PNG\r\n\x1a\n")) {
			return nil, fmt.Errorf("normalized image is not a PNG")
		}

		wantArgs := []string{
			normalizedPath,
			args[1],
			"-l", "eng",
			"--dpi", "300",
			"--psm", "3",
			"txt", "pdf",
		}
		if !slices.Equal(args, wantArgs) {
			return nil, fmt.Errorf("argv = %v, want %v", args, wantArgs)
		}

		return nil, imageOCRStubOutputs(args, "Hello, OCR world\n", "%PDF-1.4\nstub searchable pdf\n")
	}}

	detectionCalls := stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

	result, recognizeErr := RecognizeImage(context.Background(), runner, input, textPath, pdfPath, ImageOCROptions{DPI: 300})
	if recognizeErr != nil {
		t.Fatalf("expected recognition to succeed, got %v", recognizeErr)
	}

	if detectionCalls == nil || *detectionCalls != 1 {
		t.Fatalf("expected one capability detection, got %v", detectionCalls)
	}
	if len(runner.snapshotCalls()) != 1 {
		t.Fatalf("expected exactly one tesseract process, got %v", runner.snapshotCalls())
	}
	if result.TextPath != textPath || result.PDFPath != pdfPath {
		t.Fatalf("unexpected result paths: %+v", result)
	}
	if result.TextCharacters != 14 {
		t.Fatalf("expected 14 non-whitespace characters, got %d", result.TextCharacters)
	}
	if result.Width != 64 || result.Height != 48 {
		t.Fatalf("expected the normalized 64x48 dimensions, got %dx%d", result.Width, result.Height)
	}
	if len(result.Warnings) != 0 {
		t.Fatalf("expected no warnings, got %v", result.Warnings)
	}

	text, readErr := os.ReadFile(textPath)
	if readErr != nil || string(text) != "Hello, OCR world\n" {
		t.Fatalf("unexpected published text %q (err %v)", string(text), readErr)
	}
	pdf, readErr := os.ReadFile(pdfPath)
	if readErr != nil || !bytes.HasPrefix(pdf, []byte("%PDF")) {
		t.Fatalf("unexpected published PDF %q (err %v)", string(pdf), readErr)
	}

	if _, statErr := os.Stat(normalizedPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the normalized PNG to be removed, stat err %v", statErr)
	}
	if _, statErr := os.Stat(filepath.Dir(normalizedPath)); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the OCR workspace to be removed, stat err %v", statErr)
	}
}

func TestImageOCRRecognizeDefaultsToRecommendedLanguage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 20, 10)
	textPath := filepath.Join(dir, "photo.txt")

	runner := &imageOCRStubRunner{onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
		if !slices.Contains(args, "-l") || args[slices.Index(args, "-l")+1] != "afr" {
			return nil, fmt.Errorf("expected the recommended language, argv %v", args)
		}

		return nil, imageOCRStubOutputs(args, "text\n", "%PDF-1.4\n")
	}}
	stubImageOCRCapabilities(t, engine.OCRCapabilities{
		Tesseract:           true,
		Languages:           []string{"afr"},
		RecommendedLanguage: "afr",
	})

	if _, err := RecognizeImage(context.Background(), runner, input, textPath, "", ImageOCROptions{}); err != nil {
		t.Fatalf("expected recognition to succeed, got %v", err)
	}
}

func TestImageOCRRecognizeAcceptsLanguageExpression(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 20, 10)
	textPath := filepath.Join(dir, "photo.txt")

	runner := &imageOCRStubRunner{onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
		if !slices.Contains(args, "eng+spa") {
			return nil, fmt.Errorf("expected eng+spa in argv, got %v", args)
		}

		return nil, imageOCRStubOutputs(args, "text\n", "%PDF-1.4\n")
	}}
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng", "spa"))

	if _, err := RecognizeImage(context.Background(), runner, input, textPath, "", ImageOCROptions{Language: "eng+spa"}); err != nil {
		t.Fatalf("expected the language expression to be accepted, got %v", err)
	}
}

func TestImageOCRRecognizeRefusesExistingOutputs(t *testing.T) {
	cases := []struct {
		name    string
		prepare func(t *testing.T, textPath, pdfPath string)
	}{
		{
			name: "existing text output",
			prepare: func(t *testing.T, textPath, _ string) {
				if err := os.WriteFile(textPath, []byte("existing"), 0o644); err != nil {
					t.Fatalf("write existing text: %v", err)
				}
			},
		},
		{
			name: "existing pdf output",
			prepare: func(t *testing.T, _, pdfPath string) {
				if err := os.WriteFile(pdfPath, []byte("%PDF-existing"), 0o644); err != nil {
					t.Fatalf("write existing pdf: %v", err)
				}
			},
		},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "photo.png")
			writePlanPNG(t, input, 20, 10)
			textPath := filepath.Join(dir, "photo.txt")
			pdfPath := filepath.Join(dir, "photo_ocr.pdf")
			testCase.prepare(t, textPath, pdfPath)

			runner := &imageOCRStubRunner{}
			stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

			_, err := RecognizeImage(context.Background(), runner, input, textPath, pdfPath, ImageOCROptions{})
			var ocrErr *ImageOCRError
			if !errors.As(err, &ocrErr) || ocrErr.Code != ImageOCRCodeOutputExists {
				t.Fatalf("expected %s, got %v", ImageOCRCodeOutputExists, err)
			}
			if len(runner.snapshotCalls()) != 0 {
				t.Fatalf("expected no process for a refused output, got %v", runner.snapshotCalls())
			}
		})
	}
}

func TestImageOCRRecognizeRejectsMissingLanguage(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 20, 10)
	textPath := filepath.Join(dir, "photo.txt")

	runner := &imageOCRStubRunner{}
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

	_, err := RecognizeImage(context.Background(), runner, input, textPath, "", ImageOCROptions{Language: "spa"})
	var ocrErr *ImageOCRError
	if !errors.As(err, &ocrErr) || ocrErr.Code != ImageOCRCodeLanguageUnavailable {
		t.Fatalf("expected %s, got %v", ImageOCRCodeLanguageUnavailable, err)
	}
	if len(runner.snapshotCalls()) != 0 {
		t.Fatalf("expected no process for a missing language, got %v", runner.snapshotCalls())
	}
}

func TestImageOCRRecognizeRejectsInvalidDPI(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 20, 10)
	textPath := filepath.Join(dir, "photo.txt")

	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

	for _, dpi := range []int{-1, engine.MinOCRDPI - 1, engine.MaxOCRDPI + 1} {
		_, err := RecognizeImage(context.Background(), &imageOCRStubRunner{}, input, textPath, "", ImageOCROptions{DPI: dpi})
		var ocrErr *ImageOCRError
		if !errors.As(err, &ocrErr) || ocrErr.Code != ImageOCRCodeDPIInvalid {
			t.Fatalf("dpi %d: expected %s, got %v", dpi, ImageOCRCodeDPIInvalid, err)
		}
	}
}

func TestImageOCRRecognizeCancellation(t *testing.T) {
	t.Run("cancelled before start", func(t *testing.T) {
		dir := t.TempDir()
		input := filepath.Join(dir, "photo.png")
		writePlanPNG(t, input, 20, 10)

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		runner := &imageOCRStubRunner{}
		stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

		_, err := RecognizeImage(ctx, runner, input, filepath.Join(dir, "photo.txt"), "", ImageOCROptions{})
		var ocrErr *ImageOCRError
		if !errors.As(err, &ocrErr) || ocrErr.Code != ImageOCRCodeCancelled {
			t.Fatalf("expected %s, got %v", ImageOCRCodeCancelled, err)
		}
		if len(runner.snapshotCalls()) != 0 {
			t.Fatalf("expected no process after cancellation, got %v", runner.snapshotCalls())
		}
	})

	t.Run("cancelled while tesseract runs", func(t *testing.T) {
		dir := t.TempDir()
		input := filepath.Join(dir, "photo.png")
		writePlanPNG(t, input, 20, 10)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		runner := &imageOCRStubRunner{onRun: func(runCtx context.Context, _ string, _ []string) ([]byte, error) {
			cancel()
			<-runCtx.Done()

			return nil, runCtx.Err()
		}}
		stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

		_, err := RecognizeImage(ctx, runner, input, filepath.Join(dir, "photo.txt"), "", ImageOCROptions{})
		var ocrErr *ImageOCRError
		if !errors.As(err, &ocrErr) || ocrErr.Code != ImageOCRCodeCancelled {
			t.Fatalf("expected %s, got %v", ImageOCRCodeCancelled, err)
		}
		if len(runner.snapshotCalls()) != 1 {
			t.Fatalf("expected one cancelled process, got %v", runner.snapshotCalls())
		}
	})

	t.Run("cancelled after tesseract returns", func(t *testing.T) {
		dir := t.TempDir()
		input := filepath.Join(dir, "photo.png")
		writePlanPNG(t, input, 20, 10)
		textPath := filepath.Join(dir, "photo.txt")
		pdfPath := filepath.Join(dir, "photo_ocr.pdf")

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		runner := &imageOCRStubRunner{onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
			outputsErr := imageOCRStubOutputs(args, "text\n", "%PDF-1.4\n")
			cancel()

			return nil, outputsErr
		}}
		stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

		_, err := RecognizeImage(ctx, runner, input, textPath, pdfPath, ImageOCROptions{})
		var ocrErr *ImageOCRError
		if !errors.As(err, &ocrErr) || ocrErr.Code != ImageOCRCodeCancelled {
			t.Fatalf("expected %s, got %v", ImageOCRCodeCancelled, err)
		}
		for _, path := range []string{textPath, pdfPath} {
			if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("a cancelled run must not publish %s, stat err %v", path, statErr)
			}
		}
	})
}

// imageOCRTextPresenceContext reports cancellation once textPath exists. It
// deterministically targets the window between the TXT and PDF publishes
// without adding test hooks to the production publisher.
type imageOCRTextPresenceContext struct {
	context.Context
	textPath  string
	mu        sync.Mutex
	cancelled bool
}

func (c *imageOCRTextPresenceContext) Err() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.cancelled {
		if _, err := os.Stat(c.textPath); err == nil {
			c.cancelled = true
		}
	}
	if c.cancelled {
		return context.Canceled
	}

	return c.Context.Err()
}

func TestImageOCRRecognizeCancellationBetweenPublishes(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 20, 10)
	textPath := filepath.Join(dir, "photo.txt")
	pdfPath := filepath.Join(dir, "photo_ocr.pdf")

	runner := &imageOCRStubRunner{onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
		return nil, imageOCRStubOutputs(args, "text\n", "%PDF-1.4\n")
	}}
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

	ctx := &imageOCRTextPresenceContext{Context: context.Background(), textPath: textPath}
	_, err := RecognizeImage(ctx, runner, input, textPath, pdfPath, ImageOCROptions{})
	var ocrErr *ImageOCRError
	if !errors.As(err, &ocrErr) || ocrErr.Code != ImageOCRCodeCancelled {
		t.Fatalf("expected %s, got %v", ImageOCRCodeCancelled, err)
	}
	for _, path := range []string{textPath, pdfPath} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("the published TXT must be rolled back and the PDF skipped, %s stat err %v", path, statErr)
		}
	}
}

func TestImageOCRRecognizeNoTextIsAWarning(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "blank.png")
	writePlanPNG(t, input, 20, 10)
	textPath := filepath.Join(dir, "blank.txt")
	pdfPath := filepath.Join(dir, "blank_ocr.pdf")

	runner := &imageOCRStubRunner{onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
		return nil, imageOCRStubOutputs(args, "", "%PDF-1.4\n")
	}}
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

	result, err := RecognizeImage(context.Background(), runner, input, textPath, pdfPath, ImageOCROptions{})
	if err != nil {
		t.Fatalf("a no-text result must not fail, got %v", err)
	}
	if result.TextCharacters != 0 {
		t.Fatalf("expected zero characters, got %d", result.TextCharacters)
	}
	if !slices.Contains(result.Warnings, ImageOCRWarningNoText) {
		t.Fatalf("expected the no-text warning, got %v", result.Warnings)
	}
	if _, statErr := os.Stat(textPath); statErr != nil {
		t.Fatalf("expected an empty text file to be published, got %v", statErr)
	}
}

func TestImageOCRRecognizeValidatesPDFHeader(t *testing.T) {
	cases := []struct {
		name  string
		pdf   string
		write bool
	}{
		{name: "wrong header", pdf: "not a pdf", write: true},
		{name: "empty file", pdf: "", write: true},
		{name: "missing file", write: false},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			dir := t.TempDir()
			input := filepath.Join(dir, "photo.png")
			writePlanPNG(t, input, 20, 10)
			textPath := filepath.Join(dir, "photo.txt")
			pdfPath := filepath.Join(dir, "photo_ocr.pdf")

			runner := &imageOCRStubRunner{onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
				if writeErr := os.WriteFile(args[1]+".txt", []byte("text\n"), 0o644); writeErr != nil {
					return nil, writeErr
				}
				if !testCase.write {
					return nil, nil
				}

				return nil, os.WriteFile(args[1]+".pdf", []byte(testCase.pdf), 0o644)
			}}
			stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

			_, err := RecognizeImage(context.Background(), runner, input, textPath, pdfPath, ImageOCROptions{})
			var ocrErr *ImageOCRError
			if !errors.As(err, &ocrErr) || ocrErr.Code != ImageOCRCodeVerifyFailed {
				t.Fatalf("expected %s, got %v", ImageOCRCodeVerifyFailed, err)
			}
			if _, statErr := os.Stat(textPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("expected no text output when the PDF is invalid, stat err %v", statErr)
			}
			if _, statErr := os.Stat(pdfPath); !errors.Is(statErr, os.ErrNotExist) {
				t.Fatalf("expected no PDF output, stat err %v", statErr)
			}
		})
	}
}

func TestImageOCRRecognizeMissingTextFailsAndCleansWorkspace(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 20, 10)
	textPath := filepath.Join(dir, "photo.txt")
	pdfPath := filepath.Join(dir, "photo_ocr.pdf")

	var workspace string
	runner := &imageOCRStubRunner{onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
		workspace = filepath.Dir(args[0])

		// Tesseract wrote the PDF but not the TXT.
		return nil, os.WriteFile(args[1]+".pdf", []byte("%PDF-1.4\n"), 0o644)
	}}
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

	_, err := RecognizeImage(context.Background(), runner, input, textPath, pdfPath, ImageOCROptions{})
	var ocrErr *ImageOCRError
	if !errors.As(err, &ocrErr) || ocrErr.Code != ImageOCRCodeRecognizeFailed {
		t.Fatalf("expected %s, got %v", ImageOCRCodeRecognizeFailed, err)
	}
	for _, path := range []string{textPath, pdfPath} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("expected no published output %s, stat err %v", path, statErr)
		}
	}

	if workspace == "" {
		t.Fatal("expected the stub to observe the OCR workspace")
	}
	if _, statErr := os.Stat(workspace); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected the OCR workspace to be removed on failure, stat err %v", statErr)
	}
}

func TestImageOCRRecognizeUndecodableInput(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "broken.png")
	if err := os.WriteFile(input, []byte("this is not a decodable image"), 0o644); err != nil {
		t.Fatalf("write undecodable fixture: %v", err)
	}

	runner := &imageOCRStubRunner{}
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

	_, err := RecognizeImage(context.Background(), runner, input, filepath.Join(dir, "broken.txt"), "", ImageOCROptions{})
	var ocrErr *ImageOCRError
	if !errors.As(err, &ocrErr) || ocrErr.Code != ImageOCRCodeNormalizeFailed {
		t.Fatalf("expected %s, got %v", ImageOCRCodeNormalizeFailed, err)
	}
	if len(runner.snapshotCalls()) != 0 {
		t.Fatalf("expected no Tesseract process for undecodable input, got %v", runner.snapshotCalls())
	}
}

func TestImageOCRRecognizePublishFailureRollsBackText(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("POSIX directory permissions are required")
	}
	if os.Geteuid() == 0 {
		t.Skip("root can write to a 0555 directory")
	}

	root := t.TempDir()
	textDir := filepath.Join(root, "text")
	pdfDir := filepath.Join(root, "pdf")
	for _, dir := range []string{textDir, pdfDir} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatalf("create fixture dir %s: %v", dir, err)
		}
	}

	input := filepath.Join(textDir, "photo.png")
	writePlanPNG(t, input, 20, 10)
	textPath := filepath.Join(textDir, "photo.txt")
	pdfPath := filepath.Join(pdfDir, "photo_ocr.pdf")

	runner := &imageOCRStubRunner{onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
		if outputsErr := imageOCRStubOutputs(args, "text\n", "%PDF-1.4\n"); outputsErr != nil {
			return nil, outputsErr
		}

		// The Tesseract step succeeds, then the PDF destination becomes
		// unwritable, so the TXT publish must be rolled back.
		return nil, os.Chmod(pdfDir, 0o555)
	}}
	t.Cleanup(func() {
		_ = os.Chmod(pdfDir, 0o755)
	})
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

	_, err := RecognizeImage(context.Background(), runner, input, textPath, pdfPath, ImageOCROptions{})
	var ocrErr *ImageOCRError
	if !errors.As(err, &ocrErr) || ocrErr.Code != ImageOCRCodePublishFailed {
		t.Fatalf("expected %s, got %v", ImageOCRCodePublishFailed, err)
	}
	for _, path := range []string{textPath, pdfPath} {
		if _, statErr := os.Stat(path); !errors.Is(statErr, os.ErrNotExist) {
			t.Fatalf("expected no partial output %s after the publish failure, stat err %v", path, statErr)
		}
	}
}

func TestImageOCRRecognizeHandlesPathsWithSpaces(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "with spaces")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatalf("create spaced fixture dir: %v", err)
	}

	input := filepath.Join(dir, "my photo.png")
	writePlanPNG(t, input, 20, 10)
	textPath := filepath.Join(dir, "my photo.txt")
	pdfPath := filepath.Join(dir, "my photo_ocr.pdf")

	runner := &imageOCRStubRunner{onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
		return nil, imageOCRStubOutputs(args, "hello\n", "%PDF-1.4\n")
	}}
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

	result, err := RecognizeImage(context.Background(), runner, input, textPath, pdfPath, ImageOCROptions{})
	if err != nil {
		t.Fatalf("expected spaced paths to succeed, got %v", err)
	}
	if result.TextPath != textPath || result.PDFPath != pdfPath {
		t.Fatalf("unexpected result paths: %+v", result)
	}
	for _, path := range []string{textPath, pdfPath} {
		if _, statErr := os.Stat(path); statErr != nil {
			t.Fatalf("expected %s to exist, got %v", path, statErr)
		}
	}
}

func TestImageOCRRecognizeDefaultsTo300DPI(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 20, 10)
	textPath := filepath.Join(dir, "photo.txt")

	runner := &imageOCRStubRunner{onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
		dpiIndex := slices.Index(args, "--dpi")
		if dpiIndex < 0 || dpiIndex+1 >= len(args) || args[dpiIndex+1] != "300" {
			return nil, fmt.Errorf("expected the default --dpi 300 argv, got %v", args)
		}

		return nil, imageOCRStubOutputs(args, "text\n", "%PDF-1.4\n")
	}}
	stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

	if _, err := RecognizeImage(context.Background(), runner, input, textPath, "", ImageOCROptions{}); err != nil {
		t.Fatalf("expected the default DPI to be accepted, got %v", err)
	}
}

func TestImageOCRValidateInput(t *testing.T) {
	dir := t.TempDir()
	imagePath := filepath.Join(dir, "photo.png")
	writePlanPNG(t, imagePath, 10, 10)
	textPath := filepath.Join(dir, "notes.txt")
	if err := os.WriteFile(textPath, []byte("notes"), 0o644); err != nil {
		t.Fatalf("write text fixture: %v", err)
	}

	cases := []struct {
		name  string
		input string
		code  string
	}{
		{name: "empty path", input: "  ", code: ImageOCRCodeInputRequired},
		{name: "missing file", input: filepath.Join(dir, "missing.png"), code: ImageOCRCodeInputNotFound},
		{name: "folder", input: dir, code: ImageOCRCodeInputUnsupported},
		{name: "unsupported extension", input: textPath, code: ImageOCRCodeInputUnsupported},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := ValidateImageOCRInput(testCase.input)
			if err == nil || err.Code != testCase.code {
				t.Fatalf("expected %s, got %v", testCase.code, err)
			}
		})
	}
}

func TestImageOCRValidateRequest(t *testing.T) {
	dir := t.TempDir()
	input := filepath.Join(dir, "photo.png")
	writePlanPNG(t, input, 10, 10)
	missingDir := filepath.Join(dir, "missing")

	cases := []struct {
		name     string
		textPath string
		pdfPath  string
		code     string
	}{
		{name: "text path required", textPath: "", code: ImageOCRCodeOutputInvalid},
		{name: "text extension", textPath: filepath.Join(dir, "photo.md"), code: ImageOCRCodeOutputInvalid},
		{name: "pdf extension", textPath: filepath.Join(dir, "photo.txt"), pdfPath: filepath.Join(dir, "photo.txt"), code: ImageOCRCodeOutputInvalid},
		{name: "text collides with input", textPath: input, code: ImageOCRCodeOutputInvalid},
		{name: "missing output dir", textPath: filepath.Join(missingDir, "photo.txt"), code: ImageOCRCodeOutputDirInvalid},
	}

	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			err := validateImageOCRRequest(input, testCase.textPath, testCase.pdfPath)
			if err == nil || err.Code != testCase.code {
				t.Fatalf("expected %s, got %v", testCase.code, err)
			}
		})
	}
}

func TestImageOCRRecognizeNormalizesEXIFOrientation(t *testing.T) {
	const storedWidth, storedHeight = 48, 32

	stored := encodeWatermarkQuadrantJPEG(t, storedWidth, storedHeight)
	cases := []struct {
		orientation int
		swapped     bool
	}{
		{orientation: 3},
		{orientation: 6, swapped: true},
		{orientation: 8, swapped: true},
	}

	for _, testCase := range cases {
		t.Run(fmt.Sprintf("orientation-%d", testCase.orientation), func(t *testing.T) {
			oriented := injectWatermarkEXIFOrientation(t, stored, byte(testCase.orientation))

			metadata, metaErr := bimg.NewImage(oriented).Metadata()
			if metaErr != nil || metadata.Orientation != testCase.orientation {
				t.Skipf("libvips build does not read the injected EXIF orientation (orientation=%d err=%v)", metadata.Orientation, metaErr)
			}

			dir := t.TempDir()
			input := filepath.Join(dir, "rotated.jpg")
			if err := os.WriteFile(input, oriented, 0o644); err != nil {
				t.Fatalf("write rotated fixture: %v", err)
			}
			textPath := filepath.Join(dir, "rotated.txt")

			var normalizedBytes []byte
			runner := &imageOCRStubRunner{onRun: func(_ context.Context, _ string, args []string) ([]byte, error) {
				normalized, readErr := os.ReadFile(args[0])
				if readErr != nil {
					return nil, fmt.Errorf("read normalized png: %w", readErr)
				}
				normalizedBytes = normalized

				return nil, imageOCRStubOutputs(args, "text\n", "%PDF-1.4\n")
			}}
			stubImageOCRCapabilities(t, imageOCRTestCapabilities("eng"))

			result, err := RecognizeImage(context.Background(), runner, input, textPath, "", ImageOCROptions{})
			if err != nil {
				t.Fatalf("expected recognition to succeed, got %v", err)
			}

			wantWidth, wantHeight := storedWidth, storedHeight
			if testCase.swapped {
				wantWidth, wantHeight = storedHeight, storedWidth
			}
			if result.Width != wantWidth || result.Height != wantHeight {
				t.Fatalf("expected the visual axes %dx%d, got %dx%d", wantWidth, wantHeight, result.Width, result.Height)
			}

			if len(normalizedBytes) == 0 {
				t.Fatal("expected the stub to capture the normalized PNG")
			}
			normalizedMetadata, metaErr := bimg.NewImage(normalizedBytes).Metadata()
			if metaErr != nil {
				t.Fatalf("read normalized metadata: %v", metaErr)
			}
			if normalizedMetadata.Orientation > 1 {
				t.Fatalf("expected the orientation tag to be stripped, got %d", normalizedMetadata.Orientation)
			}

			// The reference mapping comes from the EXIF specification via the
			// watermark fixture; it never goes through bimg's normalization.
			decoded, decodeErr := png.Decode(bytes.NewReader(normalizedBytes))
			if decodeErr != nil {
				t.Fatalf("decode normalized png: %v", decodeErr)
			}
			expected := watermarkDisplayColors(testCase.orientation)
			centers := [4]image.Point{
				{X: wantWidth / 4, Y: wantHeight / 4},
				{X: 3 * wantWidth / 4, Y: wantHeight / 4},
				{X: wantWidth / 4, Y: 3 * wantHeight / 4},
				{X: 3 * wantWidth / 4, Y: 3 * wantHeight / 4},
			}
			for index, center := range centers {
				got := watermarkAveragePatch(decoded, center, 4)
				if !watermarkNearColor(got, expected[index], 16) {
					t.Fatalf("quadrant %d: expected the EXIF mapping color %v, got %v", index, expected[index], got)
				}
			}
		})
	}
}

// writeImageOCRTextFixture renders large bold text on white so real Tesseract
// has an unambiguous target.
func writeImageOCRTextFixture(t *testing.T, path string) {
	t.Helper()

	const (
		width  = 1100
		height = 300
	)

	canvas := gg.NewContext(width, height)
	canvas.SetRGB(1, 1, 1)
	canvas.Clear()

	face, faceErr := watermarkFontFace(true, 110)
	if faceErr != nil {
		t.Fatalf("create fixture font face: %v", faceErr)
	}
	canvas.SetFontFace(face)
	canvas.SetRGB(0, 0, 0)
	canvas.DrawStringAnchored("FILEFORGE OCR", width/2, height/2, 0.5, 0.5)

	file, createErr := os.Create(path)
	if createErr != nil {
		t.Fatalf("create OCR fixture %s: %v", path, createErr)
	}
	if encodeErr := png.Encode(file, canvas.Image()); encodeErr != nil {
		_ = file.Close()
		t.Fatalf("encode OCR fixture %s: %v", path, encodeErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		t.Fatalf("close OCR fixture %s: %v", path, closeErr)
	}
}

// TestImageOCRRecognizeRealTesseract runs the real binary. It skips when the
// machine has no usable Tesseract runtime; Tesseract itself is never mocked.
func TestImageOCRRecognizeRealTesseract(t *testing.T) {
	capabilities := engine.DetectCapabilities(context.Background(), nil)
	if !ImageOCRAvailable(capabilities) {
		t.Skipf("local image OCR is unavailable: %s", ImageOCRUnavailableReason(capabilities))
	}

	dir := t.TempDir()
	input := filepath.Join(dir, "ocr-fixture.png")
	writeImageOCRTextFixture(t, input)
	textPath := filepath.Join(dir, "ocr-fixture.txt")

	result, err := RecognizeImage(context.Background(), nil, input, textPath, "", ImageOCROptions{
		Language: capabilities.RecommendedLanguage,
		DPI:      300,
	})
	if err != nil {
		t.Fatalf("expected real Tesseract recognition to succeed, got %v", err)
	}

	recognized, readErr := os.ReadFile(textPath)
	if readErr != nil {
		t.Fatalf("read recognized text: %v", readErr)
	}
	normalized := strings.Join(strings.Fields(strings.ToUpper(string(recognized))), " ")
	if !strings.Contains(normalized, "FILEFORGE OCR") {
		t.Fatalf("expected FILEFORGE OCR in the recognized text, got %q", string(recognized))
	}
	if result.TextCharacters == 0 {
		t.Fatal("expected a positive character count")
	}
	if result.Width != 1100 || result.Height != 300 {
		t.Fatalf("unexpected normalized dimensions %dx%d", result.Width, result.Height)
	}
}
