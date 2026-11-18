package pdf

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/jung-kurt/gofpdf"

	"fileforge-desktop/internal/models"
	"fileforge-desktop/internal/pdf/engine"
)

const pdfToDOCXToolDocumentXML = `<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:body><w:p><w:r><w:b/><w:t>Stub</w:t></w:r></w:p></w:body></w:document>`

func pdfToDOCXToolRequest(mode string, inputPaths []string, options map[string]any) models.JobRequestV1 {
	return models.JobRequestV1{
		ToolID:     ToolIDPDFToDOCXV1,
		Mode:       mode,
		InputPaths: inputPaths,
		Options:    options,
	}
}

// writePDFToDOCXToolFixture writes a real text PDF with the requested pages.
func writePDFToDOCXToolFixture(t *testing.T, path string, pages int) {
	t.Helper()

	document := gofpdf.New("P", "mm", "A4", "")
	document.SetFont("Helvetica", "", 12)
	for page := 0; page < pages; page++ {
		document.AddPage()
		document.SetXY(20, 30)
		document.MultiCell(0, 8, fmt.Sprintf("Page %d of the PDF to DOCX tool fixture.", page+1), "", "L", false)
	}
	if err := document.OutputFileAndClose(path); err != nil {
		t.Fatalf("write pdf fixture %s: %v", path, err)
	}
}

// writePDFToDOCXToolPackage writes a minimal, structurally valid DOCX package.
func writePDFToDOCXToolPackage(t *testing.T, path string) {
	t.Helper()

	if err := writePDFToDOCXToolPackageFile(path); err != nil {
		t.Fatalf("write docx fixture %s: %v", path, err)
	}
}

