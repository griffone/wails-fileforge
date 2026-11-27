package engine

import (
	"archive/zip"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"fileforge-desktop/internal/toolchain"
)

type stubRenderer struct {
	render func(ctx context.Context, inputPath, outputPath string) error
}

func (r stubRenderer) Render(ctx context.Context, inputPath, outputPath string) error {
	if r.render == nil {
		return nil
	}

	return r.render(ctx, inputPath, outputPath)
}

type stubCommandRunner struct {
	run func(ctx context.Context, name string, args ...string) (CommandResult, error)
}

func (r stubCommandRunner) Run(ctx context.Context, name string, args ...string) (CommandResult, error) {
	if r.run == nil {
		return CommandResult{}, nil
	}

	return r.run(ctx, name, args...)
}

// writeDOCXFixture writes a minimal valid OOXML package. Text is separated by
// explicit page breaks so the renderer produces exactly the requested pages.
func writeDOCXFixture(t *testing.T, path string, pages int) {
	t.Helper()

	if pages < 1 {
		pages = 1
	}

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}

	archive := zip.NewWriter(file)
	writeZipEntry(t, archive, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)
	writeZipEntry(t, archive, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)

	var body strings.Builder
	for page := 1; page <= pages; page++ {
		body.WriteString("<w:p><w:r>")
		if page > 1 {
			body.WriteString(`<w:br w:type="page"/>`)
		}
		fmt.Fprintf(&body, `<w:t>Page %d</w:t></w:r></w:p>`, page)
	}
	writeZipEntry(t, archive, "word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body>`+body.String()+`
<w:sectPr><w:pgSz w:w="11906" w:h="16838"/><w:pgMar w:top="1440" w:right="1440" w:bottom="1440" w:left="1440" w:header="720" w:footer="720" w:gutter="0"/></w:sectPr>
</w:body>
</w:document>`)

	if err := archive.Close(); err != nil {
		t.Fatalf("close docx writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close docx: %v", err)
	}
}

func writeZipEntry(t *testing.T, archive *zip.Writer, name, content string) {
	t.Helper()

	entry, err := archive.Create(name)
	if err != nil {
		t.Fatalf("create zip entry %s: %v", name, err)
	}
	if _, err := entry.Write([]byte(content)); err != nil {
		t.Fatalf("write zip entry %s: %v", name, err)
	}
}

func writeProtectedDOCX(t *testing.T, path string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create protected docx: %v", err)
	}

	archive := zip.NewWriter(file)
	writeZipEntry(t, archive, "word/settings.xml", `<w:settings xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main"><w:documentProtection w:edit="readOnly" w:enforcement="1"/></w:settings>`)
	if err := archive.Close(); err != nil {
		t.Fatalf("close protected docx writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close protected docx: %v", err)
	}
}

func writeEncryptedZipDOCX(t *testing.T, path string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create encrypted docx: %v", err)
	}

	archive := zip.NewWriter(file)
	header := &zip.FileHeader{Name: "word/document.xml", Method: zip.Store}
	header.Flags |= 0x1
	entry, err := archive.CreateHeader(header)
	if err != nil {
		t.Fatalf("create encrypted zip entry: %v", err)
	}
	if _, err := entry.Write([]byte("encrypted")); err != nil {
		t.Fatalf("write encrypted zip entry: %v", err)
	}
	if err := archive.Close(); err != nil {
		t.Fatalf("close encrypted docx writer: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close encrypted docx: %v", err)
	}
}

func writeCFBDOCX(t *testing.T, path string) {
	t.Helper()

	content := append([]byte{}, cfbMagic...)
	content = append(content, make([]byte, 512)...)
	if err := os.WriteFile(path, content, 0o644); err != nil {
		t.Fatalf("write CFB docx: %v", err)
	}
}

