package engine

import (
	"archive/zip"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/jung-kurt/gofpdf"

	"fileforge-desktop/internal/toolchain"
)

const pdfToDOCXTestDocumentXML = `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:b/><w:t>Hello</w:t></w:r></w:p><w:tbl/></w:body></w:document>`

func TestValidatePDFToDOCXRequest(t *testing.T) {
	tests := []struct {
		name     string
		mutate   func(t *testing.T, dir string, req *PDFToDOCXRequest)
		wantCode string
	}{
		{
			name:   "valid request",
			mutate: func(*testing.T, string, *PDFToDOCXRequest) {},
		},
		{
			name: "missing input path",
			mutate: func(_ *testing.T, _ string, req *PDFToDOCXRequest) {
				req.InputPath = ""
			},
			wantCode: PDFToDOCXErrorInvalidInput,
		},
		{
			name: "input is not a pdf",
			mutate: func(t *testing.T, dir string, req *PDFToDOCXRequest) {
				textPath := filepath.Join(dir, "notes.txt")
				if err := os.WriteFile(textPath, []byte("notes"), 0o644); err != nil {
					t.Fatalf("write text input: %v", err)
				}
				req.InputPath = textPath
			},
			wantCode: PDFToDOCXErrorInvalidInput,
		},
		{
			name: "input does not exist",
			mutate: func(_ *testing.T, dir string, req *PDFToDOCXRequest) {
				req.InputPath = filepath.Join(dir, "missing.pdf")
			},
			wantCode: PDFToDOCXErrorInvalidInput,
		},
		{
			name: "output equals input",
			mutate: func(_ *testing.T, _ string, req *PDFToDOCXRequest) {
				req.OutputPath = req.InputPath
			},
			wantCode: PDFToDOCXErrorInvalidInput,
		},
		{
			name: "output is not a docx",
			mutate: func(_ *testing.T, dir string, req *PDFToDOCXRequest) {
				req.OutputPath = filepath.Join(dir, "result.pdf")
			},
			wantCode: PDFToDOCXErrorInvalidInput,
		},
		{
			name: "output already exists",
			mutate: func(t *testing.T, dir string, req *PDFToDOCXRequest) {
				outputPath := filepath.Join(dir, "existing.docx")
				if err := os.WriteFile(outputPath, []byte("existing"), 0o644); err != nil {
					t.Fatalf("write existing output: %v", err)
				}
				req.OutputPath = outputPath
			},
			wantCode: PDFToDOCXErrorOutputFailed,
		},
		{
			name: "output directory does not exist",
			mutate: func(_ *testing.T, dir string, req *PDFToDOCXRequest) {
				req.OutputPath = filepath.Join(dir, "missing", "result.docx")
			},
			wantCode: PDFToDOCXErrorOutputFailed,
		},
		{
			name: "python path is empty",
			mutate: func(_ *testing.T, _ string, req *PDFToDOCXRequest) {
				req.PythonPath = ""
			},
			wantCode: PDFToDOCXErrorRuntimeUnavailable,
		},
		{
			name: "python path does not exist",
			mutate: func(_ *testing.T, dir string, req *PDFToDOCXRequest) {
				req.PythonPath = filepath.Join(dir, "missing-python")
			},
			wantCode: PDFToDOCXErrorRuntimeUnavailable,
		},
		{
			name: "python path is a directory",
			mutate: func(_ *testing.T, dir string, req *PDFToDOCXRequest) {
				req.PythonPath = dir
			},
			wantCode: PDFToDOCXErrorRuntimeUnavailable,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			inputPath := filepath.Join(dir, "source.pdf")
			if err := os.WriteFile(inputPath, []byte("%PDF-1.4\n%%EOF\n"), 0o644); err != nil {
				t.Fatalf("write pdf input: %v", err)
			}
			pythonPath := filepath.Join(dir, "python3")
			if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatalf("write python fixture: %v", err)
			}

			req := PDFToDOCXRequest{
				InputPath:  inputPath,
				OutputPath: filepath.Join(dir, "result.docx"),
				PythonPath: pythonPath,
			}
			test.mutate(t, dir, &req)

			validationErr := ValidatePDFToDOCXRequest(req)
			if test.wantCode == "" {
				if validationErr != nil {
					t.Fatalf("expected a valid request, got %v", validationErr)
				}
				return
			}

			if validationErr == nil {
				t.Fatalf("expected %s, got no error", test.wantCode)
			}
			if validationErr.Code != test.wantCode {
				t.Fatalf("expected code %s, got %s (%v)", test.wantCode, validationErr.Code, validationErr)
			}
		})
	}
}

