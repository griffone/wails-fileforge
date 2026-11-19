package engine

import (
	"archive/zip"
	"context"
	"errors"
	"net/url"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

type fakeRuntimeProbe struct {
	err error
}

func (p *fakeRuntimeProbe) Check(_ context.Context) error {
	return p.err
}

type fakeCommandRunner struct {
	run func(ctx context.Context, name string, args ...string) (CommandResult, error)
}

func (r *fakeCommandRunner) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	return r.run(ctx, name, args...)
}

func writeTestDOCX(t *testing.T, path string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}

	writer := zip.NewWriter(file)
	entry, err := writer.Create("word/settings.xml")
	if err != nil {
		t.Fatalf("create docx entry: %v", err)
	}
	if _, err := entry.Write([]byte("<w:settings/>")); err != nil {
		t.Fatalf("write docx entry: %v", err)
	}
	if err := writer.Close(); err != nil {
		t.Fatalf("close docx writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
}

func TestConvertDOCXFallbackSucceedsAfterFontPrimaryError(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeTestDOCX(t, inputPath)

	runner := &fakeCommandRunner{run: func(_ context.Context, name string, args ...string) (CommandResult, error) {
		switch name {
		case "libreoffice":
			return CommandResult{Stderr: "font not found: Calibri"}, nil
		case "pandoc":
			if len(args) < 3 {
				t.Fatalf("unexpected pandoc args: %v", args)
			}
			if err := os.WriteFile(args[2], []byte("%PDF-1.4 fallback"), 0o644); err != nil {
				t.Fatalf("write pandoc output: %v", err)
			}
			return CommandResult{}, nil
		default:
			t.Fatalf("unexpected command: %s", name)
			return CommandResult{}, nil
		}
	}}

	result, err := ConvertDOCX(context.Background(), &fakeRuntimeProbe{}, runner, ConvertDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("expected success after fallback, got error: %v", err)
	}
	if !result.FallbackUsed {
		t.Fatal("expected FallbackUsed to be true")
	}
	if result.EngineUsed != EnginePandoc {
		t.Fatalf("expected engine %q, got %q", EnginePandoc, result.EngineUsed)
	}
	if result.PrimaryError == "" {
		t.Fatal("expected PrimaryError to be kept as diagnostic")
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("expected pandoc output to exist: %v", statErr)
	}
}

func TestConvertDOCXFallbackSucceedsAfterPrimaryExecutionFailure(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeTestDOCX(t, inputPath)

	runner := &fakeCommandRunner{run: func(_ context.Context, name string, args ...string) (CommandResult, error) {
		switch name {
		case "libreoffice":
			return CommandResult{}, errors.New("libreoffice crashed")
		case "pandoc":
			if err := os.WriteFile(args[2], []byte("%PDF-1.4 fallback"), 0o644); err != nil {
				t.Fatalf("write pandoc output: %v", err)
			}
			return CommandResult{}, nil
		default:
			t.Fatalf("unexpected command: %s", name)
			return CommandResult{}, nil
		}
	}}

	result, err := ConvertDOCX(context.Background(), &fakeRuntimeProbe{}, runner, ConvertDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("expected success after fallback, got error: %v", err)
	}
	if !result.FallbackUsed || result.EngineUsed != EnginePandoc {
		t.Fatalf("expected pandoc fallback result, got %+v", result)
	}
	if result.PrimaryError == "" {
		t.Fatal("expected PrimaryError to be kept as diagnostic")
	}
}

func TestConvertDOCXFailsWhenBothEnginesFail(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeTestDOCX(t, inputPath)

	runner := &fakeCommandRunner{run: func(_ context.Context, name string, _ ...string) (CommandResult, error) {
		return CommandResult{}, errors.New(name + " failed")
	}}

	result, err := ConvertDOCX(context.Background(), &fakeRuntimeProbe{}, runner, ConvertDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
	})
	if err == nil {
		t.Fatal("expected error when both engines fail")
	}
	if result.FallbackError == "" {
		t.Fatal("expected FallbackError to be reported")
	}
	var docxErr *DOCXError
	if !errors.As(err, &docxErr) || docxErr.Code != ErrorCodeDocxFallbackExecution {
		t.Fatalf("expected fallback execution error, got %v", err)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no output file, got %v", statErr)
	}
}

func TestLibreOfficeConvertTo(t *testing.T) {
	tests := []struct {
		name string
		pdfa bool
		want string
	}{
		{name: "default pdf", pdfa: false, want: "pdf"},
		{name: "pdfa filter", pdfa: true, want: `pdf:writer_pdf_Export:{"SelectPdfVersion":{"type":"long","value":"1"}}`},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := libreOfficeConvertTo(test.pdfa); got != test.want {
				t.Fatalf("expected %q, got %q", test.want, got)
			}
		})
	}
}