func docxErrorCode(t *testing.T, err error) string {
	t.Helper()

	var docxErr *DOCXError
	if !errors.As(err, &docxErr) {
		t.Fatalf("expected *DOCXError, got %T (%v)", err, err)
	}

	return docxErr.Code
}

func TestConvertDOCXRendersDOCXWithLibraryRenderer(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "report.docx")
	outputPath := filepath.Join(dir, "report.pdf")
	writeDOCXFixture(t, inputPath, 1)

	result, err := ConvertDOCX(context.Background(), nil, nil, ConvertDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
	})
	if err != nil {
		t.Fatalf("expected conversion to succeed, got %v", err)
	}
	if result.EngineUsed != EngineDocx2PDF {
		t.Fatalf("expected engine %q, got %q", EngineDocx2PDF, result.EngineUsed)
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("expected output to exist: %v", statErr)
	}
	if got := pdfPageCount(t, outputPath); got < 1 {
		t.Fatalf("expected at least one page, got %d", got)
	}
}

func TestConvertDOCXAppliesPageSelection(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "report.docx")
	outputPath := filepath.Join(dir, "report.pdf")
	writeDOCXFixture(t, inputPath, 4)

	_, err := ConvertDOCX(context.Background(), nil, nil, ConvertDOCXRequest{
		InputPath:     inputPath,
		OutputPath:    outputPath,
		PageSelection: "2-3",
	})
	if err != nil {
		t.Fatalf("expected conversion to succeed, got %v", err)
	}
	if got := pdfPageCount(t, outputPath); got != 2 {
		t.Fatalf("expected 2 pages after the selection, got %d", got)
	}
}

func TestConvertDOCXRejectsPageSelectionBeyondDocument(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "report.docx")
	outputPath := filepath.Join(dir, "report.pdf")
	writeDOCXFixture(t, inputPath, 2)

	_, err := ConvertDOCX(context.Background(), nil, nil, ConvertDOCXRequest{
		InputPath:     inputPath,
		OutputPath:    outputPath,
		PageSelection: "9-12",
	})
	if err == nil {
		t.Fatal("expected out-of-range page selection to fail")
	}
	if code := docxErrorCode(t, err); code != ErrorCodeDocxPageSelectionInvalid {
		t.Fatalf("expected %s, got %s", ErrorCodeDocxPageSelectionInvalid, code)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no published output, got %v", statErr)
	}
}

func TestConvertDOCXPDFAUsesGhostscript(t *testing.T) {
	if _, err := exec.LookPath("gs"); err != nil {
		t.Skip("ghostscript is not installed")
	}

	dir := t.TempDir()
	inputPath := filepath.Join(dir, "report.docx")
	outputPath := filepath.Join(dir, "report.pdf")
	writeDOCXFixture(t, inputPath, 1)

	_, err := ConvertDOCX(context.Background(), nil, nil, ConvertDOCXRequest{
		InputPath:  inputPath,
		OutputPath: outputPath,
		PDFA:       true,
	})
	if err != nil {
		t.Fatalf("expected PDF/A conversion to succeed, got %v", err)
	}
	if _, statErr := os.Stat(outputPath); statErr != nil {
		t.Fatalf("expected output to exist: %v", statErr)
	}
	if got := pdfPageCount(t, outputPath); got != 1 {
		t.Fatalf("expected one page, got %d", got)
	}
}