func TestPDFToDOCXProgressParser(t *testing.T) {
	steps := []struct {
		name        string
		line        string
		wantStage   string
		wantPercent int
	}{
		{
			name:        "start",
			line:        "LOG \x1b[1;36mStart to convert /tmp/report.pdf\x1b[0m",
			wantStage:   PDFToDOCXStageParse,
			wantPercent: 5,
		},
		{
			name:        "opening",
			line:        "\x1b[1;36m[1/4] Opening document...\x1b[0m",
			wantStage:   PDFToDOCXStageParse,
			wantPercent: 8,
		},
		{
			name:        "analyzing",
			line:        "\x1b[1;36m[2/4] Analyzing document...\x1b[0m",
			wantStage:   PDFToDOCXStageParse,
			wantPercent: 12,
		},
		{
			name:        "parsing pages",
			line:        "\x1b[1;36m[3/4] Parsing pages...\x1b[0m",
			wantStage:   PDFToDOCXStageParse,
			wantPercent: 15,
		},
		{
			name:        "parsing page 1 of 2",
			line:        "(1/2) Page 1",
			wantStage:   PDFToDOCXStageParse,
			wantPercent: 32,
		},
		{
			name:        "parsing page 2 of 2",
			line:        "(2/2) Page 2",
			wantStage:   PDFToDOCXStageParse,
			wantPercent: 50,
		},
		{
			name:        "creating pages",
			line:        "\x1b[1;36m[4/4] Creating pages...\x1b[0m",
			wantStage:   PDFToDOCXStageWrite,
			wantPercent: 55,
		},
		{
			name:        "creating page 2 of 2",
			line:        "(2/2) Page 2",
			wantStage:   PDFToDOCXStageWrite,
			wantPercent: 90,
		},
		{
			name:        "terminated",
			line:        "Terminated in 0.31s.",
			wantStage:   PDFToDOCXStageWrite,
			wantPercent: 95,
		},
	}

	parser := newPDFToDOCXProgressParser()
	for _, step := range steps {
		t.Run(step.name, func(t *testing.T) {
			event, ok := parser.observe(step.line)
			if !ok {
				t.Fatalf("expected a progress event for %q", step.line)
			}
			if event.Stage != step.wantStage || event.Percent != step.wantPercent {
				t.Fatalf("expected %s at %d%%, got %s at %d%%", step.wantStage, step.wantPercent, event.Stage, event.Percent)
			}
			if strings.TrimSpace(event.Message) == "" {
				t.Fatalf("expected a progress message for %q", step.line)
			}
		})
	}

	if _, ok := parser.observe("Terminated in 0.31s."); ok {
		t.Fatal("expected duplicate percentages to be suppressed")
	}
	if _, ok := parser.observe("unrelated pdf2docx output"); ok {
		t.Fatal("expected unrelated lines to be ignored")
	}
}

func TestReadPDFToDOCXStats(t *testing.T) {
	stats, statsErr := readPDFToDOCXStats(writePDFToDOCXTestPackage(t, pdfToDOCXTestDocumentXML))
	if statsErr != nil {
		t.Fatalf("expected valid stats, got %v", statsErr)
	}
	if stats.Paragraphs != 1 || stats.Runs != 1 || stats.BoldRuns != 1 || stats.Tables != 1 {
		t.Fatalf("unexpected structure stats: %+v", stats)
	}
	if stats.Images != 0 {
		t.Fatalf("expected no images, got %d", stats.Images)
	}
	if stats.TextCharacters != 5 {
		t.Fatalf("expected 5 text characters, got %d", stats.TextCharacters)
	}
}