// libreOfficePDFRunner simulates a successful LibreOffice run: it inspects the
// --convert-to value and writes the produced PDF into the requested --outdir.
func libreOfficePDFRunner(t *testing.T, wantConvertTo string) *fakeCommandRunner {
	t.Helper()

	return &fakeCommandRunner{run: func(_ context.Context, name string, args ...string) (CommandResult, error) {
		if name != "libreoffice" {
			t.Fatalf("expected libreoffice, got %q", name)
		}

		convertTo, outDir, inputPath := "", "", ""
		for index := 0; index < len(args); index++ {
			switch args[index] {
			case "--convert-to":
				convertTo = args[index+1]
				index++
			case "--outdir":
				outDir = args[index+1]
				index++
			}
		}
		if len(args) > 0 {
			inputPath = args[len(args)-1]
		}

		if convertTo != wantConvertTo {
			t.Fatalf("expected --convert-to %q, got %q (args: %v)", wantConvertTo, convertTo, args)
		}
		if outDir == "" {
			t.Fatalf("expected --outdir in args, got %v", args)
		}

		converted := filepath.Join(outDir, strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))+".pdf")
		if err := os.WriteFile(converted, []byte("%PDF-1.7 libreoffice"), 0o644); err != nil {
			t.Fatalf("write libreoffice output: %v", err)
		}

		return CommandResult{}, nil
	}}
}

func TestConvertDOCXPDFAUsesLibreOfficePDFAFilter(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeTestDOCX(t, inputPath)

	runner := libreOfficePDFRunner(t, libreOfficePDFAFilter)

	result, err := ConvertDOCX(context.Background(), &fakeRuntimeProbe{}, runner, ConvertDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
		PDFA:       true,
	})
	if err != nil {
		t.Fatalf("expected pdf/a conversion to succeed, got %v", err)
	}
	if result.EngineUsed != EngineLibreOffice || result.FallbackUsed {
		t.Fatalf("expected a libreoffice-only result, got %+v", result)
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("expected converted output to exist: %v", statErr)
	}
}

func TestConvertDOCXPlainUsesPlainPDFFilter(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeTestDOCX(t, inputPath)

	runner := libreOfficePDFRunner(t, "pdf")

	if _, err := ConvertDOCX(context.Background(), &fakeRuntimeProbe{}, runner, ConvertDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
	}); err != nil {
		t.Fatalf("expected plain conversion to succeed, got %v", err)
	}
}

func TestConvertDOCXPDFADoesNotFallbackToPandoc(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeTestDOCX(t, inputPath)

	runner := &fakeCommandRunner{run: func(_ context.Context, name string, args ...string) (CommandResult, error) {
		if name != "libreoffice" {
			t.Fatalf("pdf/a must never run the %q fallback, got args %v", name, args)
		}
		return CommandResult{}, errors.New("libreoffice crashed")
	}}

	result, err := ConvertDOCX(context.Background(), &fakeRuntimeProbe{}, runner, ConvertDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
		PDFA:       true,
	})
	if err == nil {
		t.Fatal("expected a pdf/a unsupported error")
	}

	var docxErr *DOCXError
	if !errors.As(err, &docxErr) || docxErr.Code != ErrorCodeDocxPDFAUnsupported {
		t.Fatalf("expected %s, got %v", ErrorCodeDocxPDFAUnsupported, err)
	}
	if result.FallbackUsed {
		t.Fatal("expected FallbackUsed to stay false for pdf/a")
	}
	if result.PrimaryError == "" {
		t.Fatal("expected PrimaryError to keep the libreoffice diagnostic")
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no non-archival output file, got %v", statErr)
	}
}

type countingRuntimeProbe struct {
	err   error
	calls int
}

func (p *countingRuntimeProbe) Check(_ context.Context) error {
	p.calls++
	return p.err
}