func TestConvertDOCXPDFAUsesResolvedGhostscriptBinary(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "report.docx")
	outputPath := filepath.Join(dir, "report.pdf")
	resolvedPath := filepath.Join(dir, "gswin64c")
	writeDOCXFixture(t, inputPath, 1)

	original := resolveGhostscriptBinary
	resolveGhostscriptBinary = func() (toolchain.GhostscriptBinary, bool) {
		return toolchain.GhostscriptBinary{Name: "gswin64c", Path: resolvedPath}, true
	}
	t.Cleanup(func() {
		resolveGhostscriptBinary = original
	})

	var invokedName string
	renderer := stubRenderer{render: func(_ context.Context, _ string, out string) error {
		writeMultiPagePDF(t, out, 1)
		return nil
	}}
	runner := stubCommandRunner{run: func(_ context.Context, name string, args ...string) (CommandResult, error) {
		invokedName = name
		content, readErr := os.ReadFile(inputPath)
		if readErr != nil {
			t.Fatalf("read rendered input: %v", readErr)
		}
		for _, arg := range args {
			if strings.HasPrefix(arg, "-sOutputFile=") {
				return CommandResult{}, os.WriteFile(strings.TrimPrefix(arg, "-sOutputFile="), content, 0o644)
			}
		}
		t.Fatalf("expected -sOutputFile in args, got %v", args)

		return CommandResult{}, nil
	}}

	_, err := ConvertDOCX(context.Background(), renderer, runner, ConvertDOCXRequest{
		InputPath:          inputPath,
		OutputPath:         outputPath,
		PDFA:               true,
		SkipProtectedCheck: true,
	})
	if err != nil {
		t.Fatalf("expected PDF/A conversion to succeed, got %v", err)
	}
	if invokedName != resolvedPath {
		t.Fatalf("expected the resolved path %q, got %q", resolvedPath, invokedName)
	}
}

func TestConvertDOCXPDFAReportsMissingGhostscript(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "report.docx")
	outputPath := filepath.Join(dir, "report.pdf")
	writeDOCXFixture(t, inputPath, 1)

	renderer := stubRenderer{render: func(_ context.Context, _ string, out string) error {
		writeMultiPagePDF(t, out, 1)
		return nil
	}}
	runner := stubCommandRunner{run: func(_ context.Context, _ string, _ ...string) (CommandResult, error) {
		return CommandResult{}, &exec.Error{Name: "gs", Err: exec.ErrNotFound}
	}}

	_, err := ConvertDOCX(context.Background(), renderer, runner, ConvertDOCXRequest{
		InputPath:          inputPath,
		OutputPath:         outputPath,
		PDFA:               true,
		SkipProtectedCheck: true,
	})
	if err == nil {
		t.Fatal("expected a missing Ghostscript error")
	}
	if code := docxErrorCode(t, err); code != ErrorCodeDocxPDFAGhostscriptMissing {
		t.Fatalf("expected %s, got %s", ErrorCodeDocxPDFAGhostscriptMissing, code)
	}
	var docxErr *DOCXError
	if !errors.As(err, &docxErr) {
		t.Fatalf("expected *DOCXError, got %T", err)
	}
	for _, candidate := range toolchain.GhostscriptBinaryNames {
		if !strings.Contains(docxErr.Message, candidate) {
			t.Fatalf("expected the error to list %q, got %q", candidate, docxErr.Message)
		}
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no silent non-archival output, got %v", statErr)
	}
}

func TestConvertDOCXPDFAFailureDoesNotPublishPartialOutput(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "report.docx")
	outputPath := filepath.Join(dir, "report.pdf")
	writeDOCXFixture(t, inputPath, 1)

	renderer := stubRenderer{render: func(_ context.Context, _ string, out string) error {
		writeMultiPagePDF(t, out, 1)
		return nil
	}}
	runner := stubCommandRunner{run: func(_ context.Context, _ string, _ ...string) (CommandResult, error) {
		return CommandResult{Stderr: "gs exploded"}, errors.New("gs exploded")
	}}

	_, err := ConvertDOCX(context.Background(), renderer, runner, ConvertDOCXRequest{
		InputPath:          inputPath,
		OutputPath:         outputPath,
		PDFA:               true,
		SkipProtectedCheck: true,
	})
	if err == nil {
		t.Fatal("expected Ghostscript failure to surface")
	}
	if code := docxErrorCode(t, err); code != ErrorCodeDocxPDFAUnsupported {
		t.Fatalf("expected %s, got %s", ErrorCodeDocxPDFAUnsupported, code)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no published output, got %v", statErr)
	}
}