func writePDFToDOCXToolPackageFile(path string) error {
	file, createErr := os.Create(path)
	if createErr != nil {
		return createErr
	}

	writer := zip.NewWriter(file)
	entry, entryErr := writer.Create("word/document.xml")
	if entryErr != nil {
		_ = writer.Close()
		_ = file.Close()
		return entryErr
	}
	if _, writeErr := entry.Write([]byte(pdfToDOCXToolDocumentXML)); writeErr != nil {
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

func stubPDFToDOCXPythonPath(t *testing.T, path string, available bool) {
	t.Helper()

	previous := pdf2docxPythonPath
	pdf2docxPythonPath = func() (string, bool) { return path, available }
	t.Cleanup(func() {
		pdf2docxPythonPath = previous
	})
}

func stubPDFToDOCXConverter(t *testing.T, convert func(context.Context, engine.DOCXCommandRunner, engine.PDFToDOCXRequest, func(engine.PDFToDOCXProgressEvent)) (engine.PDFToDOCXResult, *engine.PDFToDOCXError)) {
	t.Helper()

	previous := pdfToDOCXConvert
	pdfToDOCXConvert = convert
	t.Cleanup(func() {
		pdfToDOCXConvert = previous
	})
}

// preparePDFToDOCXToolExecute creates a valid input and stubs the runtime.
func preparePDFToDOCXToolExecute(t *testing.T) (dir, inputPath, pythonPath string) {
	t.Helper()

	dir = t.TempDir()
	inputPath = filepath.Join(dir, "manual.pdf")
	writePDFToDOCXToolFixture(t, inputPath, 2)

	pythonPath = filepath.Join(dir, "python3")
	stubPDFToDOCXPythonPath(t, pythonPath, true)

	return dir, inputPath, pythonPath
}

func TestPDFToDOCXToolManifest(t *testing.T) {
	manifest := NewPDFToDOCXTool().Manifest()

	if manifest.ToolID != "tool.pdf.to_docx" {
		t.Fatalf("expected tool.pdf.to_docx, got %q", manifest.ToolID)
	}
	if manifest.Name != "PDF to DOCX" {
		t.Fatalf("expected the PDF to DOCX name, got %q", manifest.Name)
	}
	if manifest.Domain != "pdf" || manifest.Version != "v1" {
		t.Fatalf("unexpected domain/version: %q/%q", manifest.Domain, manifest.Version)
	}
	if !manifest.SupportsSingle || manifest.SupportsBatch {
		t.Fatalf("expected single support only, got single=%v batch=%v", manifest.SupportsSingle, manifest.SupportsBatch)
	}
	if strings.Join(manifest.InputExtensions, ",") != "pdf" {
		t.Fatalf("unexpected input extensions: %v", manifest.InputExtensions)
	}
	if strings.Join(manifest.OutputExtensions, ",") != "docx" {
		t.Fatalf("unexpected output extensions: %v", manifest.OutputExtensions)
	}
	if strings.Join(manifest.RuntimeDeps, ",") != "python,pdf2docx" {
		t.Fatalf("unexpected runtime dependencies: %v", manifest.RuntimeDeps)
	}
	if strings.Join(manifest.Tags, ",") != "pdf,docx,word,layout,fidelity" {
		t.Fatalf("unexpected tags: %v", manifest.Tags)
	}
}

func TestPDFToDOCXToolRuntimeState(t *testing.T) {
	tests := []struct {
		name        string
		pythonPath  string
		available   bool
		wantStatus  string
		wantHealthy bool
		wantReason  string
	}{
		{
			name:        "installed runtime is enabled",
			pythonPath:  "/toolchain/pdf2docx-venv/bin/python",
			available:   true,
			wantStatus:  "enabled",
			wantHealthy: true,
		},
		{
			name:        "missing runtime is degraded",
			wantStatus:  "degraded",
			wantHealthy: false,
			wantReason:  PDFToDOCXUnavailableReason,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			stubPDFToDOCXPythonPath(t, test.pythonPath, test.available)

			state := NewPDFToDOCXTool().RuntimeState(context.Background())
			if state.Status != test.wantStatus || state.Healthy != test.wantHealthy {
				t.Fatalf("unexpected runtime state: %+v", state)
			}
			if state.Reason != test.wantReason {
				t.Fatalf("expected reason %q, got %q", test.wantReason, state.Reason)
			}
		})
	}
}

func TestPDFToDOCXToolPlan(t *testing.T) {
	t.Run("auto output", func(t *testing.T) {
		dir, inputPath, _ := preparePDFToDOCXToolExecute(t)

		plan, planErr := NewPDFToDOCXTool().Plan(context.Background(), pdfToDOCXToolRequest("single", []string{inputPath}, nil))
		if planErr != nil {
			t.Fatalf("expected plan to succeed, got %v", planErr)
		}
		if !plan.Success {
			t.Fatalf("expected a successful plan, got %+v", plan)
		}
		if plan.Message != "Ready to convert 2 pages to DOCX." {
			t.Fatalf("unexpected plan message: %q", plan.Message)
		}
		if len(plan.Outputs) != 1 {
			t.Fatalf("expected one planned output, got %+v", plan.Outputs)
		}

		output := plan.Outputs[0]
		expectedPath := filepath.Join(dir, "manual.docx")
		if output.OutputPath != expectedPath {
			t.Fatalf("expected output %s, got %s", expectedPath, output.OutputPath)
		}
		if output.InputPath != inputPath {
			t.Fatalf("expected input %s, got %s", inputPath, output.InputPath)
		}
		if output.OutputKind != "file" || output.OutputCount != 1 {
			t.Fatalf("unexpected output metadata: %+v", output)
		}
		if output.Adjusted {
			t.Fatalf("expected an unadjusted output, got %+v", output)
		}
	})

	t.Run("explicit output path", func(t *testing.T) {
		dir, inputPath, _ := preparePDFToDOCXToolExecute(t)
		explicitPath := filepath.Join(dir, "custom.docx")

		plan, planErr := NewPDFToDOCXTool().Plan(context.Background(), pdfToDOCXToolRequest("single", []string{inputPath}, map[string]any{
			"outputPath": explicitPath,
		}))
		if planErr != nil {
			t.Fatalf("expected plan to succeed, got %v", planErr)
		}
		if plan.Outputs[0].OutputPath != explicitPath {
			t.Fatalf("expected the explicit output, got %s", plan.Outputs[0].OutputPath)
		}
	})

	t.Run("existing output path fails", func(t *testing.T) {
		dir, inputPath, _ := preparePDFToDOCXToolExecute(t)
		existingPath := filepath.Join(dir, "existing.docx")
		if err := os.WriteFile(existingPath, []byte("existing"), 0o644); err != nil {
			t.Fatalf("write existing output: %v", err)
		}

		_, planErr := NewPDFToDOCXTool().Plan(context.Background(), pdfToDOCXToolRequest("single", []string{inputPath}, map[string]any{
			"outputPath": existingPath,
		}))
		if planErr == nil {
			t.Fatal("expected the existing output to fail the plan")
		}
		if planErr.DetailCode != engine.PDFToDOCXErrorOutputFailed {
			t.Fatalf("expected %s, got %s", engine.PDFToDOCXErrorOutputFailed, planErr.DetailCode)
		}
	})

	t.Run("non pdf input fails", func(t *testing.T) {
		_, inputPath, _ := preparePDFToDOCXToolExecute(t)
		textPath := filepath.Join(filepath.Dir(inputPath), "notes.txt")
		if err := os.WriteFile(textPath, []byte("notes"), 0o644); err != nil {
			t.Fatalf("write text input: %v", err)
		}

		_, planErr := NewPDFToDOCXTool().Plan(context.Background(), pdfToDOCXToolRequest("single", []string{textPath}, nil))
		if planErr == nil {
			t.Fatal("expected a non-PDF input to fail the plan")
		}
	})

	t.Run("missing input fails", func(t *testing.T) {
		_, inputPath, _ := preparePDFToDOCXToolExecute(t)

		_, planErr := NewPDFToDOCXTool().Plan(context.Background(), pdfToDOCXToolRequest("single", []string{
			filepath.Join(filepath.Dir(inputPath), "missing.pdf"),
		}, nil))
		if planErr == nil {
			t.Fatal("expected a missing input to fail the plan")
		}
	})

	t.Run("batch mode fails", func(t *testing.T) {
		_, inputPath, _ := preparePDFToDOCXToolExecute(t)

		_, planErr := NewPDFToDOCXTool().Plan(context.Background(), pdfToDOCXToolRequest("batch", []string{inputPath}, nil))
		if planErr == nil {
			t.Fatal("expected batch mode to fail the plan")
		}
	})

	t.Run("missing runtime fails", func(t *testing.T) {
		dir := t.TempDir()
		inputPath := filepath.Join(dir, "manual.pdf")
		writePDFToDOCXToolFixture(t, inputPath, 1)
		stubPDFToDOCXPythonPath(t, "", false)

		_, planErr := NewPDFToDOCXTool().Plan(context.Background(), pdfToDOCXToolRequest("single", []string{inputPath}, nil))
		if planErr == nil {
			t.Fatal("expected the missing runtime to fail the plan")
		}
		if planErr.Code != models.ErrorCodeRuntimeDepMissing {
			t.Fatalf("expected %s, got %s", models.ErrorCodeRuntimeDepMissing, planErr.Code)
		}
		if planErr.DetailCode != engine.PDFToDOCXErrorRuntimeUnavailable {
			t.Fatalf("expected %s, got %s", engine.PDFToDOCXErrorRuntimeUnavailable, planErr.DetailCode)
		}
		if planErr.Message != PDFToDOCXUnavailableReason {
			t.Fatalf("unexpected message: %q", planErr.Message)
		}
	})
}

func TestPDFToDOCXToolExecuteSingleWithProgress(t *testing.T) {
	dir, inputPath, pythonPath := preparePDFToDOCXToolExecute(t)
	expectedOutput := filepath.Join(dir, "manual.docx")

	var captured engine.PDFToDOCXRequest
	stubPDFToDOCXConverter(t, func(_ context.Context, runner engine.DOCXCommandRunner, req engine.PDFToDOCXRequest, onProgress func(engine.PDFToDOCXProgressEvent)) (engine.PDFToDOCXResult, *engine.PDFToDOCXError) {
		captured = req
		if runner == nil {
			t.Error("expected an exec command runner")
		}

		if onProgress != nil {
			onProgress(engine.PDFToDOCXProgressEvent{Stage: engine.PDFToDOCXStageParse, Percent: 15, Message: "Parsing pages"})
			onProgress(engine.PDFToDOCXProgressEvent{Stage: engine.PDFToDOCXStageWrite, Percent: 100, Message: "DOCX saved"})
		}
		if err := writePDFToDOCXToolPackageFile(req.OutputPath); err != nil {
			return engine.PDFToDOCXResult{}, &engine.PDFToDOCXError{Code: engine.PDFToDOCXErrorOutputFailed, Message: "stub write failed", Cause: err}
		}

		return engine.PDFToDOCXResult{
			OutputPath: req.OutputPath,
			Stats: engine.PDFToDOCXStats{
				Paragraphs:     4,
				Runs:           8,
				BoldRuns:       2,
				Images:         1,
				Tables:         1,
				TextCharacters: 120,
			},
		}, nil
	})

	var progress []models.JobProgressV1
	item, jobErr := NewPDFToDOCXTool().ExecuteSingleWithProgress(context.Background(), pdfToDOCXToolRequest("single", []string{inputPath}, nil), func(update models.JobProgressV1) {
		progress = append(progress, update)
	})
	if jobErr != nil {
		t.Fatalf("expected the conversion to succeed, got %v", jobErr)
	}

	if !item.Success || item.OutputPath != expectedOutput || item.InputPath != inputPath {
		t.Fatalf("unexpected result item: %+v", item)
	}
	if len(item.Outputs) != 1 || item.Outputs[0] != expectedOutput || item.OutputCount != 1 {
		t.Fatalf("unexpected outputs: %+v", item)
	}
	for _, fragment := range []string{"4 paragraphs", "2 bold runs", "1 image", "1 table"} {
		if !strings.Contains(item.Message, fragment) {
			t.Fatalf("expected %q in the message, got %q", fragment, item.Message)
		}
	}
	if strings.Contains(item.Message, "No text was found") {
		t.Fatalf("did not expect the scanned warning, got %q", item.Message)
	}

	if captured.InputPath != inputPath || captured.OutputPath != expectedOutput || captured.PythonPath != pythonPath {
		t.Fatalf("unexpected conversion request: %+v", captured)
	}

	if len(progress) != 2 {
		t.Fatalf("expected two progress updates, got %+v", progress)
	}
	for index, wantCurrent := range []int{15, 100} {
		if progress[index].Current != wantCurrent || progress[index].Total != 100 || progress[index].Stage != "processing" {
			t.Fatalf("unexpected progress update %d: %+v", index, progress[index])
		}
	}
}

func TestPDFToDOCXToolExecuteFailure(t *testing.T) {
	dir, inputPath, _ := preparePDFToDOCXToolExecute(t)

	stubPDFToDOCXConverter(t, func(_ context.Context, _ engine.DOCXCommandRunner, _ engine.PDFToDOCXRequest, _ func(engine.PDFToDOCXProgressEvent)) (engine.PDFToDOCXResult, *engine.PDFToDOCXError) {
		return engine.PDFToDOCXResult{}, &engine.PDFToDOCXError{
			Code:    engine.PDFToDOCXErrorConversionFailed,
			Message: "failed to convert manual.pdf to DOCX",
			Details: map[string]any{"stderr": "traceback"},
			Cause:   errors.New("exit status 1"),
		}
	})

	item, jobErr := NewPDFToDOCXTool().ExecuteSingle(context.Background(), pdfToDOCXToolRequest("single", []string{inputPath}, nil))
	if jobErr == nil {
		t.Fatal("expected the conversion to fail")
	}
	if jobErr.Code != engine.PDFToDOCXErrorConversionFailed || jobErr.DetailCode != engine.PDFToDOCXErrorConversionFailed {
		t.Fatalf("unexpected error codes: %+v", jobErr)
	}
	if item.Success {
		t.Fatalf("expected a failure item, got %+v", item)
	}
	if item.OutputPath != filepath.Join(dir, "manual.docx") {
		t.Fatalf("expected the planned output path, got %q", item.OutputPath)
	}
	if item.Error == nil || item.Message != jobErr.Message {
		t.Fatalf("expected the item to mirror the error, got %+v", item)
	}
	if reason, ok := jobErr.Details["reason"].(string); !ok || reason != "exit status 1" {
		t.Fatalf("expected the bounded reason, got %v", jobErr.Details)
	}
}

func TestPDFToDOCXToolExecuteCancellation(t *testing.T) {
	t.Run("engine reports cancellation", func(t *testing.T) {
		_, inputPath, _ := preparePDFToDOCXToolExecute(t)
		stubPDFToDOCXConverter(t, func(_ context.Context, _ engine.DOCXCommandRunner, _ engine.PDFToDOCXRequest, _ func(engine.PDFToDOCXProgressEvent)) (engine.PDFToDOCXResult, *engine.PDFToDOCXError) {
			return engine.PDFToDOCXResult{}, &engine.PDFToDOCXError{Code: engine.PDFToDOCXErrorCancelled, Message: "PDF to DOCX canceled"}
		})

		_, jobErr := NewPDFToDOCXTool().ExecuteSingle(context.Background(), pdfToDOCXToolRequest("single", []string{inputPath}, nil))
		assertPDFToDOCXCancelled(t, jobErr)
	})

	t.Run("context is cancelled", func(t *testing.T) {
		_, inputPath, _ := preparePDFToDOCXToolExecute(t)
		stubPDFToDOCXConverter(t, func(_ context.Context, _ engine.DOCXCommandRunner, _ engine.PDFToDOCXRequest, _ func(engine.PDFToDOCXProgressEvent)) (engine.PDFToDOCXResult, *engine.PDFToDOCXError) {
			return engine.PDFToDOCXResult{}, &engine.PDFToDOCXError{Code: engine.PDFToDOCXErrorConversionFailed, Message: "killed"}
		})

		ctx, cancel := context.WithCancel(context.Background())
		cancel()

		_, jobErr := NewPDFToDOCXTool().ExecuteSingle(ctx, pdfToDOCXToolRequest("single", []string{inputPath}, nil))
		assertPDFToDOCXCancelled(t, jobErr)
	})
}

func assertPDFToDOCXCancelled(t *testing.T, jobErr *models.JobErrorV1) {
	t.Helper()

	if jobErr == nil {
		t.Fatal("expected the conversion to be cancelled")
	}
	if jobErr.Code != models.ErrorCodeCancelledByUser {
		t.Fatalf("expected %s, got %s", models.ErrorCodeCancelledByUser, jobErr.Code)
	}
	if jobErr.DetailCode != engine.PDFToDOCXErrorCancelled {
		t.Fatalf("expected %s, got %s", engine.PDFToDOCXErrorCancelled, jobErr.DetailCode)
	}
	if jobErr.Message != "PDF to DOCX was canceled." {
		t.Fatalf("unexpected message: %q", jobErr.Message)
	}
}

func TestPDFToDOCXToolExecuteScannedWarning(t *testing.T) {
	_, inputPath, _ := preparePDFToDOCXToolExecute(t)

	stubPDFToDOCXConverter(t, func(_ context.Context, _ engine.DOCXCommandRunner, req engine.PDFToDOCXRequest, _ func(engine.PDFToDOCXProgressEvent)) (engine.PDFToDOCXResult, *engine.PDFToDOCXError) {
		if err := writePDFToDOCXToolPackageFile(req.OutputPath); err != nil {
			return engine.PDFToDOCXResult{}, &engine.PDFToDOCXError{Code: engine.PDFToDOCXErrorOutputFailed, Message: "stub write failed", Cause: err}
		}

		return engine.PDFToDOCXResult{
			OutputPath: req.OutputPath,
			Stats: engine.PDFToDOCXStats{
				Paragraphs: 1,
				Runs:       1,
			},
		}, nil
	})

	item, jobErr := NewPDFToDOCXTool().ExecuteSingle(context.Background(), pdfToDOCXToolRequest("single", []string{inputPath}, nil))
	if jobErr != nil {
		t.Fatalf("expected the conversion to succeed, got %v", jobErr)
	}
	if !strings.Contains(item.Message, "No text was found; the PDF may be scanned. Run PDF OCR first.") {
		t.Fatalf("expected the scanned warning, got %q", item.Message)
	}
}