func TestRunLibreOfficeUsesIsolatedProfile(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeTestDOCX(t, inputPath)

	var profileURL string
	var profilePath string
	runner := &fakeCommandRunner{run: func(_ context.Context, name string, args ...string) (CommandResult, error) {
		if name != "libreoffice" {
			t.Fatalf("expected libreoffice, got %q", name)
		}

		outDir := ""
		for index := 0; index < len(args); index++ {
			switch {
			case strings.HasPrefix(args[index], "-env:UserInstallation="):
				profileURL = strings.TrimPrefix(args[index], "-env:UserInstallation=")
			case args[index] == "--outdir":
				outDir = args[index+1]
				index++
			}
		}
		if profileURL == "" {
			t.Fatalf("expected a per-conversion UserInstallation profile, got %v", args)
		}

		parsed, err := url.Parse(profileURL)
		if err != nil {
			t.Fatalf("parse profile URL %q: %v", profileURL, err)
		}
		if parsed.Scheme != "file" {
			t.Fatalf("expected a file URL, got %q", profileURL)
		}
		profilePath = parsed.Path
		if _, statErr := os.Stat(profilePath); statErr != nil {
			t.Fatalf("expected the profile dir to exist during conversion: %v", statErr)
		}

		converted := filepath.Join(outDir, strings.TrimSuffix(filepath.Base(inputPath), filepath.Ext(inputPath))+".pdf")
		if err := os.WriteFile(converted, []byte("%PDF-1.7 libreoffice"), 0o644); err != nil {
			t.Fatalf("write libreoffice output: %v", err)
		}

		return CommandResult{}, nil
	}}

	if err := runLibreOffice(context.Background(), runner, inputPath, outputPath, false); err != nil {
		t.Fatalf("expected libreoffice conversion to succeed, got %v", err)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected converted output: %v", err)
	}
	if profilePath == "" {
		t.Fatal("expected a captured profile path")
	}
	if _, err := os.Stat(profilePath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected the profile dir to be removed, got %v", err)
	}
}

func TestRunPandocFailureRemovesTemp(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeTestDOCX(t, inputPath)

	runner := &fakeCommandRunner{run: func(_ context.Context, name string, _ ...string) (CommandResult, error) {
		if name != "pandoc" {
			t.Fatalf("expected pandoc, got %q", name)
		}
		return CommandResult{Stderr: "pandoc exploded"}, errors.New("pandoc exploded")
	}}

	if err := runPandoc(context.Background(), runner, inputPath, outputPath); err == nil {
		t.Fatal("expected pandoc to fail")
	}
	if _, err := os.Stat(outputPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("expected no final output, got %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read output dir: %v", err)
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".pdf") {
			t.Fatalf("expected the temp pdf to be removed, found %q", entry.Name())
		}
	}
}

func TestConvertDOCXSkipsRuntimeCheckWhenRequested(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeTestDOCX(t, inputPath)

	probe := &countingRuntimeProbe{err: errors.New("runtime missing")}
	runner := libreOfficePDFRunner(t, "pdf")

	result, err := ConvertDOCX(context.Background(), probe, runner, ConvertDOCXRequest{
		InputPath:        inputPath,
		OutputPath:       outputPath,
		SkipRuntimeCheck: true,
	})
	if err != nil {
		t.Fatalf("expected conversion to skip the runtime probe, got %v", err)
	}
	if result.EngineUsed != EngineLibreOffice {
		t.Fatalf("expected libreoffice output, got %+v", result)
	}
	if probe.calls != 0 {
		t.Fatalf("expected no runtime probe calls, got %d", probe.calls)
	}
}

func TestConvertDOCXSkipsProtectedScanWhenRequested(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	if err := os.WriteFile(inputPath, []byte("not a valid zip package"), 0o644); err != nil {
		t.Fatalf("write invalid docx: %v", err)
	}
	outputPath := filepath.Join(dir, "output.pdf")

	runner := libreOfficePDFRunner(t, "pdf")

	// The default path rejects the invalid package before conversion runs.
	if _, err := ConvertDOCX(context.Background(), &fakeRuntimeProbe{}, runner, ConvertDOCXRequest{
		InputPath:  inputPath,
		OutputPath: filepath.Join(dir, "rejected.pdf"),
	}); err == nil {
		t.Fatal("expected the default protection scan to reject an invalid package")
	}

	result, err := ConvertDOCX(context.Background(), &fakeRuntimeProbe{}, runner, ConvertDOCXRequest{
		InputPath:          inputPath,
		OutputPath:         outputPath,
		SkipProtectedCheck: true,
	})
	if err != nil {
		t.Fatalf("expected conversion to skip the protection scan, got %v", err)
	}
	if result.EngineUsed != EngineLibreOffice {
		t.Fatalf("expected libreoffice output, got %+v", result)
	}
	if _, err := os.Stat(outputPath); err != nil {
		t.Fatalf("expected converted output: %v", err)
	}
}

func TestConvertDOCXDefaultChecksStillRun(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeTestDOCX(t, inputPath)

	probe := &countingRuntimeProbe{}
	runner := libreOfficePDFRunner(t, "pdf")

	if _, err := ConvertDOCX(context.Background(), probe, runner, ConvertDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
	}); err != nil {
		t.Fatalf("expected default conversion to succeed, got %v", err)
	}
	if probe.calls != 1 {
		t.Fatalf("expected exactly one default runtime probe call, got %d", probe.calls)
	}
}