func TestConvertDOCXDoesNotPublishOutputWhenRendererFails(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeDOCXFixture(t, inputPath, 1)

	renderer := stubRenderer{render: func(_ context.Context, _ string, out string) error {
		if err := os.WriteFile(out, []byte("%PDF-partial"), 0o644); err != nil {
			t.Fatalf("write partial output: %v", err)
		}
		return errors.New("render exploded")
	}}

	_, err := ConvertDOCX(context.Background(), renderer, nil, ConvertDOCXRequest{
		InputPath:          inputPath,
		OutputPath:         outputPath,
		SkipProtectedCheck: true,
	})
	if err == nil {
		t.Fatal("expected the renderer error to surface")
	}
	if code := docxErrorCode(t, err); code != ErrorCodeDocxPrimaryExecution {
		t.Fatalf("expected %s, got %s", ErrorCodeDocxPrimaryExecution, code)
	}
	if _, statErr := os.Stat(outputPath); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("expected no published output, got %v", statErr)
	}
}

func TestConvertDOCXReportsFontFailures(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeDOCXFixture(t, inputPath, 1)

	renderer := stubRenderer{render: func(context.Context, string, string) error {
		return errors.New("load font /missing.ttf: no such file")
	}}

	_, err := ConvertDOCX(context.Background(), renderer, nil, ConvertDOCXRequest{
		InputPath:          inputPath,
		OutputPath:         outputPath,
		SkipProtectedCheck: true,
	})
	if err == nil {
		t.Fatal("expected the font error to surface")
	}
	if code := docxErrorCode(t, err); code != ErrorCodeDocxFontSubstitution {
		t.Fatalf("expected %s, got %s", ErrorCodeDocxFontSubstitution, code)
	}
}

func TestConvertDOCXHonorsCancellation(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "input.docx")
	outputPath := filepath.Join(dir, "output.pdf")
	writeDOCXFixture(t, inputPath, 1)

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	renderer := stubRenderer{render: func(ctx context.Context, _ string, _ string) error {
		return ctx.Err()
	}}

	_, err := ConvertDOCX(ctx, renderer, nil, ConvertDOCXRequest{
		InputPath:          inputPath,
		OutputPath:         outputPath,
		SkipProtectedCheck: true,
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("expected context.Canceled, got %v", err)
	}
}

func TestDetectProtectedDOCX(t *testing.T) {
	tests := []struct {
		name     string
		write    func(t *testing.T, path string)
		wantCode string
		wantOK   bool
	}{
		{
			name:   "plain package",
			write:  func(t *testing.T, path string) { writeDOCXFixture(t, path, 1) },
			wantOK: true,
		},
		{
			name:     "document protection metadata",
			write:    writeProtectedDOCX,
			wantCode: ErrorCodeDocxInputProtected,
		},
		{
			name:     "zip encryption flag",
			write:    writeEncryptedZipDOCX,
			wantCode: ErrorCodeDocxInputEncrypted,
		},
		{
			name:     "CFB document",
			write:    writeCFBDOCX,
			wantCode: ErrorCodeDocxInputEncrypted,
		},
		{
			name: "corrupt package",
			write: func(t *testing.T, path string) {
				t.Helper()
				if err := os.WriteFile(path, []byte("not a zip package"), 0o644); err != nil {
					t.Fatalf("write corrupt docx: %v", err)
				}
			},
			wantCode: ErrorCodeDocxInputEncrypted,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "input.docx")
			test.write(t, path)

			err := DetectProtectedDOCX(path)
			if test.wantOK {
				if err != nil {
					t.Fatalf("expected no protection error, got %v", err)
				}
				return
			}
			if err == nil {
				t.Fatalf("expected %s, got nil", test.wantCode)
			}
			if code := docxErrorCode(t, err); code != test.wantCode {
				t.Fatalf("expected %s, got %s", test.wantCode, code)
			}
		})
	}
}