func TestReadPDFToDOCXStatsDisabledBoldAndDrawing(t *testing.T) {
	documentXML := `<w:document><w:body><w:p><w:r><w:b w:val="0"/><w:t>Off</w:t></w:r><w:r><w:drawing/></w:r></w:p></w:body></w:document>`

	stats, statsErr := readPDFToDOCXStats(writePDFToDOCXTestPackage(t, documentXML))
	if statsErr != nil {
		t.Fatalf("expected valid stats, got %v", statsErr)
	}
	if stats.BoldRuns != 0 {
		t.Fatalf("expected w:val=0 to disable bold, got %d bold runs", stats.BoldRuns)
	}
	if stats.Images != 1 {
		t.Fatalf("expected one drawing, got %d", stats.Images)
	}
	if stats.TextCharacters != 3 {
		t.Fatalf("expected 3 text characters, got %d", stats.TextCharacters)
	}
}

func TestReadPDFToDOCXStatsRejectsInvalidPackages(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, dir string) string
	}{
		{
			name: "not a zip file",
			setup: func(t *testing.T, dir string) string {
				path := filepath.Join(dir, "broken.docx")
				if err := os.WriteFile(path, []byte("not a zip"), 0o644); err != nil {
					t.Fatalf("write broken docx: %v", err)
				}
				return path
			},
		},
		{
			name: "missing document part",
			setup: func(t *testing.T, dir string) string {
				return writePDFToDOCXTestZip(t, filepath.Join(dir, "missing.docx"), "docProps/app.xml", `<Properties/>`)
			},
		},
		{
			name: "malformed document xml",
			setup: func(t *testing.T, dir string) string {
				return writePDFToDOCXTestPackage(t, `<w:document><w:body>`)
			},
		},
		{
			name: "empty document part",
			setup: func(t *testing.T, dir string) string {
				return writePDFToDOCXTestPackage(t, "")
			},
		},
		{
			name: "no paragraphs",
			setup: func(t *testing.T, dir string) string {
				return writePDFToDOCXTestPackage(t, `<w:document><w:body/></w:document>`)
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := test.setup(t, t.TempDir())
			if _, statsErr := readPDFToDOCXStats(path); statsErr == nil {
				t.Fatal("expected the DOCX package to be rejected")
			}
		})
	}
}

// fakeDOCXCommandRunner streams canned log lines and optionally writes a valid
// DOCX package to the path the engine passes as the last argument.
type fakeDOCXCommandRunner struct {
	lines       []string
	documentXML string
	stderr      string
	err         error
	cancel      context.CancelFunc

	dir  string
	args []string
}

func (r *fakeDOCXCommandRunner) RunDOCX(_ context.Context, dir, name string, args []string, onLine func(string)) (string, error) {
	r.dir = dir
	r.args = append([]string(nil), args...)

	for _, line := range r.lines {
		if onLine != nil {
			onLine(line)
		}
	}

	if r.cancel != nil {
		r.cancel()
	}
	if r.err != nil {
		return r.stderr, r.err
	}
	if r.documentXML != "" {
		if writeErr := writePDFToDOCXTestZipFile(args[len(args)-1], pdfToDOCXRootElement, r.documentXML); writeErr != nil {
			return r.stderr, writeErr
		}
	}

	return r.stderr, nil
}

