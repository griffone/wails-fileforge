package preview

import (
	"archive/zip"
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

// writePreviewDOCXFixture writes the same minimal one-page OOXML package the
// doc engine tests generate, so preview coverage uses a real DOCX without a
// binary fixture in the repository.
func writePreviewDOCXFixture(t *testing.T, path string) {
	t.Helper()

	file, err := os.Create(path)
	if err != nil {
		t.Fatalf("create docx: %v", err)
	}

	archive := zip.NewWriter(file)
	writePreviewZipEntry(t, archive, "[Content_Types].xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Types xmlns="http://schemas.openxmlformats.org/package/2006/content-types">
<Default Extension="rels" ContentType="application/vnd.openxmlformats-package.relationships+xml"/>
<Default Extension="xml" ContentType="application/xml"/>
<Override PartName="/word/document.xml" ContentType="application/vnd.openxmlformats-officedocument.wordprocessingml.document.main+xml"/>
</Types>`)
	writePreviewZipEntry(t, archive, "_rels/.rels", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<Relationships xmlns="http://schemas.openxmlformats.org/package/2006/relationships">
<Relationship Id="rId1" Type="http://schemas.openxmlformats.org/officeDocument/2006/relationships/officeDocument" Target="word/document.xml"/>
</Relationships>`)
	writePreviewZipEntry(t, archive, "word/document.xml", `<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<w:document xmlns:w="http://schemas.openxmlformats.org/wordprocessingml/2006/main">
<w:body><w:p><w:r><w:t>Preview page</w:t></w:r></w:p>
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

func writePreviewZipEntry(t *testing.T, archive *zip.Writer, name, content string) {
	t.Helper()

	entry, err := archive.Create(name)
	if err != nil {
		t.Fatalf("create zip entry %s: %v", name, err)
	}
	if _, err := entry.Write([]byte(content)); err != nil {
		t.Fatalf("write zip entry %s: %v", name, err)
	}
}

// encodeTestJPEG builds a frame the document processor returns in place of a
// real pdftoppm result, keeping the test hermetic.
func encodeTestJPEG(t *testing.T, width, height int) []byte {
	t.Helper()

	img := image.NewRGBA(image.Rect(0, 0, width, height))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			img.SetRGBA(x, y, color.RGBA{R: uint8(x % 256), G: uint8(y % 256), B: 96, A: 255})
		}
	}

	var buffer bytes.Buffer
	if err := jpeg.Encode(&buffer, img, nil); err != nil {
		t.Fatalf("encode jpeg fixture: %v", err)
	}
	return buffer.Bytes()
}

func documentPreviewRequest(path string) PreviewRequest {
	return PreviewRequest{
		Path:      path,
		Width:     320,
		Height:    240,
		Format:    "jpeg",
		PageRange: &PageRange{Start: 1, End: 1},
	}
}

func assertPreviewDimensions(t *testing.T, data []byte) {
	t.Helper()

	cfg, format, err := image.DecodeConfig(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("expected a decodable preview image, got %v", err)
	}
	if format != "jpeg" {
		t.Fatalf("expected a jpeg preview, got %q", format)
	}
	if cfg.Width != 320 || cfg.Height != 240 {
		t.Fatalf("expected a 320x240 preview, got %dx%d", cfg.Width, cfg.Height)
	}
}

func capturePDFFromRunner(t *testing.T, pdfData *[]byte) *stubExecRunner {
	t.Helper()

	return &stubExecRunner{
		stdout: encodeTestJPEG(t, 320, 240),
		onRun: func(name string, args []string) {
			if name != "pdftoppm" || len(args) == 0 {
				return
			}
			rendered, err := os.ReadFile(args[len(args)-1])
			if err != nil {
				t.Errorf("read rendered pdf: %v", err)
				return
			}
			*pdfData = rendered
		},
	}
}

func TestDocumentProcessorRendersDOCXPreview(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "report.docx")
	writePreviewDOCXFixture(t, inputPath)

	var renderedPDF []byte
	runner := capturePDFFromRunner(t, &renderedPDF)

	data, contentType, err := NewDocumentProcessor(runner).Process(context.Background(), documentPreviewRequest(inputPath))
	if err != nil {
		t.Fatalf("expected the DOCX preview to succeed, got %v", err)
	}
	if contentType != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", contentType)
	}
	if !bytes.HasPrefix(renderedPDF, []byte("%PDF")) {
		t.Fatalf("expected the document engine to render a PDF, got %q", renderedPDF)
	}
	assertPreviewDimensions(t, data)
}

func TestDocumentProcessorRendersMarkdownPreview(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "notes.md")
	if err := os.WriteFile(inputPath, []byte("# Title\n\nBody text.\n"), 0o600); err != nil {
		t.Fatalf("write markdown: %v", err)
	}

	var renderedPDF []byte
	runner := capturePDFFromRunner(t, &renderedPDF)

	data, contentType, err := NewDocumentProcessor(runner).Process(context.Background(), documentPreviewRequest(inputPath))
	if err != nil {
		t.Fatalf("expected the Markdown preview to succeed, got %v", err)
	}
	if contentType != "image/jpeg" {
		t.Fatalf("expected image/jpeg, got %q", contentType)
	}
	if !bytes.HasPrefix(renderedPDF, []byte("%PDF")) {
		t.Fatalf("expected the Markdown engine to render a PDF, got %q", renderedPDF)
	}
	assertPreviewDimensions(t, data)
}

func TestDocumentProcessorDegradesCorruptDOCXToNoPreview(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "broken.docx")
	if err := os.WriteFile(inputPath, []byte("not a docx package"), 0o600); err != nil {
		t.Fatalf("write corrupt docx: %v", err)
	}

	runner := &stubExecRunner{stdout: encodeTestJPEG(t, 320, 240)}
	data, contentType, err := NewDocumentProcessor(runner).Process(context.Background(), documentPreviewRequest(inputPath))
	if err == nil {
		t.Fatal("expected a corrupt document to have no preview")
	}
	if !errors.Is(err, ErrNoPreview) {
		t.Fatalf("expected ErrNoPreview, got %v", err)
	}
	if data != nil || contentType != "" {
		t.Fatalf("expected no image bytes, got data=%d contentType=%q", len(data), contentType)
	}
	if runner.name != "" {
		t.Fatalf("expected the corrupt document to fail before pdftoppm, got %q", runner.name)
	}
}

func TestDocumentProcessorRejectsOversizedDocumentWithoutRendering(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "huge.docx")
	file, err := os.Create(inputPath)
	if err != nil {
		t.Fatalf("create oversized docx: %v", err)
	}
	if err := file.Truncate(docPreviewMaxInputBytes + 1); err != nil {
		_ = file.Close()
		t.Fatalf("truncate oversized docx: %v", err)
	}
	if err := file.Close(); err != nil {
		t.Fatalf("close oversized docx: %v", err)
	}

	renderer := &countingDOCXRenderer{}
	processor := NewDocumentProcessor(&stubExecRunner{}).(*DocumentProcessor)
	processor.renderer = renderer

	_, _, err = processor.Process(context.Background(), documentPreviewRequest(inputPath))
	if !errors.Is(err, ErrNoPreview) {
		t.Fatalf("expected ErrNoPreview for an oversized document, got %v", err)
	}
	if renderer.calls != 0 {
		t.Fatalf("expected the oversized document to be rejected before rendering, got %d renders", renderer.calls)
	}
}

type countingDOCXRenderer struct {
	calls int
	err   error
}

func (r *countingDOCXRenderer) Render(_ context.Context, _ string, outputPath string) error {
	r.calls++
	if r.err != nil {
		return r.err
	}
	return os.WriteFile(outputPath, []byte("%PDF-1.4\n%%EOF\n"), 0o644)
}

func TestPreviewServiceMarksCorruptDocumentAsNoPreview(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "broken.docx")
	if err := os.WriteFile(inputPath, []byte("not a docx package"), 0o600); err != nil {
		t.Fatalf("write corrupt docx: %v", err)
	}

	svc := newTestService(t, Config{MaxQueue: 4})
	job := &previewJob{Req: documentPreviewRequest(inputPath), State: JobStateQueued, Ctx: context.Background()}
	if err := svc.handleJob(job); err != nil {
		t.Fatalf("expected a missing preview to stay an expected outcome, got %v", err)
	}
	if job.State != JobStateFailed {
		t.Fatalf("expected the card to see a failed preview, got %s", job.State)
	}
	if job.Result == nil || job.Result.Message != ErrNoPreview.Error() {
		t.Fatalf("expected the no-preview message, got %+v", job.Result)
	}
	if job.Result.Success {
		t.Fatal("expected no preview payload to be reported as unsuccessful")
	}
}

func TestPreviewServiceReusesCachedDocumentPreview(t *testing.T) {
	dir := t.TempDir()
	inputPath := filepath.Join(dir, "report.docx")
	writePreviewDOCXFixture(t, inputPath)

	svc := newTestService(t, Config{MaxQueue: 4})
	svc.cache = newTestCache(t, 1<<20, 1<<20, 1<<10)

	runner := &stubExecRunner{stdout: encodeTestJPEG(t, 320, 240)}
	renderer := &countingDOCXRenderer{}
	docProc := NewDocumentProcessor(runner).(*DocumentProcessor)
	docProc.renderer = renderer
	svc.docProc = docProc

	first := &previewJob{Req: documentPreviewRequest(inputPath), State: JobStateQueued, Ctx: context.Background()}
	if err := svc.handleJob(first); err != nil {
		t.Fatalf("first preview job: %v", err)
	}
	if first.State != JobStateSucceeded {
		t.Fatalf("expected the first preview to succeed, got %s (%v)", first.State, first.Result)
	}
	svc.cache.Wait()

	second := &previewJob{Req: documentPreviewRequest(inputPath), State: JobStateQueued, Ctx: context.Background()}
	if err := svc.handleJob(second); err != nil {
		t.Fatalf("second preview job: %v", err)
	}
	if second.State != JobStateSucceeded || second.Result == nil {
		t.Fatalf("expected the cached preview to succeed, got %s", second.State)
	}
	if second.Result.Message != "ok (from cache)" {
		t.Fatalf("expected the cached result, got %q", second.Result.Message)
	}
	if renderer.calls != 1 {
		t.Fatalf("expected one render for two identical previews, got %d", renderer.calls)
	}
}