func TestConvertPDFToDOCXWithRunner(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(inputPath, []byte("%PDF-1.4\n%%EOF\n"), 0o644); err != nil {
		t.Fatalf("write pdf input: %v", err)
	}
	outputPath := filepath.Join(dir, "result.docx")
	pythonPath := filepath.Join(dir, "python3")
	if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write python fixture: %v", err)
	}

	runner := &fakeDOCXCommandRunner{
		lines: []string{
			"Start to convert " + inputPath,
			"\x1b[1;36m[1/4] Opening document...\x1b[0m",
			"\x1b[1;36m[2/4] Analyzing document...\x1b[0m",
			"\x1b[1;36m[3/4] Parsing pages...\x1b[0m",
			"(1/1) Page 1",
			"\x1b[1;36m[4/4] Creating pages...\x1b[0m",
			"(1/1) Page 1",
			"Terminated in 0.10s.",
		},
		documentXML: pdfToDOCXTestDocumentXML,
	}

	var progress []PDFToDOCXProgressEvent
	result, conversionErr := ConvertPDFToDOCX(context.Background(), runner, PDFToDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
		PythonPath: pythonPath,
	}, func(event PDFToDOCXProgressEvent) {
		progress = append(progress, event)
	})
	if conversionErr != nil {
		t.Fatalf("expected the conversion to succeed, got %v", conversionErr)
	}

	if result.OutputPath != outputPath {
		t.Fatalf("expected output %s, got %s", outputPath, result.OutputPath)
	}
	if result.Stats.Paragraphs != 1 || result.Stats.BoldRuns != 1 || result.Stats.Tables != 1 || result.Stats.TextCharacters != 5 {
		t.Fatalf("unexpected stats: %+v", result.Stats)
	}

	info, statErr := os.Stat(outputPath)
	if statErr != nil || !info.Mode().IsRegular() || info.Size() == 0 {
		t.Fatalf("expected a published DOCX, got %v", statErr)
	}
	if info.Mode().Perm() != 0o644 {
		t.Fatalf("expected 0644 permissions, got %o", info.Mode().Perm())
	}

	if len(progress) == 0 {
		t.Fatal("expected progress events")
	}
	if last := progress[len(progress)-1]; last.Percent != 100 || last.Stage != PDFToDOCXStageWrite {
		t.Fatalf("expected the final 100%% write event, got %+v", last)
	}
	stages := make(map[string]bool, 2)
	for _, event := range progress {
		stages[event.Stage] = true
		if event.Percent < 0 || event.Percent > 100 {
			t.Fatalf("progress percent out of range: %+v", event)
		}
	}
	if !stages[PDFToDOCXStageParse] || !stages[PDFToDOCXStageWrite] {
		t.Fatalf("expected parse and write stages, got %v", stages)
	}

	assertNoPDFToDOCXTempFiles(t, dir)

	if runner.dir == "" {
		t.Fatal("expected the runner to receive a workspace directory")
	}
	if len(runner.args) != 3 {
		t.Fatalf("expected bridge, input and output arguments, got %v", runner.args)
	}
	if filepath.Base(runner.args[0]) != pdfToDOCXBridgeFileName {
		t.Fatalf("expected the embedded bridge path, got %s", runner.args[0])
	}
	if !strings.HasSuffix(runner.args[2], ".docx") || runner.args[2] == outputPath {
		t.Fatalf("expected a hidden temp output path, got %s", runner.args[2])
	}
}

func TestConvertPDFToDOCXFailureLeavesNoOutput(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(inputPath, []byte("%PDF-1.4\n%%EOF\n"), 0o644); err != nil {
		t.Fatalf("write pdf input: %v", err)
	}
	outputPath := filepath.Join(dir, "result.docx")
	pythonPath := filepath.Join(dir, "python3")
	if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write python fixture: %v", err)
	}

	runner := &fakeDOCXCommandRunner{
		lines:  []string{"Start to convert"},
		stderr: "Traceback (most recent call last):\nRuntimeError: broken pdf",
		err:    errors.New("exit status 1"),
	}

	_, conversionErr := ConvertPDFToDOCX(context.Background(), runner, PDFToDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
		PythonPath: pythonPath,
	}, nil)
	if conversionErr == nil {
		t.Fatal("expected the conversion to fail")
	}
	if conversionErr.Code != PDFToDOCXErrorConversionFailed {
		t.Fatalf("expected %s, got %s", PDFToDOCXErrorConversionFailed, conversionErr.Code)
	}
	if stderr, ok := conversionErr.Details["stderr"].(string); !ok || !strings.Contains(stderr, "RuntimeError") {
		t.Fatalf("expected the stderr tail in the details, got %v", conversionErr.Details)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no published output, got %v", statErr)
	}

	assertNoPDFToDOCXTempFiles(t, dir)
}

func TestConvertPDFToDOCXRejectsInvalidOutputPackage(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(inputPath, []byte("%PDF-1.4\n%%EOF\n"), 0o644); err != nil {
		t.Fatalf("write pdf input: %v", err)
	}
	outputPath := filepath.Join(dir, "result.docx")
	pythonPath := filepath.Join(dir, "python3")
	if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write python fixture: %v", err)
	}

	runner := &fakeDOCXCommandRunner{documentXML: `<w:document><w:body>`}

	_, conversionErr := ConvertPDFToDOCX(context.Background(), runner, PDFToDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
		PythonPath: pythonPath,
	}, nil)
	if conversionErr == nil || conversionErr.Code != PDFToDOCXErrorOutputFailed {
		t.Fatalf("expected %s, got %v", PDFToDOCXErrorOutputFailed, conversionErr)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no published output, got %v", statErr)
	}

	assertNoPDFToDOCXTempFiles(t, dir)
}

func TestConvertPDFToDOCXCancellation(t *testing.T) {
	tests := []struct {
		name   string
		runner *fakeDOCXCommandRunner
		cancel bool
	}{
		{
			name: "runner reports cancellation",
			runner: &fakeDOCXCommandRunner{
				err: context.Canceled,
			},
		},
		{
			name: "context is cancelled while running",
			runner: &fakeDOCXCommandRunner{
				err: errors.New("killed"),
			},
			cancel: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			inputPath := filepath.Join(dir, "source.pdf")
			if err := os.WriteFile(inputPath, []byte("%PDF-1.4\n%%EOF\n"), 0o644); err != nil {
				t.Fatalf("write pdf input: %v", err)
			}
			outputPath := filepath.Join(dir, "result.docx")
			pythonPath := filepath.Join(dir, "python3")
			if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
				t.Fatalf("write python fixture: %v", err)
			}

			ctx := context.Background()
			if test.cancel {
				cancelCtx, cancel := context.WithCancel(ctx)
				ctx = cancelCtx
				test.runner.cancel = cancel
			}

			_, conversionErr := ConvertPDFToDOCX(ctx, test.runner, PDFToDOCXRequest{
				InputPath:  inputPath,
				OutputPath: outputPath,
				PythonPath: pythonPath,
			}, nil)
			if conversionErr == nil || conversionErr.Code != PDFToDOCXErrorCancelled {
				t.Fatalf("expected %s, got %v", PDFToDOCXErrorCancelled, conversionErr)
			}

			assertNoPDFToDOCXTempFiles(t, dir)
		})
	}
}

func TestExecDOCXCommandRunnerStreamsOutput(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the streaming runner test uses a POSIX shell")
	}
	shellPath, lookErr := exec.LookPath("sh")
	if lookErr != nil {
		t.Skip("sh is not available")
	}

	runner := &ExecDOCXCommandRunner{}
	var lines []string
	stderr, runErr := runner.RunDOCX(context.Background(), t.TempDir(), shellPath, []string{
		"-c", "printf 'one\\ntwo\\n'; printf 'oops\\n' >&2",
	}, func(line string) {
		lines = append(lines, line)
	})
	if runErr != nil {
		t.Fatalf("expected the command to succeed, got %v", runErr)
	}
	if strings.Join(lines, ",") != "one,two" {
		t.Fatalf("unexpected stdout lines: %v", lines)
	}
	if !strings.Contains(stderr, "oops") {
		t.Fatalf("expected the stderr tail, got %q", stderr)
	}

	stderr, runErr = runner.RunDOCX(context.Background(), t.TempDir(), shellPath, []string{
		"-c", "printf 'first\\nbad\\n' >&2; exit 3",
	}, nil)
	if runErr == nil {
		t.Fatal("expected the failing command to return an error")
	}
	if strings.Join(strings.Split(stderr, "\n"), ",") != "first,bad" {
		t.Fatalf("unexpected stderr tail: %q", stderr)
	}
}

func TestConvertPDFToDOCXRuntimeNotFound(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "source.pdf")
	if err := os.WriteFile(inputPath, []byte("%PDF-1.4\n%%EOF\n"), 0o644); err != nil {
		t.Fatalf("write pdf input: %v", err)
	}
	pythonPath := filepath.Join(dir, "python3")
	if err := os.WriteFile(pythonPath, []byte("#!/bin/sh\n"), 0o755); err != nil {
		t.Fatalf("write python fixture: %v", err)
	}

	runner := &fakeDOCXCommandRunner{
		err: &exec.Error{Name: pythonPath, Err: exec.ErrNotFound},
	}

	_, conversionErr := ConvertPDFToDOCX(context.Background(), runner, PDFToDOCXRequest{
		InputPath:  inputPath,
		OutputPath: filepath.Join(dir, "result.docx"),
		PythonPath: pythonPath,
	}, nil)
	if conversionErr == nil || conversionErr.Code != PDFToDOCXErrorRuntimeUnavailable {
		t.Fatalf("expected %s, got %v", PDFToDOCXErrorRuntimeUnavailable, conversionErr)
	}
}

// TestConvertPDFToDOCXReal converts a generated PDF with the managed virtual
// environment. It is skipped when pdf2docx is not installed.
func TestConvertPDFToDOCXReal(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping the real pdf2docx conversion in short mode")
	}

	dataDir, dataErr := toolchain.DataDir()
	if dataErr != nil {
		t.Skipf("the application data directory is unavailable: %v", dataErr)
	}
	pythonPath, available := toolchain.ManagedPDF2DOCXPythonPath(dataDir)
	if !available {
		t.Skip("the managed pdf2docx virtual environment is not installed")
	}

	dir := t.TempDir()
	imagePath := filepath.Join(dir, "logo.png")
	writePDFToDOCXTestImage(t, imagePath)
	inputPath := filepath.Join(dir, "report.pdf")
	writePDFToDOCXTestPDF(t, inputPath, imagePath)
	outputPath := filepath.Join(dir, "report.docx")

	result, conversionErr := ConvertPDFToDOCX(context.Background(), &ExecDOCXCommandRunner{}, PDFToDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
		PythonPath: pythonPath,
	}, nil)
	if conversionErr != nil {
		t.Fatalf("expected the real conversion to succeed, got %v", conversionErr)
	}

	if result.OutputPath != outputPath {
		t.Fatalf("expected output %s, got %s", outputPath, result.OutputPath)
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("expected the DOCX to exist: %v", statErr)
	}
	if result.Stats.TextCharacters <= 0 {
		t.Fatalf("expected text in the generated DOCX, got %+v", result.Stats)
	}
	if result.Stats.BoldRuns <= 0 {
		t.Fatalf("expected bold runs in the generated DOCX, got %+v", result.Stats)
	}
	if result.Stats.Tables <= 0 {
		t.Fatalf("expected a table in the generated DOCX, got %+v", result.Stats)
	}
	if result.Stats.Images <= 0 {
		t.Fatalf("expected an image in the generated DOCX, got %+v", result.Stats)
	}

	t.Logf("real pdf2docx stats: %+v", result.Stats)
}

func writePDFToDOCXTestPackage(t *testing.T, documentXML string) string {
	t.Helper()

	return writePDFToDOCXTestZip(t, filepath.Join(t.TempDir(), "document.docx"), pdfToDOCXRootElement, documentXML)
}

func writePDFToDOCXTestZip(t *testing.T, path, entryName, content string) string {
	t.Helper()

	if err := writePDFToDOCXTestZipFile(path, entryName, content); err != nil {
		t.Fatalf("write docx fixture %s: %v", path, err)
	}

	return path
}

func writePDFToDOCXTestZipFile(path, entryName, content string) error {
	file, createErr := os.Create(path)
	if createErr != nil {
		return createErr
	}

	writer := zip.NewWriter(file)
	entry, entryErr := writer.Create(entryName)
	if entryErr != nil {
		_ = writer.Close()
		_ = file.Close()
		return entryErr
	}
	if _, writeErr := entry.Write([]byte(content)); writeErr != nil {
		_ = writer.Close()
		_ = file.Close()
		return writeErr
	}
	if closeErr := writer.Close(); closeErr != nil {
		_ = file.Close()
		return closeErr
	}

	return file.Close()
}

func assertNoPDFToDOCXTempFiles(t *testing.T, dir string) {
	t.Helper()

	matches, globErr := filepath.Glob(filepath.Join(dir, ".fileforge-docx-*"))
	if globErr != nil {
		t.Fatalf("glob temp files: %v", globErr)
	}
	if len(matches) != 0 {
		t.Fatalf("expected no temp files, got %v", matches)
	}
}

func writePDFToDOCXTestImage(t *testing.T, path string) {
	t.Helper()

	const width, height = 96, 48
	canvas := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			canvas.Set(x, y, color.RGBA{R: 30, G: uint8(x * 2), B: uint8(y * 4), A: 255})
		}
	}

	file, createErr := os.Create(path)
	if createErr != nil {
		t.Fatalf("create image fixture %s: %v", path, createErr)
	}
	if encodeErr := png.Encode(file, canvas); encodeErr != nil {
		_ = file.Close()
		t.Fatalf("encode image fixture %s: %v", path, encodeErr)
	}
	if closeErr := file.Close(); closeErr != nil {
		t.Fatalf("close image fixture %s: %v", path, closeErr)
	}
}

func writePDFToDOCXTestPDF(t *testing.T, path, imagePath string) {
	t.Helper()

	pdf := gofpdf.New("P", "mm", "A4", "")
	pdf.SetMargins(18, 18, 18)
	pdf.AddPage()

	pdf.SetFont("Helvetica", "B", 20)
	pdf.CellFormat(0, 12, "Quarterly report", "", 1, "L", false, 0, "")
	pdf.SetFont("Helvetica", "", 11)
	pdf.MultiCell(0, 6, "This fixture exercises bold text, a paragraph, a bordered table and an inline image.", "", "L", false)
	pdf.Ln(4)

	widths := []float64{70, 40, 40}
	pdf.SetFont("Helvetica", "B", 11)
	for index, header := range []string{"Item", "Quantity", "Price"} {
		pdf.CellFormat(widths[index], 8, header, "1", 0, "L", false, 0, "")
	}
	pdf.Ln(-1)

	pdf.SetFont("Helvetica", "", 11)
	for _, row := range [][]string{
		{"Widget", "2", "10.00"},
		{"Gadget", "1", "25.00"},
		{"Cable", "4", "3.50"},
	} {
		for index, cell := range row {
			pdf.CellFormat(widths[index], 8, cell, "1", 0, "L", false, 0, "")
		}
		pdf.Ln(-1)
	}

	pdf.Ln(6)
	pdf.ImageOptions(imagePath, 18, 0, 45, 0, false, gofpdf.ImageOptions{ImageType: "png"}, 0, "")

	if outputErr := pdf.OutputFileAndClose(path); outputErr != nil {
		t.Fatalf("write pdf fixture %s: %v", path, outputErr)
	}
	if pdfErr := pdf.Error(); pdfErr != nil {
		t.Fatalf("render pdf fixture %s: %v", path, pdfErr)
	}
}
